// aliases.go - the only code that writes member_aliases.
//
// Every alias write in the app goes through saveAliasTx, deleteAliasesTx or
// deleteAliasByIDTx, so the scope rules and the audit trail can't drift apart between
// the VS import, the mobile commit, the Season Hub import, LastRank and the member
// modal (private-docs 195). Each change writes its own activity row inside the
// caller's transaction: the record is the point, so an alias change and its record
// land or fail together.

package app

import (
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// aliasActor is who an alias change is attributed to, and the path it came through
// ("VS import", "mobile", …), which ends the activity row's details.
type aliasActor struct {
	UserID   int
	Username string
	Via      string
}

// aliasWrite is one alias to save. Actor owns a personal alias and names the audit row.
// MayOverrideGlobal is the caller's manage_members, resolved BEFORE its transaction
// opened: userHasPermission reads through db, which would wait on the open
// transaction's connection (decision 3 of Project 15).
type aliasWrite struct {
	MemberID          int
	Alias             string
	Category          string
	Actor             aliasActor
	MayOverrideGlobal bool
}

// aliasChange describes a write that changed something: created, or updated with the
// mapping it replaced.
type aliasChange struct {
	Action       string // "created" or "updated"
	Alias        string
	MemberName   string
	Category     string
	PrevMembers  []string // members the text pointed at before, other than MemberID
	PrevCategory string   // set when the same member's alias changed category
}

// aliasRefusal is a write the rules refuse, worded for the person who asked; the
// caller reports it per item. Any other error from these helpers is a database error.
type aliasRefusal struct{ msg string }

func (e *aliasRefusal) Error() string { return e.msg }

func refuseAlias(msg string) error { return &aliasRefusal{msg: msg} }

// isAliasRefusal reports whether err is a refusal rather than a database error.
func isAliasRefusal(err error) bool {
	var r *aliasRefusal
	return errors.As(err, &r)
}

type aliasRow struct {
	id       int
	memberID int
	category string
	member   string
}

// readAliasRows reads the rows with this text (compared as resolution compares them,
// LOWER()) in the given categories — personal ones only when they belong to ownerID —
// with their members' names. The cursor is closed before it returns.
func readAliasRows(tx *sql.Tx, alias string, categories []string, ownerID int) ([]aliasRow, error) {
	q := `SELECT a.id, a.member_id, a.category, COALESCE(m.name, '')
	      FROM member_aliases a LEFT JOIN members m ON m.id = a.member_id
	      WHERE LOWER(a.alias) = LOWER(?) AND a.category IN (?` + strings.Repeat(",?", len(categories)-1) + `)`
	args := []any{alias}
	for _, c := range categories {
		args = append(args, c)
	}
	if ownerID > 0 {
		q += ` AND (a.category != 'personal' OR a.user_id = ?)`
		args = append(args, ownerID)
	} else {
		q += ` AND a.category != 'personal'`
	}
	rows, err := tx.Query(q+` ORDER BY a.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []aliasRow
	for rows.Next() {
		var r aliasRow
		if err := rows.Scan(&r.id, &r.memberID, &r.category, &r.member); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// saveAliasTx writes one alias under the scope rules (Project 15, decision 11):
//
//   - global replaces every global and OCR row with the same text — global is curated,
//     OCR a background guess;
//   - ocr replaces only OCR rows, and is refused when a global row sends the text to a
//     different member, unless MayOverrideGlobal, when it replaces that row too;
//   - personal replaces only its owner's rows.
//
// No write touches another user's personal alias. Writing a mapping that already
// exists — one row, same member and category, or for OCR a global row already sending
// the text to that member — changes nothing and returns (nil, nil). Duplicate rows for
// a text are healed to one whenever the text is saved.
func saveAliasTx(tx *sql.Tx, w aliasWrite) (*aliasChange, error) {
	w.Alias = strings.TrimSpace(w.Alias)
	if w.Alias == "" {
		return nil, refuseAlias("alias cannot be empty")
	}
	var scope []string
	switch w.Category {
	case "global":
		scope = []string{"global", "ocr"}
	case "ocr":
		scope = []string{"ocr"}
	case "personal":
		if w.Actor.UserID <= 0 {
			return nil, refuseAlias("a personal alias needs an owner")
		}
		scope = []string{"personal"}
	default:
		return nil, refuseAlias("invalid alias category \"" + w.Category + "\"")
	}
	// foreign_keys is off, so a stale id would insert happily.
	var memberName string
	if err := tx.QueryRow(`SELECT name FROM members WHERE id = ?`, w.MemberID).Scan(&memberName); err != nil {
		if err == sql.ErrNoRows {
			return nil, refuseAlias("no member with id " + strconv.Itoa(w.MemberID))
		}
		return nil, err
	}

	owner := 0
	if w.Category == "personal" {
		owner = w.Actor.UserID
	}
	existing, err := readAliasRows(tx, w.Alias, scope, owner)
	if err != nil {
		return nil, err
	}

	if w.Category == "ocr" {
		globals, err := readAliasRows(tx, w.Alias, []string{"global"}, 0)
		if err != nil {
			return nil, err
		}
		for _, g := range globals {
			if g.memberID == w.MemberID {
				return nil, nil // the global alias already sends the text there
			}
		}
		if len(globals) > 0 {
			if !w.MayOverrideGlobal {
				return nil, refuseAlias("'" + w.Alias + "' is a global alias for " + globals[0].member +
					"; change it on the Members page")
			}
			existing = append(existing, globals...)
		}
	}

	if len(existing) == 1 && existing[0].memberID == w.MemberID && existing[0].category == w.Category {
		return nil, nil
	}

	for _, r := range existing {
		if _, err := tx.Exec(`DELETE FROM member_aliases WHERE id = ?`, r.id); err != nil {
			return nil, err
		}
	}
	var ownerArg any
	if w.Category == "personal" {
		ownerArg = w.Actor.UserID
	}
	if _, err := tx.Exec(`INSERT INTO member_aliases (member_id, user_id, category, alias) VALUES (?, ?, ?, ?)`,
		w.MemberID, ownerArg, w.Category, w.Alias); err != nil {
		return nil, err
	}

	ch := &aliasChange{Action: "created", Alias: w.Alias, MemberName: memberName, Category: w.Category}
	if len(existing) > 0 {
		ch.Action = "updated"
		seen := map[string]bool{}
		for _, r := range existing {
			if r.memberID != w.MemberID {
				if !seen[r.member] {
					seen[r.member] = true
					ch.PrevMembers = append(ch.PrevMembers, r.member)
				}
			} else if r.category != w.Category && ch.PrevCategory == "" {
				ch.PrevCategory = r.category
			}
		}
		sort.Strings(ch.PrevMembers)
	}
	details := memberName + " (" + w.Category + ") · via " + w.Actor.Via
	if len(ch.PrevMembers) > 0 {
		details += "; re-pointed from " + strings.Join(ch.PrevMembers, ", ")
	} else if ch.PrevCategory != "" {
		details += "; category " + ch.PrevCategory + " → " + w.Category
	}
	if err := logActivityTx(tx, w.Actor.UserID, w.Actor.Username, ch.Action, "alias", w.Alias, false, details); err != nil {
		return nil, err
	}
	return ch, nil
}

// deleteAliasesTx deletes every non-personal row with this text in the given
// categories, logging one row per deletion, and returns how many went.
func deleteAliasesTx(tx *sql.Tx, alias string, categories []string, actor aliasActor) (int, error) {
	rows, err := readAliasRows(tx, strings.TrimSpace(alias), categories, 0)
	if err != nil {
		return 0, err
	}
	for _, r := range rows {
		if err := deleteAliasRowTx(tx, r, alias, actor); err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}

// deleteAliasByIDTx deletes one alias row (the member modal's delete). The caller has
// already checked the user may.
func deleteAliasByIDTx(tx *sql.Tx, id int, actor aliasActor) error {
	var r aliasRow
	var text string
	err := tx.QueryRow(`SELECT a.id, a.member_id, a.category, COALESCE(m.name, ''), a.alias
		FROM member_aliases a LEFT JOIN members m ON m.id = a.member_id WHERE a.id = ?`, id).
		Scan(&r.id, &r.memberID, &r.category, &r.member, &text)
	if err != nil {
		return err
	}
	return deleteAliasRowTx(tx, r, text, actor)
}

func deleteAliasRowTx(tx *sql.Tx, r aliasRow, text string, actor aliasActor) error {
	if _, err := tx.Exec(`DELETE FROM member_aliases WHERE id = ?`, r.id); err != nil {
		return err
	}
	return logActivityTx(tx, actor.UserID, actor.Username, "deleted", "alias", text, false,
		r.member+" ("+r.category+") · via "+actor.Via)
}
