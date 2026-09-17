package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// Event levels live on the event TYPE row (migration 073), not on `settings`.
//
// Two things were reproduced before the change (2026-09-15):
//
//   - POSTing an event of a CUSTOM type with level 12 returned 201 and stored the
//     12. The modal hides the level field for a custom type but never clears it,
//     so switching the type dropdown carried the old value into the request, and
//     the server's is_system gate meant nothing checked.
//   - Inserting a brand-new SYSTEM type and POSTing an event of it silently
//     applied Marshal's Guard's baseline and ceiling, because the column switch
//     defaulted to MG's columns for anything that was not "ZS".
//
// Both are now errors: has_level = 0 refuses a level, and a system type with no
// numbers of its own is a 500 rather than a borrow.

func typeIDByShort(t *testing.T, short string) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name = ?`, short).Scan(&id); err != nil {
		t.Fatalf("event type %q missing: %v", short, err)
	}
	return id
}

func mgTypeID(t *testing.T) int { return typeIDByShort(t, "MG") }

func typeLevelsOf(t *testing.T, typeID int) (has bool, baseline, max *int) {
	t.Helper()
	tl, err := loadTypeLevels(db, typeID)
	if err != nil {
		t.Fatalf("loadTypeLevels(%d): %v", typeID, err)
	}
	return tl.HasLevel, tl.Baseline, tl.Max
}

func newCustomType(t *testing.T, name, short string, hasLevel bool) int {
	t.Helper()
	flag := 0
	if hasLevel {
		flag = 1
	}
	res, err := db.Exec(`INSERT INTO schedule_event_types (name, short_name, icon, is_system, active, sort_order, has_level)
		VALUES (?, ?, '🎯', 0, 1, 50, ?)`, name, short, flag)
	if err != nil {
		t.Fatalf("seed custom type: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func postEvent(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := scheduleTestActor(httptest.NewRequest(http.MethodPost, "/api/schedule/events", strings.NewReader(string(b))))
	rr := httptest.NewRecorder()
	createScheduleEvent(rr, req)
	return rr
}

func putEvent(t *testing.T, id int, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPut, "/api/schedule/events/"+strconv.Itoa(id), strings.NewReader(string(b)))
	r = mux.SetURLVars(r, map[string]string{"id": strconv.Itoa(id)})
	r = r.WithContext(context.WithValue(r.Context(), authUserKey,
		&AuthUser{ID: 1, Username: "tester", IsAdmin: true}))
	rr := httptest.NewRecorder()
	updateScheduleEvent(rr, r)
	return rr
}

func putEventType(t *testing.T, id int, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPut, "/api/schedule/event-types/"+strconv.Itoa(id), strings.NewReader(string(b)))
	r = mux.SetURLVars(r, map[string]string{"id": strconv.Itoa(id)})
	r = r.WithContext(context.WithValue(r.Context(), authUserKey,
		&AuthUser{ID: 1, Username: "tester", IsAdmin: true}))
	rr := httptest.NewRecorder()
	updateScheduleEventType(rr, r)
	return rr
}

func putCeiling(t *testing.T, id int, max any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"max_level": max})
	r := httptest.NewRequest(http.MethodPut, "/api/schedule/event-types/"+strconv.Itoa(id)+"/ceiling", strings.NewReader(string(b)))
	r = mux.SetURLVars(r, map[string]string{"id": strconv.Itoa(id)})
	r = r.WithContext(context.WithValue(r.Context(), authUserKey,
		&AuthUser{ID: 1, Username: "tester", IsAdmin: true}))
	rr := httptest.NewRecorder()
	updateScheduleEventTypeCeiling(rr, r)
	return rr
}

// The migration must carry the configured numbers across verbatim, or every
// install silently reverts to the defaults on upgrade.
func TestMigrationCopiesSettingsLevelsOntoTypeRows(t *testing.T) {
	setupSettingsTestDB(t)

	// The type rows arrive already migrated, so re-run the copy over values the
	// install did not have at migration time.
	if _, err := db.Exec(`UPDATE settings SET mg_baseline=12, max_mg_level=70, zs_baseline=11, max_zs_level=12 WHERE id=1`); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	for _, q := range []string{
		`UPDATE schedule_event_types SET has_level=1,
		   baseline_level=(SELECT COALESCE(mg_baseline,1) FROM settings WHERE id=1),
		   max_level=(SELECT COALESCE(max_mg_level,1) FROM settings WHERE id=1)
		 WHERE short_name='MG' AND is_system=1`,
		`UPDATE schedule_event_types SET has_level=1,
		   baseline_level=(SELECT COALESCE(zs_baseline,1) FROM settings WHERE id=1),
		   max_level=(SELECT COALESCE(max_zs_level,1) FROM settings WHERE id=1)
		 WHERE short_name='ZS' AND is_system=1`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("migration copy: %v", err)
		}
	}

	for _, tc := range []struct{ short string; wantBase, wantMax int }{
		{"MG", 12, 70},
		{"ZS", 11, 12},
	} {
		has, base, max := typeLevelsOf(t, typeIDByShort(t, tc.short))
		if !has || base == nil || max == nil {
			t.Fatalf("%s: has_level=%v baseline=%v max=%v — the copy did not land", tc.short, has, base, max)
		}
		if *base != tc.wantBase || *max != tc.wantMax {
			t.Errorf("%s came across as %d/%d, want %d/%d", tc.short, *base, *max, tc.wantBase, tc.wantMax)
		}
	}
}

// A level on a type that does not carry one is REFUSED, on create and on update.
// This is #85's leak: the field hides on a type switch but keeps its value.
func TestCustomTypeLevelRequiresTheFlag(t *testing.T) {
	setupSettingsTestDB(t)
	typeID := newCustomType(t, "Scratch Encounter", "SCR", false)

	rr := postEvent(t, map[string]any{
		"event_date": "2026-10-05", "event_type_id": typeID, "event_time": "12:00", "level": 12,
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("level on an unlevelled type accepted with %d (body %s)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "do not carry a level") {
		t.Errorf("message should say the type carries no level; got %q", rr.Body.String())
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM schedule_events WHERE event_type_id = ?`, typeID).Scan(&n)
	if n != 0 {
		t.Error("the refused event was inserted anyway")
	}

	// Flip the flag on and the same request is fine — and the level is stored.
	if rr := putEventType(t, typeID, map[string]any{"name": "Scratch Encounter", "short_name": "SCR", "has_level": true}); rr.Code != http.StatusNoContent {
		t.Fatalf("enabling has_level failed with %d: %s", rr.Code, rr.Body.String())
	}
	rr = postEvent(t, map[string]any{
		"event_date": "2026-10-05", "event_type_id": typeID, "event_time": "12:00", "level": 12,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("level on a levelled custom type rejected with %d: %s", rr.Code, rr.Body.String())
	}
	var stored *int
	db.QueryRow(`SELECT level FROM schedule_events WHERE event_type_id = ?`, typeID).Scan(&stored)
	if stored == nil || *stored != 12 {
		t.Errorf("stored level = %v, want 12", stored)
	}
}

// Retyping an event to a type that carries no level DROPS the level rather than
// carrying it along. The update path's half of the same leak.
func TestRetypingToAnUnlevelledTypeClearsTheLevel(t *testing.T) {
	setupSettingsTestDB(t)
	mg := mgTypeID(t)
	custom := newCustomType(t, "Scratch Encounter", "SCR", false)

	rr := postEvent(t, map[string]any{"event_date": "2026-10-05", "event_type_id": mg, "event_time": "12:00", "level": 1})
	if rr.Code != http.StatusCreated {
		t.Fatalf("seed MG event: %d %s", rr.Code, rr.Body.String())
	}
	var eventID int
	db.QueryRow(`SELECT id FROM schedule_events WHERE event_type_id = ?`, mg).Scan(&eventID)

	// The client sends no level (the modal cleared the box), only the new type.
	if rr := putEvent(t, eventID, map[string]any{
		"event_date": "2026-10-05", "event_type_id": custom, "event_time": "12:00",
	}); rr.Code != http.StatusNoContent {
		t.Fatalf("retype rejected with %d: %s", rr.Code, rr.Body.String())
	}
	var stored *int
	db.QueryRow(`SELECT level FROM schedule_events WHERE id = ?`, eventID).Scan(&stored)
	if stored != nil {
		t.Errorf("level = %v after retyping to an unlevelled type, want NULL", *stored)
	}
}

// A custom type has no baseline: a blank level stays blank rather than being
// filled in with a number nobody chose.
func TestCustomTypeBlankLevelStaysBlank(t *testing.T) {
	setupSettingsTestDB(t)
	typeID := newCustomType(t, "Scratch Encounter", "SCR", true)

	if rr := postEvent(t, map[string]any{
		"event_date": "2026-10-05", "event_type_id": typeID, "event_time": "12:00",
	}); rr.Code != http.StatusCreated {
		t.Fatalf("blank level rejected with %d: %s", rr.Code, rr.Body.String())
	}
	var stored *int
	db.QueryRow(`SELECT level FROM schedule_events WHERE event_type_id = ?`, typeID).Scan(&stored)
	if stored != nil {
		t.Errorf("stored level = %d, want NULL — a custom type has no baseline to substitute", *stored)
	}

	// The only rule that applies is the floor.
	if rr := postEvent(t, map[string]any{
		"event_date": "2026-10-06", "event_type_id": typeID, "event_time": "12:00", "level": 0,
	}); rr.Code != http.StatusBadRequest {
		t.Errorf("level 0 accepted with %d, want 400", rr.Code)
	}
	// ...and there is no ceiling: nothing in the app knows what a custom scale runs to.
	if rr := postEvent(t, map[string]any{
		"event_date": "2026-10-07", "event_type_id": typeID, "event_time": "12:00", "level": 5000,
	}); rr.Code != http.StatusCreated {
		t.Errorf("a high level on a custom type was rejected with %d — custom types have no ceiling", rr.Code)
	}
}

// The default-branch hazard: before this, a system type with no configuration
// silently took Marshal's Guard's numbers. Now it is an error, and nothing is
// written.
func TestUnknownSystemTypeIsAnErrorNotMGsNumbers(t *testing.T) {
	setupSettingsTestDB(t)
	res, err := db.Exec(`INSERT INTO schedule_event_types (name, short_name, icon, is_system, active, sort_order, has_level)
		VALUES ('Probe Event','PRB','🛰️',1,1,50,1)`)
	if err != nil {
		t.Fatalf("seed system type: %v", err)
	}
	id64, _ := res.LastInsertId()
	typeID := int(id64)

	rr := postEvent(t, map[string]any{"event_date": "2026-10-05", "event_type_id": typeID, "event_time": "12:00"})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("a system type with NULL levels returned %d, want 500 (body %s)", rr.Code, rr.Body.String())
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM schedule_events WHERE event_type_id = ?`, typeID).Scan(&n)
	if n != 0 {
		t.Errorf("%d rows were inserted despite the error", n)
	}

	// Give it its own numbers and it works, on its own scale — not MG's.
	if _, err := db.Exec(`UPDATE schedule_event_types SET baseline_level=6, max_level=6 WHERE id=?`, typeID); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if rr := postEvent(t, map[string]any{"event_date": "2026-10-05", "event_type_id": typeID, "event_time": "12:00"}); rr.Code != http.StatusCreated {
		t.Fatalf("configured system type rejected with %d: %s", rr.Code, rr.Body.String())
	}
	var stored *int
	db.QueryRow(`SELECT level FROM schedule_events WHERE event_type_id = ?`, typeID).Scan(&stored)
	if stored == nil || *stored != 6 {
		t.Errorf("substituted level = %v, want its own baseline of 6", stored)
	}
}

// The create handler must accept every field the modal sends, or a new custom
// type loses its checkbox until somebody edits it — which reads as the checkbox
// not working.
func TestCreateTypeAcceptsHasLevel(t *testing.T) {
	setupSettingsTestDB(t)

	body, _ := json.Marshal(map[string]any{
		"name": "Scratch Encounter", "short_name": "SCR", "has_level": true,
	})
	req := scheduleTestActor(httptest.NewRequest(http.MethodPost, "/api/schedule/event-types", strings.NewReader(string(body))))
	rr := httptest.NewRecorder()
	createScheduleEventType(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", rr.Code, rr.Body.String())
	}
	var out struct{ ID int }
	json.Unmarshal(rr.Body.Bytes(), &out)

	// No intermediate edit: an event with a level is accepted straight away.
	if rr := postEvent(t, map[string]any{
		"event_date": "2026-10-05", "event_type_id": out.ID, "event_time": "12:00", "level": 3,
	}); rr.Code != http.StatusCreated {
		t.Fatalf("level on the newly created type rejected with %d: %s — has_level was lost on create", rr.Code, rr.Body.String())
	}
}

func TestBaselineCannotExceedCeiling(t *testing.T) {
	setupSettingsTestDB(t)
	mg := mgTypeID(t)
	if rr := putCeiling(t, mg, 12); rr.Code != http.StatusNoContent {
		t.Fatalf("setting the ceiling failed with %d: %s", rr.Code, rr.Body.String())
	}

	// Baseline above the ceiling, through the type PUT.
	rr := putEventType(t, mg, map[string]any{"baseline_level": 13})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("baseline above the ceiling accepted with %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "12") {
		t.Errorf("message should name the ceiling in the way; got %q", rr.Body.String())
	}
	if rr := putEventType(t, mg, map[string]any{"baseline_level": 12}); rr.Code != http.StatusNoContent {
		t.Errorf("baseline equal to the ceiling rejected with %d: %s", rr.Code, rr.Body.String())
	}

	// Ceiling below the baseline, through the ceiling PUT.
	rr = putCeiling(t, mg, 11)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("ceiling below the baseline accepted with %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "baseline") {
		t.Errorf("message should name the baseline; got %q", rr.Body.String())
	}

	// And the sanity bound still applies.
	if rr := putCeiling(t, mg, maxEventLevelCeiling+1); rr.Code != http.StatusBadRequest {
		t.Errorf("ceiling above the sanity bound accepted with %d", rr.Code)
	}
}

// setRankPermissions rewrites one rank's permission JSON to exactly the keys
// given, so a case can express "holds this and not that".
func setRankPermissions(t *testing.T, rank string, perms map[string]int) {
	t.Helper()
	b, _ := json.Marshal(perms)
	if _, err := db.Exec(`UPDATE rank_permissions SET permissions = ? WHERE rank = ?`, string(b), rank); err != nil {
		t.Fatalf("set %s permissions: %v", rank, err)
	}
}

func rankActor(rank string) *http.Request {
	memberID := 1
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	return req.WithContext(context.WithValue(req.Context(), authUserKey,
		&AuthUser{ID: 2, Username: "ranked", Rank: rank, MemberID: &memberID}))
}

// The ceiling endpoints are manage_settings while the type PUT is manage_schedule.
// The permissions are INDEPENDENT — manage_settings defaults to no rank at all —
// which is why Game Limits reads its own endpoint rather than the general types
// list: requirePermission takes one key, so a manage_settings holder without
// view_schedule would otherwise get a 403 and an empty section with no
// explanation.
func TestCeilingEndpointsAreGatedManageSettings(t *testing.T) {
	setupSettingsTestDB(t)

	// R4: manages the schedule, cannot touch settings.
	setRankPermissions(t, "R4", map[string]int{"view_schedule": 1, "manage_schedule": 1})
	// R3: manages settings, cannot even VIEW the schedule.
	setRankPermissions(t, "R3", map[string]int{"manage_settings": 1})

	mg := mgTypeID(t)
	ceilingsGET := requirePermission("manage_settings", getScheduleEventTypeCeilings)
	ceilingPUT := requirePermission("manage_settings", updateScheduleEventTypeCeiling)
	typesGET := requirePermission("view_schedule", getScheduleEventTypes)

	// The schedule manager is refused BOTH ceiling endpoints.
	rr := httptest.NewRecorder()
	ceilingsGET(rr, rankActor("R4"))
	if rr.Code != http.StatusForbidden {
		t.Errorf("manage_schedule-only user got %d on the ceilings GET, want 403", rr.Code)
	}
	rr = httptest.NewRecorder()
	req := rankActor("R4")
	req = mux.SetURLVars(req, map[string]string{"id": strconv.Itoa(mg)})
	ceilingPUT(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("manage_schedule-only user got %d on the ceiling PUT, want 403 — the ceiling changed hands", rr.Code)
	}

	// The settings holder gets the ceilings GET even with no view_schedule...
	rr = httptest.NewRecorder()
	ceilingsGET(rr, rankActor("R3"))
	if rr.Code != http.StatusOK {
		t.Fatalf("manage_settings-only user got %d on the ceilings GET, want 200 — Game Limits would render empty", rr.Code)
	}
	// ...which is exactly why it cannot be the general types endpoint.
	rr2 := httptest.NewRecorder()
	typesGET(rr2, rankActor("R3"))
	if rr2.Code != http.StatusForbidden {
		t.Errorf("the general types GET returned %d for a manage_settings-only user; the test's premise no longer holds", rr2.Code)
	}

	// The handler-level contract: the ceilings GET returns only levelled SYSTEM
	// types, which is all Game Limits needs and all it is allowed to show.
	rr = httptest.NewRecorder()
	getScheduleEventTypeCeilings(rr, scheduleTestActor(httptest.NewRequest(http.MethodGet, "/api/schedule/event-types/ceilings", nil)))
	if rr.Code != http.StatusOK {
		t.Fatalf("ceilings GET returned %d: %s", rr.Code, rr.Body.String())
	}
	var out []ScheduleEventTypeCeiling
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("no levelled system types returned")
	}
	for _, c := range out {
		var isSystem, hasLevel int
		db.QueryRow(`SELECT is_system, has_level FROM schedule_event_types WHERE id = ?`, c.ID).Scan(&isSystem, &hasLevel)
		if isSystem != 1 || hasLevel != 1 {
			t.Errorf("%s is in the ceilings list but is_system=%d has_level=%d", c.Name, isSystem, hasLevel)
		}
	}

	// A custom type has no ceiling to set.
	custom := newCustomType(t, "Scratch Encounter", "SCR", true)
	if rr := putCeiling(t, custom, 5); rr.Code != http.StatusBadRequest {
		t.Errorf("ceiling PUT on a custom type returned %d, want 400", rr.Code)
	}
}

// Turning the flag off on a type whose events already carry levels would strand
// those values — visible to nothing, editable by nothing.
func TestDisablingHasLevelWithLevelledEventsIsRefused(t *testing.T) {
	setupSettingsTestDB(t)
	typeID := newCustomType(t, "Scratch Encounter", "SCR", true)
	if rr := postEvent(t, map[string]any{
		"event_date": "2026-10-05", "event_type_id": typeID, "event_time": "12:00", "level": 4,
	}); rr.Code != http.StatusCreated {
		t.Fatalf("seed event: %d %s", rr.Code, rr.Body.String())
	}

	rr := putEventType(t, typeID, map[string]any{"name": "Scratch Encounter", "short_name": "SCR", "has_level": false})
	if rr.Code != http.StatusConflict {
		t.Fatalf("disabling has_level over levelled events returned %d, want 409", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "1 ") {
		t.Errorf("message should name the count; got %q", rr.Body.String())
	}
}

// An event stored above a ceiling the operator has since LOWERED must stay
// editable: the level input sits inside event-form, so rejecting an untouched
// legacy value would block edits to that event's notes and time as well.
func TestUpdateScheduleEventGrandfathersAnUnchangedLevel(t *testing.T) {
	setupSettingsTestDB(t)
	typeID := mgTypeID(t)
	if rr := putCeiling(t, typeID, 30); rr.Code != http.StatusNoContent {
		t.Fatalf("raise ceiling: %d %s", rr.Code, rr.Body.String())
	}
	if rr := putEventType(t, typeID, map[string]any{"baseline_level": 12}); rr.Code != http.StatusNoContent {
		t.Fatalf("set baseline: %d %s", rr.Code, rr.Body.String())
	}

	res, err := db.Exec(`INSERT INTO schedule_events
		(event_date, event_type_id, event_time, all_day, level, notes, created_by, created_at, updated_at)
		VALUES ('2026-09-10', ?, '00:30', 0, 25, 'original', 1, datetime('now'), datetime('now'))`, typeID)
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}
	id64, _ := res.LastInsertId()
	eventID := int(id64)

	// The operator lowers the ceiling below the stored level.
	if rr := putCeiling(t, typeID, 20); rr.Code != http.StatusNoContent {
		t.Fatalf("lower ceiling: %d %s", rr.Code, rr.Body.String())
	}

	base := func() map[string]any {
		return map[string]any{
			"event_date": "2026-09-10", "event_type_id": typeID,
			"event_time": "00:30", "all_day": false, "notes": "original",
		}
	}

	edit := base()
	edit["level"] = 25
	edit["notes"] = "edited note"
	if rr := putEvent(t, eventID, edit); rr.Code != http.StatusNoContent {
		t.Fatalf("unchanged out-of-range level blocked an unrelated edit: %d %s", rr.Code, rr.Body.String())
	}
	var gotNotes string
	db.QueryRow(`SELECT COALESCE(notes,'') FROM schedule_events WHERE id=?`, eventID).Scan(&gotNotes)
	if gotNotes != "edited note" {
		t.Errorf("notes = %q, want %q", gotNotes, "edited note")
	}

	// Omitting the level entirely takes the same path.
	if rr := putEvent(t, eventID, base()); rr.Code != http.StatusNoContent {
		t.Errorf("omitted level rejected with %d: %s", rr.Code, rr.Body.String())
	}

	// A newly typed out-of-range level is still rejected, naming the type and ceiling.
	bad := base()
	bad["level"] = 26
	rr := putEvent(t, eventID, bad)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("a newly typed out-of-range level was accepted with %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "between 1 and 20") {
		t.Errorf("message should name the ceiling; got %q", rr.Body.String())
	}

	ok := base()
	ok["level"] = 18
	if rr := putEvent(t, eventID, ok); rr.Code != http.StatusNoContent {
		t.Errorf("in-range level rejected with %d: %s", rr.Code, rr.Body.String())
	}
}

// The grandfathering is per TYPE. Carrying a level onto a different type with a
// lower ceiling is a new value for that type, not a legacy one.
func TestRetypingRevalidatesTheLevelAgainstTheNewCeiling(t *testing.T) {
	setupSettingsTestDB(t)
	mg, zs := mgTypeID(t), typeIDByShort(t, "ZS")
	if rr := putCeiling(t, mg, 70); rr.Code != http.StatusNoContent {
		t.Fatalf("raise MG ceiling: %s", rr.Body.String())
	}
	if rr := putCeiling(t, zs, 12); rr.Code != http.StatusNoContent {
		t.Fatalf("set ZS ceiling: %s", rr.Body.String())
	}

	res, _ := db.Exec(`INSERT INTO schedule_events
		(event_date, event_type_id, event_time, all_day, level, notes, created_by, created_at, updated_at)
		VALUES ('2026-09-10', ?, '00:30', 0, 70, '', 1, datetime('now'), datetime('now'))`, mg)
	id64, _ := res.LastInsertId()

	rr := putEvent(t, int(id64), map[string]any{
		"event_date": "2026-09-10", "event_type_id": zs, "event_time": "00:30", "level": 70,
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("level 70 carried onto a ZS capped at 12 was accepted with %d", rr.Code)
	}
}

// Nothing may push a template level onto a type that does not carry one — the
// push is a write path into schedule_events like any other.
func TestPushSkipsLevelOnUnlevelledType(t *testing.T) {
	setupSettingsTestDB(t)
	typeID := newCustomType(t, "Scratch Encounter", "SCR", false)

	// The push's decision is this predicate, read from the type row exactly as the
	// create handler reads it. Asserting it here rather than standing up a whole
	// season keeps the test on the rule; the handler path is covered above.
	tl, err := loadTypeLevels(db, typeID)
	if err != nil {
		t.Fatalf("loadTypeLevels: %v", err)
	}
	if tl.HasLevel {
		t.Fatal("setup: the type should not carry a level")
	}

	// ...and the same request through the handler is refused, which is what the
	// push's skip-and-count now mirrors.
	if rr := postEvent(t, map[string]any{
		"event_date": "2026-10-05", "event_type_id": typeID, "event_time": "12:00", "level": 5,
	}); rr.Code != http.StatusBadRequest {
		t.Errorf("a level on an unlevelled type was accepted with %d", rr.Code)
	}
}

// getSettings' SELECT and Scan lists are positional. Removing four fields from
// the middle of them is exactly the change that shifts everything after it, so
// the neighbours the level fields used to sit between are asserted by value.
func TestScheduleDefaultsSurviveTheWriteReadRoundTrip(t *testing.T) {
	setupSettingsTestDB(t)

	s := baseSettings()
	s["mg_default_time"] = "01:45"
	s["zs_default_time"] = "22:15"
	s["mg_anchor_date"] = "2026-09-07"
	s["zs_weekdays"] = "2,5"
	if rr := putSettings(t, s); rr.Code != http.StatusOK {
		t.Fatalf("save rejected with %d: %s", rr.Code, rr.Body.String())
	}

	rr := httptest.NewRecorder()
	getSettings(rr, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("getSettings returned %d: %s", rr.Code, rr.Body.String())
	}
	var out Settings
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.MGDefaultTime != "01:45" || out.ZSDefaultTime != "22:15" {
		t.Errorf("default times came back as %q/%q, want 01:45/22:15 — positional shift in the SELECT or UPDATE",
			out.MGDefaultTime, out.ZSDefaultTime)
	}
	if out.MGAnchorDate != "2026-09-07" || out.ZSWeekdays != "2,5" {
		t.Errorf("neighbouring columns came back as %q/%q, want 2026-09-07 and 2,5",
			out.MGAnchorDate, out.ZSWeekdays)
	}
}

// The migration must leave every install able to save what it already has: no
// stored event may exceed its own type's seeded ceiling.
func TestMigrationSeedsCeilingsAboveEverythingAlreadyScheduled(t *testing.T) {
	setupSettingsTestDB(t)

	typeID := mgTypeID(t)
	for i, lvl := range []int{8, 10, 12} {
		if _, err := db.Exec(`INSERT INTO schedule_events
			(event_date, event_type_id, event_time, all_day, level, notes, created_by, created_at, updated_at)
			VALUES (?, ?, '00:30', 0, ?, '', 1, datetime('now'), datetime('now'))`,
			fmt.Sprintf("2026-09-%02d", i+10), typeID, lvl); err != nil {
			t.Fatalf("seed event: %v", err)
		}
	}

	// Re-run the migration's seed expression over this data.
	if _, err := db.Exec(`UPDATE schedule_event_types
		SET max_level = max(COALESCE(max_level,1), COALESCE(baseline_level,1),
		    COALESCE((SELECT max(e.level) FROM schedule_events e WHERE e.event_type_id = schedule_event_types.id), 1))
		WHERE has_level = 1`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, _, max := typeLevelsOf(t, typeID)
	if max == nil || *max < 12 {
		t.Errorf("seeded ceiling %v is below the highest scheduled level 12 — existing events would be uneditable", max)
	}
}
