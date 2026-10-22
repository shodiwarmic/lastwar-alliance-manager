package app

import (
	"net/http"
	"testing"
	"time"
)

func TestSeasonParticipationReport(t *testing.T) {
	seedAttendanceSeason(t) // started 2026-09-07; three members; two completed wars in week 2
	mustExecT(t, `INSERT INTO season_participation (season_id, member_id, week_number, score) VALUES
		(1, 1, 1, 'full'), (1, 1, 2, 'absent'), (1, 2, 1, 'partial')`)
	mustExecT(t, `INSERT INTO season_war_attendance (war_id, member_id, attended, poll_response) VALUES
		(1, 1, 1, 'yes'), (2, 1, 1, 'no'), (1, 2, 0, 'yes')`)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC) // two weeks complete
	tx, _ := db.Begin()
	defer tx.Rollback()
	rep, err := runSeasonParticipationReport(tx, seasonReportParams{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Season.ID != 1 || len(rep.Seasons) != 1 || rep.CompletedWeeks != 2 || !rep.InProgress || len(rep.Weeks) != 8 {
		t.Fatalf("report header: %+v", rep)
	}
	by := map[int]seasonReportMember{}
	for _, m := range rep.Members {
		by[m.MemberID] = m
	}
	a, b := by[1], by[2]
	// Absent and not scored are told apart; weeks still to come are blank.
	if a.Weekly[0] != "full" || a.Weekly[1] != "absent" || a.Weekly[2] != "" || a.Absent != 1 || a.NotScored != 0 {
		t.Errorf("Alpha weekly %v absent %d not scored %d", a.Weekly, a.Absent, a.NotScored)
	}
	if b.Weekly[1] != "not_scored" || b.NotScored != 1 || b.Partial != 1 {
		t.Errorf("Bravo weekly %v", b.Weekly)
	}
	if a.WarsAttack != 1 || a.WarsDefense != 1 || a.WarsAttended != 2 || *a.WarsCompleted != 2 || *a.PollPct != 100 {
		t.Errorf("Alpha wars: %+v", a)
	}
	if b.WarsAttended != 0 || *b.PollAttackPct != 100 || *b.PollDefensePct != 0 {
		t.Errorf("Bravo polls: attack %v defense %v", *b.PollAttackPct, *b.PollDefensePct)
	}
	tx.Rollback()

	// An archived season reads its snapshot: a member deleted since is still listed.
	mustExecT(t, `INSERT INTO season_roster_snapshot (season_id, member_id, name, rank) VALUES (1, 1, 'Alpha', 'R3'), (1, 2, 'Bravo', 'R3'), (1, 3, 'Charlie Then', 'R3')`)
	mustExecT(t, `UPDATE seasons SET archived_at = '2026-11-01 00:00:00', is_active = 0 WHERE id = 1`)
	mustExecT(t, `DELETE FROM members WHERE id = 3`)
	tx2, _ := db.Begin()
	defer tx2.Rollback()
	rep, err = runSeasonParticipationReport(tx2, seasonReportParams{SeasonID: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range rep.Members {
		found = found || m.Name == "Charlie Then"
	}
	if !found || rep.InProgress || rep.CompletedWeeks != 8 {
		t.Errorf("archived: in progress %v, weeks %d, members %+v", rep.InProgress, rep.CompletedWeeks, rep.Members)
	}
}

func TestSeasonParticipationReportGate(t *testing.T) {
	setupPreviewTestDB(t)
	route := requirePermission("view_report_season_participation", handleSeasonParticipationReport)
	set := func(v string) {
		mustExec(t, `UPDATE rank_permissions SET permissions = json_set(permissions, '$.view_report_season_participation', json('`+v+`')) WHERE rank = 'R3'`)
	}
	officer := sessionCookie(t, 3, "")
	set("false")
	if w := serveAs(officer, http.MethodGet, "/api/reports/season-participation", "", route); w.Code != http.StatusForbidden {
		t.Errorf("without the key: %d", w.Code)
	}
	set("true")
	if w := serveAs(officer, http.MethodGet, "/api/reports/season-participation", "", route); w.Code != http.StatusOK {
		t.Errorf("with the key: %d %s", w.Code, w.Body.String())
	}
}
