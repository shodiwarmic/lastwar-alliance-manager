package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// A server event is a WINDOW; the things that happen inside it are encounters.
// Sky Predator happens inside General's Trial, Glacieradon inside Zombie Invasion.
//
// Verified against the live database (2026-09-15): all ten stored encounter rows
// sit on DAY 1 of their parent's occurrence, so no stored row exercises a day-2 or
// day-3 date. These tests do, because that is exactly where a wrong window
// calculation would hide.

func serverEventIDByName(t *testing.T, name string) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`SELECT id FROM server_events WHERE name = ?`, name).Scan(&id); err != nil {
		t.Fatalf("server event %q missing: %v", name, err)
	}
	return id
}

// anchorServerEvent gives a seeded window a start date. Migration 025 seeds the
// five repeating windows with NO anchor_date — the operator fills each one in when
// they first see that event in the game — so a test that wants a computable window
// has to say when it opens, exactly as an operator would.
func anchorServerEvent(t *testing.T, name, anchor string) int {
	t.Helper()
	id := serverEventIDByName(t, name)
	if _, err := db.Exec(`UPDATE server_events SET anchor_date = ? WHERE id = ?`, anchor, id); err != nil {
		t.Fatalf("anchor %q: %v", name, err)
	}
	return id
}

// The Go occurrence arithmetic is a MIRROR of the browser's
// getServerEventOccurrencesInWeek. It is table-tested against dates the JS is known
// to produce, because there is no shared implementation to keep them honest.
func TestServerEventOccurrenceMirrorsTheClient(t *testing.T) {
	every14 := func(anchor string, duration int) ServerEvent {
		n := 14
		return ServerEvent{Active: true, AnchorDate: anchor, RepeatType: "every_n_days",
			RepeatInterval: &n, DurationDays: duration}
	}
	weekday := func(repeat, anchor string, dow, duration int) ServerEvent {
		return ServerEvent{Active: true, AnchorDate: anchor, RepeatType: repeat,
			RepeatWeekday: &dow, DurationDays: duration}
	}

	cases := []struct {
		name string
		ev   ServerEvent
		date string
		want bool
	}{
		// General's Trial as this install has it: every 14 days from 2026-04-08,
		// three days long. 2026-09-23 is an occurrence start.
		{"GT day 1", every14("2026-04-08", 3), "2026-09-23", true},
		{"GT day 2", every14("2026-04-08", 3), "2026-09-24", true},
		{"GT day 3", every14("2026-04-08", 3), "2026-09-25", true},
		{"GT day 4 is outside", every14("2026-04-08", 3), "2026-09-26", false},
		{"the day before is outside", every14("2026-04-08", 3), "2026-09-22", false},
		{"a date before the anchor", every14("2026-04-08", 3), "2026-04-07", false},
		{"the anchor itself", every14("2026-04-08", 3), "2026-04-08", true},

		{"one-off covers its anchor", ServerEvent{Active: true, AnchorDate: "2026-06-08",
			RepeatType: "none", DurationDays: 5}, "2026-06-12", true},
		{"one-off ends", ServerEvent{Active: true, AnchorDate: "2026-06-08",
			RepeatType: "none", DurationDays: 5}, "2026-06-13", false},

		// Anchor 2026-04-08 is a Wednesday (Mon=0 -> 2). Target Monday (0) is the
		// following 2026-04-13.
		{"weekly lands on its weekday", weekday("weekly", "2026-04-08", 0, 1), "2026-04-13", true},
		{"weekly skips other days", weekday("weekly", "2026-04-08", 0, 1), "2026-04-14", false},
		{"weekly recurs", weekday("weekly", "2026-04-08", 0, 1), "2026-04-20", true},
		{"biweekly skips a week", weekday("biweekly", "2026-04-08", 0, 1), "2026-04-20", false},
		{"biweekly recurs", weekday("biweekly", "2026-04-08", 0, 1), "2026-04-27", true},

		{"an inactive window covers nothing", func() ServerEvent {
			ev := every14("2026-04-08", 3)
			ev.Active = false
			return ev
		}(), "2026-09-23", false},
	}
	for _, tc := range cases {
		if got := serverEventCoversDate(tc.ev, tc.date); got != tc.want {
			t.Errorf("%s: coversDate(%s) = %v, want %v", tc.name, tc.date, got, tc.want)
		}
	}
}

// getEventsInRange reads the calendar payload the browser reads, so a test asserts
// on what the page is actually handed rather than on the table underneath it.
func getEventsInRange(t *testing.T, from, to string) []ScheduleEvent {
	t.Helper()
	req := scheduleTestActor(httptest.NewRequest(http.MethodGet,
		"/api/schedule/events?from="+from+"&to="+to, nil))
	rr := httptest.NewRecorder()
	getScheduleEvents(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("getScheduleEvents returned %d: %s", rr.Code, rr.Body.String())
	}
	var events []ScheduleEvent
	if err := json.Unmarshal(rr.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return events
}

// The encounter must fall inside its parent's window — including on days 2 and 3,
// which no stored row exercises.
func TestEncounterMustFallInsideItsWindow(t *testing.T) {
	setupSettingsTestDB(t)
	sp := typeIDByShort(t, "SP")
	gt := anchorServerEvent(t, "General's Trial", "2026-04-08") // the live install's anchor

	var parentID *int
	db.QueryRow(`SELECT server_event_id FROM schedule_event_types WHERE id = ?`, sp).Scan(&parentID)
	if parentID == nil || *parentID != gt {
		t.Fatalf("migration 075 did not link Sky Predator to General's Trial (parent=%v)", parentID)
	}

	for _, date := range []string{"2026-09-23", "2026-09-24", "2026-09-25"} {
		if rr := postEvent(t, map[string]any{
			"event_date": date, "event_type_id": sp, "event_time": "23:00",
		}); rr.Code != http.StatusCreated {
			t.Errorf("%s (inside the window) rejected with %d: %s", date, rr.Code, rr.Body.String())
		}
	}

	rr := postEvent(t, map[string]any{
		"event_date": "2026-09-26", "event_type_id": sp, "event_time": "23:00",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("2026-09-26 (day 4) accepted with %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "General's Trial") || !strings.Contains(rr.Body.String(), "2026-09-23") {
		t.Errorf("rejection should name the parent and the nearest window; got %q", rr.Body.String())
	}
}

// An UNANCHORED parent skips the rule; it does not fail it.
//
// This is the fresh-install and the not-yet-configured case, and it is the one
// that decides whether this feature can ship at all. Migration 025 seeds all five
// windows with no anchor_date — the operator fills each one in when they first see
// that event in the game — while migration 075 links Sky Predator to General's
// Trial on EVERY install regardless. Enforce here and Sky Predator becomes
// unschedulable, on upgrade, for anyone who had not happened to set that anchor:
// a rule introduced by this change breaking an existing workflow over unrelated
// missing configuration.
//
// It could not even explain itself. Every other rejection in the schedule names
// the rule and the date it compared against; this one would have neither a window
// nor a date to name. A rule that cannot state what it wants must not enforce it.
//
// Deliberately asserted with NO call to anchorServerEvent — the seeded state is
// the fixture.
func TestUnanchoredParentDoesNotBlockItsEncounters(t *testing.T) {
	setupSettingsTestDB(t)
	sp := typeIDByShort(t, "SP")

	var anchor string
	db.QueryRow(`SELECT COALESCE(anchor_date,'') FROM server_events WHERE name = 'General''s Trial'`).Scan(&anchor)
	if anchor != "" {
		t.Fatalf("fixture drift: General's Trial arrived with anchor_date %q; this case needs the unanchored seed", anchor)
	}

	// Any date at all is acceptable, because the app cannot say which are not.
	for _, date := range []string{"2026-09-23", "2026-09-26", "2027-01-01"} {
		if rr := postEvent(t, map[string]any{
			"event_date": date, "event_type_id": sp, "event_time": "23:00",
		}); rr.Code != http.StatusCreated {
			t.Errorf("%s rejected with %d against an unanchored parent: %s", date, rr.Code, rr.Body.String())
		}
	}

	// And nothing is flagged on the calendar either — an unanchored window draws no
	// banner in the browser, so "unknown" has to read the same way on both sides.
	// Every READER has to agree with the write path, or the app badges as wrong the
	// event it just accepted.
	for _, ev := range getEventsInRange(t, "2026-09-20", "2026-09-30") {
		if ev.OutsideWindow {
			t.Errorf("%s on %s flagged outside a window that cannot be computed", ev.TypeShort, ev.EventDate)
		}
	}

	// The second reader: saving the parent must not report every future encounter
	// as stranded just because its anchor is still blank.
	var gt int
	db.QueryRow(`SELECT id FROM server_events WHERE name = 'General''s Trial'`).Scan(&gt)
	stranded, err := strandedEncounters(db, gt)
	if err != nil {
		t.Fatalf("strandedEncounters: %v", err)
	}
	if len(stranded) != 0 {
		t.Errorf("an unanchored window stranded %d encounters; it can strand none", len(stranded))
	}
}

// The window rule applies to a CUSTOM type with a parent too. It had to, on all
// three write paths: each used to gate the validator on is_system, so each was its
// own hole.
func TestEncounterRuleAppliesToCustomTypesWithAParent(t *testing.T) {
	setupSettingsTestDB(t)
	zi := anchorServerEvent(t, "Zombie Invasion", "2026-04-01") // the live install's anchor
	glac := newCustomType(t, "Glacieradon (ZI)", "ZI-G", false)
	if _, err := db.Exec(`UPDATE schedule_event_types SET server_event_id = ? WHERE id = ?`, zi, glac); err != nil {
		t.Fatalf("link: %v", err)
	}

	// The live case, boundary by boundary. Glacieradon returned to the game in
	// September 2026; the dev install's Zombie Invasion runs every 14 days from
	// 2026-04-01 for 3 days, which puts the window at 09-16 to 09-18 (the previous
	// one was 09-02 to 09-04). Both edges are asserted, not just the inside: an
	// off-by-one in the occurrence walk would pass a test that only ever checked a
	// date in the middle.
	for _, tc := range []struct {
		date string
		want int
	}{
		{"2026-09-15", http.StatusBadRequest}, // the day before it opens
		{"2026-09-16", http.StatusCreated},    // day 1
		{"2026-09-17", http.StatusCreated},    // day 2
		{"2026-09-18", http.StatusCreated},    // day 3, the last
		{"2026-09-19", http.StatusBadRequest}, // the day after it closes
	} {
		rr := postEvent(t, map[string]any{
			"event_date": tc.date, "event_type_id": glac, "event_time": "12:00",
		})
		if rr.Code != tc.want {
			t.Errorf("create %s: got %d, want %d (%s)", tc.date, rr.Code, tc.want, rr.Body.String())
		}
	}

	// CREATE path, well clear of the window.
	rr := postEvent(t, map[string]any{
		"event_date": "2026-09-20", "event_type_id": glac, "event_time": "12:00",
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("create: a custom encounter outside its window was accepted with %d", rr.Code)
	}
	if body := rr.Body.String(); !strings.Contains(body, "2026-09-16 to 2026-09-18") {
		t.Errorf("the rejection must name the nearest window, got %q", body)
	}

	// UPDATE path: move a legal one out of the window.
	var id int
	db.QueryRow(`SELECT id FROM schedule_events WHERE event_type_id = ?`, glac).Scan(&id)
	if rr := putEvent(t, id, map[string]any{
		"event_date": "2026-09-20", "event_type_id": glac, "event_time": "12:00",
	}); rr.Code != http.StatusBadRequest {
		t.Errorf("update: a custom encounter moved outside its window was accepted with %d", rr.Code)
	}

	// PUSH path: the same predicate the push now runs, via the shared validator.
	tr, err := loadScheduleTypeRules(db, glac)
	if err != nil {
		t.Fatalf("loadScheduleTypeRules: %v", err)
	}
	if msg, err := validateEventRules(db, tr, "2026-09-20", "12:00", 0); err != nil || msg == "" {
		t.Errorf("push: validateEventRules allowed a custom encounter outside its window (msg=%q err=%v)", msg, err)
	}
}

// Deleting a window an encounter uses is REFUSED. foreign_keys is off app-wide, so
// nothing else would clear the link.
func TestServerEventWithEncountersCannotBeDeleted(t *testing.T) {
	setupSettingsTestDB(t)
	gt := serverEventIDByName(t, "General's Trial")

	r := scheduleTestActor(httptest.NewRequest(http.MethodDelete, "/api/schedule/server-events/"+strconv.Itoa(gt), nil))
	r = mux.SetURLVars(r, map[string]string{"id": strconv.Itoa(gt)})
	rr := httptest.NewRecorder()
	deleteServerEvent(rr, r)
	if rr.Code != http.StatusConflict {
		t.Fatalf("deleting a window with an encounter returned %d, want 409", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Sky Predator") {
		t.Errorf("refusal should name the encounter; got %q", rr.Body.String())
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM server_events WHERE id = ?`, gt).Scan(&n)
	if n != 1 {
		t.Error("the window was deleted anyway")
	}
}

// detachEncounterParents is what the season-delete purge uses instead of refusing:
// the window is going whatever happens, so the link is cleared and counted.
func TestDetachEncounterParentsClearsAndCounts(t *testing.T) {
	setupSettingsTestDB(t)
	gt := serverEventIDByName(t, "General's Trial")
	sp := typeIDByShort(t, "SP")

	n, err := detachEncounterParents(db, []int{gt})
	if err != nil {
		t.Fatalf("detach: %v", err)
	}
	if n != 1 {
		t.Errorf("detached %d types, want 1", n)
	}
	var parent *int
	db.QueryRow(`SELECT server_event_id FROM schedule_event_types WHERE id = ?`, sp).Scan(&parent)
	if parent != nil {
		t.Errorf("Sky Predator still points at %d", *parent)
	}
	// And with nothing to detach it is a no-op, not an error.
	if n, err := detachEncounterParents(db, nil); err != nil || n != 0 {
		t.Errorf("empty detach returned %d, %v", n, err)
	}
}

// Moving a window strands the encounters that were legal where it used to be. The
// app reports them and NEVER moves them: the officer knows why they moved the
// window, and silently relocating somebody's schedule is the worse failure.
func TestParentChangeReportsStrandedEncounters(t *testing.T) {
	setupSettingsTestDB(t)
	sp := typeIDByShort(t, "SP")
	gt := anchorServerEvent(t, "General's Trial", "2026-04-08")

	if rr := postEvent(t, map[string]any{
		"event_date": "2026-09-23", "event_type_id": sp, "event_time": "23:00",
	}); rr.Code != http.StatusCreated {
		t.Fatalf("seed SP: %d %s", rr.Code, rr.Body.String())
	}

	// Move the anchor by one day; the whole ladder of occurrences moves with it.
	body, _ := json.Marshal(map[string]any{
		"name": "General's Trial", "short_name": "GT", "icon": "⚔️", "duration_days": 3,
		"repeat_type": "every_n_days", "repeat_interval": 14, "anchor_date": "2026-04-09",
	})
	r := scheduleTestActor(httptest.NewRequest(http.MethodPut, "/api/schedule/server-events/"+strconv.Itoa(gt), strings.NewReader(string(body))))
	r = mux.SetURLVars(r, map[string]string{"id": strconv.Itoa(gt)})
	rr := httptest.NewRecorder()
	updateServerEvent(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("update returned %d: %s", rr.Code, rr.Body.String())
	}

	var out struct {
		Stranded []strandedEncounter `json:"stranded"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Stranded) == 0 {
		t.Fatal("moving the window stranded an encounter and the response did not say so")
	}
	if out.Stranded[0].Date != "2026-09-23" || out.Stranded[0].Type != "Sky Predator" {
		t.Errorf("stranded[0] = %+v, want the 2026-09-23 Sky Predator", out.Stranded[0])
	}

	// It was reported, not moved.
	var date string
	db.QueryRow(`SELECT event_date FROM schedule_events WHERE event_type_id = ?`, sp).Scan(&date)
	if date != "2026-09-23" {
		t.Errorf("the event was relocated to %s — the app must never move somebody's schedule", date)
	}
}

// The calendar payload flags an encounter outside its window so the card can say
// so, rather than rendering a parent banner and an event that silently disagree.
func TestGetScheduleEventsFlagsOutsideWindow(t *testing.T) {
	setupSettingsTestDB(t)
	sp := typeIDByShort(t, "SP")
	anchorServerEvent(t, "General's Trial", "2026-04-08")

	// Inserted directly: the handler would refuse it, which is the point — this is
	// the state a moved window leaves behind.
	if _, err := db.Exec(`INSERT INTO schedule_events
		(event_date, event_type_id, event_time, all_day, notes, created_by, created_at, updated_at)
		VALUES ('2026-09-26', ?, '23:00', 0, '', 1, datetime('now'), datetime('now'))`, sp); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if rr := postEvent(t, map[string]any{
		"event_date": "2026-09-24", "event_type_id": sp, "event_time": "23:00",
	}); rr.Code != http.StatusCreated {
		t.Fatalf("seed in-window: %d %s", rr.Code, rr.Body.String())
	}

	events := getEventsInRange(t, "2026-09-21", "2026-09-27")
	flags := map[string]bool{}
	for _, ev := range events {
		flags[ev.EventDate] = ev.OutsideWindow
		if ev.OutsideWindow && ev.ParentName != "General's Trial" {
			t.Errorf("%s: parent_name = %q, want General's Trial", ev.EventDate, ev.ParentName)
		}
	}
	if !flags["2026-09-26"] {
		t.Error("2026-09-26 is outside the General's Trial window and was not flagged")
	}
	if flags["2026-09-24"] {
		t.Error("2026-09-24 is day 2 of the window and was flagged as outside it")
	}
}

// The migration promotes Sky Predator IN PLACE, so its recorded events keep their
// rows: retyping them onto a fresh type would lose the history the schedule is for.
func TestMigrationPromotesSkyPredatorInPlace(t *testing.T) {
	setupSettingsTestDB(t)

	// Simulate the pre-075 shape: the hand-made custom type with its events.
	if _, err := db.Exec(`UPDATE schedule_event_types SET name='Sky Predator (GT)', short_name='GT-SP',
		is_system=0, has_level=0, baseline_level=NULL, max_level=NULL, server_event_id=NULL
		WHERE short_name='SP'`); err != nil {
		t.Fatalf("un-promote: %v", err)
	}
	id := typeIDByShort(t, "GT-SP")
	if _, err := db.Exec(`INSERT INTO schedule_events
		(event_date, event_type_id, event_time, all_day, level, notes, created_by, created_at, updated_at)
		VALUES ('2026-09-09', ?, '23:00', 0, NULL, '', 1, datetime('now'), datetime('now'))`, id); err != nil {
		t.Fatalf("seed event: %v", err)
	}

	runMigrationBody(t, "migrations/075_encounters.sql")

	if got := typeIDByShort(t, "SP"); got != id {
		t.Errorf("Sky Predator is now type %d, was %d — it was recreated rather than promoted", got, id)
	}
	var name string
	var isSystem, hasLevel int
	var parent *int
	db.QueryRow(`SELECT name, is_system, has_level, server_event_id FROM schedule_event_types WHERE id = ?`, id).
		Scan(&name, &isSystem, &hasLevel, &parent)
	if name != "Sky Predator" || isSystem != 1 || hasLevel != 1 {
		t.Errorf("promoted row: name=%q is_system=%d has_level=%d", name, isSystem, hasLevel)
	}
	if parent == nil || *parent != serverEventIDByName(t, "General's Trial") {
		t.Errorf("parent = %v, want General's Trial", parent)
	}

	// The existing event kept its row AND its NULL level: the seed cannot know this
	// alliance's real Sky Predator level, and must not invent one.
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM schedule_events WHERE event_type_id = ? AND level IS NULL`, id).Scan(&n)
	if n != 1 {
		t.Errorf("%d unlevelled events survived, want 1", n)
	}
	_, base, max := typeLevelsOf(t, id)
	if base == nil || max == nil || *base != 1 || *max != 1 {
		t.Errorf("seeded levels = %v/%v, want 1/1 — the seed cannot know the real value", base, max)
	}
}

// A fresh install has no hand-made type to promote, so the system row is seeded.
func TestMigrationSeedsSkyPredatorOnFreshInstall(t *testing.T) {
	setupSettingsTestDB(t)
	// The setup DB IS a fresh install, so 075's INSERT branch has already run.
	id := typeIDByShort(t, "SP")
	var name, short string
	var isSystem int
	var parent *int
	db.QueryRow(`SELECT name, short_name, is_system, server_event_id FROM schedule_event_types WHERE id = ?`, id).
		Scan(&name, &short, &isSystem, &parent)
	if name != "Sky Predator" || short != "SP" || isSystem != 1 {
		t.Errorf("seeded row: name=%q short=%q is_system=%d", name, short, isSystem)
	}
	if parent == nil {
		t.Error("the seeded row has no parent window")
	}

	// And re-running the migration does not add a second one.
	runMigrationBody(t, "migrations/075_encounters.sql")
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM schedule_event_types WHERE short_name = 'SP'`).Scan(&n)
	if n != 1 {
		t.Errorf("%d Sky Predator rows after a re-run, want 1", n)
	}
}

// The game renamed Ironclad Vehicle to Rally Challenge.
func TestMigrationRenamesIroncladVehicle(t *testing.T) {
	setupSettingsTestDB(t)
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM server_events WHERE short_name = 'RC' AND name = 'Rally Challenge'`).Scan(&n)
	if n != 1 {
		t.Errorf("%d Rally Challenge rows, want 1", n)
	}
	db.QueryRow(`SELECT COUNT(*) FROM server_events WHERE name = 'Ironclad Vehicle'`).Scan(&n)
	if n != 0 {
		t.Errorf("%d Ironclad Vehicle rows remain", n)
	}
}
