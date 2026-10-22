package app

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/pressly/goose/v3"
)

func attendanceRequest(ev, member int, kind string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	method, path := http.MethodPost, "/"
	var body any = map[string]any{"member_id": member, "kind": kind, "reason": "away"}
	if kind == "undo" {
		method, path, body = http.MethodDelete, "/?member_id="+strconv.Itoa(member), nil
	}
	handleParticipationException(rr, ptReq(method, path, body, map[string]string{"eventID": strconv.Itoa(ev)}, nil))
	return rr
}

func seedAttendanceBoard(t *testing.T) int {
	t.Helper()
	setupParticipationTestDB(t)
	ev := seedEvent(t, dsTypeID(t), "2026-09-25")
	if _, err := db.Exec(`UPDATE schedule_events SET task_force = 'A' WHERE id = ?`, ev); err != nil {
		t.Fatal(err)
	}
	rr := putBoard(t, ev, map[string]any{
		"entries": []any{entry(1, "Alpha", 1, "points", 100), entry(2, "Charlie", 3, "points", 50)},
		"roles":   []any{map[string]any{"member_id": 1, "role": "starter", "task_force": "A"}, map[string]any{"member_id": 2, "role": "sub", "task_force": "A"}, map[string]any{"member_id": 3, "role": "sub", "task_force": "A"}, map[string]any{"member_id": 4, "role": "starter", "task_force": "A"}},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("save = %d %s", rr.Code, rr.Body.String())
	}
	return ev
}

func TestDSManualSubMissWorkflow(t *testing.T) {
	ev := seedAttendanceBoard(t)
	check := func(want string, dismissed bool, suggested bool) {
		t.Helper()
		d, err := buildBoardDetail(db, ev)
		if err != nil {
			t.Fatal(err)
		}
		s := statusOf(d.Statuses, 2)
		if want == "" {
			if s != nil {
				t.Fatalf("absent sub = %+v", s)
			}
			return
		}
		if s == nil || s.Status != want || s.Role != "sub" || s.TaskForce != "A" || s.Dismissed != dismissed {
			t.Fatalf("sub = %+v", s)
		}
		if (statusOf(d.Suggestions, 2) != nil) != suggested {
			t.Fatalf("suggestions = %+v", d.Suggestions)
		}
	}
	request := func(kind string) {
		t.Helper()
		rr := attendanceRequest(ev, 2, kind)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("%s = %d %s", kind, rr.Code, rr.Body.String())
		}
	}
	check("", false, false)
	request("missed")
	check("missed", false, true)
	if n := count(t, `SELECT COUNT(*) FROM accountability_strikes`); n != 0 {
		t.Fatal("mark created a strike")
	}
	request("dismissed")
	check("missed", true, false)
	request("undo")
	check("missed", false, true)
	request("excused")
	check("excused", false, false)
	request("undo")
	check("missed", false, true)
	// Saving the board keeps the officer's judgement.
	if rr := putBoard(t, ev, map[string]any{"entries": []any{entry(1, "Alpha", 1, "points", 100)}, "roles": []any{map[string]any{"member_id": 2, "role": "sub", "task_force": "A"}}}); rr.Code != http.StatusOK {
		t.Fatal(rr.Body.String())
	}
	check("missed", false, true)
	request("undo")
	check("", false, false)
	request("missed")
	rr := httptest.NewRecorder()
	handleParticipationStrikes(rr, ptReq(http.MethodPost, "/", map[string]any{"member_id": 2}, map[string]string{"eventID": strconv.Itoa(ev)}, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("confirm = %d %s", rr.Code, rr.Body.String())
	}
	if count(t, `SELECT COUNT(*) FROM accountability_strikes WHERE member_id = 2 AND strike_type = 'storm_no_show'`) != 1 {
		t.Fatal("missing DS strike")
	}
	check("missed", false, false)
}

func TestDSLateWorkflow(t *testing.T) {
	ev := seedAttendanceBoard(t)
	for _, id := range []int{1, 3} {
		rr := attendanceRequest(ev, id, "late")
		if rr.Code != http.StatusNoContent {
			t.Fatalf("late = %d %s", rr.Code, rr.Body.String())
		}
	}
	d, err := buildBoardDetail(db, ev)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{1, 3} {
		s := statusOf(d.Statuses, id)
		if s == nil || s.Status != "late" || s.Rank == nil || s.Values["points"] == nil || s.Role == "" || s.TaskForce != "A" {
			t.Fatalf("late = %+v", s)
		}
		if statusOf(d.Suggestions, id) != nil {
			t.Fatal("late suggested for strike")
		}
	}
	h, err := memberParticipation(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Rows) != 1 || h.Rows[0].Status != "late" || h.Rows[0].Rank == nil || h.Rows[0].Score == nil || h.Rows[0].Role != "sub" || h.Rows[0].TaskForce != "A" || h.Missed != 0 {
		t.Fatalf("late history = %+v", h)
	}
	if d.Statuses[0].MemberID != 1 || d.Statuses[1].MemberID != 3 {
		t.Fatalf("lost ranked ordering: %+v", d.Statuses)
	}
	rr := attendanceRequest(ev, 1, "undo")
	if rr.Code != http.StatusNoContent {
		t.Fatal(rr.Body.String())
	}
	d, err = buildBoardDetail(db, ev)
	if err != nil {
		t.Fatal(err)
	}
	if s := statusOf(d.Statuses, 1); s == nil || s.Status != "present" {
		t.Fatalf("undo late = %+v", s)
	}
	// A stale late mark never invents participation after the result is removed.
	if _, err := db.Exec(`DELETE FROM participation_entries WHERE member_id = 3`); err != nil {
		t.Fatal(err)
	}
	d, err = buildBoardDetail(db, ev)
	if err != nil {
		t.Fatal(err)
	}
	if s := statusOf(d.Statuses, 3); s != nil {
		t.Fatalf("absent late sub = %+v", s)
	}
}

func TestDSAttendanceValidation(t *testing.T) {
	ev := seedAttendanceBoard(t)
	for _, tc := range []struct {
		member int
		kind   string
	}{{1, "missed"}, {3, "missed"}, {4, "missed"}, {99, "missed"}, {2, "late"}, {4, "late"}, {99, "late"}} {
		rr := attendanceRequest(ev, tc.member, tc.kind)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%+v = %d %s", tc, rr.Code, rr.Body.String())
		}
	}
	// A roster member who is absent but not in the saved lineup is also ineligible.
	if _, err := db.Exec(`DELETE FROM participation_roles WHERE member_id = 2`); err != nil {
		t.Fatal(err)
	}
	if rr := attendanceRequest(ev, 2, "missed"); rr.Code != http.StatusBadRequest {
		t.Fatal("accepted missing snapshot")
	}
	for _, short := range []string{"MG", "ZS"} {
		var typ int
		if err := db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name = ?`, short).Scan(&typ); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE schedule_events SET event_type_id = ? WHERE id = ?`, typ, ev); err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"late", "missed"} {
			if rr := attendanceRequest(ev, 1, kind); rr.Code != http.StatusBadRequest {
				t.Fatalf("%s %s = %d", short, kind, rr.Code)
			}
		}
	}
	if _, err := db.Exec(`UPDATE schedule_events SET event_type_id = ? WHERE id = ?`, dsTypeID(t), ev); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE participation_types SET absence_rule = 'absent' WHERE event_type_id = ?`, dsTypeID(t)); err != nil {
		t.Fatal(err)
	}
	if rr := attendanceRequest(ev, 1, "late"); rr.Code != http.StatusBadRequest {
		t.Fatal("accepted wrong DS rule")
	}
}

func TestDSLegacyMissRemainsProtected(t *testing.T) {
	ev := seedAttendanceBoard(t)
	if _, err := db.Exec(`UPDATE participation_boards SET source = 'legacy' WHERE schedule_event_id = ?`, ev); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO participation_exceptions (board_id,member_id,kind) SELECT id,2,'missed' FROM participation_boards WHERE schedule_event_id = ?`, ev); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"missed", "late", "dismissed", "undo"} {
		rr := attendanceRequest(ev, 2, kind)
		if rr.Code != http.StatusBadRequest && rr.Code != http.StatusConflict {
			t.Fatalf("legacy %s = %d", kind, rr.Code)
		}
	}
	for _, kind := range []string{"excused", "undo"} {
		rr := attendanceRequest(ev, 2, kind)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("legacy %s = %d %s", kind, rr.Code, rr.Body.String())
		}
	}
	if rr := attendanceRequest(ev, 2, "undo"); rr.Code != http.StatusConflict {
		t.Fatal("restored legacy miss became removable")
	}
}

func TestAttendanceMigrationPreservesRowsAndForeignKeys(t *testing.T) {
	conn := migrateTo(t, 84)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`PRAGMA foreign_keys = ON`)
	exec(`INSERT INTO users (id,username,password) VALUES (7,'officer','x'),(8,'scheduler','x')`)
	exec(`INSERT INTO members (id,name,rank) VALUES (1,'A','R3'),(2,'B','R3'),(3,'C','R3'),(4,'D','R3')`)
	exec(`INSERT INTO schedule_events (id,event_type_id,event_date,event_time,created_by) SELECT 1,id,'2026-09-25','18:00',8 FROM schedule_event_types WHERE short_name='DS'`)
	exec(`INSERT INTO participation_boards (id,schedule_event_id,source) VALUES (1,1,'legacy')`)
	for i, kind := range []string{"missed", "excused", "dismissed"} {
		exec(`INSERT INTO participation_exceptions (id,board_id,member_id,kind,reason,recorded_by,created_at) VALUES (?,1,?,?, 'original',7,'2026-09-25 18:01:00')`, i+1, i+1, kind)
	}
	if err := goose.UpTo(conn, "migrations", 85); err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM participation_exceptions WHERE id=member_id AND reason='original' AND recorded_by=7 AND created_at='2026-09-25 18:01:00'`).Scan(&n); err != nil || n != 3 {
			t.Fatalf("preserved = %d, %v", n, err)
		}
	}
	check()
	exec(`INSERT INTO participation_exceptions (board_id,member_id,kind) VALUES (1,4,'late')`)
	if err := goose.Down(conn, "migrations"); err != nil {
		t.Fatal(err)
	}
	check()
	if _, err := conn.Exec(`INSERT INTO participation_exceptions (board_id,member_id,kind) VALUES (1,4,'late')`); err == nil {
		t.Fatal("Down still accepts late")
	}
	if err := goose.UpTo(conn, "migrations", 85); err != nil {
		t.Fatal(err)
	}
	exec(`DELETE FROM users WHERE id=7`)
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM participation_exceptions WHERE recorded_by IS NULL`).Scan(&n); err != nil || n != 3 {
		t.Fatal("lost SET NULL action", n, err)
	}
	exec(`DELETE FROM members WHERE id=1`)
	if err := conn.QueryRow(`SELECT COUNT(*) FROM participation_exceptions`).Scan(&n); err != nil || n != 2 {
		t.Fatal("lost member cascade", n, err)
	}
	exec(`DELETE FROM participation_boards WHERE id=1`)
	if err := conn.QueryRow(`SELECT COUNT(*) FROM participation_exceptions`).Scan(&n); err != nil || n != 0 {
		t.Fatal("lost board cascade", n, err)
	}
	rows, err := conn.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key violation")
	}
}

func TestAttendanceManagementPermission(t *testing.T) {
	ev := seedAttendanceBoard(t)
	if _, err := db.Exec(`UPDATE rank_permissions SET permissions = json_set(permissions, '$.manage_participation', json('false')) WHERE rank='R2'`); err != nil {
		t.Fatal(err)
	}
	actor := &AuthUser{ID: 5, Username: "viewer", Rank: "R2"}
	for _, kind := range []string{"missed", "late"} {
		rr := httptest.NewRecorder()
		requirePermission("manage_participation", handleParticipationException)(rr, ptReq(http.MethodPost, "/", map[string]any{"member_id": 2, "kind": kind}, map[string]string{"eventID": strconv.Itoa(ev)}, actor))
		if rr.Code != http.StatusForbidden {
			t.Fatalf("viewer %s = %d", kind, rr.Code)
		}
	}
}

func TestDeriveAttendanceMarks(t *testing.T) {
	entries := []ptEntry{{MemberID: ip(1), Name: "A", Rank: ip(2), Values: vals("points", 10)}}
	roles := []ptRole{{MemberID: 1, Role: "sub", TaskForce: "B"}, {MemberID: 2, Role: "sub", TaskForce: "B"}}
	for _, rule := range []string{ruleRole, ruleAbsent, ruleZero} {
		ss := deriveBoard(rule, "points", nil, entries, roles, []ptException{{MemberID: 1, Kind: "late"}, {MemberID: 2, Kind: "late"}})
		want := "present"
		if rule == ruleRole {
			want = "late"
		}
		if s := statusOf(ss, 1); s == nil || s.Status != want || s.Rank == nil || *s.Rank != 2 || s.Values["points"] == nil || *s.Values["points"] != 10 || s.Role != "sub" || s.TaskForce != "B" {
			t.Fatalf("%s late entry = %+v", rule, s)
		}
		if s := statusOf(ss, 2); s != nil {
			t.Fatalf("%s late manufactured attendance: %+v", rule, s)
		}
	}
	ss := deriveBoard(ruleZero, "points", nil, []ptEntry{{MemberID: ip(1), Values: vals("points", 0)}}, nil, []ptException{{MemberID: 1, Kind: "late"}})
	if s := statusOf(ss, 1); s == nil || s.Status != "zero" {
		t.Fatalf("late changed zero rule: %+v", s)
	}
	ss = deriveBoard(ruleRole, "points", nil, entries, roles, []ptException{{MemberID: 1, Kind: "missed", ManualMissed: true}})
	if s := statusOf(ss, 1); s == nil || s.Status != "present" {
		t.Fatalf("miss overrode participation: %+v", s)
	}
}
