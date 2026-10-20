// handlers_mobile_season_war.go — the mobile API's season_war store (private-docs 271, 277,
// 278). The game shows which faction an alliance is in, its power, members and hero power,
// war results and Trade Post owners; a phone that reads them writes them here. Plans and
// judgement (strategy, terms, overrides) have no mobile path.

package app

import (
	"log/slog"
	"net/http"
)

// GET /api/mobile/season-hub/war (view_season_war) — the active season's factions and alliance
// rows, so a phone can match what it reads to a registry identity (tag AND server).
func getMobileSeasonWar(w http.ResponseWriter, r *http.Request) {
	s, err := loadActiveSeason()
	if err != nil {
		slog.Error("getMobileSeasonWar: season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if s == nil {
		writeJSON(w, map[string]any{"season": nil, "factions": []any{}, "alliances": []any{}})
		return
	}
	factions, err := loadSeasonFactions(db, s.ID)
	if err != nil {
		slog.Error("getMobileSeasonWar: factions", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	rows, err := loadSeasonAlliances(db, s.ID)
	if err != nil {
		slog.Error("getMobileSeasonWar: alliances", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	type mobileSeasonAlliance struct {
		ID           int    `json:"id"`
		Tag          string `json:"tag"`
		Name         string `json:"name"`
		Server       int    `json:"server"`
		FactionKey   string `json:"faction_key"`
		Relationship string `json:"relationship"`
	}
	out := make([]mobileSeasonAlliance, 0, len(rows))
	for _, a := range rows {
		out = append(out, mobileSeasonAlliance{ID: a.ID, Tag: a.Tag, Name: a.Name, Server: a.Server,
			FactionKey: a.FactionKey, Relationship: a.Relationship})
	}
	writeJSON(w, map[string]any{
		"season":    map[string]any{"id": s.ID, "name": s.Name, "season_number": s.SeasonNumber, "start_date": s.StartDate, "week_count": s.WeekCount},
		"factions":  factions,
		"alliances": out,
	})
}
