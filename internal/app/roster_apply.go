// roster_apply.go — the roster write primitives: rank change, rename, archive, add and
// rejoin.
//
// Shared by the web member paths, LastRank (its commit and its review queue) and the
// mobile roster route, so "rename a member" or "archive a member" means one thing
// wherever it is asked for. Moved out of lastrank_apply.go, whose LastRank-specific half
// stays behind; when LastRank goes (private-docs 157) these stay.
//
// Each helper takes the caller's transaction (or db, for the single-statement ones),
// does one change, and reports whether it changed anything. None of them write an
// activity row for the member change itself — callers know the wording and the batch —
// except that a rename's alias changes are logged by the alias helper inside tx.

package app

import (
	"database/sql"
	"strconv"
	"strings"
)

// applyRankChange sets a member's rank. Reports false when the member already has it,
// which is how a queued proposal that reality overtook resolves as superseded rather
// than as a spurious "applied".
func applyRankChange(tx execer, memberID int, newRank string) (bool, error) {
	if newRank == "" || memberID == 0 {
		return false, nil
	}
	res, err := tx.Exec(`UPDATE members SET rank = ? WHERE id = ? AND rank != ?`, newRank, memberID, newRank)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// applyRankChangeFrom is applyRankChange guarded on the rank the caller saw: it changes
// nothing (false) unless the member's rank is still from.
func applyRankChangeFrom(tx execer, memberID int, from, to string) (bool, error) {
	if to == "" || memberID == 0 {
		return false, nil
	}
	res, err := tx.Exec(`UPDATE members SET rank = ? WHERE id = ? AND rank = ? AND rank != 'EX'`, to, memberID, from)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// renameMemberTx renames a member. The old name becomes a global alias (through the one
// alias helper, so it re-points an existing alias rather than duplicating it), so
// historical imports and OCR that still use it keep resolving; an OCR alias spelling
// the new name is dropped, so the name isn't both a primary and a background guess. A
// change of case alone writes no alias. Reports false when the member doesn't exist or
// already has the name.
func renameMemberTx(tx *sql.Tx, actor aliasActor, memberID int, newName string) (bool, error) {
	newName = strings.TrimSpace(newName)
	if memberID == 0 || newName == "" {
		return false, nil
	}
	var oldName string
	if err := tx.QueryRow(`SELECT name FROM members WHERE id = ?`, memberID).Scan(&oldName); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	if oldName == newName {
		return false, nil
	}
	if _, err := tx.Exec(`UPDATE members SET name = ? WHERE id = ?`, newName, memberID); err != nil {
		return false, err
	}
	if oldName != "" && !strings.EqualFold(oldName, newName) {
		if _, err := saveAliasTx(tx, aliasWrite{MemberID: memberID, Alias: oldName, Category: "global", Actor: actor}); err != nil {
			return false, err
		}
	}
	if _, err := deleteAliasesTx(tx, newName, []string{"ocr"}, actor); err != nil {
		return false, err
	}
	return true, nil
}

// Leave reasons the roster primitives store for a departure no officer typed.
const (
	leaveReasonLastRank = "Left alliance (via LastRank)"
	leaveReasonSeen     = "Left alliance (seen in game)"
)

// applyArchive marks a member as departed with the given reason. The guard on
// rank != 'EX' means archiving someone already archived reports false rather than
// inflating counts.
func applyArchive(tx execer, memberID int, reason string) (bool, error) {
	if memberID == 0 {
		return false, nil
	}
	res, err := tx.Exec(`UPDATE members SET rank = 'EX', eligible = 0, leave_reason = ?
		WHERE id = ? AND rank != 'EX'`, reason, memberID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// reactivateMemberTx brings a former member back at rank. False when the member isn't
// archived.
func reactivateMemberTx(tx execer, memberID int, rank string) (bool, error) {
	res, err := tx.Exec(`UPDATE members SET rank = ?, eligible = 1 WHERE id = ? AND rank = 'EX'`, rank, memberID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// newMember is a member to add. JoinedAt "" means today's game date.
type newMember struct {
	Name             string
	Rank             string
	Eligible         bool
	SquadType        string
	TroopLevel       int
	Profession       string
	JoinedAt         string
	LastRankPublicID int
}

// addMemberTx inserts a member and returns its id.
func addMemberTx(tx execer, m newMember) (int, error) {
	if m.JoinedAt == "" {
		m.JoinedAt = gameDate()
	}
	var pubID any
	if m.LastRankPublicID != 0 {
		pubID = m.LastRankPublicID
	}
	res, err := tx.Exec(`INSERT INTO members (name, rank, eligible, squad_type, troop_level, profession, joined_at, lastrank_public_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, m.Name, m.Rank, m.Eligible, m.SquadType, m.TroopLevel, m.Profession, m.JoinedAt, pubID)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

// memberAttributeChanges words a change to troop level, squad type or profession for the
// activity log, as the member modal always has. A blank new squad or profession is "not
// sent" and is not reported.
func memberAttributeChanges(oldTroop int, oldSquad, oldProf string, newTroop int, newSquad, newProf string) []string {
	var changes []string
	if oldTroop != newTroop {
		changes = append(changes, "troop level: "+strconv.Itoa(oldTroop)+" → "+strconv.Itoa(newTroop))
	}
	if oldProf != newProf && newProf != "" {
		changes = append(changes, "profession: "+oldProf+" → "+newProf)
	}
	if oldSquad != newSquad && newSquad != "" {
		changes = append(changes, "squad: "+oldSquad+" → "+newSquad)
	}
	return changes
}
