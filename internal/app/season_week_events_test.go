package app

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestWeekEventDetail(t *testing.T) {
	seedAttendanceSeason(t)
	create := func(body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		handleWeekEventCreate(rr, shReq(http.MethodPost, body, nil, nil))
		return rr
	}
	if rr := create(`{"season_id":1,"week_number":2,"label":"Monday VS","kind":"vs","required_for_core":true}`); rr.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	if rr := create(`{"season_id":1,"week_number":2,"label":"Monday VS"}`); rr.Code != http.StatusConflict {
		t.Errorf("the same label twice in a week: %d", rr.Code)
	}
	if rr := create(`{"season_id":1,"week_number":3,"label":"Monday VS"}`); rr.Code != http.StatusOK {
		t.Errorf("the same label in another week: %d", rr.Code)
	}
	var ev2, ev3 int
	db.QueryRow(`SELECT id FROM season_participation_events WHERE week_number = 2`).Scan(&ev2)
	db.QueryRow(`SELECT id FROM season_participation_events WHERE week_number = 3`).Scan(&ev3)
	save := func(body string) int {
		rr := httptest.NewRecorder()
		handleParticipationSave(rr, shReq(http.MethodPut, body, nil, nil))
		return rr.Code
	}
	e2 := strconv.Itoa(ev2)
	if c := save(`{"season_id":1,"week_number":2,"entries":[{"member_id":1,"score":"full","events":[{"event_id":` + e2 + `,"result":"partial","note":"left early"}]}]}`); c != http.StatusOK {
		t.Fatalf("save: %d", c)
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_participation_event_entries WHERE member_id = 1 AND result = 'partial' AND note = 'left early'`); n != 1 {
		t.Error("the event result was not saved with the week")
	}
	// The score is the officers' own; the detail does not touch it.
	var score string
	db.QueryRow(`SELECT score FROM season_participation WHERE member_id = 1 AND week_number = 2`).Scan(&score)
	if score != "full" {
		t.Errorf("score %q", score)
	}
	if c := save(`{"season_id":1,"week_number":2,"entries":[{"member_id":1,"score":"full","events":[{"event_id":` + e2 + `,"result":"heroic"}]}]}`); c != http.StatusBadRequest {
		t.Errorf("an unknown result: %d", c)
	}
	if c := save(`{"season_id":1,"week_number":2,"entries":[{"member_id":1,"score":"full","events":[{"event_id":` + strconv.Itoa(ev3) + `,"result":"full"}]}]}`); c != http.StatusBadRequest {
		t.Errorf("another week's event: %d", c)
	}
	// Clearing the result removes the line; deleting the event removes its results.
	save(`{"season_id":1,"week_number":2,"entries":[{"member_id":1,"score":"full","events":[{"event_id":` + e2 + `,"result":""}]}]}`)
	if n := countT(t, `SELECT COUNT(*) FROM season_participation_event_entries`); n != 0 {
		t.Error("a cleared result stayed")
	}
	save(`{"season_id":1,"week_number":2,"entries":[{"member_id":2,"score":"full","events":[{"event_id":` + e2 + `,"result":"absent"}]}]}`)
	rr := httptest.NewRecorder()
	handleWeekEventDelete(rr, shReq(http.MethodDelete, "", map[string]string{"id": e2}, nil))
	if rr.Code != http.StatusOK || countT(t, `SELECT COUNT(*) FROM season_participation_event_entries`) != 0 {
		t.Errorf("delete: %d %s", rr.Code, rr.Body.String())
	}
}
