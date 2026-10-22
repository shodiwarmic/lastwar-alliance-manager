// handlers_season_week_events.go — the weekly event detail (private-docs 281).
//
// A season week's events (its Core set: label, kind, whether Core requires it), ordered, and
// each member's result in each — entered in the Participation grid's detail row and saved with
// Save Week. The weekly score stays the officers' call; the detail neither sets nor suggests it.

package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

var validEventResults = map[string]bool{"full": true, "partial": true, "absent": true, "excused": true, "completed": true, "na": true}

// weekEvent is one event of a season week.
type weekEvent struct {
	ID              int    `json:"id"`
	WeekNumber      int    `json:"week_number"`
	Label           string `json:"label"`
	Kind            string `json:"kind"`
	RequiredForCore bool   `json:"required_for_core"`
	Position        int    `json:"position"`
}

// weekEventEntry is one member's result in one event.
type weekEventEntry struct {
	EventID  int    `json:"event_id"`
	MemberID int    `json:"member_id"`
	Result   string `json:"result"`
	Note     string `json:"note"`
}

// weekEventIn is a member's line for an event in the week save; an empty result removes it.
type weekEventIn struct {
	EventID int    `json:"event_id"`
	Result  string `json:"result"`
	Note    string `json:"note"`
}

func loadWeekEvents(q rowQueryer, seasonID int) ([]weekEvent, error) {
	rows, err := q.Query(`SELECT id, week_number, label, kind, required_for_core, position FROM season_participation_events
		WHERE season_id = ? ORDER BY week_number, position, id`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []weekEvent{}
	for rows.Next() {
		var e weekEvent
		var req int
		if err := rows.Scan(&e.ID, &e.WeekNumber, &e.Label, &e.Kind, &req, &e.Position); err != nil {
			return nil, err
		}
		e.RequiredForCore = req == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

func loadWeekEventEntries(q rowQueryer, seasonID int) ([]weekEventEntry, error) {
	rows, err := q.Query(`SELECT x.event_id, x.member_id, x.result, x.note FROM season_participation_event_entries x
		JOIN season_participation_events e ON e.id = x.event_id WHERE e.season_id = ?`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []weekEventEntry{}
	for rows.Next() {
		var x weekEventEntry
		if err := rows.Scan(&x.EventID, &x.MemberID, &x.Result, &x.Note); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// writeWeekEventEntryTx stores or removes (empty result) a member's result in an event.
func writeWeekEventEntryTx(tx *sql.Tx, memberID int, in weekEventIn) error {
	if in.Result == "" {
		_, err := tx.Exec(`DELETE FROM season_participation_event_entries WHERE event_id = ? AND member_id = ?`, in.EventID, memberID)
		return err
	}
	_, err := tx.Exec(`INSERT INTO season_participation_event_entries (event_id, member_id, result, note) VALUES (?, ?, ?, ?)
		ON CONFLICT(event_id, member_id) DO UPDATE SET result = excluded.result, note = excluded.note`,
		in.EventID, memberID, in.Result, strings.TrimSpace(in.Note))
	return err
}

// weekEventBody is the create/update body.
type weekEventBody struct {
	SeasonID        int    `json:"season_id"`
	WeekNumber      int    `json:"week_number"`
	Label           string `json:"label"`
	Kind            string `json:"kind"`
	RequiredForCore bool   `json:"required_for_core"`
}

// POST /api/season-hub/participation-events
func handleWeekEventCreate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var b weekEventBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	s := landSeason(w, b.SeasonID)
	if s == nil {
		return
	}
	if ok, msg := seasonAcceptsEdits(s.ID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	b.Label, b.Kind = strings.TrimSpace(b.Label), strings.TrimSpace(b.Kind)
	if b.Label == "" {
		return400(w, "an event needs a label")
		return
	}
	if b.WeekNumber < 1 || b.WeekNumber > s.WeekCount {
		return400(w, fmt.Sprintf("week_number must be between 1 and %d", s.WeekCount))
		return
	}
	var pos int
	db.QueryRow(`SELECT COALESCE(MAX(position) + 1, 0) FROM season_participation_events WHERE season_id = ? AND week_number = ?`,
		s.ID, b.WeekNumber).Scan(&pos)
	res, err := db.Exec(`INSERT INTO season_participation_events (season_id, week_number, label, kind, required_for_core, position)
		VALUES (?, ?, ?, ?, ?, ?)`, s.ID, b.WeekNumber, b.Label, b.Kind, boolToInt(b.RequiredForCore), pos)
	if isUniqueViolation(err) {
		http.Error(w, "week "+strconv.Itoa(b.WeekNumber)+" already has an event called "+b.Label, http.StatusConflict)
		return
	}
	if err != nil {
		slog.Error("handleWeekEventCreate", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	logActivity(user.ID, user.Username, "created", "season_participation_event", fmt.Sprintf("%s, week %d, %s", b.Label, b.WeekNumber, s.Name), false)
	writeJSON(w, map[string]any{"id": id})
}

func weekEventFromPath(w http.ResponseWriter, r *http.Request) (int, int, string, bool) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return 0, 0, "", false
	}
	var seasonID int
	var label string
	if err := db.QueryRow(`SELECT season_id, label FROM season_participation_events WHERE id = ?`, id).Scan(&seasonID, &label); err == sql.ErrNoRows {
		http.Error(w, "Event not found", http.StatusNotFound)
		return 0, 0, "", false
	} else if err != nil {
		slog.Error("weekEventFromPath", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return 0, 0, "", false
	}
	if ok, msg := seasonAcceptsEdits(seasonID); !ok {
		seasonEditRefusal(w, msg)
		return 0, 0, "", false
	}
	return id, seasonID, label, true
}

// PUT /api/season-hub/participation-events/{id} {label, kind, required_for_core}
func handleWeekEventUpdate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, seasonID, _, ok := weekEventFromPath(w, r)
	if !ok {
		return
	}
	var b weekEventBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	b.Label, b.Kind = strings.TrimSpace(b.Label), strings.TrimSpace(b.Kind)
	if b.Label == "" {
		return400(w, "an event needs a label")
		return
	}
	_, err := db.Exec(`UPDATE season_participation_events SET label = ?, kind = ?, required_for_core = ? WHERE id = ?`,
		b.Label, b.Kind, boolToInt(b.RequiredForCore), id)
	if isUniqueViolation(err) {
		http.Error(w, "that week already has an event called "+b.Label, http.StatusConflict)
		return
	}
	if err != nil {
		slog.Error("handleWeekEventUpdate", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "updated", "season_participation_event", b.Label+", "+seasonTagName(&seasonID), false)
	writeJSON(w, map[string]string{"message": "Saved"})
}

// PUT /api/season-hub/participation-events/order {ids: [...]} — one week's events in order.
func handleWeekEventOrder(w http.ResponseWriter, r *http.Request) {
	var b struct {
		IDs []int `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || len(b.IDs) == 0 {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	var seasonID, week int
	if err := db.QueryRow(`SELECT season_id, week_number FROM season_participation_events WHERE id = ?`, b.IDs[0]).Scan(&seasonID, &week); err != nil {
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	}
	if ok, msg := seasonAcceptsEdits(seasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleWeekEventOrder: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	for pos, id := range b.IDs {
		if _, err := tx.Exec(`UPDATE season_participation_events SET position = ? WHERE id = ? AND season_id = ? AND week_number = ?`,
			pos, id, seasonID, week); err != nil {
			slog.Error("handleWeekEventOrder", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handleWeekEventOrder: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"message": "Saved"})
}

// DELETE /api/season-hub/participation-events/{id} — with its members' results.
func handleWeekEventDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, seasonID, label, ok := weekEventFromPath(w, r)
	if !ok {
		return
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleWeekEventDelete: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM season_participation_event_entries WHERE event_id = ?`, id)
	if err == nil {
		_, err = tx.Exec(`DELETE FROM season_participation_events WHERE id = ?`, id)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		slog.Error("handleWeekEventDelete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	logActivity(user.ID, user.Username, "deleted", "season_participation_event", label+", "+seasonTagName(&seasonID), false,
		fmt.Sprintf("%d member results removed", n))
	writeJSON(w, map[string]any{"message": "Deleted", "results_removed": n})
}
