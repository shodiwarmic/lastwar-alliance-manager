// handlers_recruiting.go - Recruiting page and prospects CRUD handlers

package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

var validSeatColors = map[string]bool{
	"": true, "gold": true, "purple": true, "blue": true, "grey": true,
}

func getProspects(w http.ResponseWriter, r *http.Request) {
	query := `
		SELECT
			p.id, p.name, p.server, p.source_alliance,
			p.power, p.rank_in_alliance,
			p.recruiter_id, COALESCE(m.name, '') as recruiter_name,
			p.status, p.notes,
			p.hero_power, p.seat_color, p.interested_in_r4,
			p.first_contacted, p.created_at, p.updated_at
		FROM prospects p
		LEFT JOIN members m ON m.id = p.recruiter_id
		ORDER BY p.name
	`

	rows, err := db.Query(query)
	if err != nil {
		slog.Error("Failed to query prospects", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	prospects := []Prospect{}
	for rows.Next() {
		var p Prospect
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Server, &p.SourceAlliance,
			&p.Power, &p.RankInAlliance,
			&p.RecruiterID, &p.RecruiterName,
			&p.Status, &p.Notes,
			&p.HeroPower, &p.SeatColor, &p.InterestedInR4,
			&p.FirstContacted, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			slog.Error("Failed to scan prospect row", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		prospects = append(prospects, p)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(prospects)
}

func createProspect(w http.ResponseWriter, r *http.Request) {
	var p Prospect
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if p.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}
	if !validProspectStatus(p.Status) {
		http.Error(w, "Invalid status value", http.StatusBadRequest)
		return
	}
	if !validSeatColors[p.SeatColor] {
		http.Error(w, "seat_color must be one of: gold, purple, blue, grey, or empty", http.StatusBadRequest)
		return
	}

	result, err := db.Exec(`
		INSERT INTO prospects
			(name, server, source_alliance, power, rank_in_alliance,
			 recruiter_id, status, notes,
			 hero_power, seat_color, interested_in_r4,
			 first_contacted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Server, p.SourceAlliance, p.Power, p.RankInAlliance,
		p.RecruiterID, p.Status, p.Notes,
		p.HeroPower, p.SeatColor, p.InterestedInR4,
		p.FirstContacted,
	)
	if err != nil {
		slog.Error("Failed to create prospect", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	p.ID = int(id)

	if p.RecruiterID != nil {
		db.QueryRow("SELECT name FROM members WHERE id = ?", *p.RecruiterID).Scan(&p.RecruiterName)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(p)
}

func updateProspect(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid prospect ID", http.StatusBadRequest)
		return
	}

	var p Prospect
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if p.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}
	if !validProspectStatus(p.Status) {
		http.Error(w, "Invalid status value", http.StatusBadRequest)
		return
	}
	if !validSeatColors[p.SeatColor] {
		http.Error(w, "seat_color must be one of: gold, purple, blue, grey, or empty", http.StatusBadRequest)
		return
	}

	result, err := db.Exec(`
		UPDATE prospects
		SET name=?, server=?, source_alliance=?, power=?, rank_in_alliance=?,
		    recruiter_id=?, status=?, notes=?,
		    hero_power=?, seat_color=?, interested_in_r4=?,
		    first_contacted=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?`,
		p.Name, p.Server, p.SourceAlliance, p.Power, p.RankInAlliance,
		p.RecruiterID, p.Status, p.Notes,
		p.HeroPower, p.SeatColor, p.InterestedInR4,
		p.FirstContacted, id,
	)
	if err != nil {
		slog.Error("Failed to update prospect", "prospect_id", id, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		http.Error(w, "Prospect not found", http.StatusNotFound)
		return
	}

	p.ID = id
	if p.RecruiterID != nil {
		db.QueryRow("SELECT name FROM members WHERE id = ?", *p.RecruiterID).Scan(&p.RecruiterName)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

func deleteProspect(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid prospect ID", http.StatusBadRequest)
		return
	}

	result, err := db.Exec("DELETE FROM prospects WHERE id = ?", id)
	if err != nil {
		slog.Error("Failed to delete prospect", "prospect_id", id, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		http.Error(w, "Prospect not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func validProspectStatus(status string) bool {
	switch status {
	case "interested", "pending", "declined", "qualified_transfer", "unqualified_transfer":
		return true
	}
	return false
}
