package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

func getAwards(w http.ResponseWriter, r *http.Request) {
	weekDate := r.URL.Query().Get("week")

	var query string
	var rows *sql.Rows
	var err error

	if weekDate != "" {
		query = `
			SELECT a.id, a.week_date, a.award_type, a.rank, a.member_id, m.name, a.created_at
			FROM awards a
			JOIN members m ON a.member_id = m.id
			WHERE a.week_date = ?
			ORDER BY a.award_type, a.rank
		`
		rows, err = db.Query(query, weekDate)
	} else {
		query = `
			SELECT a.id, a.week_date, a.award_type, a.rank, a.member_id, m.name, a.created_at
			FROM awards a
			JOIN members m ON a.member_id = m.id
			ORDER BY a.week_date DESC, a.award_type, a.rank
		`
		rows, err = db.Query(query)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	awards := []Award{}
	for rows.Next() {
		var a Award
		if err := rows.Scan(&a.ID, &a.WeekDate, &a.AwardType, &a.Rank, &a.MemberID, &a.MemberName, &a.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		awards = append(awards, a)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(awards)
}

func saveAwards(w http.ResponseWriter, r *http.Request) {
	var data struct {
		WeekDate string `json:"week_date"`
		Awards   []struct {
			AwardType string `json:"award_type"`
			Rank      int    `json:"rank"`
			MemberID  int    `json:"member_id"`
		} `json:"awards"`
	}

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec("DELETE FROM awards WHERE week_date = ?", data.WeekDate)
	if err != nil {
		tx.Rollback()
		http.Error(w, "Failed to clear existing awards", http.StatusInternalServerError)
		return
	}

	for _, award := range data.Awards {
		if award.MemberID > 0 {
			_, err = tx.Exec(
				"INSERT INTO awards (week_date, award_type, rank, member_id) VALUES (?, ?, ?, ?)",
				data.WeekDate, award.AwardType, award.Rank, award.MemberID)
			if err != nil {
				tx.Rollback()
				http.Error(w, "Failed to save award", http.StatusInternalServerError)
				return
			}
		}
	}

	err = tx.Commit()
	if err != nil {
		http.Error(w, "Failed to save changes", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Awards saved successfully"})
}

func deleteWeekAwards(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	weekDate := vars["week"]

	_, err := db.Exec("DELETE FROM awards WHERE week_date = ?", weekDate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func getAwardTypes(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT id, name, active, sort_order, created_at
		FROM award_types
		ORDER BY sort_order, name
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	awardTypes := []AwardType{}
	for rows.Next() {
		var at AwardType
		if err := rows.Scan(&at.ID, &at.Name, &at.Active, &at.SortOrder, &at.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		awardTypes = append(awardTypes, at)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(awardTypes)
}

func createAwardType(w http.ResponseWriter, r *http.Request) {
	var at AwardType
	if err := json.NewDecoder(r.Body).Decode(&at); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(at.Name) == "" {
		http.Error(w, "Award type name is required", http.StatusBadRequest)
		return
	}

	var existingID int
	err := db.QueryRow("SELECT id FROM award_types WHERE name = ?", at.Name).Scan(&existingID)
	if err == nil {
		http.Error(w, "Award type already exists", http.StatusConflict)
		return
	}

	var maxOrder int
	err = db.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM award_types").Scan(&maxOrder)
	if err != nil {
		maxOrder = -1
	}

	result, err := db.Exec(
		"INSERT INTO award_types (name, active, sort_order) VALUES (?, ?, ?)",
		at.Name, true, maxOrder+1)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	at.ID = int(id)
	at.Active = true
	at.SortOrder = maxOrder + 1
	at.CreatedAt = time.Now().Format(time.RFC3339)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(at)
}

func updateAwardType(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var at AwardType
	if err := json.NewDecoder(r.Body).Decode(&at); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err = db.Exec(
		"UPDATE award_types SET active = ?, name = ? WHERE id = ?",
		at.Active, at.Name, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Award type updated"})
}

func deleteAwardType(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	force := r.URL.Query().Get("force") == "true"

	var name string
	err = db.QueryRow("SELECT name FROM award_types WHERE id = ?", id).Scan(&name)
	if err != nil {
		http.Error(w, "Award type not found", http.StatusNotFound)
		return
	}

	if force {
		_, err = db.Exec("DELETE FROM awards WHERE award_type = ?", name)
		if err != nil {
			http.Error(w, "Failed to delete related awards", http.StatusInternalServerError)
			return
		}
	} else {
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM awards WHERE award_type = ?", name).Scan(&count)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if count > 0 {
			http.Error(w, "Cannot delete award type that is used in awards. Use force=true to delete anyway.", http.StatusBadRequest)
			return
		}
	}

	_, err = db.Exec("DELETE FROM award_types WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func getRecommendations(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT 
			rec.id, 
			rec.member_id, 
			m.name, 
			m.rank,
			u.username,
			rec.recommended_by_id,
			COALESCE(rec.notes, ''),
			rec.created_at,
			CASE 
				WHEN EXISTS (
					SELECT 1 FROM train_schedules ts
					WHERE (ts.conductor_id = rec.member_id OR (ts.backup_id = rec.member_id AND ts.conductor_showed_up = 0))
					AND ts.date >= date(rec.created_at)
				) THEN 1
				ELSE 0
			END as expired
		FROM recommendations rec
		JOIN members m ON rec.member_id = m.id
		JOIN users u ON rec.recommended_by_id = u.id
		ORDER BY rec.created_at DESC
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	recommendations := []Recommendation{}
	for rows.Next() {
		var rec Recommendation
		if err := rows.Scan(&rec.ID, &rec.MemberID, &rec.MemberName, &rec.MemberRank,
			&rec.RecommendedBy, &rec.RecommendedByID, &rec.Notes, &rec.CreatedAt, &rec.Expired); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		recommendations = append(recommendations, rec)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(recommendations)
}

func createRecommendation(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)

	var input struct {
		MemberID int    `json:"member_id"`
		Notes    string `json:"notes"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if input.MemberID == 0 {
		http.Error(w, "Member ID is required", http.StatusBadRequest)
		return
	}

	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM members WHERE id = ?)", input.MemberID).Scan(&exists)
	if err != nil || !exists {
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}

	result, err := db.Exec(
		"INSERT INTO recommendations (member_id, recommended_by_id, notes) VALUES (?, ?, ?)",
		input.MemberID, userID, input.Notes,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()

	var rec Recommendation
	err = db.QueryRow(`
		SELECT 
			rec.id, 
			rec.member_id, 
			m.name, 
			m.rank,
			u.username,
			rec.recommended_by_id,
			COALESCE(rec.notes, ''),
			rec.created_at,
			CASE 
				WHEN EXISTS (
					SELECT 1 FROM train_schedules ts
					WHERE (ts.conductor_id = rec.member_id OR (ts.backup_id = rec.member_id AND ts.conductor_showed_up = 0))
					AND ts.date >= date(rec.created_at)
				) THEN 1
				ELSE 0
			END as expired
		FROM recommendations rec
		JOIN members m ON rec.member_id = m.id
		JOIN users u ON rec.recommended_by_id = u.id
		WHERE rec.id = ?
	`, id).Scan(&rec.ID, &rec.MemberID, &rec.MemberName, &rec.MemberRank,
		&rec.RecommendedBy, &rec.RecommendedByID, &rec.Notes, &rec.CreatedAt, &rec.Expired)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(rec)
}

func deleteRecommendation(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	isAdmin, _ := session.Values["is_admin"].(bool)

	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var recommendedByID int
	err = db.QueryRow("SELECT recommended_by_id FROM recommendations WHERE id = ?", id).Scan(&recommendedByID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Recommendation not found", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	if recommendedByID != userID && !isAdmin {
		http.Error(w, "You can only delete your own recommendations", http.StatusForbidden)
		return
	}

	_, err = db.Exec("DELETE FROM recommendations WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func getDynoRecommendations(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	isAdmin, _ := session.Values["is_admin"].(bool)

	var userRank string
	var canViewAnon bool

	if userID > 0 {
		err := db.QueryRow(`
			SELECT COALESCE(m.rank, ''), COALESCE(rp.view_anonymous_authors, 0)
			FROM users u
			LEFT JOIN members m ON u.member_id = m.id
			LEFT JOIN rank_permissions rp ON m.rank = rp.rank
			WHERE u.id = ?
		`, userID).Scan(&userRank, &canViewAnon)

		if err != nil && err != sql.ErrNoRows {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if isAdmin {
		canViewAnon = true
	}

	rows, err := db.Query(`
		SELECT 
			dr.id, dr.member_id, m.name, m.rank, dr.points, dr.notes,
			u.username, dr.created_by_id, dr.created_at,
			CASE WHEN datetime(dr.created_at, '+7 days') < datetime('now') THEN 1 ELSE 0 END as expired,
			dr.is_author_public, dr.min_view_rank
		FROM dyno_recommendations dr
		JOIN members m ON dr.member_id = m.id
		JOIN users u ON dr.created_by_id = u.id
		ORDER BY dr.created_at DESC
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	dynoRecs := []DynoRecommendation{}
	for rows.Next() {
		var dr DynoRecommendation
		if err := rows.Scan(&dr.ID, &dr.MemberID, &dr.MemberName, &dr.MemberRank,
			&dr.Points, &dr.Notes, &dr.CreatedBy, &dr.CreatedByID, &dr.CreatedAt, &dr.Expired,
			&dr.IsAuthorPublic, &dr.MinViewRank); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Targeted Visibility: Filter out if user's rank is below the minimum view rank,
		// UNLESS they are an admin, OR they are the original author.
		if !isAdmin && dr.MinViewRank != "" && userRank < dr.MinViewRank && dr.CreatedByID != userID {
			continue // Silently drop
		}

		// Semi-Anonymous & RBAC Zero-Trust Scrubbing
		if !dr.IsAuthorPublic && !canViewAnon && dr.CreatedByID != userID {
			dr.CreatedBy = "Anonymous"
			dr.CreatedByID = 0
		}

		dynoRecs = append(dynoRecs, dr)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dynoRecs)
}

func createDynoRecommendation(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)

	var input struct {
		MemberID       int    `json:"member_id"`
		Points         int    `json:"points"`
		Notes          string `json:"notes"`
		IsAuthorPublic bool   `json:"is_author_public"`
		MinViewRank    string `json:"min_view_rank"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if input.MemberID == 0 || input.Notes == "" {
		http.Error(w, "Member ID and Notes are required", http.StatusBadRequest)
		return
	}

	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM members WHERE id = ?)", input.MemberID).Scan(&exists)
	if err != nil || !exists {
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}

	result, err := db.Exec(
		"INSERT INTO dyno_recommendations (member_id, points, notes, created_by_id, is_author_public, min_view_rank) VALUES (?, ?, ?, ?, ?, ?)",
		input.MemberID, input.Points, input.Notes, userID, input.IsAuthorPublic, input.MinViewRank,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()

	var dr DynoRecommendation
	err = db.QueryRow(`
		SELECT 
			dr.id, dr.member_id, m.name, m.rank, dr.points, dr.notes,
			u.username, dr.created_by_id, dr.created_at,
			CASE WHEN datetime(dr.created_at, '+7 days') < datetime('now') THEN 1 ELSE 0 END as expired,
			dr.is_author_public, dr.min_view_rank
		FROM dyno_recommendations dr
		JOIN members m ON dr.member_id = m.id
		JOIN users u ON dr.created_by_id = u.id
		WHERE dr.id = ?
	`, id).Scan(&dr.ID, &dr.MemberID, &dr.MemberName, &dr.MemberRank,
		&dr.Points, &dr.Notes, &dr.CreatedBy, &dr.CreatedByID, &dr.CreatedAt, &dr.Expired,
		&dr.IsAuthorPublic, &dr.MinViewRank)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(dr)
}

func updateDynoRecommendation(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)

	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var input struct {
		MemberID       int    `json:"member_id"`
		Points         int    `json:"points"`
		Notes          string `json:"notes"`
		IsAuthorPublic bool   `json:"is_author_public"`
		MinViewRank    string `json:"min_view_rank"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Zero-Trust verification: Ensure the user actually owns this record
	var createdByID int
	err = db.QueryRow("SELECT created_by_id FROM dyno_recommendations WHERE id = ?", id).Scan(&createdByID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Not found", http.StatusNotFound)
		} else {
			http.Error(w, "Database error", http.StatusInternalServerError)
		}
		return
	}

	if createdByID != userID {
		http.Error(w, "You can only modify your own shoutouts.", http.StatusForbidden)
		return
	}

	_, err = db.Exec(`
		UPDATE dyno_recommendations 
		SET member_id = ?, points = ?, notes = ?, is_author_public = ?, min_view_rank = ? 
		WHERE id = ?`,
		input.MemberID, input.Points, input.Notes, input.IsAuthorPublic, input.MinViewRank, id,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func deleteDynoRecommendation(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	isAdmin, _ := session.Values["is_admin"].(bool)

	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// Fetch manage_dyno permission dynamically for non-admins
	var canManageDyno bool
	if !isAdmin {
		err := db.QueryRow(`
			SELECT COALESCE(rp.manage_dyno, 0)
			FROM users u
			LEFT JOIN members m ON u.member_id = m.id
			LEFT JOIN rank_permissions rp ON m.rank = rp.rank
			WHERE u.id = ?
		`, userID).Scan(&canManageDyno)
		if err != nil && err != sql.ErrNoRows {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	var createdByID int
	err = db.QueryRow("SELECT created_by_id FROM dyno_recommendations WHERE id = ?", id).Scan(&createdByID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Dyno recommendation not found", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Authorize if user is the creator, an admin, or possesses manage_dyno permissions
	if createdByID != userID && !isAdmin && !canManageDyno {
		http.Error(w, "You do not have permission to delete this shoutout", http.StatusForbidden)
		return
	}

	_, err = db.Exec("DELETE FROM dyno_recommendations WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
