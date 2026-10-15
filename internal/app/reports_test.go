package app

import (
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Game time is UTC-2: 2026-10-19T01:00Z is still Sunday the 18th in the game.
func TestLastCompleteSaturday(t *testing.T) {
	cases := map[string]string{
		"2026-10-15T12:00:00Z": "2026-10-10", // Thursday
		"2026-10-17T12:00:00Z": "2026-10-10", // Saturday: today's Saturday is not over
		"2026-10-18T12:00:00Z": "2026-10-17", // Sunday: yesterday
		"2026-10-19T01:00:00Z": "2026-10-17", // UTC Monday, game Sunday
		"2026-10-19T03:00:00Z": "2026-10-17", // game Monday: the day before yesterday
	}
	for now, want := range cases {
		if got := lastCompleteSaturday(mustTime(t, now)); got != want {
			t.Errorf("%s: got %s, want %s", now, got, want)
		}
	}
}

func TestResolveReportRange(t *testing.T) {
	setupSettingsTestDB(t)
	thu := mustTime(t, "2026-10-15T12:00:00Z")
	cases := []struct {
		name string
		p    reportRangeParams
		now  time.Time
		want reportRange
	}{
		{"4 complete weeks", reportRangeParams{Weeks: 4}, thu, reportRange{"2026-09-14", "2026-10-10"}},
		{"1 week", reportRangeParams{Weeks: 1}, thu, reportRange{"2026-10-05", "2026-10-10"}},
		{"current week added", reportRangeParams{Weeks: 1, Current: true}, thu, reportRange{"2026-10-05", "2026-10-15"}},
		{"sunday includes this week", reportRangeParams{Weeks: 1}, mustTime(t, "2026-10-19T01:00:00Z"), reportRange{"2026-10-12", "2026-10-17"}},
		{"monday", reportRangeParams{Weeks: 1}, mustTime(t, "2026-10-19T03:00:00Z"), reportRange{"2026-10-12", "2026-10-17"}},
		{"explicit", reportRangeParams{From: "2026-09-01", To: "2026-09-30"}, thu, reportRange{"2026-09-01", "2026-09-30"}},
		{"explicit clamped to today", reportRangeParams{From: "2026-10-01", To: "2026-12-31"}, thu, reportRange{"2026-10-01", "2026-10-15"}},
	}
	for _, c := range cases {
		got, err := resolveReportRange(db, c.p, c.now)
		if err != nil || got != c.want {
			t.Errorf("%s: got %+v, %v; want %+v", c.name, got, err, c.want)
		}
	}

	for name, p := range map[string]reportRangeParams{
		"reversed":    {From: "2026-10-10", To: "2026-10-01"},
		"half given":  {From: "2026-10-10"},
		"bad date":    {From: "10/01/2026", To: "2026-10-10"},
		"future":      {From: "2026-11-01", To: "2026-11-30"},
		"negative":    {Weeks: -1},
		"all, no VS ": {All: true},
	} {
		if _, err := resolveReportRange(db, p, thu); !isReportParamError(err) {
			t.Errorf("%s: err = %v, want a parameter error", name, err)
		}
	}

	mustExec(t, `INSERT INTO members (id, name, rank) VALUES (1, 'A', 'R3')`)
	mustExec(t, `INSERT INTO vs_points (member_id, week_date, monday) VALUES (1, '2026-03-09', 1), (1, '2026-10-05', 1)`)
	got, err := resolveReportRange(db, reportRangeParams{All: true}, thu)
	if err != nil || got != (reportRange{"2026-03-09", "2026-10-10"}) {
		t.Errorf("all: got %+v, %v", got, err)
	}
}

func TestVisibleReports(t *testing.T) {
	setupSettingsTestDB(t)
	mustExec(t, `UPDATE rank_permissions SET permissions = json_set(permissions,
		'$.view_report_weekly', json('false'), '$.view_report_vs_misses', json('true'),
		'$.view_report_participation', json('false')) WHERE rank = 'R3'`)
	mustExec(t, `UPDATE rank_permissions SET permissions = json_set(permissions,
		'$.view_report_weekly', json('false'), '$.view_report_vs_misses', json('false'),
		'$.view_report_participation', json('false')) WHERE rank = 'R1'`)
	mid := 1
	keys := func(u *AuthUser) (out []string) {
		for _, d := range visibleReports(u) {
			out = append(out, d.Key)
		}
		return out
	}
	if got := keys(&AuthUser{ID: 1, IsAdmin: true}); len(got) != len(reportDefs) {
		t.Errorf("admin sees %v", got)
	}
	if got := keys(&AuthUser{ID: 2, MemberID: &mid, Rank: "R3"}); len(got) != 1 || got[0] != "vs-misses" {
		t.Errorf("R3 sees %v", got)
	}
	if got := keys(&AuthUser{ID: 3, MemberID: &mid, Rank: "R1"}); len(got) != 0 {
		t.Errorf("R1 sees %v", got)
	}
}

// Migration 085 on an install with history: the report keys follow view_accountability,
// a key set by hand first is left alone, and the current week's setting row is recorded.
func TestReportsMigrationSeedsKeysAndTheCurrentWeek(t *testing.T) {
	conn := migrateTo(t, 84)
	for _, q := range []string{
		`INSERT INTO members (id, name, rank) VALUES (1, 'A', 'R3')`,
		`INSERT INTO vs_points (member_id, week_date, monday) VALUES (1, '2026-10-05', 5)`,
		`UPDATE settings SET vs_minimum_points = 7200000 WHERE id = 1`,
		`UPDATE rank_permissions SET permissions = json_set(permissions, '$.view_accountability', json('true')) WHERE rank IN ('R4', 'R5')`,
		`UPDATE rank_permissions SET permissions = json_set(permissions, '$.view_accountability', json('false')) WHERE rank IN ('R1', 'R2', 'R3')`,
		`UPDATE rank_permissions SET permissions = json_set(permissions, '$.view_report_vs_misses', json('false')) WHERE rank = 'R5'`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("fixture %q: %v", q, err)
		}
	}
	if err := goose.UpTo(conn, "migrations", 85); err != nil {
		t.Fatalf("goose.UpTo(85): %v", err)
	}

	perm := func(rank, key string) any {
		var v any
		conn.QueryRow(`SELECT json_extract(permissions, '$.' || ?) FROM rank_permissions WHERE rank = ?`, key, rank).Scan(&v)
		return v
	}
	for _, key := range []string{"view_report_weekly", "view_report_vs_misses", "view_report_participation"} {
		if v := perm("R4", key); v != int64(1) {
			t.Errorf("R4 %s = %v, want 1", key, v)
		}
		if v := perm("R3", key); v != nil {
			t.Errorf("R3 %s = %v, want absent", key, v)
		}
	}
	if v := perm("R5", "view_report_vs_misses"); v != int64(0) {
		t.Errorf("R5's hand-set key was overwritten: %v", v)
	}

	var n, pts int
	var week string
	conn.QueryRow(`SELECT COUNT(*), MAX(week_date), MAX(minimum_points) FROM vs_week_minimums WHERE kind = 'setting'`).Scan(&n, &week, &pts)
	if n != 1 || week != currentVSWeekMonday() || pts != 7200000 {
		t.Errorf("setting rows = %d, week %s (want %s), points %d", n, week, currentVSWeekMonday(), pts)
	}
}

// A fresh install has no VS history, so nothing is recorded as if chosen.
func TestReportsMigrationRecordsNothingOnAFreshInstall(t *testing.T) {
	setupSettingsTestDB(t)
	if n := countRows(t, `SELECT COUNT(*) FROM vs_week_minimums`); n != 0 {
		t.Errorf("fresh install has %d minimum rows", n)
	}
}
