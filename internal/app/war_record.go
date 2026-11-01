// war_record.go — season war and faction win/loss records (private-docs 278). Pure.
//
// A war counts once it is completed with a battle date on or before today (decision 7). Wins
// and losses make the percentage; draws and unknown outcomes are listed, never counted.

package app

import "sort"

// wlRecord is one W/L line.
type wlRecord struct {
	Wins    int      `json:"wins"`
	Losses  int      `json:"losses"`
	Draws   int      `json:"draws"`
	Unknown int      `json:"unknown"`
	Pct     *float64 `json:"pct"` // wins ÷ (wins + losses) × 100; nil with neither
}

func (r *wlRecord) add(outcome string) {
	switch outcome {
	case "win":
		r.Wins++
	case "loss":
		r.Losses++
	case "draw":
		r.Draws++
	default:
		r.Unknown++
	}
}

func (r *wlRecord) finish() {
	if n := r.Wins + r.Losses; n > 0 {
		p := float64(r.Wins) / float64(n) * 100
		r.Pct = &p
	}
}

// warRecord is our record: overall, attacking and defending.
type warRecord struct {
	Overall wlRecord `json:"overall"`
	Attack  wlRecord `json:"attack"`
	Defense wlRecord `json:"defense"`
}

// warRecordRow is what the record reads of a war.
type warRecordRow struct {
	Status     string
	Role       string
	Outcome    string
	BattleDate string
}

// computeWarRecord counts completed wars dated on or before today (a game date).
func computeWarRecord(wars []warRecordRow, today string) warRecord {
	var out warRecord
	for _, w := range wars {
		if w.Status != "completed" || w.BattleDate == "" || w.BattleDate > today {
			continue
		}
		out.Overall.add(w.Outcome)
		if w.Role == "attack" {
			out.Attack.add(w.Outcome)
		} else {
			out.Defense.add(w.Outcome)
		}
	}
	out.Overall.finish()
	out.Attack.finish()
	out.Defense.finish()
	return out
}

// factionBattleRow is what the faction record reads of a battle: the recording alliance's
// faction (resolved at read time, "" when it has none) and the week.
type factionBattleRow struct {
	FactionID *int
	Week      int
	Outcome   string
}

// factionRecord is one faction's season and weekly record.
type factionRecord struct {
	FactionID *int             `json:"faction_id"` // nil: alliances with no faction (counted in no faction)
	Season    wlRecord         `json:"season"`
	Weeks     []factionWeekRow `json:"weeks"`
}

type factionWeekRow struct {
	Week   int      `json:"week"`
	Record wlRecord `json:"record"`
}

// computeFactionRecords groups battles by the recording alliance's faction; battles of an
// alliance with no faction come last, under a nil faction.
func computeFactionRecords(battles []factionBattleRow, factionOrder []int) []factionRecord {
	type acc struct {
		season wlRecord
		weeks  map[int]*wlRecord
	}
	by := map[int]*acc{}
	var unknown *acc
	for _, b := range battles {
		var a *acc
		if b.FactionID == nil {
			if unknown == nil {
				unknown = &acc{weeks: map[int]*wlRecord{}}
			}
			a = unknown
		} else {
			if by[*b.FactionID] == nil {
				by[*b.FactionID] = &acc{weeks: map[int]*wlRecord{}}
			}
			a = by[*b.FactionID]
		}
		a.season.add(b.Outcome)
		if a.weeks[b.Week] == nil {
			a.weeks[b.Week] = &wlRecord{}
		}
		a.weeks[b.Week].add(b.Outcome)
	}
	build := func(id *int, a *acc) factionRecord {
		r := factionRecord{FactionID: id, Weeks: []factionWeekRow{}}
		if a == nil {
			r.Season.finish()
			return r
		}
		r.Season = a.season
		r.Season.finish()
		var weeks []int
		for w := range a.weeks {
			weeks = append(weeks, w)
		}
		sort.Ints(weeks)
		for _, w := range weeks {
			rec := *a.weeks[w]
			rec.finish()
			r.Weeks = append(r.Weeks, factionWeekRow{Week: w, Record: rec})
		}
		return r
	}
	out := []factionRecord{}
	for _, id := range factionOrder {
		id := id
		out = append(out, build(&id, by[id]))
	}
	if unknown != nil {
		out = append(out, build(nil, unknown))
	}
	return out
}
