// activity.go - Audit log helper. Writes activity_log entries with create-batching.

package app

import (
	"database/sql"
	"log/slog"
)

// neverBatched lists entity types whose "created" events must always get their own
// row, overriding the 15-minute merge below.
//
// Batching keeps the feed readable for bulk data entry, but it overwrites entity_name
// with the most recent value and only bumps a counter — so issuing three reset links in
// a row collapsed to one row reading "created 3 password reset links" naming only the
// last user. For credential-granting actions that is the wrong trade: the audit trail
// exists precisely to answer "who was given access, and by whom", and that question
// becomes unanswerable the moment two are issued together.
//
// The participation types are here for the audit trail's other question, "which
// event": two boards recorded in fifteen minutes are two events, and five excusals in
// a row are five members, each of which a merged row would reduce to the last one.
var neverBatched = map[string]bool{
	"password_reset_link":     true,
	"invite":                  true,
	"participation_board":     true,
	"participation_exception": true,
	// One row per alias change: a bulk save collapsed into one row naming only the
	// last alias, which is the record private-docs 195 exists to keep.
	"alias": true,
}

// logActivity records an audit entry. When action is "created", consecutive writes
// by the same user for the same entity_type within 15 minutes are merged: the
// entity_count increments and entity_name updates to the most-recent value — unless
// the entity_type is in neverBatched, which always gets its own row.
// For all other actions each call always inserts a new row.
// An optional details string (first element of the variadic) provides extra context.
func logActivity(userID int, username, action, entityType, entityName string, isSensitive bool, details ...string) {
	if err := writeActivity(db, userID, username, action, entityType, entityName, isSensitive, details...); err != nil {
		slog.Error("activity_log write failed", "error", err)
	}
}

// logActivityTx is logActivity inside the caller's transaction, for a change whose
// record must commit or fail with it (alias writes). logActivity itself can't be used
// there: it goes through db, which waits on the open transaction's connection. The
// error is returned so the caller rolls the change back with its record.
func logActivityTx(tx *sql.Tx, userID int, username, action, entityType, entityName string, isSensitive bool, details ...string) error {
	return writeActivity(tx, userID, username, action, entityType, entityName, isSensitive, details...)
}

// activityWriter is the subset of db and *sql.Tx the activity log writes through.
type activityWriter interface {
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

func writeActivity(q activityWriter, userID int, username, action, entityType, entityName string, isSensitive bool, details ...string) error {
	sensitive := 0
	if isSensitive {
		sensitive = 1
	}

	det := ""
	if len(details) > 0 {
		det = details[0]
	}

	// Batching is per-user; a scheduled run has no user, and `user_id = NULL` never
	// matches anyway. Skipping explicitly keeps the intent visible.
	if action == "created" && !neverBatched[entityType] && userID > 0 {
		var id int
		err := q.QueryRow(`
			SELECT id FROM activity_log
			WHERE user_id = ? AND action = 'created' AND entity_type = ?
			  AND updated_at > datetime('now', '-15 minutes')
			ORDER BY updated_at DESC LIMIT 1
		`, userID, entityType).Scan(&id)
		if err == nil {
			_, err2 := q.Exec(`
				UPDATE activity_log
				SET entity_count = entity_count + 1,
				    entity_name  = ?,
				    updated_at   = CURRENT_TIMESTAMP
				WHERE id = ?
			`, entityName, id)
			return err2
		}
	}

	// A non-positive id means "not a user" — the scheduler, which has no session.
	// Write NULL rather than 0: activity_log.user_id references users(id), where 0
	// never exists, and a sentinel would be a dangling reference that any future
	// join or FK enforcement would trip over. ActivityLog.UserID is already *int,
	// so the read path handles it.
	var actor any
	if userID > 0 {
		actor = userID
	}
	_, err := q.Exec(`
		INSERT INTO activity_log (user_id, username, action, entity_type, entity_name, details, is_sensitive)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, actor, username, action, entityType, entityName, det, sensitive)
	return err
}
