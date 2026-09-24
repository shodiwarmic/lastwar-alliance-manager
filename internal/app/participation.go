package app

import (
	"database/sql"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// participation.go — the event participation framework (#13): the declared
// per-type configuration, the ONE function that decides a member's status on a
// board, and the readers every surface shares.
//
// Shape of every read: load (short queries, each cursor drained and closed) →
// derive in Go → write. Nothing here holds a cursor across another statement —
// one connection (database.go) turns that into a hang.
//
// Status is never stored. A board stores what the mail said (entries + values),
// the roles for that battle, and the human judgements (exceptions); deriveBoard
// turns those into present / zero / missed / excused under the type's rule.
// Suggestions are derived again on every read, so recording, excusing, dismissing
// or striking never leaves a stale list behind.

// Absence rules, one per tracked type (migration 080 explains each).
const (
	ruleAbsent = "absent"
	ruleZero   = "zero"
	ruleRole   = "role"
)

type ptTrackable struct {
	ID        int    `json:"id"`
	Key       string `json:"key"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
}

type ptType struct {
	EventTypeID int           `json:"event_type_id"`
	Name        string        `json:"name"`
	Short       string        `json:"short_name"`
	Icon        string        `json:"icon"`
	AbsenceRule string        `json:"absence_rule"`
	StrikeType  string        `json:"strike_type"`
	StrikeLabel string        `json:"strike_label"`
	Trackables  []ptTrackable `json:"trackables"`
}

// primaryKey is the trackable the 'zero' rule reads: the lowest sort_order.
func (t *ptType) primaryKey() string {
	if t == nil || len(t.Trackables) == 0 {
		return ""
	}
	return t.Trackables[0].Key
}

func (t *ptType) trackableIDs() map[string]int {
	m := make(map[string]int, len(t.Trackables))
	for _, tr := range t.Trackables {
		m[tr.Key] = tr.ID
	}
	return m
}

type ptEvent struct {
	ID          int     `json:"id"`
	EventDate   string  `json:"event_date"`
	EventTime   string  `json:"event_time"`
	AllDay      bool    `json:"all_day"`
	EventTypeID int     `json:"event_type_id"`
	TypeName    string  `json:"type_name"`
	TypeShort   string  `json:"type_short"`
	TypeIcon    string  `json:"type_icon"`
	Level       *int    `json:"level"`
	TaskForce   *string `json:"task_force"` // Desert Storm; NULL on legacy battles
}

type ptBoard struct {
	ID         int             `json:"id"`
	Source     string          `json:"source"`
	Notes      string          `json:"notes"`
	Result     json.RawMessage `json:"result"`
	RecordedBy string          `json:"recorded_by"`
	CreatedAt  string          `json:"created_at"`
	UpdatedAt  string          `json:"updated_at"`
}

// ptEntry is one row of the mail's ranked list. Name is what the board said;
// MemberName is the roster's current name for the matched member, if any.
type ptEntry struct {
	ID         int               `json:"-"`
	MemberID   *int              `json:"member_id"`
	MemberName string            `json:"member_name,omitempty"`
	Name       string            `json:"name"`
	Rank       *int              `json:"rank"`
	Values     map[string]*int64 `json:"values"`
}

type ptRole struct {
	MemberID   int    `json:"member_id"`
	MemberName string `json:"member_name"`
	Role       string `json:"role"`       // starter | sub | "" (legacy)
	TaskForce  string `json:"task_force"` // A | B | ""
}

type ptException struct {
	MemberID   int    `json:"member_id"`
	MemberName string `json:"member_name"`
	Kind       string `json:"kind"` // excused | dismissed | missed
	Reason     string `json:"reason"`
	RecordedBy string `json:"recorded_by"`
	CreatedAt  string `json:"created_at"`
}

type ptRosterMember struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Rank     string `json:"rank"`
	JoinedAt string `json:"-"`
}

// ptStatus is a member's derived standing on one board.
type ptStatus struct {
	MemberID  int               `json:"member_id"`
	Name      string            `json:"name"`
	Status    string            `json:"status"` // present | zero | missed | excused
	Rank      *int              `json:"rank"`
	Values    map[string]*int64 `json:"values,omitempty"`
	Role      string            `json:"role,omitempty"`
	TaskForce string            `json:"task_force,omitempty"`
	Reason    string            `json:"reason,omitempty"`
	Dismissed bool              `json:"dismissed,omitempty"`
	// Struck: a strike of the board's category already exists for this date, which
	// is why a missed member may not be suggested. Set on the detail read only.
	Struck bool `json:"struck,omitempty"`
}

// ptBoardData is one occurrence with everything recorded against it. Board is nil
// for an occurrence nobody has recorded — the normal case, not a gap.
type ptBoardData struct {
	Event      ptEvent
	Board      *ptBoard
	Type       *ptType
	Entries    []ptEntry
	Roles      []ptRole
	Exceptions []ptException
}

// eligibleRoster is who an 'absent' board can call missing: active members who had
// joined by the event date. A member who has since left (rank EX) is not listed on a
// historic board — the app records no departure date, a stated limit rather than a
// guess. Only the 'absent' rule reads it.
func eligibleRoster(roster []ptRosterMember, eventDate string) []ptRosterMember {
	out := make([]ptRosterMember, 0, len(roster))
	for _, m := range roster {
		if m.Rank == "EX" {
			continue
		}
		if j := m.JoinedAt; len(j) >= 10 && j[:10] > eventDate {
			continue
		}
		out = append(out, m)
	}
	return out
}

// deriveBoard is the single implementation of "what does this board say about each
// member". Nothing else decides status.
//
//   - present: an entry matched to the member. Under 'zero', an entry whose primary
//     value is PRESENT AND 0 is 'zero'; a missing value is not a zero.
//   - missed:  'absent' → every roster member with no entry; 'role' → every starter
//     with no entry; any rule → a stored 'missed' exception (legacy) with no entry.
//   - excused: an excuse overrides anything that is not present.
//   - dismissed keeps the derived status and only takes the member off the
//     suggestion list.
//
// A member the rule says nothing about — absent under 'zero', an absent sub — is
// not listed at all: they were not expected, so the board has nothing to say.
func deriveBoard(rule, primaryKey string, roster []ptRosterMember, entries []ptEntry,
	roles []ptRole, exceptions []ptException) []ptStatus {

	byMember := map[int]*ptStatus{}
	entered := map[int]bool{}
	get := func(id int, name string) *ptStatus {
		s := byMember[id]
		if s == nil {
			s = &ptStatus{MemberID: id, Name: name}
			byMember[id] = s
		}
		return s
	}

	for _, e := range entries {
		if e.MemberID == nil {
			continue // never matched: on the board, about nobody
		}
		name := e.MemberName
		if name == "" {
			name = e.Name
		}
		s := get(*e.MemberID, name)
		entered[*e.MemberID] = true
		s.Status, s.Rank, s.Values = "present", e.Rank, e.Values
		if rule == ruleZero && primaryKey != "" {
			if v := e.Values[primaryKey]; v != nil && *v == 0 {
				s.Status = "zero"
			}
		}
	}

	switch rule {
	case ruleAbsent:
		for _, m := range roster {
			if !entered[m.ID] {
				get(m.ID, m.Name).Status = "missed"
			}
		}
	case ruleRole:
		for _, r := range roles {
			if r.Role == "starter" && !entered[r.MemberID] {
				get(r.MemberID, r.MemberName).Status = "missed"
			}
		}
	}

	for _, x := range exceptions {
		switch x.Kind {
		case "missed":
			if !entered[x.MemberID] {
				get(x.MemberID, x.MemberName).Status = "missed"
			}
		}
	}
	for _, x := range exceptions {
		switch x.Kind {
		case "excused":
			s := get(x.MemberID, x.MemberName)
			if s.Status != "present" {
				s.Status, s.Reason = "excused", x.Reason
			}
		case "dismissed":
			if s := byMember[x.MemberID]; s != nil {
				s.Dismissed = true
			}
		}
	}

	for _, r := range roles {
		if s := byMember[r.MemberID]; s != nil {
			s.Role, s.TaskForce = r.Role, r.TaskForce
		}
	}

	out := make([]ptStatus, 0, len(byMember))
	for _, s := range byMember {
		if s.Status == "" {
			continue // a dismissal or role for someone the board says nothing about
		}
		out = append(out, *s)
	}
	sortStatuses(out)
	return out
}

// Board order: ranked entries by rank, then unranked entries, then everyone the
// board lists without an entry; names break ties.
func sortStatuses(ss []ptStatus) {
	group := func(s ptStatus) int {
		switch {
		case (s.Status == "present" || s.Status == "zero") && s.Rank != nil:
			return 0
		case s.Status == "present" || s.Status == "zero":
			return 1
		case s.Status == "missed":
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(ss, func(i, j int) bool {
		gi, gj := group(ss[i]), group(ss[j])
		if gi != gj {
			return gi < gj
		}
		if gi == 0 && *ss[i].Rank != *ss[j].Rank {
			return *ss[i].Rank < *ss[j].Rank
		}
		return strings.ToLower(ss[i].Name) < strings.ToLower(ss[j].Name)
	})
}

// struckKey identifies a strike the way confirmation de-duplicates it.
func struckKey(memberID int, strikeType, refDate string) string {
	return strings.Join([]string{strconv.Itoa(memberID), strikeType, refDate}, "|")
}

// boardSuggestions is the strike list a board produces: missed and zero, minus
// anyone dismissed or excused, minus anyone who already has a strike of this
// type for this date (active or excused — an excused strike was a decision too).
func boardSuggestions(statuses []ptStatus, strikeType, eventDate string, struck map[string]bool) []ptStatus {
	out := []ptStatus{}
	for _, s := range statuses {
		if s.Status != "missed" && s.Status != "zero" {
			continue
		}
		if s.Dismissed || struck[struckKey(s.MemberID, strikeType, eventDate)] {
			continue
		}
		out = append(out, s)
	}
	return out
}

// derive runs deriveBoard for one loaded board.
func (bd *ptBoardData) derive(roster []ptRosterMember) []ptStatus {
	if bd.Type == nil || bd.Board == nil {
		return []ptStatus{}
	}
	var eligible []ptRosterMember
	if bd.Type.AbsenceRule == ruleAbsent {
		eligible = eligibleRoster(roster, bd.Event.EventDate)
	}
	return deriveBoard(bd.Type.AbsenceRule, bd.Type.primaryKey(), eligible, bd.Entries, bd.Roles, bd.Exceptions)
}

// ---------------------------------------------------------------------------
// Readers. q is db or a tx.
// ---------------------------------------------------------------------------

// loadParticipationTypes returns every tracked type, keyed by event_type_id.
func loadParticipationTypes(q rowQueryer) (map[int]*ptType, error) {
	rows, err := q.Query(`
		SELECT pt.event_type_id, t.name, t.short_name, t.icon, pt.absence_rule, pt.strike_type,
		       COALESCE(st.label, pt.strike_type)
		FROM participation_types pt
		JOIN schedule_event_types t ON t.id = pt.event_type_id
		LEFT JOIN strike_types st ON st.key = pt.strike_type`)
	if err != nil {
		return nil, err
	}
	types := map[int]*ptType{}
	for rows.Next() {
		t := &ptType{Trackables: []ptTrackable{}}
		if err := rows.Scan(&t.EventTypeID, &t.Name, &t.Short, &t.Icon, &t.AbsenceRule, &t.StrikeType, &t.StrikeLabel); err != nil {
			rows.Close()
			return nil, err
		}
		types[t.EventTypeID] = t
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = q.Query(`SELECT id, event_type_id, key, label, sort_order FROM participation_trackables ORDER BY event_type_id, sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var tr ptTrackable
		var typeID int
		if err := rows.Scan(&tr.ID, &typeID, &tr.Key, &tr.Label, &tr.SortOrder); err != nil {
			return nil, err
		}
		if t := types[typeID]; t != nil {
			t.Trackables = append(t.Trackables, tr)
		}
	}
	return types, rows.Err()
}

// sortedParticipationTypes is the tracked types in the schedule's own type order.
func sortedParticipationTypes(q rowQueryer, types map[int]*ptType) ([]*ptType, error) {
	rows, err := q.Query(`SELECT id FROM schedule_event_types ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ptType{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if t := types[id]; t != nil {
			out = append(out, t)
		}
	}
	return out, rows.Err()
}

// loadParticipationRoster is every member with what eligibleRoster needs. EX
// members are included: they appear on historic boards by name.
func loadParticipationRoster(q rowQueryer) ([]ptRosterMember, error) {
	rows, err := q.Query(`SELECT id, name, rank, COALESCE(joined_at, '') FROM members ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ptRosterMember{}
	for rows.Next() {
		var m ptRosterMember
		if err := rows.Scan(&m.ID, &m.Name, &m.Rank, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// loadStruckSet returns every (member, strike type, ref date) that already carries a
// strike, for the strike types tracked types file under.
func loadStruckSet(q rowQueryer) (map[string]bool, error) {
	rows, err := q.Query(`
		SELECT member_id, strike_type, COALESCE(ref_date, '') FROM accountability_strikes
		WHERE strike_type IN (SELECT strike_type FROM participation_types)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var id int
		var st, ref string
		if err := rows.Scan(&id, &st, &ref); err != nil {
			return nil, err
		}
		if len(ref) >= 10 {
			ref = ref[:10]
		}
		set[struckKey(id, st, ref)] = true
	}
	return set, rows.Err()
}

// boardFilter selects which occurrences loadBoards reads.
type boardFilter struct {
	EventID  int  // one occurrence, board or not
	TypeID   int  // recorded boards of one type
	Recorded bool // only occurrences that have a board
}

// loadBoards reads occurrences and everything recorded against them in a fixed
// number of queries, whatever the count. Newest first.
func loadBoards(q rowQueryer, types map[int]*ptType, f boardFilter) ([]*ptBoardData, error) {
	where := []string{"1=1"}
	args := []any{}
	if f.EventID != 0 {
		where = append(where, "se.id = ?")
		args = append(args, f.EventID)
	}
	if f.TypeID != 0 {
		where = append(where, "se.event_type_id = ?")
		args = append(args, f.TypeID)
	}
	if f.Recorded {
		where = append(where, "b.id IS NOT NULL")
	}
	rows, err := q.Query(`
		SELECT se.id, se.event_date, se.event_time, se.all_day, se.event_type_id,
		       t.name, t.short_name, t.icon, se.level, se.task_force,
		       b.id, COALESCE(b.source,''), COALESCE(b.notes,''), COALESCE(b.result_json,'{}'),
		       COALESCE(u.username,''), COALESCE(b.created_at,''), COALESCE(b.updated_at,'')
		FROM schedule_events se
		JOIN schedule_event_types t ON t.id = se.event_type_id
		LEFT JOIN participation_boards b ON b.schedule_event_id = se.id
		LEFT JOIN users u ON u.id = b.recorded_by
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY se.event_date DESC, se.all_day, se.event_time DESC, se.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	var out []*ptBoardData
	byBoard := map[int]*ptBoardData{}
	for rows.Next() {
		bd := &ptBoardData{Entries: []ptEntry{}, Roles: []ptRole{}, Exceptions: []ptException{}}
		var allDay int
		var boardID sql.NullInt64
		var b ptBoard
		var result string
		if err := rows.Scan(&bd.Event.ID, &bd.Event.EventDate, &bd.Event.EventTime, &allDay, &bd.Event.EventTypeID,
			&bd.Event.TypeName, &bd.Event.TypeShort, &bd.Event.TypeIcon, &bd.Event.Level, &bd.Event.TaskForce,
			&boardID, &b.Source, &b.Notes, &result, &b.RecordedBy, &b.CreatedAt, &b.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		bd.Event.AllDay = allDay == 1
		bd.Type = types[bd.Event.EventTypeID]
		if boardID.Valid {
			b.ID = int(boardID.Int64)
			if !json.Valid([]byte(result)) {
				result = "{}"
			}
			b.Result = json.RawMessage(result)
			bd.Board = &b
			byBoard[b.ID] = bd
		}
		out = append(out, bd)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(byBoard) == 0 {
		return out, nil
	}

	ids := make([]any, 0, len(byBoard))
	for id := range byBoard {
		ids = append(ids, id)
	}
	in := "(" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"

	// Entries, then their values attached by entry id.
	rows, err = q.Query(`
		SELECT e.id, e.board_id, e.member_id, COALESCE(m.name,''), e.name_snapshot, e.rank
		FROM participation_entries e LEFT JOIN members m ON m.id = e.member_id
		WHERE e.board_id IN `+in+`
		ORDER BY e.board_id, e.rank IS NULL, e.rank, e.id`, ids...)
	if err != nil {
		return nil, err
	}
	type entryRef struct {
		bd  *ptBoardData
		idx int
	}
	entries := map[int]entryRef{}
	for rows.Next() {
		var e ptEntry
		var boardID int
		if err := rows.Scan(&e.ID, &boardID, &e.MemberID, &e.MemberName, &e.Name, &e.Rank); err != nil {
			rows.Close()
			return nil, err
		}
		e.Values = map[string]*int64{}
		bd := byBoard[boardID]
		bd.Entries = append(bd.Entries, e)
		entries[e.ID] = entryRef{bd, len(bd.Entries) - 1}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = q.Query(`
		SELECT v.entry_id, t.key, v.value
		FROM participation_values v
		JOIN participation_trackables t ON t.id = v.trackable_id
		JOIN participation_entries e ON e.id = v.entry_id
		WHERE e.board_id IN `+in, ids...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var entryID int
		var key string
		var v int64
		if err := rows.Scan(&entryID, &key, &v); err != nil {
			rows.Close()
			return nil, err
		}
		if ref, ok := entries[entryID]; ok {
			val := v
			ref.bd.Entries[ref.idx].Values[key] = &val
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = q.Query(`
		SELECT r.board_id, r.member_id, COALESCE(m.name,''), COALESCE(r.role,''), COALESCE(r.task_force,'')
		FROM participation_roles r LEFT JOIN members m ON m.id = r.member_id
		WHERE r.board_id IN `+in+` ORDER BY m.name COLLATE NOCASE`, ids...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r ptRole
		var boardID int
		if err := rows.Scan(&boardID, &r.MemberID, &r.MemberName, &r.Role, &r.TaskForce); err != nil {
			rows.Close()
			return nil, err
		}
		byBoard[boardID].Roles = append(byBoard[boardID].Roles, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// recorded_by is a user who may since have been deleted: tolerated here, and
	// cleared by every user-delete path (decision 21).
	rows, err = q.Query(`
		SELECT x.board_id, x.member_id, COALESCE(m.name,''), x.kind, x.reason,
		       COALESCE(u.username,''), COALESCE(x.created_at,'')
		FROM participation_exceptions x
		LEFT JOIN members m ON m.id = x.member_id
		LEFT JOIN users u ON u.id = x.recorded_by
		WHERE x.board_id IN `+in+` ORDER BY m.name COLLATE NOCASE`, ids...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var x ptException
		var boardID int
		if err := rows.Scan(&boardID, &x.MemberID, &x.MemberName, &x.Kind, &x.Reason, &x.RecordedBy, &x.CreatedAt); err != nil {
			return nil, err
		}
		byBoard[boardID].Exceptions = append(byBoard[boardID].Exceptions, x)
	}
	return out, rows.Err()
}

// ptStore bundles what a derivation needs, loaded once per request.
type ptStore struct {
	Types  map[int]*ptType
	Roster []ptRosterMember
	Struck map[string]bool
}

func loadPtStore(q rowQueryer) (*ptStore, error) {
	types, err := loadParticipationTypes(q)
	if err != nil {
		return nil, err
	}
	roster, err := loadParticipationRoster(q)
	if err != nil {
		return nil, err
	}
	struck, err := loadStruckSet(q)
	if err != nil {
		return nil, err
	}
	return &ptStore{Types: types, Roster: roster, Struck: struck}, nil
}

func (s *ptStore) suggestions(bd *ptBoardData, statuses []ptStatus) []ptStatus {
	if bd.Type == nil || bd.Board == nil {
		return []ptStatus{}
	}
	return boardSuggestions(statuses, bd.Type.StrikeType, bd.Event.EventDate, s.Struck)
}

// canViewParticipation is the read gate for anyone else's participation. manage
// does not imply view (userHasPermission reads one key), and the recording screen
// reads everything it writes, so either key opens every read.
func canViewParticipation(u *AuthUser) bool {
	return userHasPermission(u, "view_participation") || userHasPermission(u, "manage_participation")
}
