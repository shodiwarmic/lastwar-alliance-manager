// handlers_vs_power.go - Handlers for VS points and power tracking features

package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
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

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
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

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	n, _ := result.RowsAffected()
	logActivity(actorID, actorName, "deleted", "vs_points", weekDate, false, strconv.FormatInt(n, 10)+" records")

	w.WriteHeader(http.StatusNoContent)
}

// HTTP handler to process VS points screenshot via Cloud Run Worker
func processVSPointsScreenshot(w http.ResponseWriter, r *http.Request) {
	err := r.ParseMultipartForm(50 << 20)
	if err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["images"]
	if len(files) == 0 {
		files = r.MultipartForm.File["image"]
	}

	if len(files) == 0 {
		http.Error(w, "No image files provided", http.StatusBadRequest)
		return
	}

	var workerURL string
	err = db.QueryRow("SELECT COALESCE(cv_worker_url, '') FROM settings WHERE id = 1").Scan(&workerURL)
	if err != nil || workerURL == "" {
		http.Error(w, "CV Worker URL is not configured in the admin settings", http.StatusInternalServerError)
		return
	}

	// Ship directly to the Python Worker
	workerResults, err := ProcessImagesViaWorker(r.Context(), files, workerURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("Microservice processing failed: %v", err), http.StatusInternalServerError)
		return
	}

	validDayColumns := map[string]bool{
		"monday": true, "tuesday": true, "wednesday": true,
		"thursday": true, "friday": true, "saturday": true,
	}

	// We only care about the explicit day requested, or the first daily category returned
	detectedDay := strings.ToLower(r.FormValue("day"))
	var records []OCRPlayer

	if detectedDay != "" {
		if !validDayColumns[detectedDay] {
			http.Error(w, "Invalid day parameter", http.StatusBadRequest)
			return
		}
		records = workerResults[detectedDay]
	} else {
		// Attempt to auto-detect the day from what the worker returned
		for category, parsedRecords := range workerResults {
			if validDayColumns[category] {
				detectedDay = category
				records = parsedRecords
				break
			}
		}
	}

	if len(records) == 0 || detectedDay == "" {
		http.Error(w, "Could not determine which day these VS points are for. Please specify the day manually.", http.StatusBadRequest)
		return
	}

	weekParam := r.FormValue("week")
	if weekParam == "" {
		weekParam = "current"
	}
	now := time.Now()
	if weekParam == "last" {
		now = now.AddDate(0, 0, -7)
	}
	weekday := now.Weekday()
	daysFromMonday := int(weekday) - 1
	if weekday == time.Sunday {
		daysFromMonday = 6
	}
	monday := now.AddDate(0, 0, -daysFromMonday)
	weekDate := monday.Format("2006-01-02")

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	successCount := 0
	notFoundMembers := []string{}
	updatedMembers := []string{}

	for _, record := range records {
		var memberID int
		var memberName string

		// Candidate pre-pass: if the OCR worker flagged an ambiguous name/score merge,
		// try the alias engine on each split before falling through to fuzzy matching.
		if len(record.Candidates) > 0 {
			if _, rScore, rMember, _ := resolveOCRPlayer(tx, record, 0); rMember != nil {
				memberID = rMember.ID
				memberName = rMember.Name
				record.Score = rScore
			} else {
				// No alias match — use heuristic split for the lookup below.
				record.PlayerName = record.Candidates[0].PlayerName
				record.Score = record.Candidates[0].Score
			}
		}

		if memberID == 0 {
			err := tx.QueryRow("SELECT id, name FROM members WHERE LOWER(name) = LOWER(?)", record.PlayerName).Scan(&memberID, &memberName)

			if err == sql.ErrNoRows {
				rows, err := tx.Query("SELECT id, name FROM members")
				if err != nil {
					continue
				}

				bestMatch := ""
				bestScore := 0
				bestID := 0

				for rows.Next() {
					var id int
					var name string
					if err := rows.Scan(&id, &name); err != nil {
						continue
					}

					score := calculateSimilarity(record.PlayerName, name)
					if score > bestScore {
						bestScore = score
						bestMatch = name
						bestID = id
					}
				}
				rows.Close()

				if bestScore >= 70 {
					memberID = bestID
					memberName = bestMatch
					log.Printf("Fuzzy matched '%s' to '%s' (similarity: %d%%)", record.PlayerName, bestMatch, bestScore)
				} else {
					notFoundMembers = append(notFoundMembers, record.PlayerName)
					continue
				}
			}
		}

		var existingID int
		err = tx.QueryRow("SELECT id FROM vs_points WHERE member_id = ? AND week_date = ?",
			memberID, weekDate).Scan(&existingID)

		if err == sql.ErrNoRows {
			query := fmt.Sprintf(`
				INSERT INTO vs_points (member_id, week_date, %s, updated_at)
				VALUES (?, ?, ?, CURRENT_TIMESTAMP)`, detectedDay)
			_, err = tx.Exec(query, memberID, weekDate, record.Score)
		} else if err == nil {
			query := fmt.Sprintf(`
				UPDATE vs_points 
				SET %s = ?, updated_at = CURRENT_TIMESTAMP
				WHERE member_id = ? AND week_date = ?`, detectedDay)
			_, err = tx.Exec(query, record.Score, memberID, weekDate)
		}

		if err != nil {
			log.Printf("Failed to save VS points for %s: %v", memberName, err)
			continue
		}

		successCount++
		updatedMembers = append(updatedMembers, memberName)
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to save changes", http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "imported", "vs_points", weekDate+" ("+detectedDay+")", false,
		strconv.Itoa(successCount)+" members updated via screenshot")

	response := map[string]interface{}{
		"message":         fmt.Sprintf("Successfully updated VS points for %d members on %s", successCount, detectedDay),
		"day":             detectedDay,
		"week_date":       weekDate,
		"success_count":   successCount,
		"updated_members": updatedMembers,
	}

	if len(notFoundMembers) > 0 {
		response["not_found_members"] = notFoundMembers
		response["warning"] = fmt.Sprintf("%d members could not be matched to the database", len(notFoundMembers))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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

// Process screenshot data with OCR support via Cloud Run Worker
func processPowerScreenshot(w http.ResponseWriter, r *http.Request) {
	var powerTrackingEnabled bool
	err := db.QueryRow("SELECT COALESCE(power_tracking_enabled, 0) FROM settings WHERE id = 1").Scan(&powerTrackingEnabled)
	if err != nil || !powerTrackingEnabled {
		http.Error(w, "Power tracking is not enabled", http.StatusForbidden)
		return
	}

	err = r.ParseMultipartForm(50 << 20)
	if err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["images"]
	if len(files) == 0 {
		files = r.MultipartForm.File["image"]
	}

	if len(files) == 0 {
		http.Error(w, "No image files provided", http.StatusBadRequest)
		return
	}

	var workerURL string
	err = db.QueryRow("SELECT COALESCE(cv_worker_url, '') FROM settings WHERE id = 1").Scan(&workerURL)
	if err != nil || workerURL == "" {
		http.Error(w, "CV Worker URL is not configured in the Admin Settings", http.StatusInternalServerError)
		return
	}

	workerResults, err := ProcessImagesViaWorker(r.Context(), files, workerURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("Microservice processing failed: %v", err), http.StatusInternalServerError)
		return
	}

	records := workerResults["power"]
	if len(records) == 0 {
		http.Error(w, "No valid power records found in the uploaded images", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	successCount := 0
	failedCount := 0
	errors := []string{}

	allMembers := []struct {
		ID   int
		Name string
	}{}
	rows, err := tx.Query("SELECT id, name FROM members")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var m struct {
				ID   int
				Name string
			}
			if rows.Scan(&m.ID, &m.Name) == nil {
				allMembers = append(allMembers, m)
			}
		}
	}

	for _, record := range records {
		var memberID int

		// Candidate pre-pass: if the OCR worker flagged an ambiguous name/score merge,
		// try the alias engine on each split before falling through to fuzzy matching.
		if len(record.Candidates) > 0 {
			if _, rScore, rMember, _ := resolveOCRPlayer(tx, record, 0); rMember != nil {
				memberID = rMember.ID
				record.Score = rScore
			} else {
				record.PlayerName = record.Candidates[0].PlayerName
				record.Score = record.Candidates[0].Score
			}
		}

		if memberID == 0 {
			err := tx.QueryRow("SELECT id FROM members WHERE name = ?", record.PlayerName).Scan(&memberID)
			if err != nil {
				err = tx.QueryRow("SELECT id FROM members WHERE LOWER(name) = LOWER(?)", record.PlayerName).Scan(&memberID)
			}

			if err != nil {
				bestMatch := ""
				bestMatchID := 0
				bestScore := 0

				for _, member := range allMembers {
					score := calculateSimilarity(record.PlayerName, member.Name)
					if score > bestScore {
						bestScore = score
						bestMatch = member.Name
						bestMatchID = member.ID
					}
				}

				if bestMatchID > 0 && bestScore >= 50 {
					memberID = bestMatchID
				} else {
					failedCount++
					if bestMatch != "" {
						errors = append(errors, fmt.Sprintf("Member '%s' not found (closest: '%s' at %d%%, need 50%%+)", record.PlayerName, bestMatch, bestScore))
					} else {
						errors = append(errors, fmt.Sprintf("Member '%s' not found (no members in database)", record.PlayerName))
					}
					continue
				}
			}
		}

		_, err = tx.Exec("INSERT INTO power_history (member_id, power) VALUES (?, ?)",
			memberID, record.Score)
		if err != nil {
			failedCount++
			errors = append(errors, fmt.Sprintf("Failed to add record for '%s': %v", record.PlayerName, err))
			continue
		}

		successCount++
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to save records", http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "imported", "power_records", "power screenshot", false,
		strconv.Itoa(successCount)+" records saved, "+strconv.Itoa(failedCount)+" failed")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":       fmt.Sprintf("Processed %d records successfully, %d failed", successCount, failedCount),
		"success_count": successCount,
		"failed_count":  failedCount,
		"errors":        errors,
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

	var workerURL string
	err = db.QueryRow("SELECT COALESCE(cv_worker_url, '') FROM settings WHERE id = 1").Scan(&workerURL)
	if err != nil || workerURL == "" {
		http.Error(w, "CV Worker URL is not configured in the Admin Settings", http.StatusInternalServerError)
		return
	}

	// Ship the entire batch to the Python Worker and let it do the heavily lifting
	workerResults, err := ProcessImagesViaWorker(r.Context(), files, workerURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("Microservice processing failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Calculate VS week date
	now := time.Now()
	if weekParam == "last" {
		now = now.AddDate(0, 0, -7)
	}
	weekday := now.Weekday()
	daysFromMonday := int(weekday) - 1
	if weekday == time.Sunday {
		daysFromMonday = 6
	}
	monday := now.AddDate(0, 0, -daysFromMonday)
	weekDate := monday.Format("2006-01-02")

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	session, _ := store.Get(r, "session")
	currentUserID, _ := session.Values["user_id"].(int)

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
		if activeCategory != "power" {
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

			if activeCategory == "power" {
				row.UpdatedFields["power"] = int(resolvedScore)
			} else {
				row.UpdatedFields[activeCategory] = int(resolvedScore)
			}
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
	})
}
