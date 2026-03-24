package main

import (
	"encoding/json"
	"net/http"
	"time"
)

const scheduleID = 1

// getSchedule returns the single alliance schedule (id=1), creating it with
// defaults if it doesn't exist yet.
func getSchedule(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)

	// Ensure the singleton row exists — INSERT OR IGNORE is a no-op if id=1 is already there.
	if userID != 0 {
		db.Exec(
			`INSERT OR IGNORE INTO schedules (id, name, duration_days, is_active, schedule_data, created_by) VALUES (?, ?, ?, 1, ?, ?)`,
			scheduleID, "Alliance Schedule", 14, `{}`, userID,
		)
	}

	var s Schedule
	var isActive int
	// COALESCE guards against any legacy NULL schedule_data rows.
	err := db.QueryRow(`
		SELECT id, name, duration_days, is_active, COALESCE(schedule_data, '{}'), created_by, created_at, updated_at
		FROM schedules WHERE id = ?`, scheduleID).
		Scan(&s.ID, &s.Name, &s.DurationDays, &isActive, &s.ScheduleData, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.IsActive = isActive == 1

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}

// putSchedule upserts the single alliance schedule (id=1).
func putSchedule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string          `json:"name"`
		DurationDays int             `json:"duration_days"`
		ScheduleData json.RawMessage `json:"schedule_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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

	name := req.Name
	if name == "" {
		name = "Alliance Schedule"
	}

	now := time.Now().UTC().Format(time.RFC3339)
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)

	_, err := db.Exec(`
		INSERT INTO schedules (id, name, duration_days, is_active, schedule_data, created_by, updated_at)
		VALUES (?, ?, ?, 1, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		    name=excluded.name,
		    duration_days=excluded.duration_days,
		    schedule_data=excluded.schedule_data,
		    updated_at=excluded.updated_at`,
		scheduleID, name, req.DurationDays, string(req.ScheduleData), userID, now,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var s Schedule
	var isActive int
	db.QueryRow(`
		SELECT id, name, duration_days, is_active, schedule_data, created_by, created_at, updated_at
		FROM schedules WHERE id = ?`, scheduleID).
		Scan(&s.ID, &s.Name, &s.DurationDays, &isActive, &s.ScheduleData, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	s.IsActive = isActive == 1

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}
