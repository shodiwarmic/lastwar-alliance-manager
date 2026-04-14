// handlers_season_hub.go — Season Hub: participation tracking, contribution scores,
// reward logging, and season mail for structured in-game season management.

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type Season struct {
	ID               int          `json:"id"`
	Name             string       `json:"name"`
	SeasonNumber     int          `json:"season_number"`
	StartDate        string       `json:"start_date"`
	EndDate          string       `json:"end_date"`
	WeekCount        int          `json:"week_count"`
	KeyEventName     string       `json:"key_event_name"`
	KeyEventRequired int          `json:"key_event_required"`
	TierActiveMinPct int          `json:"tier_active_min_pct"`
	TierAtRiskMinPct int          `json:"tier_at_risk_min_pct"`
	TierCountLeader  int          `json:"tier_count_leader"`
	TierCountCore    int          `json:"tier_count_core"`
	TierCountElite   int          `json:"tier_count_elite"`
	TierCountValued  int          `json:"tier_count_valued"`
	IsActive         bool         `json:"is_active"`
	ArchivedAt       string       `json:"archived_at"`
	CreatedAt        string       `json:"created_at"`
	ScoreLevels      []ScoreLevel `json:"score_levels"`
}

type ScoreLevel struct {
	ID        int    `json:"id"`
	SeasonID  int    `json:"season_id"`
	Key       string `json:"key"`
	Label     string `json:"label"`
	Points    int    `json:"points"`
	SortOrder int    `json:"sort_order"`
}

type SeasonMember struct {
	MemberID           int      `json:"member_id"`
	Name               string   `json:"name"`
	Rank               string   `json:"rank"`
	ParticipationPts   int      `json:"participation_pts"`
	ParticipationPct   float64  `json:"participation_pct"`
	ContributionTotal  int64    `json:"contribution_total"`
	ContributionPct    float64  `json:"contribution_pct"`
	KeyEventAttendance int      `json:"key_event_attendance"`
	KeyEventEligible   bool     `json:"key_event_eligible"`
	WeeklyScores       []string `json:"weekly_scores"`    // len=week_count, e.g. ["full","absent",...]
	WeeklyKeyEvent     []int    `json:"weekly_key_event"` // key event count per week (may be >1)
	RewardTier         string   `json:"reward_tier"`      // "" if unassigned
	ClassTag           string   `json:"class_tag"`        // "Active Member","At Risk / Inconsistent","Dead Weight"
}

type ParticipationEntry struct {
	MemberID         int    `json:"member_id"`
	Score            string `json:"score"`
	AttendedKeyEvent int    `json:"attended_key_event"` // count, not bool — key event may occur >1× per week
	Note             string `json:"note"`
}

type ContributionImportRow struct {
	OriginalName  string  `json:"original_name"`
	MemberID      int     `json:"member_id"`
	MemberName    string  `json:"member_name"`
	MemberRank    string  `json:"member_rank"`
	MatchType     string  `json:"match_type"`
	Points        int64   `json:"points"`
}

type SeasonReward struct {
	ID               int     `json:"id"`
	SeasonID         int     `json:"season_id"`
	MemberID         int     `json:"member_id"`
	MemberName       string  `json:"member_name"`
	MemberRank       string  `json:"member_rank"`
	RewardTier       string  `json:"reward_tier"`
	ParticipationPct float64 `json:"participation_pct"`
	ContributionPct  float64 `json:"contribution_pct"`
	Note             string  `json:"note"`
	LoggedBy         string  `json:"logged_by"`
	LoggedAt         string  `json:"logged_at"`
}

type SeasonMailItem struct {
	ID       int    `json:"id"`
	SeasonID int    `json:"season_id"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	PostedBy string `json:"posted_by"`
	PostedAt string `json:"posted_at"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// loadActiveSeason returns the active season and its score levels.
// Returns nil season (not an error) if no active season exists.
func loadActiveSeason() (*Season, error) {
	var s Season
	var archivedAt, endDate sql.NullString
	err := db.QueryRow(`
		SELECT id, name, season_number, start_date, COALESCE(end_date,''), week_count,
		       key_event_name, key_event_required,
		       tier_active_min_pct, tier_at_risk_min_pct,
		       tier_count_leader, tier_count_core, tier_count_elite, tier_count_valued,
		       is_active, COALESCE(archived_at,''), created_at
		FROM seasons WHERE is_active = 1 LIMIT 1`,
	).Scan(&s.ID, &s.Name, &s.SeasonNumber, &s.StartDate, &endDate,
		&s.WeekCount, &s.KeyEventName, &s.KeyEventRequired,
		&s.TierActiveMinPct, &s.TierAtRiskMinPct,
		&s.TierCountLeader, &s.TierCountCore, &s.TierCountElite, &s.TierCountValued,
		&s.IsActive, &archivedAt, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if endDate.Valid {
		s.EndDate = endDate.String
	}
	if archivedAt.Valid {
		s.ArchivedAt = archivedAt.String
	}

	rows, err := db.Query(`SELECT id, season_id, key, label, points, sort_order
		FROM season_score_levels WHERE season_id = ? ORDER BY sort_order ASC`, s.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sl ScoreLevel
		if err := rows.Scan(&sl.ID, &sl.SeasonID, &sl.Key, &sl.Label, &sl.Points, &sl.SortOrder); err != nil {
			continue
		}
		s.ScoreLevels = append(s.ScoreLevels, sl)
	}
	return &s, nil
}

// loadSeasonByID loads a season by ID including its score levels.
func loadSeasonByID(id int) (*Season, error) {
	var s Season
	var archivedAt, endDate sql.NullString
	err := db.QueryRow(`
		SELECT id, name, season_number, start_date, COALESCE(end_date,''), week_count,
		       key_event_name, key_event_required,
		       tier_active_min_pct, tier_at_risk_min_pct,
		       tier_count_leader, tier_count_core, tier_count_elite, tier_count_valued,
		       is_active, COALESCE(archived_at,''), created_at
		FROM seasons WHERE id = ?`, id,
	).Scan(&s.ID, &s.Name, &s.SeasonNumber, &s.StartDate, &endDate,
		&s.WeekCount, &s.KeyEventName, &s.KeyEventRequired,
		&s.TierActiveMinPct, &s.TierAtRiskMinPct,
		&s.TierCountLeader, &s.TierCountCore, &s.TierCountElite, &s.TierCountValued,
		&s.IsActive, &archivedAt, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	if endDate.Valid {
		s.EndDate = endDate.String
	}
	if archivedAt.Valid {
		s.ArchivedAt = archivedAt.String
	}
	rows, err := db.Query(`SELECT id, season_id, key, label, points, sort_order
		FROM season_score_levels WHERE season_id = ? ORDER BY sort_order ASC`, s.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sl ScoreLevel
		if err := rows.Scan(&sl.ID, &sl.SeasonID, &sl.Key, &sl.Label, &sl.Points, &sl.SortOrder); err != nil {
			continue
		}
		s.ScoreLevels = append(s.ScoreLevels, sl)
	}
	return &s, nil
}


// autoActivateUpcomingSeason checks whether any pending (inactive, non-archived) season
// has reached its start date and, if so, archives the current active season and activates
// the pending one. Called at the start of read endpoints so activation is seamless.
func autoActivateUpcomingSeason() {
	var pendingID int
	err := db.QueryRow(`
		SELECT id FROM seasons
		WHERE is_active = 0 AND archived_at IS NULL AND start_date <= date('now')
		ORDER BY start_date ASC LIMIT 1`).Scan(&pendingID)
	if err != nil || pendingID == 0 {
		return
	}
	tx, err := db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	// Archive the outgoing active season
	tx.Exec(`UPDATE seasons SET is_active = 0,
		archived_at = CURRENT_TIMESTAMP,
		end_date = COALESCE(end_date, date('now'))
		WHERE is_active = 1`)
	// Activate the incoming season
	tx.Exec(`UPDATE seasons SET is_active = 1 WHERE id = ?`, pendingID)
	tx.Commit()
	slog.Info("autoActivateUpcomingSeason: activated season", "id", pendingID)
}

// ---------------------------------------------------------------------------
// Season management
// ---------------------------------------------------------------------------

// handleSeasonList returns all seasons ordered newest-first, for the season selector.
func handleSeasonList(w http.ResponseWriter, r *http.Request) {
	autoActivateUpcomingSeason()
	data := getPageData(r, "", "")
	if !data.IsAuthenticated {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	type SeasonSummary struct {
		ID           int    `json:"id"`
		Name         string `json:"name"`
		SeasonNumber int    `json:"season_number"`
		IsActive     bool   `json:"is_active"`
		ArchivedAt   string `json:"archived_at"`
	}
	rows, err := db.Query(`SELECT id, name, season_number, is_active, COALESCE(archived_at,'')
		FROM seasons ORDER BY start_date DESC`)
	if err != nil {
		slog.Error("handleSeasonList: query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	seasons := []SeasonSummary{}
	for rows.Next() {
		var s SeasonSummary
		var isActive int
		if err := rows.Scan(&s.ID, &s.Name, &s.SeasonNumber, &isActive, &s.ArchivedAt); err != nil {
			continue
		}
		s.IsActive = isActive == 1
		seasons = append(seasons, s)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"seasons": seasons})
}

func handleSeasonCreate(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)

	var body struct {
		Name             string       `json:"name"`
		SeasonNumber     int          `json:"season_number"`
		StartDate        string       `json:"start_date"`
		WeekCount        int          `json:"week_count"`
		KeyEventName     string       `json:"key_event_name"`
		KeyEventRequired int          `json:"key_event_required"`
		TierActiveMinPct int          `json:"tier_active_min_pct"`
		TierAtRiskMinPct int          `json:"tier_at_risk_min_pct"`
		TierCountLeader  int          `json:"tier_count_leader"`
		TierCountCore    int          `json:"tier_count_core"`
		TierCountElite   int          `json:"tier_count_elite"`
		TierCountValued  int          `json:"tier_count_valued"`
		ScoreLevels      []ScoreLevel `json:"score_levels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.Name == "" || body.SeasonNumber == 0 || body.StartDate == "" {
		http.Error(w, "name, season_number, and start_date are required", http.StatusBadRequest)
		return
	}

	// Apply defaults for omitted config fields
	if body.WeekCount == 0 {
		body.WeekCount = 8
	}
	if body.KeyEventName == "" {
		body.KeyEventName = "Rare Soil War"
	}
	if body.KeyEventRequired == 0 {
		body.KeyEventRequired = 4
	}
	if body.TierActiveMinPct == 0 {
		body.TierActiveMinPct = 70
	}
	if body.TierAtRiskMinPct == 0 {
		body.TierAtRiskMinPct = 60
	}
	if body.TierCountLeader == 0 {
		body.TierCountLeader = 1
	}
	if body.TierCountCore == 0 {
		body.TierCountCore = 10
	}
	if body.TierCountElite == 0 {
		body.TierCountElite = 20
	}
	if body.TierCountValued == 0 {
		body.TierCountValued = 69
	}

	// Default score levels (Season II values) if not provided
	if len(body.ScoreLevels) == 0 {
		body.ScoreLevels = []ScoreLevel{
			{Key: "full", Label: "FULL", Points: 10, SortOrder: 0},
			{Key: "partial", Label: "PARTIAL", Points: 5, SortOrder: 1},
			{Key: "absent", Label: "ABSENT", Points: 0, SortOrder: 2},
		}
	}

	// Block duplicate season numbers before starting the transaction
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM seasons WHERE season_number = ?`, body.SeasonNumber).Scan(&existing); err != nil {
		slog.Error("handleSeasonCreate: check duplicate", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if existing > 0 {
		http.Error(w, fmt.Sprintf("Season number %d already exists", body.SeasonNumber), http.StatusConflict)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleSeasonCreate: begin tx", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// If the new season starts on or before today, archive the previous active season
	// and make this one active immediately.
	// If the start date is in the future, the existing active season stays active;
	// autoActivateUpcomingSeason will handle the transition when the date arrives.
	today := time.Now().Format("2006-01-02")
	isFuture := body.StartDate > today
	if !isFuture {
		if _, err := tx.Exec(`UPDATE seasons SET is_active = 0,
			archived_at = CURRENT_TIMESTAMP,
			end_date = COALESCE(end_date, ?)
			WHERE is_active = 1`, body.StartDate); err != nil {
			slog.Error("handleSeasonCreate: archive existing", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	newIsActive := 1
	if isFuture {
		newIsActive = 0
	}
	res, err := tx.Exec(`
		INSERT INTO seasons (name, season_number, start_date, week_count, key_event_name, key_event_required,
		    tier_active_min_pct, tier_at_risk_min_pct,
		    tier_count_leader, tier_count_core, tier_count_elite, tier_count_valued, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		body.Name, body.SeasonNumber, body.StartDate, body.WeekCount,
		body.KeyEventName, body.KeyEventRequired,
		body.TierActiveMinPct, body.TierAtRiskMinPct,
		body.TierCountLeader, body.TierCountCore, body.TierCountElite, body.TierCountValued,
		newIsActive,
	)
	if err != nil {
		slog.Error("handleSeasonCreate: insert season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	seasonID, _ := res.LastInsertId()

	for _, sl := range body.ScoreLevels {
		if _, err := tx.Exec(`INSERT INTO season_score_levels (season_id, key, label, points, sort_order) VALUES (?, ?, ?, ?, ?)`,
			seasonID, sl.Key, sl.Label, sl.Points, sl.SortOrder); err != nil {
			slog.Error("handleSeasonCreate: insert score level", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		slog.Error("handleSeasonCreate: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	logActivity(userID, username, "created", "season_config", body.Name, false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": seasonID, "message": "Season created"})
}

func handleSeasonArchive(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)

	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid season ID", http.StatusBadRequest)
		return
	}

	s, err := loadSeasonByID(id)
	if err == sql.ErrNoRows {
		http.Error(w, "Season not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleSeasonArchive: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if s.ArchivedAt != "" {
		http.Error(w, "Season is already archived", http.StatusConflict)
		return
	}

	today := time.Now().UTC().Format("2006-01-02")
	if _, err := db.Exec(`UPDATE seasons SET is_active=0, archived_at=CURRENT_TIMESTAMP, end_date=? WHERE id=?`, today, id); err != nil {
		slog.Error("handleSeasonArchive: update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	logActivity(userID, username, "archived", "season_config", s.Name, false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Season archived"})
}

func handleSeasonUpdate(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)

	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid season ID", http.StatusBadRequest)
		return
	}

	old, err := loadSeasonByID(id)
	if err == sql.ErrNoRows {
		http.Error(w, "Season not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleSeasonUpdate: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var body struct {
		Name             string `json:"name"`
		SeasonNumber     int    `json:"season_number"`
		StartDate        string `json:"start_date"`
		WeekCount        int    `json:"week_count"`
		KeyEventName     string `json:"key_event_name"`
		KeyEventRequired int    `json:"key_event_required"`
		TierActiveMinPct int    `json:"tier_active_min_pct"`
		TierAtRiskMinPct int    `json:"tier_at_risk_min_pct"`
		TierCountLeader  int    `json:"tier_count_leader"`
		TierCountCore    int    `json:"tier_count_core"`
		TierCountElite   int    `json:"tier_count_elite"`
		TierCountValued  int    `json:"tier_count_valued"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.Name == "" || body.SeasonNumber == 0 || body.StartDate == "" {
		http.Error(w, "name, season_number, and start_date are required", http.StatusBadRequest)
		return
	}

	// Block duplicate season number on a different row
	var dupID int
	if err := db.QueryRow(`SELECT id FROM seasons WHERE season_number = ? AND id != ?`, body.SeasonNumber, id).Scan(&dupID); err == nil {
		http.Error(w, fmt.Sprintf("Season number %d already exists", body.SeasonNumber), http.StatusConflict)
		return
	}

	if _, err := db.Exec(`
		UPDATE seasons SET name=?, season_number=?, start_date=?, week_count=?,
		    key_event_name=?, key_event_required=?,
		    tier_active_min_pct=?, tier_at_risk_min_pct=?,
		    tier_count_leader=?, tier_count_core=?, tier_count_elite=?, tier_count_valued=?
		WHERE id=?`,
		body.Name, body.SeasonNumber, body.StartDate, body.WeekCount,
		body.KeyEventName, body.KeyEventRequired,
		body.TierActiveMinPct, body.TierAtRiskMinPct,
		body.TierCountLeader, body.TierCountCore, body.TierCountElite, body.TierCountValued,
		id,
	); err != nil {
		slog.Error("handleSeasonUpdate: update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var changes []string
	if old.Name != body.Name {
		changes = append(changes, "name: "+old.Name+" → "+body.Name)
	}
	if old.StartDate != body.StartDate {
		changes = append(changes, "start_date: "+old.StartDate+" → "+body.StartDate)
	}
	if old.WeekCount != body.WeekCount {
		changes = append(changes, fmt.Sprintf("week_count: %d → %d", old.WeekCount, body.WeekCount))
	}
	logActivity(userID, username, "updated", "season_config", body.Name, false, strings.Join(changes, "; "))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Season updated"})
}

func handleSeasonDelete(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)

	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid season ID", http.StatusBadRequest)
		return
	}

	s, err := loadSeasonByID(id)
	if err == sql.ErrNoRows {
		http.Error(w, "Season not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleSeasonDelete: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if s.IsActive {
		http.Error(w, "Cannot delete an active season — archive it first", http.StatusConflict)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleSeasonDelete: begin tx", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Child rows cascade via ON DELETE CASCADE; season_rewards and season_mail do not — delete explicitly
	for _, tbl := range []string{"season_participation", "season_contributions", "season_score_levels", "season_rewards", "season_mail"} {
		if _, err := tx.Exec(`DELETE FROM `+tbl+` WHERE season_id = ?`, id); err != nil {
			slog.Error("handleSeasonDelete: delete children", "table", tbl, "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if _, err := tx.Exec(`DELETE FROM seasons WHERE id = ?`, id); err != nil {
		slog.Error("handleSeasonDelete: delete season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handleSeasonDelete: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	logActivity(userID, username, "deleted", "season_config", s.Name, false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Season deleted"})
}

// ---------------------------------------------------------------------------
// Main data endpoint
// ---------------------------------------------------------------------------

// handleSeasonHubData returns the active season config + all member standings.
// R1–R3 (non-managers) receive only their own row — enforced server-side.
func handleSeasonHubData(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "", "")
	if !data.IsAuthenticated {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	canManage := data.Permissions.ManageSeasonHub

	var season *Season
	var err error
	if sidStr := r.URL.Query().Get("season_id"); sidStr != "" {
		sid, _ := strconv.Atoi(sidStr)
		season, err = loadSeasonByID(sid)
		if err == sql.ErrNoRows {
			http.Error(w, "Season not found", http.StatusNotFound)
			return
		}
	} else {
		season, err = loadActiveSeason()
	}
	if err != nil {
		slog.Error("handleSeasonHubData: load season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if season == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"season": nil, "members": []any{}})
		return
	}

	// Build score key → points lookup
	scorePts := map[string]int{}
	maxPts := 0
	for _, sl := range season.ScoreLevels {
		scorePts[sl.Key] = sl.Points
		if sl.Points > maxPts {
			maxPts = sl.Points
		}
	}
	maxSeasonPts := season.WeekCount * maxPts
	if maxSeasonPts == 0 {
		maxSeasonPts = 1 // avoid division by zero
	}

	// Load all participation rows for this season
	type partRow struct {
		memberID       int
		weekNumber     int
		scoreKey       string
		attendedKeyEvt int // count, not bool
	}
	partRows := []partRow{}
	rows, err := db.Query(`SELECT member_id, week_number, score, attended_key_event
		FROM season_participation WHERE season_id = ?`, season.ID)
	if err != nil {
		slog.Error("handleSeasonHubData: participation query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var pr partRow
		if err := rows.Scan(&pr.memberID, &pr.weekNumber, &pr.scoreKey, &pr.attendedKeyEvt); err != nil {
			continue
		}
		partRows = append(partRows, pr)
	}
	rows.Close()

	// Load season-end contribution snapshots (week_number=0 is the canonical tie-breaker source).
	// Fall back to summing weekly rows if no season-end snapshot exists.
	type contribTotals struct {
		total int64
	}
	contribByMember := map[int]int64{}

	// Try season-end snapshots first (week_number=0)
	crows, err := db.Query(`SELECT member_id,
		mutual_assistance + siege + rare_soil_war + defeat AS total
		FROM season_contributions WHERE season_id = ? AND week_number = 0`, season.ID)
	if err != nil {
		slog.Error("handleSeasonHubData: contribution query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer crows.Close()
	snapshotMemberIDs := map[int]bool{}
	for crows.Next() {
		var mid int
		var total int64
		if err := crows.Scan(&mid, &total); err != nil {
			continue
		}
		contribByMember[mid] = total
		snapshotMemberIDs[mid] = true
	}
	crows.Close()

	// For members without a season-end snapshot, sum their weekly rows
	wrows, err := db.Query(`SELECT member_id,
		SUM(mutual_assistance + siege + rare_soil_war + defeat) AS total
		FROM season_contributions WHERE season_id = ? AND week_number > 0
		GROUP BY member_id`, season.ID)
	if err != nil {
		slog.Error("handleSeasonHubData: weekly contribution query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer wrows.Close()
	for wrows.Next() {
		var mid int
		var total int64
		if err := wrows.Scan(&mid, &total); err != nil {
			continue
		}
		if !snapshotMemberIDs[mid] {
			contribByMember[mid] = total
		}
	}
	wrows.Close()

	// Load reward assignments
	rewardByMember := map[int]string{}
	rrows, err := db.Query(`SELECT member_id, reward_tier FROM season_rewards WHERE season_id = ?`, season.ID)
	if err != nil {
		slog.Error("handleSeasonHubData: rewards query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rrows.Close()
	for rrows.Next() {
		var mid int
		var tier string
		if err := rrows.Scan(&mid, &tier); err != nil {
			continue
		}
		rewardByMember[mid] = tier
	}
	rrows.Close()

	// Load all active members plus any former members (EX) who have data for this season.
	mrows, err := db.Query(`
		SELECT DISTINCT m.id, m.name, m.rank FROM members m
		WHERE m.rank != 'EX'
		   OR m.id IN (SELECT DISTINCT member_id FROM season_participation  WHERE season_id = ?)
		   OR m.id IN (SELECT DISTINCT member_id FROM season_contributions  WHERE season_id = ?)
		   OR m.id IN (SELECT DISTINCT member_id FROM season_rewards        WHERE season_id = ?)
		ORDER BY m.name ASC`, season.ID, season.ID, season.ID)
	if err != nil {
		slog.Error("handleSeasonHubData: members query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer mrows.Close()

	type memberBasic struct {
		id   int
		name string
		rank string
	}
	var allMembers []memberBasic
	for mrows.Next() {
		var m memberBasic
		if err := mrows.Scan(&m.id, &m.name, &m.rank); err != nil {
			continue
		}
		allMembers = append(allMembers, m)
	}
	mrows.Close()

	// Index participation rows by member
	type memberPart struct {
		scores    map[int]string // week → score key
		keyEvents map[int]int    // week → key event count
	}
	partByMember := map[int]*memberPart{}
	for _, pr := range partRows {
		if partByMember[pr.memberID] == nil {
			partByMember[pr.memberID] = &memberPart{
				scores:    map[int]string{},
				keyEvents: map[int]int{},
			}
		}
		partByMember[pr.memberID].scores[pr.weekNumber] = pr.scoreKey
		partByMember[pr.memberID].keyEvents[pr.weekNumber] = pr.attendedKeyEvt
	}

	// Find top contribution for normalising contribution %
	var topContrib int64
	for _, total := range contribByMember {
		if total > topContrib {
			topContrib = total
		}
	}
	if topContrib == 0 {
		topContrib = 1
	}

	// Build member standings
	members := make([]SeasonMember, 0, len(allMembers))
	for _, m := range allMembers {
		sm := SeasonMember{
			MemberID:     m.id,
			Name:         m.name,
			Rank:         m.rank,
			WeeklyScores: make([]string, season.WeekCount),
			WeeklyKeyEvent: make([]int, season.WeekCount),
			RewardTier:   rewardByMember[m.id],
		}

		mp := partByMember[m.id]
		keyEventCount := 0
		for w := 1; w <= season.WeekCount; w++ {
			scoreKey := "absent"
			if mp != nil {
				if k, ok := mp.scores[w]; ok {
					scoreKey = k
				}
			}
			sm.WeeklyScores[w-1] = scoreKey
			sm.ParticipationPts += scorePts[scoreKey]

			if mp != nil {
				sm.WeeklyKeyEvent[w-1] = mp.keyEvents[w]
				keyEventCount += mp.keyEvents[w]
			}
		}
		sm.KeyEventAttendance = keyEventCount
		sm.KeyEventEligible = keyEventCount >= season.KeyEventRequired

		sm.ParticipationPct = float64(sm.ParticipationPts) / float64(maxSeasonPts) * 100

		sm.ContributionTotal = contribByMember[m.id]
		sm.ContributionPct = float64(sm.ContributionTotal) / float64(topContrib) * 100

		pct := sm.ParticipationPct
		switch {
		case pct >= float64(season.TierActiveMinPct):
			sm.ClassTag = "Active Member"
		case pct >= float64(season.TierAtRiskMinPct):
			sm.ClassTag = "At Risk / Inconsistent"
		default:
			sm.ClassTag = "Dead Weight"
		}

		members = append(members, sm)
	}

	// Sort: participation % DESC, then contribution % DESC
	sort.Slice(members, func(i, j int) bool {
		if members[i].ParticipationPct != members[j].ParticipationPct {
			return members[i].ParticipationPct > members[j].ParticipationPct
		}
		return members[i].ContributionPct > members[j].ContributionPct
	})

	// R1–R3 view restriction — enforced server-side: filter to own row only
	if !canManage && !data.IsAdmin {
		own := data.MemberID
		filtered := []SeasonMember{}
		for _, sm := range members {
			if sm.MemberID == own {
				filtered = append(filtered, sm)
				break
			}
		}
		members = filtered
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"season":  season,
		"members": members,
	})
}

// ---------------------------------------------------------------------------
// Participation
// ---------------------------------------------------------------------------

func handleParticipationGet(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "", "")
	if !data.IsAuthenticated {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	season, err := loadActiveSeason()
	if err != nil {
		slog.Error("handleParticipationGet: load season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if season == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"entries": []any{}})
		return
	}

	rows, err := db.Query(`
		SELECT sp.member_id, m.name, sp.week_number, sp.score, sp.attended_key_event, sp.note
		FROM season_participation sp
		JOIN members m ON m.id = sp.member_id
		WHERE sp.season_id = ?
		ORDER BY m.name ASC, sp.week_number ASC`, season.ID)
	if err != nil {
		slog.Error("handleParticipationGet: query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type entry struct {
		MemberID         int    `json:"member_id"`
		MemberName       string `json:"member_name"`
		WeekNumber       int    `json:"week_number"`
		Score            string `json:"score"`
		AttendedKeyEvent int    `json:"attended_key_event"`
		Note             string `json:"note"`
	}
	entries := []entry{}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.MemberID, &e.MemberName, &e.WeekNumber, &e.Score, &e.AttendedKeyEvent, &e.Note); err != nil {
			continue
		}
		entries = append(entries, e)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"season_id": season.ID, "entries": entries})
}

func handleParticipationSave(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)

	var body struct {
		SeasonID   int                  `json:"season_id"`
		WeekNumber int                  `json:"week_number"`
		Entries    []ParticipationEntry `json:"entries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.SeasonID == 0 || body.WeekNumber == 0 {
		http.Error(w, "season_id and week_number are required", http.StatusBadRequest)
		return
	}

	s, err := loadSeasonByID(body.SeasonID)
	if err == sql.ErrNoRows {
		http.Error(w, "Season not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleParticipationSave: load season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if s.ArchivedAt != "" {
		http.Error(w, "Season is archived and cannot be modified", http.StatusConflict)
		return
	}
	if body.WeekNumber < 1 || body.WeekNumber > s.WeekCount {
		http.Error(w, fmt.Sprintf("week_number must be between 1 and %d", s.WeekCount), http.StatusBadRequest)
		return
	}

	// Validate score keys against season_score_levels
	validKeys := map[string]bool{}
	keyRows, _ := db.Query(`SELECT key FROM season_score_levels WHERE season_id = ?`, body.SeasonID)
	if keyRows != nil {
		defer keyRows.Close()
		for keyRows.Next() {
			var k string
			keyRows.Scan(&k)
			validKeys[k] = true
		}
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleParticipationSave: begin tx", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	saved := 0
	for _, e := range body.Entries {
		if e.MemberID == 0 {
			continue
		}
		scoreKey := e.Score
		if len(validKeys) > 0 && !validKeys[scoreKey] {
			continue // skip invalid score keys silently
		}
		_, err := tx.Exec(`
			INSERT INTO season_participation (season_id, member_id, week_number, score, attended_key_event, note, recorded_by, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(season_id, member_id, week_number) DO UPDATE SET
			  score = excluded.score,
			  attended_key_event = excluded.attended_key_event,
			  note = excluded.note,
			  recorded_by = excluded.recorded_by,
			  updated_at = CURRENT_TIMESTAMP`,
			body.SeasonID, e.MemberID, body.WeekNumber, scoreKey, e.AttendedKeyEvent, e.Note, userID)
		if err != nil {
			slog.Error("handleParticipationSave: upsert", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		saved++
	}

	if err := tx.Commit(); err != nil {
		slog.Error("handleParticipationSave: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	logActivity(userID, username, "updated", "season_attendance",
		fmt.Sprintf("%s Week %d", s.Name, body.WeekNumber), false,
		fmt.Sprintf("%d members scored", saved))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"saved": saved})
}

// ---------------------------------------------------------------------------
// Contribution import (OCR)
// ---------------------------------------------------------------------------

func handleContributionsImport(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)

	if err := r.ParseMultipartForm(50 << 20); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	seasonID, _ := strconv.Atoi(r.FormValue("season_id"))
	weekNumber, _ := strconv.Atoi(r.FormValue("week_number")) // 0 = season-end snapshot
	category := r.FormValue("category")
	commit := r.FormValue("commit") == "true"

	if seasonID == 0 {
		http.Error(w, "season_id is required", http.StatusBadRequest)
		return
	}
	validCategories := map[string]bool{
		"mutual_assistance": true,
		"siege":             true,
		"rare_soil_war":     true,
		"defeat":            true,
	}
	if !validCategories[category] {
		http.Error(w, "category must be one of: mutual_assistance, siege, rare_soil_war, defeat", http.StatusBadRequest)
		return
	}

	s, err := loadSeasonByID(seasonID)
	if err == sql.ErrNoRows {
		http.Error(w, "Season not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleContributionsImport: load season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if s.ArchivedAt != "" {
		http.Error(w, "Season is archived and cannot be modified", http.StatusConflict)
		return
	}

	files := r.MultipartForm.File["images"]
	if len(files) == 0 {
		http.Error(w, "No images provided", http.StatusBadRequest)
		return
	}

	var workerURL string
	db.QueryRow(`SELECT COALESCE(cv_worker_url,'') FROM settings WHERE id=1`).Scan(&workerURL)
	if workerURL == "" {
		http.Error(w, "OCR worker URL not configured", http.StatusServiceUnavailable)
		return
	}

	workerResp, err := ProcessImagesViaWorker(context.Background(), files, workerURL)
	if err != nil {
		slog.Error("handleContributionsImport: OCR worker", "error", err)
		http.Error(w, "OCR processing failed", http.StatusInternalServerError)
		return
	}

	// Extract records for the user-selected category.
	// The microservice returns keys matching the DB column names once updated;
	// until then, the user manually selects the category and we read that key.
	records := workerResp[category]
	if len(records) == 0 {
		// Fallback: try any non-empty key if the microservice doesn't know the category yet
		for _, v := range workerResp {
			if len(v) > 0 {
				records = v
				break
			}
		}
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleContributionsImport: begin tx", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	matched := []ContributionImportRow{}
	unresolved := []ContributionImportRow{}

	for _, rec := range records {
		member, matchType, resolveErr := resolveMemberAlias(tx, rec.PlayerName, userID)
		row := ContributionImportRow{
			OriginalName: rec.PlayerName,
			Points:       rec.Score,
		}
		if resolveErr != nil || member == nil {
			unresolved = append(unresolved, row)
			continue
		}
		row.MemberID = member.ID
		row.MemberName = member.Name
		row.MemberRank = member.Rank
		row.MatchType = matchType
		matched = append(matched, row)
	}

	if !commit {
		tx.Rollback()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"matched":    matched,
			"unresolved": unresolved,
			"week_number": weekNumber,
			"category":   category,
		})
		return
	}

	// Commit: upsert the specific category column for each matched member.
	// week_number=0 is the season-end snapshot — the canonical tie-breaker source.
	colMap := map[string]string{
		"mutual_assistance": "mutual_assistance",
		"siege":             "siege",
		"rare_soil_war":     "rare_soil_war",
		"defeat":            "defeat",
	}
	col := colMap[category]

	for _, row := range matched {
		query := fmt.Sprintf(`
			INSERT INTO season_contributions (season_id, member_id, week_number, %s, imported_by)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(season_id, member_id, week_number) DO UPDATE SET
			  %s = excluded.%s,
			  imported_by = excluded.imported_by,
			  imported_at = CURRENT_TIMESTAMP`, col, col, col)
		if _, err := tx.Exec(query, seasonID, row.MemberID, weekNumber, row.Points, userID); err != nil {
			slog.Error("handleContributionsImport: upsert", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		slog.Error("handleContributionsImport: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	weekLabel := fmt.Sprintf("Week %d", weekNumber)
	if weekNumber == 0 {
		weekLabel = "Season Total"
	}
	logActivity(userID, username, "imported", "season_contributions",
		fmt.Sprintf("%s — %s %s", s.Name, strings.ReplaceAll(category, "_", " "), weekLabel), false,
		fmt.Sprintf("%d members imported", len(matched)))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"imported":   len(matched),
		"unresolved": len(unresolved),
		"matched":    matched,
		"unresolved_list": unresolved,
	})
}

// ---------------------------------------------------------------------------
// Contribution manual entry
// ---------------------------------------------------------------------------

// handleContributionsManual accepts a direct JSON payload of member contribution
// values across all four categories for a given week, and upserts to season_contributions.
// Body: { season_id, week_number, entries: [{member_id, mutual_assistance, siege, rare_soil_war, defeat}] }
// handleContributionsGet returns per-member contribution values for a given season + week,
// used to pre-populate the manual entry table.
func handleContributionsGet(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "", "")
	if !data.IsAuthenticated {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	seasonID, _ := strconv.Atoi(r.URL.Query().Get("season_id"))
	week, _ := strconv.Atoi(r.URL.Query().Get("week"))
	if seasonID == 0 {
		http.Error(w, "season_id is required", http.StatusBadRequest)
		return
	}

	type ContribEntry struct {
		MemberID        int   `json:"member_id"`
		MutualAssistance int64 `json:"mutual_assistance"`
		Siege            int64 `json:"siege"`
		RareSoilWar      int64 `json:"rare_soil_war"`
		Defeat           int64 `json:"defeat"`
	}

	rows, err := db.Query(`
		SELECT member_id, mutual_assistance, siege, rare_soil_war, defeat
		FROM season_contributions
		WHERE season_id = ? AND week_number = ?`, seasonID, week)
	if err != nil {
		slog.Error("handleContributionsGet: query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	entries := []ContribEntry{}
	for rows.Next() {
		var e ContribEntry
		if err := rows.Scan(&e.MemberID, &e.MutualAssistance, &e.Siege, &e.RareSoilWar, &e.Defeat); err != nil {
			continue
		}
		entries = append(entries, e)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"entries": entries})
}

func handleContributionsManual(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)

	var body struct {
		SeasonID   int `json:"season_id"`
		WeekNumber int `json:"week_number"`
		Entries    []struct {
			MemberID        int   `json:"member_id"`
			MutualAssistance int64 `json:"mutual_assistance"`
			Siege            int64 `json:"siege"`
			RareSoilWar      int64 `json:"rare_soil_war"`
			Defeat           int64 `json:"defeat"`
		} `json:"entries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.SeasonID == 0 {
		http.Error(w, "season_id is required", http.StatusBadRequest)
		return
	}

	s, err := loadSeasonByID(body.SeasonID)
	if err == sql.ErrNoRows {
		http.Error(w, "Season not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleContributionsManual: load season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if s.ArchivedAt != "" {
		http.Error(w, "Season is archived and cannot be modified", http.StatusConflict)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleContributionsManual: begin tx", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	saved := 0
	for _, e := range body.Entries {
		if e.MemberID == 0 {
			continue
		}
		_, err := tx.Exec(`
			INSERT INTO season_contributions
			  (season_id, member_id, week_number, mutual_assistance, siege, rare_soil_war, defeat, imported_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(season_id, member_id, week_number) DO UPDATE SET
			  mutual_assistance = excluded.mutual_assistance,
			  siege             = excluded.siege,
			  rare_soil_war     = excluded.rare_soil_war,
			  defeat            = excluded.defeat,
			  imported_by       = excluded.imported_by,
			  imported_at       = CURRENT_TIMESTAMP`,
			body.SeasonID, e.MemberID, body.WeekNumber,
			e.MutualAssistance, e.Siege, e.RareSoilWar, e.Defeat, userID)
		if err != nil {
			slog.Error("handleContributionsManual: upsert", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		saved++
	}

	if err := tx.Commit(); err != nil {
		slog.Error("handleContributionsManual: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	weekLabel := fmt.Sprintf("Week %d", body.WeekNumber)
	if body.WeekNumber == 0 {
		weekLabel = "Season Total"
	}
	logActivity(userID, username, "imported", "season_contributions",
		fmt.Sprintf("%s — %s (manual)", s.Name, weekLabel), false,
		fmt.Sprintf("%d members saved", saved))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"saved": saved})
}

// ---------------------------------------------------------------------------
// Rewards
// ---------------------------------------------------------------------------

func handleRewardsGet(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "", "")
	if !data.IsAuthenticated {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	season, err := loadActiveSeason()
	if err != nil {
		slog.Error("handleRewardsGet: load season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if season == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"rewards": []any{}})
		return
	}

	rows, err := db.Query(`
		SELECT sr.id, sr.season_id, sr.member_id, m.name, m.rank,
		       sr.reward_tier, sr.participation_pct, COALESCE(sr.contribution_pct, 0),
		       sr.note, u.username, sr.logged_at
		FROM season_rewards sr
		JOIN members m ON m.id = sr.member_id
		JOIN users u ON u.id = sr.logged_by
		WHERE sr.season_id = ?
		ORDER BY
		  CASE sr.reward_tier
		    WHEN 'alliance_leader' THEN 1
		    WHEN 'core' THEN 2
		    WHEN 'elite' THEN 3
		    WHEN 'valued' THEN 4
		  END ASC,
		  sr.participation_pct DESC`, season.ID)
	if err != nil {
		slog.Error("handleRewardsGet: query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	rewards := []SeasonReward{}
	for rows.Next() {
		var rw SeasonReward
		if err := rows.Scan(&rw.ID, &rw.SeasonID, &rw.MemberID, &rw.MemberName, &rw.MemberRank,
			&rw.RewardTier, &rw.ParticipationPct, &rw.ContributionPct,
			&rw.Note, &rw.LoggedBy, &rw.LoggedAt); err != nil {
			continue
		}
		rewards = append(rewards, rw)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"season_id": season.ID, "rewards": rewards})
}

func handleRewardSave(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)

	var body struct {
		SeasonID         int     `json:"season_id"`
		MemberID         int     `json:"member_id"`
		RewardTier       string  `json:"reward_tier"`
		ParticipationPct float64 `json:"participation_pct"`
		ContributionPct  float64 `json:"contribution_pct"`
		Note             string  `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.SeasonID == 0 || body.MemberID == 0 || body.RewardTier == "" {
		http.Error(w, "season_id, member_id, and reward_tier are required", http.StatusBadRequest)
		return
	}

	s, err := loadSeasonByID(body.SeasonID)
	if err == sql.ErrNoRows {
		http.Error(w, "Season not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleRewardSave: load season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if s.ArchivedAt != "" {
		http.Error(w, "Season is archived and cannot be modified", http.StatusConflict)
		return
	}

	var memberName string
	if err := db.QueryRow("SELECT name FROM members WHERE id = ?", body.MemberID).Scan(&memberName); err != nil {
		http.Error(w, "Member not found", http.StatusBadRequest)
		return
	}

	res, err := db.Exec(`
		INSERT INTO season_rewards (season_id, member_id, reward_tier, participation_pct, contribution_pct, note, logged_by)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(season_id, member_id) DO UPDATE SET
		  reward_tier = excluded.reward_tier,
		  participation_pct = excluded.participation_pct,
		  contribution_pct = excluded.contribution_pct,
		  note = excluded.note,
		  logged_by = excluded.logged_by,
		  logged_at = CURRENT_TIMESTAMP`,
		body.SeasonID, body.MemberID, body.RewardTier,
		body.ParticipationPct, body.ContributionPct, body.Note, userID)
	if err != nil {
		slog.Error("handleRewardSave: upsert", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	logActivity(userID, username, "created", "season_rewards",
		fmt.Sprintf("%s — %s", memberName, body.RewardTier), false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func handleRewardUpdate(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)

	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid reward ID", http.StatusBadRequest)
		return
	}

	var body struct {
		RewardTier       string  `json:"reward_tier"`
		ParticipationPct float64 `json:"participation_pct"`
		ContributionPct  float64 `json:"contribution_pct"`
		Note             string  `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Load for archive check
	var seasonID, memberID int
	var memberName string
	err = db.QueryRow(`SELECT sr.season_id, sr.member_id, m.name FROM season_rewards sr JOIN members m ON m.id = sr.member_id WHERE sr.id = ?`, id).
		Scan(&seasonID, &memberID, &memberName)
	if err == sql.ErrNoRows {
		http.Error(w, "Reward not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleRewardUpdate: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	s, _ := loadSeasonByID(seasonID)
	if s != nil && s.ArchivedAt != "" {
		http.Error(w, "Season is archived and cannot be modified", http.StatusConflict)
		return
	}

	if _, err := db.Exec(`UPDATE season_rewards SET reward_tier=?, participation_pct=?, contribution_pct=?, note=?, logged_by=?, logged_at=CURRENT_TIMESTAMP WHERE id=?`,
		body.RewardTier, body.ParticipationPct, body.ContributionPct, body.Note, userID, id); err != nil {
		slog.Error("handleRewardUpdate: update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	logActivity(userID, username, "updated", "season_rewards",
		fmt.Sprintf("%s — %s", memberName, body.RewardTier), false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Reward updated"})
}

func handleRewardDelete(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)

	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid reward ID", http.StatusBadRequest)
		return
	}

	var seasonID, memberID int
	var memberName, tier string
	err = db.QueryRow(`SELECT sr.season_id, sr.member_id, m.name, sr.reward_tier FROM season_rewards sr JOIN members m ON m.id = sr.member_id WHERE sr.id = ?`, id).
		Scan(&seasonID, &memberID, &memberName, &tier)
	if err == sql.ErrNoRows {
		http.Error(w, "Reward not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleRewardDelete: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	s, _ := loadSeasonByID(seasonID)
	if s != nil && s.ArchivedAt != "" {
		http.Error(w, "Season is archived and cannot be modified", http.StatusConflict)
		return
	}

	if _, err := db.Exec(`DELETE FROM season_rewards WHERE id = ?`, id); err != nil {
		slog.Error("handleRewardDelete: delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	logActivity(userID, username, "deleted", "season_rewards",
		fmt.Sprintf("%s — %s", memberName, tier), false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Reward deleted"})
}

// ---------------------------------------------------------------------------
// Season Mail
// ---------------------------------------------------------------------------

func handleSeasonMailList(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "", "")
	if !data.IsAuthenticated {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	season, err := loadActiveSeason()
	if err != nil {
		slog.Error("handleSeasonMailList: load season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if season == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		return
	}

	rows, err := db.Query(`
		SELECT sm.id, sm.season_id, sm.title, sm.content,
		       COALESCE(u.username,'(deleted)'), sm.posted_at
		FROM season_mail sm
		LEFT JOIN users u ON u.id = sm.posted_by
		WHERE sm.season_id = ?
		ORDER BY sm.posted_at DESC`, season.ID)
	if err != nil {
		slog.Error("handleSeasonMailList: query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := []SeasonMailItem{}
	for rows.Next() {
		var item SeasonMailItem
		if err := rows.Scan(&item.ID, &item.SeasonID, &item.Title, &item.Content,
			&item.PostedBy, &item.PostedAt); err != nil {
			continue
		}
		items = append(items, item)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"season_id": season.ID, "items": items})
}

// handleSeasonMailUpload creates a new text-based season mail entry (title + copy-paste content).
func handleSeasonMailUpload(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)

	var body struct {
		SeasonID int    `json:"season_id"`
		Title    string `json:"title"`
		Content  string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	body.Title = strings.TrimSpace(body.Title)
	if body.SeasonID == 0 || body.Title == "" {
		http.Error(w, "season_id and title are required", http.StatusBadRequest)
		return
	}

	s, err := loadSeasonByID(body.SeasonID)
	if err == sql.ErrNoRows {
		http.Error(w, "Season not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleSeasonMailUpload: load season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if s.ArchivedAt != "" {
		http.Error(w, "Season is archived and cannot be modified", http.StatusConflict)
		return
	}

	res, err := db.Exec(`INSERT INTO season_mail (season_id, title, content, posted_by) VALUES (?, ?, ?, ?)`,
		body.SeasonID, body.Title, body.Content, userID)
	if err != nil {
		slog.Error("handleSeasonMailUpload: db insert", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	logActivity(userID, username, "created", "season_mail", body.Title, false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func handleSeasonMailUpdate(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)

	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var body struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	body.Title = strings.TrimSpace(body.Title)
	if body.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}

	var seasonID int
	err = db.QueryRow(`SELECT season_id FROM season_mail WHERE id = ?`, id).Scan(&seasonID)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleSeasonMailUpdate: query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	s, _ := loadSeasonByID(seasonID)
	if s != nil && s.ArchivedAt != "" {
		http.Error(w, "Season is archived and cannot be modified", http.StatusConflict)
		return
	}

	if _, err := db.Exec(`UPDATE season_mail SET title = ?, content = ?, posted_by = ?, posted_at = CURRENT_TIMESTAMP WHERE id = ?`,
		body.Title, body.Content, userID, id); err != nil {
		slog.Error("handleSeasonMailUpdate: update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	logActivity(userID, username, "updated", "season_mail", body.Title, false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Updated"})
}

func handleSeasonMailDelete(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)

	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var title string
	var seasonID int
	err = db.QueryRow(`SELECT season_id, title FROM season_mail WHERE id = ?`, id).
		Scan(&seasonID, &title)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleSeasonMailDelete: query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	s, _ := loadSeasonByID(seasonID)
	if s != nil && s.ArchivedAt != "" {
		http.Error(w, "Season is archived and cannot be modified", http.StatusConflict)
		return
	}

	if _, err := db.Exec(`DELETE FROM season_mail WHERE id = ?`, id); err != nil {
		slog.Error("handleSeasonMailDelete: delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	logActivity(userID, username, "deleted", "season_mail", title, false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Deleted"})
}
