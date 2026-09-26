package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

// Strike categories (migration 079). accountability_strikes.strike_type holds a
// strike_types KEY; the label is what every surface renders. Keys are immutable,
// system rows can be neither deleted nor deactivated (code writes their keys), and
// a custom row that any strike uses can be deactivated but not deleted — a
// deactivated category still labels its old strikes, it just stops being offered.

type StrikeType struct {
	ID        int    `json:"id"`
	Key       string `json:"key"`
	Label     string `json:"label"`
	IsSystem  bool   `json:"is_system"`
	Active    bool   `json:"active"`
	SortOrder int    `json:"sort_order"`
	InUse     int    `json:"in_use"`
}

// strikeTypeKeyRe is the shape of a key the UI may mint. Keys copied verbatim by
// migration 079 from pre-existing free text may not match it, and do not have to:
// a key is only ever compared for equality.
var strikeTypeKeyRe = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)

var strikeTypeSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

// slugStrikeTypeKey derives a key from a label: "Diplomacy Violation" →
// "diplomacy_violation". Returns "" when nothing usable remains.
func slugStrikeTypeKey(label string) string {
	s := strikeTypeSlugRe.ReplaceAllString(strings.ToLower(foldName(label)), "_")
	s = strings.Trim(s, "_")
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "_")
	}
	return s
}

// strikeTypeIsActive reports whether key names an active category — the check
// every strike write goes through, so a strike can only carry a key the list knows.
func strikeTypeIsActive(q rowQuerier, key string) (bool, error) {
	var active int
	err := q.QueryRow(`SELECT active FROM strike_types WHERE key = ?`, key).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return active == 1, nil
}

func loadStrikeType(id int) (StrikeType, error) {
	var t StrikeType
	var isSystem, active int
	err := db.QueryRow(`
		SELECT st.id, st.key, st.label, st.is_system, st.active, st.sort_order,
		       (SELECT COUNT(*) FROM accountability_strikes s WHERE s.strike_type = st.key)
		FROM strike_types st WHERE st.id = ?`, id).
		Scan(&t.ID, &t.Key, &t.Label, &isSystem, &active, &t.SortOrder, &t.InUse)
	t.IsSystem, t.Active = isSystem == 1, active == 1
	return t, err
}

// GET /api/accountability/strike-types — active categories in display order;
// ?all=1 adds the inactive ones for the category manager.
func handleStrikeTypes(w http.ResponseWriter, r *http.Request) {
	if !userHasPermission(getAuthUser(r), "view_accountability") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	query := `
		SELECT st.id, st.key, st.label, st.is_system, st.active, st.sort_order,
		       (SELECT COUNT(*) FROM accountability_strikes s WHERE s.strike_type = st.key)
		FROM strike_types st`
	if r.URL.Query().Get("all") != "1" {
		query += ` WHERE st.active = 1`
	}
	query += ` ORDER BY st.sort_order, st.label COLLATE NOCASE`

	rows, err := db.Query(query)
	if err != nil {
		slog.Error("handleStrikeTypes: query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	types := []StrikeType{}
	for rows.Next() {
		var t StrikeType
		var isSystem, active int
		if err := rows.Scan(&t.ID, &t.Key, &t.Label, &isSystem, &active, &t.SortOrder, &t.InUse); err != nil {
			slog.Error("handleStrikeTypes: scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		t.IsSystem, t.Active = isSystem == 1, active == 1
		types = append(types, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types)
}

// POST /api/accountability/strike-types — {label, key?}. The key defaults to a slug
// of the label and cannot be changed afterwards.
func handleStrikeTypeCreate(w http.ResponseWriter, r *http.Request) {
	actor := getAuthUser(r)
	var body struct {
		Label string `json:"label"`
		Key   string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	body.Label = strings.TrimSpace(body.Label)
	if body.Label == "" {
		http.Error(w, "A category needs a label", http.StatusBadRequest)
		return
	}
	key := strings.TrimSpace(body.Key)
	if key == "" {
		key = slugStrikeTypeKey(body.Label)
	}
	if !strikeTypeKeyRe.MatchString(key) {
		http.Error(w, "The key must be 1–40 lowercase letters, digits or underscores", http.StatusBadRequest)
		return
	}

	// New categories go to the end of the list.
	res, err := db.Exec(`
		INSERT INTO strike_types (key, label, is_system, active, sort_order)
		VALUES (?, ?, 0, 1, (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM strike_types))`,
		key, body.Label)
	if isUniqueViolation(err) {
		http.Error(w, "A category with the key \""+key+"\" already exists", http.StatusConflict)
		return
	}
	if err != nil {
		slog.Error("handleStrikeTypeCreate: insert failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	logActivity(actor.ID, actor.Username, "created", "strike_type", body.Label, false, "key: "+key)

	t, err := loadStrikeType(int(id))
	if err != nil {
		slog.Error("handleStrikeTypeCreate: reload failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(t)
}

// PUT /api/accountability/strike-types/{id} — {label, active, sort_order}. The key
// is not accepted; deactivating a system category is refused.
func handleStrikeTypeUpdate(w http.ResponseWriter, r *http.Request) {
	actor := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	var body struct {
		Label     string `json:"label"`
		Active    bool   `json:"active"`
		SortOrder int    `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	body.Label = strings.TrimSpace(body.Label)
	if body.Label == "" {
		http.Error(w, "A category needs a label", http.StatusBadRequest)
		return
	}

	old, err := loadStrikeType(id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleStrikeTypeUpdate: load failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if old.IsSystem && !body.Active {
		http.Error(w, "\""+old.Label+"\" is a system category and cannot be deactivated — the app writes it itself", http.StatusConflict)
		return
	}

	active := 0
	if body.Active {
		active = 1
	}
	if _, err := db.Exec(`UPDATE strike_types SET label = ?, active = ?, sort_order = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		body.Label, active, body.SortOrder, id); err != nil {
		slog.Error("handleStrikeTypeUpdate: update failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var changes []string
	if old.Label != body.Label {
		changes = append(changes, "label: "+old.Label+" → "+body.Label)
	}
	if old.Active != body.Active {
		if body.Active {
			changes = append(changes, "reactivated")
		} else {
			changes = append(changes, "deactivated")
		}
	}
	if old.SortOrder != body.SortOrder {
		changes = append(changes, "position: "+strconv.Itoa(old.SortOrder)+" → "+strconv.Itoa(body.SortOrder))
	}
	if len(changes) > 0 {
		logActivity(actor.ID, actor.Username, "updated", "strike_type", body.Label, false, strings.Join(changes, "; "))
	}

	t, err := loadStrikeType(id)
	if err != nil {
		slog.Error("handleStrikeTypeUpdate: reload failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(t)
}

// DELETE /api/accountability/strike-types/{id} — refused for a system category and
// for any category a strike uses (deactivate those instead).
func handleStrikeTypeDelete(w http.ResponseWriter, r *http.Request) {
	actor := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	t, err := loadStrikeType(id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleStrikeTypeDelete: load failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if t.IsSystem {
		http.Error(w, "\""+t.Label+"\" is a system category and cannot be deleted", http.StatusConflict)
		return
	}
	if t.InUse > 0 {
		http.Error(w, "\""+t.Label+"\" is used by "+strconv.Itoa(t.InUse)+" strike(s) — deactivate it instead, so those strikes keep their label", http.StatusConflict)
		return
	}
	// Re-checked inside the DELETE so a strike created between the load and here
	// cannot be orphaned.
	res, err := db.Exec(`DELETE FROM strike_types WHERE id = ? AND is_system = 0
		AND NOT EXISTS (SELECT 1 FROM accountability_strikes WHERE strike_type = ?)`, id, t.Key)
	if err != nil {
		slog.Error("handleStrikeTypeDelete: delete failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, "\""+t.Label+"\" is now in use — deactivate it instead", http.StatusConflict)
		return
	}
	logActivity(actor.ID, actor.Username, "deleted", "strike_type", t.Label, false, "key: "+t.Key)
	w.WriteHeader(http.StatusNoContent)
}
