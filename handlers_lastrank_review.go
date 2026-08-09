package main

// handlers_lastrank_review.go — HTTP surface for the durable Phase-1 review queue.
//
// Two entry points, deliberately separate:
//   GET  /api/lastrank/review          the backlog, with NO upstream call
//   POST /api/lastrank/review/action   apply or defer queued decisions
//
// The read costs nothing upstream on purpose. An officer working through a
// backlog shouldn't have to spend a request against a volunteer-run service just
// to see what is waiting — and shouldn't have to re-fetch to pick up where they
// left off.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

// errBulkUnmatched rejects a batch mixing an unmatched name with anything else.
// Resolving one needs a per-row target, and req carries only one.
var errBulkUnmatched = errors.New("unmatched names must be resolved one at a time")

// getLastRankReview serves the queue. openOnly=false includes deferred rows so an
// officer can find something they parked and change their mind.
func getLastRankReview(w http.ResponseWriter, r *http.Request) {
	openOnly := r.URL.Query().Get("all") != "true"
	items, err := loadPendingChanges(openOnly)
	if err != nil {
		slog.Error("lastrank review load failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	total, byKind, err := countOpenPending()
	if err != nil {
		slog.Error("lastrank review count failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"items": items, "open_count": total, "by_kind": byKind})
}

// getLastRankReviewSummary backs the dashboard card, the Members-panel badge and
// the login toast — a count plus a few examples, without shipping the whole queue.
func getLastRankReviewSummary(w http.ResponseWriter, r *http.Request) {
	total, byKind, err := countOpenPending()
	if err != nil {
		slog.Error("lastrank review summary failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	items, err := loadPendingChanges(true)
	if err != nil {
		slog.Error("lastrank review summary items failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if len(items) > 5 {
		items = items[:5]
	}
	var lastSynced string
	db.QueryRow(`SELECT COALESCE(MAX(last_seen_at), '') FROM lastrank_pending_changes`).Scan(&lastSynced)

	writeJSON(w, map[string]any{
		"open_count":  total,
		"by_kind":     byKind,
		"top":         items,
		"last_synced": lastSynced,
	})
}

// lastRankReviewAction applies or defers queued decisions.
//
// APPLY RE-VALIDATES. A queued proposal can be days old, and the world moves on:
// an officer may have already set that rank by hand, or archived the member, or
// the upstream name may have changed again. Every apply runs through the same
// helpers the live modal uses, and a proposal that changes nothing resolves as
// "superseded" rather than reporting a phantom success.
func lastRankReviewAction(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	var req LastRankReviewActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.IDs) == 0 {
		http.Error(w, "No items selected", http.StatusBadRequest)
		return
	}

	switch req.Action {
	case "defer_once", "defer_until_changed":
		status := PendingDeferOnce
		if req.Action == "defer_until_changed" {
			status = PendingDeferUnchange
		}
		n, err := deferPendingChanges(req.IDs, status, user.ID)
		if err != nil {
			slog.Error("lastrank review defer failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if n > 0 {
			logActivity(user.ID, user.Username, "deferred", "lastrank_review",
				strconv.Itoa(n)+" item(s)", false, deferDetail(req.Action, n))
		}
		writeJSON(w, map[string]any{"deferred": n})

	case "apply":
		res, err := applyPendingChanges(req, user)
		if err != nil {
			if errors.Is(err, errBulkUnmatched) {
				http.Error(w, "Resolve unmatched names one at a time — each needs its own target member.", http.StatusBadRequest)
				return
			}
			slog.Error("lastrank review apply failed", "error", err)
			http.Error(w, "Could not apply the selected changes", http.StatusInternalServerError)
			return
		}
		writeJSON(w, res)

	default:
		http.Error(w, "Unknown action", http.StatusBadRequest)
	}
}

func deferDetail(action string, n int) string {
	if action == "defer_until_changed" {
		return strconv.Itoa(n) + " hidden until LastRank proposes something different"
	}
	return strconv.Itoa(n) + " hidden until a newer LastRank pull"
}

// deferPendingChanges parks items without applying them.
func deferPendingChanges(ids []int, status string, userID int) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var actor any
	if userID > 0 {
		actor = userID
	}
	n := 0
	for _, id := range ids {
		// Guarded on the status CHANGING, not on it being 'open': an officer moving
		// a row from "not now" to "not until it changes" is a real edit, and an
		// open-only guard would silently no-op it. Re-applying the same deferral is
		// still a no-op, which is what the count should say.
		res, err := tx.Exec(`UPDATE lastrank_pending_changes
			SET status = ?, deferred_by_user_id = ?, deferred_at = CURRENT_TIMESTAMP
			WHERE id = ? AND status != ?`, status, actor, id, status)
		if err != nil {
			return 0, err
		}
		if c, _ := res.RowsAffected(); c > 0 {
			n++
		}
	}
	return n, tx.Commit()
}

// applyPendingChanges resolves queued decisions for real.
//
// Rows are read into memory BEFORE any write: the single database connection
// cannot serve a query while a cursor over the same table is open, and this loop
// both reads the queue and deletes from it.
func applyPendingChanges(req LastRankReviewActionRequest, user *AuthUser) (map[string]any, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	pending, err := loadPendingByIDsTx(tx, req.IDs)
	if err != nil {
		return nil, err
	}

	// Resolving an unmatched name needs a per-row choice — which member, or "add
	// as new". req.Resolve/MemberID describe ONE such choice, so applying them
	// across a batch would silently alias several different LastRank names to the
	// same member. Bulk apply is for kinds that carry their own answer.
	for _, p := range pending {
		if p.Kind == PendingKindUnmatched && len(pending) > 1 {
			return nil, errBulkUnmatched
		}
	}

	applied, superseded, needsInput := 0, 0, 0
	var summary []string

	for _, p := range pending {
		var changed bool
		var err error
		resolved := true

		switch p.Kind {
		case PendingKindRank:
			changed, err = applyRankChange(tx, p.MemberID, p.ProposedValue)
		case PendingKindName:
			// resolve picks rename vs alias; default to alias, the reversible one.
			action := req.Resolve
			if action != "rename" {
				action = "alias"
			}
			changed, err = applyNameChange(tx, p.MemberID, action, p.ProposedValue)
		case PendingKindArchive:
			changed, err = applyArchive(tx, p.MemberID)
		case PendingKindUnmatched:
			// Without a choice there is nothing to apply. Leave the row queued —
			// dropping it would lose the decision entirely — and report it
			// separately from "superseded", which means the opposite.
			if req.Resolve == "" || (req.Resolve != "add" && req.MemberID == 0) {
				needsInput++
				resolved = false
				break
			}
			var out unmatchedOutcome
			out, err = applyUnmatchedAction(tx, LastRankUnmatchedAction{
				LastRankName:     p.LastRankName,
				LastRankPublicID: p.LastRankPublicID,
				Action:           req.Resolve,
				MemberID:         req.MemberID,
				NewRank:          req.NewRank,
				JoinedAt:         req.JoinedAt,
			}, "", p.CaptureDate)
			changed = out.Aliased || out.Renamed || out.Added
		}
		if err != nil {
			return nil, err
		}
		if !resolved {
			continue
		}

		if changed {
			applied++
			summary = append(summary, pendingKindLabel(p.Kind)+" for "+p.displayName())
		} else {
			// Nothing to do: the world already matches the proposal, or the officer
			// resolved it by hand. Not a failure — just no longer a decision.
			superseded++
		}

		if _, derr := tx.Exec(`DELETE FROM lastrank_pending_changes WHERE id = ?`, p.ID); derr != nil {
			return nil, derr
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	if applied > 0 {
		detail := strings.Join(summary, "; ")
		if len(detail) > 400 {
			detail = detail[:397] + "…"
		}
		logActivity(user.ID, user.Username, "accepted", "lastrank_review",
			strconv.Itoa(applied)+" change(s)", false, detail)
	}
	return map[string]any{
		"applied":     applied,
		"superseded":  superseded,
		"needs_input": needsInput,
	}, nil
}

// loadPendingByIDsTx reads the selected rows fully before the caller writes.
func loadPendingByIDsTx(tx *sql.Tx, ids []int) ([]LastRankPendingChange, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	q := `SELECT id, kind, member_id, lastrank_public_id, lastrank_name,
	             current_value, proposed_value, reason, capture_date, status
	      FROM lastrank_pending_changes WHERE id IN (?` + strings.Repeat(",?", len(ids)-1) + `)`
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := tx.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LastRankPendingChange
	for rows.Next() {
		var c LastRankPendingChange
		if err := rows.Scan(&c.ID, &c.Kind, &c.MemberID, &c.LastRankPublicID, &c.LastRankName,
			&c.CurrentValue, &c.ProposedValue, &c.Reason, &c.CaptureDate, &c.Status); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// displayName prefers the roster name we recorded, falling back to the LastRank one.
func (c LastRankPendingChange) displayName() string {
	if c.CurrentValue != "" && c.Kind != PendingKindRank {
		return c.CurrentValue
	}
	if c.LastRankName != "" {
		return c.LastRankName
	}
	return "member #" + strconv.Itoa(c.MemberID)
}
