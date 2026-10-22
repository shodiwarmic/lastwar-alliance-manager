// season_status.go — the Season Hub's status engine (private-docs 283; decisions 3–6). Pure:
// the one place rows become a member's standing.
//
//   - A week counts once it is complete: its last game day has passed (game date ≥ start + 7w),
//     or the season is archived. The current week counts for nobody.
//   - Overall: points over the completed weeks less the member's Excused weeks. A completed week
//     with no score counts as Absent and is named "not scored" in the reason, never silently.
//   - Wars: attended over the completed wars whose attendance is known; a completed war with no
//     record for the member is unknown and left out (with the count in the reason).
//   - Polls: responded (yes or no) over the completed wars that held a poll.
//   - Each category is Active, At Risk or Below Requirement against its floors (polls and wars
//     fall back to the overall floors); a category with nothing to judge is not applicable.
//     The final status is the worst applicable category.

package app

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"lastwar-alliance/internal/gametime"
)

// excusedScoreKey is the score level that takes a week out of the member's denominator.
const excusedScoreKey = "excused"

// weekInput is one completed week for a member: its score key ("" when none was entered) and
// that score's points.
type weekInput struct {
	Week   int
	Score  string
	Points int
}

// warInput is one completed war for a member.
type warInput struct {
	Attended *bool
	Poll     string // yes | no | none
	PollHeld bool
}

type standingInputs struct {
	Weeks      []weekInput
	Wars       []warInput
	MaxWeekPts int
}

type categoryStanding struct {
	Applicable  bool    `json:"applicable"`
	Numerator   int     `json:"numerator"`
	Denominator int     `json:"denominator"`
	Pct         float64 `json:"pct"`
	Status      string  `json:"status"` // active | at_risk | below | na
	Reason      string  `json:"reason"`
}

type standing struct {
	Overall        categoryStanding `json:"overall"`
	Polls          categoryStanding `json:"polls"`
	Wars           categoryStanding `json:"wars"`
	Final          string           `json:"final"`      // active | at_risk | below | na
	FinalFrom      string           `json:"final_from"` // overall | polls | wars | ""
	CompletedWeeks int              `json:"completed_weeks"`
}

// completedSeasonWeeks is how many of a season's weeks have ended at now (all of them once
// archived).
func completedSeasonWeeks(s *Season, now time.Time) int {
	if s.ArchivedAt != "" {
		return s.WeekCount
	}
	start, err := time.ParseInLocation("2006-01-02", s.StartDate, gametime.Loc)
	if err != nil {
		return 0
	}
	today := now.In(gametime.Loc)
	n := 0
	for w := 1; w <= s.WeekCount; w++ {
		if !today.Before(start.AddDate(0, 0, 7*w)) {
			n = w
		}
	}
	return n
}

// countsWeek reports whether a week counts toward the season's standings: the weeks in
// UncountedWeeks (the reward week on the shipped templates) are left out everywhere.
func (s *Season) countsWeek(week int) bool {
	for _, w := range s.UncountedWeeks {
		if w == week {
			return false
		}
	}
	return true
}

// cleanUncountedWeeks validates a season's uncounted weeks against its week count and returns
// them sorted, without repeats.
func cleanUncountedWeeks(weeks []int, weekCount int) ([]int, error) {
	seen := map[int]bool{}
	out := []int{}
	for _, w := range weeks {
		if w < 1 || w > weekCount {
			return nil, fmt.Errorf("a week that doesn't count must be between 1 and %d", weekCount)
		}
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	sort.Ints(out)
	return out, nil
}

func bandStatus(pct float64, active, atRisk int) string {
	switch {
	case pct >= float64(active):
		return "active"
	case pct >= float64(atRisk):
		return "at_risk"
	}
	return "below"
}

func floorOr(v *int, fallback int) int {
	if v != nil {
		return *v
	}
	return fallback
}

func roundPct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return math.Round(float64(n)/float64(d)*1000) / 10
}

func weekList(ws []int) string {
	parts := make([]string, len(ws))
	for i, w := range ws {
		parts[i] = fmt.Sprint(w)
	}
	return strings.Join(parts, ", ")
}

// orNone reads an empty list as "none" in an activity line.
func orNone(list string) string {
	if list == "" {
		return "none"
	}
	return list
}

// computeSeasonStanding judges one member.
func computeSeasonStanding(s *Season, in standingInputs) standing {
	var out standing
	na := categoryStanding{Status: "na"}

	// Overall.
	o := na
	var notScored, excused, uncounted []int
	for _, w := range in.Weeks {
		if !s.countsWeek(w.Week) {
			uncounted = append(uncounted, w.Week)
			continue
		}
		if w.Score == excusedScoreKey {
			excused = append(excused, w.Week)
			continue
		}
		if w.Score == "" {
			notScored = append(notScored, w.Week)
		}
		o.Numerator += w.Points
		o.Denominator += in.MaxWeekPts
	}
	counted := len(in.Weeks) - len(uncounted) - len(excused)
	if counted > 0 && in.MaxWeekPts > 0 {
		o.Applicable = true
		o.Pct = roundPct(o.Numerator, o.Denominator)
		o.Status = bandStatus(o.Pct, s.TierActiveMinPct, s.TierAtRiskMinPct)
		var why []string
		why = append(why, fmt.Sprintf("%d of %d points over %d completed week%s", o.Numerator, o.Denominator, counted, pluralS(counted)))
		if len(excused) > 0 {
			why = append(why, "excused: week "+weekList(excused))
		}
		if len(notScored) > 0 {
			why = append(why, "not scored (counted as Absent): week "+weekList(notScored))
		}
		if len(uncounted) > 0 {
			why = append(why, "doesn't count this season: week "+weekList(uncounted))
		}
		o.Reason = strings.Join(why, "; ")
	} else if len(in.Weeks)-len(uncounted) == 0 {
		o.Reason = "no completed weeks yet"
	} else {
		o.Reason = "every completed week excused"
	}
	out.Overall = o

	// Wars and polls.
	w, p := na, na
	completed, unknown, pollsHeld := len(in.Wars), 0, 0
	for _, x := range in.Wars {
		if x.Attended == nil {
			unknown++
		} else {
			w.Denominator++
			w.Numerator += boolToInt(*x.Attended)
		}
		if x.PollHeld {
			pollsHeld++
			if x.Poll == "yes" || x.Poll == "no" {
				p.Numerator++
			}
		}
	}
	switch {
	case completed == 0:
		w.Reason = "no completed wars yet"
	case w.Denominator == 0:
		w.Reason = fmt.Sprintf("attendance not recorded for any of %d completed war%s", completed, pluralS(completed))
	default:
		w.Applicable = true
		w.Pct = roundPct(w.Numerator, w.Denominator)
		w.Status = bandStatus(w.Pct, floorOr(s.WarActiveMinPct, s.TierActiveMinPct), floorOr(s.WarAtRiskMinPct, s.TierAtRiskMinPct))
		w.Reason = fmt.Sprintf("attended %d of %d completed war%s", w.Numerator, w.Denominator, pluralS(w.Denominator))
		if unknown > 0 {
			w.Reason += fmt.Sprintf("; %d not recorded", unknown)
		}
	}
	if pollsHeld == 0 {
		p.Reason = "no completed war held a poll yet"
	} else {
		p.Applicable = true
		p.Denominator = pollsHeld
		p.Pct = roundPct(p.Numerator, p.Denominator)
		p.Status = bandStatus(p.Pct, floorOr(s.PollActiveMinPct, s.TierActiveMinPct), floorOr(s.PollAtRiskMinPct, s.TierAtRiskMinPct))
		p.Reason = fmt.Sprintf("answered %d of %d poll%s", p.Numerator, p.Denominator, pluralS(p.Denominator))
	}
	out.Wars, out.Polls = w, p

	// Final: the worst applicable category.
	rank := map[string]int{"active": 0, "at_risk": 1, "below": 2}
	out.Final = "na"
	for _, c := range []struct {
		name string
		cat  categoryStanding
	}{{"overall", o}, {"polls", p}, {"wars", w}} {
		if !c.cat.Applicable {
			continue
		}
		if out.Final == "na" || rank[c.cat.Status] > rank[out.Final] {
			out.Final, out.FinalFrom = c.cat.Status, c.name
		}
	}
	return out
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
