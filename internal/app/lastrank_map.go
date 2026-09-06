// lastrank_map.go — the seam between internal/lastrank and this application.
//
// The client package returns upstream wire rows and knows nothing about our models.
// Everything that turns one into an app-facing type lives here, so the boundary stays
// one-directional: internal/lastrank never imports the app.

package app

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"lastwar-alliance/internal/lastrank"
)

// lastRankSearchAlliances runs the strict-server, fuzzy-name alliance search and maps
// the rows into the picker's shape.
func lastRankSearchAlliances(ctx context.Context, query string, server *int, limit int) ([]VSLeagueAllianceSearchResult, error) {
	rows, err := lastrank.SearchAlliances(ctx, query, server, limit)
	if err != nil {
		return nil, err
	}
	out := make([]VSLeagueAllianceSearchResult, 0, len(rows))
	for _, row := range rows {
		out = append(out, VSLeagueAllianceSearchResult{
			LastRankID: row.AllianceID,
			Tag:        row.Abbr,
			Name:       row.Name,
			Server:     row.ServerID,
			Power:      row.Power,
			Kills:      row.Kills,
			PowerRank:  row.PowerRank,
			KillsRank:  row.KillsRank,
			CapturedAt: row.CapturedAt,
		})
	}
	return out, nil
}

// lastRankSearchAllianceHits runs the site's own cross-server search and maps the hits
// into the same app-facing shape, so one picker can render either strategy. Power/kills
// are simply nil here, and a picked hit resolves its own details on the follow-up by-id
// fetch.
func lastRankSearchAllianceHits(ctx context.Context, query string, limit int) ([]VSLeagueAllianceSearchResult, error) {
	if limit <= 0 {
		limit = 20
	}
	hits, err := lastrank.SearchAllianceHits(ctx, query)
	if err != nil {
		return nil, err
	}
	return mapLastRankSearchHits(hits, limit), nil
}

// mapLastRankSearchHits converts raw search hits to the app-facing shape. Split out from
// the fetch so its filtering rules are testable without touching the network.
func mapLastRankSearchHits(hits []lastrank.SearchHit, limit int) []VSLeagueAllianceSearchResult {
	out := make([]VSLeagueAllianceSearchResult, 0, len(hits))
	for _, h := range hits {
		// kind is requested as "alliance", but the upstream is a volunteer service whose
		// shape can drift — never let a player hit render as an alliance, and never emit a
		// row with no id, which would produce an unpickable entry.
		if h.Kind != "alliance" || strings.TrimSpace(h.ID) == "" {
			continue
		}
		out = append(out, VSLeagueAllianceSearchResult{
			LastRankID: h.ID,
			Tag:        h.Abbr,
			Name:       h.Name,
			Server:     h.ServerID,
			Power:      h.Power,
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// lastRankSearchPlayers finds players by fuzzy name and maps them into the recruiting
// picker's shape.
func lastRankSearchPlayers(ctx context.Context, query string, server *int, limit int) ([]LastRankPlayerSearchResult, error) {
	rows, err := lastrank.SearchPlayers(ctx, query, server, limit)
	if err != nil {
		return nil, err
	}
	out := make([]LastRankPlayerSearchResult, 0, len(rows))
	for _, row := range rows {
		out = append(out, LastRankPlayerSearchResult{
			PublicID:     row.PublicID,
			Name:         row.Name,
			Server:       row.ServerID,
			AllianceTag:  row.AllianceAbbr,
			AllianceName: row.AllianceName,
			Country:      row.Country,
			Power:        row.Power,
			Kills:        row.Kills,
			HeroPower:    row.THP,
			PhotoURL:     row.PhotoURL,
			CapturedAt:   row.CapturedAt,
		})
	}
	return out, nil
}

// lastRankPlayerBulk is the bulk strategy with this install's operator-set freshness
// window applied. The package clamps anything below lastrank.MinEnrichAge, so a
// misconfigured setting can never turn a roster sweep into an enrich per member.
func lastRankPlayerBulk(ctx context.Context, publicID int) (*lastrank.Player, error) {
	return lastrank.PlayerBulk(ctx, publicID, lastRankEnrichMaxAge())
}

// fetchLastRankOpponentSnapshot resolves a pasted URL/id to a point-in-time opponent
// snapshot for the VS League matchup card. Surfaces power (fightpower) and kills
// (army_kill), which the app-facing LastRankAllianceMeta drops. The caller is responsible
// for a bounded context and must hold no DB handle across the call.
func fetchLastRankOpponentSnapshot(ctx context.Context, idOrURL string) (VSLeagueOpponentSnapshot, error) {
	id, ok := lastrank.ParseAllianceIDStrict(idOrURL)
	if !ok {
		return VSLeagueOpponentSnapshot{}, lastrank.ErrBadInput
	}
	a, err := lastrank.FetchAlliance(ctx, id)
	if err != nil {
		return VSLeagueOpponentSnapshot{}, err
	}
	return VSLeagueOpponentSnapshot{
		AllianceID:  a.AllianceID,
		Tag:         a.Abbr,
		Name:        a.Name,
		ServerID:    a.ServerID,
		Power:       a.Fightpower,
		Kills:       a.ArmyKill,
		MemberCount: a.CurMember,
		LastSeenAt:  a.LastSeenAt,
	}, nil
}

// fetchLastRankOpponentRoster resolves a pasted URL/id to the opponent alliance's member
// list (names + power) for the daily-MVP picker. Same strict parse as the snapshot; the
// caller owns a bounded context and holds no DB handle across the call.
func fetchLastRankOpponentRoster(ctx context.Context, idOrURL string) ([]VSLeagueOpponentMember, error) {
	id, ok := lastrank.ParseAllianceIDStrict(idOrURL)
	if !ok {
		return nil, lastrank.ErrBadInput
	}
	a, err := lastrank.FetchAlliance(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]VSLeagueOpponentMember, 0, len(a.Members))
	for _, m := range a.Members {
		pw := m.Power
		out = append(out, VSLeagueOpponentMember{Name: m.Name, Power: &pw, AllianceRank: m.AllianceRank})
	}
	return out, nil
}

// lastRankRankToString maps lastrank's alliance_rank int (5=R5 … 1=R1, nil=unranked)
// to our TEXT rank values. Returns "" for unranked / out-of-range.
func lastRankRankToString(r *int) string {
	if r == nil {
		return ""
	}
	switch *r {
	case 5, 4, 3, 2, 1:
		return "R" + strconv.Itoa(*r)
	default:
		return ""
	}
}

// slogLastRank is a tiny helper so every upstream failure is logged consistently
// server-side while the handler returns a generic message to the client.
func slogLastRank(msg string, err error) {
	slog.Error(msg, "error", err)
}
