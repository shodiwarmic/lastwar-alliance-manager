package app

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"testing"
)

// runWeeklyOnSeparateConn runs the report on its own connection to the test database
// with the global db unset, so a stray read through the global panics.
func runWeeklyOnSeparateConn(t *testing.T, now string) *weeklySummaryReport {
	t.Helper()
	conn, err := sql.Open("sqlite", os.Getenv("DATABASE_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	prev := db
	db = nil
	defer func() { db = prev }()
	rep, err := runWeeklySummaryReport(conn, weeklySummaryParams{}, mustTime(t, now))
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

// The old /api/accountability/report-data handler's JSON for the fixture, captured
// before it was deleted: the new function must reproduce every field it had.
func TestWeeklySummaryMatchesTheOldReport(t *testing.T) {
	setupPreviewTestDB(t)
	seedWeeklyFixture(t)
	rep := runWeeklyOnSeparateConn(t, weeklyGoldenNow)

	golden, err := os.ReadFile("internal/app/testdata/weekly_summary_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var want, got map[string]any
	json.Unmarshal(golden, &want)
	b, _ := json.Marshal(rep)
	json.Unmarshal(b, &got)
	// power_growth is the one field that changed on purpose (241, C6): a member with no
	// reading 30 days old has no growth, so the new joiner the old report ranked first
	// is left out and counted. TestWeeklySummaryGrowth pins the new list.
	delete(want, "power_growth")
	for k, v := range want {
		if !reflect.DeepEqual(got[k], v) {
			t.Errorf("%s:\n got %v\nwant %v", k, got[k], v)
		}
	}
	if rep.WeekDate != "2026-10-12" || rep.VSMinSource != vsMinSourceCurrent || rep.GeneratedAt != weeklyGoldenNow {
		t.Errorf("week %s, source %s, generated %s", rep.WeekDate, rep.VSMinSource, rep.GeneratedAt)
	}
}

// On a Monday the evaluated week is last week, all six days complete.
func TestWeeklySummaryMondayFallback(t *testing.T) {
	setupPreviewTestDB(t)
	seedWeeklyFixture(t)
	rep := runWeeklyOnSeparateConn(t, "2026-10-19T12:00:00Z")
	if rep.WeekDate != "2026-10-12" || !rep.FallbackActive || rep.Completed != 6 || rep.ImportedCompleted != 3 {
		t.Errorf("week %s fallback %v completed %d imported %d", rep.WeekDate, rep.FallbackActive, rep.Completed, rep.ImportedCompleted)
	}
}

// A recorded override for the week is what the summary judges against, and says so.
func TestWeeklySummaryUsesTheWeeksMinimum(t *testing.T) {
	setupPreviewTestDB(t)
	seedWeeklyFixture(t)
	pts := 2000000
	if err := setVSWeekOverride(db, "2026-10-12", &pts, 0); err != nil {
		t.Fatal(err)
	}
	rep := runWeeklyOnSeparateConn(t, weeklyGoldenNow)
	if rep.VSMin != 2000000 || rep.VSMinSource != vsMinSourceOverride {
		t.Errorf("min %d source %s", rep.VSMin, rep.VSMinSource)
	}
	for _, m := range rep.VSUnderperformers {
		if m.Name == "Bravo" {
			t.Errorf("Bravo is above a 2M minimum every day, but flagged")
		}
	}
}

func TestWeeklySummaryRoutes(t *testing.T) {
	setupPreviewTestDB(t)
	setReportKeys(t, false, true, true)
	officer, admin := sessionCookie(t, 3, ""), sessionCookie(t, 1, "")
	if w := serveRouter(t, "GET", "/api/reports/weekly", "", officer); w.Code != http.StatusForbidden {
		t.Errorf("without the key: %d", w.Code)
	}
	setReportKeys(t, true, false, false)
	if w := serveRouter(t, "GET", "/api/reports/weekly", "", officer); w.Code != http.StatusOK {
		t.Errorf("with the key: %d %s", w.Code, w.Body)
	}
	if w := serveRouter(t, "GET", "/api/reports/weekly", "", admin); w.Code != http.StatusOK {
		t.Errorf("admin: %d", w.Code)
	}
	w := serveRouter(t, "GET", "/accountability/report", "", admin)
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/reports#weekly" {
		t.Errorf("old page: %d → %q", w.Code, w.Header().Get("Location"))
	}
	if w := serveRouter(t, "GET", "/api/accountability/report-data", "", admin); w.Code != http.StatusNotFound {
		t.Errorf("old data route: %d, want 404", w.Code)
	}
}

// 241: Charlie joined with 150M and has no reading 30 days old — not 150M of growth.
func TestWeeklySummaryGrowth(t *testing.T) {
	setupPreviewTestDB(t)
	seedWeeklyFixture(t)
	rep := runWeeklyOnSeparateConn(t, weeklyGoldenNow)
	var got []string
	for _, m := range rep.PowerGrowth {
		got = append(got, m.Name+":"+strconv.Itoa(m.Value))
	}
	want := []string{"Alpha:30000000", "Delta:5000000", "Echo:1000000", "Bravo:-10000000"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("top growth %v, want %v", got, want)
	}
	// Charlie, and the two seeded members with no power history at all.
	if rep.PowerGrowthExcluded != 3 || rep.GrowthWindowDays != 30 {
		t.Errorf("excluded %d window %d", rep.PowerGrowthExcluded, rep.GrowthWindowDays)
	}
}
