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
	"unicode"

	"github.com/gorilla/mux"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type Season struct {
	ID               int               `json:"id"`
	Name             string            `json:"name"`
	SeasonNumber     int               `json:"season_number"`
	StartDate        string            `json:"start_date"`
	EndDate          string            `json:"end_date"`
	WeekCount        int               `json:"week_count"`
	KeyEventName     string            `json:"key_event_name"`
	KeyEventRequired int               `json:"key_event_required"`
	TierActiveMinPct int               `json:"tier_active_min_pct"`
	TierAtRiskMinPct int               `json:"tier_at_risk_min_pct"`
	TierCountLeader  int               `json:"tier_count_leader"`
	TierCountCore    int               `json:"tier_count_core"`
	TierCountElite   int               `json:"tier_count_elite"`
	TierCountValued  int               `json:"tier_count_valued"`
	IsActive         bool              `json:"is_active"`
	ArchivedAt       string            `json:"archived_at"`
	CreatedAt        string            `json:"created_at"`
	ScoreLevels      []ScoreLevel      `json:"score_levels"`
	Trackables       []SeasonTrackable `json:"trackables"`
}

type SeasonTrackable struct {
	ID        int    `json:"id"`
	SeasonID  int    `json:"season_id"`
	Key       string `json:"key"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
}

type SeasonEvent struct {
	ID            int    `json:"id"`
	SeasonID      int    `json:"season_id"`
	Label         string `json:"label"`
	EventTypeID   *int   `json:"event_type_id"`
	TypeName      string `json:"type_name"`
	TypeIcon      string `json:"type_icon"`
	DayOffset     *int   `json:"day_offset"`
	EventTime     string `json:"event_time"`
	AllDay        bool   `json:"all_day"`
	Level         *int   `json:"level"`
	WeekStart     int    `json:"week_start"`
	WeekEnd       int    `json:"week_end"`
	Notes         string `json:"notes"`
	IsServerEvent bool   `json:"is_server_event"`
	DurationDays  int    `json:"duration_days"`
}

type SeasonTemplate struct {
	ID           int    `json:"id"`
	TemplateName string `json:"template_name"`
	SeasonNumber int    `json:"season_number"`
	Trackables   string `json:"trackables"`
	Defaults     string `json:"defaults"`
	Events       string `json:"events"`
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
	MemberID           int               `json:"member_id"`
	Name               string            `json:"name"`
	Rank               string            `json:"rank"`
	ParticipationPts   int               `json:"participation_pts"`
	ParticipationPct   float64           `json:"participation_pct"`
	ContributionTotal  int64             `json:"contribution_total"`
	ContributionPct    float64           `json:"contribution_pct"`
	KeyEventAttendance int               `json:"key_event_attendance"`
	KeyEventEligible   bool              `json:"key_event_eligible"`
	WeeklyScores       []string          `json:"weekly_scores"`
	WeeklyKeyEvent     []int             `json:"weekly_key_event"`
	RewardTier         string            `json:"reward_tier"`
	ClassTag           string            `json:"class_tag"`
	Records            map[string]int64  `json:"records"`
}

type ParticipationEntry struct {
	MemberID         int    `json:"member_id"`
	Score            string `json:"score"`
	AttendedKeyEvent int    `json:"attended_key_event"` // count, not bool — key event may occur >1× per week
	Note             string `json:"note"`
}

type ContributionImportRow struct {
	OriginalName string      `json:"original_name"`
	MemberID     int         `json:"member_id"`
	MemberName   string      `json:"member_name"`
	MemberRank   string      `json:"member_rank"`
	MatchType    string      `json:"match_type"`
	Points       int64       `json:"points"`
	Candidates   []OCRPlayer `json:"candidates,omitempty"` // populated for ambiguous unresolved rows
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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func populateSeasonChildren(s *Season) error {
	slRows, err := db.Query(`SELECT id, season_id, key, label, points, sort_order
		FROM season_score_levels WHERE season_id = ? ORDER BY sort_order ASC`, s.ID)
	if err != nil {
		return err
	}
	defer slRows.Close()
	for slRows.Next() {
		var sl ScoreLevel
		if err := slRows.Scan(&sl.ID, &sl.SeasonID, &sl.Key, &sl.Label, &sl.Points, &sl.SortOrder); err != nil {
			continue
		}
		s.ScoreLevels = append(s.ScoreLevels, sl)
	}
	slRows.Close()

	tRows, err := db.Query(`SELECT id, season_id, key, label, sort_order
		FROM season_trackables WHERE season_id = ? ORDER BY sort_order ASC`, s.ID)
	if err != nil {
		return err
	}
	defer tRows.Close()
	for tRows.Next() {
		var t SeasonTrackable
		if err := tRows.Scan(&t.ID, &t.SeasonID, &t.Key, &t.Label, &t.SortOrder); err != nil {
			continue
		}
		s.Trackables = append(s.Trackables, t)
	}
	return nil
}

// loadActiveSeason returns the active season with score levels and trackables.
// Returns nil (not an error) if no active season exists.
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
	if err := populateSeasonChildren(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// loadSeasonByID loads a season by ID with score levels and trackables.
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
	if err := populateSeasonChildren(&s); err != nil {
		return nil, err
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

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// deriveServerEventShortName builds a short_name for a pushed server event.
// "Exclusive Weapon: <Hero> (<Class>)" → "EW<Hero>" so different EWs don't
// collide on initials (e.g. Murphy vs Marshall both → EWM).
// Otherwise: first letter/digit of each word, up to 4 chars.
func deriveServerEventShortName(label string) string {
	if strings.HasPrefix(label, "Exclusive Weapon:") {
		rest := strings.TrimSpace(strings.TrimPrefix(label, "Exclusive Weapon:"))
		// Strip parens groups like "(Tank)" / "(Aircraft)".
		if i := strings.Index(rest, "("); i >= 0 {
			rest = strings.TrimSpace(rest[:i])
		}
		// CamelCase whatever words remain, keep letters/digits only.
		camel := ""
		for _, w := range strings.Fields(rest) {
			first := true
			for _, r := range w {
				if unicode.IsLetter(r) || unicode.IsDigit(r) {
					if first {
						camel += string(unicode.ToUpper(r))
						first = false
					} else {
						camel += string(r)
					}
				}
			}
		}
		if camel != "" {
			return "EW" + camel
		}
	}
	if len([]rune(label)) <= 6 {
		return label
	}
	short := ""
	for _, w := range strings.Fields(label) {
		for _, r := range w {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				short += string(unicode.ToUpper(r))
				break
			}
		}
		if len([]rune(short)) >= 4 {
			break
		}
	}
	return short
}

func handleSeasonCreate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	var body struct {
		TemplateID       int    `json:"template_id"`
		StartDate        string `json:"start_date"`
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
	if body.TemplateID == 0 {
		http.Error(w, "template_id is required", http.StatusBadRequest)
		return
	}
	if body.StartDate == "" {
		http.Error(w, "start_date is required", http.StatusBadRequest)
		return
	}

	// Load the template
	var tmpl SeasonTemplate
	err := db.QueryRow(`SELECT id, template_name, season_number, trackables, defaults, events
		FROM season_templates WHERE id = ?`, body.TemplateID).Scan(
		&tmpl.ID, &tmpl.TemplateName, &tmpl.SeasonNumber, &tmpl.Trackables, &tmpl.Defaults, &tmpl.Events)
	if err == sql.ErrNoRows {
		http.Error(w, "Template not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleSeasonCreate: load template", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Parse template defaults
	var defs struct {
		WeekCount        int    `json:"week_count"`
		KeyEventName     string `json:"key_event_name"`
		KeyEventRequired int    `json:"key_event_required"`
	}
	json.Unmarshal([]byte(tmpl.Defaults), &defs)
	if defs.WeekCount == 0 {
		defs.WeekCount = 8
	}
	if defs.KeyEventName == "" {
		defs.KeyEventName = "Key Event"
	}

	// Load score levels default from settings
	var scoreLevelsJSON string
	db.QueryRow(`SELECT season_score_levels_default FROM settings WHERE id = 1`).Scan(&scoreLevelsJSON)
	type scoreLevelDef struct {
		Key    string `json:"key"`
		Label  string `json:"label"`
		Points int    `json:"points"`
	}
	var scoreDefs []scoreLevelDef
	json.Unmarshal([]byte(scoreLevelsJSON), &scoreDefs)
	if len(scoreDefs) == 0 {
		scoreDefs = []scoreLevelDef{
			{Key: "full", Label: "FULL", Points: 10},
			{Key: "partial", Label: "PARTIAL", Points: 5},
			{Key: "absent", Label: "ABSENT", Points: 0},
		}
	}

	// Parse trackables and events from template
	type tkDef struct {
		Key       string `json:"key"`
		Label     string `json:"label"`
		SortOrder int    `json:"sort_order"`
	}
	var tkDefs []tkDef
	json.Unmarshal([]byte(tmpl.Trackables), &tkDefs)

	type evDef struct {
		Label         string `json:"label"`
		TypeName      string `json:"type_name"` // resolved to event_type_id at creation
		EventTypeID   *int   `json:"event_type_id"`
		DayOffset     *int   `json:"day_offset"`
		EventTime     string `json:"event_time"`
		AllDay        bool   `json:"all_day"`
		Level         *int   `json:"level"`
		WeekStart     int    `json:"week_start"`
		WeekEnd       int    `json:"week_end"`
		Notes         string `json:"notes"`
		IsServerEvent bool   `json:"is_server_event"`
		DurationDays  int    `json:"duration_days"`
	}
	var evDefs []evDef
	json.Unmarshal([]byte(tmpl.Events), &evDefs)

	// Sync any missing schedule_event_types from the template so type_name lookups
	// below resolve cleanly. Server-event types are skipped (they live elsewhere).
	// backfillExisting=false: don't touch other seasons' season_events; the rows
	// for THIS season will be inserted with correct values from the template below.
	syncEventTypesFromEvents(tmpl.Events, false)

	// Resolve type_name → event_type_id for each event before the transaction.
	typeIDCache := map[string]int{}
	for i, ev := range evDefs {
		if ev.TypeName == "" {
			continue
		}
		if id, ok := typeIDCache[ev.TypeName]; ok {
			n := id
			evDefs[i].EventTypeID = &n
			continue
		}
		var typeID int
		if err := db.QueryRow(`SELECT id FROM schedule_event_types WHERE name = ?`, ev.TypeName).Scan(&typeID); err == nil {
			typeIDCache[ev.TypeName] = typeID
			n := typeID
			evDefs[i].EventTypeID = &n
		}
	}

	// Apply tier defaults
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

	// Block duplicate season numbers before starting the transaction
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM seasons WHERE season_number = ?`, tmpl.SeasonNumber).Scan(&existing); err != nil {
		slog.Error("handleSeasonCreate: check duplicate", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if existing > 0 {
		http.Error(w, fmt.Sprintf("Season number %d already exists", tmpl.SeasonNumber), http.StatusConflict)
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
		tmpl.TemplateName, tmpl.SeasonNumber, body.StartDate, defs.WeekCount,
		defs.KeyEventName, defs.KeyEventRequired,
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

	for i, sl := range scoreDefs {
		if _, err := tx.Exec(`INSERT INTO season_score_levels (season_id, key, label, points, sort_order) VALUES (?, ?, ?, ?, ?)`,
			seasonID, sl.Key, sl.Label, sl.Points, i); err != nil {
			slog.Error("handleSeasonCreate: insert score level", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	for _, t := range tkDefs {
		if _, err := tx.Exec(`INSERT INTO season_trackables (season_id, key, label, sort_order) VALUES (?, ?, ?, ?)`,
			seasonID, t.Key, t.Label, t.SortOrder); err != nil {
			slog.Error("handleSeasonCreate: insert trackable", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	for _, ev := range evDefs {
		eventTime := ev.EventTime
		if eventTime == "" {
			eventTime = "20:00"
		}
		durationDays := ev.DurationDays
		if durationDays < 1 {
			durationDays = 1
		}
		if _, err := tx.Exec(`INSERT INTO season_events
			(season_id, label, event_type_id, type_name, day_offset, event_time, all_day, level, week_start, week_end, notes, is_server_event, duration_days)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			seasonID, ev.Label, ev.EventTypeID, ev.TypeName, ev.DayOffset, eventTime,
			boolToInt(ev.AllDay), ev.Level, ev.WeekStart, ev.WeekEnd, ev.Notes,
			boolToInt(ev.IsServerEvent), durationDays); err != nil {
			slog.Error("handleSeasonCreate: insert season_event", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		slog.Error("handleSeasonCreate: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Note: tmpl.TemplateName is also the new season's name (handleSeasonCreate
	// uses it verbatim for the seasons.name column).
	logActivity(user.ID, user.Username, "created", "season_config", tmpl.TemplateName, false,
		fmt.Sprintf("Season %d", tmpl.SeasonNumber))

	// Auto-push events to the schedule. Best-effort: if the push fails we still
	// report a successful season creation; the user can re-push from the edit
	// modal. Idempotent, so re-pushing is safe.
	var pushed pushResult
	if s, err := loadSeasonByID(int(seasonID)); err == nil {
		if p, err := pushSeasonEventsToSchedule(s, user.ID, user.Username); err != nil {
			slog.Error("handleSeasonCreate: auto push", "error", err)
		} else {
			pushed = p
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":      seasonID,
		"message": "Season created",
		"pushed":  pushed,
	})
}

func handleSeasonArchive(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

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

	logActivity(user.ID, user.Username, "archived", "season_config", s.Name, false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Season archived"})
}

func handleSeasonUpdate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

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
	logActivity(user.ID, user.Username, "updated", "season_config", body.Name, false, strings.Join(changes, "; "))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Season updated"})
}

func handleSeasonDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

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

	// Reverse anything that pushSeasonEventsToSchedule materialised: for each
	// season_event, compute the (date, type) or (label, anchor_date) it would
	// have created and delete matching rows from schedule_events / server_events.
	// This is best-effort — if the season has no start_date the loop is skipped.
	purgedAlliance, purgedServer := 0, 0
	if s.StartDate != "" {
		if startDate, perr := time.Parse("2006-01-02", s.StartDate); perr == nil {
			evRows, qerr := tx.Query(`
				SELECT label, event_type_id, day_offset, week_start, week_end, is_server_event
				FROM season_events WHERE season_id = ?`, id)
			if qerr == nil {
				type pushed struct {
					label         string
					eventTypeID   sql.NullInt64
					dayOffset     sql.NullInt64
					weekStart     int
					weekEnd       int
					isServerEvent int
				}
				var pushedEvents []pushed
				for evRows.Next() {
					var p pushed
					if scanErr := evRows.Scan(&p.label, &p.eventTypeID, &p.dayOffset,
						&p.weekStart, &p.weekEnd, &p.isServerEvent); scanErr == nil {
						pushedEvents = append(pushedEvents, p)
					}
				}
				evRows.Close()
				for _, p := range pushedEvents {
					if !p.dayOffset.Valid {
						continue
					}
					weekEnd := p.weekEnd
					if weekEnd < p.weekStart {
						weekEnd = s.WeekCount
					}
					for week := p.weekStart; week <= weekEnd; week++ {
						weekStartDate := startDate.AddDate(0, 0, (week-1)*7)
						eventDate := weekStartDate.AddDate(0, 0, int(p.dayOffset.Int64)-1)
						dateStr := eventDate.Format("2006-01-02")
						if p.isServerEvent == 1 {
							res, _ := tx.Exec(`DELETE FROM server_events WHERE name = ? AND anchor_date = ?`,
								p.label, dateStr)
							if res != nil {
								if n, _ := res.RowsAffected(); n > 0 {
									purgedServer += int(n)
								}
							}
						} else if p.eventTypeID.Valid {
							res, _ := tx.Exec(`DELETE FROM schedule_events WHERE event_date = ? AND event_type_id = ?`,
								dateStr, p.eventTypeID.Int64)
							if res != nil {
								if n, _ := res.RowsAffected(); n > 0 {
									purgedAlliance += int(n)
								}
							}
						}
					}
				}
			}
		}
	}

	// season_trackables, season_member_records, season_events cascade via ON DELETE CASCADE.
	// season_rewards, season_participation, season_score_levels do not — delete explicitly.
	for _, tbl := range []string{"season_participation", "season_score_levels", "season_rewards"} {
		if _, err := tx.Exec(`DELETE FROM `+tbl+` WHERE season_id = ?`, id); err != nil {
			slog.Error("handleSeasonDelete: delete children", "table", tbl, "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	// Season mail was folded into comms_templates (migration 041); the old
	// season_mail table no longer exists. Remove this season's mail templates.
	if _, err := tx.Exec(`DELETE FROM comms_templates WHERE season_id = ?`, id); err != nil {
		slog.Error("handleSeasonDelete: delete season mail", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
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

	details := ""
	if purgedAlliance > 0 || purgedServer > 0 {
		details = fmt.Sprintf("purged %d alliance + %d server events", purgedAlliance, purgedServer)
	}
	logActivity(user.ID, user.Username, "deleted", "season_config", s.Name, false, details)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"message":           "Season deleted",
		"purged_alliance":   purgedAlliance,
		"purged_server":     purgedServer,
	})
}

// ---------------------------------------------------------------------------
// Main data endpoint
// ---------------------------------------------------------------------------

// handleSeasonHubData returns the active season config + all member standings.
// R1–R3 (non-managers) receive only their own row — enforced server-side.
func handleSeasonHubData(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	canManage := user.IsAdmin || getRankPermissions(user.Rank).ManageSeasonHub

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

	// Build trackable ID → key map for pivot query.
	trackableKeyMap := map[int]string{}
	for _, t := range season.Trackables {
		trackableKeyMap[t.ID] = t.Key
	}

	// Load season-end contribution snapshots (week_number=0 is the canonical tie-breaker source).
	// Fall back to summing weekly rows if no season-end snapshot exists.
	contribByMember := map[int]int64{}
	memberRecords := map[int]map[string]int64{}

	// Season-end snapshot totals (week_number=0)
	crows, err := db.Query(`SELECT member_id, SUM(recorded_value) AS total
		FROM season_member_records WHERE season_id = ? AND week_number = 0
		GROUP BY member_id`, season.ID)
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
	wrows, err := db.Query(`SELECT member_id, SUM(recorded_value) AS total
		FROM season_member_records WHERE season_id = ? AND week_number > 0
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

	// Pivot query: per-member per-trackable breakdown from the season-end snapshot.
	prows, err := db.Query(`SELECT member_id, trackable_id, recorded_value
		FROM season_member_records WHERE season_id = ? AND week_number = 0`, season.ID)
	if err != nil {
		slog.Error("handleSeasonHubData: records pivot query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer prows.Close()
	for prows.Next() {
		var mid, tid int
		var val int64
		if err := prows.Scan(&mid, &tid, &val); err != nil {
			continue
		}
		if key, ok := trackableKeyMap[tid]; ok {
			if memberRecords[mid] == nil {
				memberRecords[mid] = map[string]int64{}
			}
			memberRecords[mid][key] = val
		}
	}
	prows.Close()

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
		   OR m.id IN (SELECT DISTINCT member_id FROM season_participation   WHERE season_id = ?)
		   OR m.id IN (SELECT DISTINCT member_id FROM season_member_records  WHERE season_id = ?)
		   OR m.id IN (SELECT DISTINCT member_id FROM season_rewards         WHERE season_id = ?)
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
		sm.Records = memberRecords[m.id]
		if sm.Records == nil {
			sm.Records = map[string]int64{}
		}

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
	if !canManage {
		own := 0
		if user.MemberID != nil {
			own = *user.MemberID
		}
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
	user := getAuthUser(r)

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
			body.SeasonID, e.MemberID, body.WeekNumber, scoreKey, e.AttendedKeyEvent, e.Note, user.ID)
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

	logActivity(user.ID, user.Username, "updated", "season_attendance",
		fmt.Sprintf("%s Week %d", s.Name, body.WeekNumber), false,
		fmt.Sprintf("%d members scored", saved))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"saved": saved})
}

// ---------------------------------------------------------------------------
// Contribution import (OCR)
// ---------------------------------------------------------------------------

func handleContributionsImport(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

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
	if category == "" {
		http.Error(w, "category is required", http.StatusBadRequest)
		return
	}
	// Season-total screenshots always land in the week_number=0 slot — canonical tie-breaker.
	if strings.HasSuffix(category, "_season") {
		weekNumber = 0
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

	// Derive trackable key from category by stripping the granularity suffix.
	trackableKey := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(category, "_season"), "_weekly"), "_daily")
	var trackableID int
	if err := db.QueryRow(`SELECT id FROM season_trackables WHERE season_id = ? AND key = ?`,
		seasonID, trackableKey).Scan(&trackableID); err == sql.ErrNoRows {
		http.Error(w, "Invalid category for this season", http.StatusBadRequest)
		return
	} else if err != nil {
		slog.Error("handleContributionsImport: lookup trackable", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	files := r.MultipartForm.File["images[]"]
	if len(files) == 0 {
		files = r.MultipartForm.File["images"]
	}
	if len(files) == 0 {
		http.Error(w, "No images provided", http.StatusBadRequest)
		return
	}

	workerResp, ocrDiag, err := ProcessImages(context.Background(), files, category)
	if err != nil {
		slog.Error("handleContributionsImport: OCR", "error", err)
		http.Error(w, "OCR processing failed", http.StatusInternalServerError)
		return
	}

	// The user has already selected the category, so merge records from every
	// key the worker returned — different images may be classified under variant
	// keys (e.g. mutual_assistance_daily vs mutual_assistance_weekly). We trust
	// the user's selection and deduplicate by player name during resolution.
	seen := map[string]bool{}
	var records []OCRPlayer
	for _, batch := range workerResp {
		for _, rec := range batch {
			if !seen[rec.PlayerName] {
				seen[rec.PlayerName] = true
				records = append(records, rec)
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
		row := ContributionImportRow{
			OriginalName: rec.PlayerName,
			Points:       rec.Score,
		}

		resolvedName, resolvedScore, member, matchType := resolveOCRPlayer(tx, rec, user.ID)
		_ = resolvedName // original name kept in row.OriginalName for display
		row.Points = resolvedScore
		if member == nil {
			// Unresolved — surface candidates (if any) so the submitter can pick.
			if len(rec.Candidates) > 0 {
				row.Candidates = rec.Candidates
			}
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

	// Apply any manual resolutions the user provided for previously-unresolved names.
	type resolvedMapping struct {
		OriginalName string `json:"original_name"`
		Points       int64  `json:"points"`
		MemberID     int    `json:"member_id"`
		AliasType    string `json:"alias_type"` // "", "ocr", "global", "personal"
	}
	var resolvedMappings []resolvedMapping
	if raw := r.FormValue("resolved_mappings"); raw != "" {
		json.Unmarshal([]byte(raw), &resolvedMappings)
	}
	resolvedCount := 0
	for _, rm := range resolvedMappings {
		if rm.MemberID == 0 {
			continue
		}
		if _, err := tx.Exec(`
			INSERT INTO season_member_records
			  (season_id, member_id, week_number, trackable_id, recorded_value, logged_by, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(season_id, member_id, week_number, trackable_id) DO UPDATE SET
			  recorded_value = excluded.recorded_value,
			  logged_by      = excluded.logged_by,
			  updated_at     = CURRENT_TIMESTAMP`,
			seasonID, rm.MemberID, weekNumber, trackableID, rm.Points, user.ID); err != nil {
			slog.Error("handleContributionsImport: upsert resolved", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		resolvedCount++
		if rm.AliasType != "" && rm.OriginalName != "" {
			var isGlobal int
			if rm.AliasType == "global" || rm.AliasType == "ocr" {
				isGlobal = 1
			}
			var aliasUserID *int
			if rm.AliasType == "personal" {
				aliasUserID = &user.ID
			}
			tx.Exec(`INSERT INTO member_aliases (member_id, alias, category, user_id)
				VALUES (?, ?, ?, ?)
				ON CONFLICT DO NOTHING`,
				rm.MemberID, rm.OriginalName, func() string {
					if rm.AliasType == "ocr" {
						return "ocr"
					}
					if isGlobal == 1 {
						return "global"
					}
					return "personal"
				}(), aliasUserID)
		}
	}

	// Commit: upsert into season_member_records for the resolved trackable.
	for _, row := range matched {
		if _, err := tx.Exec(`
			INSERT INTO season_member_records
			  (season_id, member_id, week_number, trackable_id, recorded_value, logged_by, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(season_id, member_id, week_number, trackable_id) DO UPDATE SET
			  recorded_value = excluded.recorded_value,
			  logged_by      = excluded.logged_by,
			  updated_at     = CURRENT_TIMESTAMP`,
			seasonID, row.MemberID, weekNumber, trackableID, row.Points, user.ID); err != nil {
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
	details := fmt.Sprintf("%d committed, %d resolved", len(matched), resolvedCount)
	if summary := summarizeOCRDiagnostics(ocrDiag); summary != "" {
		details += " · " + summary
	}
	logActivity(user.ID, user.Username, "imported", "season_contributions",
		fmt.Sprintf("%s — %s %s", s.Name, strings.ReplaceAll(category, "_", " "), weekLabel), false,
		details)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"committed": len(matched) + resolvedCount,
		"resolved":  resolvedCount,
	})
}

// ---------------------------------------------------------------------------
// Contribution manual entry
// ---------------------------------------------------------------------------

// handleContributionsGet returns per-member contribution values for a given season + week,
// used to pre-populate the manual entry table.
func handleContributionsGet(w http.ResponseWriter, r *http.Request) {
	seasonID, _ := strconv.Atoi(r.URL.Query().Get("season_id"))
	week, _ := strconv.Atoi(r.URL.Query().Get("week"))
	if seasonID == 0 {
		http.Error(w, "season_id is required", http.StatusBadRequest)
		return
	}

	type ContribEntry struct {
		MemberID int              `json:"member_id"`
		Records  map[string]int64 `json:"records"`
	}

	rows, err := db.Query(`
		SELECT smr.member_id, st.key, smr.recorded_value
		FROM season_member_records smr
		JOIN season_trackables st ON st.id = smr.trackable_id
		WHERE smr.season_id = ? AND smr.week_number = ?`, seasonID, week)
	if err != nil {
		slog.Error("handleContributionsGet: query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	byMember := map[int]*ContribEntry{}
	for rows.Next() {
		var mid int
		var key string
		var val int64
		if err := rows.Scan(&mid, &key, &val); err != nil {
			continue
		}
		if byMember[mid] == nil {
			byMember[mid] = &ContribEntry{MemberID: mid, Records: map[string]int64{}}
		}
		byMember[mid].Records[key] = val
	}

	entries := make([]ContribEntry, 0, len(byMember))
	for _, e := range byMember {
		entries = append(entries, *e)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"entries": entries})
}

func handleContributionsManual(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	var body struct {
		SeasonID   int `json:"season_id"`
		WeekNumber int `json:"week_number"`
		Entries    []struct {
			MemberID int              `json:"member_id"`
			Records  map[string]int64 `json:"records"`
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

	// Build key→trackable_id map for this season.
	trackableIDMap := map[string]int{}
	for _, t := range s.Trackables {
		trackableIDMap[t.Key] = t.ID
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
		for key, val := range e.Records {
			tid, ok := trackableIDMap[key]
			if !ok {
				continue
			}
			_, err := tx.Exec(`
				INSERT INTO season_member_records
				  (season_id, member_id, week_number, trackable_id, recorded_value, logged_by, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
				ON CONFLICT(season_id, member_id, week_number, trackable_id) DO UPDATE SET
				  recorded_value = excluded.recorded_value,
				  logged_by      = excluded.logged_by,
				  updated_at     = CURRENT_TIMESTAMP`,
				body.SeasonID, e.MemberID, body.WeekNumber, tid, val, user.ID)
			if err != nil {
				slog.Error("handleContributionsManual: upsert", "error", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
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
	logActivity(user.ID, user.Username, "imported", "season_contributions",
		fmt.Sprintf("%s — %s (manual)", s.Name, weekLabel), false,
		fmt.Sprintf("%d members saved", saved))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"saved": saved})
}

// ---------------------------------------------------------------------------
// Rewards
// ---------------------------------------------------------------------------

func handleRewardsGet(w http.ResponseWriter, r *http.Request) {

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
	user := getAuthUser(r)

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
		body.ParticipationPct, body.ContributionPct, body.Note, user.ID)
	if err != nil {
		slog.Error("handleRewardSave: upsert", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	logActivity(user.ID, user.Username, "created", "season_rewards",
		fmt.Sprintf("%s — %s", memberName, body.RewardTier), false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func handleRewardUpdate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

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
		body.RewardTier, body.ParticipationPct, body.ContributionPct, body.Note, user.ID, id); err != nil {
		slog.Error("handleRewardUpdate: update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	logActivity(user.ID, user.Username, "updated", "season_rewards",
		fmt.Sprintf("%s — %s", memberName, body.RewardTier), false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Reward updated"})
}

func handleRewardDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

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

	logActivity(user.ID, user.Username, "deleted", "season_rewards",
		fmt.Sprintf("%s — %s", memberName, tier), false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Reward deleted"})
}

// ---------------------------------------------------------------------------
// Trackable management
// ---------------------------------------------------------------------------

func handleSeasonTrackableList(w http.ResponseWriter, r *http.Request) {
	seasonID, _ := strconv.Atoi(r.URL.Query().Get("season_id"))
	if seasonID == 0 {
		http.Error(w, "season_id is required", http.StatusBadRequest)
		return
	}
	rows, err := db.Query(`SELECT id, season_id, key, label, sort_order
		FROM season_trackables WHERE season_id = ? ORDER BY sort_order ASC`, seasonID)
	if err != nil {
		slog.Error("handleSeasonTrackableList: query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	trackables := []SeasonTrackable{}
	for rows.Next() {
		var t SeasonTrackable
		if err := rows.Scan(&t.ID, &t.SeasonID, &t.Key, &t.Label, &t.SortOrder); err != nil {
			continue
		}
		trackables = append(trackables, t)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"trackables": trackables})
}

func handleSeasonTrackableCreate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	var body SeasonTrackable
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.SeasonID == 0 || body.Key == "" || body.Label == "" {
		http.Error(w, "season_id, key, and label are required", http.StatusBadRequest)
		return
	}

	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM season_trackables WHERE season_id = ? AND key = ?`,
		body.SeasonID, body.Key).Scan(&existing); err != nil {
		slog.Error("handleSeasonTrackableCreate: dup check", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if existing > 0 {
		http.Error(w, "A trackable with that key already exists for this season", http.StatusConflict)
		return
	}
	res, err := db.Exec(`INSERT INTO season_trackables (season_id, key, label, sort_order) VALUES (?, ?, ?, ?)`,
		body.SeasonID, body.Key, body.Label, body.SortOrder)
	if err != nil {
		slog.Error("handleSeasonTrackableCreate: insert", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	logActivity(user.ID, user.Username, "created", "season_trackable", body.Label, false)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func handleSeasonTrackableUpdate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid trackable ID", http.StatusBadRequest)
		return
	}
	var body struct {
		Label     string `json:"label"`
		SortOrder int    `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.Label == "" {
		http.Error(w, "label is required", http.StatusBadRequest)
		return
	}
	var oldLabel string
	var oldSortOrder int
	if err := db.QueryRow(`SELECT label, sort_order FROM season_trackables WHERE id = ?`, id).
		Scan(&oldLabel, &oldSortOrder); err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	} else if err != nil {
		slog.Error("handleSeasonTrackableUpdate: load old", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := db.Exec(`UPDATE season_trackables SET label = ?, sort_order = ? WHERE id = ?`,
		body.Label, body.SortOrder, id); err != nil {
		slog.Error("handleSeasonTrackableUpdate: update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	var changes []string
	if oldLabel != body.Label {
		changes = append(changes, "label: "+oldLabel+" → "+body.Label)
	}
	if oldSortOrder != body.SortOrder {
		changes = append(changes, fmt.Sprintf("sort_order: %d → %d", oldSortOrder, body.SortOrder))
	}
	logActivity(user.ID, user.Username, "updated", "season_trackable", body.Label, false,
		strings.Join(changes, "; "))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Updated"})
}

func handleSeasonTrackableDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid trackable ID", http.StatusBadRequest)
		return
	}
	var label string
	if err := db.QueryRow(`SELECT label FROM season_trackables WHERE id = ?`, id).Scan(&label); err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	} else if err != nil {
		slog.Error("handleSeasonTrackableDelete: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	var recordCount int
	db.QueryRow(`SELECT COUNT(*) FROM season_member_records WHERE trackable_id = ?`, id).Scan(&recordCount)
	if recordCount > 0 {
		http.Error(w, "Cannot delete a trackable that has recorded data", http.StatusConflict)
		return
	}
	if _, err := db.Exec(`DELETE FROM season_trackables WHERE id = ?`, id); err != nil {
		slog.Error("handleSeasonTrackableDelete: delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "deleted", "season_trackable", label, false)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Deleted"})
}

// ---------------------------------------------------------------------------
// Season events
// ---------------------------------------------------------------------------

func handleSeasonEventList(w http.ResponseWriter, r *http.Request) {
	seasonID, _ := strconv.Atoi(r.URL.Query().Get("season_id"))
	if seasonID == 0 {
		http.Error(w, "season_id is required", http.StatusBadRequest)
		return
	}
	rows, err := db.Query(`
		SELECT se.id, se.season_id, se.label,
		       se.event_type_id,
		       CASE WHEN et.name IS NOT NULL THEN et.name ELSE se.type_name END,
		       COALESCE(et.icon,''),
		       se.day_offset, se.event_time, se.all_day, se.level,
		       se.week_start, se.week_end, se.notes,
		       se.is_server_event, se.duration_days
		FROM season_events se
		LEFT JOIN schedule_event_types et ON et.id = se.event_type_id
		WHERE se.season_id = ?
		ORDER BY se.week_start ASC, se.day_offset ASC, se.id ASC`, seasonID)
	if err != nil {
		slog.Error("handleSeasonEventList: query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	events := []SeasonEvent{}
	for rows.Next() {
		var ev SeasonEvent
		var etID sql.NullInt64
		var dayOff, level sql.NullInt64
		var allDay int
		var isServer, durationDays int
		if err := rows.Scan(&ev.ID, &ev.SeasonID, &ev.Label,
			&etID, &ev.TypeName, &ev.TypeIcon,
			&dayOff, &ev.EventTime, &allDay, &level,
			&ev.WeekStart, &ev.WeekEnd, &ev.Notes,
			&isServer, &durationDays); err != nil {
			continue
		}
		if etID.Valid {
			v := int(etID.Int64)
			ev.EventTypeID = &v
		}
		if dayOff.Valid {
			v := int(dayOff.Int64)
			ev.DayOffset = &v
		}
		if level.Valid {
			v := int(level.Int64)
			ev.Level = &v
		}
		ev.AllDay = allDay == 1
		ev.IsServerEvent = isServer == 1
		ev.DurationDays = durationDays
		if ev.DurationDays < 1 {
			ev.DurationDays = 1
		}
		events = append(events, ev)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"events": events})
}

func handleSeasonEventCreate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	var body SeasonEvent
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.SeasonID == 0 || body.Label == "" {
		http.Error(w, "season_id and label are required", http.StatusBadRequest)
		return
	}
	// week_start may be 0 or negative for pre-season events (e.g. S6 Faction Awards).
	if body.EventTime == "" {
		body.EventTime = "20:00"
	}
	durationDays := body.DurationDays
	if durationDays < 1 {
		durationDays = 1
	}
	res, err := db.Exec(`
		INSERT INTO season_events (season_id, label, event_type_id, type_name, day_offset, event_time, all_day, level, week_start, week_end, notes, is_server_event, duration_days)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		body.SeasonID, body.Label, body.EventTypeID, body.TypeName, body.DayOffset,
		body.EventTime, body.AllDay, body.Level,
		body.WeekStart, body.WeekEnd, body.Notes,
		boolToInt(body.IsServerEvent), durationDays)
	if err != nil {
		slog.Error("handleSeasonEventCreate: insert", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	logActivity(user.ID, user.Username, "created", "season_event", body.Label, false)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func handleSeasonEventUpdate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}
	var body SeasonEvent
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.Label == "" {
		http.Error(w, "label is required", http.StatusBadRequest)
		return
	}
	// week_start may be 0 or negative for pre-season events (e.g. S6 Faction Awards).
	if body.EventTime == "" {
		body.EventTime = "20:00"
	}
	updateDuration := body.DurationDays
	if updateDuration < 1 {
		updateDuration = 1
	}
	// Load old values for the diff before writing.
	var old SeasonEvent
	var oldEtID, oldDayOff, oldLevel sql.NullInt64
	var oldAllDay, oldIsServer, oldDur int
	loadErr := db.QueryRow(`SELECT label, event_type_id, type_name, day_offset, event_time,
		all_day, level, week_start, week_end, notes, is_server_event, duration_days
		FROM season_events WHERE id = ?`, id).Scan(
		&old.Label, &oldEtID, &old.TypeName, &oldDayOff, &old.EventTime,
		&oldAllDay, &oldLevel, &old.WeekStart, &old.WeekEnd, &old.Notes,
		&oldIsServer, &oldDur)
	if loadErr == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if loadErr != nil {
		slog.Error("handleSeasonEventUpdate: load old", "error", loadErr)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := db.Exec(`
		UPDATE season_events SET label=?, event_type_id=?, type_name=?, day_offset=?, event_time=?,
		  all_day=?, level=?, week_start=?, week_end=?, notes=?, is_server_event=?, duration_days=?
		WHERE id=?`,
		body.Label, body.EventTypeID, body.TypeName, body.DayOffset, body.EventTime,
		body.AllDay, body.Level, body.WeekStart, body.WeekEnd, body.Notes,
		boolToInt(body.IsServerEvent), updateDuration, id); err != nil {
		slog.Error("handleSeasonEventUpdate: update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	var changes []string
	if old.Label != body.Label {
		changes = append(changes, "label: "+old.Label+" → "+body.Label)
	}
	if old.TypeName != body.TypeName {
		changes = append(changes, "type: "+old.TypeName+" → "+body.TypeName)
	}
	newDay := 0
	if body.DayOffset != nil {
		newDay = *body.DayOffset
	}
	oldDay := 0
	if oldDayOff.Valid {
		oldDay = int(oldDayOff.Int64)
	}
	if oldDay != newDay {
		changes = append(changes, fmt.Sprintf("day_offset: %d → %d", oldDay, newDay))
	}
	if old.EventTime != body.EventTime {
		changes = append(changes, "time: "+old.EventTime+" → "+body.EventTime)
	}
	if old.WeekStart != body.WeekStart || old.WeekEnd != body.WeekEnd {
		changes = append(changes, fmt.Sprintf("weeks: %d-%d → %d-%d",
			old.WeekStart, old.WeekEnd, body.WeekStart, body.WeekEnd))
	}
	if (oldIsServer == 1) != body.IsServerEvent {
		changes = append(changes, fmt.Sprintf("is_server_event: %v → %v",
			oldIsServer == 1, body.IsServerEvent))
	}
	if oldDur != updateDuration {
		changes = append(changes, fmt.Sprintf("duration_days: %d → %d", oldDur, updateDuration))
	}
	logActivity(user.ID, user.Username, "updated", "season_event", body.Label, false,
		strings.Join(changes, "; "))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Updated"})
}

func handleSeasonEventDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}
	var label string
	if err := db.QueryRow(`SELECT label FROM season_events WHERE id = ?`, id).Scan(&label); err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	} else if err != nil {
		slog.Error("handleSeasonEventDelete: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := db.Exec(`DELETE FROM season_events WHERE id = ?`, id); err != nil {
		slog.Error("handleSeasonEventDelete: delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "deleted", "season_event", label, false)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Deleted"})
}

// ---------------------------------------------------------------------------
// Push season events to schedule
// ---------------------------------------------------------------------------

type pushResult struct {
	Created            int `json:"created"`
	Skipped            int `json:"skipped"`
	SkippedUnscheduled int `json:"skipped_unscheduled"`
	SkippedNoType      int `json:"skipped_no_type"`
}

// pushSeasonEventsToSchedule materialises every season_event row for the given
// season into either schedule_events (alliance) or server_events (server) using
// the season's start_date as the Week 1 anchor. Idempotent — safe to re-run.
func pushSeasonEventsToSchedule(s *Season, userID int, username string) (pushResult, error) {
	var result pushResult
	if s.StartDate == "" {
		return result, fmt.Errorf("season has no start date")
	}
	startDate, err := time.Parse("2006-01-02", s.StartDate)
	if err != nil {
		return result, fmt.Errorf("invalid start date: %w", err)
	}

	rows, err := db.Query(`
		SELECT id, label, event_type_id, type_name, day_offset, event_time, all_day, level,
		       week_start, week_end, notes, is_server_event, duration_days
		FROM season_events WHERE season_id = ?`, s.ID)
	if err != nil {
		return result, err
	}
	defer rows.Close()

	type pushEvent struct {
		id            int
		label         string
		eventTypeID   int
		typeName      string
		dayOffset     *int
		eventTime     string
		allDay        bool
		level         *int
		weekStart     int
		weekEnd       int
		notes         string
		isServerEvent bool
		durationDays  int
	}
	var events []pushEvent
	for rows.Next() {
		var ev pushEvent
		var etID sql.NullInt64
		var dayOff, level sql.NullInt64
		var allDay, isServer, durDays int
		if err := rows.Scan(&ev.id, &ev.label, &etID, &ev.typeName, &dayOff,
			&ev.eventTime, &allDay, &level, &ev.weekStart, &ev.weekEnd, &ev.notes,
			&isServer, &durDays); err != nil {
			continue
		}
		if etID.Valid {
			ev.eventTypeID = int(etID.Int64)
		}
		if dayOff.Valid {
			v := int(dayOff.Int64)
			ev.dayOffset = &v
		}
		if level.Valid {
			v := int(level.Int64)
			ev.level = &v
		}
		ev.allDay = allDay == 1
		ev.isServerEvent = isServer == 1
		ev.durationDays = durDays
		if ev.durationDays < 1 {
			ev.durationDays = 1
		}
		events = append(events, ev)
	}
	rows.Close()

	// Resolve event_type_id from type_name for events that were seeded before types were synced.
	typeIDCache := map[string]int{}
	for i, ev := range events {
		if ev.eventTypeID != 0 || ev.typeName == "" {
			continue
		}
		if id, ok := typeIDCache[ev.typeName]; ok {
			events[i].eventTypeID = id
			continue
		}
		var typeID int
		if err := db.QueryRow(`SELECT id FROM schedule_event_types WHERE name = ?`, ev.typeName).Scan(&typeID); err == nil {
			typeIDCache[ev.typeName] = typeID
			events[i].eventTypeID = typeID
			db.Exec(`UPDATE season_events SET event_type_id = ? WHERE id = ?`, typeID, ev.id)
		}
	}

	for _, ev := range events {
		// Alliance schedule events require a type; server events don't (type is only used for the icon).
		if ev.eventTypeID == 0 && !ev.isServerEvent {
			result.SkippedNoType++
			continue
		}
		if ev.dayOffset == nil {
			result.SkippedUnscheduled++
			continue
		}

		// week_end < week_start is the open-ended sentinel meaning "run from
		// week_start through the season's last week" (legacy: also matches
		// week_end=0 with week_start=1). Pre-season events use week_start=0,
		// week_end=0 to mean "week 0 only".
		weekEnd := ev.weekEnd
		if weekEnd < ev.weekStart {
			weekEnd = s.WeekCount
		}

		for week := ev.weekStart; week <= weekEnd; week++ {
			weekStartDate := startDate.AddDate(0, 0, (week-1)*7)
			eventDate := weekStartDate.AddDate(0, 0, *ev.dayOffset-1)
			dateStr := eventDate.Format("2006-01-02")

			if ev.isServerEvent {
				var existingID int
				err := db.QueryRow(`SELECT id FROM server_events WHERE name = ? AND anchor_date = ?`,
					ev.label, dateStr).Scan(&existingID)
				if err == nil {
					result.Skipped++
					continue
				}
				if err != sql.ErrNoRows {
					return result, err
				}
				var typeIcon string
				if ev.eventTypeID != 0 {
					db.QueryRow(`SELECT icon FROM schedule_event_types WHERE id = ?`, ev.eventTypeID).Scan(&typeIcon)
				}
				icon := typeIcon
				if icon == "" {
					icon = "🌐"
				}
				shortName := deriveServerEventShortName(ev.label)
				finalShort := shortName
				for suffix := 2; ; suffix++ {
					var n int
					db.QueryRow(`SELECT COUNT(*) FROM server_events WHERE short_name = ? AND name != ?`, finalShort, ev.label).Scan(&n)
					if n == 0 {
						break
					}
					finalShort = fmt.Sprintf("%s%d", shortName, suffix)
				}
				if _, err := db.Exec(`
					INSERT INTO server_events (name, short_name, icon, duration_days, repeat_type, anchor_date, active, sort_order)
					VALUES (?, ?, ?, ?, 'none', ?, 1, 0)`,
					ev.label, finalShort, icon, ev.durationDays, dateStr); err != nil {
					return result, err
				}
				result.Created++
				continue
			}

			var existingID int
			err := db.QueryRow(`SELECT id FROM schedule_events WHERE event_date = ? AND event_type_id = ?`,
				dateStr, ev.eventTypeID).Scan(&existingID)
			if err == nil {
				result.Skipped++
				continue
			}
			if err != sql.ErrNoRows {
				return result, err
			}
			if _, err := db.Exec(`
				INSERT INTO schedule_events (event_date, event_type_id, event_time, all_day, level, notes, created_by)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				dateStr, ev.eventTypeID, ev.eventTime, ev.allDay, ev.level, ev.notes, userID); err != nil {
				return result, err
			}
			result.Created++
		}
	}

	if result.Created > 0 {
		logActivity(userID, username, "created", "season_event",
			s.Name+" schedule push", false,
			fmt.Sprintf("%d events created", result.Created))
	}
	return result, nil
}

func handleSeasonEventPushToSchedule(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	var body struct {
		SeasonID int `json:"season_id"`
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
		slog.Error("handleSeasonEventPushToSchedule: load season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if s.ArchivedAt != "" {
		http.Error(w, "Season is archived — cannot push events", http.StatusConflict)
		return
	}

	result, err := pushSeasonEventsToSchedule(s, user.ID, user.Username)
	if err != nil {
		slog.Error("handleSeasonEventPushToSchedule: push", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}


// ---------------------------------------------------------------------------
// Season templates
// ---------------------------------------------------------------------------

func handleSeasonTemplateList(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, template_name, season_number, trackables, defaults, events
		FROM season_templates ORDER BY season_number ASC`)
	if err != nil {
		slog.Error("handleSeasonTemplateList: query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	templates := []SeasonTemplate{}
	for rows.Next() {
		var t SeasonTemplate
		if err := rows.Scan(&t.ID, &t.TemplateName, &t.SeasonNumber, &t.Trackables, &t.Defaults, &t.Events); err != nil {
			continue
		}
		templates = append(templates, t)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"templates": templates})
}

func handleSeasonTemplateGet(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var t SeasonTemplate
	err := db.QueryRow(`SELECT id, template_name, season_number, trackables, defaults, events
		FROM season_templates WHERE id = ?`, id).Scan(
		&t.ID, &t.TemplateName, &t.SeasonNumber, &t.Trackables, &t.Defaults, &t.Events)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleSeasonTemplateGet: query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(t)
}

func handleSeasonTemplateCreate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var body struct {
		TemplateName string `json:"template_name"`
		SeasonNumber int    `json:"season_number"`
		Trackables   string `json:"trackables"`
		Defaults     string `json:"defaults"`
		Events       string `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.TemplateName == "" {
		http.Error(w, "template_name is required", http.StatusBadRequest)
		return
	}
	if body.SeasonNumber > 0 {
		var existing int
		db.QueryRow(`SELECT COUNT(*) FROM season_templates WHERE season_number = ?`, body.SeasonNumber).Scan(&existing)
		if existing > 0 {
			http.Error(w, fmt.Sprintf("A template for Season %d already exists", body.SeasonNumber), http.StatusConflict)
			return
		}
	}
	if body.Trackables == "" {
		body.Trackables = "[]"
	}
	if body.Defaults == "" {
		body.Defaults = `{"week_count":8,"key_event_name":"","key_event_required":0}`
	}
	if body.Events == "" {
		body.Events = "[]"
	}
	res, err := db.Exec(`INSERT INTO season_templates (template_name, season_number, trackables, defaults, events) VALUES (?,?,?,?,?)`,
		body.TemplateName, body.SeasonNumber, body.Trackables, body.Defaults, body.Events)
	if err != nil {
		slog.Error("handleSeasonTemplateCreate: insert", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	logActivity(user.ID, user.Username, "created", "season_template", body.TemplateName, false)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func handleSeasonTemplateUpdate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var body struct {
		TemplateName string `json:"template_name"`
		SeasonNumber int    `json:"season_number"`
		Trackables   string `json:"trackables"`
		Defaults     string `json:"defaults"`
		Events       string `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.TemplateName == "" {
		http.Error(w, "template_name is required", http.StatusBadRequest)
		return
	}
	if body.SeasonNumber > 0 {
		var existing int
		db.QueryRow(`SELECT COUNT(*) FROM season_templates WHERE season_number = ? AND id != ?`, body.SeasonNumber, id).Scan(&existing)
		if existing > 0 {
			http.Error(w, fmt.Sprintf("A template for Season %d already exists", body.SeasonNumber), http.StatusConflict)
			return
		}
	}
	_, err := db.Exec(`UPDATE season_templates SET template_name=?, season_number=?, trackables=?, defaults=?, events=? WHERE id=?`,
		body.TemplateName, body.SeasonNumber, body.Trackables, body.Defaults, body.Events, id)
	if err != nil {
		slog.Error("handleSeasonTemplateUpdate: update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "updated", "season_template", body.TemplateName, false)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Updated"})
}

func handleSeasonTemplateDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid template ID", http.StatusBadRequest)
		return
	}
	var name string
	if err := db.QueryRow(`SELECT template_name FROM season_templates WHERE id = ?`, id).Scan(&name); err == sql.ErrNoRows {
		http.Error(w, "Template not found", http.StatusNotFound)
		return
	} else if err != nil {
		slog.Error("handleSeasonTemplateDelete: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := db.Exec(`DELETE FROM season_templates WHERE id = ?`, id); err != nil {
		slog.Error("handleSeasonTemplateDelete: delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "deleted", "season_template", name, false)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Deleted"})
}

func handleSeasonScoreLevelsDefaultGet(w http.ResponseWriter, r *http.Request) {
	var v string
	db.QueryRow(`SELECT season_score_levels_default FROM settings WHERE id = 1`).Scan(&v)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"score_levels":%s}`, v)
}

func handleSeasonScoreLevelsDefaultPut(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var body struct {
		ScoreLevels json.RawMessage `json:"score_levels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if _, err := db.Exec(`UPDATE settings SET season_score_levels_default = ? WHERE id = 1`, string(body.ScoreLevels)); err != nil {
		slog.Error("handleSeasonScoreLevelsDefault: update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "updated", "settings", "season score levels default", true)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Saved"})
}

// syncEventTypesFromEvents inserts a schedule_event_types row for each unique
// alliance event type_name in the given template events JSON (server-event
// types are skipped — they live in server_events, not schedule_event_types).
//
// When backfillExisting is true it also backfills event_type_id and
// is_server_event on every season_events row matching these type_names —
// global side effect intended only for the manual Resync button, since
// season_events for newly-created seasons are inserted with correct values
// directly from the template.
//
// Returns the count of newly created schedule_event_types rows.
func syncEventTypesFromEvents(eventsJSON string, backfillExisting bool) int {
	type evTypeDef struct {
		TypeName      string `json:"type_name"`
		TypeShort     string `json:"type_short"`
		TypeIcon      string `json:"type_icon"`
		IsServerEvent bool   `json:"is_server_event"`
	}
	var evDefs []evTypeDef
	json.Unmarshal([]byte(eventsJSON), &evDefs)

	// A given type_name might appear on multiple events — some flagged as server
	// events, some not. Treat the type as a server event if ANY occurrence is one.
	serverTypes := map[string]bool{}
	for _, ev := range evDefs {
		if ev.IsServerEvent {
			serverTypes[ev.TypeName] = true
		}
	}

	seen := map[string]bool{}
	created := 0
	for _, ev := range evDefs {
		if ev.TypeName == "" || seen[ev.TypeName] {
			continue
		}
		seen[ev.TypeName] = true
		if serverTypes[ev.TypeName] {
			continue
		}

		var exists int
		db.QueryRow(`SELECT COUNT(*) FROM schedule_event_types WHERE name = ?`, ev.TypeName).Scan(&exists)
		if exists > 0 {
			continue
		}

		short := ev.TypeShort
		if short == "" {
			short = ev.TypeName
			if len(short) > 4 {
				short = short[:4]
			}
		}
		icon := ev.TypeIcon
		if icon == "" {
			icon = "📅"
		}
		finalShort := short
		for suffix := 2; ; suffix++ {
			var n int
			db.QueryRow(`SELECT COUNT(*) FROM schedule_event_types WHERE short_name = ?`, finalShort).Scan(&n)
			if n == 0 {
				break
			}
			finalShort = fmt.Sprintf("%s%d", short, suffix)
		}

		if _, err := db.Exec(`INSERT INTO schedule_event_types (name, short_name, icon) VALUES (?, ?, ?)`,
			ev.TypeName, finalShort, icon); err != nil {
			slog.Error("syncEventTypesFromEvents: insert", "error", err)
			continue
		}
		created++
	}

	if backfillExisting {
		// Backfill season_events.event_type_id across ALL seasons for rows missing it.
		db.Exec(`UPDATE season_events
			SET event_type_id = (SELECT id FROM schedule_event_types WHERE name = season_events.type_name)
			WHERE event_type_id IS NULL AND type_name != ''`)

		// Align is_server_event with the template across ALL seasons.
		for _, ev := range evDefs {
			if ev.TypeName == "" {
				continue
			}
			flag := 0
			if serverTypes[ev.TypeName] {
				flag = 1
			}
			db.Exec(`UPDATE season_events SET is_server_event = ? WHERE type_name = ?`,
				flag, ev.TypeName)
		}
	}

	return created
}

func handleSeasonTemplateSyncEventTypes(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var eventsJSON string
	err := db.QueryRow(`SELECT events FROM season_templates WHERE id = ?`, id).Scan(&eventsJSON)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleSeasonTemplateSyncEventTypes: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Manual resync: also backfill is_server_event / event_type_id across all
	// existing season_events with matching type_names, to recover from rows that
	// pre-date the columns or were saved from an older template.
	created := syncEventTypesFromEvents(eventsJSON, true)

	if created > 0 {
		logActivity(user.ID, user.Username, "created", "schedule", fmt.Sprintf("%d event types synced from template", created), false)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"created": created})
}
