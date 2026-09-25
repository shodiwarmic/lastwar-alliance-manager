package app

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

// --- deriveBoard: the one function that decides status ---------------------------

func ip(n int) *int                            { return &n }
func i64(n int64) *int64                       { return &n }
func vals(k string, v int64) map[string]*int64 { return map[string]*int64{k: i64(v)} }

func statusOf(ss []ptStatus, id int) *ptStatus {
	for i := range ss {
		if ss[i].MemberID == id {
			return &ss[i]
		}
	}
	return nil
}

func TestDeriveBoard(t *testing.T) {
	roster := []ptRosterMember{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}, {ID: 3, Name: "C"}}

	t.Run("absent: a roster member with no entry is missed", func(t *testing.T) {
		ss := deriveBoard(ruleAbsent, "damage", roster,
			[]ptEntry{{MemberID: ip(1), Name: "A", Rank: ip(1), Values: vals("damage", 5)}}, nil, nil)
		if s := statusOf(ss, 1); s == nil || s.Status != "present" {
			t.Errorf("entered member = %+v, want present", s)
		}
		for _, id := range []int{2, 3} {
			if s := statusOf(ss, id); s == nil || s.Status != "missed" {
				t.Errorf("member %d = %+v, want missed", id, s)
			}
		}
	})

	t.Run("zero: 0 is the fault, absence is blameless, a missing value is not a zero", func(t *testing.T) {
		ss := deriveBoard(ruleZero, "waves", roster, []ptEntry{
			{MemberID: ip(1), Name: "A", Values: vals("waves", 0)},
			{MemberID: ip(2), Name: "B", Values: map[string]*int64{}},
		}, nil, nil)
		if s := statusOf(ss, 1); s == nil || s.Status != "zero" {
			t.Errorf("waves 0 = %+v, want zero", s)
		}
		if s := statusOf(ss, 2); s == nil || s.Status != "present" {
			t.Errorf("no value row = %+v, want present (decision 19)", s)
		}
		if s := statusOf(ss, 3); s != nil {
			t.Errorf("absent under zero rule = %+v, want not listed", s)
		}
	})

	t.Run("role: an absent starter is missed, an absent sub is not, a present sub is present", func(t *testing.T) {
		ss := deriveBoard(ruleRole, "points", nil,
			[]ptEntry{{MemberID: ip(3), Name: "C", Values: vals("points", 9)}},
			[]ptRole{{MemberID: 1, Role: "starter", TaskForce: "A"}, {MemberID: 2, Role: "sub"}, {MemberID: 3, Role: "sub"}}, nil)
		if s := statusOf(ss, 1); s == nil || s.Status != "missed" || s.TaskForce != "A" {
			t.Errorf("absent starter = %+v, want missed on TF A", s)
		}
		if s := statusOf(ss, 2); s != nil {
			t.Errorf("absent sub = %+v, want not listed", s)
		}
		if s := statusOf(ss, 3); s == nil || s.Status != "present" || s.Role != "sub" {
			t.Errorf("present sub = %+v, want present", s)
		}
	})

	t.Run("excused overrides missed and zero; dismissed only hides the suggestion", func(t *testing.T) {
		ss := deriveBoard(ruleAbsent, "damage", roster, nil, nil, []ptException{
			{MemberID: 1, Kind: "excused", Reason: "sick"},
			{MemberID: 2, Kind: "dismissed"},
		})
		if s := statusOf(ss, 1); s == nil || s.Status != "excused" || s.Reason != "sick" {
			t.Errorf("excused = %+v", s)
		}
		if s := statusOf(ss, 2); s == nil || s.Status != "missed" || !s.Dismissed {
			t.Errorf("dismissed = %+v, want missed + dismissed", s)
		}
		sugg := boardSuggestions(ss, "exercise_no_show", "2026-09-01", map[string]bool{
			struckKey(3, "exercise_no_show", "2026-09-01"): true,
		})
		if len(sugg) != 0 {
			t.Errorf("suggestions = %+v, want none (1 excused, 2 dismissed, 3 already struck)", sugg)
		}
		zs := deriveBoard(ruleZero, "waves", nil, []ptEntry{{MemberID: ip(1), Name: "A", Values: vals("waves", 0)}}, nil,
			[]ptException{{MemberID: 1, Kind: "excused", Reason: "shield"}})
		if s := statusOf(zs, 1); s == nil || s.Status != "excused" {
			t.Errorf("excused zero = %+v, want excused", s)
		}
	})

	t.Run("legacy missed exception stands in for a role", func(t *testing.T) {
		ss := deriveBoard(ruleRole, "points", nil, nil, nil, []ptException{{MemberID: 2, MemberName: "B", Kind: "missed"}})
		if s := statusOf(ss, 2); s == nil || s.Status != "missed" {
			t.Errorf("legacy miss = %+v", s)
		}
	})

	t.Run("roster eligibility: EX and not-yet-joined are excluded", func(t *testing.T) {
		el := eligibleRoster([]ptRosterMember{
			{ID: 1, Rank: "R3", JoinedAt: "2026-01-01"},
			{ID: 2, Rank: "EX"},
			{ID: 3, Rank: "R3", JoinedAt: "2026-09-02"},
			{ID: 4, Rank: "R3"},
		}, "2026-09-01")
		var ids []string
		for _, m := range el {
			ids = append(ids, strconv.Itoa(m.ID))
		}
		if strings.Join(ids, ",") != "1,4" {
			t.Errorf("eligible = %v, want 1,4", ids)
		}
	})
}

// --- API ------------------------------------------------------------------------

type ptFixture struct {
	mg, ls, zs, custom int
}

func setupParticipationTestDB(t *testing.T) ptFixture {
	t.Helper()
	setupSettingsTestDB(t)
	var f ptFixture
	for short, dst := range map[string]*int{"MG": &f.mg, "LS": &f.ls, "ZS": &f.zs} {
		if err := db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name = ?`, short).Scan(dst); err != nil {
			t.Fatalf("type %s: %v", short, err)
		}
	}
	res, err := db.Exec(`INSERT INTO schedule_event_types (name, short_name, icon, is_system, active, sort_order) VALUES ('City Clash','CC','🏙️',0,1,9)`)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	f.custom = int(id)
	if _, err := db.Exec(`INSERT INTO members (id, name, rank, joined_at) VALUES
		(1,'Alpha','R4','2025-01-01'),(2,'Bravo','R3','2025-01-01'),(3,'Charlie','R3',NULL),(4,'Delta','EX','2025-01-01')`); err != nil {
		t.Fatal(err)
	}
	return f
}

func seedEvent(t *testing.T, typeID int, date string) int {
	t.Helper()
	res, err := db.Exec(`INSERT INTO schedule_events (event_date, event_type_id, event_time, notes, created_by) VALUES (?, ?, '20:00', '', 1)`, date, typeID)
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func ptReq(method, path string, body any, vars map[string]string, actor *AuthUser) *http.Request {
	var r *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, strings.NewReader(string(b)))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if actor == nil {
		actor = &AuthUser{ID: 1, Username: "tester", IsAdmin: true}
	}
	r = r.WithContext(context.WithValue(r.Context(), authUserKey, actor))
	if vars != nil {
		r = mux.SetURLVars(r, vars)
	}
	return r
}

func putBoard(t *testing.T, eventID int, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	handleParticipationBoardPut(rr, ptReq(http.MethodPut, "/", body, map[string]string{"eventID": strconv.Itoa(eventID)}, nil))
	return rr
}

func entry(rank int, name string, member int, key string, v int64) map[string]any {
	e := map[string]any{"rank": rank, "name": name, "values": map[string]int64{key: v}}
	if member > 0 {
		e["member_id"] = member
	}
	return e
}

func count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func TestParticipationMigrationSeeds(t *testing.T) {
	setupSettingsTestDB(t)
	if n := count(t, `SELECT COUNT(*) FROM participation_types pt JOIN schedule_event_types t ON t.id = pt.event_type_id WHERE t.short_name IN ('MG','LS','ZS')`); n != 3 {
		t.Errorf("tracked types = %d, want MG, LS and ZS", n)
	}
	if n := count(t, `SELECT COUNT(*) FROM participation_types pt JOIN schedule_event_types t ON t.id = pt.event_type_id WHERE t.short_name = 'SP'`); n != 0 {
		t.Error("Sky Predator must not track participation")
	}
	if n := count(t, `SELECT COUNT(*) FROM strike_types WHERE key IN ('exercise_no_show','zs_no_defense') AND is_system = 1 AND active = 1`); n != 2 {
		t.Errorf("seeded strike categories = %d, want 2 system rows", n)
	}
	for _, rank := range []string{"R4", "R5"} {
		p := getRankPermissions(rank)
		if !p.ViewParticipation || !p.ManageParticipation {
			t.Errorf("%s participation permissions = %v/%v, want both", rank, p.ViewParticipation, p.ManageParticipation)
		}
	}
	for _, rank := range []string{"R1", "R2", "R3"} {
		if p := getRankPermissions(rank); p.ViewParticipation || p.ManageParticipation {
			t.Errorf("%s holds a participation permission it was not granted", rank)
		}
	}
}

func TestParticipationBoardPutValidates(t *testing.T) {
	f := setupParticipationTestDB(t)
	past := seedEvent(t, f.mg, "2026-09-01")
	future := seedEvent(t, f.mg, time.Now().AddDate(0, 0, 10).Format("2006-01-02"))
	custom := seedEvent(t, f.custom, "2026-09-01")

	for _, tc := range []struct {
		name  string
		event int
		body  map[string]any
		want  string
	}{
		{"future date", future, map[string]any{"entries": []any{}}, "has not happened yet"},
		{"untracked type", custom, map[string]any{"entries": []any{}}, "does not track participation"},
		{"duplicate rank", past, map[string]any{"entries": []any{entry(1, "A", 1, "damage", 5), entry(1, "B", 2, "damage", 4)}}, "Rank 1 appears twice"},
		{"duplicate member", past, map[string]any{"entries": []any{entry(1, "A", 1, "damage", 5), entry(2, "A2", 1, "damage", 4)}}, "already on the board"},
		{"missing value", past, map[string]any{"entries": []any{map[string]any{"rank": 1, "name": "A", "member_id": 1, "values": map[string]any{}}}}, "needs a value for Total Damage"},
		{"unknown value", past, map[string]any{"entries": []any{map[string]any{"rank": 1, "name": "A", "values": map[string]any{"damage": 1, "waves": 2}}}}, "unknown value"},
		{"roles on a non-role type", past, map[string]any{"entries": []any{}, "roles": []any{map[string]any{"member_id": 1, "role": "starter"}}}, "does not use roles"},
		{"deleted member", past, map[string]any{"entries": []any{entry(1, "Ghost", 99, "damage", 5)}}, "no longer exists"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := putBoard(t, tc.event, tc.body)
			if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), tc.want) {
				t.Errorf("= %d %q, want 400 containing %q", rr.Code, rr.Body.String(), tc.want)
			}
		})
	}
	if n := count(t, `SELECT COUNT(*) FROM participation_boards`); n != 0 {
		t.Errorf("%d boards written by rejected requests", n)
	}
}

func TestParticipationBoardPutReplacesWithoutOrphans(t *testing.T) {
	f := setupParticipationTestDB(t)
	ev := seedEvent(t, f.mg, "2026-09-01")
	first := putBoard(t, ev, map[string]any{"entries": []any{
		entry(1, "Alpha", 1, "damage", 100), entry(2, "Bravo", 2, "damage", 50), entry(3, "Stranger", 0, "damage", 10),
	}})
	if first.Code != http.StatusOK {
		t.Fatalf("first PUT = %d %s", first.Code, first.Body.String())
	}
	var d ptBoardDetail
	json.Unmarshal(first.Body.Bytes(), &d)
	// Charlie (joined NULL, R3) is the only eligible member not on the board; Delta is EX.
	if len(d.Suggestions) != 1 || d.Suggestions[0].MemberID != 3 {
		t.Errorf("suggestions = %+v, want only Charlie", d.Suggestions)
	}

	second := putBoard(t, ev, map[string]any{"entries": []any{entry(1, "Alpha", 1, "damage", 120)}})
	if second.Code != http.StatusOK {
		t.Fatalf("second PUT = %d %s", second.Code, second.Body.String())
	}
	if n := count(t, `SELECT COUNT(*) FROM participation_entries`); n != 1 {
		t.Errorf("entries after replace = %d, want 1", n)
	}
	if n := count(t, `SELECT COUNT(*) FROM participation_values`); n != 1 {
		t.Errorf("values after replace = %d, want 1 (entries × trackables, no orphans)", n)
	}
	if n := count(t, `SELECT COUNT(*) FROM participation_boards`); n != 1 {
		t.Errorf("boards = %d, want 1", n)
	}
}

func TestParticipationOccurrenceRunsTheScheduleValidator(t *testing.T) {
	f := setupParticipationTestDB(t)
	seedEvent(t, f.mg, "2026-09-01")

	body := map[string]any{"event_type_id": f.mg, "event_date": "2026-09-02", "event_time": "20:00"}
	sched := httptest.NewRecorder()
	createScheduleEvent(sched, ptReq(http.MethodPost, "/", body, nil, nil))
	occ := httptest.NewRecorder()
	handleParticipationOccurrence(occ, ptReq(http.MethodPost, "/", body, nil, nil))
	if occ.Code != http.StatusBadRequest || occ.Body.String() != sched.Body.String() {
		t.Errorf("occurrence = %d %q, want the Schedule page's own %d %q", occ.Code, occ.Body.String(), sched.Code, sched.Body.String())
	}

	ok := httptest.NewRecorder()
	handleParticipationOccurrence(ok, ptReq(http.MethodPost, "/", map[string]any{"event_type_id": f.mg, "event_date": "2026-09-03", "event_time": "20:00"}, nil, nil))
	if ok.Code != http.StatusCreated {
		t.Errorf("legal occurrence = %d %q", ok.Code, ok.Body.String())
	}
	bad := httptest.NewRecorder()
	handleParticipationOccurrence(bad, ptReq(http.MethodPost, "/", map[string]any{"event_type_id": f.custom, "event_date": "2026-09-03", "event_time": "20:00"}, nil, nil))
	if bad.Code != http.StatusBadRequest {
		t.Errorf("untracked occurrence = %d, want 400", bad.Code)
	}
}

func TestParticipationStrikeConfirmIsIdempotent(t *testing.T) {
	f := setupParticipationTestDB(t)
	ev := seedEvent(t, f.zs, "2026-09-01")
	if rr := putBoard(t, ev, map[string]any{"entries": []any{entry(1, "Alpha", 1, "waves", 0), entry(2, "Bravo", 2, "waves", 7)}}); rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rr.Code, rr.Body.String())
	}
	confirm := func(body map[string]any) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		handleParticipationStrikes(rr, ptReq(http.MethodPost, "/", body, map[string]string{"eventID": strconv.Itoa(ev)}, nil))
		return rr
	}
	if rr := confirm(map[string]any{"member_id": 1}); rr.Code != http.StatusOK {
		t.Fatalf("first confirm = %d %s", rr.Code, rr.Body.String())
	}
	if rr := confirm(map[string]any{"member_id": 1}); rr.Code != http.StatusConflict {
		t.Errorf("second confirm = %d, want 409", rr.Code)
	}
	if rr := confirm(map[string]any{"all": true}); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"created":0`) {
		t.Errorf("confirm all after = %d %s, want nothing left", rr.Code, rr.Body.String())
	}
	var key, ref, reason string
	if err := db.QueryRow(`SELECT strike_type, ref_date, reason FROM accountability_strikes WHERE member_id = 1`).Scan(&key, &ref, &reason); err != nil {
		t.Fatal(err)
	}
	if key != "zs_no_defense" || !strings.HasPrefix(ref, "2026-09-01") || !strings.Contains(reason, "0 waves") {
		t.Errorf("strike = %s %s %q", key, ref, reason)
	}
	if n := count(t, `SELECT COUNT(*) FROM accountability_strikes`); n != 1 {
		t.Errorf("strikes = %d, want 1 (Bravo defended)", n)
	}
}

func TestParticipationExceptions(t *testing.T) {
	f := setupParticipationTestDB(t)
	ev := seedEvent(t, f.mg, "2026-09-01")
	putBoard(t, ev, map[string]any{"entries": []any{entry(1, "Alpha", 1, "damage", 1)}})
	vars := map[string]string{"eventID": strconv.Itoa(ev)}
	post := func(body map[string]any) int {
		rr := httptest.NewRecorder()
		handleParticipationException(rr, ptReq(http.MethodPost, "/", body, vars, nil))
		return rr.Code
	}
	if c := post(map[string]any{"member_id": 2, "kind": "excused"}); c != http.StatusBadRequest {
		t.Errorf("excuse without reason = %d, want 400", c)
	}
	if c := post(map[string]any{"member_id": 2, "kind": "excused", "reason": "travelling"}); c != http.StatusNoContent {
		t.Errorf("excuse = %d", c)
	}
	if c := post(map[string]any{"member_id": 3, "kind": "dismissed"}); c != http.StatusNoContent {
		t.Errorf("dismiss = %d", c)
	}
	d, _ := buildBoardDetail(db, ev)
	if len(d.Suggestions) != 0 {
		t.Errorf("suggestions = %+v, want none after excuse + dismiss", d.Suggestions)
	}
	// A re-save keeps both judgements.
	putBoard(t, ev, map[string]any{"entries": []any{entry(1, "Alpha", 1, "damage", 2)}})
	if n := count(t, `SELECT COUNT(*) FROM participation_exceptions`); n != 2 {
		t.Errorf("exceptions after re-save = %d, want 2", n)
	}
}

func TestScheduleEventWithBoardIsProtected(t *testing.T) {
	f := setupParticipationTestDB(t)
	ev := seedEvent(t, f.mg, "2026-09-01")
	putBoard(t, ev, map[string]any{"entries": []any{entry(1, "Alpha", 1, "damage", 1)}})
	vars := map[string]string{"id": strconv.Itoa(ev)}

	rr := httptest.NewRecorder()
	deleteScheduleEvent(rr, ptReq(http.MethodDelete, "/", nil, vars, nil))
	if rr.Code != http.StatusConflict {
		t.Errorf("delete boarded event = %d, want 409", rr.Code)
	}
	rr = httptest.NewRecorder()
	updateScheduleEvent(rr, ptReq(http.MethodPut, "/", map[string]any{"event_type_id": f.zs, "event_time": "20:00"}, vars, nil))
	if rr.Code != http.StatusConflict {
		t.Errorf("retype boarded event = %d %s, want 409", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	updateScheduleEvent(rr, ptReq(http.MethodPut, "/", map[string]any{"event_type_id": f.mg, "event_time": "20:00", "notes": "late start"}, vars, nil))
	if rr.Code != http.StatusNoContent {
		t.Errorf("edit notes on boarded event = %d %s, want 204", rr.Code, rr.Body.String())
	}
}

func TestSeasonDeleteKeepsBoardedEvents(t *testing.T) {
	setupSettingsTestDB(t)
	s, _, _ := seedPushableSeason(t, "2026-09-07")
	if _, err := pushSeasonEventsToSchedule(s, 1, "tester"); err != nil {
		t.Fatalf("push: %v", err)
	}
	var boarded int
	db.QueryRow(`SELECT id FROM schedule_events ORDER BY event_date LIMIT 1`).Scan(&boarded)
	if _, err := db.Exec(`INSERT INTO participation_boards (schedule_event_id, recorded_by) VALUES (?, 1)`, boarded); err != nil {
		t.Fatal(err)
	}
	db.Exec(`UPDATE seasons SET is_active = 0 WHERE id = 1`)
	rr := httptest.NewRecorder()
	handleSeasonDelete(rr, ptReq(http.MethodDelete, "/", nil, map[string]string{"id": "1"}, nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"kept_with_boards":1`) {
		t.Fatalf("season delete = %d %s", rr.Code, rr.Body.String())
	}
	var sid any
	if err := db.QueryRow(`SELECT season_event_id FROM schedule_events WHERE id = ?`, boarded).Scan(&sid); err != nil {
		t.Fatalf("boarded event was deleted: %v", err)
	}
	if sid != nil {
		t.Errorf("boarded event still stamped %v, want detached", sid)
	}
	if n := count(t, `SELECT COUNT(*) FROM schedule_events`); n != 1 {
		t.Errorf("schedule_events left = %d, want only the boarded one", n)
	}
}

func TestDeleteMemberClearsParticipationInOneTransaction(t *testing.T) {
	f := setupParticipationTestDB(t)
	ds := seedEvent(t, f.mg, "2026-09-01")
	other := seedEvent(t, f.mg, "2026-09-03")
	// Bravo's linked account recorded the other board.
	res, _ := db.Exec(`INSERT INTO users (username, password, member_id) VALUES ('bravo', 'x', 2)`)
	uid, _ := res.LastInsertId()
	putBoard(t, ds, map[string]any{"entries": []any{entry(1, "Bravo", 2, "damage", 5)}})
	putBoard(t, other, map[string]any{"entries": []any{entry(1, "Alpha", 1, "damage", 5)}})
	db.Exec(`UPDATE participation_boards SET recorded_by = ? WHERE schedule_event_id = ?`, uid, other)
	var board int
	db.QueryRow(`SELECT id FROM participation_boards WHERE schedule_event_id = ?`, ds).Scan(&board)
	db.Exec(`INSERT INTO participation_roles (board_id, member_id, role) VALUES (?, 2, 'starter')`, board)
	db.Exec(`INSERT INTO participation_exceptions (board_id, member_id, kind, reason, recorded_by) VALUES (?, 2, 'excused', 'x', ?)`, board, uid)

	// A failing child delete leaves the member whole.
	db.Exec(`CREATE TRIGGER fail_roles BEFORE DELETE ON participation_roles BEGIN SELECT RAISE(ABORT, 'injected'); END`)
	rr := httptest.NewRecorder()
	deleteMember(rr, ptReq(http.MethodDelete, "/", nil, map[string]string{"id": "2"}, nil))
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("delete with failing child = %d, want 500", rr.Code)
	}
	if n := count(t, `SELECT COUNT(*) FROM members WHERE id = 2`); n != 1 {
		t.Error("member deleted despite a failed child delete")
	}
	if n := count(t, `SELECT COUNT(*) FROM participation_entries WHERE member_id = 2`); n != 1 {
		t.Error("entries deleted despite the rollback")
	}

	db.Exec(`DROP TRIGGER fail_roles`)
	rr = httptest.NewRecorder()
	deleteMember(rr, ptReq(http.MethodDelete, "/", nil, map[string]string{"id": "2"}, nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", rr.Code, rr.Body.String())
	}
	for _, tbl := range []string{"participation_entries", "participation_roles", "participation_exceptions"} {
		if n := count(t, `SELECT COUNT(*) FROM `+tbl+` WHERE member_id = 2`); n != 0 {
			t.Errorf("%s: %d orphan rows", tbl, n)
		}
	}
	if n := count(t, `SELECT COUNT(*) FROM participation_values WHERE entry_id NOT IN (SELECT id FROM participation_entries)`); n != 0 {
		t.Errorf("%d orphan values", n)
	}
	if n := count(t, `SELECT COUNT(*) FROM participation_boards WHERE recorded_by = ?`, uid); n != 0 {
		t.Error("the other board still points at the deleted user")
	}
	if n := count(t, `SELECT COUNT(*) FROM users WHERE id = ?`, uid); n != 0 {
		t.Error("linked user survived")
	}
}

func TestParticipationReadGates(t *testing.T) {
	f := setupParticipationTestDB(t)
	ev := seedEvent(t, f.mg, "2026-09-01")
	db.Exec(`UPDATE rank_permissions SET permissions = json_set(permissions, '$.manage_participation', json('true'), '$.view_participation', json('false')) WHERE rank = 'R3'`)
	db.Exec(`UPDATE rank_permissions SET permissions = json_set(permissions, '$.manage_participation', json('false'), '$.view_participation', json('false'), '$.view_accountability', json('false')) WHERE rank = 'R2'`)
	vars := map[string]string{"eventID": strconv.Itoa(ev)}

	manageOnly := &AuthUser{ID: 5, Username: "r3", MemberID: ip(2), Rank: "R3"}
	rr := httptest.NewRecorder()
	handleParticipationBoard(rr, ptReq(http.MethodGet, "/", nil, vars, manageOnly))
	if rr.Code != http.StatusOK {
		t.Errorf("manage-only read = %d, want 200 (manage does not imply view, so the gate takes either)", rr.Code)
	}
	neither := &AuthUser{ID: 6, Username: "r2", MemberID: ip(3), Rank: "R2"}
	rr = httptest.NewRecorder()
	handleParticipationBoard(rr, ptReq(http.MethodGet, "/", nil, vars, neither))
	if rr.Code != http.StatusForbidden {
		t.Errorf("no-permission read = %d, want 403", rr.Code)
	}
	// Their own history needs nothing.
	rr = httptest.NewRecorder()
	handleParticipationMe(rr, ptReq(http.MethodGet, "/", nil, nil, neither))
	if rr.Code != http.StatusOK {
		t.Errorf("/me for a linked member = %d, want 200", rr.Code)
	}
	rr = httptest.NewRecorder()
	handleParticipationMe(rr, ptReq(http.MethodGet, "/", nil, nil, &AuthUser{ID: 1, Username: "admin", IsAdmin: true}))
	if rr.Code != http.StatusNotFound {
		t.Errorf("/me for an unlinked account = %d, want 404", rr.Code)
	}
}

func TestParticipationBoardsAreNeverBatched(t *testing.T) {
	f := setupParticipationTestDB(t)
	a := seedEvent(t, f.mg, "2026-09-01")
	b := seedEvent(t, f.mg, "2026-09-03")
	putBoard(t, a, map[string]any{"entries": []any{entry(1, "Alpha", 1, "damage", 1)}})
	putBoard(t, b, map[string]any{"entries": []any{entry(1, "Alpha", 1, "damage", 1)}})
	if n := count(t, `SELECT COUNT(*) FROM activity_log WHERE entity_type = 'participation_board' AND action = 'created'`); n != 2 {
		t.Errorf("activity rows = %d, want one per board", n)
	}
}

func csvUpload(t *testing.T, eventID int, content string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("csv_file", "board.csv")
	fw.Write([]byte(content))
	mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r = r.WithContext(context.WithValue(r.Context(), authUserKey, &AuthUser{ID: 1, Username: "tester", IsAdmin: true}))
	r = mux.SetURLVars(r, map[string]string{"eventID": strconv.Itoa(eventID)})
	rr := httptest.NewRecorder()
	handleParticipationCSV(rr, r)
	return rr
}

// The CSV import reads and matches a board but saves nothing: its rows go to the
// check table and are saved through the ordinary PUT.
func TestParticipationCSVImport(t *testing.T) {
	f := setupParticipationTestDB(t)
	ev := seedEvent(t, f.mg, "2026-09-01")

	// BOM (our own exports start with one), a label header, suffixed and comma-grouped
	// scores, a tagged name, an accent-folded match, ranks out of order, a duplicate
	// match, an unreadable score and an unknown name.
	rr := csvUpload(t, ev, "\ufeffRank,Member,Total Damage\n"+
		"3,[PoWr] Bravo,\"1,234,567\"\n"+
		"1,alpha,81.20G\n"+
		"2,Chárlie,392.35M\n"+
		"4,Bravo,5\n"+
		"5,Nobody,abc\n")
	if rr.Code != http.StatusOK {
		t.Fatalf("csv = %d %s", rr.Code, rr.Body.String())
	}
	var out struct {
		Rows     []ptCSVRow     `json:"rows"`
		Problems []ptCSVProblem `json:"problems"`
	}
	json.Unmarshal(rr.Body.Bytes(), &out)
	if len(out.Rows) != 5 {
		t.Fatalf("rows = %d, want 5: %+v", len(out.Rows), out.Rows)
	}
	want := []struct {
		name   string
		member int
		value  int64
	}{{"alpha", 1, 81200000000}, {"Chárlie", 3, 392350000}, {"Bravo", 2, 1234567}, {"Bravo", 0, 5}, {"Nobody", 0, -1}}
	for i, w := range want {
		r := out.Rows[i]
		if r.Name != w.name {
			t.Errorf("row %d name = %q, want %q (ordered by rank, tag stripped)", i, r.Name, w.name)
		}
		got := 0
		if r.MemberID != nil {
			got = *r.MemberID
		}
		if got != w.member {
			t.Errorf("row %d (%s) member = %d, want %d", i, r.Name, got, w.member)
		}
		v := r.Values["damage"]
		if w.value < 0 {
			if v != nil {
				t.Errorf("row %d value = %d, want nil for an unreadable score", i, *v)
			}
		} else if v == nil || *v != w.value {
			t.Errorf("row %d value = %v, want %d", i, v, w.value)
		}
	}
	if len(out.Problems) != 2 {
		t.Errorf("problems = %+v, want the duplicate match and the unreadable score", out.Problems)
	}
	if n := count(t, `SELECT COUNT(*) FROM participation_boards`); n != 0 {
		t.Errorf("the import saved %d boards; it must save nothing", n)
	}

	// A missing required column fails the whole file, before any row is read.
	if rr := csvUpload(t, ev, "Name,Points\nAlpha,5\n"); rr.Code != http.StatusOK {
		t.Errorf("single-trackable type should accept Points as the value column: %d %s", rr.Code, rr.Body.String())
	}
	if rr := csvUpload(t, ev, "Name,Rank\nAlpha,1\n"); rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "Total Damage") {
		t.Errorf("missing value column = %d %q", rr.Code, rr.Body.String())
	}
	if rr := csvUpload(t, ev, "Player Name,Damage\nAlpha,5\n"); rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "Name") {
		t.Errorf("missing name column = %d %q", rr.Code, rr.Body.String())
	}
}
