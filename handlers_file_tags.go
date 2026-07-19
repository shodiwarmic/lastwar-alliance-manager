// handlers_file_tags.go - CRUD for file tags (labels attached to alliance files).
// Mirrors the ally agreement-types pattern: GET open to any file viewer, writes gated
// on manage_files, force-delete when a tag is in use.

package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

// isValidFileTagColor reports whether c is an allowed semantic color token.
func isValidFileTagColor(c string) bool {
	for _, allowed := range FileTagColors {
		if c == allowed {
			return true
		}
	}
	return false
}

// isValidRank reports whether r is one of R1–R5.
func isValidRank(r string) bool {
	for _, vr := range ValidRanks {
		if r == vr {
			return true
		}
	}
	return false
}

// getFileTags returns the tags visible to the caller (rank-filtered), each with a
// file_count. Only tags at or below the caller's rank are returned, so a lower-rank
// user never learns a restricted tag exists.
func getFileTags(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	userRankVal := rankTier(effectiveUserRank(user))

	// Read all tags, then counts — each cursor fully closed before the next query
	// (single-connection rule).
	allTags, err := loadAllFileTags()
	if err != nil {
		slog.Error("getFileTags: load tags failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	counts, err := fileTagCounts()
	if err != nil {
		slog.Error("getFileTags: counts failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	visible := []FileTag{}
	for _, t := range allTags {
		if userRankVal < rankTier(t.MinRank) {
			continue
		}
		t.FileCount = counts[t.ID]
		visible = append(visible, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(visible)
}

// fileTagCounts returns tag_id -> number of files carrying it. Cursor closed on return.
func fileTagCounts() (map[int]int, error) {
	rows, err := db.Query(`SELECT tag_id, COUNT(*) FROM file_tag_map GROUP BY tag_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int]int)
	for rows.Next() {
		var tagID, n int
		if err := rows.Scan(&tagID, &n); err != nil {
			return nil, err
		}
		out[tagID] = n
	}
	return out, rows.Err()
}

// validateTagInput checks name/min_rank/color and enforces that the caller can't
// create or raise a tag above their own rank (a tag they couldn't see or undo).
func validateTagInput(name, minRank, color string, user *AuthUser) string {
	if name == "" {
		return "Name is required"
	}
	if !isValidRank(minRank) {
		return "Invalid minimum rank"
	}
	if !isValidFileTagColor(color) {
		return "Invalid color"
	}
	if rankTier(minRank) > rankTier(effectiveUserRank(user)) {
		return "You cannot create a tag with a minimum rank above your own"
	}
	return ""
}

func createFileTag(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	var req struct {
		Name      string `json:"name"`
		MinRank   string `json:"min_rank"`
		Color     string `json:"color"`
		SortOrder int    `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.MinRank == "" {
		req.MinRank = "R1"
	}
	if req.Color == "" {
		req.Color = "neutral"
	}
	if msg := validateTagInput(req.Name, req.MinRank, req.Color, user); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	res, err := db.Exec(`INSERT INTO file_tags (name, min_rank, color, sort_order) VALUES (?, ?, ?, ?)`,
		req.Name, req.MinRank, req.Color, req.SortOrder)
	if err != nil {
		slog.Error("createFileTag: insert failed", "error", err)
		http.Error(w, "Database error (name may already exist)", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	var t FileTag
	db.QueryRow(`SELECT id, name, min_rank, color, sort_order FROM file_tags WHERE id = ?`, id).
		Scan(&t.ID, &t.Name, &t.MinRank, &t.Color, &t.SortOrder)

	logActivity(user.ID, user.Username, "created", "file_tag", req.Name, false)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(t)
}

func updateFileTag(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}
	user := getAuthUser(r)

	// Read old row first: for the diff, and to confirm the caller can see the tag
	// they're editing (never edit a tag above your rank).
	var old FileTag
	err = db.QueryRow(`SELECT id, name, min_rank, color, sort_order FROM file_tags WHERE id = ?`, id).
		Scan(&old.ID, &old.Name, &old.MinRank, &old.Color, &old.SortOrder)
	if err != nil {
		http.Error(w, "Tag not found", http.StatusNotFound)
		return
	}
	if rankTier(old.MinRank) > rankTier(effectiveUserRank(user)) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var req struct {
		Name      string `json:"name"`
		MinRank   string `json:"min_rank"`
		Color     string `json:"color"`
		SortOrder int    `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if msg := validateTagInput(req.Name, req.MinRank, req.Color, user); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	if _, err := db.Exec(`UPDATE file_tags SET name = ?, min_rank = ?, color = ?, sort_order = ? WHERE id = ?`,
		req.Name, req.MinRank, req.Color, req.SortOrder, id); err != nil {
		slog.Error("updateFileTag: update failed", "error", err)
		http.Error(w, "Database error (name may already exist)", http.StatusInternalServerError)
		return
	}

	var changes []string
	if req.Name != old.Name {
		changes = append(changes, "name: "+old.Name+" → "+req.Name)
	}
	if req.MinRank != old.MinRank {
		changes = append(changes, "min rank: "+old.MinRank+" → "+req.MinRank)
	}
	if req.Color != old.Color {
		changes = append(changes, "color: "+old.Color+" → "+req.Color)
	}
	logActivity(user.ID, user.Username, "updated", "file_tag", req.Name, false, joinChanges(changes))

	var t FileTag
	db.QueryRow(`SELECT id, name, min_rank, color, sort_order FROM file_tags WHERE id = ?`, id).
		Scan(&t.ID, &t.Name, &t.MinRank, &t.Color, &t.SortOrder)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(t)
}

func deleteFileTag(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}
	user := getAuthUser(r)

	var tagName, minRank string
	if err := db.QueryRow(`SELECT name, min_rank FROM file_tags WHERE id = ?`, id).Scan(&tagName, &minRank); err != nil {
		http.Error(w, "Tag not found", http.StatusNotFound)
		return
	}
	if rankTier(minRank) > rankTier(effectiveUserRank(user)) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	force := r.URL.Query().Get("force") == "true"
	if !force {
		var count int
		db.QueryRow(`SELECT COUNT(*) FROM file_tag_map WHERE tag_id = ?`, id).Scan(&count)
		if count > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Tag is in use by files. Use force=true to delete anyway.",
				"count": count,
			})
			return
		}
	}

	// Remove the map rows and the tag in one transaction (FKs are declarative-only).
	tx, err := db.Begin()
	if err != nil {
		slog.Error("deleteFileTag: begin failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(`DELETE FROM file_tag_map WHERE tag_id = ?`, id); err != nil {
		tx.Rollback()
		slog.Error("deleteFileTag: map delete failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(`DELETE FROM file_tags WHERE id = ?`, id); err != nil {
		tx.Rollback()
		slog.Error("deleteFileTag: delete failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("deleteFileTag: commit failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	logActivity(user.ID, user.Username, "deleted", "file_tag", tagName, false)

	w.WriteHeader(http.StatusNoContent)
}

// joinChanges renders a change list for the activity log, empty string when no diff.
func joinChanges(changes []string) string {
	if len(changes) == 0 {
		return ""
	}
	out := changes[0]
	for _, c := range changes[1:] {
		out += "; " + c
	}
	return out
}
