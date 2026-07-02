// handlers_vs_power.go - Handlers for VS points and power tracking features

package main

import (
	"context"
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

// Get VS points for a week or all weeks
func getVSPoints(w http.ResponseWriter, r *http.Request) {
	weekDate := r.URL.Query().Get("week")

	var query string
	var rows *sql.Rows
	var err error

	if weekDate != "" {
		query = `
			SELECT v.id, v.member_id, v.week_date, v.monday, v.tuesday, v.wednesday, 
				   v.thursday, v.friday, v.saturday, v.created_at, v.updated_at,
				   m.name, m.rank
			FROM vs_points v
			JOIN members m ON v.member_id = m.id
			WHERE v.week_date = ?
			ORDER BY m.name
		`
		rows, err = db.Query(query, weekDate)
	} else {
		query = `
			SELECT v.id, v.member_id, v.week_date, v.monday, v.tuesday, v.wednesday, 
				   v.thursday, v.friday, v.saturday, v.created_at, v.updated_at,
				   m.name, m.rank
			FROM vs_points v
			JOIN members m ON v.member_id = m.id
			ORDER BY v.week_date DESC, m.name
		`
		rows, err = db.Query(query)
	}

	if err != nil {
		http.Error(w, "Failed to fetch VS points", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	vsPoints := []VSPointsWithMember{}
	for rows.Next() {
		var v VSPointsWithMember
		if err := rows.Scan(&v.ID, &v.MemberID, &v.WeekDate, &v.Monday, &v.Tuesday,
			&v.Wednesday, &v.Thursday, &v.Friday, &v.Saturday, &v.CreatedAt, &v.UpdatedAt,
			&v.MemberName, &v.MemberRank); err != nil {
			http.Error(w, "Failed to read VS points", http.StatusInternalServerError)
			return
		}
		vsPoints = append(vsPoints, v)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(vsPoints)
}

// Save VS points for a week (bulk operation)
func saveVSPoints(w http.ResponseWriter, r *http.Request) {
	var data struct {
		WeekDate string `json:"week_date"`
		Points   []struct {
			MemberID  int `json:"member_id"`
			Monday    int `json:"monday"`
			Tuesday   int `json:"tuesday"`
			Wednesday int `json:"wednesday"`
			Thursday  int `json:"thursday"`
			Friday    int `json:"friday"`
			Saturday  int `json:"saturday"`
		} `json:"points"`
	}

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Snap to the game-time VS-week Monday so the browser clock can't misfile the week.
	normWeek, err := normalizeToGameWeekMonday(data.WeekDate)
	if err != nil {
		http.Error(w, "Invalid week_date: must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	data.WeekDate = normWeek

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	for _, point := range data.Points {
		var existingID int
		err = tx.QueryRow("SELECT id FROM vs_points WHERE member_id = ? AND week_date = ?",
			point.MemberID, data.WeekDate).Scan(&existingID)

		if err == sql.ErrNoRows {
			_, err = tx.Exec(`
				INSERT INTO vs_points (member_id, week_date, monday, tuesday, wednesday, thursday, friday, saturday, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
				point.MemberID, data.WeekDate, point.Monday, point.Tuesday, point.Wednesday,
				point.Thursday, point.Friday, point.Saturday)
		} else if err == nil {
			_, err = tx.Exec(`
				UPDATE vs_points 
				SET monday = ?, tuesday = ?, wednesday = ?, thursday = ?, friday = ?, saturday = ?, updated_at = CURRENT_TIMESTAMP
				WHERE member_id = ? AND week_date = ?`,
				point.Monday, point.Tuesday, point.Wednesday, point.Thursday, point.Friday, point.Saturday,
				point.MemberID, data.WeekDate)
		}

		if err != nil {
			tx.Rollback()
			http.Error(w, "Failed to save VS points", http.StatusInternalServerError)
			return
		}
	}

	err = tx.Commit()
	if err != nil {
		http.Error(w, "Failed to save changes", http.StatusInternalServerError)
		return
	}

	actor := getAuthUser(r)
	actorID, actorName := actor.ID, actor.Username
	logActivity(actorID, actorName, "updated", "vs_points", data.WeekDate, false, strconv.Itoa(len(data.Points))+" members")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "VS points saved successfully"})
}

// Delete VS points for a specific week
func deleteWeekVSPoints(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	weekDate := vars["week"]

	result, err := db.Exec("DELETE FROM vs_points WHERE week_date = ?", weekDate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	actor := getAuthUser(r)
	actorID, actorName := actor.ID, actor.Username
	n, _ := result.RowsAffected()
	logActivity(actorID, actorName, "deleted", "vs_points", weekDate, false, strconv.FormatInt(n, 10)+" records")

	w.WriteHeader(http.StatusNoContent)
}

// Get power history for a specific member or all members
func getPowerHistory(w http.ResponseWriter, r *http.Request) {
	memberID := r.URL.Query().Get("member_id")
	limit := r.URL.Query().Get("limit")

	if limit == "" {
		limit = "30"
	}

	var rows *sql.Rows
	var err error

	if memberID != "" {
		rows, err = db.Query(`
			SELECT ph.id, ph.member_id, ph.power, ph.recorded_at
			FROM power_history ph
			WHERE ph.member_id = ?
			ORDER BY ph.recorded_at DESC
			LIMIT ?
		`, memberID, limit)
	} else {
		rows, err = db.Query(`
			SELECT ph.id, ph.member_id, ph.power, ph.recorded_at
			FROM power_history ph
			ORDER BY ph.recorded_at DESC
			LIMIT ?
		`, limit)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	history := []PowerHistory{}
	for rows.Next() {
		var ph PowerHistory
		if err := rows.Scan(&ph.ID, &ph.MemberID, &ph.Power, &ph.RecordedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		history = append(history, ph)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

// Add power record manually
func addPowerRecord(w http.ResponseWriter, r *http.Request) {
	var request struct {
		MemberID int   `json:"member_id"`
		Power    int64 `json:"power"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var exists int
	err := db.QueryRow("SELECT COUNT(*) FROM members WHERE id = ?", request.MemberID).Scan(&exists)
	if err != nil || exists == 0 {
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}

	result, err := db.Exec("INSERT INTO power_history (member_id, power) VALUES (?, ?)",
		request.MemberID, request.Power)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Power record added successfully",
		"id":      id,
	})
}

func getHeroPowerHistory(w http.ResponseWriter, r *http.Request) {
	memberID := r.URL.Query().Get("member_id")
	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "30"
	}

	var rows *sql.Rows
	var err error

	if memberID != "" {
		rows, err = db.Query(`
			SELECT id, member_id, power, recorded_at
			FROM hero_power_history
			WHERE member_id = ?
			ORDER BY recorded_at DESC
			LIMIT ?
		`, memberID, limit)
	} else {
		rows, err = db.Query(`
			SELECT id, member_id, power, recorded_at
			FROM hero_power_history
			ORDER BY recorded_at DESC
			LIMIT ?
		`, limit)
	}

	if err != nil {
		slog.Error("Failed to query hero power history", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	history := []HeroPowerHistory{}
	for rows.Next() {
		var h HeroPowerHistory
		if err := rows.Scan(&h.ID, &h.MemberID, &h.Power, &h.RecordedAt); err != nil {
			slog.Error("Failed to scan hero power history row", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		history = append(history, h)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

func addHeroPowerRecord(w http.ResponseWriter, r *http.Request) {
	var request struct {
		MemberID int   `json:"member_id"`
		Power    int64 `json:"power"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var exists int
	err := db.QueryRow("SELECT COUNT(*) FROM members WHERE id = ?", request.MemberID).Scan(&exists)
	if err != nil || exists == 0 {
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}

	result, err := db.Exec("INSERT INTO hero_power_history (member_id, power) VALUES (?, ?)",
		request.MemberID, request.Power)
	if err != nil {
		slog.Error("Failed to insert hero power record", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()

	actor := getAuthUser(r)
	logActivity(actor.ID, actor.Username, "updated", "power_records", "total hero power record", false)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Hero power record added successfully",
		"id":      id,
	})
}

func getKillHistory(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT
			m.id, m.name, m.rank,
			COALESCE((SELECT kills FROM kill_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), 0) as current_kills,
			COALESCE((SELECT kills FROM kill_history WHERE member_id = m.id AND recorded_at <= datetime('now', '-7 days') ORDER BY recorded_at DESC LIMIT 1), 0) as kills_7d,
			COALESCE((SELECT kills FROM kill_history WHERE member_id = m.id AND recorded_at <= datetime('now', '-30 days') ORDER BY recorded_at DESC LIMIT 1), 0) as kills_30d,
			COALESCE((SELECT recorded_at FROM kill_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), '') as last_recorded_at
		FROM members m
		WHERE m.rank != 'EX'
		  AND EXISTS (SELECT 1 FROM kill_history WHERE member_id = m.id)
		ORDER BY current_kills DESC
	`)
	if err != nil {
		slog.Error("Failed to query kill history", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []KillCount
	for rows.Next() {
		var k KillCount
		var kills7d, kills30d int64
		if err := rows.Scan(&k.MemberID, &k.MemberName, &k.MemberRank, &k.CurrentKills, &kills7d, &kills30d, &k.LastRecordedAt); err != nil {
			slog.Error("Failed to scan kill history row", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if kills7d > 0 {
			delta := k.CurrentKills - kills7d
			k.KillsDelta7d = &delta
		}
		if kills30d > 0 {
			delta := k.CurrentKills - kills30d
			k.KillsDelta30d = &delta
		}
		result = append(result, k)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func postKillHistory(w http.ResponseWriter, r *http.Request) {
	var request struct {
		MemberID int   `json:"member_id"`
		Kills    int64 `json:"kills"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var memberRank string
	err := db.QueryRow("SELECT rank FROM members WHERE id = ?", request.MemberID).Scan(&memberRank)
	if err == sql.ErrNoRows {
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("Failed to look up member for kill history", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if memberRank == "EX" {
		http.Error(w, "Cannot record kills for archived members", http.StatusBadRequest)
		return
	}

	result, err := db.Exec("INSERT INTO kill_history (member_id, kills) VALUES (?, ?)", request.MemberID, request.Kills)
	if err != nil {
		slog.Error("Failed to insert kill history record", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()

	actor := getAuthUser(r)
	logActivity(actor.ID, actor.Username, "updated", "kill_count", "troop kill record", false)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Kill count record added successfully",
		"id":      id,
	})
}

// Process images with automatic bucketing via Cloud Run Worker
func processSmartScreenshot(w http.ResponseWriter, r *http.Request) {
	err := r.ParseMultipartForm(50 << 20)
	if err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	weekParam := r.FormValue("week")
	if weekParam == "" {
		weekParam = "current"
	}

	files := r.MultipartForm.File["images"]
	if len(files) == 0 {
		files = r.MultipartForm.File["image"] // Legacy fallback
	}

	if len(files) == 0 {
		http.Error(w, "No image files provided", http.StatusBadRequest)
		return
	}

	forceCategory := r.FormValue("force_category")

	// Dispatch through the configured backend (cloud Vision worker OR the local
	// PaddleOCR sidecar) instead of calling the cloud worker directly, so the
	// upload page works in local mode too. In local mode the selected category is
	// required and forwarded as the classification override; in cloud mode the
	// worker ignores it (auto-detect) and it's only applied as the force-category
	// override in the loop below. "auto"/"total" are cloud-only UI sentinels, not
	// real categories, so they map to "" (no override).
	ocrCategory := forceCategory
	if ocrCategory == "auto" || ocrCategory == "total" {
		ocrCategory = ""
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	workerResults, ocrDiag, err := ProcessImages(ctx, files, ocrCategory)
	if err != nil {
		http.Error(w, fmt.Sprintf("Microservice processing failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Calculate VS week date in game time (UTC-2), so the stored week_date matches
	// the game-time Monday the dashboard/accountability evaluate against.
	weeksBack := 0
	if weekParam == "last" {
		weeksBack = 1
	}
	weekDate := gameWeekStart(time.Now(), weeksBack)

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	currentUserID := getAuthUser(r).ID

	payloadMap := make(map[string]*VSImportRow)
	var processedSummaries []string
	seenSummaries := make(map[string]bool)

	// Iterate through the structured JSON the worker handed back
	for category, records := range workerResults {
		// If the user explicitly forced a category from the frontend, override the worker's logic
		activeCategory := category
		if forceCategory != "" && forceCategory != "auto" {
			activeCategory = forceCategory
		}

		if activeCategory == "unknown" {
			continue // Skip unreadable images unless forced
		}

		summary := "Power Rankings"
		if activeCategory == "kills" {
			summary = "Troop Kills"
		} else if activeCategory != "power" {
			summary = fmt.Sprintf("VS Points (%s)", strings.Title(activeCategory))
		}

		if !seenSummaries[summary] {
			processedSummaries = append(processedSummaries, summary)
			seenSummaries[summary] = true
		}

		for _, record := range records {
			_, resolvedScore, member, matchType := resolveOCRPlayer(tx, record, currentUserID)

			row, exists := payloadMap[record.PlayerName]
			if !exists {
				row = &VSImportRow{
					OriginalName:  record.PlayerName,
					UpdatedFields: make(map[string]int),
				}
				if member != nil {
					row.MatchedMember = member
					row.MatchType = matchType
				} else {
					row.MatchType = "unresolved"
					if len(record.Candidates) > 0 {
						row.Candidates = record.Candidates
					}
				}
				payloadMap[record.PlayerName] = row
			}

			row.UpdatedFields[activeCategory] = int(resolvedScore)
		}
	}

	response := VSImportPreviewResponse{}
	for _, row := range payloadMap {
		if row.MatchType == "unresolved" {
			response.Unresolved = append(response.Unresolved, *row)
		} else {
			response.Matched = append(response.Matched, *row)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":          fmt.Sprintf("OCR Processed successfully. Found %d matched and %d unresolved records.", len(response.Matched), len(response.Unresolved)),
		"processed_groups": processedSummaries,
		"matched":          response.Matched,
		"unresolved":       response.Unresolved,
		"week_date":        weekDate,
		// Compact OCR diagnostics summary; the client echoes it back on commit so
		// commitCSVImport can enrich the activity-log entry (empty on legacy OCR).
		"ocr_summary": summarizeOCRDiagnostics(ocrDiag),
	})
}
