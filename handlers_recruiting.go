// handlers_recruiting.go - Recruiting page and prospects CRUD handlers

package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

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

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	prospectDetails := ""
	if p.Server != "" || p.SourceAlliance != "" {
		prospectDetails = "server: " + p.Server
		if p.SourceAlliance != "" {
			prospectDetails += ", from: " + p.SourceAlliance
		}
	}
	logActivity(actorID, actorName, "created", "prospect", p.Name, false, prospectDetails)

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

	var oldName, oldStatus, oldServer, oldSourceAlliance, oldRankInAlliance, oldSeatColor, oldFirstContacted string
	var oldPower, oldHeroPower *int64
	var oldRecruiterID *int
	var oldInterestedInR4 bool
	db.QueryRow(`SELECT name, status, COALESCE(server,''), COALESCE(source_alliance,''), COALESCE(rank_in_alliance,''), power, hero_power, recruiter_id, COALESCE(seat_color,''), interested_in_r4, COALESCE(first_contacted,'') FROM prospects WHERE id = ?`, id).Scan(
		&oldName, &oldStatus, &oldServer, &oldSourceAlliance, &oldRankInAlliance,
		&oldPower, &oldHeroPower, &oldRecruiterID, &oldSeatColor, &oldInterestedInR4, &oldFirstContacted,
	)

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

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	var prospectChanges []string
	if oldName != p.Name {
		prospectChanges = append(prospectChanges, "name: "+oldName+" → "+p.Name)
	}
	if oldStatus != p.Status {
		prospectChanges = append(prospectChanges, "status: "+oldStatus+" → "+p.Status)
	}
	if oldServer != p.Server {
		prospectChanges = append(prospectChanges, "server: "+oldServer+" → "+p.Server)
	}
	if oldSourceAlliance != p.SourceAlliance {
		prospectChanges = append(prospectChanges, "source alliance: "+oldSourceAlliance+" → "+p.SourceAlliance)
	}
	if oldRankInAlliance != p.RankInAlliance {
		prospectChanges = append(prospectChanges, "rank in alliance: "+oldRankInAlliance+" → "+p.RankInAlliance)
	}
	oldPowerStr := ""
	if oldPower != nil {
		oldPowerStr = strconv.FormatInt(*oldPower, 10)
	}
	newPowerStr := ""
	if p.Power != nil {
		newPowerStr = strconv.FormatInt(*p.Power, 10)
	}
	if oldPowerStr != newPowerStr {
		prospectChanges = append(prospectChanges, "power: "+oldPowerStr+" → "+newPowerStr)
	}
	oldHeroPowerStr := ""
	if oldHeroPower != nil {
		oldHeroPowerStr = strconv.FormatInt(*oldHeroPower, 10)
	}
	newHeroPowerStr := ""
	if p.HeroPower != nil {
		newHeroPowerStr = strconv.FormatInt(*p.HeroPower, 10)
	}
	if oldHeroPowerStr != newHeroPowerStr {
		prospectChanges = append(prospectChanges, "hero power: "+oldHeroPowerStr+" → "+newHeroPowerStr)
	}
	oldRecruiterName := ""
	if oldRecruiterID != nil {
		db.QueryRow("SELECT name FROM members WHERE id = ?", *oldRecruiterID).Scan(&oldRecruiterName)
	}
	newRecruiterName := ""
	if p.RecruiterID != nil {
		db.QueryRow("SELECT name FROM members WHERE id = ?", *p.RecruiterID).Scan(&newRecruiterName)
	}
	if oldRecruiterName != newRecruiterName {
		prospectChanges = append(prospectChanges, "recruiter: "+oldRecruiterName+" → "+newRecruiterName)
	}
	if oldSeatColor != p.SeatColor {
		prospectChanges = append(prospectChanges, "seat color: "+oldSeatColor+" → "+p.SeatColor)
	}
	if oldInterestedInR4 != p.InterestedInR4 {
		oldR4 := "no"
		if oldInterestedInR4 {
			oldR4 = "yes"
		}
		newR4 := "no"
		if p.InterestedInR4 {
			newR4 = "yes"
		}
		prospectChanges = append(prospectChanges, "interested in R4: "+oldR4+" → "+newR4)
	}
	if oldFirstContacted != p.FirstContacted {
		prospectChanges = append(prospectChanges, "first contacted: "+oldFirstContacted+" → "+p.FirstContacted)
	}
	logActivity(actorID, actorName, "updated", "prospect", p.Name, false, strings.Join(prospectChanges, "; "))

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

	var prospectName string
	db.QueryRow("SELECT name FROM prospects WHERE id = ?", id).Scan(&prospectName)

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

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "deleted", "prospect", prospectName, false)

	w.WriteHeader(http.StatusNoContent)
}

func validProspectStatus(status string) bool {
	switch status {
	case "interested", "pending", "declined", "qualified_transfer", "unqualified_transfer":
		return true
	}
	return false
}
