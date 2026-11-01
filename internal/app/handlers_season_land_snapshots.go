// handlers_season_land_snapshots.go — Cpt Hedge snapshots on the Land tab (private-docs 276).
//
// A snapshot links a season to a map-planner export (a season-tagged .json in Files) and an
// optional screenshot, and to a route or none. The export is read on every view
// (readHedgeExport), so the card always says what the stored bytes hold; a malformed file is
// still kept, with its problems listed and no import offered. A proposed path imports as
// targets appended to a route — the planner's claims never become holdings or targets.

package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// landSnapshot is a snapshot as the Land tab shows it.
type landSnapshot struct {
	ID          int          `json:"id"`
	RouteID     *int         `json:"route_id"`
	RouteSlot   string       `json:"route_slot"`
	FileID      int          `json:"file_id"`
	FileTitle   string       `json:"file_title"`
	ImageFileID *int         `json:"image_file_id"`
	GridName    string       `json:"grid_name"`
	ExportDate  string       `json:"export_date"`
	Note        string       `json:"note"`
	UploadedBy  string       `json:"uploaded_by"`
	CreatedAt   string       `json:"created_at"`
	Summary     hedgeSummary `json:"summary"`
	fileName    string
}

// readSnapshotFile reads a stored export's bytes; a missing file reads as a problem.
func readSnapshotFile(fileName string) ([]byte, error) {
	if fileName == "" || strings.ContainsAny(fileName, `/\`) {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(filepath.Join(getStoragePath(), fileName))
}

func snapshotSummary(fileName string, seasonNumber int) hedgeSummary {
	b, err := readSnapshotFile(fileName)
	if err != nil {
		return hedgeSummary{Problems: []string{"the export file is missing from storage"}, GridNames: []string{}, Paths: []hedgePathView{}}
	}
	return readHedgeExport(b, seasonNumber)
}

func loadLandSnapshots(q rowQueryer, s *Season) ([]landSnapshot, error) {
	rows, err := q.Query(`SELECT n.id, n.route_id, COALESCE(r.slot, ''), n.file_id, COALESCE(f.title, ''), COALESCE(f.file_name, ''),
		n.image_file_id, n.grid_name, n.export_date, n.note, COALESCE(u.username, ''), strftime('%Y-%m-%d %H:%M', n.created_at)
		FROM season_land_snapshots n
		LEFT JOIN season_land_routes r ON r.id = n.route_id
		LEFT JOIN files f ON f.id = n.file_id
		LEFT JOIN users u ON u.id = n.uploaded_by
		WHERE n.season_id = ? ORDER BY n.id DESC`, s.ID)
	if err != nil {
		return nil, err
	}
	out := []landSnapshot{}
	for rows.Next() {
		var n landSnapshot
		if err := rows.Scan(&n.ID, &n.RouteID, &n.RouteSlot, &n.FileID, &n.FileTitle, &n.fileName, &n.ImageFileID,
			&n.GridName, &n.ExportDate, &n.Note, &n.UploadedBy, &n.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// The files are read after the cursor is closed.
	for i := range out {
		out[i].Summary = snapshotSummary(out[i].fileName, s.SeasonNumber)
	}
	return out, nil
}

// snapshotFile checks a file a snapshot names: it exists and is tagged to the season; want is
// the file_type it must have ("" for any).
func snapshotFile(id, seasonID int, want string) (fileName, title, msg string, err error) {
	var ftype string
	var sid sql.NullInt64
	err = db.QueryRow(`SELECT file_name, title, file_type, season_id FROM files WHERE id = ?`, id).Scan(&fileName, &title, &ftype, &sid)
	if err == sql.ErrNoRows {
		return "", "", fmt.Sprintf("file %d not found", id), nil
	}
	if err != nil {
		return "", "", "", err
	}
	if !sid.Valid || int(sid.Int64) != seasonID {
		return "", "", "the file is not tagged to this season", nil
	}
	if want != "" && ftype != want {
		return "", "", fmt.Sprintf("%s is not a %s file", title, want), nil
	}
	return fileName, title, "", nil
}

// landRouteForSlotTx finds a season's route in a slot, creating the (empty) route when the slot
// has none; the season's first route is its active one.
func landRouteForSlotTx(tx *sql.Tx, seasonID int, slot string) (int, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM season_land_routes WHERE season_id = ? AND slot = ?`, seasonID, slot).Scan(&id)
	if err == nil {
		return int(id), nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM season_land_routes WHERE season_id = ?`, seasonID).Scan(&n); err != nil {
		return 0, err
	}
	res, err := tx.Exec(`INSERT INTO season_land_routes (season_id, slot, name, is_active, updated_at) VALUES (?, ?, '', ?, CURRENT_TIMESTAMP)`,
		seasonID, slot, boolToInt(n == 0))
	if err != nil {
		return 0, err
	}
	id, _ = res.LastInsertId()
	return int(id), nil
}

func validLandSlot(slot string) bool {
	for _, s := range landRouteSlots {
		if s == slot {
			return true
		}
	}
	return false
}

// POST /api/season-hub/land/snapshots {season_id, file_id, image_file_id, slot, note}
func handleSeasonLandSnapshotCreate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var body struct {
		SeasonID    int    `json:"season_id"`
		FileID      int    `json:"file_id"`
		ImageFileID *int   `json:"image_file_id"`
		Slot        string `json:"slot"`
		Note        string `json:"note"`
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
	if body.Slot != "" && !validLandSlot(body.Slot) {
		http.Error(w, "slot must be primary, backup_a, backup_b or empty", http.StatusBadRequest)
		return
	}
	fileName, title, msg, err := snapshotFile(body.FileID, s.ID, "data")
	if err == nil && msg == "" && body.ImageFileID != nil {
		_, _, msg, err = snapshotFile(*body.ImageFileID, s.ID, "image")
	}
	if err != nil {
		slog.Error("handleSeasonLandSnapshotCreate: file", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	sum := snapshotSummary(fileName, s.SeasonNumber)
	grid := strings.Join(sum.GridNames, ", ")
	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleSeasonLandSnapshotCreate: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	var routeID any
	if body.Slot != "" {
		id, err := landRouteForSlotTx(tx, s.ID, body.Slot)
		if err != nil {
			slog.Error("handleSeasonLandSnapshotCreate: route", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		routeID = id
	}
	res, err := tx.Exec(`INSERT INTO season_land_snapshots (season_id, route_id, file_id, image_file_id, grid_name, export_date,
		note, uploaded_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, s.ID, routeID, body.FileID, body.ImageFileID, grid, sum.ExportDate,
		strings.TrimSpace(body.Note), user.ID)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		slog.Error("handleSeasonLandSnapshotCreate: insert", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	logActivity(user.ID, user.Username, "created", "season_land_snapshot", title+", "+seasonTagName(&s.ID), false)
	writeJSON(w, map[string]any{"id": id, "summary": sum})
}

// loadSnapshotForWrite reads a snapshot's season and file and refuses an archived season.
func loadSnapshotForWrite(w http.ResponseWriter, r *http.Request) (int, *Season, string, string, bool) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid snapshot ID", http.StatusBadRequest)
		return 0, nil, "", "", false
	}
	var seasonID int
	var fileName, title string
	err = db.QueryRow(`SELECT n.season_id, COALESCE(f.file_name, ''), COALESCE(f.title, '') FROM season_land_snapshots n
		LEFT JOIN files f ON f.id = n.file_id WHERE n.id = ?`, id).Scan(&seasonID, &fileName, &title)
	if err == sql.ErrNoRows {
		http.Error(w, "Snapshot not found", http.StatusNotFound)
		return 0, nil, "", "", false
	}
	if err != nil {
		slog.Error("loadSnapshotForWrite", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return 0, nil, "", "", false
	}
	s := landSeason(w, seasonID)
	if s == nil {
		return 0, nil, "", "", false
	}
	if ok, msg := seasonAcceptsEdits(seasonID); !ok {
		seasonEditRefusal(w, msg)
		return 0, nil, "", "", false
	}
	return id, s, fileName, title, true
}

// DELETE /api/season-hub/land/snapshots/{id} — unlinks the snapshot; its files stay in Files.
func handleSeasonLandSnapshotDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, s, _, title, ok := loadSnapshotForWrite(w, r)
	if !ok {
		return
	}
	if _, err := db.Exec(`DELETE FROM season_land_snapshots WHERE id = ?`, id); err != nil {
		slog.Error("handleSeasonLandSnapshotDelete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "deleted", "season_land_snapshot", title+", "+seasonTagName(&s.ID), false)
	writeJSON(w, map[string]string{"message": "Removed"})
}

// POST /api/season-hub/land/snapshots/{id}/import {grid_id, alliance_id, slot} — appends a
// proposed path's steps to a route as planned targets (no dates, connection unknown) and saves
// a revision. Steps the route already holds are appended again; the answer names them.
func handleSeasonLandSnapshotImport(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, s, fileName, title, ok := loadSnapshotForWrite(w, r)
	if !ok {
		return
	}
	var body struct {
		GridID     string `json:"grid_id"`
		AllianceID string `json:"alliance_id"`
		Slot       string `json:"slot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if !validLandSlot(body.Slot) {
		http.Error(w, "slot must be primary, backup_a or backup_b", http.StatusBadRequest)
		return
	}
	sum := snapshotSummary(fileName, s.SeasonNumber)
	var path *hedgePathView
	for i := range sum.Paths {
		if sum.Paths[i].GridID == body.GridID && sum.Paths[i].AllianceID == body.AllianceID {
			path = &sum.Paths[i]
		}
	}
	if path == nil {
		http.Error(w, "the export has no path for that grid and alliance", http.StatusBadRequest)
		return
	}
	if len(path.Problems) > 0 {
		http.Error(w, "the path can't be imported: "+strings.Join(path.Problems, "; "), http.StatusBadRequest)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleSeasonLandSnapshotImport: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	routeID, err := landRouteForSlotTx(tx, s.ID, body.Slot)
	if err != nil {
		slog.Error("handleSeasonLandSnapshotImport: route", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	held := map[string]bool{}
	var next int
	rows, err := tx.Query(`SELECT area_id, position FROM season_land_targets WHERE route_id = ?`, routeID)
	if err != nil {
		slog.Error("handleSeasonLandSnapshotImport: targets", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	for rows.Next() {
		var area string
		var pos int
		rows.Scan(&area, &pos)
		held[area] = true
		if pos+1 > next {
			next = pos + 1
		}
	}
	rows.Close()
	var already []string
	for i, st := range path.Steps {
		if held[st.AreaID] {
			already = append(already, st.Name)
		}
		if _, err := tx.Exec(`INSERT INTO season_land_targets (route_id, position, area_id, name, kind, level, status, connected, notes)
			VALUES (?, ?, ?, ?, ?, ?, 'planned', 'unknown', ?)`, routeID, next+i, st.AreaID, st.Name, st.Kind, st.Level,
			"From Cpt Hedge: "+title+", step "+strconv.Itoa(st.Step)); err != nil {
			slog.Error("handleSeasonLandSnapshotImport: insert", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if _, err := tx.Exec(`UPDATE season_land_routes SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, routeID); err != nil {
		slog.Error("handleSeasonLandSnapshotImport: touch", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := syncLandCalendarTx(tx, s, user.ID, time.Now()); err != nil {
		slog.Error("handleSeasonLandSnapshotImport: sync", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := writeRouteRevisionTx(tx, routeID, user.ID); err != nil {
		slog.Error("handleSeasonLandSnapshotImport: revision", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handleSeasonLandSnapshotImport: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "imported", "season_land_route", landSlotLabel(body.Slot)+" route, "+seasonTagName(&s.ID),
		false, fmt.Sprintf("%d targets from snapshot %d (%s) · via Cpt Hedge export", len(path.Steps), id, title))
	if already == nil {
		already = []string{}
	}
	writeJSON(w, map[string]any{"route_id": routeID, "added": len(path.Steps), "already_on_route": already})
}
