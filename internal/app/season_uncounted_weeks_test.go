package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Weeks that don't count (migration 090, private-docs 283; owner, 2026-10-22): the reward
// week is left out of every standing.
func TestUncountedWeeksStanding(t *testing.T) {
	s := statusSeason()
	s.UncountedWeeks = []int{8}
	// Seven Full weeks and an unscored week 8: 100 %, not 87.5 %.
	got := computeSeasonStanding(s, standingInputs{Weeks: weeksOf("full", "full", "full", "full", "full", "full", "full", ""), MaxWeekPts: 10})
	if got.Overall.Pct != 100 || got.Overall.Status != "active" {
		t.Fatalf("overall %+v", got.Overall)
	}
	if strings.Contains(got.Overall.Reason, "not scored") || !strings.Contains(got.Overall.Reason, "doesn't count this season: week 8") {
		t.Errorf("reason %q", got.Overall.Reason)
	}
	// A score recorded in the week anyway changes nothing.
	got = computeSeasonStanding(s, standingInputs{Weeks: weeksOf("full", "full", "full", "full", "full", "full", "full", "absent"), MaxWeekPts: 10})
	if got.Overall.Pct != 100 {
		t.Errorf("an Absent in the uncounted week moved the standing: %+v", got.Overall)
	}
	// Without the setting the same week counts as Absent (decision 4).
	s.UncountedWeeks = nil
	if got := computeSeasonStanding(s, standingInputs{Weeks: weeksOf("full", "full", "full", "full", "full", "full", "full", ""), MaxWeekPts: 10}); got.Overall.Pct != 87.5 {
		t.Errorf("counted week 8: %+v", got.Overall)
	}

	if _, err := cleanUncountedWeeks([]int{9}, 8); err == nil {
		t.Error("week 9 of an eight-week season was accepted")
	}
	if w, err := cleanUncountedWeeks([]int{8, 7, 8}, 8); err != nil || len(w) != 2 || w[0] != 7 || w[1] != 8 {
		t.Errorf("clean: %v %v", w, err)
	}
}

// 090 leaves week 8 out only on an archived eight-week season nobody scored for it, and gives
// the shipped templates the default only while they still read as shipped.
func TestMigration090UncountedWeeks(t *testing.T) {
	conn := migrateTo(t, 89)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO members (id, name, rank) VALUES (1, 'Alpha', 'R3')`)
	exec(`INSERT INTO seasons (id, name, season_number, start_date, week_count, key_event_name, key_event_required, is_active, archived_at)
		VALUES (1, 'Scored W8', 1, '2026-01-05', 8, 'K', 0, 0, '2026-03-02 00:00:00'),
		       (2, 'Empty W8', 2, '2026-03-02', 8, 'K', 0, 0, '2026-04-27 00:00:00'),
		       (3, 'Active', 3, '2026-04-27', 8, 'K', 0, 1, NULL),
		       (4, 'Ten weeks', 4, '2025-01-06', 10, 'K', 0, 0, '2025-03-17 00:00:00')`)
	exec(`INSERT INTO season_participation (season_id, member_id, week_number, score) VALUES (1, 1, 8, 'full'), (2, 1, 7, 'full')`)
	// An operator's edited template keeps its defaults.
	exec(`UPDATE season_templates SET defaults = '{"week_count":9}' WHERE season_number = 2`)
	if err := gooseUpTo(conn, 90); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[int]string{1: "[]", 2: "[8]", 3: "[]", 4: "[]"} {
		var got string
		conn.QueryRow(`SELECT uncounted_weeks FROM seasons WHERE id = ?`, id).Scan(&got)
		if got != want {
			t.Errorf("season %d: %s, want %s", id, got, want)
		}
	}
	var s4, s2 string
	conn.QueryRow(`SELECT json_extract(defaults, '$.uncounted_weeks') FROM season_templates WHERE season_number = 4`).Scan(&s4)
	conn.QueryRow(`SELECT defaults FROM season_templates WHERE season_number = 2`).Scan(&s2)
	if s4 != "[8]" || s2 != `{"week_count":9}` {
		t.Errorf("templates: S4 %q, edited S2 %q", s4, s2)
	}
}

func TestSeasonUncountedWeeksHandlers(t *testing.T) {
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	m := seedMember(t, "Alpha", "R3")
	var tmplID int
	db.QueryRow(`SELECT id FROM season_templates WHERE season_number = 4`).Scan(&tmplID)
	id, rr := createSeason(t, `{"template_id":`+strconv.Itoa(tmplID)+`,"start_date":"2031-03-03"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	s, err := loadSeasonByID(id)
	if err != nil || len(s.UncountedWeeks) != 1 || s.UncountedWeeks[0] != 8 {
		t.Fatalf("a template season starts with week 8 left out: %+v %v", s.UncountedWeeks, err)
	}

	update := func(weeks string) *httptest.ResponseRecorder {
		body := `{"name":"` + s.Name + `","season_number":` + strconv.Itoa(s.SeasonNumber) + `,"start_date":"2031-03-03","week_count":8,
			"key_event_name":"K","tier_active_min_pct":70,"tier_at_risk_min_pct":60,"uncounted_weeks":` + weeks + `}`
		rr := httptest.NewRecorder()
		handleSeasonUpdate(rr, shReq(http.MethodPut, body, map[string]string{"id": strconv.Itoa(id)}, nil))
		return rr
	}
	if rr := update(`[9]`); rr.Code != http.StatusBadRequest {
		t.Errorf("week 9 of 8: %d", rr.Code)
	}
	if rr := update(`[8,7,8]`); rr.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rr.Code, rr.Body.String())
	}
	var stored string
	db.QueryRow(`SELECT uncounted_weeks FROM seasons WHERE id = ?`, id).Scan(&stored)
	if stored != "[7,8]" {
		t.Errorf("stored %s", stored)
	}
	if rr := update(`[8]`); rr.Code != http.StatusOK {
		t.Fatalf("update: %d", rr.Code)
	}

	// The report: seven Full weeks and nothing in week 8 read 100 %, week 8 "not_counted".
	for w := 1; w <= 7; w++ {
		mustExecT(t, `INSERT INTO season_participation (season_id, member_id, week_number, score) VALUES (?, ?, ?, 'full')`, id, m, w)
	}
	if err := archiveSeason(id, "2031-04-28"); err != nil {
		t.Fatal(err)
	}
	rep, err := runSeasonParticipationReport(db, seasonReportParams{SeasonID: id}, time.Date(2031, 5, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Members) != 1 {
		b, _ := json.Marshal(rep)
		t.Fatalf("members: %s", b)
	}
	r := rep.Members[0]
	if r.Weekly[7] != "not_counted" || r.NotScored != 0 || r.Full != 7 || r.Standing.Overall.Pct != 100 {
		t.Errorf("report row: weekly %v, not scored %d, full %d, overall %v", r.Weekly, r.NotScored, r.Full, r.Standing.Overall.Pct)
	}
	if len(rep.UncountedWeeks) != 1 || rep.UncountedWeeks[0] != 8 {
		t.Errorf("report uncounted weeks %v", rep.UncountedWeeks)
	}
}
