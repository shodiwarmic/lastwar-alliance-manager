// handlers_recruiting.go - Recruiting page and prospects CRUD handlers

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
			p.first_contacted, p.created_at, p.updated_at,
			p.prospect_type, p.lastrank_public_id
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
			&p.ProspectType, &p.LastRankPublicID,
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
	if p.ProspectType == "" {
		p.ProspectType = "transfer"
	}
	if !validProspectType(p.ProspectType) {
		http.Error(w, "prospect_type must be 'transfer' or 'prospect'", http.StatusBadRequest)
		return
	}
	if !statusAllowedForType(p.Status, p.ProspectType) {
		http.Error(w, "status not valid for prospect type", http.StatusBadRequest)
		return
	}

	result, err := db.Exec(`
		INSERT INTO prospects
			(name, server, source_alliance, power, rank_in_alliance,
			 recruiter_id, status, notes,
			 hero_power, seat_color, interested_in_r4,
			 first_contacted, prospect_type)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Server, p.SourceAlliance, p.Power, p.RankInAlliance,
		p.RecruiterID, p.Status, p.Notes,
		p.HeroPower, p.SeatColor, p.InterestedInR4,
		p.FirstContacted, p.ProspectType,
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

	user := getAuthUser(r)
	prospectDetails := ""
	if p.Server != "" || p.SourceAlliance != "" {
		prospectDetails = "server: " + p.Server
		if p.SourceAlliance != "" {
			prospectDetails += ", from: " + p.SourceAlliance
		}
	}
	logActivity(user.ID, user.Username, "created", "prospect", p.Name, false, prospectDetails)

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
	if p.ProspectType == "" {
		p.ProspectType = "transfer"
	}
	if !validProspectType(p.ProspectType) {
		http.Error(w, "prospect_type must be 'transfer' or 'prospect'", http.StatusBadRequest)
		return
	}
	// Auto-reset transfer-specific statuses before the cross-field validation so the
	// inline Move button works without a second round-trip.
	if p.ProspectType == "prospect" && (p.Status == "qualified_transfer" || p.Status == "unqualified_transfer") {
		p.Status = "interested"
	}
	if !statusAllowedForType(p.Status, p.ProspectType) {
		http.Error(w, "status not valid for prospect type", http.StatusBadRequest)
		return
	}

	var oldName, oldStatus, oldServer, oldSourceAlliance, oldRankInAlliance, oldSeatColor, oldFirstContacted string
	var oldPower, oldHeroPower *int64
	var oldRecruiterID *int
	var oldInterestedInR4 bool
	var oldProspectType string
	db.QueryRow(`SELECT name, status, COALESCE(server,''), COALESCE(source_alliance,''), COALESCE(rank_in_alliance,''), power, hero_power, recruiter_id, COALESCE(seat_color,''), interested_in_r4, COALESCE(first_contacted,''), COALESCE(prospect_type,'transfer') FROM prospects WHERE id = ?`, id).Scan(
		&oldName, &oldStatus, &oldServer, &oldSourceAlliance, &oldRankInAlliance,
		&oldPower, &oldHeroPower, &oldRecruiterID, &oldSeatColor, &oldInterestedInR4, &oldFirstContacted,
		&oldProspectType,
	)

	result, err := db.Exec(`
		UPDATE prospects
		SET name=?, server=?, source_alliance=?, power=?, rank_in_alliance=?,
		    recruiter_id=?, status=?, notes=?,
		    hero_power=?, seat_color=?, interested_in_r4=?,
		    first_contacted=?, prospect_type=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?`,
		p.Name, p.Server, p.SourceAlliance, p.Power, p.RankInAlliance,
		p.RecruiterID, p.Status, p.Notes,
		p.HeroPower, p.SeatColor, p.InterestedInR4,
		p.FirstContacted, p.ProspectType, id,
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

	user := getAuthUser(r)
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
		prospectChanges = append(prospectChanges, "total hero power: "+oldHeroPowerStr+" → "+newHeroPowerStr)
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
	if oldProspectType != p.ProspectType {
		prospectChanges = append(prospectChanges, "type: "+oldProspectType+" → "+p.ProspectType)
	}
	logActivity(user.ID, user.Username, "updated", "prospect", p.Name, false, strings.Join(prospectChanges, "; "))

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

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "deleted", "prospect", prospectName, false)

	w.WriteHeader(http.StatusNoContent)
}

type ConvertProspectRequest struct {
	Rank  string `json:"rank"`
	Level int    `json:"level"`
	Power *int64 `json:"power"`
}

func convertProspectToMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid prospect ID", http.StatusBadRequest)
		return
	}

	var req ConvertProspectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	validRanks := map[string]bool{"R1": true, "R2": true, "R3": true, "R4": true, "R5": true}
	if !validRanks[req.Rank] {
		http.Error(w, "rank must be one of R1–R5", http.StatusBadRequest)
		return
	}

	var prospectName string
	var prospectPower *int64
	err = db.QueryRow(`SELECT name, power FROM prospects WHERE id = ?`, id).Scan(&prospectName, &prospectPower)
	if err == sql.ErrNoRows {
		http.Error(w, "Prospect not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("Failed to load prospect for conversion", "prospect_id", id, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Fall back to prospect's stored power if none supplied in request
	if req.Power == nil {
		req.Power = prospectPower
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("Failed to begin transaction for prospect conversion", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		"INSERT INTO members (name, rank, level, eligible) VALUES (?, ?, ?, 1)",
		prospectName, req.Rank, req.Level,
	)
	if err != nil {
		slog.Error("Failed to insert member from prospect", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	memberID, _ := result.LastInsertId()

	if req.Power != nil {
		if _, err := tx.Exec(
			"INSERT INTO power_history (member_id, power, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)",
			memberID, *req.Power,
		); err != nil {
			slog.Error("Failed to insert initial power history for converted member", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	if _, err := tx.Exec("DELETE FROM prospects WHERE id = ?", id); err != nil {
		slog.Error("Failed to delete prospect after conversion", "prospect_id", id, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		slog.Error("Failed to commit prospect conversion", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "created", "member", prospectName, false, "converted from prospect")

	m := Member{
		ID:       int(memberID),
		Name:     prospectName,
		Rank:     req.Rank,
		Level:    req.Level,
		Eligible: true,
		Power:    req.Power,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(m)
}

func validProspectStatus(status string) bool {
	switch status {
	case "interested", "pending", "declined", "qualified_transfer", "unqualified_transfer":
		return true
	}
	return false
}

func validProspectType(t string) bool {
	return t == "transfer" || t == "prospect"
}

func statusAllowedForType(status, prospectType string) bool {
	if prospectType == "prospect" {
		return status != "qualified_transfer" && status != "unqualified_transfer"
	}
	return true
}
