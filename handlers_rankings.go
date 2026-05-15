package main

import (
	"encoding/json"
	"net/http"
)

type GrowthData struct {
	MemberID         int    `json:"member_id"`
	Name             string `json:"name"`
	Rank             string `json:"rank"`
	TroopLevel       int    `json:"troop_level"`
	SquadType        string `json:"squad_type"`
	CurrentPower     int64  `json:"current_power"`
	Power7D          int64  `json:"power_7d"`
	Power30D         int64  `json:"power_30d"`
	Growth7D         int64  `json:"growth_7d"`
	Growth30D        int64  `json:"growth_30d"`
	CurrentHeroPower int64  `json:"current_hero_power"`
	HeroPower7D      int64  `json:"hero_power_7d"`
	HeroPower30D     int64  `json:"hero_power_30d"`
	HeroGrowth7D     int64  `json:"hero_growth_7d"`
	HeroGrowth30D    int64  `json:"hero_growth_30d"`
}

func getMemberRankings(w http.ResponseWriter, r *http.Request) {
	query := `
		SELECT
			m.id, m.name, m.rank, COALESCE(m.troop_level, 0), COALESCE(m.squad_type, ''),
			COALESCE((SELECT power FROM power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), 0) as current_power,
			COALESCE((SELECT power FROM power_history WHERE member_id = m.id AND recorded_at <= datetime('now', '-7 days') ORDER BY recorded_at DESC LIMIT 1), 0) as power_7d,
			COALESCE((SELECT power FROM power_history WHERE member_id = m.id AND recorded_at <= datetime('now', '-30 days') ORDER BY recorded_at DESC LIMIT 1), 0) as power_30d,
			COALESCE((SELECT power FROM hero_power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), 0) as current_hero_power,
			COALESCE((SELECT power FROM hero_power_history WHERE member_id = m.id AND recorded_at <= datetime('now', '-7 days') ORDER BY recorded_at DESC LIMIT 1), 0) as hero_power_7d,
			COALESCE((SELECT power FROM hero_power_history WHERE member_id = m.id AND recorded_at <= datetime('now', '-30 days') ORDER BY recorded_at DESC LIMIT 1), 0) as hero_power_30d
		FROM members m
		WHERE eligible = 1
		ORDER BY current_power DESC
	`

	rows, err := db.Query(query)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var growthList []GrowthData
	for rows.Next() {
		var g GrowthData
		if err := rows.Scan(
			&g.MemberID, &g.Name, &g.Rank, &g.TroopLevel, &g.SquadType,
			&g.CurrentPower, &g.Power7D, &g.Power30D,
			&g.CurrentHeroPower, &g.HeroPower7D, &g.HeroPower30D,
		); err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		if g.Power7D > 0 {
			g.Growth7D = g.CurrentPower - g.Power7D
		}
		if g.Power30D > 0 {
			g.Growth30D = g.CurrentPower - g.Power30D
		}
		if g.HeroPower7D > 0 {
			g.HeroGrowth7D = g.CurrentHeroPower - g.HeroPower7D
		}
		if g.HeroPower30D > 0 {
			g.HeroGrowth30D = g.CurrentHeroPower - g.HeroPower30D
		}

		growthList = append(growthList, g)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"growth_data": growthList,
	})
}

func getMemberTimelines(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{}`))
}
