package main

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

// replaceMemberSkills atomically replaces all skills for a member within a transaction.
// Deduplicates input keys before inserting to avoid UNIQUE constraint violations.
func replaceMemberSkills(tx *sql.Tx, memberID int, skills []string, recordedBy *int) error {
	seen := make(map[string]bool)
	deduped := make([]string, 0, len(skills))
	for _, k := range skills {
		if !seen[k] {
			seen[k] = true
			deduped = append(deduped, k)
		}
	}

	if _, err := tx.Exec("DELETE FROM member_skills WHERE member_id = ?", memberID); err != nil {
		return err
	}
	for _, key := range deduped {
		if _, err := tx.Exec(
			"INSERT INTO member_skills (member_id, skill_key, recorded_by) VALUES (?, ?, ?)",
			memberID, key, recordedBy,
		); err != nil {
			return err
		}
	}
	return nil
}

// getSkillRegistry returns the canonical list of skill keys and labels.
// GET /api/skills
func getSkillRegistry(w http.ResponseWriter, r *http.Request) {
	type skillEntry struct {
		Key   string `json:"key"`
		Label string `json:"label"`
	}
	result := make([]skillEntry, 0, len(ValidSkillKeys))
	for _, k := range ValidSkillKeys {
		result = append(result, skillEntry{Key: k, Label: SkillLabels[k]})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// getMembersWithSkills returns all active (non-EX) members with their comma-separated skills.
// GET /api/members/skills
func getMembersWithSkills(w http.ResponseWriter, r *http.Request) {
	type MemberSkillRow struct {
		ID     int    `json:"id"`
		Name   string `json:"name"`
		Rank   string `json:"rank"`
		Skills string `json:"skills"`
	}

	rows, err := db.Query(`
		SELECT m.id, m.name, m.rank,
		       COALESCE((
		           SELECT GROUP_CONCAT(skill_key)
		           FROM (SELECT skill_key FROM member_skills WHERE member_id = m.id ORDER BY skill_key)
		       ), '') AS skills
		FROM members m
		WHERE m.rank != 'EX'
		ORDER BY m.name
	`)
	if err != nil {
		slog.Error("getMembersWithSkills query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	members := []MemberSkillRow{}
	for rows.Next() {
		var m MemberSkillRow
		if err := rows.Scan(&m.ID, &m.Name, &m.Rank, &m.Skills); err != nil {
			slog.Error("getMembersWithSkills scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		members = append(members, m)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(members)
}

// updateMemberSkills replaces the skill set for a specific member (officer use).
// PUT /api/members/{id}/skills  — requires manage_members
func updateMemberSkills(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid member ID", http.StatusBadRequest)
		return
	}

	var req struct {
		Skills []string `json:"skills"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate all keys
	validSet := make(map[string]bool, len(ValidSkillKeys))
	for _, k := range ValidSkillKeys {
		validSet[k] = true
	}
	for _, k := range req.Skills {
		if !validSet[k] {
			http.Error(w, "Unknown skill key: "+k, http.StatusBadRequest)
			return
		}
	}

	// Fetch member name for activity log
	var memberName string
	if err := db.QueryRow("SELECT name FROM members WHERE id = ?", id).Scan(&memberName); err != nil {
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}

	// Fetch existing skills for diff
	var oldSkills string
	db.QueryRow(`
		SELECT COALESCE(GROUP_CONCAT(skill_key), '')
		FROM (SELECT skill_key FROM member_skills WHERE member_id = ? ORDER BY skill_key)
	`, id).Scan(&oldSkills)

	user := getAuthUser(r)
	recordedBy := user.ID

	tx, err := db.Begin()
	if err != nil {
		slog.Error("updateMemberSkills begin tx", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if err := replaceMemberSkills(tx, id, req.Skills, &recordedBy); err != nil {
		slog.Error("replaceMemberSkills failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		slog.Error("updateMemberSkills commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	newSkills := strings.Join(req.Skills, ",")
	logActivity(user.ID, user.Username, "updated", "member", memberName, false, "skills: "+oldSkills+" → "+newSkills)

	w.WriteHeader(http.StatusOK)
}

// updateProfileSkills replaces the skill set for the calling user's linked member (self-service).
// PUT /api/profile/me/skills
func updateProfileSkills(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	if user.MemberID == nil {
		http.Error(w, "Forbidden: No linked member profile", http.StatusForbidden)
		return
	}
	memberID := *user.MemberID

	var req struct {
		Skills []string `json:"skills"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate all keys
	validSet := make(map[string]bool, len(ValidSkillKeys))
	for _, k := range ValidSkillKeys {
		validSet[k] = true
	}
	for _, k := range req.Skills {
		if !validSet[k] {
			http.Error(w, "Unknown skill key: "+k, http.StatusBadRequest)
			return
		}
	}

	// Fetch existing skills for diff
	var oldSkills string
	db.QueryRow(`
		SELECT COALESCE(GROUP_CONCAT(skill_key), '')
		FROM (SELECT skill_key FROM member_skills WHERE member_id = ? ORDER BY skill_key)
	`, memberID).Scan(&oldSkills)

	recordedBy := user.ID

	tx, err := db.Begin()
	if err != nil {
		slog.Error("updateProfileSkills begin tx", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if err := replaceMemberSkills(tx, memberID, req.Skills, &recordedBy); err != nil {
		slog.Error("replaceMemberSkills failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		slog.Error("updateProfileSkills commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	newSkills := strings.Join(req.Skills, ",")
	logActivity(user.ID, user.Username, "updated", "member", user.Username, false, "skills: "+oldSkills+" → "+newSkills)

	w.WriteHeader(http.StatusOK)
}

