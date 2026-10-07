// handlers_mobile_roster.go - the mobile roster store: member attributes a scan reads
// (troop level, squad type, profession) and roster changes an officer confirms on the
// phone (rank, rename, join, rejoin, leave). Both go through the shared roster
// primitives (roster_apply.go), so a change from the phone means what it means on the web.

package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
)

// mobileMaxBody caps a mobile JSON body (Project 15, decision 22).
const mobileMaxBody = 2 << 20

// decodeMobileJSON reads a capped JSON body into v, answering 400 itself on failure.
func decodeMobileJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, mobileMaxBody)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

// --- Attributes ---------------------------------------------------------------

type mobileAttributeRecord struct {
	MemberID   int     `json:"member_id"`
	TroopLevel *int    `json:"troop_level,omitempty"`
	SquadType  *string `json:"squad_type,omitempty"`
	Profession *string `json:"profession,omitempty"`
}

// POST /api/mobile/members/attributes (manage_members)
// Only the fields sent change. Troop level is a key of TroopTierMinHQ and never below
// the stored value (the game can't lower it); squad type and profession come from the
// member modal's lists.
func mobileMemberAttributes(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var req struct {
		Records []mobileAttributeRecord `json:"records"`
	}
	if !decodeMobileJSON(w, r, &req) {
		return
	}

	type logged struct{ name, details string }
	var logs []logged
	errs := []string{}
	updated, unchanged := 0, 0

	tx, err := db.Begin()
	if err != nil {
		dbError(w, "mobileMemberAttributes begin", err)
		return
	}
	defer tx.Rollback()

	for _, rec := range req.Records {
		var name, rank, squad, prof string
		var troop int
		err := tx.QueryRow(`SELECT name, rank, COALESCE(troop_level, 0), COALESCE(squad_type, ''), COALESCE(profession, '')
			FROM members WHERE id = ?`, rec.MemberID).Scan(&name, &rank, &troop, &squad, &prof)
		if err == sql.ErrNoRows || (err == nil && rank == "EX") {
			errs = append(errs, fmt.Sprintf("member_id %d: not an active member", rec.MemberID))
			continue
		}
		if err != nil {
			dbError(w, "mobileMemberAttributes read", err)
			return
		}
		newTroop, newSquad, newProf := troop, squad, prof
		if rec.TroopLevel != nil {
			if _, ok := TroopTierMinHQ[*rec.TroopLevel]; !ok {
				errs = append(errs, fmt.Sprintf("%s: troop level %d is not 1–11", name, *rec.TroopLevel))
				continue
			}
			if *rec.TroopLevel < troop {
				errs = append(errs, fmt.Sprintf("%s: troop level %d is below the stored %d, which the game can't lower — likely a misread", name, *rec.TroopLevel, troop))
				continue
			}
			newTroop = *rec.TroopLevel
		}
		if rec.SquadType != nil {
			if !slices.Contains(ValidSquadTypes, *rec.SquadType) {
				errs = append(errs, fmt.Sprintf("%s: squad type %q is not one of %s", name, *rec.SquadType, strings.Join(ValidSquadTypes, ", ")))
				continue
			}
			newSquad = *rec.SquadType
		}
		if rec.Profession != nil {
			if !slices.Contains(ValidProfessions, *rec.Profession) {
				errs = append(errs, fmt.Sprintf("%s: profession %q is not one of %s", name, *rec.Profession, strings.Join(ValidProfessions, ", ")))
				continue
			}
			newProf = *rec.Profession
		}
		changes := memberAttributeChanges(troop, squad, prof, newTroop, newSquad, newProf)
		if len(changes) == 0 {
			unchanged++
			continue
		}
		if _, err := tx.Exec(`UPDATE members SET troop_level = ?, squad_type = ?, profession = ? WHERE id = ?`,
			newTroop, newSquad, newProf, rec.MemberID); err != nil {
			dbError(w, "mobileMemberAttributes update", err)
			return
		}
		updated++
		logs = append(logs, logged{name, strings.Join(changes, "; ") + " · via mobile"})
	}
	if err := tx.Commit(); err != nil {
		dbError(w, "mobileMemberAttributes commit", err)
		return
	}
	for _, l := range logs {
		logActivity(user.ID, user.Username, "updated", "member", l.name, false, l.details)
	}
	writeJSON(w, map[string]any{"updated": updated, "unchanged": unchanged, "errors": errs})
}

// --- Confirmed roster changes ------------------------------------------------

type mobileRosterChange struct {
	Kind     string `json:"kind"` // rank | rename | join | rejoin | leave
	MemberID int    `json:"member_id,omitempty"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	Name     string `json:"name,omitempty"`
	Rank     string `json:"rank,omitempty"`
}

type mobileRosterResult struct {
	Index   int    `json:"index"`
	Applied bool   `json:"applied"`
	Error   string `json:"error,omitempty"`
}

// rosterRefusal is a change the roster refuses, worded for results[].
type rosterRefusal string

func (e rosterRefusal) Error() string { return string(e) }

func validActiveRank(r string) bool { return slices.Contains(ValidRanks, r) }

// POST /api/mobile/roster/changes (manage_members)
// Every change carries the value the officer saw and is refused if the roster no longer
// holds it, so a stale or blind change can't land. All run in one transaction, each
// checked against the roster as it stands when it is applied, so a batch is validated
// against itself. A refused change doesn't stop the others; a database error aborts the
// request.
func mobileRosterChanges(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var req struct {
		Changes []mobileRosterChange `json:"changes"`
	}
	if !decodeMobileJSON(w, r, &req) {
		return
	}
	actor := aliasActor{UserID: user.ID, Username: user.Username, Via: "mobile"}

	var logs []rosterLog
	results := make([]mobileRosterResult, 0, len(req.Changes))
	applied := 0

	tx, err := db.Begin()
	if err != nil {
		dbError(w, "mobileRosterChanges begin", err)
		return
	}
	defer tx.Rollback()

	for i, ch := range req.Changes {
		lg, err := applyMobileRosterChange(tx, actor, ch)
		res := mobileRosterResult{Index: i}
		var refusal rosterRefusal
		switch {
		case err == nil:
			res.Applied = true
			applied++
			logs = append(logs, lg)
		case errors.As(err, &refusal):
			res.Error = string(refusal)
		default:
			dbError(w, "mobileRosterChanges", err)
			return
		}
		results = append(results, res)
	}
	if err := tx.Commit(); err != nil {
		dbError(w, "mobileRosterChanges commit", err)
		return
	}
	for _, l := range logs {
		logActivity(user.ID, user.Username, l.action, "member", l.name, false, l.details)
	}
	writeJSON(w, map[string]any{"results": results, "applied": applied})
}

type rosterLog struct{ action, name, details string }

// rosterMember reads a member's name and rank inside tx; found=false when there is none.
func rosterMember(tx *sql.Tx, id int) (name, rank string, found bool, err error) {
	err = tx.QueryRow(`SELECT name, rank FROM members WHERE id = ?`, id).Scan(&name, &rank)
	if err == sql.ErrNoRows {
		return "", "", false, nil
	}
	return name, rank, err == nil, err
}

// memberNamed finds a member whose name equals name, ignoring case.
func memberNamed(tx *sql.Tx, name string, exceptID int) (id int, rank string, found bool, err error) {
	err = tx.QueryRow(`SELECT id, rank FROM members WHERE LOWER(name) = LOWER(?) AND id != ? ORDER BY rank = 'EX' LIMIT 1`,
		name, exceptID).Scan(&id, &rank)
	if err == sql.ErrNoRows {
		return 0, "", false, nil
	}
	return id, rank, err == nil, err
}

func applyMobileRosterChange(tx *sql.Tx, actor aliasActor, ch mobileRosterChange) (rosterLog, error) {
	switch ch.Kind {
	case "rank":
		name, rank, found, err := rosterMember(tx, ch.MemberID)
		if err != nil {
			return rosterLog{}, err
		}
		if !found || rank == "EX" {
			return rosterLog{}, rosterRefusal(fmt.Sprintf("member_id %d is not an active member", ch.MemberID))
		}
		if !validActiveRank(ch.To) {
			return rosterLog{}, rosterRefusal("rank must be R1–R5")
		}
		if rank != ch.From {
			return rosterLog{}, rosterRefusal(fmt.Sprintf("%s's rank is now %s", name, rank))
		}
		if ch.From == ch.To {
			return rosterLog{}, rosterRefusal(fmt.Sprintf("%s is already %s", name, rank))
		}
		if _, err := applyRankChangeFrom(tx, ch.MemberID, ch.From, ch.To); err != nil {
			return rosterLog{}, err
		}
		return rosterLog{"updated", name, "rank: " + ch.From + " → " + ch.To + " · via mobile"}, nil

	case "rename":
		name, _, found, err := rosterMember(tx, ch.MemberID)
		if err != nil {
			return rosterLog{}, err
		}
		if !found {
			return rosterLog{}, rosterRefusal(fmt.Sprintf("no member with id %d", ch.MemberID))
		}
		to := strings.TrimSpace(ch.To)
		if name != ch.From {
			return rosterLog{}, rosterRefusal(fmt.Sprintf("the member's name is now %q", name))
		}
		if to == "" || to == name {
			return rosterLog{}, rosterRefusal("the new name is empty or unchanged")
		}
		if otherID, otherRank, taken, err := memberNamed(tx, to, ch.MemberID); err != nil {
			return rosterLog{}, err
		} else if taken && otherRank == "EX" {
			return rosterLog{}, rosterRefusal(fmt.Sprintf("%q is a former member (id %d); use rejoin", to, otherID))
		} else if taken {
			return rosterLog{}, rosterRefusal(fmt.Sprintf("%q is already another member's name", to))
		}
		if _, err := renameMemberTx(tx, actor, ch.MemberID, to); err != nil {
			return rosterLog{}, err
		}
		return rosterLog{"updated", to, "name: " + name + " → " + to + " · via mobile"}, nil

	case "join":
		name := strings.TrimSpace(ch.Name)
		if name == "" {
			return rosterLog{}, rosterRefusal("name is required")
		}
		if !validActiveRank(ch.Rank) {
			return rosterLog{}, rosterRefusal("rank must be R1–R5")
		}
		// The name must resolve to nobody — exact, alias or folded (tiers 1–3) — so a
		// misread of an existing member can't create a duplicate.
		// The folded index is built per join: an earlier change in this batch may have
		// added or renamed a member.
		idx, err := buildFoldedNameIndex(tx, actor.UserID)
		if err != nil {
			return rosterLog{}, err
		}
		if m, _, err := resolveMemberAliasWithIndex(tx, name, actor.UserID, idx); err == nil && m != nil {
			if m.Rank == "EX" {
				return rosterLog{}, rosterRefusal(fmt.Sprintf("%q matches former member %s (id %d); use rejoin", name, m.Name, m.ID))
			}
			return rosterLog{}, rosterRefusal(fmt.Sprintf("%q matches member %s", name, m.Name))
		} else if err != nil && err != sql.ErrNoRows {
			return rosterLog{}, err
		}
		if _, err := addMemberTx(tx, newMember{Name: name, Rank: ch.Rank, Eligible: true}); err != nil {
			return rosterLog{}, err
		}
		return rosterLog{"created", name, "joined as " + ch.Rank + " · via mobile"}, nil

	case "rejoin":
		name, rank, found, err := rosterMember(tx, ch.MemberID)
		if err != nil {
			return rosterLog{}, err
		}
		if !found || rank != "EX" {
			return rosterLog{}, rosterRefusal(fmt.Sprintf("member_id %d is not a former member", ch.MemberID))
		}
		if !validActiveRank(ch.Rank) {
			return rosterLog{}, rosterRefusal("rank must be R1–R5")
		}
		if _, err := reactivateMemberTx(tx, ch.MemberID, ch.Rank); err != nil {
			return rosterLog{}, err
		}
		return rosterLog{"unarchived", name, "rejoined as " + ch.Rank + " · via mobile"}, nil

	case "leave":
		name, rank, found, err := rosterMember(tx, ch.MemberID)
		if err != nil {
			return rosterLog{}, err
		}
		if !found || rank == "EX" {
			return rosterLog{}, rosterRefusal(fmt.Sprintf("member_id %d is not an active member", ch.MemberID))
		}
		if rank != ch.From {
			return rosterLog{}, rosterRefusal(fmt.Sprintf("%s's rank is now %s", name, rank))
		}
		if _, err := applyArchive(tx, ch.MemberID, leaveReasonSeen); err != nil {
			return rosterLog{}, err
		}
		return rosterLog{"archived", name, leaveReasonSeen + " · via mobile"}, nil
	}
	slog.Debug("mobile roster: unknown change kind", "kind", ch.Kind)
	return rosterLog{}, rosterRefusal(fmt.Sprintf("unknown kind %q", ch.Kind))
}
