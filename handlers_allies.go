// handlers_allies.go - Allies tracker handlers

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

// parseServerNumber extracts a server number from officer-entered free text:
// "1712", "S1712", "#1712", " s1712 ", "Server 1712" all → 1712. ok=false when there are no
// digits. This is the canonical text → INTEGER conversion for every server field.
//
// It takes the FIRST contiguous run of digits, not every digit: stripping all non-digits would
// silently fuse a multi-server entry like "1712 / 1713" into 17121713 — a corrupt server number
// that matches nothing and looks plausible in the DB.
func parseServerNumber(s string) (int, bool) {
	// IndexAny over ASCII digits, not strings.IndexFunc(s, unicode.IsDigit): IsDigit also matches
	// non-ASCII numerals, which the ASCII loop below would reject at the first byte — silently
	// returning ok=false past perfectly good ASCII digits later in the string.
	start := strings.IndexAny(s, "0123456789")
	if start < 0 {
		return 0, false
	}
	end := start
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, err := strconv.Atoi(s[start:end])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// ourAllianceTagTx reads our own alliance tag inside a transaction. Must read on tx, not db:
// SetMaxOpenConns(1) means a db query while a tx is open deadlocks.
func ourAllianceTagTx(tx *sql.Tx) string {
	var tag string
	tx.QueryRow(`SELECT COALESCE(alliance_tag, '') FROM settings WHERE id = 1`).Scan(&tag)
	return strings.TrimSpace(tag)
}

// ourAllianceIdentity returns our configured LastRank alliance id (normalized) and tag. The id is
// run through parseLastRankAllianceID because updateSettings stores the raw string when it doesn't
// parse; an unparseable id is reported as empty so callers fall back to the tag.
func ourAllianceIdentity() (lastrankID, tag string) {
	var rawID, rawTag string
	db.QueryRow(`SELECT COALESCE(lastrank_alliance_id, ''), COALESCE(alliance_tag, '') FROM settings WHERE id = 1`).
		Scan(&rawID, &rawTag)
	if parsed, ok := parseLastRankAllianceID(strings.TrimSpace(rawID)); ok {
		lastrankID = parsed
	}
	return lastrankID, strings.TrimSpace(rawTag)
}

// isOwnAlliance reports whether a (lastrank id, tag) pair identifies OUR alliance — the Rule 2
// test, shared by every path that could otherwise write us into external_alliances.
//
// Both sides of a comparison must be non-empty: an empty configured tag matching a blank upstream
// abbr would brand a *foreign* alliance as us, which is worse than failing to recognise ourselves.
func isOwnAlliance(lastrankID, tag string) bool {
	ourID, ourTag := ourAllianceIdentity()
	if ourID != "" && lastrankID != "" && strings.EqualFold(strings.TrimSpace(lastrankID), ourID) {
		return true
	}
	if ourTag != "" && tag != "" && strings.EqualFold(strings.TrimSpace(tag), ourTag) {
		return true
	}
	return false
}

// findOrCreateExternalAllianceTx returns the external_alliances registry id for an alliance
// identity, deduping by tag (globally unique at a moment) then name — creating a row if none
// exists and refreshing name/server on an existing one. Server is parsed from text
// (external_alliances.server is INTEGER). Used to link allies (and prospects) into the registry
// so it stays the single identity source. Returns an invalid NullInt64 when there's no identity.
//
// Our own alliance is never registered: external_alliances is a registry of EXTERNAL alliances
// and feeds the VS League opponent picker and the prospect source-alliance field, so a row for us
// would let an officer pick their own alliance as an opponent.
func findOrCreateExternalAllianceTx(tx *sql.Tx, tag, name, serverStr string) (sql.NullInt64, error) {
	tag, name = strings.TrimSpace(tag), strings.TrimSpace(name)

	// Never mint a registry row for our own alliance. An invalid NullInt64 leaves the caller's
	// ally/prospect row intact — it simply carries no registry link. Safe against collisions: a
	// tag is globally unique in-game at any one moment (see migration 057).
	if ourTag := ourAllianceTagTx(tx); ourTag != "" && strings.EqualFold(tag, ourTag) {
		return sql.NullInt64{}, nil
	}

	var server any // nil → NULL
	if n, ok := parseServerNumber(serverStr); ok {
		server = n
	}
	var id int64
	var err error
	switch {
	case tag != "":
		err = tx.QueryRow(`SELECT id FROM external_alliances WHERE tag = ? COLLATE NOCASE ORDER BY updated_at DESC LIMIT 1`, tag).Scan(&id)
	case name != "":
		err = tx.QueryRow(`SELECT id FROM external_alliances WHERE name = ? COLLATE NOCASE ORDER BY updated_at DESC LIMIT 1`, name).Scan(&id)
	default:
		return sql.NullInt64{}, nil
	}
	if err == nil {
		if _, uerr := tx.Exec(`UPDATE external_alliances SET name = COALESCE(NULLIF(?,''), name),
			server = COALESCE(?, server), updated_at = CURRENT_TIMESTAMP WHERE id = ?`, name, server, id); uerr != nil {
			return sql.NullInt64{}, uerr
		}
		return sql.NullInt64{Int64: id, Valid: true}, nil
	}
	res, ierr := tx.Exec(`INSERT INTO external_alliances (tag, name, server) VALUES (?,?,?)`, nullStr(tag), nullStr(name), server)
	if ierr != nil {
		return sql.NullInt64{}, ierr
	}
	nid, _ := res.LastInsertId()
	return sql.NullInt64{Int64: nid, Valid: true}, nil
}

func nullStr(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

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

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "created", "agreement_type", req.Name, false)

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

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "updated", "agreement_type", t.Name, false)

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

	var typeName string
	db.QueryRow(`SELECT name FROM ally_agreement_types WHERE id = ?`, id).Scan(&typeName)

	if _, err := db.Exec(`DELETE FROM ally_agreement_types WHERE id = ?`, id); err != nil {
		slog.Error("deleteAllyAgreementType: delete failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "deleted", "agreement_type", typeName, false)

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
	// Normalize on write, not just on the registry link: the browser sends a numeric input, but
	// the mobile API could still post "S1712".
	if n, ok := parseServerNumber(req.Server); ok {
		req.Server = strconv.Itoa(n)
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("createAlly: begin tx failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	eaID, err := findOrCreateExternalAllianceTx(tx, req.Tag, req.Name, req.Server)
	if err != nil {
		slog.Error("createAlly: registry link failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	res, err := tx.Exec(`INSERT INTO allies (server, tag, name, notes, contact, external_alliance_id) VALUES (?, ?, ?, ?, ?, ?)`,
		req.Server, req.Tag, req.Name, req.Notes, req.Contact, eaID)
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

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "created", "ally", req.Name, false)

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
	// Normalize on write (see createAlly).
	if n, ok := parseServerNumber(req.Server); ok {
		req.Server = strconv.Itoa(n)
	}

	// Fetch current values for diff
	var oldName, oldServer, oldTag string
	var oldActive int
	db.QueryRow(`SELECT name, server, tag, active FROM allies WHERE id = ?`, id).Scan(&oldName, &oldServer, &oldTag, &oldActive)

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
	eaID, err := findOrCreateExternalAllianceTx(tx, req.Tag, req.Name, req.Server)
	if err != nil {
		slog.Error("updateAlly: registry link failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(`UPDATE allies SET server=?, tag=?, name=?, active=?, notes=?, contact=?, external_alliance_id=? WHERE id=?`,
		req.Server, req.Tag, req.Name, active, req.Notes, req.Contact, eaID, id); err != nil {
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

	user := getAuthUser(r)
	var allyChanges []string
	if oldName != req.Name {
		allyChanges = append(allyChanges, "name: "+oldName+" → "+req.Name)
	}
	if oldServer != req.Server {
		allyChanges = append(allyChanges, "server: "+oldServer+" → "+req.Server)
	}
	if oldTag != req.Tag {
		allyChanges = append(allyChanges, "tag: "+oldTag+" → "+req.Tag)
	}
	if oldActive != active {
		was, now := "inactive", "inactive"
		if oldActive == 1 {
			was = "active"
		}
		if active == 1 {
			now = "active"
		}
		allyChanges = append(allyChanges, was+" → "+now)
	}
	logActivity(user.ID, user.Username, "updated", "ally", req.Name, false, strings.Join(allyChanges, "; "))

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

	var allyName string
	db.QueryRow(`SELECT name FROM allies WHERE id = ?`, id).Scan(&allyName)

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

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "deleted", "ally", allyName, false)

	w.WriteHeader(http.StatusNoContent)
}
