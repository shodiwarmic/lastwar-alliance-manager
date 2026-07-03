package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

func getMembers(w http.ResponseWriter, r *http.Request) {
	userID := getAuthUser(r).ID

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
		latest_hero_power AS (
			SELECT member_id, power, recorded_at
			FROM (
				SELECT member_id, power, recorded_at,
					ROW_NUMBER() OVER (PARTITION BY member_id ORDER BY recorded_at DESC) as rn
				FROM hero_power_history
			) WHERE rn = 1
		),
		latest_kills AS (
			SELECT member_id, kills, recorded_at
			FROM (
				SELECT member_id, kills, recorded_at,
					ROW_NUMBER() OVER (PARTITION BY member_id ORDER BY recorded_at DESC) as rn
				FROM kill_history
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
		),
		member_skills_agg AS (
			SELECT member_id, GROUP_CONCAT(skill_key) AS skills
			FROM (SELECT member_id, skill_key FROM member_skills ORDER BY skill_key)
			GROUP BY member_id
		)
		SELECT m.id, m.name, m.rank,
			   COALESCE((SELECT hq_level FROM hq_level_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), 0) as level,
			   COALESCE(m.eligible, 1),
			   COALESCE(m.squad_type, ''), COALESCE(m.troop_level, 0), COALESCE(m.profession, ''),
			   (SELECT profession_level FROM profession_level_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1) as profession_level,
			   COALESCE(lp.power, 0) as latest_power,
			   COALESCE(lp.recorded_at, '') as latest_power_date,
			   COALESCE(lsp.power, 0) as latest_squad_power,
			   COALESCE(lsp.recorded_at, '') as latest_squad_power_date,
			   EXISTS(SELECT 1 FROM users WHERE member_id = m.id) as has_user,
			   COALESCE(ga.aliases, '') as global_aliases,
			   COALESCE(pa.aliases, '') as personal_aliases,
			   COALESCE(m.notes, '') as notes,
			   COALESCE(lhp.power, 0) as latest_hero_power,
			   COALESCE(lhp.recorded_at, '') as latest_hero_power_date,
			   COALESCE(lk.kills, 0) as latest_kills,
			   COALESCE(lk.recorded_at, '') as latest_kills_date,
			   COALESCE(msa.skills, '') as skills,
			   m.lastrank_public_id, m.lastrank_synced_at,
			   COALESCE(m.lastrank_photo_url, ''), COALESCE(m.lastrank_photo_failover, ''),
			   COALESCE(m.joined_at, '')
		FROM members m
		LEFT JOIN latest_power lp ON lp.member_id = m.id
		LEFT JOIN latest_squad_power lsp ON lsp.member_id = m.id
		LEFT JOIN latest_hero_power lhp ON lhp.member_id = m.id
		LEFT JOIN latest_kills lk ON lk.member_id = m.id
		LEFT JOIN global_aliases ga ON ga.member_id = m.id
		LEFT JOIN personal_aliases pa ON pa.member_id = m.id
		LEFT JOIN member_skills_agg msa ON msa.member_id = m.id
		WHERE m.rank != 'EX'
		ORDER BY m.name
	`

	rows, err := db.Query(query, userID)
	if err != nil {
		slog.Error("getMembers: query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	members := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(
			&m.ID, &m.Name, &m.Rank, &m.Level, &m.Eligible, &m.SquadType, &m.TroopLevel,
			&m.Profession, &m.ProfessionLevel, &m.Power, &m.PowerUpdatedAt, &m.SquadPower, &m.SquadPowerUpdatedAt,
			&m.HasUser, &m.GlobalAliases, &m.PersonalAliases, &m.Notes,
			&m.HeroPower, &m.HeroPowerUpdatedAt,
			&m.CurrentKills, &m.KillsUpdatedAt,
			&m.Skills,
			&m.LastRankPublicID, &m.LastRankSyncedAt,
			&m.LastRankPhotoURL, &m.LastRankPhotoFailover,
			&m.JoinedAt,
		); err != nil {
			slog.Error("getMembers: scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
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
		http.Error(w, "Invalid request body", http.StatusBadRequest)
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

	// Manual add = a genuine new join → stamp joined_at with today's game date.
	// HQ level is history-only (no members.level column); seed it below.
	result, err := tx.Exec("INSERT INTO members (name, rank, eligible, squad_type, troop_level, profession, joined_at) VALUES (?, ?, ?, ?, ?, ?, ?)", m.Name, m.Rank, m.Eligible, m.SquadType, m.TroopLevel, m.Profession, gameDate())
	if err != nil {
		slog.Error("createMember: insert failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	m.ID = int(id)

	// Seed HQ level + profession level history (history-only fields).
	if m.Level > 0 {
		tx.Exec(`INSERT INTO hq_level_history (member_id, hq_level, source) VALUES (?, ?, 'manual')`, m.ID, m.Level)
	}
	if m.ProfessionLevel != nil && *m.ProfessionLevel > 0 {
		tx.Exec(`INSERT INTO profession_level_history (member_id, profession_level, source) VALUES (?, ?, 'manual')`, m.ID, *m.ProfessionLevel)
	}

	if m.Power != nil {
		if _, err := tx.Exec(`INSERT INTO power_history (member_id, power, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, m.ID, *m.Power); err != nil {
			http.Error(w, "Failed to log initial power history", http.StatusInternalServerError)
			return
		}
	}

	if m.SquadPower != nil {
		tx.Exec(`INSERT INTO squad_power_history (member_id, power, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, m.ID, *m.SquadPower)
	}

	if m.HeroPower != nil && *m.HeroPower > 0 {
		tx.Exec(`INSERT INTO hero_power_history (member_id, power, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, m.ID, *m.HeroPower)
	}

	if m.CurrentKills != nil && *m.CurrentKills > 0 {
		tx.Exec(`INSERT INTO kill_history (member_id, kills, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, m.ID, *m.CurrentKills)
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to save member", http.StatusInternalServerError)
		return
	}

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "created", "member", m.Name, false)

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
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Join date is optional; when present it must be a valid YYYY-MM-DD date.
	if m.JoinedAt != "" {
		if _, perr := parseDate(m.JoinedAt); perr != nil {
			http.Error(w, "Invalid join date (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
	}

	// Read-modify-write in one transaction so the activity-log diff is consistent
	// and concurrent edits can't interleave (matches createMember).
	tx, err := db.Begin()
	if err != nil {
		slog.Error("updateMember: begin failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// 1. Fetch current values before overwriting so we can diff for the activity log
	var old struct {
		Name             string
		Rank             string
		TroopLevel       int
		Profession       string
		SquadType        string
		Eligible         bool
		LastRankPublicID sql.NullInt64
		JoinedAt         string
	}
	err = tx.QueryRow("SELECT name, rank, troop_level, profession, squad_type, eligible, lastrank_public_id, COALESCE(joined_at, '') FROM members WHERE id = ?", id).Scan(
		&old.Name, &old.Rank, &old.TroopLevel, &old.Profession, &old.SquadType, &old.Eligible, &old.LastRankPublicID, &old.JoinedAt,
	)
	if err != nil {
		slog.Error("Error fetching current member name", "error", err)
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}

	// HQ level + profession level are history-only (no members column); read the
	// current (latest) values for diffing and change-detection.
	oldHQ, _ := latestHistoryValue(tx, "hq_level_history", "hq_level", id)
	oldProfLevel, hadProfLevel := latestHistoryValue(tx, "profession_level_history", "profession_level", id)

	// 2. Perform the main UPDATE. joined_at = NULLIF(?, '') so a blank clears it.
	_, err = tx.Exec("UPDATE members SET name = ?, rank = ?, eligible = ?, squad_type = ?, troop_level = ?, profession = ?, notes = ?, lastrank_public_id = ?, joined_at = NULLIF(?, '') WHERE id = ?", m.Name, m.Rank, m.Eligible, m.SquadType, m.TroopLevel, m.Profession, m.Notes, m.LastRankPublicID, m.JoinedAt, id)
	if err != nil {
		slog.Error("updateMember: exec failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// 3. If the name changed, save the old name as a Global Alias
	if m.Name != "" && old.Name != m.Name {
		_, aliasErr := tx.Exec("INSERT OR IGNORE INTO member_aliases (member_id, user_id, category, alias) VALUES (?, NULL, 'global', ?)", id, old.Name)
		if aliasErr != nil {
			slog.Warn("Failed to auto-create global alias for name change", "member_id", id, "error", aliasErr)
		}
	}

	// Build activity details from changed fields
	var changes []string
	if old.Name != m.Name {
		changes = append(changes, "name: "+old.Name+" → "+m.Name)
	}
	if old.Rank != m.Rank {
		changes = append(changes, "rank: "+old.Rank+" → "+m.Rank)
	}
	if m.Level > 0 && oldHQ != m.Level {
		changes = append(changes, "HQ level: "+strconv.Itoa(oldHQ)+" → "+strconv.Itoa(m.Level))
	}
	if m.ProfessionLevel != nil && *m.ProfessionLevel > 0 && (!hadProfLevel || oldProfLevel != *m.ProfessionLevel) {
		prev := "—"
		if hadProfLevel {
			prev = strconv.Itoa(oldProfLevel)
		}
		changes = append(changes, "profession level: "+prev+" → "+strconv.Itoa(*m.ProfessionLevel))
	}
	if old.TroopLevel != m.TroopLevel {
		changes = append(changes, "troop level: "+strconv.Itoa(old.TroopLevel)+" → "+strconv.Itoa(m.TroopLevel))
	}
	if old.Profession != m.Profession && m.Profession != "" {
		changes = append(changes, "profession: "+old.Profession+" → "+m.Profession)
	}
	if old.SquadType != m.SquadType && m.SquadType != "" {
		changes = append(changes, "squad: "+old.SquadType+" → "+m.SquadType)
	}
	if old.Eligible != m.Eligible {
		eligStr := func(b bool) string {
			if b {
				return "eligible"
			}
			return "ineligible"
		}
		changes = append(changes, eligStr(old.Eligible)+" → "+eligStr(m.Eligible))
	}
	oldPub := 0
	if old.LastRankPublicID.Valid {
		oldPub = int(old.LastRankPublicID.Int64)
	}
	newPub := 0
	if m.LastRankPublicID != nil {
		newPub = *m.LastRankPublicID
	}
	if oldPub != newPub {
		changes = append(changes, "LastRank ID: "+strconv.Itoa(oldPub)+" → "+strconv.Itoa(newPub))
	}
	if old.JoinedAt != m.JoinedAt {
		oldSince, newSince := old.JoinedAt, m.JoinedAt
		if oldSince == "" {
			oldSince = "(none)"
		}
		if newSince == "" {
			newSince = "(none)"
		}
		changes = append(changes, "member since: "+oldSince+" → "+newSince)
	}

	// Handle Power History
	if m.Power != nil {
		var currentPower int64 = -1
		_ = tx.QueryRow(`SELECT power FROM power_history WHERE member_id = ? ORDER BY recorded_at DESC LIMIT 1`, id).Scan(&currentPower)

		if currentPower != *m.Power {
			_, insertErr := tx.Exec(`INSERT INTO power_history (member_id, power, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, id, *m.Power)
			if insertErr != nil {
				slog.Warn("Failed to log power history", "member_id", id, "error", insertErr)
			}
		}
	}

	// Handle Squad Power History
	if m.SquadPower != nil {
		var currentSquadPower int64 = -1
		_ = tx.QueryRow(`SELECT power FROM squad_power_history WHERE member_id = ? ORDER BY recorded_at DESC LIMIT 1`, id).Scan(&currentSquadPower)
		if currentSquadPower != *m.SquadPower {
			tx.Exec(`INSERT INTO squad_power_history (member_id, power, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, id, *m.SquadPower)
		}
	}

	// Handle Hero Power History
	if m.HeroPower != nil && *m.HeroPower > 0 {
		var currentHeroPower int64 = -1
		_ = tx.QueryRow(`SELECT power FROM hero_power_history WHERE member_id = ? ORDER BY recorded_at DESC LIMIT 1`, id).Scan(&currentHeroPower)
		if currentHeroPower != *m.HeroPower {
			tx.Exec(`INSERT INTO hero_power_history (member_id, power, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, id, *m.HeroPower)
		}
	}

	// Handle Kill Count History
	if m.CurrentKills != nil && *m.CurrentKills > 0 {
		var currentKills int64 = -1
		_ = tx.QueryRow(`SELECT kills FROM kill_history WHERE member_id = ? ORDER BY recorded_at DESC LIMIT 1`, id).Scan(&currentKills)
		if currentKills != *m.CurrentKills {
			tx.Exec(`INSERT INTO kill_history (member_id, kills, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, id, *m.CurrentKills)
		}
	}

	// HQ level + profession level history (history-only fields; dedup against latest).
	if m.Level > 0 && m.Level != oldHQ {
		tx.Exec(`INSERT INTO hq_level_history (member_id, hq_level, source) VALUES (?, ?, 'manual')`, id, m.Level)
	}
	if m.ProfessionLevel != nil && *m.ProfessionLevel > 0 && (!hadProfLevel || *m.ProfessionLevel != oldProfLevel) {
		tx.Exec(`INSERT INTO profession_level_history (member_id, profession_level, source) VALUES (?, ?, 'manual')`, id, *m.ProfessionLevel)
	}

	if err := tx.Commit(); err != nil {
		slog.Error("updateMember: commit failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Activity log after commit (logActivity uses its own connection — calling it
	// while the tx held the write lock would deadlock SQLite's single writer).
	if len(changes) > 0 {
		user := getAuthUser(r)
		logActivity(user.ID, user.Username, "updated", "member", m.Name, false, strings.Join(changes, "; "))
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

	var memberName string
	db.QueryRow("SELECT name FROM members WHERE id = ?", id).Scan(&memberName)

	if _, err = db.Exec("DELETE FROM users WHERE member_id = ?", id); err != nil {
		slog.Error("Failed to delete linked user for member", "member_id", id, "error", err)
	}

	if _, err = db.Exec("DELETE FROM power_history WHERE member_id = ?", id); err != nil {
		slog.Error("Failed to delete power history for member", "member_id", id, "error", err)
	}

	if _, err = db.Exec("DELETE FROM squad_power_history WHERE member_id = ?", id); err != nil {
		slog.Error("Failed to delete squad power history for member", "member_id", id, "error", err)
	}

	if _, err = db.Exec("DELETE FROM hero_power_history WHERE member_id = ?", id); err != nil {
		slog.Error("Failed to delete hero power history for member", "member_id", id, "error", err)
	}

	if _, err = db.Exec("DELETE FROM kill_history WHERE member_id = ?", id); err != nil {
		slog.Error("Failed to delete kill history for member", "member_id", id, "error", err)
	}

	if _, err = db.Exec("DELETE FROM members WHERE id = ?", id); err != nil {
		slog.Error("Failed to delete member", "member_id", id, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "deleted", "member", memberName, false)

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

	var memberName string
	db.QueryRow("SELECT name FROM members WHERE id = ?", id).Scan(&memberName)

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

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "archived", "member", memberName, false)

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

	var memberName string
	db.QueryRow("SELECT name FROM members WHERE id = ?", id).Scan(&memberName)

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

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "unarchived", "member", memberName, false)

	w.WriteHeader(http.StatusNoContent)
}

func updateFormerMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid member ID", http.StatusBadRequest)
		return
	}

	var req struct {
		Name        string `json:"name"`
		LeaveReason string `json:"leave_reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	var oldName, oldLeaveReason string
	err = db.QueryRow("SELECT name, COALESCE(leave_reason, '') FROM members WHERE id = ? AND rank = 'EX'", id).Scan(&oldName, &oldLeaveReason)
	if err != nil {
		http.Error(w, "Former member not found", http.StatusNotFound)
		return
	}

	_, err = db.Exec("UPDATE members SET name = ?, leave_reason = ? WHERE id = ? AND rank = 'EX'", req.Name, req.LeaveReason, id)
	if err != nil {
		slog.Error("Failed to update former member", "member_id", id, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if oldName != req.Name {
		_, aliasErr := db.Exec("INSERT OR IGNORE INTO member_aliases (member_id, user_id, category, alias) VALUES (?, NULL, 'global', ?)", id, oldName)
		if aliasErr != nil {
			slog.Warn("Failed to auto-create global alias for former member name change", "member_id", id, "error", aliasErr)
		}
	}

	var changes []string
	if oldName != req.Name {
		changes = append(changes, "name: "+oldName+" → "+req.Name)
	}
	if oldLeaveReason != req.LeaveReason {
		changes = append(changes, "leave reason: "+oldLeaveReason+" → "+req.LeaveReason)
	}
	if len(changes) > 0 {
		user := getAuthUser(r)
		logActivity(user.ID, user.Username, "updated", "member", req.Name, false, strings.Join(changes, "; "))
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
		} else if strings.Contains(lowerCol, "join") || strings.Contains(lowerCol, "since") {
			headerMap["joined"] = i
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
	userID := getAuthUser(r).ID

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

		// Optional join-date column accepts either a YYYY-MM-DD date or a plain
		// "days ago" integer (matching the in-game "joined N days ago" display).
		// Anything else is ignored and falls back to today's game date at commit.
		if joinIdx, hasJoin := headerMap["joined"]; hasJoin && len(record) > joinIdx {
			js := strings.TrimSpace(record[joinIdx])
			if js != "" {
				if _, perr := parseDate(js); perr == nil {
					detected.JoinedAt = js
				} else if n, nerr := strconv.Atoi(js); nerr == nil && n >= 0 {
					detected.JoinedAt = gameNow().AddDate(0, 0, -n).Format("2006-01-02")
				}
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
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	result := ConfirmResult{}
	src := provenanceSource(request.Source)

	for _, rename := range request.Renames {
		db.Exec("UPDATE members SET name = ? WHERE name = ?", rename.NewName, rename.OldName)
	}

	for _, member := range request.Members {
		var existingID int
		var existingRank string
		err := db.QueryRow("SELECT id, rank FROM members WHERE name = ?", member.Name).Scan(&existingID, &existingRank)

		if err == sql.ErrNoRows {
			// New member discovered via roster import. Use the per-member join date
			// (from the CSV column or the preview UI) when valid; else today's game date.
			joinDate := gameDate()
			if member.JoinedAt != "" {
				if _, perr := parseDate(member.JoinedAt); perr == nil {
					joinDate = member.JoinedAt
				}
			}
			res, err := db.Exec("INSERT INTO members (name, rank, joined_at) VALUES (?, ?, ?)", member.Name, member.Rank, joinDate)
			if err == nil {
				id, _ := res.LastInsertId()
				existingID = int(id)
				result.Added++
			}
		} else if err == nil {
			db.Exec("UPDATE members SET rank = ? WHERE id = ?", member.Rank, existingID)

			if existingRank != member.Rank {
				result.Updated++
			} else {
				result.Unchanged++
			}
		}

		// HQ level is history-only; record it (deduped) with the import's provenance.
		if member.Level > 0 && existingID > 0 {
			recordHistoryIfChanged(db, "hq_level_history", "hq_level", existingID, member.Level, src)
		}

		if member.Power > 0 && existingID > 0 {
			var currentPower int64 = -1
			db.QueryRow("SELECT power FROM power_history WHERE member_id = ? ORDER BY recorded_at DESC LIMIT 1", existingID).Scan(&currentPower)
			if currentPower != member.Power {
				db.Exec("INSERT INTO power_history (member_id, power, recorded_at, source) VALUES (?, ?, CURRENT_TIMESTAMP, ?)", existingID, member.Power, src)
			}
		}

		if member.SquadPower > 0 && existingID > 0 {
			var currentSquadPower int64 = -1
			db.QueryRow("SELECT power FROM squad_power_history WHERE member_id = ? ORDER BY recorded_at DESC LIMIT 1", existingID).Scan(&currentSquadPower)
			if currentSquadPower != member.SquadPower {
				db.Exec("INSERT INTO squad_power_history (member_id, power, recorded_at, source) VALUES (?, ?, CURRENT_TIMESTAMP, ?)", existingID, member.SquadPower, src)
			}
		}
	}

	if len(request.RemoveMemberIDs) > 0 {
		for _, id := range request.RemoveMemberIDs {
			db.Exec("DELETE FROM users WHERE member_id = ?", id)
			db.Exec("DELETE FROM power_history WHERE member_id = ?", id)
			db.Exec("DELETE FROM squad_power_history WHERE member_id = ?", id)
			db.Exec("DELETE FROM hero_power_history WHERE member_id = ?", id)
			db.Exec("DELETE FROM kill_history WHERE member_id = ?", id)
			db.Exec("DELETE FROM members WHERE id = ?", id)
			result.Removed++
		}
	}

	total := result.Added + result.Updated + result.Removed
	if total > 0 {
		user := getAuthUser(r)
		logActivity(user.ID, user.Username, "imported", "member", strconv.Itoa(total)+" member updates", false)
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
		slog.Error("getMemberStats: query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	stats := []MemberStats{}
	for rows.Next() {
		var s MemberStats
		var lastDate sql.NullString
		if err := rows.Scan(&s.ID, &s.Name, &s.Rank, &s.ConductorCount, &lastDate, &s.BackupCount, &s.BackupUsedCount, &s.ConductorNoShowCount); err != nil {
			slog.Error("getMemberStats: scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
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
	user := getAuthUser(r)
	ok := user.MemberID != nil
	if !ok {
		http.Error(w, "No linked member profile found", http.StatusNotFound)
		return
	}
	memberID := *user.MemberID

	var m Member

	// Added m.eligible and updated to fetch the latest squad_power history
	err := db.QueryRow(`
		SELECT
			m.id, m.name, m.rank, m.eligible,
			COALESCE((SELECT hq_level FROM hq_level_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), 0) as level,
			COALESCE(m.joined_at, '') as joined_at,
			(SELECT power FROM power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1) as power,
			COALESCE((SELECT recorded_at FROM power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), '') as power_updated_at,
			COALESCE(m.squad_type, ''),
			(SELECT power FROM squad_power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1) as squad_power,
			COALESCE((SELECT recorded_at FROM squad_power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), '') as squad_power_updated_at,
			COALESCE(m.troop_level, 0),
			COALESCE(m.profession, ''),
			(SELECT profession_level FROM profession_level_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1) as profession_level,
			(SELECT power FROM hero_power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1) as hero_power,
			COALESCE((SELECT recorded_at FROM hero_power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), '') as hero_power_updated_at,
			COALESCE((SELECT kills FROM kill_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), 0) as current_kills,
			COALESCE((SELECT recorded_at FROM kill_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), '') as kills_updated_at,
			COALESCE((
				SELECT GROUP_CONCAT(skill_key)
				FROM (SELECT skill_key FROM member_skills WHERE member_id = m.id ORDER BY skill_key)
			), '') as skills
		FROM members m WHERE m.id = ?`, memberID).
		Scan(&m.ID, &m.Name, &m.Rank, &m.Eligible, &m.Level, &m.JoinedAt, &m.Power, &m.PowerUpdatedAt, &m.SquadType, &m.SquadPower, &m.SquadPowerUpdatedAt, &m.TroopLevel, &m.Profession, &m.ProfessionLevel, &m.HeroPower, &m.HeroPowerUpdatedAt, &m.CurrentKills, &m.KillsUpdatedAt, &m.Skills)

	if err != nil {
		slog.Error("Profile fetch error", "error", err)
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(m)
}

// updateMyProfile allows a user to self-serve update their own in-game stats
func updateMyProfile(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	if user.MemberID == nil {
		http.Error(w, "Forbidden: No linked member profile", http.StatusForbidden)
		return
	}
	memberID := *user.MemberID

	var req struct {
		Name         string `json:"name"`
		Level        int    `json:"level"`
		Power        int64  `json:"power"`
		TroopLevel   int    `json:"troop_level"`
		SquadType    string `json:"squad_type"`
		SquadPower   int64  `json:"squad_power"`
		HeroPower    int64  `json:"hero_power"`
		CurrentKills int64  `json:"current_kills"`
		Profession   string `json:"profession"`
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

	// Fetch current values for diff. HQ level is history-only (no members column).
	var oldName, oldSquadType, oldProfession string
	var oldTroopLevel int
	db.QueryRow(`SELECT name, troop_level, COALESCE(squad_type,''), COALESCE(profession,'') FROM members WHERE id = ?`, memberID).
		Scan(&oldName, &oldTroopLevel, &oldSquadType, &oldProfession)
	oldLevel, _ := latestHistoryValue(db, "hq_level_history", "hq_level", memberID)

	// Execute database updates...
	_, err := db.Exec(`
		UPDATE members
		SET name = ?, troop_level = ?, squad_type = ?, profession = ?
		WHERE id = ?`,
		req.Name, req.TroopLevel, req.SquadType, req.Profession, memberID)

	if err != nil {
		slog.Error("Profile update error", "error", err)
		http.Error(w, "Failed to update profile", http.StatusInternalServerError)
		return
	}

	if req.Power > 0 {
		db.Exec(`INSERT INTO power_history (member_id, power) VALUES (?, ?)`, memberID, req.Power)
	}
	if req.SquadPower > 0 {
		db.Exec(`INSERT INTO squad_power_history (member_id, power) VALUES (?, ?)`, memberID, req.SquadPower)
	}
	if req.HeroPower > 0 {
		db.Exec(`INSERT INTO hero_power_history (member_id, power) VALUES (?, ?)`, memberID, req.HeroPower)
	}
	if req.CurrentKills > 0 {
		db.Exec(`INSERT INTO kill_history (member_id, kills) VALUES (?, ?)`, memberID, req.CurrentKills)
	}
	// HQ level history (history-only field; dedup against the latest recorded value).
	if req.Level > 0 && req.Level != oldLevel {
		db.Exec(`INSERT INTO hq_level_history (member_id, hq_level, source) VALUES (?, ?, 'manual')`, memberID, req.Level)
	}

	var profileChanges []string
	if oldName != req.Name && req.Name != "" {
		profileChanges = append(profileChanges, "name: "+oldName+" → "+req.Name)
	}
	if oldLevel != req.Level {
		profileChanges = append(profileChanges, "HQ level: "+strconv.Itoa(oldLevel)+" → "+strconv.Itoa(req.Level))
	}
	if oldTroopLevel != req.TroopLevel {
		profileChanges = append(profileChanges, "troop level: "+strconv.Itoa(oldTroopLevel)+" → "+strconv.Itoa(req.TroopLevel))
	}
	if oldSquadType != req.SquadType && req.SquadType != "" {
		profileChanges = append(profileChanges, "squad: "+oldSquadType+" → "+req.SquadType)
	}
	if oldProfession != req.Profession && req.Profession != "" {
		profileChanges = append(profileChanges, "profession: "+oldProfession+" → "+req.Profession)
	}
	if req.Power > 0 {
		profileChanges = append(profileChanges, "power updated")
	}
	if len(profileChanges) > 0 {
		logActivity(user.ID, user.Username, "updated", "member", req.Name, false, strings.Join(profileChanges, "; "))
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
	userID := getAuthUser(r).ID

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
	user := getAuthUser(r)
	userID := user.ID
	isAdmin := user.IsAdmin

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

	var memberName string
	db.QueryRow("SELECT name FROM members WHERE id = ?", memberID).Scan(&memberName)
	category := "personal"
	if req.IsGlobal {
		category = "global"
	}
	logActivity(userID, user.Username, "created", "alias", req.Alias, false, memberName+" ("+category+")")

	w.WriteHeader(http.StatusCreated)
}

func deleteMemberAlias(w http.ResponseWriter, r *http.Request) {
	aliasID := mux.Vars(r)["id"]
	user := getAuthUser(r)
	userID := user.ID
	isAdmin := user.IsAdmin

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

	var category, aliasText, memberName string
	var ownerID *int
	err := db.QueryRow(`
		SELECT ma.category, ma.user_id, ma.alias, m.name
		FROM member_aliases ma
		JOIN members m ON m.id = ma.member_id
		WHERE ma.id = ?`, aliasID).Scan(&category, &ownerID, &aliasText, &memberName)
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

	logActivity(userID, user.Username, "deleted", "alias", aliasText, false, memberName+" ("+category+")")

	w.WriteHeader(http.StatusOK)
}
