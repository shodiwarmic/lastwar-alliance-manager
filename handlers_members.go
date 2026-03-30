package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"log"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

func getMembers(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)

	// CTE-based query replaces 6 correlated subqueries per row with a single pass.
	query := `
		WITH latest_power AS (
			SELECT member_id, power, recorded_at
			FROM (
				SELECT member_id, power, recorded_at,
					ROW_NUMBER() OVER (PARTITION BY member_id ORDER BY recorded_at DESC) as rn
				FROM power_history
			) WHERE rn = 1
		),
		latest_squad_power AS (
			SELECT member_id, power, recorded_at
			FROM (
				SELECT member_id, power, recorded_at,
					ROW_NUMBER() OVER (PARTITION BY member_id ORDER BY recorded_at DESC) as rn
				FROM squad_power_history
			) WHERE rn = 1
		),
		global_aliases AS (
			SELECT member_id, GROUP_CONCAT(alias, ', ') as aliases
			FROM member_aliases WHERE category = 'global'
			GROUP BY member_id
		),
		personal_aliases AS (
			SELECT member_id, GROUP_CONCAT(alias, ', ') as aliases
			FROM member_aliases WHERE category = 'personal' AND user_id = ?
			GROUP BY member_id
		)
		SELECT m.id, m.name, m.rank, COALESCE(m.level, 0), COALESCE(m.eligible, 1),
			   COALESCE(m.squad_type, ''), COALESCE(m.troop_level, 0), COALESCE(m.profession, ''),
			   COALESCE(lp.power, 0) as latest_power,
			   COALESCE(lp.recorded_at, '') as latest_power_date,
			   COALESCE(lsp.power, 0) as latest_squad_power,
			   COALESCE(lsp.recorded_at, '') as latest_squad_power_date,
			   EXISTS(SELECT 1 FROM users WHERE member_id = m.id) as has_user,
			   COALESCE(ga.aliases, '') as global_aliases,
			   COALESCE(pa.aliases, '') as personal_aliases,
			   COALESCE(m.notes, '') as notes
		FROM members m
		LEFT JOIN latest_power lp ON lp.member_id = m.id
		LEFT JOIN latest_squad_power lsp ON lsp.member_id = m.id
		LEFT JOIN global_aliases ga ON ga.member_id = m.id
		LEFT JOIN personal_aliases pa ON pa.member_id = m.id
		ORDER BY m.name
	`

	rows, err := db.Query(query, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	members := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(
			&m.ID, &m.Name, &m.Rank, &m.Level, &m.Eligible, &m.SquadType, &m.TroopLevel,
			&m.Profession, &m.Power, &m.PowerUpdatedAt, &m.SquadPower, &m.SquadPowerUpdatedAt,
			&m.HasUser, &m.GlobalAliases, &m.PersonalAliases, &m.Notes,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		members = append(members, m)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(members)
}

func createMember(w http.ResponseWriter, r *http.Request) {
	var m Member
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if !m.Eligible {
		m.Eligible = true
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	result, err := tx.Exec("INSERT INTO members (name, rank, level, eligible, squad_type, troop_level, profession) VALUES (?, ?, ?, ?, ?, ?, ?)", m.Name, m.Rank, m.Level, m.Eligible, m.SquadType, m.TroopLevel, m.Profession)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	m.ID = int(id)

	if m.Power != nil {
		if _, err := tx.Exec(`INSERT INTO power_history (member_id, power, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, m.ID, *m.Power); err != nil {
			http.Error(w, "Failed to log initial power history", http.StatusInternalServerError)
			return
		}
	}

	if m.SquadPower != nil {
		tx.Exec(`INSERT INTO squad_power_history (member_id, power, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, m.ID, *m.SquadPower)
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to save member", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(m)
}

func updateMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid member ID", http.StatusBadRequest)
		return
	}

	var m Member
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		log.Printf("JSON Decode Error: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 1. NEW: Fetch the CURRENT name before we overwrite it
	var oldName string
	err = db.QueryRow("SELECT name FROM members WHERE id = ?", id).Scan(&oldName)
	if err != nil {
		log.Printf("Error fetching current member name: %v", err)
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}

	// 2. Perform the main UPDATE
	_, err = db.Exec("UPDATE members SET name = ?, rank = ?, level = ?, eligible = ?, squad_type = ?, troop_level = ?, profession = ?, notes = ? WHERE id = ?", m.Name, m.Rank, m.Level, m.Eligible, m.SquadType, m.TroopLevel, m.Profession, m.Notes, id)
	if err != nil {
		log.Printf("DB Update Error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 3. NEW: If the update succeeded and the name actually changed, save the old name as a Global Alias
	if m.Name != "" && oldName != m.Name {
		_, aliasErr := db.Exec("INSERT OR IGNORE INTO member_aliases (member_id, user_id, category, alias) VALUES (?, NULL, 'global', ?)", id, oldName)
		if aliasErr != nil {
			log.Printf("Warning: Failed to auto-create global alias for name change (member %d): %v", id, aliasErr)
		}
	}

	// Handle Power History
	if m.Power != nil {
		var currentPower int64 = -1
		_ = db.QueryRow(`SELECT power FROM power_history WHERE member_id = ? ORDER BY recorded_at DESC LIMIT 1`, id).Scan(&currentPower)

		if currentPower != *m.Power {
			_, insertErr := db.Exec(`INSERT INTO power_history (member_id, power, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, id, *m.Power)
			if insertErr != nil {
				log.Printf("Warning: Failed to log power history for member %d: %v", id, insertErr)
			}
		}
	}

	// Handle Squad Power History
	if m.SquadPower != nil {
		var currentSquadPower int64 = -1
		_ = db.QueryRow(`SELECT power FROM squad_power_history WHERE member_id = ? ORDER BY recorded_at DESC LIMIT 1`, id).Scan(&currentSquadPower)
		if currentSquadPower != *m.SquadPower {
			db.Exec(`INSERT INTO squad_power_history (member_id, power, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, id, *m.SquadPower)
		}
	}

	m.ID = id
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(m)
}

func deleteMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid member ID", http.StatusBadRequest)
		return
	}

	if _, err = db.Exec("DELETE FROM users WHERE member_id = ?", id); err != nil {
		slog.Error("Failed to delete linked user for member", "member_id", id, "error", err)
	}

	if _, err = db.Exec("DELETE FROM power_history WHERE member_id = ?", id); err != nil {
		slog.Error("Failed to delete power history for member", "member_id", id, "error", err)
	}

	if _, err = db.Exec("DELETE FROM squad_power_history WHERE member_id = ?", id); err != nil {
		slog.Error("Failed to delete squad power history for member", "member_id", id, "error", err)
	}

	if _, err = db.Exec("DELETE FROM members WHERE id = ?", id); err != nil {
		slog.Error("Failed to delete member", "member_id", id, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func archiveMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid member ID", http.StatusBadRequest)
		return
	}

	var req struct {
		LeaveReason string `json:"leave_reason"`
	}
	json.NewDecoder(r.Body).Decode(&req) // soft decode — field is optional

	result, err := db.Exec(
		"UPDATE members SET rank = 'EX', eligible = 0, leave_reason = ? WHERE id = ? AND rank != 'EX'",
		req.LeaveReason, id,
	)
	if err != nil {
		slog.Error("Failed to archive member", "member_id", id, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		http.Error(w, "Member not found or already archived", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func reactivateMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid member ID", http.StatusBadRequest)
		return
	}

	var body struct {
		Rank string `json:"rank"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	validRanks := map[string]bool{"R1": true, "R2": true, "R3": true, "R4": true, "R5": true}
	if !validRanks[body.Rank] {
		http.Error(w, "Rank must be R1–R5", http.StatusBadRequest)
		return
	}

	result, err := db.Exec("UPDATE members SET rank = ?, eligible = 1 WHERE id = ? AND rank = 'EX'", body.Rank, id)
	if err != nil {
		slog.Error("Failed to reactivate member", "member_id", id, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		http.Error(w, "Member not found or not archived", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func getFormerMembers(w http.ResponseWriter, r *http.Request) {
	query := `
		SELECT
			m.id,
			m.name,
			COALESCE((
				SELECT power FROM power_history
				WHERE member_id = m.id
				ORDER BY recorded_at DESC LIMIT 1
			), 0) as last_power,
			COUNT(DISTINCT tl.id) as train_count,
			COALESCE(MAX(vp.week_date), '') as last_vs_week,
			m.leave_reason
		FROM members m
		LEFT JOIN train_logs tl ON tl.conductor_id = m.id
		LEFT JOIN vs_points vp ON vp.member_id = m.id
		WHERE m.rank = 'EX'
		GROUP BY m.id
		ORDER BY m.name
	`

	rows, err := db.Query(query)
	if err != nil {
		slog.Error("Failed to query former members", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	members := []FormerMember{}
	for rows.Next() {
		var fm FormerMember
		if err := rows.Scan(&fm.ID, &fm.Name, &fm.LastPower, &fm.TrainCount, &fm.LastVSWeek, &fm.LeaveReason); err != nil {
			slog.Error("Failed to scan former member row", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		members = append(members, fm)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(members)
}

func importCSV(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxCSVUploadSize)
	err := r.ParseMultipartForm(MaxCSVUploadSize)
	if err != nil {
		http.Error(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil || len(records) == 0 {
		http.Error(w, "Failed to parse or empty CSV", http.StatusBadRequest)
		return
	}

	startIndex := 0
	headerMap := make(map[string]int)

	for i, col := range records[0] {
		lowerCol := strings.ToLower(strings.TrimSpace(col))
		if strings.Contains(lowerCol, "name") || strings.Contains(lowerCol, "user") {
			headerMap["name"] = i
		} else if strings.Contains(lowerCol, "rank") {
			headerMap["rank"] = i
		} else if strings.Contains(lowerCol, "power") {
			headerMap["power"] = i
		} else if strings.Contains(lowerCol, "level") || strings.Contains(lowerCol, "hq") {
			headerMap["level"] = i
		}
	}

	if len(headerMap) > 0 {
		startIndex = 1
	} else {
		headerMap["name"] = 0
		if len(records[0]) > 1 {
			headerMap["rank"] = 1
		}
	}

	nameIdx, nameOk := headerMap["name"]
	if !nameOk {
		http.Error(w, "CSV must contain a 'Username' or 'Name' column", http.StatusBadRequest)
		return
	}

	rankIdx, hasRank := headerMap["rank"]
	powerIdx, hasPower := headerMap["power"]
	levelIdx, hasLevel := headerMap["level"]

	validRanks := map[string]bool{"R1": true, "R2": true, "R3": true, "R4": true, "R5": true}
	detectedMembers := []DetectedMember{}
	errors := []string{}

	// --- NEW ALIAS LOOKUP LOGIC ---
	// 1. Get the current user's ID to fetch their personal aliases
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)

	// 2. Build fast-lookup maps for canonical names and aliases
	existingMembersByID := make(map[int]Member)
	existingMembersByName := make(map[string]int) // Case-insensitive lookup

	rows, err := db.Query("SELECT id, name, rank FROM members")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var m Member
			rows.Scan(&m.ID, &m.Name, &m.Rank)
			existingMembersByID[m.ID] = m
			existingMembersByName[strings.ToLower(m.Name)] = m.ID
		}
	}

	aliasToID := make(map[string]int) // lower(alias) -> member_id
	if userID > 0 {
		aliasRows, err := db.Query("SELECT member_id, alias FROM member_aliases WHERE user_id IS NULL OR user_id = ?", userID)
		if err == nil {
			defer aliasRows.Close()
			for aliasRows.Next() {
				var mID int
				var alias string
				aliasRows.Scan(&mID, &alias)
				aliasToID[strings.ToLower(alias)] = mID
			}
		}
	}
	// ------------------------------

	for i := startIndex; i < len(records); i++ {
		record := records[i]
		if len(record) <= nameIdx {
			continue
		}

		name := strings.TrimSpace(record[nameIdx])
		if name == "" {
			continue
		}
		lowerName := strings.ToLower(name)

		rank := "R1"
		if hasRank && len(record) > rankIdx {
			parsedRank := strings.ToUpper(strings.TrimSpace(record[rankIdx]))
			if validRanks[parsedRank] {
				rank = parsedRank
			}
		}

		detected := DetectedMember{
			Name: name, // This will be overwritten with the canonical name if an alias matches
			Rank: rank,
		}

		if hasPower && len(record) > powerIdx {
			powerStr := regexp.MustCompile(`[^0-9]`).ReplaceAllString(record[powerIdx], "")
			if p, err := strconv.ParseInt(powerStr, 10, 64); err == nil {
				detected.Power = p
			}
		}

		if hasLevel && len(record) > levelIdx {
			levelStr := regexp.MustCompile(`[^0-9]`).ReplaceAllString(record[levelIdx], "")
			if l, err := strconv.Atoi(levelStr); err == nil {
				detected.Level = l
			}
		}

		// --- RESOLVE MEMBER IDENTITY ---
		var matchedMember Member
		var isExisting bool

		// Priority 1: Exact Name Match
		if mID, found := existingMembersByName[lowerName]; found {
			matchedMember = existingMembersByID[mID]
			isExisting = true
		} else if mID, found := aliasToID[lowerName]; found {
			// Priority 2: Alias Match
			matchedMember = existingMembersByID[mID]
			isExisting = true
		}

		if isExisting {
			// Overwrite the CSV name with the database's canonical name
			// This prevents the system from accidentally creating a duplicate or failing to remove them
			detected.Name = matchedMember.Name

			if !hasRank {
				detected.Rank = matchedMember.Rank
			} else if matchedMember.Rank != rank {
				detected.RankChanged = true
				detected.OldRank = matchedMember.Rank
			}
		} else {
			detected.IsNew = true
			similarNames := []string{}
			for _, existingName := range existingMembersByID {
				if areSimilar(name, existingName.Name) {
					similarNames = append(similarNames, existingName.Name)
				}
			}
			if len(similarNames) > 0 {
				detected.SimilarMatch = similarNames
			}
		}

		detectedMembers = append(detectedMembers, detected)
	}

	membersToRemove := []MemberToRemove{}
	csvNames := make(map[string]bool)
	for _, m := range detectedMembers {
		csvNames[m.Name] = true
	}
	for _, existing := range existingMembersByID {
		if !csvNames[existing.Name] {
			membersToRemove = append(membersToRemove, MemberToRemove{
				ID:   existing.ID,
				Name: existing.Name,
				Rank: existing.Rank,
			})
		}
	}

	result := map[string]interface{}{
		"detected_members":  detectedMembers,
		"members_to_remove": membersToRemove,
		"errors":            errors,
		"total_rows":        len(records) - startIndex,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func confirmMemberUpdates(w http.ResponseWriter, r *http.Request) {
	var request ConfirmRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result := ConfirmResult{}

	for _, rename := range request.Renames {
		db.Exec("UPDATE members SET name = ? WHERE name = ?", rename.NewName, rename.OldName)
	}

	for _, member := range request.Members {
		var existingID int
		var existingRank string
		err := db.QueryRow("SELECT id, rank FROM members WHERE name = ?", member.Name).Scan(&existingID, &existingRank)

		if err == sql.ErrNoRows {
			res, err := db.Exec("INSERT INTO members (name, rank, level) VALUES (?, ?, ?)", member.Name, member.Rank, member.Level)
			if err == nil {
				id, _ := res.LastInsertId()
				existingID = int(id)
				result.Added++
			}
		} else if err == nil {
			query := "UPDATE members SET rank = ?"
			args := []interface{}{member.Rank}

			if member.Level > 0 {
				query += ", level = ?"
				args = append(args, member.Level)
			}
			query += " WHERE id = ?"
			args = append(args, existingID)

			db.Exec(query, args...)

			if existingRank != member.Rank {
				result.Updated++
			} else {
				result.Unchanged++
			}
		}

		if member.Power > 0 && existingID > 0 {
			var currentPower int64 = -1
			db.QueryRow("SELECT power FROM power_history WHERE member_id = ? ORDER BY recorded_at DESC LIMIT 1", existingID).Scan(&currentPower)
			if currentPower != member.Power {
				db.Exec("INSERT INTO power_history (member_id, power, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)", existingID, member.Power)
			}
		}

		if member.SquadPower > 0 && existingID > 0 {
			var currentSquadPower int64 = -1
			db.QueryRow("SELECT power FROM squad_power_history WHERE member_id = ? ORDER BY recorded_at DESC LIMIT 1", existingID).Scan(&currentSquadPower)
			if currentSquadPower != member.SquadPower {
				db.Exec("INSERT INTO squad_power_history (member_id, power, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)", existingID, member.SquadPower)
			}
		}
	}

	if len(request.RemoveMemberIDs) > 0 {
		for _, id := range request.RemoveMemberIDs {
			db.Exec("DELETE FROM users WHERE member_id = ?", id)
			db.Exec("DELETE FROM power_history WHERE member_id = ?", id)
			db.Exec("DELETE FROM squad_power_history WHERE member_id = ?", id)
			db.Exec("DELETE FROM members WHERE id = ?", id)
			result.Removed++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// Get member statistics for train scheduling
func getMemberStats(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT 
			m.id, 
			m.name, 
			m.rank,
			COUNT(DISTINCT CASE WHEN ts.conductor_id = m.id THEN ts.date END) as conductor_count,
			MAX(CASE WHEN ts.conductor_id = m.id THEN ts.date END) as last_conductor_date,
			COUNT(DISTINCT CASE WHEN ts.backup_id = m.id THEN ts.date END) as backup_count,
			COUNT(DISTINCT CASE WHEN ts.backup_id = m.id AND ts.conductor_showed_up = 0 THEN ts.date END) as backup_used_count,
			COUNT(DISTINCT CASE WHEN ts.conductor_id = m.id AND ts.conductor_showed_up = 0 THEN ts.date END) as conductor_no_show_count
		FROM members m
		LEFT JOIN train_schedules ts ON ts.conductor_id = m.id OR ts.backup_id = m.id
		GROUP BY m.id, m.name, m.rank
		ORDER BY m.name
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	stats := []MemberStats{}
	for rows.Next() {
		var s MemberStats
		var lastDate sql.NullString
		if err := rows.Scan(&s.ID, &s.Name, &s.Rank, &s.ConductorCount, &lastDate, &s.BackupCount, &s.BackupUsedCount, &s.ConductorNoShowCount); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if lastDate.Valid {
			s.LastConductorDate = &lastDate.String
		}
		stats = append(stats, s)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// getMyProfile retrieves the current user's linked member stats and latest power history
func getMyProfile(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	memberID, ok := session.Values["member_id"].(int)
	if !ok {
		http.Error(w, "No linked member profile found", http.StatusNotFound)
		return
	}

	var m Member

	// Added m.eligible and updated to fetch the latest squad_power history
	err := db.QueryRow(`
		SELECT 
			m.id, m.name, m.rank, m.eligible, m.level, 
			(SELECT power FROM power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1) as power,
			COALESCE(m.squad_type, ''), 
			(SELECT power FROM squad_power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1) as squad_power,
			COALESCE(m.troop_level, 0), 
			COALESCE(m.profession, '') 
		FROM members m WHERE m.id = ?`, memberID).
		Scan(&m.ID, &m.Name, &m.Rank, &m.Eligible, &m.Level, &m.Power, &m.SquadType, &m.SquadPower, &m.TroopLevel, &m.Profession)

	if err != nil {
		log.Printf("Profile Fetch Error: %v", err)
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(m)
}

// updateMyProfile allows a user to self-serve update their own in-game stats
func updateMyProfile(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	memberID, ok := session.Values["member_id"].(int)
	if !ok {
		http.Error(w, "Forbidden: No linked member profile", http.StatusForbidden)
		return
	}

	var req struct {
		Name       string `json:"name"`
		Level      int    `json:"level"`
		Power      int64  `json:"power"`
		TroopLevel int    `json:"troop_level"`
		SquadType  string `json:"squad_type"`
		SquadPower int64  `json:"squad_power"`
		Profession string `json:"profession"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// --- ZERO-TRUST GAME LOGIC VALIDATION ---
	// 1. Enforce Max HQ Level from Settings
	var maxHQ int
	db.QueryRow("SELECT max_hq_level FROM settings WHERE id = 1").Scan(&maxHQ)
	if req.Level > maxHQ {
		req.Level = maxHQ
	}

	// 2. Enforce Troop Level Requirements based on HQ Level
	troopReqs := map[int]int{1: 1, 2: 4, 3: 6, 4: 10, 5: 14, 6: 17, 7: 20, 8: 24, 9: 27, 10: 30, 11: 35}
	if reqHQ, exists := troopReqs[req.TroopLevel]; exists && req.Level < reqHQ {
		maxValid := 0
		for t, hq := range troopReqs {
			if req.Level >= hq && t > maxValid {
				maxValid = t
			}
		}
		req.TroopLevel = maxValid // Downgrade to the highest legal tier
	}

	// Execute database updates...
	_, err := db.Exec(`
		UPDATE members 
		SET name = ?, level = ?, troop_level = ?, squad_type = ?, profession = ?
		WHERE id = ?`,
		req.Name, req.Level, req.TroopLevel, req.SquadType, req.Profession, memberID)

	if err != nil {
		log.Printf("Profile Update Error: %v", err)
		http.Error(w, "Failed to update profile", http.StatusInternalServerError)
		return
	}

	if req.Power > 0 {
		db.Exec(`INSERT INTO power_history (member_id, power) VALUES (?, ?)`, memberID, req.Power)
	}
	if req.SquadPower > 0 {
		db.Exec(`INSERT INTO squad_power_history (member_id, power) VALUES (?, ?)`, memberID, req.SquadPower)
	}

	w.WriteHeader(http.StatusOK)
}

type AliasResponse struct {
	ID       int    `json:"id"`
	Alias    string `json:"alias"`
	Category string `json:"category"` // Changed from IsGlobal bool
	IsMine   bool   `json:"is_mine"`
}

func getMemberAliases(w http.ResponseWriter, r *http.Request) {
	memberID := mux.Vars(r)["id"]
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)

	// Fetch all aliases for this member (Global, OCR, and the user's specific Personal aliases)
	rows, err := db.Query(`
		SELECT id, alias, category, user_id = ? as is_mine
		FROM member_aliases
		WHERE member_id = ? AND (category != 'personal' OR user_id = ?)
		ORDER BY 
			CASE category 
				WHEN 'personal' THEN 1 
				WHEN 'global' THEN 2 
				WHEN 'ocr' THEN 3 
				ELSE 4 
			END ASC, 
			alias ASC
	`, userID, memberID, userID)

	if err != nil {
		http.Error(w, "Failed to fetch aliases", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var aliases []AliasResponse
	for rows.Next() {
		var a AliasResponse
		rows.Scan(&a.ID, &a.Alias, &a.Category, &a.IsMine)
		aliases = append(aliases, a)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(aliases)
}

func addMemberAlias(w http.ResponseWriter, r *http.Request) {
	memberID := mux.Vars(r)["id"]
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	isAdmin, _ := session.Values["is_admin"].(bool)

	canManageGlobal := isAdmin
	if !canManageGlobal {
		_ = db.QueryRow(`
			SELECT p.manage_members 
			FROM users u
			JOIN members m ON u.member_id = m.id
			JOIN permissions p ON m.rank = p.rank
			WHERE u.id = ?
		`, userID).Scan(&canManageGlobal)
	}

	var req struct {
		Alias    string `json:"alias"`
		IsGlobal bool   `json:"is_global"` // UI still sends a boolean for simplicity
	}
	json.NewDecoder(r.Body).Decode(&req)

	if req.Alias == "" {
		http.Error(w, "Alias cannot be empty", http.StatusBadRequest)
		return
	}

	if req.IsGlobal && !canManageGlobal {
		http.Error(w, "Only administrators or roster managers can create global aliases", http.StatusForbidden)
		return
	}

	var err error
	if req.IsGlobal {
		_, err = db.Exec("INSERT INTO member_aliases (member_id, category, alias) VALUES (?, 'global', ?)", memberID, req.Alias)
	} else {
		_, err = db.Exec("INSERT INTO member_aliases (member_id, user_id, category, alias) VALUES (?, ?, 'personal', ?)", memberID, userID, req.Alias)
	}

	if err != nil {
		http.Error(w, "Failed to save alias. It may already exist.", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func deleteMemberAlias(w http.ResponseWriter, r *http.Request) {
	aliasID := mux.Vars(r)["id"]
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	isAdmin, _ := session.Values["is_admin"].(bool)

	canManageGlobal := isAdmin
	if !canManageGlobal {
		_ = db.QueryRow(`
			SELECT p.manage_members 
			FROM users u
			JOIN members m ON u.member_id = m.id
			JOIN permissions p ON m.rank = p.rank
			WHERE u.id = ?
		`, userID).Scan(&canManageGlobal)
	}

	var category string
	var ownerID *int
	err := db.QueryRow("SELECT category, user_id FROM member_aliases WHERE id = ?", aliasID).Scan(&category, &ownerID)
	if err != nil {
		http.Error(w, "Alias not found", http.StatusNotFound)
		return
	}

	// Security Check
	if (category == "global" || category == "ocr") && !canManageGlobal {
		http.Error(w, "Only administrators or roster managers can delete global/ocr aliases", http.StatusForbidden)
		return
	}
	if category == "personal" && ownerID != nil && *ownerID != userID && !isAdmin {
		http.Error(w, "You can only delete your own personal aliases", http.StatusForbidden)
		return
	}

	db.Exec("DELETE FROM member_aliases WHERE id = ?", aliasID)
	w.WriteHeader(http.StatusOK)
}
