package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// Only plans without cancellation or a completion link are mutable. Names saved
// with the assignment preserve the plan even if a member is later hard-deleted.
const trainScheduleSelect = `SELECT s.id, s.date, s.train_type, s.conductor_id,
    COALESCE(c.name, s.conductor_name), s.vip_id, COALESCE(v.name, s.vip_name),
    s.vip_type, s.notes, s.completed_log_id, s.cancelled_at, s.cancelled_by,
    s.created_by, s.created_at, s.updated_at
    FROM train_schedule_entries s
    LEFT JOIN members c ON c.id = s.conductor_id
    LEFT JOIN members v ON v.id = s.vip_id`

func scanTrainSchedule(row interface{ Scan(...any) error }) (TrainScheduleEntry, error) {
	var s TrainScheduleEntry
	err := row.Scan(&s.ID, &s.Date, &s.TrainType, &s.ConductorID, &s.ConductorName,
		&s.VIPID, &s.VIPName, &s.VIPType, &s.Notes, &s.CompletedLogID,
		&s.CancelledAt, &s.CancelledBy, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	s.Status = "planned"
	if s.CompletedLogID != nil {
		s.Status = "completed"
	} else if s.CancelledAt != nil {
		s.Status = "cancelled"
	}
	return s, err
}

// Upcoming keeps every unresolved plan, including overdue ones. Closed entries
// remain only while their scheduled date is today or later; rows are never purged.
func getTrainSchedule(w http.ResponseWriter, r *http.Request) {
	today := gameDate()
	rows, err := db.Query(trainScheduleSelect+` WHERE
        (s.completed_log_id IS NULL AND s.cancelled_at IS NULL) OR s.date >= ?
        ORDER BY s.date, s.id`, today)
	if err != nil {
		dbError(w, "getTrainSchedule", err)
		return
	}
	defer rows.Close()
	entries := []TrainScheduleEntry{}
	for rows.Next() {
		s, err := scanTrainSchedule(rows)
		if err != nil {
			dbError(w, "getTrainSchedule scan", err)
			return
		}
		entries = append(entries, s)
	}
	if err := rows.Err(); err != nil {
		dbError(w, "getTrainSchedule rows", err)
		return
	}
	writeJSON(w, map[string]any{"entries": entries, "game_date": today})
}

func scheduleContext(s TrainScheduleEntry) string {
	out := s.Date + " " + s.TrainType + "; conductor: " + s.ConductorName
	if s.VIPName != "" {
		out += "; guest: " + s.VIPName
		if s.VIPType != nil {
			out += " (" + *s.VIPType + ")"
		}
	}
	return out
}

func postTrainSchedule(w http.ResponseWriter, r *http.Request) { saveTrainSchedule(w, r, false) }
func putTrainSchedule(w http.ResponseWriter, r *http.Request)  { saveTrainSchedule(w, r, true) }

// Schedule validation reuses the log's type/VIP pairing rules; it deliberately
// has its own date rule, since actual train logs must continue rejecting futures.
func saveTrainSchedule(w http.ResponseWriter, r *http.Request, editing bool) {
	var req trainLogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", 400)
		return
	}
	if msg := validateTrainLogRequest(req.Date, req.TrainType, req.ConductorID, req.VIPID, req.VIPType); msg != "" {
		http.Error(w, msg, 400)
		return
	}
	if _, err := time.Parse(dateLayout, req.Date); err != nil {
		http.Error(w, "date must be YYYY-MM-DD", 400)
		return
	}
	today := gameDate()
	if req.Date < today {
		http.Error(w, "A planned train must be today or later in game time", 400)
		return
	}
	req.Notes = strings.TrimSpace(req.Notes)
	tx, err := db.Begin()
	if err != nil {
		dbError(w, "saveTrainSchedule begin", err)
		return
	}
	defer tx.Rollback()
	var old TrainScheduleEntry
	id := 0
	if editing {
		id, _ = strconv.Atoi(mux.Vars(r)["id"])
		old, err = scanTrainSchedule(tx.QueryRow(trainScheduleSelect+` WHERE s.id = ?`, id))
		if !scheduleFound(w, err) {
			return
		}
		if old.Status != "planned" || old.Date < today {
			http.Error(w, "Only a current or future unresolved plan can be edited", 409)
			return
		}
	}
	memberName := func(id int) (string, error) {
		var name, rank string
		err := tx.QueryRow(`SELECT name, rank FROM members WHERE id = ?`, id).Scan(&name, &rank)
		if err == nil && rank == "EX" {
			return "", sql.ErrNoRows
		}
		return name, err
	}
	conductor, err := memberName(req.ConductorID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Choose a current alliance conductor", 400)
		} else {
			dbError(w, "saveTrainSchedule conductor", err)
		}
		return
	}
	vip := ""
	if req.VIPID != nil {
		vip, err = memberName(*req.VIPID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "Choose a current alliance guest", 400)
			} else {
				dbError(w, "saveTrainSchedule guest", err)
			}
			return
		}
	}
	actor := getAuthUser(r)
	action := "created"
	if editing {
		_, err = tx.Exec(`UPDATE train_schedule_entries SET date=?, train_type=?, conductor_id=?, conductor_name=?,
            vip_id=?, vip_name=?, vip_type=?, notes=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
			req.Date, req.TrainType, req.ConductorID, conductor, req.VIPID, vip, req.VIPType, req.Notes, id)
		action = "updated"
	} else {
		var result sql.Result
		result, err = tx.Exec(`INSERT INTO train_schedule_entries
            (date, train_type, conductor_id, conductor_name, vip_id, vip_name, vip_type, notes, created_by)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, req.Date, req.TrainType, req.ConductorID, conductor, req.VIPID, vip, req.VIPType, req.Notes, actor.ID)
		if err == nil {
			n, e := result.LastInsertId()
			id, err = int(n), e
		}
	}
	if err != nil {
		dbError(w, "saveTrainSchedule write", err)
		return
	}
	entry, err := scanTrainSchedule(tx.QueryRow(trainScheduleSelect+` WHERE s.id = ?`, id))
	if err != nil {
		dbError(w, "saveTrainSchedule read", err)
		return
	}
	details := "scheduled; " + scheduleContext(entry)
	if editing {
		details = "plan: " + scheduleContext(old) + " → " + scheduleContext(entry)
		if old.Notes != entry.Notes {
			details += "; notes: " + old.Notes + " → " + entry.Notes
		}
	}
	if err := logActivityTx(tx, actor.ID, actor.Username, action, "train_schedule_entry", entry.Date+" "+entry.TrainType, false, details); err != nil {
		dbError(w, "saveTrainSchedule activity", err)
		return
	}
	if err := tx.Commit(); err != nil {
		dbError(w, "saveTrainSchedule commit", err)
		return
	}
	if !editing {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
	}
	writeJSON(w, entry)
}

func scheduleFound(w http.ResponseWriter, err error) bool {
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Train plan not found", 404)
		return false
	}
	if err != nil {
		dbError(w, "train schedule lookup", err)
		return false
	}
	return true
}

func cancelTrainSchedule(w http.ResponseWriter, r *http.Request) {
	tx, err := db.Begin()
	if err != nil {
		dbError(w, "cancelTrainSchedule begin", err)
		return
	}
	defer tx.Rollback()
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	s, err := scanTrainSchedule(tx.QueryRow(trainScheduleSelect+` WHERE s.id=?`, id))
	if !scheduleFound(w, err) {
		return
	}
	if s.Status != "planned" {
		http.Error(w, "This plan is already completed or cancelled", 409)
		return
	}
	actor := getAuthUser(r)
	if _, err := tx.Exec(`UPDATE train_schedule_entries SET cancelled_at=CURRENT_TIMESTAMP, cancelled_by=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, actor.ID, id); err != nil {
		dbError(w, "cancelTrainSchedule update", err)
		return
	}
	if err := logActivityTx(tx, actor.ID, actor.Username, "updated", "train_schedule_entry", s.Date+" "+s.TrainType, false, "cancelled; "+scheduleContext(s)); err != nil {
		dbError(w, "cancelTrainSchedule activity", err)
		return
	}
	if err := tx.Commit(); err != nil {
		dbError(w, "cancelTrainSchedule commit", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Complete is serialised by the transaction's single DB connection. A retry sees
// the completion link; a failed validation, log insert or link update rolls it all back.
// The body describes actual people/notes only: date and train type come from the plan.
func completeTrainSchedule(w http.ResponseWriter, r *http.Request) {
	actor := getAuthUser(r)
	// Resolve permissions before opening the single-connection transaction.
	if !userHasPermission(actor, "manage_train") {
		http.Error(w, "Completing a train also requires permission to manage train logs", 403)
		return
	}
	var actual struct {
		ConductorID int     `json:"conductor_id"`
		VIPID       *int    `json:"vip_id"`
		VIPType     *string `json:"vip_type"`
		Notes       string  `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&actual); err != nil {
		http.Error(w, "Invalid request body", 400)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		dbError(w, "completeTrainSchedule begin", err)
		return
	}
	defer tx.Rollback()
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	s, err := scanTrainSchedule(tx.QueryRow(trainScheduleSelect+` WHERE s.id=?`, id))
	if !scheduleFound(w, err) {
		return
	}
	if s.Status != "planned" {
		http.Error(w, "This plan is already completed or cancelled", 409)
		return
	}
	if s.Date > gameDate() {
		http.Error(w, "A future train cannot be completed", 409)
		return
	}
	req := trainLogRequest{Date: s.Date, TrainType: s.TrainType, ConductorID: actual.ConductorID, VIPID: actual.VIPID, VIPType: actual.VIPType, Notes: actual.Notes}
	logID, _, status, msg, err := writeTrainLogTx(tx, 0, req, actor.ID, false)
	if err != nil {
		dbError(w, "completeTrainSchedule log", err)
		return
	}
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	if _, err := tx.Exec(`UPDATE train_schedule_entries SET completed_log_id=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, logID, id); err != nil {
		dbError(w, "completeTrainSchedule link", err)
		return
	}
	var conductor string
	if err := tx.QueryRow(`SELECT name FROM members WHERE id=?`, req.ConductorID).Scan(&conductor); err != nil {
		dbError(w, "completeTrainSchedule conductor", err)
		return
	}
	details := "completed; planned: " + scheduleContext(s) + "; actual conductor: " + conductor
	if req.VIPID != nil {
		var guest string
		if err := tx.QueryRow(`SELECT name FROM members WHERE id=?`, *req.VIPID).Scan(&guest); err != nil {
			dbError(w, "completeTrainSchedule guest", err)
			return
		}
		details += "; actual guest: " + guest + " (" + *req.VIPType + ")"
	} else {
		details += "; actual guest: none"
	}
	if err := logActivityTx(tx, actor.ID, actor.Username, "created", "train_log", req.Date+" "+req.TrainType, false, "conductor: "+conductor); err != nil {
		dbError(w, "completeTrainSchedule log activity", err)
		return
	}
	if err := logActivityTx(tx, actor.ID, actor.Username, "updated", "train_schedule_entry", s.Date+" "+s.TrainType, false, details); err != nil {
		dbError(w, "completeTrainSchedule activity", err)
		return
	}
	if err := tx.Commit(); err != nil {
		dbError(w, "completeTrainSchedule commit", err)
		return
	}
	log, err := fetchTrainLog(logID)
	if err != nil {
		dbError(w, "completeTrainSchedule fetch", err)
		return
	}
	writeJSON(w, map[string]any{"train_log": log, "limit_warning": checkDailyLimit(req.Date, req.TrainType, logID)})
}

// SQLite foreign_keys is normally off (database.go). Mirror SET NULL explicitly
// in the app's member/user/log deletion paths, preserving saved assignment names.
func clearTrainScheduleUserReferences(q interface {
	Exec(string, ...any) (sql.Result, error)
}, where string, args ...any) error {
	for _, column := range []string{"created_by", "cancelled_by"} {
		if _, err := q.Exec(`UPDATE train_schedule_entries SET `+column+`=NULL WHERE `+column+` IN (SELECT id FROM users WHERE `+where+`)`, args...); err != nil {
			return fmt.Errorf("train plan %s: %w", column, err)
		}
	}
	return nil
}
