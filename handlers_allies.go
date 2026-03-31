// handlers_allies.go - Allies tracker handlers

package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

// --- Agreement Types ---

func getAllyAgreementTypes(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, name, active, sort_order, created_at FROM ally_agreement_types ORDER BY sort_order, id`)
	if err != nil {
		slog.Error("getAllyAgreementTypes: query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	types := []AllyAgreementType{}
	for rows.Next() {
		var t AllyAgreementType
		var active int
		if err := rows.Scan(&t.ID, &t.Name, &active, &t.SortOrder, &t.CreatedAt); err != nil {
			slog.Error("getAllyAgreementTypes: scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		t.Active = active == 1
		types = append(types, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types)
}

func createAllyAgreementType(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		SortOrder int    `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	res, err := db.Exec(`INSERT INTO ally_agreement_types (name, sort_order) VALUES (?, ?)`, req.Name, req.SortOrder)
	if err != nil {
		slog.Error("createAllyAgreementType: insert failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	var t AllyAgreementType
	db.QueryRow(`SELECT id, name, active, sort_order, created_at FROM ally_agreement_types WHERE id = ?`, id).Scan(
		&t.ID, &t.Name, &t.Active, &t.SortOrder, &t.CreatedAt,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(t)
}

func updateAllyAgreementType(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var req struct {
		Name      string `json:"name"`
		Active    *bool  `json:"active"`
		SortOrder *int   `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name != "" {
		if _, err := db.Exec(`UPDATE ally_agreement_types SET name = ? WHERE id = ?`, req.Name, id); err != nil {
			slog.Error("updateAllyAgreementType: name update failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if req.Active != nil {
		active := 0
		if *req.Active {
			active = 1
		}
		if _, err := db.Exec(`UPDATE ally_agreement_types SET active = ? WHERE id = ?`, active, id); err != nil {
			slog.Error("updateAllyAgreementType: active update failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if req.SortOrder != nil {
		if _, err := db.Exec(`UPDATE ally_agreement_types SET sort_order = ? WHERE id = ?`, *req.SortOrder, id); err != nil {
			slog.Error("updateAllyAgreementType: sort_order update failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	var t AllyAgreementType
	var active int
	db.QueryRow(`SELECT id, name, active, sort_order, created_at FROM ally_agreement_types WHERE id = ?`, id).Scan(
		&t.ID, &t.Name, &active, &t.SortOrder, &t.CreatedAt,
	)
	t.Active = active == 1

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(t)
}

func deleteAllyAgreementType(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	force := r.URL.Query().Get("force") == "true"

	if !force {
		var count int
		db.QueryRow(`SELECT COUNT(*) FROM ally_agreements WHERE agreement_type_id = ?`, id).Scan(&count)
		if count > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Agreement type is in use by allies. Use force=true to delete anyway.",
				"count": count,
			})
			return
		}
	}

	if _, err := db.Exec(`DELETE FROM ally_agreement_types WHERE id = ?`, id); err != nil {
		slog.Error("deleteAllyAgreementType: delete failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Allies ---

func getAllies(w http.ResponseWriter, r *http.Request) {
	includeInactive := r.URL.Query().Get("include_inactive") == "true"

	query := `SELECT id, server, tag, name, active, notes, contact, created_at FROM allies`
	if !includeInactive {
		query += ` WHERE active = 1`
	}
	query += ` ORDER BY name`

	rows, err := db.Query(query)
	if err != nil {
		slog.Error("getAllies: query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	allies := []Ally{}
	for rows.Next() {
		var a Ally
		var active int
		if err := rows.Scan(&a.ID, &a.Server, &a.Tag, &a.Name, &active, &a.Notes, &a.Contact, &a.CreatedAt); err != nil {
			slog.Error("getAllies: scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		a.Active = active == 1
		a.AgreementTypeIDs = []int{}
		allies = append(allies, a)
	}

	// Fetch agreement type IDs for each ally
	for i, a := range allies {
		typeRows, err := db.Query(`SELECT agreement_type_id FROM ally_agreements WHERE ally_id = ? ORDER BY agreement_type_id`, a.ID)
		if err != nil {
			slog.Error("getAllies: agreement query failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		for typeRows.Next() {
			var tid int
			typeRows.Scan(&tid)
			allies[i].AgreementTypeIDs = append(allies[i].AgreementTypeIDs, tid)
		}
		typeRows.Close()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allies)
}

func createAlly(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Server           string `json:"server"`
		Tag              string `json:"tag"`
		Name             string `json:"name"`
		Notes            string `json:"notes"`
		Contact          string `json:"contact"`
		AgreementTypeIDs []int  `json:"agreement_type_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Server == "" || req.Tag == "" || req.Name == "" {
		http.Error(w, "Server, tag, and name are required", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("createAlly: begin tx failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO allies (server, tag, name, notes, contact) VALUES (?, ?, ?, ?, ?)`,
		req.Server, req.Tag, req.Name, req.Notes, req.Contact)
	if err != nil {
		slog.Error("createAlly: insert failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	allyID, _ := res.LastInsertId()

	for _, tid := range req.AgreementTypeIDs {
		if _, err := tx.Exec(`INSERT INTO ally_agreements (ally_id, agreement_type_id) VALUES (?, ?)`, allyID, tid); err != nil {
			slog.Error("createAlly: agreement insert failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		slog.Error("createAlly: commit failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var a Ally
	var active int
	db.QueryRow(`SELECT id, server, tag, name, active, notes, contact, created_at FROM allies WHERE id = ?`, allyID).Scan(
		&a.ID, &a.Server, &a.Tag, &a.Name, &active, &a.Notes, &a.Contact, &a.CreatedAt,
	)
	a.Active = active == 1
	a.AgreementTypeIDs = req.AgreementTypeIDs
	if a.AgreementTypeIDs == nil {
		a.AgreementTypeIDs = []int{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(a)
}

func updateAlly(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var req struct {
		Server           string `json:"server"`
		Tag              string `json:"tag"`
		Name             string `json:"name"`
		Active           *bool  `json:"active"`
		Notes            string `json:"notes"`
		Contact          string `json:"contact"`
		AgreementTypeIDs []int  `json:"agreement_type_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Server == "" || req.Tag == "" || req.Name == "" {
		http.Error(w, "Server, tag, and name are required", http.StatusBadRequest)
		return
	}
	if req.Active == nil {
		t := true
		req.Active = &t
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("updateAlly: begin tx failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	active := 0
	if *req.Active {
		active = 1
	}
	if _, err := tx.Exec(`UPDATE allies SET server=?, tag=?, name=?, active=?, notes=?, contact=? WHERE id=?`,
		req.Server, req.Tag, req.Name, active, req.Notes, req.Contact, id); err != nil {
		slog.Error("updateAlly: update failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if _, err := tx.Exec(`DELETE FROM ally_agreements WHERE ally_id = ?`, id); err != nil {
		slog.Error("updateAlly: delete agreements failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	for _, tid := range req.AgreementTypeIDs {
		if _, err := tx.Exec(`INSERT INTO ally_agreements (ally_id, agreement_type_id) VALUES (?, ?)`, id, tid); err != nil {
			slog.Error("updateAlly: agreement insert failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		slog.Error("updateAlly: commit failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var a Ally
	var activeVal int
	db.QueryRow(`SELECT id, server, tag, name, active, notes, contact, created_at FROM allies WHERE id = ?`, id).Scan(
		&a.ID, &a.Server, &a.Tag, &a.Name, &activeVal, &a.Notes, &a.Contact, &a.CreatedAt,
	)
	a.Active = activeVal == 1
	a.AgreementTypeIDs = req.AgreementTypeIDs
	if a.AgreementTypeIDs == nil {
		a.AgreementTypeIDs = []int{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(a)
}

func deleteAlly(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("deleteAlly: begin tx failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM allies WHERE id = ?`, id); err != nil {
		slog.Error("deleteAlly: delete failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		slog.Error("deleteAlly: commit failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
