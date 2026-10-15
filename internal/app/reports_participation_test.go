package app

import (
	"net/http"
	"testing"
)

// seedGridBoard records a board for a new occurrence of short on date: entries as
// member → primary value (nil = on the board with no value), roles as member → role.
func seedGridBoard(t *testing.T, short, date, taskForce string, entries map[int]*int64, roles map[int]string, excused map[int]string) int {
	t.Helper()
	var typeID, trackable int
	if err := db.QueryRow(`SELECT t.id, tr.id FROM schedule_event_types t
		JOIN participation_trackables tr ON tr.event_type_id = t.id WHERE t.short_name = ? ORDER BY tr.sort_order LIMIT 1`, short).Scan(&typeID, &trackable); err != nil {
		t.Fatalf("type %s: %v", short, err)
	}
	ev := seedEvent(t, typeID, date)
	if taskForce != "" {
		mustExec(t, `UPDATE schedule_events SET task_force = ? WHERE id = ?`, taskForce, ev)
	}
	res, err := db.Exec(`INSERT INTO participation_boards (schedule_event_id) VALUES (?)`, ev)
	if err != nil {
		t.Fatal(err)
	}
	board, _ := res.LastInsertId()
	rank := 1
	for id, v := range entries {
		res, err := db.Exec(`INSERT INTO participation_entries (board_id, member_id, name_snapshot, rank) VALUES (?, ?, 'x', ?)`, board, id, rank)
		if err != nil {
			t.Fatal(err)
		}
		rank++
		if v != nil {
			eid, _ := res.LastInsertId()
			mustExec(t, `INSERT INTO participation_values (entry_id, trackable_id, value) VALUES (?, ?, ?)`, eid, trackable, *v)
		}
	}
	for id, role := range roles {
		mustExec(t, `INSERT INTO participation_roles (board_id, member_id, role, task_force) VALUES (?, ?, ?, ?)`, board, id, role, taskForce)
	}
	for id, reason := range excused {
		mustExec(t, `INSERT INTO participation_exceptions (board_id, member_id, kind, reason) VALUES (?, ?, 'excused', ?)`, board, id, reason)
	}
	return ev
}

func TestParticipationGrid(t *testing.T) {
	setupParticipationTestDB(t) // 1 Alpha R4, 2 Bravo R3, 3 Charlie R3, 4 Delta EX
	mustExec(t, `INSERT INTO members (id, name, rank, joined_at) VALUES (5, 'Echo', 'R3', '2026-10-01')`)
	mg := seedGridBoard(t, "MG", "2026-09-20", "", map[int]*int64{1: i64(100)}, nil, nil)          // absent rule
	zs := seedGridBoard(t, "ZS", "2026-09-25", "", map[int]*int64{1: i64(0), 2: i64(5)}, nil, nil) // zero rule
	dsA := seedGridBoard(t, "DS", "2026-10-02", "A", map[int]*int64{3: i64(10), 4: i64(7)},        // role rule
		map[int]string{1: "starter", 2: "sub", 3: "starter"}, map[int]string{1: "away"})
	dsB := seedGridBoard(t, "DS", "2026-10-02", "B", nil, map[int]string{5: "starter"}, nil)
	seedGridBoard(t, "MG", "2026-08-01", "", map[int]*int64{1: i64(1)}, nil, nil) // out of range
	var mgType int
	db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name = 'MG'`).Scan(&mgType)
	seedEvent(t, mgType, "2026-09-27") // an occurrence nobody recorded
	mustExec(t, `INSERT INTO vs_points (member_id, week_date, monday, tuesday, wednesday, thursday, friday, saturday) VALUES
		(1, '2026-09-28', 9000000, 9000000, 9000000, 9000000, 9000000, 9000000),
		(2, '2026-09-28', 1000000, 9000000, 1000000, 9000000, 9000000, 9000000)`)

	now := mustTime(t, "2026-10-15T12:00:00Z")
	rng := reportRangeParams{From: "2026-09-01", To: "2026-10-10"}
	rep, err := runParticipationGrid(db, participationGridParams{Range: rng}, now)
	if err != nil {
		t.Fatal(err)
	}

	if len(rep.Boards) != 4 {
		t.Fatalf("boards = %+v, want the 4 recorded in range", rep.Boards)
	}
	var tfs []string
	for _, b := range rep.Boards {
		if b.TypeShort == "DS" {
			tfs = append(tfs, b.TaskForce)
		}
	}
	if len(tfs) != 2 || tfs[0] == tfs[1] {
		t.Errorf("Desert Storm task forces %v, want A and B apart", tfs)
	}

	by := map[string]gridMember{}
	for _, m := range rep.Members {
		by[m.Name] = m
	}
	status := func(name string, ev int) string { return by[name].Cells[ev].Status }
	cases := []struct {
		name string
		ev   int
		want string
	}{
		{"Alpha", mg, "present"}, {"Bravo", mg, "missed"}, {"Charlie", mg, "missed"},
		{"Echo", mg, ""},                                                     // joined after the event: no cell
		{"Delta", mg, ""},                                                    // former member: not on the roster for 'absent'
		{"Alpha", zs, "zero"}, {"Bravo", zs, "present"}, {"Charlie", zs, ""}, // absent from a 'zero' board is blameless
		{"Alpha", dsA, "excused"}, {"Bravo", dsA, ""}, {"Charlie", dsA, "present"}, // a sub without an entry: no cell
		{"Echo", dsB, "missed"},
	}
	for _, c := range cases {
		if got := status(c.name, c.ev); got != c.want {
			t.Errorf("%s on board %d = %q, want %q", c.name, c.ev, got, c.want)
		}
	}
	if a := by["Alpha"]; a.Boards != 3 || a.Missed != 1 || a.Excused != 1 || a.Cells[dsA].Reason != "away" {
		t.Errorf("Alpha %+v", a)
	}

	// The VS column is report 2's figures for the same range; a former member has none.
	vs, _ := runVSMissesReport(db, vsMissesParams{Range: rng}, now)
	for _, v := range vs.Members {
		g := by[v.Name]
		if g.VS == nil || g.VS.Misses != v.Misses || g.VS.DaysEvaluated != v.DaysEvaluated {
			t.Errorf("%s VS %+v, report %d/%d", v.Name, g.VS, v.Misses, v.DaysEvaluated)
		}
	}
	if d, ok := by["Delta"]; !ok || d.Rank != "EX" || d.VS != nil || d.Cells[dsA].Status != "present" {
		t.Errorf("Delta (EX, on a board) = %+v", d)
	}
	if by["Bravo"].VS.Misses != 2 {
		t.Errorf("Bravo VS misses %d, want 2", by["Bravo"].VS.Misses)
	}
}

func TestParticipationGridRoute(t *testing.T) {
	setupPreviewTestDB(t)
	setReportKeys(t, true, true, false)
	officer := sessionCookie(t, 3, "")
	if w := serveRouter(t, "GET", "/api/reports/participation?weeks=4", "", officer); w.Code != http.StatusForbidden {
		t.Errorf("without the key: %d", w.Code)
	}
	setReportKeys(t, false, false, true)
	if w := serveRouter(t, "GET", "/api/reports/participation?weeks=4", "", officer); w.Code != http.StatusOK {
		t.Errorf("with the key: %d %s", w.Code, w.Body)
	}
	if w := serveRouter(t, "GET", "/api/reports/participation?from=2026-10-01", "", officer); w.Code != http.StatusBadRequest {
		t.Errorf("half a range: %d", w.Code)
	}
}
