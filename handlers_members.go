package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

func getMembers(w http.ResponseWriter, r *http.Request) {
	query := `
        SELECT m.id, m.name, m.rank, COALESCE(m.level, 0), COALESCE(m.eligible, 1),
			   COALESCE(m.squad_type, ''), COALESCE(m.troop_level, 0), COALESCE(m.profession, ''),
               COALESCE((SELECT power FROM power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), 0) as latest_power,
               COALESCE((SELECT recorded_at FROM power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), '') as latest_power_date,
			   COALESCE((SELECT power FROM squad_power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), 0) as latest_squad_power,
               COALESCE((SELECT recorded_at FROM squad_power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), '') as latest_squad_power_date,
               EXISTS(SELECT 1 FROM users WHERE member_id = m.id) as has_user
        FROM members m
        ORDER BY m.name
    `
	rows, err := db.Query(query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	members := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.Name, &m.Rank, &m.Level, &m.Eligible, &m.SquadType, &m.TroopLevel, &m.Profession, &m.Power, &m.PowerUpdatedAt, &m.SquadPower, &m.SquadPowerUpdatedAt, &m.HasUser); err != nil {
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

	result, err := db.Exec("INSERT INTO members (name, rank, level, eligible, squad_type, troop_level, profession) VALUES (?, ?, ?, ?, ?, ?, ?)", m.Name, m.Rank, m.Level, m.Eligible, m.SquadType, m.TroopLevel, m.Profession)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	m.ID = int(id)

	if m.Power != nil {
		_, insertErr := db.Exec(`INSERT INTO power_history (member_id, power, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, m.ID, *m.Power)
		if insertErr != nil {
			log.Printf("Warning: Failed to log initial power history for member %d: %v", m.ID, insertErr)
		}
	}

	if m.SquadPower != nil {
		db.Exec(`INSERT INTO squad_power_history (member_id, power, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, m.ID, *m.SquadPower)
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

	_, err = db.Exec("UPDATE members SET name = ?, rank = ?, level = ?, eligible = ?, squad_type = ?, troop_level = ?, profession = ? WHERE id = ?", m.Name, m.Rank, m.Level, m.Eligible, m.SquadType, m.TroopLevel, m.Profession, id)
	if err != nil {
		log.Printf("DB Update Error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

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

	_, err = db.Exec("DELETE FROM users WHERE member_id = ?", id)
	if err != nil {
		log.Printf("Warning: Failed to delete linked user for member %d: %v", id, err)
	}

	_, err = db.Exec("DELETE FROM power_history WHERE member_id = ?", id)
	if err != nil {
		log.Printf("Warning: Failed to delete power history for member %d: %v", id, err)
	}

	_, err = db.Exec("DELETE FROM squad_power_history WHERE member_id = ?", id)
	if err != nil {
		log.Printf("Warning: Failed to delete squad power history for member %d: %v", id, err)
	}

	_, err = db.Exec("DELETE FROM members WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func importCSV(w http.ResponseWriter, r *http.Request) {
	err := r.ParseMultipartForm(10 << 20)
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

	existingMembers := make(map[string]Member)
	rows, err := db.Query("SELECT id, name, rank FROM members")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var m Member
			rows.Scan(&m.ID, &m.Name, &m.Rank)
			existingMembers[m.Name] = m
		}
	}

	for i := startIndex; i < len(records); i++ {
		record := records[i]
		if len(record) <= nameIdx {
			continue
		}

		name := strings.TrimSpace(record[nameIdx])
		if name == "" {
			continue
		}

		rank := "R1"
		if hasRank && len(record) > rankIdx {
			parsedRank := strings.ToUpper(strings.TrimSpace(record[rankIdx]))
			if validRanks[parsedRank] {
				rank = parsedRank
			}
		}

		detected := DetectedMember{
			Name: name,
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

		if existing, found := existingMembers[name]; found {
			if !hasRank {
				detected.Rank = existing.Rank
			} else if existing.Rank != rank {
				detected.RankChanged = true
				detected.OldRank = existing.Rank
			}
		} else {
			detected.IsNew = true
			similarNames := []string{}
			for existingName := range existingMembers {
				if areSimilar(name, existingName) {
					similarNames = append(similarNames, existingName)
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
	for _, existing := range existingMembers {
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
