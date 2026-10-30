// resource_projection.go — the War tab's resource side (private-docs 279). Pure.
//
// Our total is the latest observation; the projection adds what our held cities produce per
// hour (the level table's output; strongholds produce no Copper) from that observation up to
// now and to the next game reset, capped at the season's production stop. A projection is
// labelled as one and never written back as a balance. Loot scenarios are the season's steps
// of an opponent's latest total, each against our gap to the target rank's cutoff.

package app

import (
	"math"
	"time"

	"lastwar-alliance/internal/gametime"
)

// productionSource is one holding's contribution.
type productionSource struct {
	Name    string `json:"name"`
	Level   *int   `json:"level"`
	PerHour int64  `json:"per_hour"`
}

// productionFromHoldings sums the hourly output of held cities (and a Capitol, read as the
// city level it is in the table). Holdings without a known output are listed, not guessed.
func productionFromHoldings(held []landHolding, levels []landLevel) (int64, []productionSource, []string) {
	rate := map[int]int64{}
	for _, l := range levels {
		if l.Kind == "city" && l.ResourcePerHour != nil {
			rate[l.Level] = int64(*l.ResourcePerHour)
		}
	}
	var total int64
	sources, unknown := []productionSource{}, []string{}
	for _, h := range held {
		if h.Kind != "city" && h.Kind != "capitol" {
			continue
		}
		if h.Level == nil {
			unknown = append(unknown, h.Name)
			continue
		}
		r, ok := rate[*h.Level]
		if !ok {
			unknown = append(unknown, h.Name)
			continue
		}
		total += r
		sources = append(sources, productionSource{Name: h.Name, Level: h.Level, PerHour: r})
	}
	return total, sources, unknown
}

// nextGameReset is the next 00:00 server time after now.
func nextGameReset(now time.Time) time.Time {
	st := now.In(gametime.Loc)
	return time.Date(st.Year(), st.Month(), st.Day(), 0, 0, 0, 0, gametime.Loc).AddDate(0, 0, 1)
}

// productionStopAt is the instant production stops (the reset starting week W day D), nil when
// the season sets none.
func productionStopAt(seasonStart string, week, day *int) *time.Time {
	if week == nil || day == nil {
		return nil
	}
	t, err := landUnlockAt(seasonStart, *week, *day, 0)
	if err != nil {
		return nil
	}
	return &t
}

// projectTotal is total plus perHour for every hour from observedAt to at, never past stop.
func projectTotal(total, perHour int64, observedAt, at time.Time, stop *time.Time) int64 {
	end := at
	if stop != nil && stop.Before(end) {
		end = *stop
	}
	if !end.After(observedAt) {
		return total
	}
	return total + int64(math.Floor(float64(perHour)*end.Sub(observedAt).Hours()))
}

// lootScenario is one step of an opponent's total, against our gap.
type lootScenario struct {
	Pct    int      `json:"pct"`
	Amount int64    `json:"amount"`
	OfGap  *float64 `json:"of_gap"` // % of our gap; nil when there is no gap or no cutoff
}

// lootScenarios are step, 2×step … up to max percent of their total. gap nil means unknown;
// gap ≤ 0 means we are already past the cutoff.
func lootScenarios(theirs int64, stepPct, maxPct int, gap *int64) []lootScenario {
	out := []lootScenario{}
	if stepPct <= 0 {
		return out
	}
	for p := stepPct; p <= maxPct; p += stepPct {
		s := lootScenario{Pct: p, Amount: theirs * int64(p) / 100}
		if gap != nil && *gap > 0 {
			v := math.Round(float64(s.Amount)/float64(*gap)*100*10) / 10
			s.OfGap = &v
		}
		out = append(out, s)
	}
	return out
}

// rankGroup is one reward group of war ranks.
type rankGroup struct {
	ID       int    `json:"id"`
	Label    string `json:"label"`
	RankFrom int    `json:"rank_from"`
	RankTo   int    `json:"rank_to"`
	Position int    `json:"position"`
}

func groupForRank(groups []rankGroup, rank *int) *rankGroup {
	if rank == nil {
		return nil
	}
	for i := range groups {
		if *rank >= groups[i].RankFrom && *rank <= groups[i].RankTo {
			return &groups[i]
		}
	}
	return nil
}

// rewardTierStatus judges our rank goal: on_track when the projection at the reset reaches the
// target cutoff; at_risk when the latest total is short but the gap is within the largest loot
// scenario of any opponent considered; behind otherwise; unknown without a total, target or
// cutoff.
func rewardTierStatus(latest, projectedAtReset *int64, cutoff *int64, largestLoot int64) string {
	if latest == nil || cutoff == nil {
		return "unknown"
	}
	if projectedAtReset != nil && *projectedAtReset >= *cutoff {
		return "on_track"
	}
	if *latest >= *cutoff {
		return "on_track"
	}
	if gap := *cutoff - *latest; largestLoot > 0 && gap <= largestLoot {
		return "at_risk"
	}
	return "behind"
}
