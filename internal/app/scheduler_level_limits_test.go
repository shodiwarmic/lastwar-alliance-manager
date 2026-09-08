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

// levelSettings builds a settings payload carrying the four level fields plus the
// unrelated ones updateSettings validates, so a case fails on the level rules
// rather than on something incidental.
func levelSettings(mgBase, zsBase, maxMG, maxZS int) map[string]any {
	s := baseSettings()
	s["mg_baseline"] = mgBase
	s["zs_baseline"] = zsBase
	s["max_mg_level"] = maxMG
	s["max_zs_level"] = maxZS
	// The columns immediately after the new pair in the SELECT and UPDATE lists.
	// Carried so the round-trip case can detect a positional shift into them.
	s["mg_default_time"] = "00:30"
	s["zs_default_time"] = "23:00"
	return s
}

func readLevelSettings(t *testing.T) Settings {
	t.Helper()
	rr := httptest.NewRecorder()
	getSettings(rr, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("getSettings returned %d: %s", rr.Code, rr.Body.String())
	}
	var out Settings
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// A baseline may not exceed its own type's ceiling. The two are edited on different
// pages -- Schedule -> Settings and Settings -> Game Limits -- so nothing in the UI
// stops one being walked past the other.
func TestUpdateSettingsRejectsBaselineAboveItsCeiling(t *testing.T) {
	setupSettingsTestDB(t)

	if rr := putSettings(t, levelSettings(12, 11, 12, 11)); rr.Code != http.StatusOK {
		t.Fatalf("baseline equal to the ceiling rejected with %d: %s", rr.Code, rr.Body.String())
	}

	rr := putSettings(t, levelSettings(13, 11, 12, 11))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("MG baseline above its ceiling accepted with %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "MG baseline") {
		t.Errorf("message should name the field the operator must fix; got %q", rr.Body.String())
	}

	if rr := putSettings(t, levelSettings(12, 12, 12, 11)); rr.Code != http.StatusBadRequest {
		t.Errorf("ZS baseline above its ceiling accepted with %d", rr.Code)
	}
	if rr := putSettings(t, levelSettings(0-1, 11, 12, 11)); rr.Code != http.StatusBadRequest {
		t.Errorf("negative MG baseline accepted with %d", rr.Code)
	}
	if rr := putSettings(t, levelSettings(12, 11, maxEventLevelCeiling+1, 11)); rr.Code != http.StatusBadRequest {
		t.Errorf("ceiling above the sanity bound accepted with %d", rr.Code)
	}
}

// Settings uses primitive ints, so an omitted field unmarshals to 0. The handler
// writes every column raw, so without the merge a payload that touches only the
// ceiling would silently store a baseline of 0 -- data loss, not a validation error.
func TestUpdateSettingsPartialPayloadDoesNotZeroLevelFields(t *testing.T) {
	setupSettingsTestDB(t)

	if rr := putSettings(t, levelSettings(12, 11, 20, 20)); rr.Code != http.StatusOK {
		t.Fatalf("setup save rejected with %d: %s", rr.Code, rr.Body.String())
	}

	// Only the MG ceiling. Everything else is absent, i.e. zero.
	partial := baseSettings()
	partial["max_mg_level"] = 30
	if rr := putSettings(t, partial); rr.Code != http.StatusOK {
		t.Fatalf("ceiling-only payload rejected with %d: %s", rr.Code, rr.Body.String())
	}

	got := readLevelSettings(t)
	if got.MaxMGLevel != 30 {
		t.Errorf("max_mg_level = %d, want 30 — the supplied field did not land", got.MaxMGLevel)
	}
	if got.MGBaseline != 12 || got.ZSBaseline != 11 || got.MaxZSLevel != 20 {
		t.Errorf("omitted fields were zeroed: mg_baseline=%d zs_baseline=%d max_zs_level=%d, want 12/11/20",
			got.MGBaseline, got.ZSBaseline, got.MaxZSLevel)
	}
}

// getSettings has TestGetSettingsScansEveryColumn guarding SELECT/Scan lockstep.
// Nothing guarded UPDATE/args, which is the half that corrupts data silently: a
// column added to one list and not the other shifts every later field.
func TestLevelLimitsSurviveTheWriteReadRoundTrip(t *testing.T) {
	setupSettingsTestDB(t)

	if rr := putSettings(t, levelSettings(9, 8, 70, 60)); rr.Code != http.StatusOK {
		t.Fatalf("save rejected with %d: %s", rr.Code, rr.Body.String())
	}
	got := readLevelSettings(t)
	if got.MGBaseline != 9 || got.ZSBaseline != 8 || got.MaxMGLevel != 70 || got.MaxZSLevel != 60 {
		t.Errorf("round trip returned mg=%d zs=%d maxMG=%d maxZS=%d, want 9/8/70/60 — UPDATE and its args are likely out of lockstep",
			got.MGBaseline, got.ZSBaseline, got.MaxMGLevel, got.MaxZSLevel)
	}
	// Neighbouring columns must be untouched by the insertion.
	if got.MGDefaultTime != "00:30" || got.ZSDefaultTime != "23:00" {
		t.Errorf("adjacent columns came back as mg=%q zs=%q, want 00:30/23:00 — positional shift in the SELECT or UPDATE",
			got.MGDefaultTime, got.ZSDefaultTime)
	}
}

func mgTypeID(t *testing.T) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name='MG'`).Scan(&id); err != nil {
		t.Fatalf("MG event type missing: %v", err)
	}
	return id
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

// An event stored above a ceiling the operator has since LOWERED must stay editable.
// The level input lives inside event-form, so rejecting an untouched legacy value
// would block edits to that event's notes and time as well, over a value nobody chose
// in that request. Typing a new out-of-range level is still rejected.
func TestUpdateScheduleEventGrandfathersAnUnchangedLevel(t *testing.T) {
	setupSettingsTestDB(t)

	if rr := putSettings(t, levelSettings(12, 11, 30, 30)); rr.Code != http.StatusOK {
		t.Fatalf("setup save rejected with %d: %s", rr.Code, rr.Body.String())
	}
	typeID := mgTypeID(t)
	res, err := db.Exec(`INSERT INTO schedule_events
		(event_date, event_type_id, event_time, all_day, level, notes, created_by, created_at, updated_at)
		VALUES ('2026-09-10', ?, '00:30', 0, 25, 'original', 1, datetime('now'), datetime('now'))`, typeID)
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}
	eventID64, _ := res.LastInsertId()
	eventID := int(eventID64)

	// The operator lowers the ceiling below the stored level.
	if rr := putSettings(t, levelSettings(12, 11, 20, 20)); rr.Code != http.StatusOK {
		t.Fatalf("lowering the ceiling rejected with %d: %s", rr.Code, rr.Body.String())
	}

	base := func() map[string]any {
		return map[string]any{
			"event_date": "2026-09-10", "event_type_id": typeID,
			"event_time": "00:30", "all_day": false, "notes": "original",
		}
	}

	// Editing only the notes, carrying the stored level back unchanged, must succeed.
	edit := base()
	edit["level"] = 25
	edit["notes"] = "edited note"
	if rr := putEvent(t, eventID, edit); rr.Code != http.StatusNoContent {
		t.Fatalf("unchanged out-of-range level blocked an unrelated edit: %d %s", rr.Code, rr.Body.String())
	}
	var gotNotes string
	if err := db.QueryRow(`SELECT COALESCE(notes,'') FROM schedule_events WHERE id=?`, eventID).Scan(&gotNotes); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if gotNotes != "edited note" {
		t.Errorf("notes = %q, want %q", gotNotes, "edited note")
	}

	// Omitting the level entirely takes the same path (the handler substitutes old).
	if rr := putEvent(t, eventID, base()); rr.Code != http.StatusNoContent {
		t.Errorf("omitted level rejected with %d: %s", rr.Code, rr.Body.String())
	}

	// Changing it to another out-of-range value is still rejected.
	bad := base()
	bad["level"] = 26
	rr := putEvent(t, eventID, bad)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("a newly typed out-of-range level was accepted with %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "MG level must be between 1 and 20") {
		t.Errorf("message should name the type and ceiling; got %q", rr.Body.String())
	}

	// Bringing it back inside the ceiling is accepted.
	ok := base()
	ok["level"] = 18
	if rr := putEvent(t, eventID, ok); rr.Code != http.StatusNoContent {
		t.Errorf("in-range level rejected with %d: %s", rr.Code, rr.Body.String())
	}
}

// The seed must leave every install able to save what it already has: no stored
// event and no baseline may exceed its seeded ceiling.
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
	if _, err := db.Exec(`UPDATE settings SET max_mg_level = max(1, COALESCE(mg_baseline, 1),
		COALESCE((SELECT max(e.level) FROM schedule_events e
		          JOIN schedule_event_types t ON t.id = e.event_type_id
		          WHERE t.short_name = 'MG'), 0)) WHERE id = 1`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var ceiling, highest int
	db.QueryRow(`SELECT max_mg_level FROM settings WHERE id=1`).Scan(&ceiling)
	db.QueryRow(`SELECT max(level) FROM schedule_events`).Scan(&highest)
	if ceiling < highest {
		t.Errorf("seeded ceiling %d is below the highest scheduled level %d — existing events would be uneditable", ceiling, highest)
	}
}
