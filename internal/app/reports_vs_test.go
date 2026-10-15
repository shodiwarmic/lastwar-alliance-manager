package app

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// --- The per-week VS minimum (vsMinimumForWeek) ---------------------------------------

func TestVSMinimumResolution(t *testing.T) {
	setupSettingsTestDB(t)
	mustExec(t, `UPDATE settings SET vs_minimum_points = 7200000 WHERE id = 1`)
	const w0, w1, w2, w3, w4 = "2026-08-31", "2026-09-07", "2026-09-14", "2026-09-21", "2026-09-28"
	all := []string{w0, w1, w2, w3, w4}

	check := func(label string, want map[string]vsWeekMinimum) {
		t.Helper()
		many, err := vsMinimumsForWeeks(db, all)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range all {
			one, err := vsMinimumForWeek(db, w)
			if err != nil {
				t.Fatal(err)
			}
			if one != want[w] {
				t.Errorf("%s: %s = %+v, want %+v", label, w, one, want[w])
			}
			if many[w] != one {
				t.Errorf("%s: batch %s = %+v, single %+v", label, w, many[w], one)
			}
		}
	}
	m := func(w string, pts int, src string) vsWeekMinimum { return vsWeekMinimum{w, pts, src} }

	check("no rows", map[string]vsWeekMinimum{
		w0: m(w0, 7200000, "current_setting"), w1: m(w1, 7200000, "current_setting"), w2: m(w2, 7200000, "current_setting"),
		w3: m(w3, 7200000, "current_setting"), w4: m(w4, 7200000, "current_setting")})

	if err := recordVSMinimumSetting(db, w1, 5000000, 0); err != nil {
		t.Fatal(err)
	}
	if err := recordVSMinimumSetting(db, w3, 6000000, 0); err != nil {
		t.Fatal(err)
	}
	check("two settings", map[string]vsWeekMinimum{
		w0: m(w0, 7200000, "current_setting"), w1: m(w1, 5000000, "recorded"), w2: m(w2, 5000000, "recorded"),
		w3: m(w3, 6000000, "recorded"), w4: m(w4, 6000000, "recorded")})

	four := 4000000
	if err := setVSWeekOverride(db, w2, &four, 0); err != nil {
		t.Fatal(err)
	}
	check("override", map[string]vsWeekMinimum{
		w0: m(w0, 7200000, "current_setting"), w1: m(w1, 5000000, "recorded"), w2: m(w2, 4000000, "override"),
		w3: m(w3, 6000000, "recorded"), w4: m(w4, 6000000, "recorded")})

	zero := 0
	if err := setVSWeekOverride(db, w2, &zero, 0); err != nil {
		t.Fatal(err)
	}
	if got, _ := vsMinimumForWeek(db, w2); got != m(w2, 5000000, "recorded") {
		t.Errorf("cleared override: %+v", got)
	}
}

// --- Settings save --------------------------------------------------------------------

func settingsWithMinimum(v int) map[string]any {
	s := baseSettings()
	s["vs_minimum_points"] = v
	return s
}

func TestSettingsSaveRecordsTheWeeksMinimum(t *testing.T) {
	setupSettingsTestDB(t)
	var current int
	db.QueryRow(`SELECT vs_minimum_points FROM settings WHERE id = 1`).Scan(&current)

	if w := putSettings(t, settingsWithMinimum(current)); w.Code != http.StatusOK {
		t.Fatalf("unchanged: %d %s", w.Code, w.Body)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM vs_week_minimums`); n != 0 {
		t.Errorf("an unchanged minimum wrote %d rows", n)
	}

	putSettings(t, settingsWithMinimum(3000000))
	putSettings(t, settingsWithMinimum(4000000))
	var n, pts int
	var week string
	db.QueryRow(`SELECT COUNT(*), MAX(week_date), MAX(minimum_points) FROM vs_week_minimums WHERE kind = 'setting'`).Scan(&n, &week, &pts)
	if n != 1 || week != currentVSWeekMonday() || pts != 4000000 {
		t.Errorf("rows %d, week %s, points %d — want one row for %s at 4000000", n, week, pts, currentVSWeekMonday())
	}
	var details string
	db.QueryRow(`SELECT details FROM activity_log WHERE entity_type = 'vs_week_minimum' ORDER BY id DESC LIMIT 1`).Scan(&details)
	if details != "setting: 3000000 → 4000000" {
		t.Errorf("activity details %q", details)
	}

	if w := putSettings(t, settingsWithMinimum(0)); w.Code != http.StatusBadRequest {
		t.Errorf("0: status %d, want 400", w.Code)
	}

	// A failing history write rolls the setting back with it.
	mustExec(t, `DROP TABLE vs_week_minimums`)
	if w := putSettings(t, settingsWithMinimum(5000000)); w.Code != http.StatusInternalServerError {
		t.Errorf("failing history write: status %d", w.Code)
	}
	db.QueryRow(`SELECT vs_minimum_points FROM settings WHERE id = 1`).Scan(&current)
	if current != 4000000 {
		t.Errorf("setting = %d after a failed save, want 4000000", current)
	}
}

// --- The League week override ---------------------------------------------------------

func seedLeagueWeek(t *testing.T) int {
	t.Helper()
	setupPreviewTestDB(t)
	mustExec(t, `INSERT INTO vs_league_seasons (id, season_number, start_date, is_active) VALUES (1, 34, '2026-09-07', 1)`)
	mustExec(t, `INSERT INTO vs_league_weeks (id, season_id, week_number, week_date) VALUES (7, 1, 2, '2026-09-14')`)
	return 7
}

func overrideAt(t *testing.T, week string) (int, bool) {
	t.Helper()
	var pts int
	err := db.QueryRow(`SELECT minimum_points FROM vs_week_minimums WHERE week_date = ? AND kind = 'override'`, week).Scan(&pts)
	return pts, err == nil
}

func TestLeagueWeekOverride(t *testing.T) {
	id := seedLeagueWeek(t)
	admin := sessionCookie(t, 1, "")
	path := "/api/vs-league/weeks/" + strconv.Itoa(id)

	if w := serveRouter(t, "PUT", path, `{"vs_minimum": 5000000}`, admin); w.Code != http.StatusOK {
		t.Fatalf("set: %d %s", w.Code, w.Body)
	}
	if pts, ok := overrideAt(t, "2026-09-14"); !ok || pts != 5000000 {
		t.Errorf("override = %d, %v", pts, ok)
	}

	w := serveRouter(t, "GET", "/api/vs-league/weeks?season_id=1", "", admin)
	var weeks []VSLeagueWeek
	json.Unmarshal(w.Body.Bytes(), &weeks)
	if len(weeks) != 1 || weeks[0].VSMinimum == nil || *weeks[0].VSMinimum != 5000000 ||
		weeks[0].VSMinimumSetBy == nil || *weeks[0].VSMinimumSetBy != "admin" {
		t.Errorf("GET weeks: %s", w.Body)
	}

	if w := serveRouter(t, "PUT", path, `{"notes": "push"}`, admin); w.Code != http.StatusOK {
		t.Fatalf("omitted: %d", w.Code)
	}
	if pts, _ := overrideAt(t, "2026-09-14"); pts != 5000000 {
		t.Errorf("an omitted vs_minimum changed the override: %d", pts)
	}
	if w := serveRouter(t, "PUT", path, `{"vs_minimum": -1}`, admin); w.Code != http.StatusBadRequest {
		t.Errorf("-1: status %d", w.Code)
	}

	// A date correction carries the override to the new date.
	if w := serveRouter(t, "PUT", path, `{"week_date": "2026-09-21"}`, admin); w.Code != http.StatusOK {
		t.Fatalf("move: %d %s", w.Code, w.Body)
	}
	if _, ok := overrideAt(t, "2026-09-14"); ok {
		t.Error("override left behind on the old date")
	}
	if pts, ok := overrideAt(t, "2026-09-21"); !ok || pts != 5000000 {
		t.Errorf("override not moved: %d, %v", pts, ok)
	}

	// A user delete keeps the row and clears who set it.
	if err := clearUserReferences(db, "id = ?", 1); err != nil {
		t.Fatal(err)
	}
	var setBy *int
	db.QueryRow(`SELECT set_by FROM vs_week_minimums WHERE kind = 'override'`).Scan(&setBy)
	if setBy != nil {
		t.Errorf("set_by = %d after the user delete", *setBy)
	}

	// 0 clears; a week delete removes it with the week.
	serveRouter(t, "PUT", path, `{"vs_minimum": 0}`, admin)
	if _, ok := overrideAt(t, "2026-09-21"); ok {
		t.Error("0 did not clear the override")
	}
	serveRouter(t, "PUT", path, `{"vs_minimum": 4000000}`, admin)
	if w := serveRouter(t, "DELETE", path, "", admin); w.Code != http.StatusOK {
		t.Fatalf("delete: %d", w.Code)
	}
	if _, ok := overrideAt(t, "2026-09-21"); ok {
		t.Error("override outlived its week")
	}
}
