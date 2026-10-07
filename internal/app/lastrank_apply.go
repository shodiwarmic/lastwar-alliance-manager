package app

// lastrank_apply.go — the write primitives for LastRank Phase-1 decisions.
//
// Extracted from lastRankCommit so the live review modal and the durable queue
// share ONE implementation. Two code paths that both "apply a rank change" would
// inevitably drift, and the queue path is the one nobody watches.
//
// Each helper takes the caller's transaction, does exactly one decision, and
// reports whether it changed anything. The member writes themselves — rank change,
// rename, archive, add — are the shared roster primitives in roster_apply.go. None of
// them log the member change — callers summarise a whole batch into a single activity
// row — though alias changes are logged by the alias helper inside tx.

import (
	"database/sql"
	"strings"
)

// applyNameChange renames a member or records the new name as a global alias.
//
// On rename the OLD name becomes a global alias, so historical imports and OCR
// that still use it keep resolving. The new primary may itself have been matched
// via an OCR alias, which is now redundant — dropped, so the name isn't
// simultaneously a member's primary and a background-guessed alias.
func applyNameChange(tx *sql.Tx, actor aliasActor, memberID int, action, newName string) (bool, error) {
	if memberID == 0 || strings.TrimSpace(newName) == "" {
		return false, nil
	}
	switch action {
	case "rename":
		return renameMemberTx(tx, actor, memberID, newName)
	case "alias":
		if err := addGlobalAliasOverwritingOCR(tx, actor, memberID, newName); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// stampLastRankIdentity records the durable public_id link and advances
// lastrank_synced_at.
//
// Always called after resolving a member, even when nothing else changed: the
// public id survives an in-game rename, which is what stops the same player
// landing in "unmatched" every sync. Written via strftime in SQL at millisecond
// precision — the extended sync orders on this, and a Go time.Time would write a
// local-zone string that sorts against CURRENT_TIMESTAMP's UTC form lexically.
func stampLastRankIdentity(tx *sql.Tx, memberID, publicID int) {
	if memberID == 0 {
		return
	}
	if publicID != 0 {
		tx.Exec(`UPDATE members SET lastrank_public_id = ?, lastrank_synced_at = strftime('%Y-%m-%d %H:%M:%f','now') WHERE id = ?`,
			publicID, memberID)
		return
	}
	tx.Exec(`UPDATE members SET lastrank_synced_at = strftime('%Y-%m-%d %H:%M:%f','now') WHERE id = ?`, memberID)
}

// unmatchedOutcome reports what applyUnmatchedAction did, so a caller can build an
// accurate summary without re-deriving it.
type unmatchedOutcome struct {
	Aliased  bool
	Renamed  bool
	Added    bool
	MemberID int // resolved or newly created member
	// History rows written by the paired-stats pass, so a caller's summary counts
	// them rather than silently under-reporting the import.
	PowerRows int
	HeroRows  int
	HQRows    int
}

// applyUnmatchedAction resolves a LastRank name that matched nobody: map it to a
// member as a global alias, rename a member to it, or add a new member.
//
// applyStats carries the entry's power/hero/HQ onto the resolved member. It is
// gated server-side by lastRankApplyPairedStats regardless of what the client
// asked for, so accepting a pairing can never overwrite fresher local data.
func applyUnmatchedAction(tx *sql.Tx, actor aliasActor, act LastRankUnmatchedAction, recordedAt, captureDate string) (unmatchedOutcome, error) {
	var out unmatchedOutcome

	switch act.Action {
	case "alias":
		if act.MemberID == 0 {
			return out, nil
		}
		if err := addGlobalAliasOverwritingOCR(tx, actor, act.MemberID, act.LastRankName); err != nil {
			return out, err
		}
		out.Aliased, out.MemberID = true, act.MemberID

	case "rename":
		if act.MemberID == 0 {
			return out, nil
		}
		ok, err := renameMemberTx(tx, actor, act.MemberID, act.LastRankName)
		if err != nil {
			return out, err
		}
		if !ok {
			return out, nil
		}
		out.Renamed, out.MemberID = true, act.MemberID

	case "add":
		rank := act.NewRank
		if rank == "" {
			rank = "R1"
		}
		// Blank or unparseable → today's GAME date (UTC−2), matching how every
		// other join date in the app is defaulted.
		joinDate := ""
		if act.JoinedAt != "" {
			if d, err := parseDate(act.JoinedAt); err == nil {
				joinDate = d.Format("2006-01-02")
			}
		}
		id, err := addMemberTx(tx, newMember{Name: act.LastRankName, Rank: rank, Eligible: true,
			JoinedAt: joinDate, LastRankPublicID: act.LastRankPublicID})
		if err != nil {
			return out, err
		}
		out.Added, out.MemberID = true, id

	default: // "ignore" or unknown
		return out, nil
	}

	if out.MemberID != 0 {
		stampLastRankIdentity(tx, out.MemberID, act.LastRankPublicID)
		if act.ApplyStats {
			out.PowerRows, out.HeroRows, out.HQRows = lastRankApplyPairedStats(
				tx, out.MemberID, act.Power, act.HeroPower, act.BaseLevel, recordedAt, captureDate)
		}
	}
	return out, nil
}
