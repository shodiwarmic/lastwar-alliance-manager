// handlers_season_land.go — the Season Hub's Land tab (private-docs 274–276).
//
// Configuration (caps, durations, the level table) is per season and copied with it; routes
// are the plan — three slots, exactly one active, each an ordered list of targets saved with a
// revision. Readiness is derived (land_status.go). Holdings, drops and Cpt Hedge snapshots
// build on these tables. Every write refuses an archived season.

package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// landConfig is a season's Land rules. Saved is false when the season has no row yet and
// these are the defaults.
type landConfig struct {
	SeasonID                int    `json:"season_id"`
	CityDailyCaptures       int    `json:"city_daily_captures"`
	StrongholdDailyCaptures int    `json:"stronghold_daily_captures"`
	CityCap                 int    `json:"city_cap"`
	StrongholdCap           int    `json:"stronghold_cap"`
	CapitolCountsTowardCap  bool   `json:"capitol_counts_toward_cap"`
	AbandonMinutes          int    `json:"abandon_minutes"`
	UnlockOffsetHours       int    `json:"unlock_offset_hours"`
	RedeclareCooldownDays   *int   `json:"redeclare_cooldown_days"`
	Source                  string `json:"source"`
	Grade                   string `json:"grade"`
	Saved                   bool   `json:"saved"`
}

func defaultLandConfig(seasonID int) landConfig {
	return landConfig{SeasonID: seasonID, CityDailyCaptures: 2, StrongholdDailyCaptures: 2, CityCap: 6,
		StrongholdCap: 6, AbandonMinutes: 60, UnlockOffsetHours: 12, Grade: "unverified"}
}

func loadLandConfig(q rowQuerier, seasonID int) (landConfig, error) {
	c := defaultLandConfig(seasonID)
	var capitol int
	var cooldown sql.NullInt64
	err := q.QueryRow(`SELECT city_daily_captures, stronghold_daily_captures, city_cap, stronghold_cap,
		capitol_counts_toward_cap, abandon_minutes, unlock_offset_hours, redeclare_cooldown_days, source, grade
		FROM season_land_config WHERE season_id = ?`, seasonID).Scan(&c.CityDailyCaptures, &c.StrongholdDailyCaptures,
		&c.CityCap, &c.StrongholdCap, &capitol, &c.AbandonMinutes, &c.UnlockOffsetHours, &cooldown, &c.Source, &c.Grade)
	if err == sql.ErrNoRows {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	c.Saved = true
	c.CapitolCountsTowardCap = capitol == 1
	if cooldown.Valid {
		v := int(cooldown.Int64)
		c.RedeclareCooldownDays = &v
	}
	return c, nil
}

// landLevel is one row of the level table: a city or stronghold level's name, output and
// unlock, with anything else the season knows about it in Extra (boss level, resistance…).
type landLevel struct {
	ID              int             `json:"id"`
	Kind            string          `json:"kind"` // city | stronghold
	Level           int             `json:"level"`
	Name            string          `json:"name"`
	ResourcePerHour *int            `json:"resource_per_hour"`
	UnlockWeek      *int            `json:"unlock_week"`
	UnlockDay       *int            `json:"unlock_day"`
	Extra           json.RawMessage `json:"extra"`
	Source          string          `json:"source"`
	Grade           string          `json:"grade"`
}

func loadLandLevels(q rowQueryer, seasonID int) ([]landLevel, error) {
	rows, err := q.Query(`SELECT id, kind, level, name, resource_per_hour, unlock_week, unlock_day, extra, source, grade
		FROM season_land_levels WHERE season_id = ? ORDER BY kind, level`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []landLevel{}
	for rows.Next() {
		var l landLevel
		var extra string
		if err := rows.Scan(&l.ID, &l.Kind, &l.Level, &l.Name, &l.ResourcePerHour, &l.UnlockWeek, &l.UnlockDay,
			&extra, &l.Source, &l.Grade); err != nil {
			return nil, err
		}
		l.Extra = json.RawMessage(extra)
		out = append(out, l)
	}
	return out, rows.Err()
}

// landRoute is one of a season's three route slots with its targets in order.
type landRoute struct {
	ID        int          `json:"id"`
	Slot      string       `json:"slot"`
	Name      string       `json:"name"`
	IsActive  bool         `json:"is_active"`
	UpdatedAt string       `json:"updated_at"`
	Targets   []targetView `json:"targets"`
}

// targetView is a target as the API carries it: instants in server time.
type targetView struct {
	landTarget
	UnlockAt         string `json:"unlock_at"`
	PlannedCaptureAt string `json:"planned_capture_at"`
	ActualCaptureAt  string `json:"actual_capture_at"`
}

var landRouteSlots = []string{"primary", "backup_a", "backup_b"}

var validTargetStatuses = map[string]bool{"planned": true, "declared": true, "captured": true, "skipped": true, "failed": true}
var validConnected = map[string]bool{"touching": true, "not_touching": true, "unknown": true}
var validLandKinds = map[string]bool{"city": true, "stronghold": true, "capitol": true, "trade_post": true, "landmark": true}

// loadLandRoutes reads a season's routes and every target, cursors closed in turn.
func loadLandRoutes(q rowQueryer, seasonID int) ([]landRoute, error) {
	rows, err := q.Query(`SELECT id, slot, name, is_active, COALESCE(updated_at, '') FROM season_land_routes
		WHERE season_id = ?`, seasonID)
	if err != nil {
		return nil, err
	}
	bySlot := map[string]*landRoute{}
	for rows.Next() {
		var r landRoute
		var active int
		if err := rows.Scan(&r.ID, &r.Slot, &r.Name, &active, &r.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		r.IsActive = active == 1
		r.Targets = []targetView{}
		bySlot[r.Slot] = &r
	}
	rows.Close()
	trows, err := q.Query(`SELECT t.id, t.route_id, t.position, t.area_id, t.name, t.kind, t.level,
		COALESCE(t.unlock_at,''), COALESCE(t.planned_capture_at,''), COALESCE(t.actual_capture_at,''),
		t.prerequisite_target_id, t.buff, t.resource_per_hour, t.status, t.connected, t.coordinator_member_id, t.notes
		FROM season_land_targets t JOIN season_land_routes r ON r.id = t.route_id
		WHERE r.season_id = ? ORDER BY t.route_id, t.position, t.id`, seasonID)
	if err != nil {
		return nil, err
	}
	defer trows.Close()
	byID := map[int]*landRoute{}
	for _, r := range bySlot {
		byID[r.ID] = r
	}
	for trows.Next() {
		var t landTarget
		var routeID int
		var unlock, planned, actual string
		if err := trows.Scan(&t.ID, &routeID, &t.Position, &t.AreaID, &t.Name, &t.Kind, &t.Level, &unlock, &planned,
			&actual, &t.PrerequisiteTargetID, &t.Buff, &t.ResourcePerHour, &t.Status, &t.Connected,
			&t.CoordinatorMemberID, &t.Notes); err != nil {
			return nil, err
		}
		t.UnlockAt, t.PlannedCaptureAt, t.ActualCaptureAt = parseLandStored(unlock), parseLandStored(planned), parseLandStored(actual)
		if r := byID[routeID]; r != nil {
			r.Targets = append(r.Targets, targetView{landTarget: t, UnlockAt: landST(t.UnlockAt),
				PlannedCaptureAt: landST(t.PlannedCaptureAt), ActualCaptureAt: landST(t.ActualCaptureAt)})
		}
	}
	out := []landRoute{}
	for _, slot := range landRouteSlots {
		if r := bySlot[slot]; r != nil {
			out = append(out, *r)
		} else {
			out = append(out, landRoute{Slot: slot, Targets: []targetView{}})
		}
	}
	return out, trows.Err()
}

// activeRouteReadiness computes the Land Path Status for a season's active route.
func activeRouteReadiness(routes []landRoute, bufferHours int, now time.Time) landReadiness {
	for _, r := range routes {
		if !r.IsActive {
			continue
		}
		targets := make([]landTarget, len(r.Targets))
		for i, tv := range r.Targets {
			targets[i] = tv.landTarget
		}
		name := r.Name
		if name == "" {
			name = landSlotLabel(r.Slot)
		}
		return computeLandReadiness(r.ID, name, r.Slot, targets, bufferHours, now)
	}
	return computeLandReadiness(0, "", "", nil, bufferHours, now)
}

func landSlotLabel(slot string) string {
	switch slot {
	case "primary":
		return "Primary"
	case "backup_a":
		return "Backup A"
	case "backup_b":
		return "Backup B"
	}
	return slot
}

// landSeason loads the season a Land request names and answers 404/500 itself.
func landSeason(w http.ResponseWriter, seasonID int) *Season {
	if seasonID == 0 {
		http.Error(w, "season_id is required", http.StatusBadRequest)
		return nil
	}
	s, err := loadSeasonByID(seasonID)
	if err == sql.ErrNoRows {
		http.Error(w, "Season not found", http.StatusNotFound)
		return nil
	}
	if err != nil {
		slog.Error("landSeason", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return nil
	}
	return s
}

// GET /api/season-hub/land?season_id= — everything the Land tab shows.
func handleSeasonLandGet(w http.ResponseWriter, r *http.Request) {
	sid, _ := strconv.Atoi(r.URL.Query().Get("season_id"))
	s := landSeason(w, sid)
	if s == nil {
		return
	}
	cfg, err := loadLandConfig(db, sid)
	if err != nil {
		slog.Error("handleSeasonLandGet: config", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	levels, err := loadLandLevels(db, sid)
	if err != nil {
		slog.Error("handleSeasonLandGet: levels", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	routes, err := loadLandRoutes(db, sid)
	if err != nil {
		slog.Error("handleSeasonLandGet: routes", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	extra, err := loadLandExtras(db, s, cfg, routes, time.Now())
	if err != nil {
		slog.Error("handleSeasonLandGet: extras", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	out := map[string]any{
		"season":     map[string]any{"id": s.ID, "season_number": s.SeasonNumber, "start_date": s.StartDate, "archived": s.ArchivedAt != ""},
		"config":     cfg,
		"levels":     levels,
		"routes":     routes,
		"readiness":  activeRouteReadiness(routes, s.LandReadinessBufferHours, time.Now()),
		"has_lookup": landLookupFor(s.SeasonNumber) != nil,
	}
	for k, v := range extra {
		out[k] = v
	}
	writeJSON(w, out)
}

// loadLandExtras adds what later issues build on the same tab (holdings, drops, snapshots).
func loadLandExtras(q rowQueryer, s *Season, cfg landConfig, routes []landRoute, now time.Time) (map[string]any, error) {
	return map[string]any{}, nil
}

// GET /api/season-hub/land/lookup?season_id= — the season's map areas, for naming targets.
func handleSeasonLandLookup(w http.ResponseWriter, r *http.Request) {
	sid, _ := strconv.Atoi(r.URL.Query().Get("season_id"))
	s := landSeason(w, sid)
	if s == nil {
		return
	}
	areas := landLookupFor(s.SeasonNumber)
	if areas == nil {
		areas = []landArea{}
	}
	writeJSON(w, map[string]any{"areas": areas})
}

// PUT /api/season-hub/land/config — the season's Land rules.
func handleSeasonLandConfigPut(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var c landConfig
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if ok, msg := seasonAcceptsEdits(c.SeasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	for _, v := range []int{c.CityDailyCaptures, c.StrongholdDailyCaptures, c.CityCap, c.StrongholdCap, c.AbandonMinutes, c.UnlockOffsetHours} {
		if v < 0 || v > 1000 {
			http.Error(w, "the counts, caps and durations are whole numbers from 0", http.StatusBadRequest)
			return
		}
	}
	if c.RedeclareCooldownDays != nil && *c.RedeclareCooldownDays < 0 {
		http.Error(w, "redeclare_cooldown_days cannot be negative", http.StatusBadRequest)
		return
	}
	if c.Grade != "" && !validEventGrades[c.Grade] {
		http.Error(w, "grade is not a recognised grade", http.StatusBadRequest)
		return
	}
	if _, err := db.Exec(`INSERT INTO season_land_config (season_id, city_daily_captures, stronghold_daily_captures,
		city_cap, stronghold_cap, capitol_counts_toward_cap, abandon_minutes, unlock_offset_hours, redeclare_cooldown_days,
		source, grade) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(season_id) DO UPDATE SET city_daily_captures = excluded.city_daily_captures,
		stronghold_daily_captures = excluded.stronghold_daily_captures, city_cap = excluded.city_cap,
		stronghold_cap = excluded.stronghold_cap, capitol_counts_toward_cap = excluded.capitol_counts_toward_cap,
		abandon_minutes = excluded.abandon_minutes, unlock_offset_hours = excluded.unlock_offset_hours,
		redeclare_cooldown_days = excluded.redeclare_cooldown_days, source = excluded.source, grade = excluded.grade`,
		c.SeasonID, c.CityDailyCaptures, c.StrongholdDailyCaptures, c.CityCap, c.StrongholdCap,
		boolToInt(c.CapitolCountsTowardCap), c.AbandonMinutes, c.UnlockOffsetHours, c.RedeclareCooldownDays,
		strings.TrimSpace(c.Source), normaliseEventGrade(c.Grade)); err != nil {
		slog.Error("handleSeasonLandConfigPut", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "updated", "season_land_config", seasonTagName(&c.SeasonID), false,
		fmt.Sprintf("caps %d cities / %d strongholds, %d+%d captures a day", c.CityCap, c.StrongholdCap,
			c.CityDailyCaptures, c.StrongholdDailyCaptures))
	writeJSON(w, map[string]string{"message": "Saved"})
}

// validateLandLevels checks a whole level table.
func validateLandLevels(levels []landLevel) string {
	seen := map[string]bool{}
	for i, l := range levels {
		if l.Kind != "city" && l.Kind != "stronghold" {
			return fmt.Sprintf("row %d: kind must be city or stronghold", i+1)
		}
		if l.Level < 1 || l.Level > 20 {
			return fmt.Sprintf("row %d: level must be from 1 to 20", i+1)
		}
		k := l.Kind + strconv.Itoa(l.Level)
		if seen[k] {
			return fmt.Sprintf("%s level %d appears twice", l.Kind, l.Level)
		}
		seen[k] = true
		if l.ResourcePerHour != nil && *l.ResourcePerHour < 0 {
			return fmt.Sprintf("row %d: output per hour cannot be negative", i+1)
		}
		if (l.UnlockWeek == nil) != (l.UnlockDay == nil) {
			return fmt.Sprintf("row %d: an unlock needs both a week and a day", i+1)
		}
		if l.UnlockDay != nil && (*l.UnlockDay < 1 || *l.UnlockDay > 7) {
			return fmt.Sprintf("row %d: unlock day is 1 (Monday) to 7", i+1)
		}
		if len(l.Extra) > 0 {
			var obj map[string]any
			if json.Unmarshal(l.Extra, &obj) != nil {
				return fmt.Sprintf("row %d: extra must be a JSON object", i+1)
			}
		}
		if l.Grade != "" && !validEventGrades[l.Grade] {
			return fmt.Sprintf("row %d: grade is not a recognised grade", i+1)
		}
	}
	return ""
}

// replaceLandLevelsTx writes a season's whole level table.
func replaceLandLevelsTx(tx *sql.Tx, seasonID int, levels []landLevel) error {
	if _, err := tx.Exec(`DELETE FROM season_land_levels WHERE season_id = ?`, seasonID); err != nil {
		return err
	}
	for _, l := range levels {
		extra := "{}"
		if len(l.Extra) > 0 {
			extra = string(l.Extra)
		}
		if _, err := tx.Exec(`INSERT INTO season_land_levels (season_id, kind, level, name, resource_per_hour,
			unlock_week, unlock_day, extra, source, grade) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			seasonID, l.Kind, l.Level, strings.TrimSpace(l.Name), l.ResourcePerHour, l.UnlockWeek, l.UnlockDay, extra,
			strings.TrimSpace(l.Source), normaliseEventGrade(l.Grade)); err != nil {
			return err
		}
	}
	return nil
}

// PUT /api/season-hub/land/levels {season_id, levels} — the whole level table.
func handleSeasonLandLevelsPut(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var body struct {
		SeasonID int         `json:"season_id"`
		Levels   []landLevel `json:"levels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if ok, msg := seasonAcceptsEdits(body.SeasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	if msg := validateLandLevels(body.Levels); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleSeasonLandLevelsPut: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	if err := replaceLandLevelsTx(tx, body.SeasonID, body.Levels); err != nil {
		slog.Error("handleSeasonLandLevelsPut: write", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handleSeasonLandLevelsPut: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "updated", "season_land_config", seasonTagName(&body.SeasonID), false,
		fmt.Sprintf("%d land levels", len(body.Levels)))
	writeJSON(w, map[string]any{"saved": len(body.Levels)})
}

// routeTargetIn is one target in a route save. ID 0 is a new target.
type routeTargetIn struct {
	ID                   int    `json:"id"`
	AreaID               string `json:"area_id"`
	Name                 string `json:"name"`
	Kind                 string `json:"kind"`
	Level                *int   `json:"level"`
	UnlockAt             string `json:"unlock_at"`
	PlannedCaptureAt     string `json:"planned_capture_at"`
	ActualCaptureAt      string `json:"actual_capture_at"`
	PrerequisiteTargetID *int   `json:"prerequisite_target_id"`
	Buff                 string `json:"buff"`
	ResourcePerHour      *int   `json:"resource_per_hour"`
	Status               string `json:"status"`
	Connected            string `json:"connected"`
	CoordinatorMemberID  *int   `json:"coordinator_member_id"`
	Notes                string `json:"notes"`
}

// storedInstant parses an ST instant from a save into its stored UTC form; nil when blank.
func storedInstant(s string) (any, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	t, err := parseLandST(s)
	if err != nil {
		return nil, err
	}
	return landUTC(t), nil
}

// PUT /api/season-hub/land/routes {season_id, slot, name, targets} — one route slot, its
// targets in order. Targets keep their ids (drops and the calendar point at them); a target
// left out is deleted unless something still points at it. Every save appends a revision.
func handleSeasonLandRoutePut(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var body struct {
		SeasonID int             `json:"season_id"`
		Slot     string          `json:"slot"`
		Name     string          `json:"name"`
		Targets  []routeTargetIn `json:"targets"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	validSlot := false
	for _, s := range landRouteSlots {
		validSlot = validSlot || s == body.Slot
	}
	if !validSlot {
		http.Error(w, "slot must be primary, backup_a or backup_b", http.StatusBadRequest)
		return
	}
	s := landSeason(w, body.SeasonID)
	if s == nil {
		return
	}
	if ok, msg := seasonAcceptsEdits(body.SeasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	type prepared struct {
		in                      routeTargetIn
		unlock, planned, actual any
	}
	var targets []prepared
	for i, t := range body.Targets {
		t.Name, t.AreaID = strings.TrimSpace(t.Name), strings.TrimSpace(t.AreaID)
		if t.Name == "" && t.AreaID != "" {
			if a, ok := landAreaByID(s.SeasonNumber, t.AreaID); ok {
				t.Name = a.Name
				if t.Kind == "" {
					t.Kind = a.Kind
				}
				if t.Level == nil {
					lv := a.Level
					t.Level = &lv
				}
			}
		}
		if t.Name == "" {
			return400(w, fmt.Sprintf("target %d needs a name or a map ID", i+1))
			return
		}
		if t.Kind == "" {
			t.Kind = "city"
		}
		if !validLandKinds[t.Kind] {
			return400(w, fmt.Sprintf("target %d: kind is not a recognised land kind", i+1))
			return
		}
		if t.Status == "" {
			t.Status = "planned"
		}
		if !validTargetStatuses[t.Status] {
			return400(w, fmt.Sprintf("target %d: status must be planned, declared, captured, skipped or failed", i+1))
			return
		}
		if t.Connected == "" {
			t.Connected = "unknown"
		}
		if !validConnected[t.Connected] {
			return400(w, fmt.Sprintf("target %d: connected must be touching, not_touching or unknown", i+1))
			return
		}
		var p prepared
		var err error
		if p.unlock, err = storedInstant(t.UnlockAt); err != nil {
			return400(w, fmt.Sprintf("target %d: unlock must be YYYY-MM-DD HH:MM (server time)", i+1))
			return
		}
		if p.planned, err = storedInstant(t.PlannedCaptureAt); err != nil {
			return400(w, fmt.Sprintf("target %d: planned capture must be YYYY-MM-DD HH:MM (server time)", i+1))
			return
		}
		if p.actual, err = storedInstant(t.ActualCaptureAt); err != nil {
			return400(w, fmt.Sprintf("target %d: actual capture must be YYYY-MM-DD HH:MM (server time)", i+1))
			return
		}
		p.in = t
		targets = append(targets, p)
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleSeasonLandRoutePut: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	var routeID int64
	err = tx.QueryRow(`SELECT id FROM season_land_routes WHERE season_id = ? AND slot = ?`, body.SeasonID, body.Slot).Scan(&routeID)
	if err == sql.ErrNoRows {
		// The first route a season saves is its active one.
		var any int
		tx.QueryRow(`SELECT COUNT(*) FROM season_land_routes WHERE season_id = ?`, body.SeasonID).Scan(&any)
		res, ierr := tx.Exec(`INSERT INTO season_land_routes (season_id, slot, name, is_active, updated_at)
			VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`, body.SeasonID, body.Slot, strings.TrimSpace(body.Name), boolToInt(any == 0))
		if ierr != nil {
			slog.Error("handleSeasonLandRoutePut: insert route", "error", ierr)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		routeID, _ = res.LastInsertId()
	} else if err != nil {
		slog.Error("handleSeasonLandRoutePut: route", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	} else if _, err := tx.Exec(`UPDATE season_land_routes SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		strings.TrimSpace(body.Name), routeID); err != nil {
		slog.Error("handleSeasonLandRoutePut: rename", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	existing := map[int]bool{}
	erows, err := tx.Query(`SELECT id FROM season_land_targets WHERE route_id = ?`, routeID)
	if err != nil {
		slog.Error("handleSeasonLandRoutePut: existing", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	for erows.Next() {
		var id int
		erows.Scan(&id)
		existing[id] = true
	}
	erows.Close()
	kept := map[int]bool{}
	for pos, p := range targets {
		t := p.in
		if t.ID != 0 {
			if !existing[t.ID] {
				return400(w, fmt.Sprintf("target %d is not on this route", t.ID))
				return
			}
			kept[t.ID] = true
			if _, err := tx.Exec(`UPDATE season_land_targets SET position = ?, area_id = ?, name = ?, kind = ?, level = ?,
				unlock_at = ?, planned_capture_at = ?, actual_capture_at = ?, prerequisite_target_id = ?, buff = ?,
				resource_per_hour = ?, status = ?, connected = ?, coordinator_member_id = ?, notes = ? WHERE id = ?`,
				pos, t.AreaID, t.Name, t.Kind, t.Level, p.unlock, p.planned, p.actual, t.PrerequisiteTargetID,
				strings.TrimSpace(t.Buff), t.ResourcePerHour, t.Status, t.Connected, t.CoordinatorMemberID,
				strings.TrimSpace(t.Notes), t.ID); err != nil {
				slog.Error("handleSeasonLandRoutePut: update target", "error", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			continue
		}
		if _, err := tx.Exec(`INSERT INTO season_land_targets (route_id, position, area_id, name, kind, level, unlock_at,
			planned_capture_at, actual_capture_at, prerequisite_target_id, buff, resource_per_hour, status, connected,
			coordinator_member_id, notes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			routeID, pos, t.AreaID, t.Name, t.Kind, t.Level, p.unlock, p.planned, p.actual, t.PrerequisiteTargetID,
			strings.TrimSpace(t.Buff), t.ResourcePerHour, t.Status, t.Connected, t.CoordinatorMemberID,
			strings.TrimSpace(t.Notes)); err != nil {
			slog.Error("handleSeasonLandRoutePut: insert target", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	for id := range existing {
		if kept[id] {
			continue
		}
		if msg, err := landTargetInUseTx(tx, id); err != nil {
			slog.Error("handleSeasonLandRoutePut: in use", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		} else if msg != "" {
			http.Error(w, msg, http.StatusConflict)
			return
		}
		if _, err := tx.Exec(`UPDATE season_land_targets SET prerequisite_target_id = NULL WHERE prerequisite_target_id = ?`, id); err != nil {
			slog.Error("handleSeasonLandRoutePut: detach prerequisite", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if _, err := tx.Exec(`DELETE FROM season_land_targets WHERE id = ?`, id); err != nil {
			slog.Error("handleSeasonLandRoutePut: delete target", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	syncs, err := afterLandRouteSaveTx(tx, s, int(routeID), user.ID)
	if err != nil {
		slog.Error("handleSeasonLandRoutePut: after save", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := writeRouteRevisionTx(tx, int(routeID), user.ID); err != nil {
		slog.Error("handleSeasonLandRoutePut: revision", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handleSeasonLandRoutePut: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "updated", "season_land_route", landSlotLabel(body.Slot)+" route, "+seasonTagName(&body.SeasonID),
		false, fmt.Sprintf("%d targets", len(targets)))
	writeJSON(w, map[string]any{"route_id": routeID, "calendar": syncs})
}

func return400(w http.ResponseWriter, msg string) { http.Error(w, msg, http.StatusBadRequest) }

// landTargetInUseTx names what still points at a target, or "" — a drop's replacement.
func landTargetInUseTx(tx *sql.Tx, id int) (string, error) {
	return "", nil
}

// afterLandRouteSaveTx runs what a route save sets off beyond its own rows (the calendar's
// planned captures, private-docs 275).
func afterLandRouteSaveTx(tx *sql.Tx, s *Season, routeID, actorID int) ([]syncResult, error) {
	return nil, nil
}

// writeRouteRevisionTx appends the route's targets as they now stand.
func writeRouteRevisionTx(tx *sql.Tx, routeID, userID int) error {
	rows, err := tx.Query(`SELECT position, area_id, name, kind, level, COALESCE(unlock_at,''), COALESCE(planned_capture_at,''),
		status, connected, notes FROM season_land_targets WHERE route_id = ? ORDER BY position, id`, routeID)
	if err != nil {
		return err
	}
	type snap struct {
		Position  int    `json:"position"`
		AreaID    string `json:"area_id"`
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		Level     *int   `json:"level"`
		UnlockAt  string `json:"unlock_at"`
		PlannedAt string `json:"planned_capture_at"`
		Status    string `json:"status"`
		Connected string `json:"connected"`
		Notes     string `json:"notes"`
	}
	var out []snap
	for rows.Next() {
		var sn snap
		var unlock, planned string
		if err := rows.Scan(&sn.Position, &sn.AreaID, &sn.Name, &sn.Kind, &sn.Level, &unlock, &planned, &sn.Status,
			&sn.Connected, &sn.Notes); err != nil {
			rows.Close()
			return err
		}
		sn.UnlockAt, sn.PlannedAt = landST(parseLandStored(unlock)), landST(parseLandStored(planned))
		out = append(out, sn)
	}
	rows.Close()
	enc, _ := json.Marshal(out)
	_, err = tx.Exec(`INSERT INTO season_land_route_revisions (route_id, saved_at, saved_by, snapshot)
		VALUES (?, CURRENT_TIMESTAMP, ?, ?)`, routeID, userID, string(enc))
	return err
}

// PUT /api/season-hub/land/routes/{id}/activate — exactly one active route per season.
func handleSeasonLandRouteActivate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid route ID", http.StatusBadRequest)
		return
	}
	var seasonID int
	var slot string
	if err := db.QueryRow(`SELECT season_id, slot FROM season_land_routes WHERE id = ?`, id).Scan(&seasonID, &slot); err == sql.ErrNoRows {
		http.Error(w, "Route not found", http.StatusNotFound)
		return
	} else if err != nil {
		slog.Error("handleSeasonLandRouteActivate: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if ok, msg := seasonAcceptsEdits(seasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	if _, err := db.Exec(`UPDATE season_land_routes SET is_active = (id = ?) WHERE season_id = ?`, id, seasonID); err != nil {
		slog.Error("handleSeasonLandRouteActivate: update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "updated", "season_land_route", landSlotLabel(slot)+" route, "+seasonTagName(&seasonID),
		false, "made the active route")
	writeJSON(w, map[string]string{"message": "Activated"})
}

// GET /api/season-hub/land/routes/{id}/revisions — every save of a route, newest first.
func handleSeasonLandRouteRevisions(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid route ID", http.StatusBadRequest)
		return
	}
	rows, err := db.Query(`SELECT v.id, strftime('%Y-%m-%d %H:%M:%S', v.saved_at), COALESCE(u.username, ''), v.snapshot
		FROM season_land_route_revisions v LEFT JOIN users u ON u.id = v.saved_by
		WHERE v.route_id = ? ORDER BY v.id DESC LIMIT 50`, id)
	if err != nil {
		slog.Error("handleSeasonLandRouteRevisions", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type rev struct {
		ID      int             `json:"id"`
		SavedAt string          `json:"saved_at"`
		SavedBy string          `json:"saved_by"`
		Targets json.RawMessage `json:"targets"`
	}
	out := []rev{}
	for rows.Next() {
		var v rev
		var snap string
		if err := rows.Scan(&v.ID, &v.SavedAt, &v.SavedBy, &snap); err != nil {
			continue
		}
		if snap == "" || snap == "null" {
			snap = "[]"
		}
		v.Targets = json.RawMessage(snap)
		out = append(out, v)
	}
	writeJSON(w, map[string]any{"revisions": out})
}
