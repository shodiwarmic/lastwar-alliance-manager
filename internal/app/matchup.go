// matchup.go — the War tab's matchup score (private-docs 277, decision 13). Pure.
//
// Per category the advantage is (ours − theirs) ÷ theirs × 100. A category with either side
// unknown (or theirs not positive) is dropped and the remaining weights re-normalised; below the
// season's minimum number of categories the rating is withheld. Bands are half-open:
// Very Favorable ≥ +10, Favorable [+3, +10), Even (−3, +3), Unfavorable (−10, −3],
// Very Unfavorable ≤ −10, each threshold per season. The officer's override sits beside the
// calculated rating and never replaces it.

package app

import "math"

// warConfig is a season's War settings; Saved is false when these are the defaults.
type warConfig struct {
	SeasonID            int     `json:"season_id"`
	WeightPower         int     `json:"weight_power"`
	WeightTop10         int     `json:"weight_top10"`
	WeightTop5          int     `json:"weight_top5"`
	WeightTop3          int     `json:"weight_top3"`
	BandVeryFavorable   float64 `json:"band_very_favorable"`
	BandFavorable       float64 `json:"band_favorable"`
	BandUnfavorable     float64 `json:"band_unfavorable"`
	BandVeryUnfavorable float64 `json:"band_very_unfavorable"`
	MinCategories       int     `json:"min_categories"`
	LootStepPct         int     `json:"loot_step_pct"`
	LootMaxPct          int     `json:"loot_max_pct"`
	ProductionStopWeek  *int    `json:"production_stop_week"`
	ProductionStopDay   *int    `json:"production_stop_day"`
	WarEventTypeID      *int    `json:"war_event_type_id"`
	Saved               bool    `json:"saved"`
}

func defaultWarConfig(seasonID int) warConfig {
	return warConfig{SeasonID: seasonID, WeightPower: 25, WeightTop10: 35, WeightTop5: 25, WeightTop3: 15,
		BandVeryFavorable: 10, BandFavorable: 3, BandUnfavorable: -3, BandVeryUnfavorable: -10,
		MinCategories: 2, LootStepPct: 3, LootMaxPct: 15}
}

// sideFigures are one side's four figures; nil is unknown, never zero.
type sideFigures struct {
	Power *int64 `json:"power"`
	Top10 *int64 `json:"top10_thp"`
	Top5  *int64 `json:"top5_thp"`
	Top3  *int64 `json:"top3_thp"`
}

type matchupCategory struct {
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	Ours         *int64   `json:"ours"`
	Theirs       *int64   `json:"theirs"`
	Pct          *float64 `json:"pct"`          // advantage, % (nil when the category is dropped)
	Weight       int      `json:"weight"`       // as configured
	Effective    float64  `json:"effective"`    // re-normalised, % of the used weight
	Contribution float64  `json:"contribution"` // Pct × Effective ÷ 100
}

type matchupResult struct {
	Categories []matchupCategory `json:"categories"`
	Used       int               `json:"used"`
	Score      *float64          `json:"score"`  // nil when withheld
	Rating     string            `json:"rating"` // band key, or insufficient_data
}

// computeMatchup scores our side against theirs.
func computeMatchup(ours, theirs sideFigures, cfg warConfig) matchupResult {
	cats := []matchupCategory{
		{Key: "power", Label: "Alliance power", Ours: ours.Power, Theirs: theirs.Power, Weight: cfg.WeightPower},
		{Key: "top10", Label: "Top 10 THP", Ours: ours.Top10, Theirs: theirs.Top10, Weight: cfg.WeightTop10},
		{Key: "top5", Label: "Top 5 THP", Ours: ours.Top5, Theirs: theirs.Top5, Weight: cfg.WeightTop5},
		{Key: "top3", Label: "Top 3 THP", Ours: ours.Top3, Theirs: theirs.Top3, Weight: cfg.WeightTop3},
	}
	used, totalWeight := 0, 0
	for i := range cats {
		c := &cats[i]
		if c.Ours == nil || c.Theirs == nil || *c.Theirs <= 0 || c.Weight <= 0 {
			continue
		}
		p := float64(*c.Ours-*c.Theirs) / float64(*c.Theirs) * 100
		c.Pct = &p
		used++
		totalWeight += c.Weight
	}
	out := matchupResult{Categories: cats, Used: used, Rating: "insufficient_data"}
	min := cfg.MinCategories
	if min < 1 {
		min = 1
	}
	if used < min || totalWeight == 0 {
		return out
	}
	score := 0.0
	for i := range cats {
		c := &cats[i]
		if c.Pct == nil {
			continue
		}
		c.Effective = float64(c.Weight) / float64(totalWeight) * 100
		c.Contribution = *c.Pct * c.Effective / 100
		score += c.Contribution
	}
	score = math.Round(score*100) / 100
	out.Score = &score
	out.Rating = matchupBand(score, cfg)
	return out
}

// matchupBand places a score in its half-open band.
func matchupBand(score float64, cfg warConfig) string {
	switch {
	case score >= cfg.BandVeryFavorable:
		return "very_favorable"
	case score >= cfg.BandFavorable:
		return "favorable"
	case score > cfg.BandUnfavorable:
		return "even"
	case score > cfg.BandVeryUnfavorable:
		return "unfavorable"
	}
	return "very_unfavorable"
}

// topNSum is the sum of the n largest values, or nil when fewer than n are known — a missing
// member's THP is unknown, not zero.
func topNSum(sorted []int64, n int) *int64 {
	if len(sorted) < n {
		return nil
	}
	var s int64
	for _, v := range sorted[:n] {
		s += v
	}
	return &s
}
