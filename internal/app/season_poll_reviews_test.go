package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPollReviews(t *testing.T) {
	seedAttendanceSeason(t) // completed wars 1 (attack 2026-09-17) and 2 (defense 2026-09-19)
	// Alpha voted Yes and did not attend war 1; Bravo voted Yes and attended; Charlie voted Yes
	// and did not attend war 2.
	mustExecT(t, `INSERT INTO season_war_attendance (war_id, member_id, attended, poll_response) VALUES
		(1, 1, 0, 'yes'), (1, 2, 1, 'yes'), (2, 3, 0, 'yes')`)
	today := gametimeDateOf(time.Now())
	pending, err := pendingPollReviews(db, 1, today)
	if err != nil || len(pending) != 2 || pending[0].MemberID != 1 {
		t.Fatalf("pending: %+v %v", pending, err)
	}
	decide := func(body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		handlePollReviewDecide(rr, shReq(http.MethodPost, body, nil, nil))
		return rr
	}
	if rr := decide(`{"war_id":1,"member_id":1,"decision":"applied"}`); rr.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", rr.Code, rr.Body.String())
	}
	var reason string
	var sid, week, war int
	db.QueryRow(`SELECT reason, season_id, season_week, season_war_id FROM accountability_strikes WHERE member_id = 1 AND strike_type = 'war_poll_yes_no_show'`).
		Scan(&reason, &sid, &week, &war)
	if sid != 1 || week != 2 || war != 1 || !strings.Contains(reason, "Voted Yes on the Attack poll") {
		t.Errorf("strike: %q season %d week %d war %d", reason, sid, week, war)
	}
	if rr := decide(`{"war_id":1,"member_id":1,"decision":"applied"}`); rr.Code != http.StatusConflict {
		t.Errorf("second apply: %d", rr.Code)
	}
	// Dismiss persists when the attendance is saved again.
	if rr := decide(`{"war_id":2,"member_id":3,"decision":"dismissed"}`); rr.Code != http.StatusOK {
		t.Fatalf("dismiss: %d %s", rr.Code, rr.Body.String())
	}
	rr := httptest.NewRecorder()
	handleParticipationSave(rr, shReq(http.MethodPut, `{"season_id":1,"week_number":2,"entries":[{"member_id":3,"score":"full","wars":[{"war_id":2,"attended":false,"poll_response":"yes"}]}]}`, nil, nil))
	if pending, _ = pendingPollReviews(db, 1, today); len(pending) != 0 {
		t.Errorf("after the re-save: %+v", pending)
	}
	// A changed attendance (No → Yes) removes a pending review.
	mustExecT(t, `INSERT INTO season_war_attendance (war_id, member_id, attended, poll_response) VALUES (2, 2, 0, 'yes')`)
	if pending, _ = pendingPollReviews(db, 1, today); len(pending) != 1 {
		t.Fatalf("Bravo's review: %+v", pending)
	}
	mustExecT(t, `UPDATE season_war_attendance SET attended = 1 WHERE war_id = 2 AND member_id = 2`)
	if pending, _ = pendingPollReviews(db, 1, today); len(pending) != 0 {
		t.Errorf("attended now, still pending: %+v", pending)
	}

	// The strike form's season context is validated.
	create := func(body string) int {
		rr := httptest.NewRecorder()
		handleStrikeCreate(rr, shReq(http.MethodPost, body, nil, nil))
		return rr.Code
	}
	if c := create(`{"member_id":2,"strike_type":"war_poll_yes_no_show","reason":"x","season_id":1,"season_week":9}`); c != http.StatusBadRequest {
		t.Errorf("week 9 of 8: %d", c)
	}
	if c := create(`{"member_id":2,"strike_type":"war_poll_yes_no_show","reason":"x","season_week":2}`); c != http.StatusBadRequest {
		t.Errorf("a week without a season: %d", c)
	}
	if c := create(`{"member_id":2,"strike_type":"war_poll_yes_no_show","reason":"x","season_id":1,"season_week":2,"season_war_id":2}`); c != http.StatusOK {
		t.Errorf("valid season context: %d", c)
	}
	counts, _ := seasonStrikeCounts(db, 1)
	if counts[1] != 1 || counts[2] != 1 {
		t.Errorf("season strike counts %v", counts)
	}
}
