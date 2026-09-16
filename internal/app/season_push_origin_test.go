package app

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gorilla/mux"
)

// seedPushableSeason creates a season with one alliance event and one server
// event, both running for two weeks.
func seedPushableSeason(t *testing.T, startDate string) (*Season, int, int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO seasons (id, name, season_number, start_date, week_count,
	      key_event_name, key_event_required, tier_active_min_pct, tier_at_risk_min_pct, is_active)
	      VALUES (1, 'Season IX', 9, ?, 2, 'Rare Soil War', 4, 70, 60, 1)`, startDate); err != nil {
		t.Fatalf("seed season: %v", err)
	}
	res, err := db.Exec(`INSERT INTO schedule_event_types (name, short_name, icon, is_system, active, sort_order)
	      VALUES ('City Clash', 'CC', '🏙️', 0, 1, 9)`)
	if err != nil {
		t.Fatalf("seed type: %v", err)
	}
	ccID, _ := res.LastInsertId()

	res, err = db.Exec(`INSERT INTO season_events (season_id, label, event_type_id, type_name,
	      day_offset, event_time, week_start, week_end, notes)
	      VALUES (1, 'City Clash', ?, 'City Clash', 2, '20:00', 1, 2, '')`, ccID)
	if err != nil {
		t.Fatalf("seed alliance event: %v", err)
	}
	allianceEventID, _ := res.LastInsertId()

	res, err = db.Exec(`INSERT INTO season_events (season_id, label, event_type_id, type_name,
	      day_offset, event_time, week_start, week_end, notes, is_server_event, duration_days)
	      VALUES (1, 'Faction Assault', NULL, '', 3, '00:00', 1, 2, '', 1, 2)`)
	if err != nil {
		t.Fatalf("seed server event: %v", err)
	}
	serverEventID, _ := res.LastInsertId()

	s, err := loadSeasonByID(1)
	if err != nil {
		t.Fatalf("loadSeasonByID: %v", err)
	}
	return s, int(allianceEventID), int(serverEventID)
}

// Every pushed row carries the (season_event, week) it came from.
func TestPushStampsOrigin(t *testing.T) {
	setupSettingsTestDB(t)
	s, allianceID, serverID := seedPushableSeason(t, "2026-09-07")

	out, err := pushSeasonEventsToSchedule(s, 1, "tester")
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if out.Created != 4 {
		t.Fatalf("Created = %d, want 4 (2 alliance + 2 server) — %+v", out.Created, out)
	}

	var unstamped int
	db.QueryRow(`SELECT COUNT(*) FROM schedule_events WHERE season_event_id IS NULL`).Scan(&unstamped)
	if unstamped != 0 {
		t.Errorf("%d pushed alliance rows carry no origin", unstamped)
	}
	for _, week := range []int{1, 2} {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM schedule_events WHERE season_event_id = ? AND season_week = ?`,
			allianceID, week).Scan(&n)
		if n != 1 {
			t.Errorf("alliance week %d stamped %d times, want 1", week, n)
		}
		db.QueryRow(`SELECT COUNT(*) FROM server_events WHERE season_event_id = ? AND season_week = ?`,
			serverID, week).Scan(&n)
		if n != 1 {
			t.Errorf("server week %d stamped %d times, want 1", week, n)
		}
	}
}

// THE bug this issue was filed for, reproduced: move the season's start_date and
// push again. Before origins existed the recomputed date matched nothing, so
// every event was created a second time.
func TestRepushAfterStartDateShiftNeitherMovesNorDuplicates(t *testing.T) {
	setupSettingsTestDB(t)
	s, _, _ := seedPushableSeason(t, "2026-09-07")

	if _, err := pushSeasonEventsToSchedule(s, 1, "tester"); err != nil {
		t.Fatalf("first push: %v", err)
	}
	var before []string
	rows, _ := db.Query(`SELECT event_date FROM schedule_events ORDER BY event_date`)
	for rows.Next() {
		var d string
		rows.Scan(&d)
		before = append(before, d)
	}
	rows.Close()

	// The season slips by a day.
	if _, err := db.Exec(`UPDATE seasons SET start_date = '2026-09-08' WHERE id = 1`); err != nil {
		t.Fatalf("shift: %v", err)
	}
	shifted, err := loadSeasonByID(1)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	out, err := pushSeasonEventsToSchedule(shifted, 1, "tester")
	if err != nil {
		t.Fatalf("second push: %v", err)
	}
	if out.Created != 0 {
		t.Errorf("Created = %d after a start-date shift — the duplicates are back", out.Created)
	}
	if out.Drifted != 4 {
		t.Errorf("Drifted = %d, want 4 — the shift must be reported", out.Drifted)
	}
	if len(out.DriftedRows) == 0 || out.DriftedRows[0].Expected == out.DriftedRows[0].Actual {
		t.Errorf("DriftedRows should name both dates: %+v", out.DriftedRows)
	}

	// And nothing MOVED. The officer may have placed a row deliberately; the app
	// reports the disagreement and leaves it alone.
	var after []string
	rows, _ = db.Query(`SELECT event_date FROM schedule_events ORDER BY event_date`)
	for rows.Next() {
		var d string
		rows.Scan(&d)
		after = append(after, d)
	}
	rows.Close()
	if !sameStrings(before, after) {
		t.Errorf("the re-push moved events: %v → %v", before, after)
	}
}

// Rows pushed before origins existed are stamped in place the next time, using
// exactly the match the app already relied on to skip them.
func TestLegacyRowsAreBackStampedOnce(t *testing.T) {
	setupSettingsTestDB(t)
	s, allianceID, _ := seedPushableSeason(t, "2026-09-07")

	if _, err := pushSeasonEventsToSchedule(s, 1, "tester"); err != nil {
		t.Fatalf("push: %v", err)
	}
	// Strip every stamp: this is what an install upgrading to 078 looks like.
	db.Exec(`UPDATE schedule_events SET season_event_id = NULL, season_week = NULL`)
	db.Exec(`UPDATE server_events SET season_event_id = NULL, season_week = NULL`)

	out, err := pushSeasonEventsToSchedule(s, 1, "tester")
	if err != nil {
		t.Fatalf("re-push: %v", err)
	}
	if out.Created != 0 {
		t.Errorf("Created = %d — legacy rows were duplicated instead of stamped", out.Created)
	}
	if out.Skipped != 4 {
		t.Errorf("Skipped = %d, want 4", out.Skipped)
	}

	var stamped int
	db.QueryRow(`SELECT COUNT(*) FROM schedule_events WHERE season_event_id = ?`, allianceID).Scan(&stamped)
	if stamped != 2 {
		t.Errorf("%d alliance rows back-stamped, want 2", stamped)
	}
}

// The case that purges nothing today. Once the start_date has moved, every date
// the legacy purge recomputes is the wrong one.
func TestSeasonDeletePurgesByOriginAfterAShift(t *testing.T) {
	setupSettingsTestDB(t)
	s, _, _ := seedPushableSeason(t, "2026-09-07")
	if _, err := pushSeasonEventsToSchedule(s, 1, "tester"); err != nil {
		t.Fatalf("push: %v", err)
	}
	db.Exec(`UPDATE seasons SET start_date = '2026-09-08' WHERE id = 1`)
	// A season must be archived before it can be deleted.
	db.Exec(`UPDATE seasons SET is_active = 0 WHERE id = 1`)

	req := httptest.NewRequest(http.MethodDelete, "/api/season-hub/seasons/1", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "1"})
	req = scheduleTestActor(req)
	rr := httptest.NewRecorder()
	handleSeasonDelete(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete returned %d: %s", rr.Code, rr.Body.String())
	}

	var left int
	db.QueryRow(`SELECT COUNT(*) FROM schedule_events`).Scan(&left)
	if left != 0 {
		t.Errorf("%d alliance events survived the season delete", left)
	}
	db.QueryRow(`SELECT COUNT(*) FROM server_events WHERE name = 'Faction Assault'`).Scan(&left)
	if left != 0 {
		t.Errorf("%d server events survived the season delete", left)
	}
}

// Deleting a season unlinks any encounter type pointing at a window it purges.
// foreign_keys is off app-wide, so nothing else would.
func TestSeasonDeleteUnlinksEncounterParents(t *testing.T) {
	setupSettingsTestDB(t)
	s, _, _ := seedPushableSeason(t, "2026-09-07")
	if _, err := pushSeasonEventsToSchedule(s, 1, "tester"); err != nil {
		t.Fatalf("push: %v", err)
	}

	var windowID int
	db.QueryRow(`SELECT id FROM server_events WHERE name = 'Faction Assault' LIMIT 1`).Scan(&windowID)
	if windowID == 0 {
		t.Fatal("no materialised window to point at")
	}
	typeID := newCustomType(t, "Assault Encounter", "AE", false)
	db.Exec(`UPDATE schedule_event_types SET server_event_id = ? WHERE id = ?`, windowID, typeID)
	db.Exec(`UPDATE seasons SET is_active = 0 WHERE id = 1`)

	req := httptest.NewRequest(http.MethodDelete, "/api/season-hub/seasons/1", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "1"})
	req = scheduleTestActor(req)
	rr := httptest.NewRecorder()
	handleSeasonDelete(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete returned %d: %s", rr.Code, rr.Body.String())
	}

	var parent *int
	db.QueryRow(`SELECT server_event_id FROM schedule_event_types WHERE id = ?`, typeID).Scan(&parent)
	if parent != nil {
		t.Errorf("the type still points at server event %d, which has been deleted", *parent)
	}
}

// The identity is a DATABASE fact. The push is check-then-insert with no
// transaction, so the index is what stops two simultaneous pushes duplicating.
func TestOriginIdentityIsEnforcedByTheDatabase(t *testing.T) {
	setupSettingsTestDB(t)
	s, allianceID, _ := seedPushableSeason(t, "2026-09-07")
	if _, err := pushSeasonEventsToSchedule(s, 1, "tester"); err != nil {
		t.Fatalf("push: %v", err)
	}

	ccID := typeIDByShort(t, "CC")
	_, err := db.Exec(`INSERT INTO schedule_events
		(event_date, event_type_id, event_time, all_day, notes, created_by, season_event_id, season_week)
		VALUES ('2026-10-01', ?, '20:00', 0, '', 1, ?, 1)`, ccID, allianceID)
	if err == nil {
		t.Fatal("a second row for the same (season_event, week) was accepted")
	}
	if !isUniqueViolation(err) {
		t.Errorf("error was %v, want a UNIQUE violation the push can map to Skipped", err)
	}

	// An unstamped row is unaffected by the index — a manual schedule is full of them.
	if _, err := db.Exec(`INSERT INTO schedule_events
		(event_date, event_type_id, event_time, all_day, notes, created_by)
		VALUES ('2026-10-02', ?, '20:00', 0, '', 1)`, ccID); err != nil {
		t.Errorf("the partial index caught an unstamped row: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO schedule_events
		(event_date, event_type_id, event_time, all_day, notes, created_by)
		VALUES ('2026-10-03', ?, '20:00', 0, '', 1)`, ccID); err != nil {
		t.Errorf("two unstamped rows must not collide: %v", err)
	}
}

// Deleting a template row clears the stamps it left behind, so a later push
// against a reused id cannot claim somebody else's events.
func TestDeletingASeasonEventClearsItsStamps(t *testing.T) {
	setupSettingsTestDB(t)
	s, allianceID, _ := seedPushableSeason(t, "2026-09-07")
	if _, err := pushSeasonEventsToSchedule(s, 1, "tester"); err != nil {
		t.Fatalf("push: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/season-hub/events/1", nil)
	req = mux.SetURLVars(req, map[string]string{"id": itoaTest(allianceID)})
	req = scheduleTestActor(req)
	rr := httptest.NewRecorder()
	handleSeasonEventDelete(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete returned %d: %s", rr.Code, rr.Body.String())
	}

	var dangling int
	db.QueryRow(`SELECT COUNT(*) FROM schedule_events WHERE season_event_id = ?`, allianceID).Scan(&dangling)
	if dangling != 0 {
		t.Errorf("%d rows still point at the deleted template row", dangling)
	}
	// The events themselves stay: deleting a template row changes the plan, it
	// does not retract what is already on the calendar.
	var left int
	db.QueryRow(`SELECT COUNT(*) FROM schedule_events`).Scan(&left)
	if left != 2 {
		t.Errorf("%d alliance events left, want 2 — deleting a template row must not delete the calendar", left)
	}
}

func itoaTest(n int) string { return strconv.Itoa(n) }
