package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Helper to resolve aliases using the existing schema
func resolveMemberAliasLegacy(tx *sql.Tx, providedName string, currentUserID int) (*Member, string, error) {
	var m Member

	// 1. Exact Name
	err := tx.QueryRow(`SELECT id, name, rank FROM members WHERE LOWER(name) = LOWER(?)`, providedName).Scan(&m.ID, &m.Name, &m.Rank)
	if err == nil {
		return &m, "exact", nil
	}

	// 2. Alias Hierarchy (Personal -> Global)
	query := `
		SELECT m.id, m.name, m.rank, a.user_id 
		FROM member_aliases a
		JOIN members m ON a.member_id = m.id
		WHERE LOWER(a.alias) = LOWER(?) AND (a.user_id = ? OR a.user_id IS NULL)
		ORDER BY a.user_id DESC -- Prioritize personal (NOT NULL) over global (NULL)
		LIMIT 1`

	var aliasUserID *int
	err = tx.QueryRow(query, providedName, currentUserID).Scan(&m.ID, &m.Name, &m.Rank, &aliasUserID)
	if err == nil {
		if aliasUserID != nil {
			return &m, "personal_alias", nil
		}
		return &m, "global_alias", nil
	}

	return nil, "none", sql.ErrNoRows
}

func previewCSVImport(w http.ResponseWriter, r *http.Request) {
	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		http.Error(w, "Unable to parse form", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("csv_file")
	if err != nil {
		http.Error(w, "Missing csv_file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	weekDate := r.FormValue("week_date")

	// Get current user ID from context/session (Assuming a helper exists or use your auth standard)
	session, _ := store.Get(r, "session")
	currentUserID, _ := session.Values["user_id"].(int)

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil || len(records) < 2 {
		http.Error(w, "Invalid CSV format", http.StatusBadRequest)
		return
	}

	headers := records[0]
	colMap := make(map[string]int)
	for i, h := range headers {
		h = strings.ToLower(strings.TrimSpace(h))
		if strings.Contains(h, "day 1") {
			h = "monday"
		}
		if strings.Contains(h, "day 2") {
			h = "tuesday"
		}
		if strings.Contains(h, "day 3") {
			h = "wednesday"
		}
		if strings.Contains(h, "day 4") {
			h = "thursday"
		}
		if strings.Contains(h, "day 5") {
			h = "friday"
		}
		if strings.Contains(h, "day 6") {
			h = "saturday"
		}
		if strings.Contains(h, "total") {
			h = "total"
		}
		colMap[h] = i
	}

	tx, _ := db.Begin()
	defer tx.Rollback()

	response := VSImportPreviewResponse{}
	validDays := []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}

	for _, row := range records[1:] {
		nameIdx, ok := colMap["name"]
		if !ok || len(row) <= nameIdx {
			continue // Skip rows without names
		}
		providedName := strings.TrimSpace(row[nameIdx])
		if providedName == "" {
			continue
		}

		importRow := VSImportRow{
			OriginalName:  providedName,
			UpdatedFields: make(map[string]int),
		}

		var providedTotal *int
		for _, day := range validDays {
			if idx, exists := colMap[day]; exists && len(row) > idx && row[idx] != "" {
				val, _ := strconv.Atoi(strings.ReplaceAll(row[idx], ",", ""))
				importRow.UpdatedFields[day] = val
			}
		}
		if idx, exists := colMap["total"]; exists && len(row) > idx && row[idx] != "" {
			val, _ := strconv.Atoi(strings.ReplaceAll(row[idx], ",", ""))
			providedTotal = &val
			importRow.Total = providedTotal
		}

		member, matchType, err := resolveMemberAliasLegacy(tx, providedName, currentUserID)
		if err != nil {
			importRow.MatchType = "unresolved"
			response.Unresolved = append(response.Unresolved, importRow)
			continue
		}

		importRow.MatchedMember = member
		importRow.MatchType = matchType

		// Calculate missing Saturday logic
		if providedTotal != nil {
			_, hasSat := importRow.UpdatedFields["saturday"]
			if !hasSat {
				var dbMon, dbTue, dbWed, dbThu, dbFri int
				tx.QueryRow(`SELECT monday, tuesday, wednesday, thursday, friday FROM vs_points WHERE member_id = ? AND week_date = ?`, member.ID, weekDate).
					Scan(&dbMon, &dbTue, &dbWed, &dbThu, &dbFri)

				getVal := func(day string, dbVal int) int {
					if v, ok := importRow.UpdatedFields[day]; ok {
						return v
					}
					return dbVal
				}

				sum := getVal("monday", dbMon) + getVal("tuesday", dbTue) + getVal("wednesday", dbWed) + getVal("thursday", dbThu) + getVal("friday", dbFri)
				calcSat := *providedTotal - sum

				if calcSat >= 0 {
					importRow.UpdatedFields["saturday"] = calcSat
					importRow.CalculatedSat = true
				} else {
					importRow.Error = "Total is less than the sum of Monday-Friday"
				}
			}
		}
		response.Matched = append(response.Matched, importRow)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func commitCSVImport(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, ok := session.Values["user_id"].(int)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req VSImportCommitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	successCount := 0
	aliasCount := 0
	var dbErrors []string

	// 1. Process VS Points Records
	for _, row := range req.Records {
		if row.MatchedMember == nil || row.MatchedMember.ID == 0 {
			continue
		}

		var existingID int
		err := tx.QueryRow("SELECT id FROM vs_points WHERE member_id = ? AND week_date = ?", row.MatchedMember.ID, req.WeekDate).Scan(&existingID)

		if err == sql.ErrNoRows {
			cols := []string{"member_id", "week_date"}
			vals := []interface{}{row.MatchedMember.ID, req.WeekDate}
			placeholders := []string{"?", "?"}

			for day, val := range row.UpdatedFields {
				cols = append(cols, day)
				vals = append(vals, val)
				placeholders = append(placeholders, "?")
			}

			query := "INSERT INTO vs_points (" + strings.Join(cols, ", ") + ", updated_at) VALUES (" + strings.Join(placeholders, ", ") + ", CURRENT_TIMESTAMP)"
			_, err = tx.Exec(query, vals...)
		} else {
			var updates []string
			var vals []interface{}
			for day, val := range row.UpdatedFields {
				updates = append(updates, day+" = ?")
				vals = append(vals, val)
			}
			if len(updates) > 0 {
				vals = append(vals, row.MatchedMember.ID, req.WeekDate)
				query := "UPDATE vs_points SET " + strings.Join(updates, ", ") + ", updated_at = CURRENT_TIMESTAMP WHERE member_id = ? AND week_date = ?"
				_, err = tx.Exec(query, vals...)
			}
		}

		if err == nil {
			successCount++
		} else {
			dbErrors = append(dbErrors, fmt.Sprintf("Points Error (%s): %v", row.OriginalName, err))
		}
	}

	// 2. Process Saved Aliases
	aliasesReceived := len(req.SaveAliases)

	for _, aliasReq := range req.SaveAliases {
		if aliasReq.IsGlobal {
			// Global Alias: Wipe out any existing personal or global aliases with this exact string
			_, err := tx.Exec("DELETE FROM member_aliases WHERE LOWER(alias) = LOWER(?)", aliasReq.FailedAlias)
			if err != nil {
				dbErrors = append(dbErrors, fmt.Sprintf("Failed to clean old aliases for '%s': %v", aliasReq.FailedAlias, err))
			}

			// Insert the new Global Alias (user_id IS NULL)
			_, err = tx.Exec("INSERT INTO member_aliases (member_id, alias) VALUES (?, ?)", aliasReq.MemberID, aliasReq.FailedAlias)
			if err == nil {
				aliasCount++
			} else {
				dbErrors = append(dbErrors, fmt.Sprintf("Global Alias Insert Error ('%s'): %v", aliasReq.FailedAlias, err))
			}
		} else {
			// Personal Alias: Wipe out ONLY this user's existing mapping for this string (if they had one)
			_, err := tx.Exec("DELETE FROM member_aliases WHERE LOWER(alias) = LOWER(?) AND user_id = ?", aliasReq.FailedAlias, userID)
			if err != nil {
				dbErrors = append(dbErrors, fmt.Sprintf("Failed to clean personal alias for '%s': %v", aliasReq.FailedAlias, err))
			}

			// Insert the new Personal Alias mapping to the current user
			_, err = tx.Exec("INSERT INTO member_aliases (member_id, user_id, alias) VALUES (?, ?, ?)", aliasReq.MemberID, userID, aliasReq.FailedAlias)
			if err == nil {
				aliasCount++
			} else {
				dbErrors = append(dbErrors, fmt.Sprintf("Personal Alias Insert Error ('%s'): %v", aliasReq.FailedAlias, err))
			}
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to save changes", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"message":          fmt.Sprintf("Import successful. Updated %d records and saved %d new aliases.", successCount, aliasCount),
		"imported":         successCount,
		"aliases_saved":    aliasCount,
		"aliases_received": aliasesReceived,
	}

	if len(dbErrors) > 0 {
		response["errors"] = dbErrors
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
