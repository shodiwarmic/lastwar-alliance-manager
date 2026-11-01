package app

import (
	"strings"
	"testing"
	"time"
)

func statusSeason() *Season {
	return &Season{StartDate: "2026-09-07", WeekCount: 8, TierActiveMinPct: 70, TierAtRiskMinPct: 60}
}

func weeksOf(scores ...string) []weekInput {
	pts := map[string]int{"full": 10, "partial": 5, "absent": 0, "excused": 0, "": 0}
	out := make([]weekInput, len(scores))
	for i, s := range scores {
		out[i] = weekInput{Week: i + 1, Score: s, Points: pts[s]}
	}
	return out
}

func TestCompletedSeasonWeeks(t *testing.T) {
	s := statusSeason()
	// Week 1 ends when 2026-09-14 begins (game time); the current week counts for nobody.
	if n := completedSeasonWeeks(s, *stTime(t, "2026-09-13 23:59")); n != 0 {
		t.Errorf("before week 1 ends: %d", n)
	}
	if n := completedSeasonWeeks(s, *stTime(t, "2026-09-14 00:00")); n != 1 {
		t.Errorf("week 1 ended: %d", n)
	}
	if n := completedSeasonWeeks(s, *stTime(t, "2026-09-30 12:00")); n != 3 {
		t.Errorf("three weeks: %d", n)
	}
	s.ArchivedAt = "2026-11-01 00:00:00"
	if n := completedSeasonWeeks(s, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)); n != 8 {
		t.Errorf("archived: %d", n)
	}
}

func TestSeasonStanding(t *testing.T) {
	s := statusSeason()

	// 283's reproduction: three Full weeks, three complete → 100 % Active.
	got := computeSeasonStanding(s, standingInputs{Weeks: weeksOf("full", "full", "full"), MaxWeekPts: 10})
	if got.Overall.Pct != 100 || got.Overall.Status != "active" || got.Final != "active" {
		t.Errorf("three of three: %+v", got.Overall)
	}
	// The same three Full weeks once all eight are complete → 37.5 % Below Requirement.
	got = computeSeasonStanding(s, standingInputs{Weeks: weeksOf("full", "full", "full", "absent", "absent", "absent", "absent", "absent"), MaxWeekPts: 10})
	if got.Overall.Pct != 37.5 || got.Overall.Status != "below" {
		t.Errorf("three of eight: %+v", got.Overall)
	}

	// Excused weeks leave the denominator; a not-scored week counts as Absent and is named.
	got = computeSeasonStanding(s, standingInputs{Weeks: weeksOf("full", "excused", "", "full"), MaxWeekPts: 10})
	if got.Overall.Denominator != 30 || got.Overall.Pct != 66.7 || got.Overall.Status != "at_risk" ||
		!strings.Contains(got.Overall.Reason, "excused: week 2") || !strings.Contains(got.Overall.Reason, "not scored (counted as Absent): week 3") {
		t.Errorf("excused and not scored: %+v", got.Overall)
	}
	if got = computeSeasonStanding(s, standingInputs{MaxWeekPts: 10}); got.Overall.Applicable || got.Final != "na" {
		t.Errorf("no completed weeks: %+v", got)
	}

	// Wars: 2 of 2 after two of eight; an unrecorded war is left out; polls not applicable
	// before any war.
	yes, no := true, false
	got = computeSeasonStanding(s, standingInputs{Weeks: weeksOf("full"), MaxWeekPts: 10})
	if got.Polls.Applicable || got.Wars.Applicable {
		t.Errorf("before a war: %+v / %+v", got.Polls, got.Wars)
	}
	got = computeSeasonStanding(s, standingInputs{Weeks: weeksOf("full", "full"), MaxWeekPts: 10,
		Wars: []warInput{{Attended: &yes, Poll: "yes", PollHeld: true}, {Attended: &yes, Poll: "no", PollHeld: true}}})
	if got.Wars.Numerator != 2 || got.Wars.Denominator != 2 || got.Wars.Status != "active" || got.Polls.Pct != 100 {
		t.Errorf("2 of 2: %+v %+v", got.Wars, got.Polls)
	}
	got = computeSeasonStanding(s, standingInputs{Weeks: weeksOf("full", "full"), MaxWeekPts: 10,
		Wars: []warInput{{Attended: &yes, PollHeld: true, Poll: "no"}, {Attended: &no, PollHeld: true, Poll: "yes"}, {PollHeld: false}}})
	if got.Wars.Denominator != 2 || !strings.Contains(got.Wars.Reason, "1 not recorded") || got.Wars.Status != "below" {
		t.Errorf("unrecorded war: %+v", got.Wars)
	}
	// Final = the worst applicable: overall Active, wars Below.
	if got.Final != "below" || got.FinalFrom != "wars" {
		t.Errorf("final %s from %s", got.Final, got.FinalFrom)
	}

	// Poll and war floors fall back to the overall ones; their own floors win when set.
	got = computeSeasonStanding(s, standingInputs{Weeks: weeksOf("full"), MaxWeekPts: 10,
		Wars: []warInput{{Attended: &yes, Poll: "yes", PollHeld: true}, {Attended: &yes, Poll: "none", PollHeld: true},
			{Attended: &yes, Poll: "yes", PollHeld: true}}})
	if got.Polls.Pct != 66.7 || got.Polls.Status != "at_risk" {
		t.Errorf("polls on the overall floors: %+v", got.Polls)
	}
	low := 50
	s.PollActiveMinPct, s.PollAtRiskMinPct = &low, &low
	got = computeSeasonStanding(s, standingInputs{Weeks: weeksOf("full"), MaxWeekPts: 10,
		Wars: []warInput{{Attended: &yes, Poll: "yes", PollHeld: true}, {Attended: &yes, Poll: "none", PollHeld: true},
			{Attended: &yes, Poll: "yes", PollHeld: true}}})
	if got.Polls.Status != "active" {
		t.Errorf("polls on their own floor: %+v", got.Polls)
	}
}
