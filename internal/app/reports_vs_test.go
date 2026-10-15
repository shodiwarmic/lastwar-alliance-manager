package app

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"reflect"
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

// --- The VS-misses report --------------------------------------------------------------

// Three weeks seen on Thursday 2026-10-15. W1 has no recorded minimum (today's 4M), W2 a
// recorded 5M with Thu–Sat never imported, W3 (this week) an override of 3M. Alpha is
// ordinary, Bravo joined on W2's Wednesday, Charlie is a former member, Delta has no row
// for W1 although W1 was imported (no score recorded: not misses).
const vsMissesNow = "2026-10-15T12:00:00Z"

func seedVSMisses(t *testing.T) {
	t.Helper()
	setupSettingsTestDB(t)
	const M = 1000000
	mustExec(t, `UPDATE settings SET vs_minimum_points = 4000000 WHERE id = 1`)
	mustExec(t, `INSERT INTO members (id, name, rank, joined_at) VALUES
		(1, 'Alpha', 'R4', '2026-01-01'), (2, 'Bravo', 'R3', '2026-10-07'),
		(3, 'Charlie', 'EX', '2026-01-01'), (4, 'Delta', 'R2', '2026-01-01')`)
	ins := func(id int, week string, d ...int) {
		t.Helper()
		var v [6]int
		copy(v[:], d)
		mustExec(t, `INSERT INTO vs_points (member_id, week_date, monday, tuesday, wednesday, thursday, friday, saturday)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, week, v[0], v[1], v[2], v[3], v[4], v[5])
	}
	ins(1, "2026-09-28", 5*M, 5*M, 5*M, 3*M, 5*M, 5*M)
	ins(3, "2026-09-28", 0, 0, 0, 0, 0, 0)
	ins(1, "2026-10-05", 6*M, 4*M, 6*M)
	ins(2, "2026-10-05", 0, 0, 1*M)
	ins(4, "2026-10-05", 5*M, 5*M, 5*M)
	ins(1, "2026-10-12", 3*M, 2*M, 3*M, 9*M)
	ins(2, "2026-10-12", 3*M, 3*M, 3*M)
	ins(4, "2026-10-12", 3*M, 3*M, 1*M)
	if err := recordVSMinimumSetting(db, "2026-10-05", 5*M, 0); err != nil {
		t.Fatal(err)
	}
	three := 3 * M
	if err := setVSWeekOverride(db, "2026-10-12", &three, 0); err != nil {
		t.Fatal(err)
	}
}

func missesByName(rep *vsMissesReport) map[string]vsMissesMember {
	out := map[string]vsMissesMember{}
	for _, m := range rep.Members {
		out[m.Name] = m
	}
	return out
}

func TestVSMissesReport(t *testing.T) {
	seedVSMisses(t)
	rep, err := runVSMissesReport(db, vsMissesParams{Range: reportRangeParams{From: "2026-09-28", To: "2026-10-31"}}, mustTime(t, vsMissesNow))
	if err != nil {
		t.Fatal(err)
	}
	if rep.From != "2026-09-28" || rep.To != "2026-10-15" {
		t.Errorf("range %s – %s", rep.From, rep.To)
	}
	by := missesByName(rep)
	want := map[string][2]int{"Alpha": {12, 3}, "Bravo": {4, 1}, "Delta": {6, 1}} // days evaluated, misses
	for name, w := range want {
		if m := by[name]; m.DaysEvaluated != w[0] || m.Misses != w[1] {
			t.Errorf("%s: evaluated %d misses %d, want %v", name, m.DaysEvaluated, m.Misses, w)
		}
	}
	if _, ok := by["Charlie"]; ok {
		t.Error("a former member is listed")
	}
	if names := []string{rep.Members[0].Name, rep.Members[1].Name, rep.Members[2].Name}; names[0] != "Alpha" || names[1] != "Bravo" || names[2] != "Delta" {
		t.Errorf("order %v", names)
	}

	// Each miss is judged against its own week's minimum.
	a := by["Alpha"]
	wantDays := []vsMissDay{
		{Date: "2026-10-01", Weekday: "Thu", WeekDate: "2026-09-28", Points: 3000000, Minimum: 4000000, Shortfall: 1000000},
		{Date: "2026-10-06", Weekday: "Tue", WeekDate: "2026-10-05", Points: 4000000, Minimum: 5000000, Shortfall: 1000000},
		{Date: "2026-10-13", Weekday: "Tue", WeekDate: "2026-10-12", Points: 2000000, Minimum: 3000000, Shortfall: 1000000},
	}
	if !reflect.DeepEqual(a.Days, wantDays) {
		t.Errorf("Alpha's days:\n got %+v\nwant %+v", a.Days, wantDays)
	}
	if b := by["Bravo"]; len(b.Days) != 1 || b.Days[0].Date != "2026-10-07" {
		t.Errorf("Bravo (joined 10-07) misses %+v", b.Days)
	}
	if d := by["Delta"]; d.NoScoreDays != 6 || d.Worst == nil || d.Worst.Date != "2026-10-14" || d.AvgShortfall != 2000000 {
		t.Errorf("Delta no-score %d worst %+v avg %d", d.NoScoreDays, d.Worst, d.AvgShortfall)
	}

	cov := rep.Coverage
	if cov.DaysEvaluated != 12 || cov.MembersEvaluated != 3 || cov.MembersExcludedEX != 1 || cov.NoScoreDays != 6 {
		t.Errorf("coverage %+v", cov)
	}
	if !reflect.DeepEqual(cov.DaysNotImported, []string{"2026-10-08", "2026-10-09", "2026-10-10"}) {
		t.Errorf("not imported %v", cov.DaysNotImported)
	}
	if !reflect.DeepEqual(cov.WeeksWithoutRecord, []string{"2026-09-28"}) {
		t.Errorf("weeks without record %v", cov.WeeksWithoutRecord)
	}
	srcs := []string{rep.Weeks[0].Minimum.Source, rep.Weeks[1].Minimum.Source, rep.Weeks[2].Minimum.Source}
	if srcs[0] != vsMinSourceCurrent || srcs[1] != vsMinSourceRecorded || srcs[2] != vsMinSourceOverride {
		t.Errorf("week minimum sources %v", srcs)
	}
}

// Days outside [from, to] in a boundary week are not evaluated.
func TestVSMissesBoundaryWeek(t *testing.T) {
	seedVSMisses(t)
	rep, err := runVSMissesReport(db, vsMissesParams{Range: reportRangeParams{From: "2026-10-02", To: "2026-10-06"}}, mustTime(t, vsMissesNow))
	if err != nil {
		t.Fatal(err)
	}
	by := missesByName(rep)
	if a := by["Alpha"]; a.DaysEvaluated != 4 || a.Misses != 1 || a.Days[0].Date != "2026-10-06" {
		t.Errorf("Alpha %+v", a)
	}
	if rep.Coverage.DaysEvaluated != 4 {
		t.Errorf("days evaluated %d, want 4 (Fri, Sat, Mon, Tue)", rep.Coverage.DaysEvaluated)
	}
}

func TestVSMissesOneMember(t *testing.T) {
	seedVSMisses(t)
	now := mustTime(t, vsMissesNow)
	rep, err := runVSMissesReport(db, vsMissesParams{Range: reportRangeParams{Weeks: 2}, MemberID: 1}, now)
	if err != nil || len(rep.Members) != 1 || rep.Members[0].Name != "Alpha" {
		t.Fatalf("one member: %+v, %v", rep, err)
	}
	if _, err := runVSMissesReport(db, vsMissesParams{Range: reportRangeParams{Weeks: 2}, MemberID: 3}, now); !isReportParamError(err) {
		t.Errorf("a former member: err = %v", err)
	}
}

// vsDaysBelow is the count of vsDayStatuses' below days, and agrees with the rule as it
// was written before the split — for a member with a row. Without one it is always 0.
func TestVSDayStatusesMatchTheOldRule(t *testing.T) {
	old := func(scores [6]int, hasRow bool, joinedAt, weekDate string, completed int, dayImported [6]bool, vsMin int) int {
		if !hasRow {
			return 0
		}
		n := 0
		for i := 0; i < completed && i < 6; i++ {
			if !dayImported[i] || (joinedAt != "" && dayDate(weekDate, i) < joinedAt) {
				continue
			}
			if scores[i] < vsMin {
				n++
			}
		}
		return n
	}
	r := rand.New(rand.NewSource(19))
	for k := 0; k < 2000; k++ {
		var sc [6]int
		var imp [6]bool
		for i := range sc {
			sc[i] = r.Intn(10) * 1000000
			imp[i] = r.Intn(4) > 0
		}
		joined := ""
		if r.Intn(2) == 0 {
			joined = dayDate("2026-10-12", r.Intn(9)-1)
		}
		completed, minimum, hasRow := r.Intn(7), (r.Intn(9)+1)*1000000, r.Intn(5) > 0
		if got, want := vsDaysBelow(sc, hasRow, joined, "2026-10-12", completed, imp, minimum), old(sc, hasRow, joined, "2026-10-12", completed, imp, minimum); got != want {
			t.Fatalf("case %d: %d, old rule %d", k, got, want)
		}
	}
}

func TestVSMissesRoute(t *testing.T) {
	setupPreviewTestDB(t)
	setReportKeys(t, true, false, true)
	officer := sessionCookie(t, 3, "")
	if w := serveRouter(t, "GET", "/api/reports/vs-misses?weeks=4", "", officer); w.Code != http.StatusForbidden {
		t.Errorf("without the key: %d", w.Code)
	}
	setReportKeys(t, false, true, false)
	mustExec(t, `INSERT INTO vs_points (member_id, week_date, monday) VALUES (1, '2026-09-07', 1)`)
	for q, want := range map[string]int{"weeks=4": 200, "weeks=all": 200, "weeks=x": 400, "from=2026-10-01": 400, "weeks=2&member_id=x": 400} {
		if w := serveRouter(t, "GET", "/api/reports/vs-misses?"+q, "", officer); w.Code != want {
			t.Errorf("%s: %d, want %d (%s)", q, w.Code, want, w.Body)
		}
	}
}
