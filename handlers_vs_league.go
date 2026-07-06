package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// handlers_vs_league.go — VS Duel League tracker.
//
// Design invariants (see the plan):
//   - FKs are NOT enforced in this app, so deletions cascade EXPLICITLY in handlers and MVP
//     display LEFT JOINs members with an mvp_name fallback.
//   - Weekly Match Points / outcome / clinch are COMPUTED ON READ from day rows
//     (computeWeekStanding); our_points/opponent_points/outcome are stored only for
//     summary-only weeks (no day rows).
//   - The app runs on a single DB connection; the opponent-lookup handler holds NO DB handle
//     across the LastRank call.
//   - Leadership fields (strategy_label/strategy_result/notes) are omitted for view-only users.

const (
	entityVSLeagueSeason = "vs_league_season"
	entityVSLeagueWeek   = "vs_league_week"
)

// isUniqueConflict reports whether an error is a SQLite UNIQUE-constraint violation
// (mapped to 409, matching handlers_schedule.go).
func isUniqueConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint")
}

func badRequest(w http.ResponseWriter, msg string) { http.Error(w, msg, http.StatusBadRequest) }

func dbError(w http.ResponseWriter, where string, err error) {
	slog.Error(where, "error", err)
	http.Error(w, "Database error", http.StatusInternalServerError)
}

// ── Seasons ─────────────────────────────────────────────────────────────────

func activeVSLeagueSeasonID() (int, bool) {
	var id int
	if err := db.QueryRow(`SELECT id FROM vs_league_seasons WHERE is_active = 1 LIMIT 1`).Scan(&id); err != nil {
		return 0, false
	}
	return id, true
}

func scanVSLeagueSeason(s interface{ Scan(...any) error }) (VSLeagueSeason, error) {
	var v VSLeagueSeason
	err := s.Scan(&v.ID, &v.SeasonNumber, &v.LeagueTier, &v.StartDate, &v.EndDate,
		&v.FinalRank, &v.IsActive, &v.ArchivedAt, &v.Notes, &v.CreatedAt)
	return v, err
}

func getVSLeagueSeasons(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, season_number, league_tier, start_date, end_date,
		final_rank, is_active, archived_at, notes, created_at
		FROM vs_league_seasons ORDER BY is_active DESC, season_number DESC`)
	if err != nil {
		dbError(w, "getVSLeagueSeasons query", err)
		return
	}
	defer rows.Close()
	seasons := []VSLeagueSeason{}
	for rows.Next() {
		s, err := scanVSLeagueSeason(rows)
		if err != nil {
			dbError(w, "getVSLeagueSeasons scan", err)
			return
		}
		seasons = append(seasons, s)
	}
	writeJSON(w, seasons)
}

type vsLeagueSeasonPayload struct {
	SeasonNumber int     `json:"season_number"`
	LeagueTier   *string `json:"league_tier"`
	StartDate    *string `json:"start_date"`
	Notes        *string `json:"notes"`
}

func createVSLeagueSeason(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var p vsLeagueSeasonPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if p.SeasonNumber <= 0 {
		badRequest(w, "season_number is required")
		return
	}

	tx, err := db.Begin()
	if err != nil {
		dbError(w, "createVSLeagueSeason begin", err)
		return
	}
	defer tx.Rollback()

	// Archive the prior active season and insert the new active one atomically so there is
	// never zero or two active seasons (also backstopped by the partial unique index).
	if _, err := tx.Exec(`UPDATE vs_league_seasons SET is_active = 0,
		archived_at = CURRENT_TIMESTAMP, end_date = COALESCE(end_date, ?) WHERE is_active = 1`, gameDate()); err != nil {
		dbError(w, "createVSLeagueSeason archive", err)
		return
	}
	res, err := tx.Exec(`INSERT INTO vs_league_seasons (season_number, league_tier, start_date, notes, is_active)
		VALUES (?, ?, ?, ?, 1)`, p.SeasonNumber, p.LeagueTier, p.StartDate, p.Notes)
	if err != nil {
		if isUniqueConflict(err) {
			http.Error(w, "A League season with that number already exists", http.StatusConflict)
			return
		}
		dbError(w, "createVSLeagueSeason insert", err)
		return
	}
	if err := tx.Commit(); err != nil {
		dbError(w, "createVSLeagueSeason commit", err)
		return
	}
	id, _ := res.LastInsertId()
	logActivity(user.ID, user.Username, "created", entityVSLeagueSeason, "S"+strconv.Itoa(p.SeasonNumber), false)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

type vsLeagueSeasonUpdate struct {
	LeagueTier *string `json:"league_tier"`
	StartDate  *string `json:"start_date"`
	EndDate    *string `json:"end_date"`
	FinalRank  *int    `json:"final_rank"`
	Notes      *string `json:"notes"`
	Archive    bool    `json:"archive"`
	Activate   bool    `json:"activate"`
}

func updateVSLeagueSeason(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		badRequest(w, "Invalid id")
		return
	}
	var p vsLeagueSeasonUpdate
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if p.FinalRank != nil && (*p.FinalRank < 1 || *p.FinalRank > 16) {
		badRequest(w, "final_rank must be 1-16")
		return
	}

	var num int
	if err := db.QueryRow(`SELECT season_number FROM vs_league_seasons WHERE id = ?`, id).Scan(&num); err != nil {
		http.Error(w, "Season not found", http.StatusNotFound)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		dbError(w, "updateVSLeagueSeason begin", err)
		return
	}
	defer tx.Rollback()

	if p.Activate {
		if _, err := tx.Exec(`UPDATE vs_league_seasons SET is_active = 0,
			archived_at = COALESCE(archived_at, CURRENT_TIMESTAMP), end_date = COALESCE(end_date, ?)
			WHERE is_active = 1 AND id != ?`, gameDate(), id); err != nil {
			dbError(w, "updateVSLeagueSeason deactivate others", err)
			return
		}
	}
	_, err = tx.Exec(`UPDATE vs_league_seasons SET
		league_tier = COALESCE(?, league_tier),
		start_date  = COALESCE(?, start_date),
		end_date    = COALESCE(?, end_date),
		final_rank  = COALESCE(?, final_rank),
		notes       = COALESCE(?, notes),
		is_active   = CASE WHEN ? THEN 0 WHEN ? THEN 1 ELSE is_active END,
		archived_at = CASE WHEN ? THEN CURRENT_TIMESTAMP ELSE archived_at END
		WHERE id = ?`,
		p.LeagueTier, p.StartDate, p.EndDate, p.FinalRank, p.Notes,
		p.Archive, p.Activate, p.Archive, id)
	if err != nil {
		if isUniqueConflict(err) {
			http.Error(w, "Another season is already active", http.StatusConflict)
			return
		}
		dbError(w, "updateVSLeagueSeason update", err)
		return
	}
	if err := tx.Commit(); err != nil {
		dbError(w, "updateVSLeagueSeason commit", err)
		return
	}
	action := "updated"
	if p.Archive {
		action = "archived"
	}
	logActivity(user.ID, user.Username, action, entityVSLeagueSeason, "S"+strconv.Itoa(num), false)
	w.WriteHeader(http.StatusOK)
}

// ── Weeks (+ days computed standing) ────────────────────────────────────────

// loadVSLeagueWeek loads one week with its days and computed standing. canManage controls
// whether the leadership fields (strategy/notes) are populated in the response.
func loadVSLeagueWeek(id int, canManage bool) (*VSLeagueWeek, error) {
	row := db.QueryRow(`SELECT id, season_id, week_number, week_date, league_tier, league_rank,
		opponent_tag, opponent_name, opponent_server, opponent_lastrank_id,
		opponent_power, opponent_kills, opponent_member_count, opponent_snapshot_at, opponent_lastrank_seen_at,
		our_points, opponent_points, outcome, strategy_label, strategy_result, notes, created_at, updated_at
		FROM vs_league_weeks WHERE id = ?`, id)
	return scanVSLeagueWeek(row, canManage)
}

func scanVSLeagueWeek(row interface{ Scan(...any) error }, canManage bool) (*VSLeagueWeek, error) {
	var wk VSLeagueWeek
	var storedOur, storedOpp *int
	var storedOutcome, strategyLabel, strategyResult, notes *string
	err := row.Scan(&wk.ID, &wk.SeasonID, &wk.WeekNumber, &wk.WeekDate, &wk.LeagueTier, &wk.LeagueRank,
		&wk.OpponentTag, &wk.OpponentName, &wk.OpponentServer, &wk.OpponentLastRankID,
		&wk.OpponentPower, &wk.OpponentKills, &wk.OpponentMemberCount, &wk.OpponentSnapshotAt, &wk.OpponentLastRankSeenAt,
		&storedOur, &storedOpp, &storedOutcome, &strategyLabel, &strategyResult, &notes, &wk.CreatedAt, &wk.UpdatedAt)
	if err != nil {
		return nil, err
	}
	// Leadership fields only for manage users (F-R09/F-013).
	if canManage {
		wk.StrategyLabel, wk.StrategyResult, wk.Notes = strategyLabel, strategyResult, notes
	}
	days, err := loadVSLeagueDays(wk.ID)
	if err != nil {
		return nil, err
	}
	wk.Days = days
	if len(days) > 0 {
		var arr [6]string
		for i := range arr {
			arr[i] = "pending"
		}
		for _, d := range days {
			if d.DayNumber >= 1 && d.DayNumber <= 6 {
				arr[d.DayNumber-1] = d.Outcome
			}
		}
		wk.Standing = computeWeekStanding(arr)
		wk.SummaryOnly = false
	} else {
		// Summary-only: use the stored weekly values (no day detail to derive from).
		wk.SummaryOnly = true
		st := VSLeagueWeekStanding{Outcome: "pending"}
		if storedOur != nil {
			st.OurPoints = *storedOur
		}
		if storedOpp != nil {
			st.OpponentPoints = *storedOpp
		}
		if storedOutcome != nil {
			st.Outcome = *storedOutcome
			st.Decided = true
		}
		wk.Standing = st
	}
	return &wk, nil
}

func loadVSLeagueDays(weekID int) ([]VSLeagueDay, error) {
	rows, err := db.Query(`SELECT id, week_id, day_number, our_score, opponent_score, outcome,
		mvp_is_ours, mvp_member_id, mvp_name
		FROM vs_league_days WHERE week_id = ? ORDER BY day_number`, weekID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	days := []VSLeagueDay{}
	for rows.Next() {
		var d VSLeagueDay
		if err := rows.Scan(&d.ID, &d.WeekID, &d.DayNumber, &d.OurScore, &d.OpponentScore,
			&d.Outcome, &d.MVPIsOurs, &d.MVPMemberID, &d.MVPName); err != nil {
			return nil, err
		}
		d.Points = vsDayMatchPoints(d.DayNumber)
		days = append(days, d)
	}
	return days, nil
}

func loadWeeksForSeason(seasonID int, canManage bool) ([]VSLeagueWeek, error) {
	rows, err := db.Query(`SELECT id FROM vs_league_weeks WHERE season_id = ? ORDER BY week_number, week_date`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	weeks := []VSLeagueWeek{}
	for _, id := range ids {
		wk, err := loadVSLeagueWeek(id, canManage)
		if err != nil {
			return nil, err
		}
		weeks = append(weeks, *wk)
	}
	return weeks, nil
}

func getVSLeagueWeeks(w http.ResponseWriter, r *http.Request) {
	canManage := userHasPermission(getAuthUser(r), "manage_vs_points")
	sidParam := r.URL.Query().Get("season_id")
	var seasonID int
	if sidParam == "active" {
		id, ok := activeVSLeagueSeasonID()
		if !ok {
			writeJSON(w, []VSLeagueWeek{})
			return
		}
		seasonID = id
	} else {
		id, err := strconv.Atoi(sidParam)
		if err != nil {
			badRequest(w, "season_id is required")
			return
		}
		seasonID = id
	}
	weeks, err := loadWeeksForSeason(seasonID, canManage)
	if err != nil {
		dbError(w, "getVSLeagueWeeks", err)
		return
	}
	writeJSON(w, weeks)
}

// getVSLeagueCurrent returns the active season + its weeks in one call (avoids discover-then-fetch).
func getVSLeagueCurrent(w http.ResponseWriter, r *http.Request) {
	canManage := userHasPermission(getAuthUser(r), "manage_vs_points")
	sid, ok := activeVSLeagueSeasonID()
	if !ok {
		writeJSON(w, map[string]any{"season": nil, "weeks": []VSLeagueWeek{}, "current_week_date": currentVSWeekMonday()})
		return
	}
	season, err := scanVSLeagueSeason(db.QueryRow(`SELECT id, season_number, league_tier, start_date, end_date,
		final_rank, is_active, archived_at, notes, created_at FROM vs_league_seasons WHERE id = ?`, sid))
	if err != nil {
		dbError(w, "getVSLeagueCurrent season", err)
		return
	}
	weeks, err := loadWeeksForSeason(sid, canManage)
	if err != nil {
		dbError(w, "getVSLeagueCurrent weeks", err)
		return
	}
	writeJSON(w, map[string]any{
		"season":            season,
		"weeks":             weeks,
		"current_week_date": currentVSWeekMonday(),
	})
}

type vsLeagueWeekPayload struct {
	SeasonID       int     `json:"season_id"`
	WeekNumber     *int    `json:"week_number"`
	WeekDate       string  `json:"week_date"`
	LeagueTier     *string `json:"league_tier"`
	LeagueRank     *int    `json:"league_rank"`
	OpponentTag    *string `json:"opponent_tag"`
	OpponentName   *string `json:"opponent_name"`
	OpponentServer *int    `json:"opponent_server"`
	// Opponent snapshot (confirmed from an opponent-lookup).
	OpponentLastRankID     *string `json:"opponent_lastrank_id"`
	OpponentPower          *int64  `json:"opponent_power"`
	OpponentKills          *int64  `json:"opponent_kills"`
	OpponentMemberCount    *int    `json:"opponent_member_count"`
	OpponentLastRankSeenAt *string `json:"opponent_lastrank_seen_at"`
	SnapshotNow            bool    `json:"snapshot_now"` // stamp opponent_snapshot_at = now
	// Summary-only weekly result (allowed ONLY when the week has no day rows).
	OurPoints      *int    `json:"our_points"`
	OpponentPoints *int    `json:"opponent_points"`
	Outcome        *string `json:"outcome"`
	// Strategy context.
	StrategyLabel  *string `json:"strategy_label"`
	StrategyResult *string `json:"strategy_result"`
	Notes          *string `json:"notes"`
}

var validStrategyLabel = map[string]bool{"push": true, "save": true, "normal": true, "test": true, "recovery": true}
var validStrategyResult = map[string]bool{"worked": true, "failed": true, "mixed": true}

func (p *vsLeagueWeekPayload) validate() string {
	if p.StrategyLabel != nil && *p.StrategyLabel != "" && !validStrategyLabel[*p.StrategyLabel] {
		return "strategy_label must be one of push/save/normal/test/recovery"
	}
	if p.StrategyResult != nil && *p.StrategyResult != "" && !validStrategyResult[*p.StrategyResult] {
		return "strategy_result must be one of worked/failed/mixed"
	}
	if p.Outcome != nil {
		switch *p.Outcome {
		case "win", "loss", "tie":
		case "pending", "":
			return "week outcome is win/loss/tie; leave it empty for pending"
		default:
			return "week outcome must be win/loss/tie"
		}
	}
	if p.OurPoints != nil && (*p.OurPoints < 0 || *p.OurPoints > 13) {
		return "our_points must be 0-13"
	}
	if p.OpponentPoints != nil && (*p.OpponentPoints < 0 || *p.OpponentPoints > 13) {
		return "opponent_points must be 0-13"
	}
	return ""
}

func weekHasDayRows(weekID int) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM vs_league_days WHERE week_id = ?`, weekID).Scan(&n)
	return n > 0, err
}

// createVSLeagueWeek upserts a week matchup (unique on season_id + normalized week_date).
func createVSLeagueWeek(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var p vsLeagueWeekPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if p.SeasonID <= 0 || p.WeekDate == "" {
		badRequest(w, "season_id and week_date are required")
		return
	}
	if msg := p.validate(); msg != "" {
		badRequest(w, msg)
		return
	}
	weekDate, err := normalizeToGameWeekMonday(p.WeekDate)
	if err != nil {
		badRequest(w, "invalid week_date")
		return
	}

	var snapshotAt *string
	if p.SnapshotNow {
		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		snapshotAt = &now
	}

	res, err := db.Exec(`INSERT INTO vs_league_weeks
		(season_id, week_number, week_date, league_tier, league_rank,
		 opponent_tag, opponent_name, opponent_server,
		 opponent_lastrank_id, opponent_power, opponent_kills, opponent_member_count,
		 opponent_snapshot_at, opponent_lastrank_seen_at,
		 our_points, opponent_points, outcome, strategy_label, strategy_result, notes)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(season_id, week_date) DO UPDATE SET
		 week_number = COALESCE(excluded.week_number, week_number),
		 league_tier = COALESCE(excluded.league_tier, league_tier),
		 league_rank = COALESCE(excluded.league_rank, league_rank),
		 opponent_tag = COALESCE(excluded.opponent_tag, opponent_tag),
		 opponent_name = COALESCE(excluded.opponent_name, opponent_name),
		 opponent_server = COALESCE(excluded.opponent_server, opponent_server),
		 opponent_lastrank_id = COALESCE(excluded.opponent_lastrank_id, opponent_lastrank_id),
		 opponent_power = COALESCE(excluded.opponent_power, opponent_power),
		 opponent_kills = COALESCE(excluded.opponent_kills, opponent_kills),
		 opponent_member_count = COALESCE(excluded.opponent_member_count, opponent_member_count),
		 opponent_snapshot_at = COALESCE(excluded.opponent_snapshot_at, opponent_snapshot_at),
		 opponent_lastrank_seen_at = COALESCE(excluded.opponent_lastrank_seen_at, opponent_lastrank_seen_at),
		 strategy_label = COALESCE(excluded.strategy_label, strategy_label),
		 strategy_result = COALESCE(excluded.strategy_result, strategy_result),
		 notes = COALESCE(excluded.notes, notes),
		 updated_at = CURRENT_TIMESTAMP`,
		p.SeasonID, p.WeekNumber, weekDate, p.LeagueTier, p.LeagueRank,
		p.OpponentTag, p.OpponentName, p.OpponentServer,
		p.OpponentLastRankID, p.OpponentPower, p.OpponentKills, p.OpponentMemberCount,
		snapshotAt, p.OpponentLastRankSeenAt,
		p.OurPoints, p.OpponentPoints, p.Outcome, p.StrategyLabel, p.StrategyResult, p.Notes)
	if err != nil {
		if isUniqueConflict(err) {
			http.Error(w, "That week number is already used in this season", http.StatusConflict)
			return
		}
		dbError(w, "createVSLeagueWeek upsert", err)
		return
	}
	id, _ := res.LastInsertId()
	logActivity(user.ID, user.Username, "updated", entityVSLeagueWeek, weekLabel(p.WeekNumber, weekDate), false)
	writeJSON(w, map[string]any{"id": id, "week_date": weekDate})
}

func weekLabel(num *int, weekDate string) string {
	if num != nil {
		return "Week " + strconv.Itoa(*num)
	}
	return "Week of " + weekDate
}

func updateVSLeagueWeek(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		badRequest(w, "Invalid id")
		return
	}
	var p vsLeagueWeekPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if msg := p.validate(); msg != "" {
		badRequest(w, msg)
		return
	}

	var weekNum *int
	var weekDate string
	if err := db.QueryRow(`SELECT week_number, week_date FROM vs_league_weeks WHERE id = ?`, id).Scan(&weekNum, &weekDate); err != nil {
		http.Error(w, "Week not found", http.StatusNotFound)
		return
	}

	// Manual weekly points are only allowed on summary-only weeks (F-R07/derive-and-lock).
	if p.OurPoints != nil || p.OpponentPoints != nil || p.Outcome != nil {
		hasDays, err := weekHasDayRows(id)
		if err != nil {
			dbError(w, "updateVSLeagueWeek hasDays", err)
			return
		}
		if hasDays {
			badRequest(w, "weekly points/outcome are derived from day data; clear the days to enter a summary result")
			return
		}
	}

	var snapshotAt *string
	if p.SnapshotNow {
		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		snapshotAt = &now
	}

	_, err = db.Exec(`UPDATE vs_league_weeks SET
		week_number = COALESCE(?, week_number),
		league_tier = COALESCE(?, league_tier),
		league_rank = COALESCE(?, league_rank),
		opponent_tag = COALESCE(?, opponent_tag),
		opponent_name = COALESCE(?, opponent_name),
		opponent_server = COALESCE(?, opponent_server),
		opponent_lastrank_id = COALESCE(?, opponent_lastrank_id),
		opponent_power = COALESCE(?, opponent_power),
		opponent_kills = COALESCE(?, opponent_kills),
		opponent_member_count = COALESCE(?, opponent_member_count),
		opponent_snapshot_at = COALESCE(?, opponent_snapshot_at),
		opponent_lastrank_seen_at = COALESCE(?, opponent_lastrank_seen_at),
		our_points = COALESCE(?, our_points),
		opponent_points = COALESCE(?, opponent_points),
		outcome = COALESCE(?, outcome),
		strategy_label = COALESCE(?, strategy_label),
		strategy_result = COALESCE(?, strategy_result),
		notes = COALESCE(?, notes),
		updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		p.WeekNumber, p.LeagueTier, p.LeagueRank, p.OpponentTag, p.OpponentName, p.OpponentServer,
		p.OpponentLastRankID, p.OpponentPower, p.OpponentKills, p.OpponentMemberCount,
		snapshotAt, p.OpponentLastRankSeenAt,
		p.OurPoints, p.OpponentPoints, p.Outcome, p.StrategyLabel, p.StrategyResult, p.Notes, id)
	if err != nil {
		if isUniqueConflict(err) {
			http.Error(w, "That week number is already used in this season", http.StatusConflict)
			return
		}
		dbError(w, "updateVSLeagueWeek", err)
		return
	}
	logActivity(user.ID, user.Username, "updated", entityVSLeagueWeek, weekLabel(weekNum, weekDate), false)
	w.WriteHeader(http.StatusOK)
}

// deleteVSLeagueWeek deletes a week and its children EXPLICITLY (FKs are inert).
func deleteVSLeagueWeek(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		badRequest(w, "Invalid id")
		return
	}
	var weekNum *int
	var weekDate string
	if err := db.QueryRow(`SELECT week_number, week_date FROM vs_league_weeks WHERE id = ?`, id).Scan(&weekNum, &weekDate); err != nil {
		http.Error(w, "Week not found", http.StatusNotFound)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		dbError(w, "deleteVSLeagueWeek begin", err)
		return
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM vs_league_matchups WHERE week_id = ?`,
		`DELETE FROM vs_league_days WHERE week_id = ?`,
		`DELETE FROM vs_league_weeks WHERE id = ?`,
	} {
		if _, err := tx.Exec(q, id); err != nil {
			dbError(w, "deleteVSLeagueWeek exec", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		dbError(w, "deleteVSLeagueWeek commit", err)
		return
	}
	logActivity(user.ID, user.Username, "deleted", entityVSLeagueWeek, weekLabel(weekNum, weekDate), false)
	w.WriteHeader(http.StatusOK)
}

// ── Day batch ───────────────────────────────────────────────────────────────

type vsLeagueDayPayload struct {
	DayNumber     int     `json:"day_number"`
	OurScore      *int64  `json:"our_score"`
	OpponentScore *int64  `json:"opponent_score"`
	Outcome       string  `json:"outcome"`
	MVPIsOurs     *bool   `json:"mvp_is_ours"`
	MVPName       *string `json:"mvp_name"`
}

// dayCarriesInfo reports whether a day row is worth persisting. An all-pending, empty row is
// dropped so it can't silently flip a summary-only week to day-backed (F-R16).
func (d vsLeagueDayPayload) carriesInfo() bool {
	o := normalizeDayOutcome(d.Outcome)
	return o != "pending" || d.OurScore != nil || d.OpponentScore != nil ||
		(d.MVPName != nil && strings.TrimSpace(*d.MVPName) != "")
}

func saveVSLeagueDays(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	weekID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		badRequest(w, "Invalid id")
		return
	}
	var weekNum *int
	var weekDate string
	if err := db.QueryRow(`SELECT week_number, week_date FROM vs_league_weeks WHERE id = ?`, weekID).Scan(&weekNum, &weekDate); err != nil {
		http.Error(w, "Week not found", http.StatusNotFound)
		return
	}
	var payload struct {
		Days []vsLeagueDayPayload `json:"days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	for _, d := range payload.Days {
		if d.DayNumber < 1 || d.DayNumber > 6 {
			badRequest(w, "day_number must be 1-6")
			return
		}
		switch normalizeDayOutcome(d.Outcome) {
		case "win", "loss", "tie", "pending":
		default:
			badRequest(w, "outcome must be win/loss/tie/pending")
			return
		}
	}

	tx, err := db.Begin()
	if err != nil {
		dbError(w, "saveVSLeagueDays begin", err)
		return
	}
	defer tx.Rollback()

	for _, d := range payload.Days {
		// Normalize outcome from raw scores when both are present (F-R07).
		outcome := normalizeDayOutcome(d.Outcome)
		if d.OurScore != nil && d.OpponentScore != nil {
			derived := deriveDayOutcome(int(*d.OurScore), int(*d.OpponentScore))
			if outcome != "pending" && outcome != derived {
				badRequest(w, "day "+strconv.Itoa(d.DayNumber)+": outcome contradicts the entered scores")
				return
			}
			outcome = derived
		}

		if !d.carriesInfo() {
			// Drop an empty/pure-pending day so "has day rows" stays meaningful (F-R16).
			if _, err := tx.Exec(`DELETE FROM vs_league_days WHERE week_id = ? AND day_number = ?`, weekID, d.DayNumber); err != nil {
				dbError(w, "saveVSLeagueDays delete", err)
				return
			}
			continue
		}

		mvpIsOurs := true
		if d.MVPIsOurs != nil {
			mvpIsOurs = *d.MVPIsOurs
		}
		var mvpMemberID *int
		var mvpName *string
		if d.MVPName != nil && strings.TrimSpace(*d.MVPName) != "" {
			name := strings.TrimSpace(*d.MVPName)
			mvpName = &name
			if mvpIsOurs {
				if m, _, aerr := resolveMemberAlias(tx, name, user.ID); aerr == nil && m != nil {
					id := m.ID
					mvpMemberID = &id
				}
			}
		}

		if _, err := tx.Exec(`INSERT INTO vs_league_days
			(week_id, day_number, our_score, opponent_score, outcome, mvp_is_ours, mvp_member_id, mvp_name, updated_at)
			VALUES (?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)
			ON CONFLICT(week_id, day_number) DO UPDATE SET
			 our_score = excluded.our_score,
			 opponent_score = excluded.opponent_score,
			 outcome = excluded.outcome,
			 mvp_is_ours = excluded.mvp_is_ours,
			 mvp_member_id = excluded.mvp_member_id,
			 mvp_name = excluded.mvp_name,
			 updated_at = CURRENT_TIMESTAMP`,
			weekID, d.DayNumber, d.OurScore, d.OpponentScore, outcome, boolToInt(mvpIsOurs), mvpMemberID, mvpName); err != nil {
			dbError(w, "saveVSLeagueDays upsert", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		dbError(w, "saveVSLeagueDays commit", err)
		return
	}
	logActivity(user.ID, user.Username, "updated", entityVSLeagueWeek, weekLabel(weekNum, weekDate), false, "daily results")
	// Return the refreshed week so the client can re-render the standing.
	canManage := userHasPermission(user, "manage_vs_points")
	if wk, err := loadVSLeagueWeek(weekID, canManage); err == nil {
		writeJSON(w, wk)
	} else {
		w.WriteHeader(http.StatusOK)
	}
}

// ── Matchups (bracket) ──────────────────────────────────────────────────────

func getVSLeagueMatchups(w http.ResponseWriter, r *http.Request) {
	weekID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		badRequest(w, "Invalid id")
		return
	}
	rows, err := db.Query(`SELECT id, week_id, match_index, a_rank, a_server, a_tag, a_name, a_points,
		b_rank, b_server, b_tag, b_name, b_points, is_ours
		FROM vs_league_matchups WHERE week_id = ? ORDER BY match_index`, weekID)
	if err != nil {
		dbError(w, "getVSLeagueMatchups", err)
		return
	}
	defer rows.Close()
	ms := []VSLeagueMatchup{}
	for rows.Next() {
		var m VSLeagueMatchup
		if err := rows.Scan(&m.ID, &m.WeekID, &m.MatchIndex, &m.ARank, &m.AServer, &m.ATag, &m.AName, &m.APoints,
			&m.BRank, &m.BServer, &m.BTag, &m.BName, &m.BPoints, &m.IsOurs); err != nil {
			dbError(w, "getVSLeagueMatchups scan", err)
			return
		}
		ms = append(ms, m)
	}
	writeJSON(w, ms)
}

type vsLeagueMatchupPayload struct {
	MatchIndex *int    `json:"match_index"`
	ARank      *int    `json:"a_rank"`
	AServer    *int    `json:"a_server"`
	ATag       *string `json:"a_tag"`
	AName      *string `json:"a_name"`
	APoints    *int    `json:"a_points"`
	BRank      *int    `json:"b_rank"`
	BServer    *int    `json:"b_server"`
	BTag       *string `json:"b_tag"`
	BName      *string `json:"b_name"`
	BPoints    *int    `json:"b_points"`
	IsOurs     bool    `json:"is_ours"`
}

// saveVSLeagueMatchups batch-replaces a week's bracket. Validation runs BEFORE the tx; the tx
// only DELETEs then INSERTs (no I/O), keeping the single write lock brief.
func saveVSLeagueMatchups(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	weekID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		badRequest(w, "Invalid id")
		return
	}
	var weekNum *int
	var weekDate string
	if err := db.QueryRow(`SELECT week_number, week_date FROM vs_league_weeks WHERE id = ?`, weekID).Scan(&weekNum, &weekDate); err != nil {
		http.Error(w, "Week not found", http.StatusNotFound)
		return
	}
	var payload struct {
		Matchups []vsLeagueMatchupPayload `json:"matchups"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if len(payload.Matchups) > 8 {
		badRequest(w, "at most 8 pairings per week")
		return
	}
	seenIdx := map[int]bool{}
	oursCount := 0
	inRange := func(p *int, lo, hi int) bool { return p == nil || (*p >= lo && *p <= hi) }
	for i, m := range payload.Matchups {
		idx := i + 1
		if m.MatchIndex != nil {
			idx = *m.MatchIndex
		}
		if idx < 1 || idx > 8 || seenIdx[idx] {
			badRequest(w, "match_index values must be unique and 1-8")
			return
		}
		seenIdx[idx] = true
		if !inRange(m.ARank, 1, 16) || !inRange(m.BRank, 1, 16) {
			badRequest(w, "ranks must be 1-16")
			return
		}
		if !inRange(m.APoints, 0, 13) || !inRange(m.BPoints, 0, 13) {
			badRequest(w, "points must be 0-13")
			return
		}
		if m.APoints != nil && m.BPoints != nil && *m.APoints+*m.BPoints > 13 {
			badRequest(w, "a matchup's points can't sum to more than 13")
			return
		}
		if m.IsOurs {
			oursCount++
		}
	}
	if oursCount > 1 {
		badRequest(w, "only one pairing can be marked as ours")
		return
	}

	tx, err := db.Begin()
	if err != nil {
		dbError(w, "saveVSLeagueMatchups begin", err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM vs_league_matchups WHERE week_id = ?`, weekID); err != nil {
		dbError(w, "saveVSLeagueMatchups delete", err)
		return
	}
	for i, m := range payload.Matchups {
		idx := i + 1
		if m.MatchIndex != nil {
			idx = *m.MatchIndex
		}
		if _, err := tx.Exec(`INSERT INTO vs_league_matchups
			(week_id, match_index, a_rank, a_server, a_tag, a_name, a_points,
			 b_rank, b_server, b_tag, b_name, b_points, is_ours)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			weekID, idx, m.ARank, m.AServer, m.ATag, m.AName, m.APoints,
			m.BRank, m.BServer, m.BTag, m.BName, m.BPoints, boolToInt(m.IsOurs)); err != nil {
			dbError(w, "saveVSLeagueMatchups insert", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		dbError(w, "saveVSLeagueMatchups commit", err)
		return
	}
	logActivity(user.ID, user.Username, "updated", entityVSLeagueWeek, weekLabel(weekNum, weekDate), false, "bracket match record")
	w.WriteHeader(http.StatusOK)
}

// ── Participation (live, from vs_points) ────────────────────────────────────

func getVSLeagueParticipation(w http.ResponseWriter, r *http.Request) {
	weekDateParam := r.URL.Query().Get("week_date")
	if weekDateParam == "" {
		badRequest(w, "week_date is required")
		return
	}
	weekDate, err := normalizeToGameWeekMonday(weekDateParam)
	if err != nil {
		badRequest(w, "invalid week_date")
		return
	}
	imported := vsDayImportMask(weekDate)
	// completedVSDays only bounds the CURRENT week; past weeks are fully bounded by their data.
	rows, err := db.Query(`SELECT COALESCE(m.joined_at, ''),
		COALESCE(vp.monday,0), COALESCE(vp.tuesday,0), COALESCE(vp.wednesday,0),
		COALESCE(vp.thursday,0), COALESCE(vp.friday,0), COALESCE(vp.saturday,0)
		FROM members m
		LEFT JOIN vs_points vp ON vp.member_id = m.id AND vp.week_date = ?
		WHERE m.rank != 'EX'`, weekDate)
	if err != nil {
		dbError(w, "getVSLeagueParticipation", err)
		return
	}
	defer rows.Close()

	type memberRow struct {
		joinedAt string
		scores   [6]int
	}
	var members []memberRow
	for rows.Next() {
		var mr memberRow
		if err := rows.Scan(&mr.joinedAt, &mr.scores[0], &mr.scores[1], &mr.scores[2],
			&mr.scores[3], &mr.scores[4], &mr.scores[5]); err != nil {
			dbError(w, "getVSLeagueParticipation scan", err)
			return
		}
		members = append(members, mr)
	}

	out := make([]VSLeagueParticipationDay, 6)
	for i := 0; i < 6; i++ {
		day := VSLeagueParticipationDay{DayNumber: i + 1, Imported: imported[i]}
		if !imported[i] {
			out[i] = day
			continue
		}
		dDate := dayDate(weekDate, i)
		var active []int
		sum := 0
		for _, mr := range members {
			joinedBy := mr.joinedAt == "" || mr.joinedAt <= dDate
			if mr.scores[i] > 0 {
				active = append(active, mr.scores[i])
				sum += mr.scores[i]
			} else if joinedBy {
				day.ZeroScore++
			}
		}
		day.ActiveScorers = len(active)
		if len(active) > 0 {
			day.AvgPerActive = float64(sum) / float64(len(active))
			sort.Sort(sort.Reverse(sort.IntSlice(active)))
			top := 0
			for k := 0; k < len(active) && k < 10; k++ {
				top += active[k]
			}
			if sum > 0 {
				day.Top10Pct = float64(top) / float64(sum) * 100
			}
		}
		out[i] = day
	}
	writeJSON(w, out)
}

// ── Opponent lookup (LastRank) ──────────────────────────────────────────────

func vsLeagueOpponentLookup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if strings.TrimSpace(body.URL) == "" {
		badRequest(w, "Paste the opponent's lastrank.fun link or alliance id")
		return
	}
	// Single DB connection: hold NO DB handle across the network call (no DB work before/during).
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	snap, err := fetchLastRankOpponentSnapshot(ctx, body.URL)
	if err != nil {
		if errors.Is(err, errLastRankBadInput) {
			badRequest(w, "That doesn't look like a lastrank.fun alliance link or id")
			return
		}
		slogLastRank("vs-league opponent lookup failed", err)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			http.Error(w, "LastRank is busy right now — try again in a moment", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "Could not reach LastRank for that alliance", http.StatusBadGateway)
		return
	}
	// Now that the network call is done, persist to the opponent cache (best-effort).
	cacheExternalAlliance(snap)
	writeJSON(w, snap)
}

// cacheExternalAlliance upserts a looked-up alliance into the persistent cache, keyed on the
// stable LastRank id so a re-lookup refreshes in place. Best-effort — never fails the response.
func cacheExternalAlliance(snap VSLeagueOpponentSnapshot) {
	if snap.AllianceID == "" {
		return
	}
	res, err := db.Exec(`UPDATE external_alliances SET tag=?, name=?, server=?, power=?, kills=?,
		member_count=?, lastrank_seen_at=?, updated_at=CURRENT_TIMESTAMP WHERE lastrank_id=?`,
		snap.Tag, snap.Name, snap.ServerID, snap.Power, snap.Kills, snap.MemberCount, snap.LastSeenAt, snap.AllianceID)
	if err != nil {
		slog.Error("cacheExternalAlliance update", "error", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := db.Exec(`INSERT INTO external_alliances
			(lastrank_id, tag, name, server, power, kills, member_count, lastrank_seen_at)
			VALUES (?,?,?,?,?,?,?,?)`,
			snap.AllianceID, snap.Tag, snap.Name, snap.ServerID, snap.Power, snap.Kills, snap.MemberCount, snap.LastSeenAt); err != nil {
			slog.Error("cacheExternalAlliance insert", "error", err)
		}
	}
}

// getExternalAlliances returns the cached opposing alliances (optionally filtered by ?tag=),
// most-recently-updated first — used to prefill the opponent fields by tag.
func getExternalAlliances(w http.ResponseWriter, r *http.Request) {
	q := `SELECT id, lastrank_id, tag, name, server, power, kills, member_count, lastrank_seen_at, updated_at
		FROM external_alliances`
	var args []any
	if tag := strings.TrimSpace(r.URL.Query().Get("tag")); tag != "" {
		q += ` WHERE tag = ?`
		args = append(args, tag)
	}
	q += ` ORDER BY updated_at DESC`
	rows, err := db.Query(q, args...)
	if err != nil {
		dbError(w, "getExternalAlliances", err)
		return
	}
	defer rows.Close()
	list := []ExternalAlliance{}
	for rows.Next() {
		var a ExternalAlliance
		if err := rows.Scan(&a.ID, &a.LastRankID, &a.Tag, &a.Name, &a.Server, &a.Power,
			&a.Kills, &a.MemberCount, &a.LastSeenAt, &a.UpdatedAt); err != nil {
			dbError(w, "getExternalAlliances scan", err)
			return
		}
		list = append(list, a)
	}
	writeJSON(w, list)
}
