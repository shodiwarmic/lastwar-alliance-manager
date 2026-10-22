package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestEvaluateTier(t *testing.T) {
	eight, seven := 8, 7
	tactician := tierRule{Key: "tactician", WarsRequired: &eight}
	elite := tierRule{Key: "elite", WarsRequired: &seven}
	at := func(attended int) eligibilityInput {
		return eligibilityInput{HasWars: true, WarsAttended: attended, WarsCompleted: 4, WarsScheduled: 8}
	}
	// Pudgey: Tactician 8 of 8 — 4/4 on pace, 3/4 eliminated; Elite 7 of 8 — 3/4 can still qualify.
	if got := evaluateTier(tactician, at(4)); got.Status != "on_pace" {
		t.Errorf("tactician 4/4: %+v", got)
	}
	if got := evaluateTier(tactician, at(3)); got.Status != "eliminated" {
		t.Errorf("tactician 3/4: %+v", got)
	}
	if got := evaluateTier(elite, at(3)); got.Status != "can_still_qualify" {
		t.Errorf("elite 3/4: %+v", got)
	}
	// After the last scheduled war, on pace becomes eligible.
	done := eligibilityInput{HasWars: true, WarsAttended: 7, WarsCompleted: 8, WarsScheduled: 8, SeasonOver: true}
	if got := evaluateTier(elite, done); got.Status != "eligible" {
		t.Errorf("elite 7/8 at the end: %+v", got)
	}
	if got := evaluateTier(tierRule{Key: "valued"}, at(0)); got.Status != "not_applicable" {
		t.Errorf("no rule: %+v", got)
	}

	// Core events: a miss eliminates, an unknown can still qualify, an exemption satisfies.
	core := tierRule{Key: "core", RequiresCore: true}
	events := []weekEvent{{ID: 1, WeekNumber: 1, Label: "Monday VS", RequiredForCore: true},
		{ID: 2, WeekNumber: 1, Label: "Siege", RequiredForCore: true}, {ID: 3, WeekNumber: 2, Label: "Later", RequiredForCore: true},
		{ID: 4, WeekNumber: 1, Label: "Optional"}}
	req, att, missed, unknown := coreEventInput(events, 1, map[int]string{1: "full", 2: "excused"})
	if req != 1 || att != 1 || len(missed) != 0 || len(unknown) != 0 {
		t.Errorf("core counts %d %d %v %v", req, att, missed, unknown)
	}
	in := eligibilityInput{CoreRequired: 2, CoreAttended: 1, CoreMissed: []string{"W1 Siege"}}
	if got := evaluateTier(core, in); got.Status != "eliminated" {
		t.Errorf("core missed: %+v", got)
	}
	in.Exempt, in.ExemptNote = true, "R4 duties"
	if got := evaluateTier(core, in); got.Status != "on_pace" || !strings.Contains(got.Reasons[0], "exempt") {
		t.Errorf("exempt: %+v", got)
	}
	if got := evaluateTier(core, eligibilityInput{CoreRequired: 1, CoreUnknown: []string{"W1 Siege"}}); got.Status != "can_still_qualify" {
		t.Errorf("core unknown: %+v", got)
	}
}

func TestEligibilityCountsAndTransfer(t *testing.T) {
	seedAttendanceSeason(t) // two completed wars, three members
	mustExecT(t, `INSERT INTO season_reward_tiers (season_id, key, label, slot_count, color, sort_order, wars_required) VALUES
		(1, 'elite', 'Elite', 2, 'info', 0, 7), (1, 'valued', 'Valued', 5, 'neutral', 1, NULL)`)
	mustExecT(t, `INSERT INTO season_war_attendance (war_id, member_id, attended, poll_response) VALUES
		(1, 1, 1, 'yes'), (2, 1, 1, 'yes'), (1, 2, 0, 'no'), (2, 2, 0, 'no')`)
	mustExecT(t, `INSERT INTO season_participation (season_id, member_id, week_number, score) VALUES (1, 1, 1, 'full')`)
	req := shReq(http.MethodGet, "", nil, nil)
	req.URL.RawQuery = "season_id=1"
	rr := httptest.NewRecorder()
	handleSeasonEligibility(rr, req)
	var res seasonEligibilityResult
	json.Unmarshal(rr.Body.Bytes(), &res)
	if len(res.Tiers) != 2 || res.Tiers[0].Eligible != 1 {
		t.Fatalf("tiers: %s", rr.Body.String())
	}
	// A member nobody scored has an overall standing too — their completed weeks are Absent, as on
	// Rankings — not "no completed week yet".
	for _, m := range res.Members {
		switch {
		case m.OverallPct == nil:
			t.Errorf("%s has no overall standing", m.Name)
		case m.Name == "Bravo" && *m.OverallPct != 0:
			t.Errorf("Bravo, never scored: %v%%, want 0", *m.OverallPct)
		case m.Name == "Alpha" && *m.OverallPct <= 0:
			t.Errorf("Alpha, one Full week: %v%%", *m.OverallPct)
		}
	}

	save := func(body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		handleRewardSave(rr, shReq(http.MethodPost, body, nil, nil))
		return rr
	}
	// Bravo (0 of 2, eliminated for Elite) receiving Alpha's transferred reward: refused without
	// a reason, saved with one.
	body := `{"season_id":1,"member_id":2,"reward_tier":"elite","transferred_from_member_id":1}`
	if rr := save(body); rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "override reason") {
		t.Errorf("transfer without a reason: %d %s", rr.Code, rr.Body.String())
	}
	body = `{"season_id":1,"member_id":2,"reward_tier":"elite","transferred_from_member_id":1,"transfer_override_note":"covered Alpha's slot"}`
	if rr := save(body); rr.Code != http.StatusOK {
		t.Fatalf("transfer with a reason: %d %s", rr.Code, rr.Body.String())
	}
	var from int
	var note string
	db.QueryRow(`SELECT transferred_from_member_id, transfer_override_note FROM season_rewards WHERE member_id = 2`).Scan(&from, &note)
	if from != 1 || note == "" {
		t.Errorf("stored %d %q", from, note)
	}
	// Alpha, on pace, needs no reason.
	if rr := save(`{"season_id":1,"member_id":1,"reward_tier":"elite","transferred_from_member_id":3}`); rr.Code != http.StatusOK {
		t.Errorf("an eligible recipient: %d %s", rr.Code, rr.Body.String())
	}

	// The context endpoint and an exemption.
	rr = httptest.NewRecorder()
	handleExemptionCreate(rr, shReq(http.MethodPost, `{"season_id":1,"member_id":3,"note":"R4 duties"}`, nil, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("exemption: %d %s", rr.Code, rr.Body.String())
	}
	req = shReq(http.MethodGet, "", nil, nil)
	req.URL.RawQuery = "season_id=1&member_id=" + strconv.Itoa(3)
	rr = httptest.NewRecorder()
	handleRewardContext(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"exempt":true`) || !strings.Contains(rr.Body.String(), `"elite_tier":"elite"`) {
		t.Errorf("context: %d %s", rr.Code, rr.Body.String())
	}
}
