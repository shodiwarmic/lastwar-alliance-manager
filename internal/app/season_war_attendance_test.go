package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// seedAttendanceSeason: a season started on 2026-09-07 (so its first weeks have passed) with three
// members and two completed wars in week 2.
func seedAttendanceSeason(t *testing.T) {
	t.Helper()
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	seedSeasonRow(t, 1, 4, "2026-09-07", true, false)
	mustExecT(t, `UPDATE seasons SET key_event_total = 8 WHERE id = 1`)
	mustExecT(t, `INSERT INTO season_score_levels (season_id, key, label, points, sort_order) VALUES (1, 'full', 'Full', 2, 0), (1, 'absent', 'Absent', 0, 1)`)
	for i, n := range []string{"Alpha", "Bravo", "Charlie"} {
		mustExecT(t, `INSERT INTO members (id, name, rank) VALUES (?, ?, 'R3')`, i+1, n)
	}
	mustExecT(t, `INSERT INTO season_wars (id, season_id, week_number, role, battle_date, status, outcome, poll_held) VALUES
		(1, 1, 2, 'attack', '2026-09-17', 'completed', 'win', 1), (2, 1, 2, 'defense', '2026-09-19', 'completed', 'loss', 1)`)
}

func TestWarAttendanceWeekSave(t *testing.T) {
	seedAttendanceSeason(t)
	save := func(body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		handleParticipationSave(rr, shReq(http.MethodPut, body, nil, nil))
		return rr
	}
	rr := save(`{"season_id":1,"week_number":2,"entries":[
		{"member_id":1,"score":"full","wars":[{"war_id":1,"attended":true,"poll_response":"yes"},{"war_id":2,"attended":true,"poll_response":"no"}]},
		{"member_id":2,"score":"full","wars":[{"war_id":1,"attended":false,"poll_response":"yes","note":"voted yes, no show"},{"war_id":2,"poll_response":"none"}]},
		{"member_id":3,"score":"absent","wars":[]}]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rr.Code, rr.Body.String())
	}
	// Unknown stays NULL, and an empty line writes nothing.
	if n := countT(t, `SELECT COUNT(*) FROM season_war_attendance WHERE member_id = 2 AND war_id = 2`); n != 0 {
		t.Error("an empty line was stored")
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_war_attendance WHERE member_id = 2 AND war_id = 1 AND attended = 0`); n != 1 {
		t.Error("a no was not stored as 0")
	}
	// A war that is not this season's is refused.
	if rr := save(`{"season_id":1,"week_number":2,"entries":[{"member_id":1,"score":"full","wars":[{"war_id":99,"attended":true}]}]}`); rr.Code != http.StatusBadRequest {
		t.Errorf("unknown war: %d", rr.Code)
	}

	req := shReq(http.MethodGet, "", nil, nil)
	req.URL.RawQuery = "season_id=1"
	rr = httptest.NewRecorder()
	handleParticipationGet(rr, req)
	var got struct {
		HasWars       bool               `json:"has_wars"`
		Wars          []participationWar `json:"wars"`
		WarAttendance []warAttendanceRow `json:"war_attendance"`
	}
	json.Unmarshal(rr.Body.Bytes(), &got)
	if !got.HasWars || len(got.Wars) != 2 || len(got.WarAttendance) != 3 {
		t.Errorf("read back: %+v", got)
	}

	// The data endpoint: Alpha 2 of 2 (Pudgey's case), Bravo 0 of 1 known with one unknown.
	rr = httptest.NewRecorder()
	req = shReq(http.MethodGet, "", nil, nil)
	req.URL.RawQuery = "season_id=1"
	handleSeasonHubData(rr, req)
	var data struct {
		Members []SeasonMember `json:"members"`
	}
	json.Unmarshal(rr.Body.Bytes(), &data)
	by := map[string]SeasonMember{}
	for _, m := range data.Members {
		by[m.Name] = m
	}
	a, b := by["Alpha"], by["Bravo"]
	if a.WarsAttended == nil || *a.WarsAttended != 2 || *a.WarsCompleted != 2 || *a.WarsScheduled != 8 || *a.PollsResponded != 2 || *a.PollsApplicable != 2 {
		t.Errorf("Alpha: %+v", a)
	}
	if *b.WarsAttended != 0 || *b.WarsKnown != 1 || *b.PollsResponded != 1 {
		t.Errorf("Bravo: attended %d known %d polls %d", *b.WarsAttended, *b.WarsKnown, *b.PollsResponded)
	}
}

func TestLegacySeasonParticipationUntouched(t *testing.T) {
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	seedSeasonRow(t, 1, 3, "2026-07-13", true, false)
	mustExecT(t, `INSERT INTO members (id, name, rank) VALUES (1, 'Alpha', 'R3')`)
	rr := httptest.NewRecorder()
	handleParticipationSave(rr, shReq(http.MethodPut, `{"season_id":1,"week_number":1,"entries":[{"member_id":1,"score":"absent","attended_key_event":2,"attack_poll_voted":true}]}`, nil, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("legacy save: %d %s", rr.Code, rr.Body.String())
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_participation WHERE attended_key_event = 2 AND attack_poll_voted = 1`); n != 1 {
		t.Error("the legacy columns were not written")
	}
	rr = httptest.NewRecorder()
	req := shReq(http.MethodGet, "", nil, nil)
	req.URL.RawQuery = "season_id=1"
	handleSeasonHubData(rr, req)
	if strings.Contains(rr.Body.String(), "wars_attended") || !strings.Contains(rr.Body.String(), `"has_wars":false`) {
		t.Errorf("a legacy season got war figures: %s", rr.Body.String())
	}
}

func TestWarAttendanceImport(t *testing.T) {
	seedAttendanceSeason(t)
	call := func(h http.HandlerFunc, csv string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]any{"season_id": 1, "kind": "war_attendance", "csv": csv})
		rr := httptest.NewRecorder()
		h(rr, shReq(http.MethodPost, string(b), nil, nil))
		return rr
	}
	csv := "Member,War,Attended,Poll\nalpha,W2 attack,yes,yes\nBravo,2026-09-19,no,no\nCharlie,W7 attack,yes,\n"
	rr := call(handleSeasonImportPreview, csv)
	if !strings.Contains(rr.Body.String(), `"valid":2`) || !strings.Contains(rr.Body.String(), "no war of this season matches W7 attack") {
		t.Errorf("preview: %s", rr.Body.String())
	}
	csv = strings.Replace(csv, "Charlie,W7 attack,yes,\n", "", 1)
	if rr := call(handleSeasonImportConfirm, csv); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"written":2`) {
		t.Fatalf("confirm: %d %s", rr.Code, rr.Body.String())
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_war_attendance WHERE member_id = 1 AND war_id = 1 AND attended = 1 AND poll_response = 'yes'`); n != 1 {
		t.Error("W2 attack did not resolve to war 1 for alpha (folded)")
	}
	// A war with attendance can't be deleted.
	mustExecT(t, `UPDATE season_wars SET status = 'cancelled' WHERE id = 1`)
	rr = httptest.NewRecorder()
	handleSeasonWarDelete(rr, shReq(http.MethodDelete, "", map[string]string{"id": "1"}, nil))
	if rr.Code != http.StatusConflict {
		t.Errorf("deleting a war with attendance: %d", rr.Code)
	}
}
