// handlers_vs_power.go - Handlers for VS points and power tracking features

package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	vsPoints := []VSPointsWithMember{}
	for rows.Next() {
		var v VSPointsWithMember
		if err := rows.Scan(&v.ID, &v.MemberID, &v.WeekDate, &v.Monday, &v.Tuesday,
			&v.Wednesday, &v.Thursday, &v.Friday, &v.Saturday, &v.CreatedAt, &v.UpdatedAt,
			&v.MemberName, &v.MemberRank); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Begin transaction
	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Upsert VS points for each member
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

	// Commit transaction
	err = tx.Commit()
	if err != nil {
		http.Error(w, "Failed to save changes", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "VS points saved successfully"})
}

// Delete VS points for a specific week
func deleteWeekVSPoints(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	weekDate := vars["week"]

	_, err := db.Exec("DELETE FROM vs_points WHERE week_date = ?", weekDate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HTTP handler to process VS points screenshot
func processVSPointsScreenshot(w http.ResponseWriter, r *http.Request) {
	var records []struct {
		MemberName string `json:"member_name"`
		Points     int64  `json:"points"`
	}
	var detectedDay string
	var weekDate string

	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		err := r.ParseMultipartForm(50 << 20) // 50 MB max for multi-file uploads
		if err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		weekParam := r.FormValue("week")
		if weekParam == "" {
			weekParam = "current"
		}

		// Support both "images" (new multi-file) and "image" (legacy)
		files := r.MultipartForm.File["images"]
		if len(files) == 0 {
			files = r.MultipartForm.File["image"]
		}

		if len(files) == 0 {
			http.Error(w, "No image files provided", http.StatusBadRequest)
			return
		}

		var imageDatas [][]byte
		for _, fileHeader := range files {
			file, err := fileHeader.Open()
			if err != nil {
				http.Error(w, "Failed to open uploaded file", http.StatusInternalServerError)
				return
			}

			data, err := io.ReadAll(file)
			file.Close() // Close immediately after reading

			if err != nil {
				http.Error(w, "Failed to read image", http.StatusInternalServerError)
				return
			}
			imageDatas = append(imageDatas, data)
		}

		detectedDay, records, err = extractVSPointsDataFromImages(imageDatas)
		if err != nil {
			http.Error(w, fmt.Sprintf("OCR processing failed: %v", err), http.StatusInternalServerError)
			return
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
		weekDate = monday.Format("2006-01-02")
	} else {
		var request struct {
			Records []struct {
				MemberName string `json:"member_name"`
				Points     int64  `json:"points"`
			} `json:"records"`
			Text string `json:"text"`
			Day  string `json:"day"`
			Week string `json:"week"`
		}

		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if request.Text != "" {
			records = parseVSPointsText(request.Text)
			detectedDay = detectSelectedDay(request.Text)
		} else {
			records = request.Records
		}

		if request.Day != "" {
			detectedDay = strings.ToLower(request.Day)
		}

		weekParam := request.Week
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
		weekDate = monday.Format("2006-01-02")
	}

	if len(records) == 0 {
		http.Error(w, "No valid VS point records found", http.StatusBadRequest)
		return
	}

	if detectedDay == "" {
		http.Error(w, "Could not determine which day these VS points are for. Please specify the day manually.", http.StatusBadRequest)
		return
	}

	dayColumn := detectedDay
	validDays := []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	isValidDay := false
	for _, d := range validDays {
		if dayColumn == d {
			isValidDay = true
			break
		}
	}
	if !isValidDay {
		http.Error(w, fmt.Sprintf("Invalid day: %s. Must be monday-saturday", dayColumn), http.StatusBadRequest)
		return
	}

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
		err := tx.QueryRow("SELECT id, name FROM members WHERE LOWER(name) = LOWER(?)", record.MemberName).Scan(&memberID, &memberName)

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

				score := calculateSimilarity(record.MemberName, name)
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
				log.Printf("Fuzzy matched '%s' to '%s' (similarity: %d%%)", record.MemberName, bestMatch, bestScore)
			} else {
				notFoundMembers = append(notFoundMembers, record.MemberName)
				continue
			}
		}

		var existingID int
		err = tx.QueryRow("SELECT id FROM vs_points WHERE member_id = ? AND week_date = ?",
			memberID, weekDate).Scan(&existingID)

		if err == sql.ErrNoRows {
			query := fmt.Sprintf(`
				INSERT INTO vs_points (member_id, week_date, %s, updated_at)
				VALUES (?, ?, ?, CURRENT_TIMESTAMP)`, dayColumn)
			_, err = tx.Exec(query, memberID, weekDate, record.Points)
		} else if err == nil {
			query := fmt.Sprintf(`
				UPDATE vs_points 
				SET %s = ?, updated_at = CURRENT_TIMESTAMP
				WHERE member_id = ? AND week_date = ?`, dayColumn)
			_, err = tx.Exec(query, record.Points, memberID, weekDate)
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

// Process screenshot data with OCR support
func processPowerScreenshot(w http.ResponseWriter, r *http.Request) {
	var powerTrackingEnabled bool
	err := db.QueryRow("SELECT COALESCE(power_tracking_enabled, 0) FROM settings WHERE id = 1").Scan(&powerTrackingEnabled)
	if err != nil || !powerTrackingEnabled {
		http.Error(w, "Power tracking is not enabled", http.StatusForbidden)
		return
	}

	var records []struct {
		MemberName string `json:"member_name"`
		Power      int64  `json:"power"`
	}

	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		err := r.ParseMultipartForm(50 << 20) // 50 MB max for multi-file uploads
		if err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		// Support both "images" (new multi-file) and "image" (legacy)
		files := r.MultipartForm.File["images"]
		if len(files) == 0 {
			files = r.MultipartForm.File["image"]
		}

		if len(files) == 0 {
			http.Error(w, "No image files provided", http.StatusBadRequest)
			return
		}

		var imageDatas [][]byte
		for _, fileHeader := range files {
			file, err := fileHeader.Open()
			if err != nil {
				http.Error(w, "Failed to open uploaded file", http.StatusInternalServerError)
				return
			}

			data, err := io.ReadAll(file)
			file.Close() // Close immediately after reading

			if err != nil {
				http.Error(w, "Failed to read image", http.StatusInternalServerError)
				return
			}
			imageDatas = append(imageDatas, data)
		}

		records, err = extractPowerDataFromImages(imageDatas)
		if err != nil {
			http.Error(w, fmt.Sprintf("OCR processing failed: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		var request struct {
			Records []struct {
				MemberName string `json:"member_name"`
				Power      int64  `json:"power"`
			} `json:"records"`
			Text string `json:"text"`
		}

		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if request.Text != "" {
			records = parsePowerRankingsText(request.Text)
		} else {
			records = request.Records
		}
	}

	if len(records) == 0 {
		http.Error(w, "No valid records found", http.StatusBadRequest)
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
		err := tx.QueryRow("SELECT id FROM members WHERE name = ?", record.MemberName).Scan(&memberID)

		if err != nil {
			err = tx.QueryRow("SELECT id FROM members WHERE LOWER(name) = LOWER(?)", record.MemberName).Scan(&memberID)
		}

		if err != nil {
			bestMatch := ""
			bestMatchID := 0
			bestScore := 0

			for _, member := range allMembers {
				score := calculateSimilarity(record.MemberName, member.Name)
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
					errors = append(errors, fmt.Sprintf("Member '%s' not found (closest: '%s' at %d%%, need 50%%+)", record.MemberName, bestMatch, bestScore))
				} else {
					errors = append(errors, fmt.Sprintf("Member '%s' not found (no members in database)", record.MemberName))
				}
				continue
			}
		}

		_, err = tx.Exec("INSERT INTO power_history (member_id, power) VALUES (?, ?)",
			memberID, record.Power)
		if err != nil {
			failedCount++
			errors = append(errors, fmt.Sprintf("Failed to add record for '%s': %v", record.MemberName, err))
			continue
		}

		successCount++
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to save records", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":       fmt.Sprintf("Processed %d records successfully, %d failed", successCount, failedCount),
		"success_count": successCount,
		"failed_count":  failedCount,
		"errors":        errors,
	})
}

// Process images with automatic bucketing and detection
func processSmartScreenshot(w http.ResponseWriter, r *http.Request) {
	err := r.ParseMultipartForm(50 << 20) // 50 MB max
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

	// 1. The Sorting Hat: Group images into buckets based on visual tabs
	buckets := make(map[string][][]byte)
	for _, fileHeader := range files {
		file, err := fileHeader.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			continue
		}

		day := detectDayFromTabRegion(data)
		if day == "" {
			day = "unknown"
		}
		buckets[day] = append(buckets[day], data)
	}

	// 3. Process Each Bucket Independently (Dynamic GCP Limits)
	type ExtractedGroup struct {
		DataType string
		Day      string
		Records  []SmartRecord
	}

	var processedGroups []ExtractedGroup
	failedCount := 0
	errors := []string{}

	// GCP Vision Sweet Spot: Keep images under 12k pixels tall so it doesn't compress the text
	const maxHeightPerChunk = 8000
	const maxImagesPerChunk = 4

	for bucketName, bucketData := range buckets {
		log.Printf("Processing bucket: %s with %d images", bucketName, len(bucketData))

		var currentChunk [][]byte
		currentHeight := 0
		chunkIndex := 1

		// Helper to process a chunk
		processChunk := func(chunk [][]byte) {
			log.Printf("Stitching sub-chunk %d for bucket %s (%d images, ~%dpx tall)", chunkIndex, bucketName, len(chunk), currentHeight)
			dataType, detectedDay, records, err := extractSmartDataFromImages(chunk, bucketName)
			if err != nil {
				failedCount += len(chunk)
				errors = append(errors, fmt.Sprintf("Bucket '%s' (chunk %d) failed: %v", bucketName, chunkIndex, err))
			} else {
				processedGroups = append(processedGroups, ExtractedGroup{
					DataType: dataType,
					Day:      detectedDay,
					Records:  records,
				})
			}
			chunkIndex++
		}

		for _, imgBytes := range bucketData {
			// Instantly read image dimensions without fully decoding it into memory
			config, _, err := image.DecodeConfig(bytes.NewReader(imgBytes))
			if err != nil {
				continue // Skip corrupt images
			}

			// If adding this image exceeds our dynamic height limit OR hard file limit
			if (currentHeight+config.Height > maxHeightPerChunk || len(currentChunk) >= maxImagesPerChunk) && len(currentChunk) > 0 {
				processChunk(currentChunk)
				// Reset the chunk with the current image that pushed us over the limit
				currentChunk = [][]byte{imgBytes}
				currentHeight = config.Height
			} else {
				currentChunk = append(currentChunk, imgBytes)
				currentHeight += config.Height
			}
		}

		// Process whatever is left over in the final chunk
		if len(currentChunk) > 0 {
			processChunk(currentChunk)
		}
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

	// 3. Setup the Database Transaction for saving
	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Pre-fetch members for fuzzy matching
	allMembers := []struct {
		ID   int
		Name string
	}{}
	rows, err := tx.Query("SELECT id, name FROM members")
	if err == nil {
		for rows.Next() {
			var m struct {
				ID   int
				Name string
			}
			if rows.Scan(&m.ID, &m.Name) == nil {
				allMembers = append(allMembers, m)
			}
		}
		rows.Close()
	}

	// 4. Save Extracted Data
	successCount := 0
	notFoundMembers := []string{}
	var processedSummaries []string
	seenSummaries := make(map[string]bool)

	for _, group := range processedGroups {
		// Track what we successfully parsed
		summary := "Power Rankings"
		if group.DataType == "vs_points" {
			summary = fmt.Sprintf("VS Points (%s)", strings.Title(group.Day))
		}
		if !seenSummaries[summary] {
			processedSummaries = append(processedSummaries, summary)
			seenSummaries[summary] = true
		}

		for _, record := range group.Records {
			var memberID int
			err := tx.QueryRow("SELECT id FROM members WHERE LOWER(name) = LOWER(?)", record.MemberName).Scan(&memberID)

			if err != nil {
				bestMatch := ""
				bestMatchID := 0
				bestScore := 0

				for _, member := range allMembers {
					score := calculateSimilarity(record.MemberName, member.Name)
					if score > bestScore {
						bestScore = score
						bestMatch = member.Name
						bestMatchID = member.ID
					}
				}

				requiredScore := 50
				if group.DataType == "vs_points" {
					requiredScore = 70
				}

				if bestMatchID > 0 && bestScore >= requiredScore {
					memberID = bestMatchID
				} else {
					failedCount++
					notFoundMembers = append(notFoundMembers, record.MemberName)
					if bestMatch != "" {
						errors = append(errors, fmt.Sprintf("Member '%s' not found (closest: '%s' at %d%%, need %d%%+)", record.MemberName, bestMatch, bestScore, requiredScore))
					} else {
						errors = append(errors, fmt.Sprintf("Member '%s' not found in database", record.MemberName))
					}
					continue
				}
			}

			// Execute SQL
			if group.DataType == "vs_points" {
				var existingID int
				err = tx.QueryRow("SELECT id FROM vs_points WHERE member_id = ? AND week_date = ?", memberID, weekDate).Scan(&existingID)

				if err == sql.ErrNoRows {
					query := fmt.Sprintf(`INSERT INTO vs_points (member_id, week_date, %s, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)`, group.Day)
					_, err = tx.Exec(query, memberID, weekDate, record.Value)
				} else if err == nil {
					query := fmt.Sprintf(`UPDATE vs_points SET %s = ?, updated_at = CURRENT_TIMESTAMP WHERE member_id = ? AND week_date = ?`, group.Day)
					_, err = tx.Exec(query, record.Value, memberID, weekDate)
				}
			} else {
				_, err = tx.Exec("INSERT INTO power_history (member_id, power) VALUES (?, ?)", memberID, record.Value)
			}

			if err != nil {
				failedCount++
				errors = append(errors, fmt.Sprintf("DB Error for %s: %v", record.MemberName, err))
				continue
			}

			successCount++
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to save records", http.StatusInternalServerError)
		return
	}

	// 5. Return Response
	response := map[string]interface{}{
		"message":           fmt.Sprintf("Processed %d records successfully.", successCount),
		"processed_groups":  processedSummaries,
		"success_count":     successCount,
		"failed_count":      failedCount,
		"errors":            errors,
		"not_found_members": notFoundMembers,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
