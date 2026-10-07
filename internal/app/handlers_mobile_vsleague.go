// handlers_mobile_vsleague.go - the mobile VS Duel League store: a week's game-read
// fields (league, opponent, both sides' power, kills and member count), its day results
// and its bracket, in one write. Strategy, strategy result, notes and the summary-only
// points or outcome are officer judgement and stay web-only, as do LastRank ids.

package app

import (
	"net/http"
	"strings"
)

// mobileLeagueWeekShape is a week as the mobile API shows it: game-read fields only.
func mobileLeagueWeekShape(wk *VSLeagueWeek, matchups []VSLeagueMatchup) map[string]any {
	days := []map[string]any{}
	for _, d := range wk.Days {
		days = append(days, map[string]any{"day_number": d.DayNumber, "our_score": d.OurScore,
			"opponent_score": d.OpponentScore, "outcome": d.Outcome, "mvp_is_ours": d.MVPIsOurs, "mvp_name": d.MVPName})
	}
	return map[string]any{
		"id": wk.ID, "week_number": wk.WeekNumber, "week_date": wk.WeekDate,
		"league_tier": wk.LeagueTier, "league_rank": wk.LeagueRank,
		"opponent_tag": wk.OpponentTag, "opponent_name": wk.OpponentName, "opponent_server": wk.OpponentServer,
		"opponent_power": wk.OpponentPower, "opponent_kills": wk.OpponentKills, "opponent_member_count": wk.OpponentMemberCount,
		"our_power": wk.OurPower, "our_kills": wk.OurKills, "our_member_count": wk.OurMemberCount, "our_server": wk.OurServer,
		"days": days, "matchups": matchups,
	}
}

func loadLeagueMatchups(weekID int) ([]VSLeagueMatchup, error) {
	rows, err := db.Query(`SELECT id, week_id, match_index, a_rank, a_server, a_tag, a_name, a_points,
		b_rank, b_server, b_tag, b_name, b_points, is_ours
		FROM vs_league_matchups WHERE week_id = ? ORDER BY match_index`, weekID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ms := []VSLeagueMatchup{}
	for rows.Next() {
		var m VSLeagueMatchup
		if err := rows.Scan(&m.ID, &m.WeekID, &m.MatchIndex, &m.ARank, &m.AServer, &m.ATag, &m.AName, &m.APoints,
			&m.BRank, &m.BServer, &m.BTag, &m.BName, &m.BPoints, &m.IsOurs); err != nil {
			return nil, err
		}
		ms = append(ms, m)
	}
	return ms, rows.Err()
}

// mobileLeagueWeek loads a week in the mobile shape, or nil when the active season has no
// week for weekDate.
func mobileLeagueWeek(seasonID int, weekDate string) (map[string]any, error) {
	var id int
	if err := db.QueryRow(`SELECT id FROM vs_league_weeks WHERE season_id = ? AND week_date = ?`, seasonID, weekDate).Scan(&id); err != nil {
		return nil, nil
	}
	wk, err := loadVSLeagueWeek(id, false)
	if err != nil {
		return nil, err
	}
	ms, err := loadLeagueMatchups(id)
	if err != nil {
		return nil, err
	}
	return mobileLeagueWeekShape(wk, ms), nil
}

// GET /api/mobile/vs-league/current (view_vs_points)
func getMobileVSLeagueCurrent(w http.ResponseWriter, r *http.Request) {
	cur := currentVSWeekMonday()
	sid, ok := activeVSLeagueSeasonID()
	if !ok {
		writeJSON(w, map[string]any{"season": nil, "current_week_date": cur, "week": nil})
		return
	}
	season, err := scanVSLeagueSeason(db.QueryRow(`SELECT id, season_number, league_tier, start_date, end_date,
		final_rank, is_active, archived_at, notes, created_at FROM vs_league_seasons WHERE id = ?`, sid))
	if err != nil {
		dbError(w, "getMobileVSLeagueCurrent season", err)
		return
	}
	week, err := mobileLeagueWeek(sid, cur)
	if err != nil {
		dbError(w, "getMobileVSLeagueCurrent week", err)
		return
	}
	writeJSON(w, map[string]any{
		"season": map[string]any{"id": season.ID, "season_number": season.SeasonNumber, "league_tier": season.LeagueTier,
			"start_date": season.StartDate, "end_date": season.EndDate},
		"current_week_date": cur,
		"week":              week,
	})
}

// POST /api/mobile/vs-league/week (manage_vs_points)
// Keyed by week_date in the active season; every part optional. A field left out keeps
// its stored value; matchups, when present, are the whole bracket; a side's snapshot time
// is stamped when any of its power, kills or member count is sent. One transaction.
func postMobileVSLeagueWeek(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var req struct {
		WeekDate string `json:"week_date"`
		Week     *struct {
			WeekNumber          *int    `json:"week_number"`
			LeagueTier          *string `json:"league_tier"`
			LeagueRank          *int    `json:"league_rank"`
			OpponentTag         *string `json:"opponent_tag"`
			OpponentName        *string `json:"opponent_name"`
			OpponentServer      *int    `json:"opponent_server"`
			OpponentPower       *int64  `json:"opponent_power"`
			OpponentKills       *int64  `json:"opponent_kills"`
			OpponentMemberCount *int    `json:"opponent_member_count"`
			OurPower            *int64  `json:"our_power"`
			OurKills            *int64  `json:"our_kills"`
			OurMemberCount      *int    `json:"our_member_count"`
			OurServer           *int    `json:"our_server"`
		} `json:"week"`
		Days     []vsLeagueDayPayload     `json:"days"`
		Matchups *[]vsLeagueMatchupPayload `json:"matchups"`
	}
	if !decodeMobileJSON(w, r, &req) {
		return
	}
	weekDate, err := normalizeToGameWeekMonday(req.WeekDate)
	if err != nil {
		badRequest(w, "Invalid week_date: must be YYYY-MM-DD")
		return
	}
	sid, ok := activeVSLeagueSeasonID()
	if !ok {
		http.Error(w, "No active VS League season — start one on the web", http.StatusConflict)
		return
	}
	p := vsLeagueWeekPayload{SeasonID: sid, WeekDate: weekDate}
	var parts []string
	if wk := req.Week; wk != nil {
		p.WeekNumber, p.LeagueTier, p.LeagueRank = wk.WeekNumber, wk.LeagueTier, wk.LeagueRank
		p.OpponentTag, p.OpponentName, p.OpponentServer = wk.OpponentTag, wk.OpponentName, wk.OpponentServer
		p.OpponentPower, p.OpponentKills, p.OpponentMemberCount = wk.OpponentPower, wk.OpponentKills, wk.OpponentMemberCount
		p.OurPower, p.OurKills, p.OurMemberCount, p.OurServer = wk.OurPower, wk.OurKills, wk.OurMemberCount, wk.OurServer
		p.SnapshotNow = wk.OpponentPower != nil || wk.OpponentKills != nil || wk.OpponentMemberCount != nil
		p.OurSnapshotNow = wk.OurPower != nil || wk.OurKills != nil || wk.OurMemberCount != nil
		parts = append(parts, "week")
	}
	if msg := validateLeagueDays(req.Days); msg != "" {
		badRequest(w, msg)
		return
	}
	if len(req.Days) > 0 {
		parts = append(parts, "daily results")
	}
	if req.Matchups != nil {
		if msg := validateLeagueMatchups(*req.Matchups); msg != "" {
			badRequest(w, msg)
			return
		}
		parts = append(parts, "bracket")
	}

	tx, err := db.Begin()
	if err != nil {
		dbError(w, "postMobileVSLeagueWeek begin", err)
		return
	}
	defer tx.Rollback()
	weekID, err := upsertLeagueWeekTx(tx, p, weekDate)
	if err != nil {
		if isUniqueConflict(err) {
			http.Error(w, "That week number is already used in this season", http.StatusConflict)
			return
		}
		dbError(w, "postMobileVSLeagueWeek upsert", err)
		return
	}
	if err := saveLeagueDaysTx(tx, weekID, req.Days, user.ID); err != nil {
		dbError(w, "postMobileVSLeagueWeek days", err)
		return
	}
	if req.Matchups != nil {
		if err := replaceLeagueMatchupsTx(tx, weekID, *req.Matchups); err != nil {
			dbError(w, "postMobileVSLeagueWeek matchups", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		dbError(w, "postMobileVSLeagueWeek commit", err)
		return
	}
	details := "· via mobile"
	if len(parts) > 0 {
		details = strings.Join(parts, ", ") + " " + details
	}
	var weekNum *int
	db.QueryRow(`SELECT week_number FROM vs_league_weeks WHERE id = ?`, weekID).Scan(&weekNum)
	logActivity(user.ID, user.Username, "updated", entityVSLeagueWeek, weekLabel(weekNum, weekDate), false, details)

	out, err := mobileLeagueWeek(sid, weekDate)
	if err != nil {
		dbError(w, "postMobileVSLeagueWeek reload", err)
		return
	}
	writeJSON(w, out)
}
