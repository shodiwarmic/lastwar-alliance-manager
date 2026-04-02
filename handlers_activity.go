// handlers_activity.go - Activity log page and API handler.

package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
)

// getActivityLog serves GET /api/activity.
// Admins see all entries; non-admins with view_activity see only non-sensitive entries.
// Query params: limit (default 100, max 1000), offset (default 0), user_id (optional filter).
func getActivityLog(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	isAdmin, _ := session.Values["is_admin"].(bool)

	// Permission check: admin always allowed; non-admins need view_activity
	if !isAdmin {
		var memberID int
		if mid, ok := session.Values["member_id"].(int); ok {
			memberID = mid
		}
		if memberID == 0 {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		var rank string
		if err := db.QueryRow("SELECT rank FROM members WHERE id = ?", memberID).Scan(&rank); err != nil {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		perms := getRankPermissions(rank)
		if !perms.ViewActivity {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	// Parse query params
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}

	// Build query — non-admins cannot see sensitive entries
	args := []interface{}{}
	sensitiveFilter := ""
	if !isAdmin {
		sensitiveFilter = "WHERE is_sensitive = 0"
	}

	userIDParam := r.URL.Query().Get("user_id")
	if userIDParam != "" {
		uid, err := strconv.Atoi(userIDParam)
		if err != nil {
			http.Error(w, "Invalid user_id", http.StatusBadRequest)
			return
		}
		if sensitiveFilter == "" {
			sensitiveFilter = "WHERE user_id = ?"
		} else {
			sensitiveFilter += " AND user_id = ?"
		}
		args = append(args, uid)
	}

	args = append(args, limit, offset)
	query := `SELECT id, user_id, username, action, entity_type, entity_name, details, entity_count,
	                 is_sensitive, created_at, updated_at
	          FROM activity_log
	          ` + sensitiveFilter + `
	          ORDER BY updated_at DESC
	          LIMIT ? OFFSET ?`

	rows, err := db.Query(query, args...)
	if err != nil {
		slog.Error("failed to query activity_log", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	entries := []ActivityLog{}
	for rows.Next() {
		var e ActivityLog
		var sensitiveInt int
		if err := rows.Scan(&e.ID, &e.UserID, &e.Username, &e.Action, &e.EntityType,
			&e.EntityName, &e.Details, &e.EntityCount, &sensitiveInt, &e.CreatedAt, &e.UpdatedAt); err != nil {
			slog.Error("failed to scan activity_log row", "error", err)
			continue
		}
		e.IsSensitive = sensitiveInt == 1
		entries = append(entries, e)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}
