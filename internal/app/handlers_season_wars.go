// handlers_season_wars.go — season wars and faction battles (private-docs 278).
//
// A war is one row from Considering (or Incoming) to Completed (decision 7): the status moves
// only along its direction's flow, Completed needs an outcome and a battle date that has come,
// and Cancelled is open from any status before Completed. A planned, declared or confirmed war
// keeps a calendar row of the season's war type, adopting a pushed Copper War occurrence on its
// date (decision 16). Faction battles are the wider war's results; their W/L per faction is
// derived from the recording alliance's faction at read time.

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

var warStatusFlow = map[string]map[string]bool{
	"considering": {"planned": true, "cancelled": true},
	"planned":     {"considering": true, "declared": true, "cancelled": true},
	"declared":    {"planned": true, "completed": true, "cancelled": true},
	"incoming":    {"confirmed": true, "cancelled": true},
	"confirmed":   {"incoming": true, "completed": true, "cancelled": true},
	"completed":   {},
	"cancelled":   {"considering": true, "incoming": true},
}

var outgoingStatuses = map[string]bool{"considering": true, "planned": true, "declared": true, "completed": true, "cancelled": true}
var incomingStatuses = map[string]bool{"incoming": true, "confirmed": true, "completed": true, "cancelled": true}
var validOutcomes = map[string]bool{"win": true, "loss": true, "draw": true, "unknown": true}
var validStrategyStatuses = map[string]bool{"draft": true, "ready": true, "final": true, "archived": true}

// seasonWar is a war as the War tab carries it.
type seasonWar struct {
	ID                       int     `json:"id"`
	SeasonID                 int     `json:"season_id"`
	WeekNumber               *int    `json:"week_number"`
	Role                     string  `json:"role"`
	Direction                string  `json:"direction"`
	OpponentSeasonAllianceID *int    `json:"opponent_season_alliance_id"`
	OpponentTag              string  `json:"opponent_tag"`
	OpponentName             string  `json:"opponent_name"`
	OpponentServer           int     `json:"opponent_server"`
	BattleDate               string  `json:"battle_date"`
	BattleTime               string  `json:"battle_time"`
	Status                   string  `json:"status"`
	DeclaredByMemberID       *int    `json:"declared_by_member_id"`
	DeclaredByName           string  `json:"declared_by_name"`
	PlannedDeclarationAt     string  `json:"planned_declaration_at"`
	Priority                 *string `json:"priority"`
	Outcome                  *string `json:"outcome"`
	ParticipantCount         *int    `json:"participant_count"`
	ResourceResult           *int64  `json:"resource_result"`
	PollHeld                 bool    `json:"poll_held"`
	StrategyFileID           *int    `json:"strategy_file_id"`
	StrategyCommsID          *int    `json:"strategy_comms_id"`
	MapFileID                *int    `json:"map_file_id"`
	StrategyStatus           string  `json:"strategy_status"`
	KeyObjective             string  `json:"key_objective"`
	SpecialInstructions      string  `json:"special_instructions"`
	Notes                    string  `json:"notes"`
	ReviewWorked             string  `json:"review_worked"`
	ReviewImprove            string  `json:"review_improve"`
	ReviewAdjust             string  `json:"review_adjust"`
	StrategyUpdatedAt        string  `json:"strategy_updated_at"`
	ScheduleEventID          *int    `json:"schedule_event_id"`
	// Snapshot is the frozen figures and matchup (season_war_snapshot.go); the War tab's read fills it.
	Snapshot *warSnapshot `json:"snapshot,omitempty"`
}

const seasonWarSelect = `SELECT w.id, w.season_id, w.week_number, w.role, w.direction, w.opponent_season_alliance_id,
	COALESCE(ea.tag, ''), COALESCE(ea.name, ''), COALESCE(ea.server, 0), COALESCE(w.battle_date, ''), COALESCE(w.battle_time, ''),
	w.status, w.declared_by_member_id, COALESCE(m.name, ''), COALESCE(w.planned_declaration_at, ''), w.priority, w.outcome,
	w.participant_count, w.resource_result, w.poll_held, w.strategy_file_id, w.strategy_comms_id, w.map_file_id,
	w.strategy_status, w.key_objective, w.special_instructions, w.notes, w.review_worked, w.review_improve, w.review_adjust,
	COALESCE(w.strategy_updated_at, ''), w.schedule_event_id
	FROM season_wars w
	LEFT JOIN season_alliances sa ON sa.id = w.opponent_season_alliance_id
	LEFT JOIN external_alliances ea ON ea.id = sa.external_alliance_id
	LEFT JOIN members m ON m.id = w.declared_by_member_id`

func scanSeasonWars(rows *sql.Rows) ([]seasonWar, error) {
	defer rows.Close()
	out := []seasonWar{}
	for rows.Next() {
		var w seasonWar
		var poll int
		var planned string
		if err := rows.Scan(&w.ID, &w.SeasonID, &w.WeekNumber, &w.Role, &w.Direction, &w.OpponentSeasonAllianceID,
			&w.OpponentTag, &w.OpponentName, &w.OpponentServer, &w.BattleDate, &w.BattleTime, &w.Status,
			&w.DeclaredByMemberID, &w.DeclaredByName, &planned, &w.Priority, &w.Outcome, &w.ParticipantCount,
			&w.ResourceResult, &poll, &w.StrategyFileID, &w.StrategyCommsID, &w.MapFileID, &w.StrategyStatus,
			&w.KeyObjective, &w.SpecialInstructions, &w.Notes, &w.ReviewWorked, &w.ReviewImprove, &w.ReviewAdjust,
			&w.StrategyUpdatedAt, &w.ScheduleEventID); err != nil {
			return nil, err
		}
		w.PollHeld = poll == 1
		w.PlannedDeclarationAt = landST(parseLandStored(planned))
		out = append(out, w)
	}
	return out, rows.Err()
}

func loadSeasonWars(q rowQueryer, seasonID int) ([]seasonWar, error) {
	rows, err := q.Query(seasonWarSelect+` WHERE w.season_id = ? ORDER BY w.battle_date IS NULL, w.battle_date, w.id`, seasonID)
	if err != nil {
		return nil, err
	}
	return scanSeasonWars(rows)
}

func loadSeasonWar(q rowQueryer, id int) (*seasonWar, error) {
	rows, err := q.Query(seasonWarSelect+` WHERE w.id = ?`, id)
	if err != nil {
		return nil, err
	}
	wars, err := scanSeasonWars(rows)
	if err != nil {
		return nil, err
	}
	if len(wars) == 0 {
		return nil, sql.ErrNoRows
	}
	return &wars[0], nil
}

// seasonWeekOf is the season week a game date falls in (1-based), nil before the season.
func seasonWeekOf(seasonStart, date string) *int {
	start, err1 := time.Parse("2006-01-02", seasonStart)
	d, err2 := time.Parse("2006-01-02", date)
	if err1 != nil || err2 != nil || d.Before(start) {
		return nil
	}
	w := int(d.Sub(start).Hours()/24)/7 + 1
	return &w
}

// warCalendarStatus maps a war's status onto its calendar row's ("cancelled" when it has none).
func warCalendarStatus(status string) string {
	switch status {
	case "planned":
		return "tentative"
	case "declared", "confirmed":
		return "confirmed"
	case "completed":
		return "completed"
	}
	return "cancelled"
}

// syncWarCalendarTx keeps a war's calendar row in step and stamps its id on the war.
func syncWarCalendarTx(tx *sql.Tx, s *Season, w *seasonWar, actorID int) (syncResult, error) {
	cfg, err := loadWarConfig(tx, s.ID)
	if err != nil {
		return syncResult{}, err
	}
	status := warCalendarStatus(w.Status)
	if w.BattleDate == "" {
		status = "cancelled"
	}
	if status != "cancelled" && cfg.WarEventTypeID == nil {
		return syncResult{Action: "refused", Message: "no war event type configured; nothing scheduled"}, nil
	}
	typeID := 0
	if cfg.WarEventTypeID != nil {
		typeID = *cfg.WarEventTypeID
	}
	tm := w.BattleTime
	if tm == "" {
		// No time of its own: keep its row's, else a pushed occurrence's that day, else the reset.
		tx.QueryRow(`SELECT event_time FROM schedule_events WHERE source_kind = ? AND source_id = ?`, sourceWar, w.ID).Scan(&tm)
		if tm == "" && typeID != 0 {
			tx.QueryRow(`SELECT event_time FROM schedule_events WHERE event_type_id = ? AND event_date = ? AND source_kind IS NULL
				AND status != 'cancelled' ORDER BY season_event_id IS NULL, id LIMIT 1`, typeID, w.BattleDate).Scan(&tm)
		}
		if tm == "" {
			tm = "00:00"
		}
	}
	notes := strings.Title(w.Role)
	if w.OpponentTag != "" {
		notes += fmt.Sprintf(" vs [%s] %s (S%d)", w.OpponentTag, w.OpponentName, w.OpponentServer)
	}
	res, err := syncSourcedScheduleEvent(tx, sourcedEvent{Kind: sourceWar, ID: int64(w.ID), SeasonID: s.ID, Date: w.BattleDate,
		Time: tm, TypeID: typeID, Notes: strings.TrimSpace(notes), Status: status, AdoptPushed: true}, actorID)
	if err != nil {
		return res, err
	}
	if res.EventID != 0 && (w.ScheduleEventID == nil || int64(*w.ScheduleEventID) != res.EventID) {
		if _, err := tx.Exec(`UPDATE season_wars SET schedule_event_id = ? WHERE id = ?`, res.EventID, w.ID); err != nil {
			return res, err
		}
	}
	return res, nil
}

// warWrite is the create/update body; absent fields keep their stored value on update.
type warWrite struct {
	SeasonID                 int         `json:"season_id"`
	Role                     *string     `json:"role"`
	Direction                *string     `json:"direction"`
	OpponentSeasonAllianceID optionalInt `json:"opponent_season_alliance_id"`
	BattleDate               *string     `json:"battle_date"`
	BattleTime               *string     `json:"battle_time"`
	Status                   *string     `json:"status"`
	DeclaredByMemberID       optionalInt `json:"declared_by_member_id"`
	PlannedDeclarationAt     *string     `json:"planned_declaration_at"`
	Priority                 *string     `json:"priority"`
	Outcome                  *string     `json:"outcome"`
	ParticipantCount         optionalInt `json:"participant_count"`
	ResourceResult           *int64      `json:"resource_result"`
	ClearResourceResult      bool        `json:"clear_resource_result"`
	PollHeld                 *bool       `json:"poll_held"`
	StrategyFileID           optionalInt `json:"strategy_file_id"`
	StrategyCommsID          optionalInt `json:"strategy_comms_id"`
	MapFileID                optionalInt `json:"map_file_id"`
	StrategyStatus           *string     `json:"strategy_status"`
	KeyObjective             *string     `json:"key_objective"`
	SpecialInstructions      *string     `json:"special_instructions"`
	Notes                    *string     `json:"notes"`
}

// applyWarWrite lays a body over a war (a zero war for a create) and validates the result.
// The string is the refusal; strategyChanged says whether a strategy field moved.
func applyWarWrite(w *seasonWar, b warWrite, s *Season, creating bool, today string) (string, bool) {
	oldStatus := w.Status
	strategyChanged := false
	str := func(dst *string, v *string, strategy bool) {
		if v != nil {
			nv := strings.TrimSpace(*v)
			if strategy && nv != *dst {
				strategyChanged = true
			}
			*dst = nv
		}
	}
	opt := func(dst **int, v optionalInt, strategy bool) {
		if v.Set {
			if strategy && !sameIntPtr(*dst, v.Value) {
				strategyChanged = true
			}
			*dst = v.Value
		}
	}
	str(&w.Role, b.Role, false)
	str(&w.Direction, b.Direction, false)
	opt(&w.OpponentSeasonAllianceID, b.OpponentSeasonAllianceID, false)
	str(&w.BattleDate, b.BattleDate, false)
	str(&w.BattleTime, b.BattleTime, false)
	str(&w.Status, b.Status, false)
	opt(&w.DeclaredByMemberID, b.DeclaredByMemberID, false)
	str(&w.PlannedDeclarationAt, b.PlannedDeclarationAt, false)
	if b.Priority != nil {
		if *b.Priority == "" {
			w.Priority = nil
		} else {
			v := *b.Priority
			w.Priority = &v
		}
	}
	if b.Outcome != nil {
		if *b.Outcome == "" {
			w.Outcome = nil
		} else {
			v := *b.Outcome
			w.Outcome = &v
		}
	}
	opt(&w.ParticipantCount, b.ParticipantCount, false)
	if b.ResourceResult != nil {
		w.ResourceResult = b.ResourceResult
	} else if b.ClearResourceResult {
		w.ResourceResult = nil
	}
	if b.PollHeld != nil {
		w.PollHeld = *b.PollHeld
	}
	opt(&w.StrategyFileID, b.StrategyFileID, true)
	opt(&w.StrategyCommsID, b.StrategyCommsID, true)
	opt(&w.MapFileID, b.MapFileID, true)
	str(&w.StrategyStatus, b.StrategyStatus, true)
	str(&w.KeyObjective, b.KeyObjective, true)
	str(&w.SpecialInstructions, b.SpecialInstructions, true)
	str(&w.Notes, b.Notes, false)

	if w.Direction == "" {
		w.Direction = "outgoing"
	}
	if w.Direction != "outgoing" && w.Direction != "incoming" {
		return "direction must be outgoing or incoming", false
	}
	if w.Role != "attack" && w.Role != "defense" {
		return "role must be attack or defense", false
	}
	if w.Status == "" {
		w.Status = map[string]string{"outgoing": "considering", "incoming": "incoming"}[w.Direction]
	}
	if w.StrategyStatus == "" {
		w.StrategyStatus = "draft"
	}
	if !validStrategyStatuses[w.StrategyStatus] {
		return "strategy status must be draft, ready, final or archived", false
	}
	if w.Direction == "outgoing" && !outgoingStatuses[w.Status] {
		return "an outgoing war's status is considering, planned, declared, completed or cancelled", false
	}
	if w.Direction == "incoming" && !incomingStatuses[w.Status] {
		return "an incoming war's status is incoming, confirmed, completed or cancelled", false
	}
	if !creating && w.Status != oldStatus && !warStatusFlow[oldStatus][w.Status] {
		return fmt.Sprintf("a war can't go from %s to %s", oldStatus, w.Status), false
	}
	if w.BattleDate != "" {
		if _, err := time.Parse("2006-01-02", w.BattleDate); err != nil {
			return "battle_date must be YYYY-MM-DD", false
		}
		w.WeekNumber = seasonWeekOf(s.StartDate, w.BattleDate)
	}
	if w.BattleTime != "" && !validHHMM(w.BattleTime) {
		return "battle_time must be HH:MM", false
	}
	if w.PlannedDeclarationAt != "" {
		if _, err := parseLandST(w.PlannedDeclarationAt); err != nil {
			return "planned declaration must be YYYY-MM-DD HH:MM (server time)", false
		}
	}
	if w.Priority != nil && *w.Priority != "high" && *w.Priority != "medium" && *w.Priority != "low" {
		return "priority must be high, medium or low", false
	}
	if w.Outcome != nil && !validOutcomes[*w.Outcome] {
		return "outcome must be win, loss, draw or unknown", false
	}
	if w.Status == "completed" {
		if w.Outcome == nil {
			return "a completed war needs its outcome", false
		}
		if w.BattleDate == "" || w.BattleDate > today {
			return "a war is completed only once its battle date has come", false
		}
	}
	if w.ParticipantCount != nil && (*w.ParticipantCount < 0 || *w.ParticipantCount > 200) {
		return "participants is from 0 to 200", false
	}
	return "", strategyChanged
}

func sameIntPtr(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// checkWarReferencesTx refuses an opponent, file or mail that isn't this season's.
func checkWarReferences(seasonID int, w *seasonWar) string {
	if w.OpponentSeasonAllianceID != nil {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM season_alliances WHERE id = ? AND season_id = ?`, *w.OpponentSeasonAllianceID, seasonID).Scan(&n)
		if n == 0 {
			return "the opponent is not one of this season's alliances"
		}
	}
	for _, f := range []*int{w.StrategyFileID, w.MapFileID} {
		if f == nil {
			continue
		}
		var sid sql.NullInt64
		if err := db.QueryRow(`SELECT season_id FROM files WHERE id = ?`, *f).Scan(&sid); err != nil || !sid.Valid || int(sid.Int64) != seasonID {
			return "a strategy or map file must be a file tagged to this season"
		}
	}
	if w.StrategyCommsID != nil {
		var sid sql.NullInt64
		if err := db.QueryRow(`SELECT season_id FROM comms_templates WHERE id = ?`, *w.StrategyCommsID).Scan(&sid); err != nil || !sid.Valid || int(sid.Int64) != seasonID {
			return "the battle mail must be a mail tagged to this season"
		}
	}
	return ""
}

// writeWarTx inserts or updates a war row from w.
func writeWarTx(tx *sql.Tx, w *seasonWar, strategyChanged bool, userID int) error {
	var planned any
	if w.PlannedDeclarationAt != "" {
		t, _ := parseLandST(w.PlannedDeclarationAt)
		planned = landUTC(t)
	}
	var date, tm any
	if w.BattleDate != "" {
		date = w.BattleDate
	}
	if w.BattleTime != "" {
		tm = w.BattleTime
	}
	stamp := any(nil)
	if strategyChanged {
		stamp = landUTC(time.Now())
	}
	if w.ID == 0 {
		res, err := tx.Exec(`INSERT INTO season_wars (season_id, week_number, role, direction, opponent_season_alliance_id,
			battle_date, battle_time, status, declared_by_member_id, planned_declaration_at, priority, outcome, participant_count,
			resource_result, poll_held, strategy_file_id, strategy_comms_id, map_file_id, strategy_status, key_objective,
			special_instructions, notes, strategy_updated_at, created_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			w.SeasonID, w.WeekNumber, w.Role, w.Direction, w.OpponentSeasonAllianceID, date, tm, w.Status,
			w.DeclaredByMemberID, planned, w.Priority, w.Outcome, w.ParticipantCount, w.ResourceResult, boolToInt(w.PollHeld),
			w.StrategyFileID, w.StrategyCommsID, w.MapFileID, w.StrategyStatus, w.KeyObjective, w.SpecialInstructions,
			w.Notes, stamp, userID)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		w.ID = int(id)
		return nil
	}
	_, err := tx.Exec(`UPDATE season_wars SET week_number = ?, role = ?, direction = ?, opponent_season_alliance_id = ?,
		battle_date = ?, battle_time = ?, status = ?, declared_by_member_id = ?, planned_declaration_at = ?, priority = ?,
		outcome = ?, participant_count = ?, resource_result = ?, poll_held = ?, strategy_file_id = ?, strategy_comms_id = ?,
		map_file_id = ?, strategy_status = ?, key_objective = ?, special_instructions = ?, notes = ?,
		strategy_updated_at = COALESCE(?, strategy_updated_at), updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		w.WeekNumber, w.Role, w.Direction, w.OpponentSeasonAllianceID, date, tm, w.Status, w.DeclaredByMemberID, planned,
		w.Priority, w.Outcome, w.ParticipantCount, w.ResourceResult, boolToInt(w.PollHeld), w.StrategyFileID,
		w.StrategyCommsID, w.MapFileID, w.StrategyStatus, w.KeyObjective, w.SpecialInstructions, w.Notes, stamp, w.ID)
	return err
}

// saveWar runs a create or an update: validate, write, sync the calendar, log.
func saveWar(w http.ResponseWriter, r *http.Request, existing *seasonWar, body warWrite) {
	user := getAuthUser(r)
	s := landSeason(w, body.SeasonID)
	if s == nil {
		return
	}
	if ok, msg := seasonAcceptsEdits(s.ID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	war := seasonWar{SeasonID: s.ID, PollHeld: true}
	creating := existing == nil
	if !creating {
		war = *existing
	}
	oldStatus := war.Status
	msg, strategyChanged := applyWarWrite(&war, body, s, creating, gametimeDateOf(time.Now()))
	if msg == "" {
		msg = checkWarReferences(s.ID, &war)
	}
	if msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("saveWar: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	if err := writeWarTx(tx, &war, strategyChanged, user.ID); err != nil {
		slog.Error("saveWar: write", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	// Reload for the opponent's identity (the calendar notes name it).
	fresh, err := loadSeasonWarTx(tx, war.ID)
	if err != nil {
		slog.Error("saveWar: reload", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	cal, err := syncWarCalendarTx(tx, s, fresh, user.ID)
	if err != nil {
		slog.Error("saveWar: calendar", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	snapped, err := snapshotWarTx(tx, existing, fresh, user.ID)
	if err != nil {
		slog.Error("saveWar: snapshot", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("saveWar: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	label := warLabel(fresh)
	if creating {
		logActivity(user.ID, user.Username, "created", "season_war", label, false, joinDetails("status "+war.Status, snapped))
	} else {
		detail := ""
		if oldStatus != war.Status {
			detail = "status: " + oldStatus + " → " + war.Status
		}
		logActivity(user.ID, user.Username, "updated", "season_war", label, false, joinDetails(detail, snapped))
	}
	writeJSON(w, map[string]any{"id": war.ID, "calendar": cal})
}

func loadSeasonWarTx(tx *sql.Tx, id int) (*seasonWar, error) {
	rows, err := tx.Query(seasonWarSelect+` WHERE w.id = ?`, id)
	if err != nil {
		return nil, err
	}
	wars, err := scanSeasonWars(rows)
	if err != nil {
		return nil, err
	}
	if len(wars) == 0 {
		return nil, sql.ErrNoRows
	}
	return &wars[0], nil
}

func warLabel(w *seasonWar) string {
	l := strings.Title(w.Role)
	if w.OpponentTag != "" {
		l += " vs [" + w.OpponentTag + "]"
	}
	if w.BattleDate != "" {
		l += " " + w.BattleDate
	}
	return l + ", " + seasonTagName(&w.SeasonID)
}

func gametimeDateOf(t time.Time) string { return t.In(gametimeLoc()).Format("2006-01-02") }

// POST /api/season-hub/war/wars
func handleSeasonWarCreate(w http.ResponseWriter, r *http.Request) {
	var body warWrite
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	saveWar(w, r, nil, body)
}

// warFromPath loads the war a route names.
func warFromPath(w http.ResponseWriter, r *http.Request) *seasonWar {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid war ID", http.StatusBadRequest)
		return nil
	}
	war, err := loadSeasonWar(db, id)
	if err == sql.ErrNoRows {
		http.Error(w, "War not found", http.StatusNotFound)
		return nil
	}
	if err != nil {
		slog.Error("warFromPath", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return nil
	}
	return war
}

// PUT /api/season-hub/war/wars/{id}
func handleSeasonWarUpdate(w http.ResponseWriter, r *http.Request) {
	war := warFromPath(w, r)
	if war == nil {
		return
	}
	var body warWrite
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	body.SeasonID = war.SeasonID
	saveWar(w, r, war, body)
}

// PUT /api/season-hub/war/wars/{id}/review — the post-battle review, saved on its own so it
// never overwrites the plan.
func handleSeasonWarReview(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	war := warFromPath(w, r)
	if war == nil {
		return
	}
	if ok, msg := seasonAcceptsEdits(war.SeasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	var body struct {
		Worked  string `json:"review_worked"`
		Improve string `json:"review_improve"`
		Adjust  string `json:"review_adjust"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if _, err := db.Exec(`UPDATE season_wars SET review_worked = ?, review_improve = ?, review_adjust = ?,
		updated_at = CURRENT_TIMESTAMP WHERE id = ?`, strings.TrimSpace(body.Worked), strings.TrimSpace(body.Improve),
		strings.TrimSpace(body.Adjust), war.ID); err != nil {
		slog.Error("handleSeasonWarReview", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "updated", "season_war", warLabel(war), false, "review")
	writeJSON(w, map[string]string{"message": "Saved"})
}

// DELETE /api/season-hub/war/wars/{id} — only a war still being considered, or cancelled; its
// calendar row is cancelled, never deleted. Anything further along is cancelled instead.
func handleSeasonWarDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	war := warFromPath(w, r)
	if war == nil {
		return
	}
	if ok, msg := seasonAcceptsEdits(war.SeasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	if war.Status != "considering" && war.Status != "cancelled" {
		http.Error(w, "only a war being considered, or a cancelled one, can be deleted — cancel it instead", http.StatusConflict)
		return
	}
	if msg := warInUse(war.ID); msg != "" {
		http.Error(w, msg, http.StatusConflict)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleSeasonWarDelete: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	if _, err := syncSourcedScheduleEvent(tx, sourcedEvent{Kind: sourceWar, ID: int64(war.ID), Status: "cancelled"}, user.ID); err != nil {
		slog.Error("handleSeasonWarDelete: calendar", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(`UPDATE season_trade_posts SET related_war_id = NULL WHERE related_war_id = ?`, war.ID); err != nil {
		slog.Error("handleSeasonWarDelete: unlink posts", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(`DELETE FROM season_war_snapshots WHERE war_id = ?`, war.ID); err != nil {
		slog.Error("handleSeasonWarDelete: snapshot", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(`DELETE FROM season_wars WHERE id = ?`, war.ID); err == nil {
		err = tx.Commit()
	} else {
		slog.Error("handleSeasonWarDelete: delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "deleted", "season_war", warLabel(war), false)
	writeJSON(w, map[string]string{"message": "Deleted"})
}

// warInUse names what still hangs off a war (attendance, polls — later issues), or "".
var warInUseChecks []func(int) string

func warInUse(id int) string {
	for _, c := range warInUseChecks {
		if msg := c(id); msg != "" {
			return msg
		}
	}
	return ""
}

// ── Faction battles ────────────────────────────────────────────────────────

type factionBattle struct {
	ID                       int    `json:"id"`
	WeekNumber               *int   `json:"week_number"`
	BattleDate               string `json:"battle_date"`
	AllianceSeasonAllianceID int    `json:"alliance_season_alliance_id"`
	OpponentSeasonAllianceID *int   `json:"opponent_season_alliance_id"`
	OpponentLabel            string `json:"opponent_label"`
	Role                     string `json:"role"`
	Outcome                  string `json:"outcome"`
	RankGroupID              *int   `json:"rank_group_id"`
	Notes                    string `json:"notes"`
	Source                   string `json:"source"`
}

func loadFactionBattles(q rowQueryer, seasonID int) ([]factionBattle, error) {
	rows, err := q.Query(`SELECT id, week_number, battle_date, alliance_season_alliance_id, opponent_season_alliance_id,
		opponent_label, role, outcome, rank_group_id, notes, source FROM season_faction_battles WHERE season_id = ?
		ORDER BY battle_date, id`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []factionBattle{}
	for rows.Next() {
		var b factionBattle
		if err := rows.Scan(&b.ID, &b.WeekNumber, &b.BattleDate, &b.AllianceSeasonAllianceID, &b.OpponentSeasonAllianceID,
			&b.OpponentLabel, &b.Role, &b.Outcome, &b.RankGroupID, &b.Notes, &b.Source); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// validateFactionBattle checks one battle against the season's alliances (by id → faction).
func validateFactionBattle(b *factionBattle, s *Season, alliances map[int]SeasonAlliance) string {
	b.OpponentLabel, b.Notes = strings.TrimSpace(b.OpponentLabel), strings.TrimSpace(b.Notes)
	if _, err := time.Parse("2006-01-02", b.BattleDate); err != nil {
		return "the date must be YYYY-MM-DD"
	}
	if _, ok := alliances[b.AllianceSeasonAllianceID]; !ok {
		return "the alliance is not one of this season's alliances"
	}
	if b.OpponentSeasonAllianceID != nil {
		if _, ok := alliances[*b.OpponentSeasonAllianceID]; !ok {
			return "the opponent is not one of this season's alliances"
		}
		if *b.OpponentSeasonAllianceID == b.AllianceSeasonAllianceID {
			return "an alliance does not fight itself"
		}
	}
	if b.Role != "attack" && b.Role != "defense" {
		return "role must be attack or defense"
	}
	if b.Outcome == "" {
		b.Outcome = "unknown"
	}
	if !validOutcomes[b.Outcome] {
		return "outcome must be win, loss, draw or unknown"
	}
	b.WeekNumber = seasonWeekOf(s.StartDate, b.BattleDate)
	return ""
}

// writeFactionBattleTx inserts (id 0) or updates a battle; a duplicate (same date, alliance and
// role) reports false with no error when it is identical, and an error message otherwise.
func writeFactionBattleTx(tx *sql.Tx, seasonID int, b factionBattle, source string, userID int) (bool, string, error) {
	var existing int
	var outcome, label string
	var opp sql.NullInt64
	err := tx.QueryRow(`SELECT id, outcome, opponent_label, opponent_season_alliance_id FROM season_faction_battles
		WHERE season_id = ? AND battle_date = ? AND alliance_season_alliance_id = ? AND role = ?`,
		seasonID, b.BattleDate, b.AllianceSeasonAllianceID, b.Role).Scan(&existing, &outcome, &label, &opp)
	if err != nil && err != sql.ErrNoRows {
		return false, "", err
	}
	if err == nil && existing != b.ID {
		if b.ID == 0 && outcome == b.Outcome && label == b.OpponentLabel && sameIntPtr(nullIntPtr(opp), b.OpponentSeasonAllianceID) {
			return false, "", nil
		}
		return false, fmt.Sprintf("that alliance already has a %s battle recorded on %s", b.Role, b.BattleDate), nil
	}
	if b.ID == 0 {
		var by any
		if userID > 0 {
			by = userID
		}
		_, err = tx.Exec(`INSERT INTO season_faction_battles (season_id, week_number, battle_date, alliance_season_alliance_id,
			opponent_season_alliance_id, opponent_label, role, outcome, rank_group_id, notes, source, recorded_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, seasonID, b.WeekNumber, b.BattleDate, b.AllianceSeasonAllianceID,
			b.OpponentSeasonAllianceID, b.OpponentLabel, b.Role, b.Outcome, b.RankGroupID, b.Notes, source, by)
		return err == nil, "", err
	}
	res, err := tx.Exec(`UPDATE season_faction_battles SET week_number = ?, battle_date = ?, alliance_season_alliance_id = ?,
		opponent_season_alliance_id = ?, opponent_label = ?, role = ?, outcome = ?, rank_group_id = ?, notes = ?
		WHERE id = ? AND season_id = ?`, b.WeekNumber, b.BattleDate, b.AllianceSeasonAllianceID, b.OpponentSeasonAllianceID,
		b.OpponentLabel, b.Role, b.Outcome, b.RankGroupID, b.Notes, b.ID, seasonID)
	if err != nil {
		return false, "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, fmt.Sprintf("battle %d is not this season's", b.ID), nil
	}
	return true, "", nil
}

func nullIntPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	i := int(v.Int64)
	return &i
}

func seasonAllianceMap(list []SeasonAlliance) map[int]SeasonAlliance {
	m := map[int]SeasonAlliance{}
	for _, a := range list {
		m[a.ID] = a
	}
	return m
}

// PUT /api/season-hub/war/faction-battles {season_id, battles: [...]} — saves every row sent
// (new rows have no id) in one transaction; the first refusal names its row and saves nothing.
func handleSeasonFactionBattlesPut(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var body struct {
		SeasonID int             `json:"season_id"`
		Battles  []factionBattle `json:"battles"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	s := landSeason(w, body.SeasonID)
	if s == nil {
		return
	}
	if ok, msg := seasonAcceptsEdits(s.ID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	list, err := loadSeasonAlliances(db, s.ID)
	if err != nil {
		slog.Error("handleSeasonFactionBattlesPut: alliances", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	alliances := seasonAllianceMap(list)
	for i := range body.Battles {
		if msg := validateFactionBattle(&body.Battles[i], s, alliances); msg != "" {
			http.Error(w, fmt.Sprintf("row %d: %s", i+1, msg), http.StatusBadRequest)
			return
		}
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleSeasonFactionBattlesPut: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	written := 0
	for i, b := range body.Battles {
		ok, msg, err := writeFactionBattleTx(tx, s.ID, b, "manual", user.ID)
		if err != nil {
			slog.Error("handleSeasonFactionBattlesPut: write", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if msg != "" {
			http.Error(w, fmt.Sprintf("row %d: %s", i+1, msg), http.StatusConflict)
			return
		}
		if ok {
			written++
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handleSeasonFactionBattlesPut: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if written > 0 {
		logActivity(user.ID, user.Username, "updated", "season_faction_battle", seasonTagName(&s.ID), false,
			strconv.Itoa(written)+" battles")
	}
	writeJSON(w, map[string]any{"written": written})
}

// DELETE /api/season-hub/war/faction-battles/{id}
func handleSeasonFactionBattleDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid battle ID", http.StatusBadRequest)
		return
	}
	var seasonID int
	var date string
	if err := db.QueryRow(`SELECT season_id, battle_date FROM season_faction_battles WHERE id = ?`, id).Scan(&seasonID, &date); err == sql.ErrNoRows {
		http.Error(w, "Battle not found", http.StatusNotFound)
		return
	} else if err != nil {
		slog.Error("handleSeasonFactionBattleDelete: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if ok, msg := seasonAcceptsEdits(seasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	if _, err := db.Exec(`DELETE FROM season_faction_battles WHERE id = ?`, id); err != nil {
		slog.Error("handleSeasonFactionBattleDelete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "deleted", "season_faction_battle", seasonTagName(&seasonID), false, date)
	writeJSON(w, map[string]string{"message": "Deleted"})
}

// warRecordsFor derives our record and the faction records for the War tab.
func warRecordsFor(wars []seasonWar, battles []factionBattle, alliances []SeasonAlliance, factions []SeasonFaction, today string) (warRecord, []factionRecord) {
	rows := make([]warRecordRow, 0, len(wars))
	for _, w := range wars {
		o := ""
		if w.Outcome != nil {
			o = *w.Outcome
		}
		rows = append(rows, warRecordRow{Status: w.Status, Role: w.Role, Outcome: o, BattleDate: w.BattleDate})
	}
	byID := seasonAllianceMap(alliances)
	fb := make([]factionBattleRow, 0, len(battles))
	for _, b := range battles {
		week := 0
		if b.WeekNumber != nil {
			week = *b.WeekNumber
		}
		fb = append(fb, factionBattleRow{FactionID: byID[b.AllianceSeasonAllianceID].FactionID, Week: week, Outcome: b.Outcome})
	}
	order := make([]int, 0, len(factions))
	for _, f := range factions {
		order = append(order, f.ID)
	}
	return computeWarRecord(rows, today), computeFactionRecords(fb, order)
}

// warAllianceInUse names a season alliance's wars and faction battles.
func warAllianceInUse(id int) string {
	var wars, battles int
	db.QueryRow(`SELECT COUNT(*) FROM season_wars WHERE opponent_season_alliance_id = ?`, id).Scan(&wars)
	db.QueryRow(`SELECT COUNT(*) FROM season_faction_battles WHERE alliance_season_alliance_id = ? OR opponent_season_alliance_id = ?`, id, id).Scan(&battles)
	if wars+battles > 0 {
		return fmt.Sprintf("this alliance is in %d wars and %d faction battles this season", wars, battles)
	}
	return ""
}

// ── CSV kinds ──────────────────────────────────────────────────────────────

// importAllianceRef resolves a tag + server cell pair to a season alliance (0, message on a miss).
func importAllianceRef(ctx *importCtx, tag, server string) (int, string) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return 0, ""
	}
	n, err := strconv.Atoi(strings.TrimSpace(server))
	if err != nil || n <= 0 {
		return 0, "[" + tag + "] needs its server"
	}
	if id := seasonAllianceByTagServer(ctx.Data["alliances"].([]SeasonAlliance), tag, n); id != 0 {
		return id, ""
	}
	return 0, fmt.Sprintf("[%s] on server %d is not one of this season's alliances", tag, n)
}

func init() {
	registerSeasonImportKind("faction_battles", importKind{
		Title: "Faction battles",
		Perms: []string{"manage_season_war"},
		Columns: []importColumn{
			{Key: "date", Label: "Date", Aliases: []string{"battle_date"}, Required: true},
			{Key: "alliance_tag", Label: "Alliance tag", Aliases: []string{"alliance", "tag"}, Required: true},
			{Key: "alliance_server", Label: "Alliance server", Aliases: []string{"server"}, Required: true},
			{Key: "opponent_tag", Label: "Opponent tag", Aliases: []string{"opponent"}},
			{Key: "opponent_server", Label: "Opponent server"},
			{Key: "role", Label: "Role", Aliases: []string{"attack_defense", "side"}, Required: true},
			{Key: "outcome", Label: "Outcome", Aliases: []string{"result"}, Required: true},
			{Key: "notes", Label: "Notes", Aliases: []string{"note"}},
		},
		Prepare: func(ctx *importCtx) error {
			a, err := loadSeasonAlliances(db, ctx.Season.ID)
			ctx.Data["alliances"] = a
			ctx.Data["byID"] = seasonAllianceMap(a)
			return err
		},
		Parse: func(ctx *importCtx, rec map[string]string) (any, map[string]any, []string) {
			summary := map[string]any{"date": rec["date"], "alliance": rec["alliance_tag"], "opponent": rec["opponent_tag"],
				"role": rec["role"], "outcome": rec["outcome"]}
			b := factionBattle{BattleDate: rec["date"], Role: strings.ToLower(rec["role"]), Outcome: strings.ToLower(rec["outcome"]), Notes: rec["notes"]}
			id, msg := importAllianceRef(ctx, rec["alliance_tag"], rec["alliance_server"])
			if msg != "" || id == 0 {
				if msg == "" {
					msg = "Alliance tag is empty"
				}
				return nil, summary, []string{msg}
			}
			b.AllianceSeasonAllianceID = id
			if opp, msg := importAllianceRef(ctx, rec["opponent_tag"], rec["opponent_server"]); msg != "" {
				// An opponent outside the season's list is kept as a label.
				b.OpponentLabel = rec["opponent_tag"]
				if rec["opponent_server"] != "" {
					b.OpponentLabel += " (S" + rec["opponent_server"] + ")"
				}
			} else if opp != 0 {
				b.OpponentSeasonAllianceID = &opp
			}
			if msg := validateFactionBattle(&b, ctx.Season, ctx.Data["byID"].(map[int]SeasonAlliance)); msg != "" {
				return nil, summary, []string{msg}
			}
			return b, summary, nil
		},
		Write: func(tx *sql.Tx, ctx *importCtx, items []any) (int, int, error) {
			written, unchanged := 0, 0
			for _, it := range items {
				ok, msg, err := writeFactionBattleTx(tx, ctx.Season.ID, it.(factionBattle), "csv", ctx.User.ID)
				if err != nil {
					return 0, 0, err
				}
				if msg != "" {
					return 0, 0, fmt.Errorf("%s", msg)
				}
				if ok {
					written++
				} else {
					unchanged++
				}
			}
			return written, unchanged, nil
		},
	})

	registerSeasonImportKind("war_results", importKind{
		Title: "War results",
		Perms: []string{"manage_season_war"},
		Columns: []importColumn{
			{Key: "date", Label: "Date", Aliases: []string{"battle_date"}, Required: true},
			{Key: "role", Label: "Role", Aliases: []string{"attack_defense", "side"}, Required: true},
			{Key: "outcome", Label: "Outcome", Aliases: []string{"result"}, Required: true},
			{Key: "participants", Label: "Participants", Aliases: []string{"participant_count"}},
			{Key: "resources", Label: "Resources", Aliases: []string{"resource_result", "loot"}},
		},
		Prepare: func(ctx *importCtx) error {
			w, err := loadSeasonWars(db, ctx.Season.ID)
			ctx.Data["wars"] = w
			return err
		},
		Parse: func(ctx *importCtx, rec map[string]string) (any, map[string]any, []string) {
			summary := map[string]any{"date": rec["date"], "role": rec["role"], "outcome": rec["outcome"],
				"participants": rec["participants"], "resources": rec["resources"]}
			r := warResultIn{BattleDate: rec["date"], Role: strings.ToLower(rec["role"]), Outcome: strings.ToLower(rec["outcome"])}
			if v, err := optIntCell(rec["participants"]); err != nil {
				return nil, summary, []string{"Participants is not a whole number"}
			} else {
				r.ParticipantCount = v
			}
			if v, err := optIntCell(rec["resources"]); err != nil {
				return nil, summary, []string{"Resources is not a whole number"}
			} else if v != nil {
				x := int64(*v)
				r.ResourceResult = &x
			}
			war, msg := matchWarResult(ctx.Data["wars"].([]seasonWar), r)
			if msg != "" {
				return nil, summary, []string{msg}
			}
			summary["war"] = warLabel(war)
			r.WarID = war.ID
			return r, summary, nil
		},
		Write: func(tx *sql.Tx, ctx *importCtx, items []any) (int, int, error) {
			written, unchanged := 0, 0
			for _, it := range items {
				ok, err := writeWarResultTx(tx, it.(warResultIn))
				if err != nil {
					return 0, 0, err
				}
				if ok {
					written++
				} else {
					unchanged++
				}
			}
			return written, unchanged, nil
		},
	})
}

// warResultIn is a result for one war, by id or by (battle date, role).
type warResultIn struct {
	WarID            int    `json:"war_id"`
	BattleDate       string `json:"battle_date"`
	Role             string `json:"role"`
	Outcome          string `json:"outcome"`
	ParticipantCount *int   `json:"participant_count"`
	ResourceResult   *int64 `json:"resource_result"`
}

// matchWarResult finds the war a result names; the result's own fields are checked too.
func matchWarResult(wars []seasonWar, r warResultIn) (*seasonWar, string) {
	if !validOutcomes[r.Outcome] {
		return nil, "outcome must be win, loss, draw or unknown"
	}
	if r.ParticipantCount != nil && (*r.ParticipantCount < 0 || *r.ParticipantCount > 200) {
		return nil, "participants is from 0 to 200"
	}
	var found []*seasonWar
	for i := range wars {
		w := &wars[i]
		if (r.WarID != 0 && w.ID == r.WarID) || (r.WarID == 0 && w.BattleDate == r.BattleDate && w.Role == r.Role && w.Status != "cancelled") {
			found = append(found, w)
		}
	}
	if len(found) == 0 {
		return nil, "no war of this season matches (date and role, not cancelled)"
	}
	if len(found) > 1 {
		return nil, "more than one war matches that date and role — send war_id"
	}
	return found[0], ""
}

// writeWarResultTx records a result: outcome, participants and resources. It does not change
// the status — completing a war is an officer's step on the War tab.
func writeWarResultTx(tx *sql.Tx, r warResultIn) (bool, error) {
	res, err := tx.Exec(`UPDATE season_wars SET outcome = ?, participant_count = COALESCE(?, participant_count),
		resource_result = COALESCE(?, resource_result), updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND NOT (outcome IS ? AND participant_count IS COALESCE(?, participant_count)
		AND resource_result IS COALESCE(?, resource_result))`,
		r.Outcome, r.ParticipantCount, r.ResourceResult, r.WarID, r.Outcome, r.ParticipantCount, r.ResourceResult)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
