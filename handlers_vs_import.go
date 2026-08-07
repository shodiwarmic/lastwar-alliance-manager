package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

// resolveMemberAlias resolves aliases using the tiered Alias Engine.
//
// This is the single-lookup form: on a tier 1/2 miss it builds the folded index
// itself, costing one extra query. Callers resolving many names in a loop must
// use resolveMemberAliasWithIndex with a prebuilt index instead — see
// buildFoldedNameIndex in namematch.go.
func resolveMemberAlias(tx *sql.Tx, providedName string, currentUserID int) (*Member, string, error) {
	return resolveMemberAliasWithIndex(tx, providedName, currentUserID, nil)
}

// resolveMemberAliasWithIndex is resolveMemberAlias with a caller-supplied folded
// index for tier 3. Pass nil to have it built on demand (and only on a tier 1/2
// miss, so the common path never pays for it).
func resolveMemberAliasWithIndex(tx *sql.Tx, providedName string, currentUserID int, idx *foldedNameIndex) (*Member, string, error) {
	var m Member

	// 1. Exact Name
	err := tx.QueryRow(`SELECT id, name, rank FROM members WHERE LOWER(name) = LOWER(?)`, providedName).Scan(&m.ID, &m.Name, &m.Rank)
	if err == nil {
		return &m, "exact", nil
	}

	// 2. Alias Hierarchy (Personal -> Global -> OCR)
	query := `
		SELECT m.id, m.name, m.rank, a.category
		FROM member_aliases a
		JOIN members m ON a.member_id = m.id
		WHERE LOWER(a.alias) = LOWER(?) AND (a.user_id = ? OR a.user_id IS NULL)
		ORDER BY
			CASE a.category
				WHEN 'personal' THEN 1
				WHEN 'global' THEN 2
				WHEN 'ocr' THEN 3
				ELSE 4
			END ASC
		LIMIT 1`

	var category string
	err = tx.QueryRow(query, providedName, currentUserID).Scan(&m.ID, &m.Name, &m.Rank, &category)
	if err == nil {
		return &m, category + "_alias", nil
	}

	// 3. Accent-folded fallback. Tiers 1 and 2 compare with SQLite's LOWER(),
	// which is ASCII-only, so "Pàcha" and "Pacha" miss each other. Folding
	// catches that; an ambiguous fold is reported as no match rather than a
	// guess (see foldedNameIndex.lookup).
	if idx == nil {
		built, err := buildFoldedNameIndex(tx, currentUserID)
		if err != nil {
			return nil, "none", sql.ErrNoRows
		}
		idx = built
	}
	if folded, ok := idx.lookup(providedName); ok {
		return folded, "folded", nil
	}

	return nil, "none", sql.ErrNoRows
}

// resolveOCRPlayer resolves an OCRPlayer to a Member using the alias engine.
// For ambiguous rows (Candidates non-nil), it tries each candidate split in order;
// the first that resolves via the alias engine wins, and its score travels with it.
// If no candidate resolves, it returns the heuristic fallback (candidates[0]) with
// a nil Member so the caller can decide how to handle it (surface for manual review,
// fall through to fuzzy matching, etc.).
//
// idx is the shared accent-folded index for tier 3; pass nil to have it built on
// demand. Callers in a loop should build it once (buildFoldedNameIndex) and pass
// it — an ambiguous OCR row tries every candidate, so a nil index can mean
// several rebuilds for a single entry.
func resolveOCRPlayer(tx *sql.Tx, entry OCRPlayer, userID int, idx *foldedNameIndex) (name string, score int64, member *Member, matchType string) {
	if len(entry.Candidates) == 0 {
		m, mt, err := resolveMemberAliasWithIndex(tx, entry.PlayerName, userID, idx)
		if err != nil {
			return entry.PlayerName, entry.Score, nil, "none"
		}
		return entry.PlayerName, entry.Score, m, mt
	}
	for _, c := range entry.Candidates {
		m, mt, err := resolveMemberAliasWithIndex(tx, c.PlayerName, userID, idx)
		if err == nil && m != nil {
			return c.PlayerName, c.Score, m, mt
		}
	}
	// No candidate resolved — return heuristic fallback (smallest score / rightmost split).
	return entry.Candidates[0].PlayerName, entry.Candidates[0].Score, nil, "none"
}

// commitCSVImport processes the final confirmed records from both CSV and OCR uploads
func commitCSVImport(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	userID := user.ID

	var req VSImportCommitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Snap to the game-time VS-week Monday so the client clock can't misfile the week.
	if normWeek, err := normalizeToGameWeekMonday(req.WeekDate); err != nil {
		http.Error(w, "Invalid week_date: must be YYYY-MM-DD", http.StatusBadRequest)
		return
	} else {
		req.WeekDate = normWeek
	}

	// Datapoint provenance: 'ocr' (upload.js) or 'csv' (vs.js); else 'import'.
	src := provenanceSource(req.Source)

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	successCount := 0
	aliasCount := 0
	var dbErrors []string

	// 1. Process Records (VS Points & Power)
	for _, row := range req.Records {
		if row.MatchedMember == nil || row.MatchedMember.ID == 0 {
			continue
		}

		hasVSPoints := false
		var powerVal int
		hasPower := false
		var killsVal int
		hasKills := false

		// Separate power / kills from VS points
		for day, val := range row.UpdatedFields {
			if day == "power" {
				hasPower = true
				powerVal = val
			} else if day == "kills" {
				hasKills = true
				killsVal = val
			} else {
				hasVSPoints = true
			}
		}

		// Save Power Record
		if hasPower {
			_, err = tx.Exec("INSERT INTO power_history (member_id, power, source) VALUES (?, ?, ?)", row.MatchedMember.ID, powerVal, src)
			if err != nil {
				dbErrors = append(dbErrors, fmt.Sprintf("Power Error (%s): %v", row.OriginalName, err))
			} else {
				successCount++
			}
		}

		// Save Kill Count Record
		if hasKills {
			_, err = tx.Exec("INSERT INTO kill_history (member_id, kills, source) VALUES (?, ?, ?)", row.MatchedMember.ID, killsVal, src)
			if err != nil {
				dbErrors = append(dbErrors, fmt.Sprintf("Kill Count Error (%s): %v", row.OriginalName, err))
			} else {
				successCount++
			}
		}

		// Save VS Points Record
		if hasVSPoints {
			var existingID int
			err := tx.QueryRow("SELECT id FROM vs_points WHERE member_id = ? AND week_date = ?", row.MatchedMember.ID, req.WeekDate).Scan(&existingID)

			var vsErr error
			if err == sql.ErrNoRows {
				cols, placeholders := []string{"member_id", "week_date"}, []string{"?", "?"}
				vals := []interface{}{row.MatchedMember.ID, req.WeekDate}

				for day, val := range row.UpdatedFields {
					if day == "power" || day == "kills" {
						continue
					}
					cols = append(cols, day)
					vals = append(vals, val)
					placeholders = append(placeholders, "?")
				}

				query := "INSERT INTO vs_points (" + strings.Join(cols, ", ") + ", updated_at) VALUES (" + strings.Join(placeholders, ", ") + ", CURRENT_TIMESTAMP)"
				_, vsErr = tx.Exec(query, vals...)

			} else if err == nil {
				var updates []string
				var vals []interface{}
				for day, val := range row.UpdatedFields {
					if day == "power" || day == "kills" {
						continue
					}
					updates = append(updates, day+" = ?")
					vals = append(vals, val)
				}
				if len(updates) > 0 {
					vals = append(vals, row.MatchedMember.ID, req.WeekDate)
					query := "UPDATE vs_points SET " + strings.Join(updates, ", ") + ", updated_at = CURRENT_TIMESTAMP WHERE member_id = ? AND week_date = ?"
					_, vsErr = tx.Exec(query, vals...)
				}
			} else {
				vsErr = err // Capture the actual database error
			}

			if vsErr != nil {
				dbErrors = append(dbErrors, fmt.Sprintf("VS Points Error (%s): %v", row.OriginalName, vsErr))
			} else if !hasPower && !hasKills {
				// Only increment success if it wasn't already incremented by power/kills inserts
				successCount++
			}
		}
	}

	// 2. Process Saved Aliases
	aliasesReceived := len(req.SaveAliases)
	for _, aliasReq := range req.SaveAliases {
		if aliasReq.Category == "global" || aliasReq.Category == "ocr" {
			_, err = tx.Exec("DELETE FROM member_aliases WHERE LOWER(alias) = LOWER(?)", aliasReq.FailedAlias)
			if err != nil {
				dbErrors = append(dbErrors, fmt.Sprintf("Failed to clear old global alias: %v", err))
				continue
			}

			_, err = tx.Exec("INSERT INTO member_aliases (member_id, category, alias) VALUES (?, ?, ?)", aliasReq.MemberID, aliasReq.Category, aliasReq.FailedAlias)
			if err == nil {
				aliasCount++
			} else {
				dbErrors = append(dbErrors, fmt.Sprintf("Alias Insert Error: %v", err))
			}

		} else if aliasReq.Category == "personal" {
			_, err = tx.Exec("DELETE FROM member_aliases WHERE LOWER(alias) = LOWER(?) AND user_id = ?", aliasReq.FailedAlias, userID)
			if err != nil {
				dbErrors = append(dbErrors, fmt.Sprintf("Failed to clear old personal alias: %v", err))
				continue
			}

			_, err = tx.Exec("INSERT INTO member_aliases (member_id, user_id, category, alias) VALUES (?, ?, 'personal', ?)", aliasReq.MemberID, userID, aliasReq.FailedAlias)
			if err == nil {
				aliasCount++
			} else {
				dbErrors = append(dbErrors, fmt.Sprintf("Alias Insert Error: %v", err))
			}
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to save changes", http.StatusInternalServerError)
		return
	}

	details := strconv.Itoa(successCount) + " members"
	if aliasCount > 0 {
		details += ", " + strconv.Itoa(aliasCount) + " aliases saved"
	}
	if req.OCRSummary != "" {
		details += " · " + req.OCRSummary
	}
	logActivity(userID, user.Username, "imported", "vs_points", req.WeekDate, false, details)

	response := map[string]interface{}{
		"message":          fmt.Sprintf("Import successful. Saved data for %d members and registered %d new aliases.", successCount, aliasCount),
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

// previewCSVImport processes uploaded CSV files and maps them to the existing alias engine
func previewCSVImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxCSVUploadSize)
	err := r.ParseMultipartForm(MaxCSVUploadSize)
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

	currentUserID := getAuthUser(r).ID

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
		if h == "member" {
			h = "name"
		} else if strings.Contains(h, "day 1") {
			h = "monday"
		} else if strings.Contains(h, "day 2") {
			h = "tuesday"
		} else if strings.Contains(h, "day 3") {
			h = "wednesday"
		} else if strings.Contains(h, "day 4") {
			h = "thursday"
		} else if strings.Contains(h, "day 5") {
			h = "friday"
		} else if strings.Contains(h, "day 6") {
			h = "saturday"
		} else if strings.Contains(h, "total") {
			h = "total"
		}
		colMap[h] = i
	}

	tx, _ := db.Begin()
	defer tx.Rollback()

	response := VSImportPreviewResponse{}

	// NEW: Fetch the complete roster so the frontend can populate the alias assignment dropdowns
	rows, err := tx.Query("SELECT id, name, rank FROM members ORDER BY LOWER(name) ASC")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var m Member
			if err := rows.Scan(&m.ID, &m.Name, &m.Rank); err == nil {
				response.AllMembers = append(response.AllMembers, m)
			}
		}
	}

	nameIdx, ok := colMap["name"]
	if !ok {
		http.Error(w, "CSV missing required column: Member (or Name)", http.StatusBadRequest)
		return
	}

	validDays := []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}

	// Built once for the whole file — the tier-3 folded fallback inside
	// resolveMemberAlias would otherwise rebuild it for every unmatched row.
	// Non-fatal on failure: exact and alias matching still work.
	foldIdx, err := buildFoldedNameIndex(tx, currentUserID)
	if err != nil {
		slog.Error("CSV import: folded name index build failed; continuing without accent tolerance", "error", err)
		foldIdx = nil
	}

	for _, row := range records[1:] {
		if len(row) <= nameIdx {
			continue
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

		member, matchType, err := resolveMemberAliasWithIndex(tx, providedName, currentUserID, foldIdx)
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
