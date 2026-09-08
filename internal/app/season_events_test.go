package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/pressly/goose/v3"
)

// The Season Hub push was reported as "nothing happens". The cause was not in
// the push at all: the Edit Season modal read the event-type list from a
// wrapper key the endpoint does not send, so its type dropdown was empty and
// every Save blanked the row's type. These tests pin the three halves of the
// fix — the response shape the client depends on, the server now deriving
// type_name from the id, and the migration that repairs rows already blanked.

func seasonEventActor(req *http.Request) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), authUserKey,
		&AuthUser{ID: 1, Username: "tester", IsAdmin: true}))
}

// TestScheduleEventTypesRespondsWithBareArray guards the contract from the
// server side. The JS half cannot be unit-tested here, so this is what stops a
// well-meaning wrap in {"event_types": …} silently re-breaking the modal.
func TestScheduleEventTypesRespondsWithBareArray(t *testing.T) {
	setupSettingsTestDB(t)

	rr := httptest.NewRecorder()
	getScheduleEventTypes(rr, httptest.NewRequest(http.MethodGet, "/api/schedule/event-types", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var types []ScheduleEventType
	if err := json.Unmarshal(rr.Body.Bytes(), &types); err != nil {
		t.Fatalf("response is not a bare JSON array: %v (body %s)", err, rr.Body.String())
	}
	if len(types) < 2 {
		t.Fatalf("got %d types, want the two seeded system types", len(types))
	}
}

// seedSeasonWithEvent creates a season carrying one MG-typed event and returns
// the season event's id and the MG type id.
func seedSeasonWithEvent(t *testing.T) (eventID, mgTypeID int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO seasons (id, name, season_number, start_date, week_count,
	      key_event_name, key_event_required, tier_active_min_pct, tier_at_risk_min_pct, is_active)
	      VALUES (1, 'Season IX', 9, '2026-09-07', 8, 'Rare Soil War', 4, 70, 60, 1)`); err != nil {
		t.Fatalf("seed season: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name = 'MG'`).Scan(&mgTypeID); err != nil {
		t.Fatalf("resolve MG type: %v", err)
	}
	res, err := db.Exec(`INSERT INTO season_events (season_id, label, event_type_id, type_name,
	      day_offset, event_time, week_start, week_end, notes) VALUES (1, 'Guard Duty', ?, 'Marshal''s Guard', 3, '20:00', 1, 1, '')`, mgTypeID)
	if err != nil {
		t.Fatalf("seed season event: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id), mgTypeID
}

func putSeasonEvent(t *testing.T, id int, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/season-hub/season-events/1", strings.NewReader(string(b)))
	req = mux.SetURLVars(seasonEventActor(req), map[string]string{"id": strconv.Itoa(id)})
	rr := httptest.NewRecorder()
	handleSeasonEventUpdate(rr, req)
	return rr
}

func TestSeasonEventUpdateDerivesTypeNameFromID(t *testing.T) {
	setupSettingsTestDB(t)
	eventID, mgTypeID := seedSeasonWithEvent(t)

	var zsTypeID int
	if err := db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name = 'ZS'`).Scan(&zsTypeID); err != nil {
		t.Fatalf("resolve ZS type: %v", err)
	}
	_ = mgTypeID

	// The client sends no type_name at all — the server must supply it.
	rr := putSeasonEvent(t, eventID, map[string]any{
		"label": "Guard Duty", "event_type_id": zsTypeID,
		"day_offset": 3, "event_time": "20:00", "week_start": 1, "week_end": 1,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rr.Code, rr.Body.String())
	}
	var gotName string
	var gotID int
	if err := db.QueryRow(`SELECT event_type_id, type_name FROM season_events WHERE id = ?`, eventID).Scan(&gotID, &gotName); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if gotID != zsTypeID || gotName != "Zombie Siege" {
		t.Fatalf("got (%d, %q), want (%d, %q)", gotID, gotName, zsTypeID, "Zombie Siege")
	}
}

func TestSeasonEventUpdatePreservesTypeNameWhenIDCleared(t *testing.T) {
	setupSettingsTestDB(t)
	eventID, _ := seedSeasonWithEvent(t)

	rr := putSeasonEvent(t, eventID, map[string]any{
		"label": "Guard Duty", "event_type_id": nil,
		"day_offset": 3, "event_time": "20:00", "week_start": 1, "week_end": 1,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rr.Code, rr.Body.String())
	}
	var gotName string
	if err := db.QueryRow(`SELECT type_name FROM season_events WHERE id = ?`, eventID).Scan(&gotName); err != nil {
		t.Fatalf("read back: %v", err)
	}
	// Clearing the id must not blank the name: Sync Event Types re-links on it.
	if gotName != "Marshal's Guard" {
		t.Fatalf("type_name = %q, want it preserved as %q", gotName, "Marshal's Guard")
	}
}

func TestSeasonEventUpdateRejectsUnknownEventType(t *testing.T) {
	setupSettingsTestDB(t)
	eventID, _ := seedSeasonWithEvent(t)

	rr := putSeasonEvent(t, eventID, map[string]any{
		"label": "Guard Duty", "event_type_id": 9999,
		"day_offset": 3, "event_time": "20:00", "week_start": 1, "week_end": 1,
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

// TestMigration070RepairsOrphanedSeasonEvents applies 070 over a database
// already holding blanked rows, the state every install that opened Edit Season
// is in.
func TestMigration070RepairsOrphanedSeasonEvents(t *testing.T) {
	conn := migrateTo(t, 69)

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(q, args...); err != nil {
			t.Fatalf("fixture exec %q: %v", q, err)
		}
	}

	exec(`INSERT INTO season_templates (id, template_name, season_number, trackables, defaults, events)
	      VALUES (900, 'Test Season', 900, '[]', '{}', '[
	        {"label":"Guard Duty","type_name":"Marshal''s Guard","day_offset":3,"event_time":"20:00","week_start":1,"week_end":1},
	        {"label":"Siege Night","type_name":"Zombie Siege","day_offset":5,"event_time":"23:00","week_start":1,"week_end":1}
	      ]')`)
	// An unparseable events blob on another template must not fail the migration:
	// the template save handlers store this column without validating it.
	exec(`INSERT INTO season_templates (id, template_name, season_number, trackables, defaults, events)
	      VALUES (901, 'Corrupt', 901, '[]', '{}', 'not json at all')`)
	exec(`INSERT INTO seasons (id, name, season_number, start_date, week_count, key_event_name,
	      key_event_required, tier_active_min_pct, tier_at_risk_min_pct, is_active)
	      VALUES (900, 'Test Season', 900, '2026-09-07', 8, 'Rare Soil War', 4, 70, 60, 1)`)

	// Two blanked rows, one renamed blanked row, one still-typed row.
	exec(`INSERT INTO season_events (id, season_id, label, event_type_id, type_name, day_offset, event_time, week_start, week_end, notes)
	      VALUES (901, 900, 'Guard Duty',  NULL, '', 3, '20:00', 1, 1, '')`)
	exec(`INSERT INTO season_events (id, season_id, label, event_type_id, type_name, day_offset, event_time, week_start, week_end, notes)
	      VALUES (902, 900, 'Siege Night', NULL, '', 5, '23:00', 1, 1, '')`)
	exec(`INSERT INTO season_events (id, season_id, label, event_type_id, type_name, day_offset, event_time, week_start, week_end, notes)
	      VALUES (903, 900, 'Gold Dust Conquest', NULL, '', 6, '12:00', 1, 1, '')`)
	exec(`INSERT INTO season_events (id, season_id, label, event_type_id, type_name, day_offset, event_time, week_start, week_end, notes)
	      SELECT 904, 900, 'Guard Duty', id, 'Marshal''s Guard', 3, '20:00', 2, 2, '' FROM schedule_event_types WHERE short_name = 'MG'`)

	goose.SetDialect("sqlite3")
	goose.SetLogger(goose.NopLogger())
	if err := goose.Up(conn, "migrations"); err != nil {
		t.Fatalf("goose.Up to 070: %v", err)
	}

	type row struct {
		name string
		id   *int
	}
	read := func(id int) row {
		t.Helper()
		var r row
		if err := conn.QueryRow(`SELECT type_name, event_type_id FROM season_events WHERE id = ?`, id).Scan(&r.name, &r.id); err != nil {
			t.Fatalf("read %d: %v", id, err)
		}
		return r
	}

	if r := read(901); r.name != "Marshal's Guard" || r.id == nil {
		t.Fatalf("row 901 = %+v, want Marshal's Guard relinked", r)
	}
	if r := read(902); r.name != "Zombie Siege" || r.id == nil {
		t.Fatalf("row 902 = %+v, want Zombie Siege relinked", r)
	}
	// Renamed in the modal — no template entry matches, so it stays for the
	// officer to re-type rather than being guessed at.
	if r := read(903); r.name != "" || r.id != nil {
		t.Fatalf("row 903 = %+v, want left untouched", r)
	}
	if r := read(904); r.name != "Marshal's Guard" || r.id == nil {
		t.Fatalf("row 904 = %+v, want unchanged", r)
	}
}

// TestPushReportsEveryCounter drives a season carrying one of each case, so a
// counter that is computed but never returned is caught here.
func TestPushReportsEveryCounter(t *testing.T) {
	setupSettingsTestDB(t)
	_, mgTypeID := seedSeasonWithEvent(t) // 'Guard Duty', day 3, week 1 — the new row
	var zsTypeID int
	if err := db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name = 'ZS'`).Scan(&zsTypeID); err != nil {
		t.Fatalf("resolve ZS type: %v", err)
	}

	// No day_offset — unscheduled.
	if _, err := db.Exec(`INSERT INTO season_events (season_id, label, event_type_id, type_name,
	      day_offset, event_time, week_start, week_end, notes)
	      VALUES (1, 'Floating', ?, 'Zombie Siege', NULL, '20:00', 1, 1, '')`, zsTypeID); err != nil {
		t.Fatalf("seed unscheduled: %v", err)
	}
	// No type at all — the state issue #6 is about.
	if _, err := db.Exec(`INSERT INTO season_events (season_id, label, event_type_id, type_name,
	      day_offset, event_time, week_start, week_end, notes)
	      VALUES (1, 'Typeless', NULL, '', 4, '20:00', 1, 1, '')`); err != nil {
		t.Fatalf("seed typeless: %v", err)
	}
	// Already on the schedule on the date the push would compute (start + 4 days).
	if _, err := db.Exec(`INSERT INTO schedule_events (event_date, event_type_id, event_time, level, notes, created_by)
	      VALUES ('2026-09-11', ?, '20:00', 1, '', 1)`, zsTypeID); err != nil {
		t.Fatalf("seed existing: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO season_events (season_id, label, event_type_id, type_name,
	      day_offset, event_time, week_start, week_end, notes)
	      VALUES (1, 'Siege Night', ?, 'Zombie Siege', 5, '20:00', 1, 1, '')`, zsTypeID); err != nil {
		t.Fatalf("seed existing season event: %v", err)
	}
	_ = mgTypeID

	s, err := loadSeasonByID(1)
	if err != nil {
		t.Fatalf("loadSeasonByID: %v", err)
	}
	res, err := pushSeasonEventsToSchedule(s, 1, "tester")
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if res.Created != 1 {
		t.Errorf("Created = %d, want 1", res.Created)
	}
	if res.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", res.Skipped)
	}
	if res.SkippedUnscheduled != 1 {
		t.Errorf("SkippedUnscheduled = %d, want 1", res.SkippedUnscheduled)
	}
	if res.SkippedNoType != 1 {
		t.Errorf("SkippedNoType = %d, want 1", res.SkippedNoType)
	}
}
