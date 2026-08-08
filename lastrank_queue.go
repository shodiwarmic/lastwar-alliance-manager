package main

// lastrank_queue.go — the durable LastRank Phase-1 review queue.
//
// The review modal used to be entirely ephemeral: close it and every proposal was
// gone, and a rejected rank change returned identically on the next sync. This
// makes proposals outlive the modal, gives rejections two distinct meanings, and
// gives an unattended pull somewhere safe to land.
//
// The queue holds DECISIONS, not data. Power / hero / HQ are staleness-gated,
// provenance-stamped, append-only history — nothing there needs a human.

import (
	"database/sql"
	"strconv"
	"strings"
)

// Queue kinds.
const (
	PendingKindRank      = "rank"
	PendingKindName      = "name"
	PendingKindUnmatched = "unmatched"
	PendingKindArchive   = "archive"
)

// Queue statuses.
const (
	PendingOpen          = "open"
	PendingDeferOnce     = "deferred_once"
	PendingDeferUnchange = "deferred_until_changed"
)

// lastRankSubjectKey builds the canonical identity for a proposal.
//
// A naive (member_id, lastrank_public_id) key collides: a missing upstream
// public_id decodes to 0, so two unmatched entries without one would both key on
// (0, 0) and the second would clobber the first. Member-keyed kinds use the member;
// unmatched entries use the public id when they have one and a FOLDED name when
// they don't — folded so a re-accented name doesn't mint a duplicate row.
func lastRankSubjectKey(kind string, memberID, publicID int, lastrankName string) string {
	if kind == PendingKindUnmatched {
		if publicID != 0 {
			return "p:" + strconv.Itoa(publicID)
		}
		return "n:" + foldName(lastrankName)
	}
	return "m:" + strconv.Itoa(memberID)
}

// lastRankFingerprint identifies the PROPOSAL, so "defer until it changes" can tell
// "the same suggestion again" from "a different suggestion".
func lastRankFingerprint(kind string, memberID int, proposed string) string {
	return kind + "|" + strconv.Itoa(memberID) + "|" + proposed
}

// pendingProposal is one decision a pull wants to put in front of an officer.
type pendingProposal struct {
	Kind           string
	MemberID       int
	PublicID       int
	LastRankName   string
	CurrentValue   string
	ProposedValue  string
	Reason         string
}

// reconcilePendingChanges makes the queue match what this pull proposes. It is the
// single point both the manual fetch and (later) the scheduled pull converge on.
//
// Per proposal:
//   - fingerprint CHANGED           → overwrite and re-open. This is what makes
//     "defer until it changes" mean what it says.
//   - fingerprint SAME, deferred_once, capture_date ADVANCED → re-open.
//   - fingerprint SAME, deferred_until_changed → stays deferred.
//
// Proposals absent from this pull are deleted: upstream withdrew them, so leaving
// them would ask an officer to decide something that is no longer true.
//
// Runs inside the caller's transaction, and issues no queries while a cursor is
// open — see the MaxOpenConns(1) rule in jobs.go.
func reconcilePendingChanges(tx *sql.Tx, proposals []pendingProposal, captureDate string) error {
	seen := make(map[string]bool, len(proposals))

	for _, p := range proposals {
		key := lastRankSubjectKey(p.Kind, p.MemberID, p.PublicID, p.LastRankName)
		seen[p.Kind+"\x00"+key] = true
		fp := lastRankFingerprint(p.Kind, p.MemberID, p.ProposedValue)

		var id int
		var oldFP, status, oldCapture string
		err := tx.QueryRow(`SELECT id, fingerprint, status, capture_date
			FROM lastrank_pending_changes WHERE kind = ? AND subject_key = ?`, p.Kind, key).
			Scan(&id, &oldFP, &status, &oldCapture)

		if err == sql.ErrNoRows {
			if _, err := tx.Exec(`INSERT INTO lastrank_pending_changes
				(kind, subject_key, member_id, lastrank_public_id, lastrank_name,
				 current_value, proposed_value, fingerprint, reason, capture_date, status)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'open')`,
				p.Kind, key, p.MemberID, p.PublicID, p.LastRankName,
				p.CurrentValue, p.ProposedValue, fp, p.Reason, captureDate); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}

		newStatus := status
		switch {
		case oldFP != fp:
			// A different suggestion than the one that was deferred.
			newStatus = PendingOpen
		case status == PendingDeferOnce && lastRankCaptureNewer(captureDate, oldCapture):
			// "Not now" expires when a genuinely newer pull arrives — not merely
			// when someone clicks Fetch again.
			newStatus = PendingOpen
		}

		if _, err := tx.Exec(`UPDATE lastrank_pending_changes
			SET member_id = ?, lastrank_public_id = ?, lastrank_name = ?,
			    current_value = ?, proposed_value = ?, fingerprint = ?, reason = ?,
			    capture_date = ?, status = ?, last_seen_at = CURRENT_TIMESTAMP
			WHERE id = ?`,
			p.MemberID, p.PublicID, p.LastRankName, p.CurrentValue, p.ProposedValue,
			fp, p.Reason, captureDate, newStatus, id); err != nil {
			return err
		}
	}

	// Withdraw anything this pull no longer proposes. Read the full set first, then
	// delete — never delete while iterating a cursor on the same connection.
	rows, err := tx.Query(`SELECT id, kind, subject_key FROM lastrank_pending_changes`)
	if err != nil {
		return err
	}
	var stale []int
	for rows.Next() {
		var id int
		var kind, key string
		if err := rows.Scan(&id, &kind, &key); err != nil {
			rows.Close()
			return err
		}
		if !seen[kind+"\x00"+key] {
			stale = append(stale, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, id := range stale {
		if _, err := tx.Exec(`DELETE FROM lastrank_pending_changes WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

// loadPendingChanges returns queue rows, newest-seen first. openOnly filters to
// items still awaiting a decision.
func loadPendingChanges(openOnly bool) ([]LastRankPendingChange, error) {
	q := `SELECT id, kind, member_id, lastrank_public_id, lastrank_name,
	             current_value, proposed_value, reason, capture_date, status,
	             first_seen_at, last_seen_at
	      FROM lastrank_pending_changes`
	if openOnly {
		q += ` WHERE status = 'open'`
	}
	q += ` ORDER BY kind ASC, LOWER(lastrank_name) ASC, id ASC`

	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []LastRankPendingChange{}
	for rows.Next() {
		var c LastRankPendingChange
		if err := rows.Scan(&c.ID, &c.Kind, &c.MemberID, &c.LastRankPublicID, &c.LastRankName,
			&c.CurrentValue, &c.ProposedValue, &c.Reason, &c.CaptureDate, &c.Status,
			&c.FirstSeenAt, &c.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// countOpenPending backs the dashboard card, the panel badge and the login toast.
func countOpenPending() (total int, byKind map[string]int, err error) {
	rows, qerr := db.Query(`SELECT kind, COUNT(*) FROM lastrank_pending_changes
		WHERE status = 'open' GROUP BY kind`)
	if qerr != nil {
		return 0, nil, qerr
	}
	defer rows.Close()

	byKind = map[string]int{}
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			return 0, nil, err
		}
		byKind[kind] = n
		total += n
	}
	return total, byKind, rows.Err()
}

// pendingKindLabel renders a queue kind for activity-log details.
func pendingKindLabel(kind string) string {
	switch kind {
	case PendingKindRank:
		return "rank change"
	case PendingKindName:
		return "name change"
	case PendingKindUnmatched:
		return "unmatched name"
	case PendingKindArchive:
		return "possible departure"
	}
	return strings.ReplaceAll(kind, "_", " ")
}
