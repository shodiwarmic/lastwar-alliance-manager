package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
)

func listSchedules(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT id, name, duration_days, is_active, schedule_data, created_by, created_at, updated_at
		FROM schedules
		ORDER BY is_active DESC, updated_at DESC
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	schedules := []Schedule{}
	for rows.Next() {
		var s Schedule
		var isActive int
		if err := rows.Scan(&s.ID, &s.Name, &s.DurationDays, &isActive, &s.ScheduleData, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.IsActive = isActive == 1
		schedules = append(schedules, s)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(schedules)
}

func createSchedule(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, ok := session.Values["user_id"].(int)
	if !ok || userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name         string          `json:"name"`
		DurationDays int             `json:"duration_days"`
		ScheduleData json.RawMessage `json:"schedule_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.DurationDays != 7 && req.DurationDays != 14 {
		http.Error(w, "duration_days must be 7 or 14", http.StatusBadRequest)
		return
	}
	if !json.Valid(req.ScheduleData) {
		http.Error(w, "schedule_data must be valid JSON", http.StatusBadRequest)
		return
	}

	result, err := db.Exec(
		`INSERT INTO schedules (name, duration_days, schedule_data, created_by) VALUES (?, ?, ?, ?)`,
		req.Name, req.DurationDays, string(req.ScheduleData), userID,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	var s Schedule
	var isActive int
	db.QueryRow(`SELECT id, name, duration_days, is_active, schedule_data, created_by, created_at, updated_at FROM schedules WHERE id = ?`, id).
		Scan(&s.ID, &s.Name, &s.DurationDays, &isActive, &s.ScheduleData, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	s.IsActive = isActive == 1

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(s)
}

func updateSchedule(w http.ResponseWriter, r *http.Request) {
	idStr := mux.Vars(r)["id"]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var req struct {
		Name         string          `json:"name"`
		DurationDays int             `json:"duration_days"`
		ScheduleData json.RawMessage `json:"schedule_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.DurationDays != 7 && req.DurationDays != 14 {
		http.Error(w, "duration_days must be 7 or 14", http.StatusBadRequest)
		return
	}
	if !json.Valid(req.ScheduleData) {
		http.Error(w, "schedule_data must be valid JSON", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`UPDATE schedules SET name=?, duration_days=?, schedule_data=?, updated_at=? WHERE id=?`,
		req.Name, req.DurationDays, string(req.ScheduleData), now, id,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		http.Error(w, "schedule not found", http.StatusNotFound)
		return
	}

	var s Schedule
	var isActive int
	db.QueryRow(`SELECT id, name, duration_days, is_active, schedule_data, created_by, created_at, updated_at FROM schedules WHERE id = ?`, id).
		Scan(&s.ID, &s.Name, &s.DurationDays, &isActive, &s.ScheduleData, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	s.IsActive = isActive == 1

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}

func deleteSchedule(w http.ResponseWriter, r *http.Request) {
	idStr := mux.Vars(r)["id"]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var isActive int
	if err := db.QueryRow(`SELECT is_active FROM schedules WHERE id = ?`, id).Scan(&isActive); err != nil {
		http.Error(w, "schedule not found", http.StatusNotFound)
		return
	}
	if isActive == 1 {
		http.Error(w, "cannot delete the active schedule", http.StatusConflict)
		return
	}

	db.Exec(`DELETE FROM schedules WHERE id = ?`, id)
	w.WriteHeader(http.StatusNoContent)
}

func setActiveSchedule(w http.ResponseWriter, r *http.Request) {
	idStr := mux.Vars(r)["id"]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if _, err := tx.Exec(`UPDATE schedules SET is_active = 0`); err != nil {
		tx.Rollback()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res, err := tx.Exec(`UPDATE schedules SET is_active = 1 WHERE id = ?`, id)
	if err != nil {
		tx.Rollback()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		tx.Rollback()
		http.Error(w, "schedule not found", http.StatusNotFound)
		return
	}
	tx.Commit()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "schedule activated"})
}

