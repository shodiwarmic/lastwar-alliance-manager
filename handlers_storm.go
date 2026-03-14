package main

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

func getStormAssignments(w http.ResponseWriter, r *http.Request) {
	taskForce := r.URL.Query().Get("task_force")
	if taskForce == "" {
		taskForce = "A"
	}

	rows, err := db.Query(`
		SELECT id, task_force, building_id, member_id, position
		FROM storm_assignments
		WHERE task_force = ?
		ORDER BY building_id, position
	`, taskForce)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	assignments := []StormAssignment{}
	for rows.Next() {
		var a StormAssignment
		if err := rows.Scan(&a.ID, &a.TaskForce, &a.BuildingID, &a.MemberID, &a.Position); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		assignments = append(assignments, a)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(assignments)
}

func saveStormAssignments(w http.ResponseWriter, r *http.Request) {
	var request struct {
		TaskForce   string `json:"task_force"`
		Assignments []struct {
			BuildingID string `json:"building_id"`
			MemberID   int    `json:"member_id"`
			Position   int    `json:"position"`
		} `json:"assignments"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if request.TaskForce != "A" && request.TaskForce != "B" {
		http.Error(w, "Invalid task force - must be A or B", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec("DELETE FROM storm_assignments WHERE task_force = ?", request.TaskForce)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, assignment := range request.Assignments {
		_, err = tx.Exec(`
			INSERT INTO storm_assignments (task_force, building_id, member_id, position)
			VALUES (?, ?, ?, ?)
		`, request.TaskForce, assignment.BuildingID, assignment.MemberID, assignment.Position)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Assignments saved successfully",
	})
}

func deleteStormAssignments(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	taskForce := vars["taskForce"]

	if taskForce != "A" && taskForce != "B" {
		http.Error(w, "Invalid task force - must be A or B", http.StatusBadRequest)
		return
	}

	_, err := db.Exec("DELETE FROM storm_assignments WHERE task_force = ?", taskForce)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
