package main

// VS Duel League — pure scoring functions.
//
// A weekly matchup is played over 6 days (Mon–Sat). Winning a day awards fixed
// "match points": Day 1 = 1, Days 2–5 = 2 each, Day 6 (Enemy Buster) = 4 → a 13-point
// pool. Whoever accumulates more match points wins the week (7 of 13 = a clinching
// majority in a clean week). A tied *day* awards 0 to both, shrinking the pool, so a
// week can also end in an overall tie (e.g. 6–6).
//
// These functions are deliberately DB-free and total (no panics) so they can be unit
// tested exhaustively before any handler/database code exists.

// vsDayMatchPoints returns the match points at stake for winning day n (1–6).
// Any other day number (incl. Sunday's 7 / "Alliance Star") is worth 0.
func vsDayMatchPoints(n int) int {
	switch n {
	case 1:
		return 1
	case 2, 3, 4, 5:
		return 2
	case 6:
		return 4
	default:
		return 0
	}
}

// awardMatchPoints returns the match points each side earns for a day's outcome.
// This is the single place the tie policy lives (tie → 0 to both); if the game turns
// out to award ties differently, only this function changes.
func awardMatchPoints(outcome string, day int) (our, opp int) {
	p := vsDayMatchPoints(day)
	switch outcome {
	case "win":
		return p, 0
	case "loss":
		return 0, p
	default: // "tie", "pending", "" → 0 to both
		return 0, 0
	}
}

// deriveDayOutcome resolves a day's outcome from the two raw Alliance Duel Scores.
// Used by the day-write normalizer (F-R07): when both raw scores are present the stored
// outcome is overwritten with this, so a later score edit can never leave a stale outcome.
func deriveDayOutcome(ourScore, oppScore int) string {
	switch {
	case ourScore > oppScore:
		return "win"
	case oppScore > ourScore:
		return "loss"
	default:
		return "tie"
	}
}

// normalizeDayOutcome collapses anything not win/loss/tie to "pending".
func normalizeDayOutcome(o string) string {
	switch o {
	case "win", "loss", "tie":
		return o
	default:
		return "pending"
	}
}

// VSLeagueWeekStanding is the fully-derived state of a weekly matchup, computed on read
// from the six day outcomes. Nothing here is persisted (see the plan's compute-on-read rule).
type VSLeagueWeekStanding struct {
	OurPoints      int    `json:"our_points"`      // accumulated match points (0–13)
	OpponentPoints int    `json:"opponent_points"` // accumulated match points (0–13)
	Remaining      int    `json:"remaining"`       // match points still in play (pending days)
	Outcome        string `json:"outcome"`         // "win" | "loss" | "tie" | "pending"
	Decided        bool   `json:"decided"`         // mathematically settled (clinched/eliminated or all days played)
	ClinchDay      int    `json:"clinch_day"`      // 1–6 the matchup was decided; 0 while pending or on an overall tie
}

// computeWeekStanding rolls up the six day outcomes (index 0 = day 1 … index 5 = day 6;
// "" or any non-decided value = pending) into the full weekly standing.
//
// The clinch day is truthful to sequential play: at the moment day D is played, all days
// after D are still in play (at full weight), so D clinches iff the leader after D already
// exceeds the trailer plus every future day's points.
func computeWeekStanding(days [6]string) VSLeagueWeekStanding {
	var our, opp, remaining int
	for i := 0; i < 6; i++ {
		day := i + 1
		o := normalizeDayOutcome(days[i])
		switch o {
		case "win":
			our += vsDayMatchPoints(day)
		case "loss":
			opp += vsDayMatchPoints(day)
		case "pending":
			remaining += vsDayMatchPoints(day)
			// "tie": 0 to both, and its points leave the pool
		}
	}

	st := VSLeagueWeekStanding{OurPoints: our, OpponentPoints: opp, Remaining: remaining}

	switch {
	case our > opp+remaining:
		st.Decided, st.Outcome = true, "win" // opponent can no longer catch up
	case opp > our+remaining:
		st.Decided, st.Outcome = true, "loss"
	case remaining == 0:
		st.Decided = true
		switch {
		case our > opp:
			st.Outcome = "win"
		case opp > our:
			st.Outcome = "loss"
		default:
			st.Outcome = "tie"
		}
	default:
		st.Outcome = "pending"
	}

	// Clinch day (only for a decided win/loss — a tie is never "clinched" early).
	if st.Decided && st.Outcome != "tie" {
		st.ClinchDay = clinchDay(days)
	}
	return st
}

// clinchDay walks days in order and returns the first decided day after which the leader's
// running total exceeds the trailer's plus all still-future days' points (0 if never).
func clinchDay(days [6]string) int {
	var our, opp int
	for i := 0; i < 6; i++ {
		day := i + 1
		switch normalizeDayOutcome(days[i]) {
		case "win":
			our += vsDayMatchPoints(day)
		case "loss":
			opp += vsDayMatchPoints(day)
		}
		// points still available on every later day (all treated as in-play, as they were when day `day` finished)
		future := 0
		for k := i + 1; k < 6; k++ {
			future += vsDayMatchPoints(k + 1)
		}
		lead, trail := our, opp
		if opp > our {
			lead, trail = opp, our
		}
		if lead != trail && lead > trail+future {
			return day
		}
	}
	return 0
}
