// activity.go - Audit log helper. Writes activity_log entries with create-batching.

package main

import "log/slog"

// logActivity records an audit entry. When action is "created", consecutive writes
// by the same user for the same entity_type within 15 minutes are merged: the
// entity_count increments and entity_name updates to the most-recent value.
// For all other actions each call always inserts a new row.
// An optional details string (first element of the variadic) provides extra context.
func logActivity(userID int, username, action, entityType, entityName string, isSensitive bool, details ...string) {
	sensitive := 0
	if isSensitive {
		sensitive = 1
	}

	det := ""
	if len(details) > 0 {
		det = details[0]
	}

	if action == "created" {
		var id int
		err := db.QueryRow(`
			SELECT id FROM activity_log
			WHERE user_id = ? AND action = 'created' AND entity_type = ?
			  AND updated_at > datetime('now', '-15 minutes')
			ORDER BY updated_at DESC LIMIT 1
		`, userID, entityType).Scan(&id)
		if err == nil {
			if _, err2 := db.Exec(`
				UPDATE activity_log
				SET entity_count = entity_count + 1,
				    entity_name  = ?,
				    updated_at   = CURRENT_TIMESTAMP
				WHERE id = ?
			`, entityName, id); err2 != nil {
				slog.Error("activity_log batch update failed", "error", err2)
			}
			return
		}
	}

	if _, err := db.Exec(`
		INSERT INTO activity_log (user_id, username, action, entity_type, entity_name, details, is_sensitive)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, userID, username, action, entityType, entityName, det, sensitive); err != nil {
		slog.Error("activity_log insert failed", "error", err)
	}
}
