package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

// handlers_participation.go — the recording screen and the participation API.
// Framework rules (derivation, exceptions, delete order) live in participation.go;
// CLAUDE.md → "Participation framework" is the summary.

// --- Pages ---------------------------------------------------------------------

// GET /participation/new and /participation/{eventID}. Dedicated handlers rather
// than the pages map because one carries an id; the page reads it from its own URL.
func handleParticipationPage(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "Participation - Alliance Manager", "accountability")
	if !data.IsAuthenticated {
		http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
		return
	}
	if !data.Permissions.ViewParticipation && !data.Permissions.ManageParticipation {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	renderTemplate(w, r, "participation.html", data)
}

// --- Shared response shapes ------------------------------------------------------

type ptBoardSummary struct {
	EventID    int    `json:"event_id"`
	BoardID    int    `json:"board_id"`
	EventDate  string `json:"event_date"`
	EventTime  string `json:"event_time"`
	AllDay     bool   `json:"all_day"`
	TypeID     int    `json:"event_type_id"`
	TypeName   string `json:"type_name"`
	TypeShort  string `json:"type_short"`
	TypeIcon   string `json:"type_icon"`
	Source     string `json:"source"`
	RecordedBy string `json:"recorded_by"`
	UpdatedAt  string `json:"updated_at"`
	Rows       int    `json:"rows"`
	Matched    int    `json:"matched"`
	Missed     int    `json:"missed"`
	Excused    int    `json:"excused"`
	Pending    int    `json:"pending"`
}

type ptBoardDetail struct {
	Event        ptEvent          `json:"event"`
	Type         *ptType          `json:"type"`
	Board        *ptBoard         `json:"board"`
	Entries      []ptEntry        `json:"entries"`
	Roles        []ptRole         `json:"roles"`
	RolesPrefill []ptRole         `json:"roles_prefill,omitempty"`
	Exceptions   []ptException    `json:"exceptions"`
	Statuses     []ptStatus       `json:"statuses"`
	Suggestions  []ptStatus       `json:"suggestions"`
	Roster       []ptRosterMember `json:"roster"`
	GameDate     string           `json:"game_date"`
}

func summarizeBoard(bd *ptBoardData, statuses, suggestions []ptStatus) ptBoardSummary {
	s := ptBoardSummary{
		EventID: bd.Event.ID, EventDate: bd.Event.EventDate, EventTime: bd.Event.EventTime, AllDay: bd.Event.AllDay,
		TypeID: bd.Event.EventTypeID, TypeName: bd.Event.TypeName, TypeShort: bd.Event.TypeShort, TypeIcon: bd.Event.TypeIcon,
		Rows: len(bd.Entries), Pending: len(suggestions),
	}
	if bd.Board != nil {
		s.BoardID, s.Source, s.RecordedBy, s.UpdatedAt = bd.Board.ID, bd.Board.Source, bd.Board.RecordedBy, bd.Board.UpdatedAt
	}
	for _, e := range bd.Entries {
		if e.MemberID != nil {
			s.Matched++
		}
	}
	for _, st := range statuses {
		switch st.Status {
		case "missed", "zero":
			s.Missed++
		case "excused":
			s.Excused++
		}
	}
	return s
}

// activeRoster is the member picker's list: everyone not EX.
func activeRoster(roster []ptRosterMember) []ptRosterMember {
	out := []ptRosterMember{}
	for _, m := range roster {
		if m.Rank != "EX" {
			out = append(out, m)
		}
	}
	return out
}

// buildBoardDetail reads one occurrence and everything the recording screen shows.
// q is db or a tx. Returns (nil, nil) when the event does not exist.
func buildBoardDetail(q rowQueryer, eventID int) (*ptBoardDetail, error) {
	store, err := loadPtStore(q)
	if err != nil {
		return nil, err
	}
	boards, err := loadBoards(q, store.Types, boardFilter{EventID: eventID})
	if err != nil {
		return nil, err
	}
	if len(boards) == 0 {
		return nil, nil
	}
	bd := boards[0]
	statuses := bd.derive(store.Roster)
	if bd.Type != nil {
		for i := range statuses {
			statuses[i].Struck = store.Struck[struckKey(statuses[i].MemberID, bd.Type.StrikeType, bd.Event.EventDate)]
		}
	}
	d := &ptBoardDetail{
		Event: bd.Event, Type: bd.Type, Board: bd.Board,
		Entries: bd.Entries, Roles: bd.Roles, Exceptions: bd.Exceptions,
		Statuses: statuses, Suggestions: store.suggestions(bd, statuses),
		Roster: activeRoster(store.Roster), GameDate: gameDate(),
	}
	if bd.Type != nil && bd.Type.AbsenceRule == ruleRole && bd.Board == nil {
		prefill, err := loadRolePrefill(q, "")
		if err != nil {
			return nil, err
		}
		d.RolesPrefill = prefill
	}
	return d, nil
}

// loadRolePrefill offers the Desert Storm planner's CURRENT lineup as the roles for
// a board being recorded for the first time (decision 5: the snapshot is taken
// when the recording screen is first opened, not when the occurrence is created,
// because generated occurrences exist weeks before their lineup does). It is a
// prefill the leader corrects, never a stored fact until the board is saved.
//
// Both lineup tables carry is_sub, and the save handlers keep their populations
// disjoint; the building row still wins on a duplicate, as belt and braces.
// taskForce filters to one task force when non-empty.
func loadRolePrefill(q rowQueryer, taskForce string) ([]ptRole, error) {
	rows, err := q.Query(`
		SELECT x.member_id, COALESCE(m.name,''), x.is_sub, sg.task_force, x.pref
		FROM (
			SELECT sgbm.member_id, sgbm.is_sub, sgb.group_id, 0 AS pref
			FROM storm_group_building_members sgbm
			JOIN storm_group_buildings sgb ON sgb.id = sgbm.group_building_id
			UNION ALL
			SELECT sgm.member_id, sgm.is_sub, sgm.group_id, 1 AS pref
			FROM storm_group_members sgm
		) x
		JOIN storm_groups sg ON sg.id = x.group_id
		JOIN members m ON m.id = x.member_id
		WHERE (? = '' OR sg.task_force = ?)
		ORDER BY x.pref, m.name COLLATE NOCASE`, taskForce, taskForce)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[int]bool{}
	out := []ptRole{}
	for rows.Next() {
		var r ptRole
		var isSub, pref int
		if err := rows.Scan(&r.MemberID, &r.MemberName, &isSub, &r.TaskForce, &pref); err != nil {
			return nil, err
		}
		if seen[r.MemberID] {
			continue
		}
		seen[r.MemberID] = true
		r.Role = "starter"
		if isSub == 1 {
			r.Role = "sub"
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func eventIDVar(r *http.Request) (int, bool) {
	id, err := strconv.Atoi(mux.Vars(r)["eventID"])
	return id, err == nil && id > 0
}

// --- Reads -----------------------------------------------------------------------

// GET /api/participation/types — the tracked types, for the recording screen's
// chooser and the Accountability tab's filter chips.
func handleParticipationTypes(w http.ResponseWriter, r *http.Request) {
	if !canViewParticipation(getAuthUser(r)) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	types, err := loadParticipationTypes(db)
	if err != nil {
		slog.Error("handleParticipationTypes: load failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	sorted, err := sortedParticipationTypes(db, types)
	if err != nil {
		slog.Error("handleParticipationTypes: order failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, sorted)
}

// GET /api/participation/boards?type= — recorded boards only, newest first. An
// occurrence with no board is not listed: optional means optional.
func handleParticipationBoards(w http.ResponseWriter, r *http.Request) {
	if !canViewParticipation(getAuthUser(r)) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	typeID, _ := strconv.Atoi(r.URL.Query().Get("type"))
	store, err := loadPtStore(db)
	if err != nil {
		slog.Error("handleParticipationBoards: load store failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	boards, err := loadBoards(db, store.Types, boardFilter{TypeID: typeID, Recorded: true})
	if err != nil {
		slog.Error("handleParticipationBoards: load boards failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	out := make([]ptBoardSummary, 0, len(boards))
	for _, bd := range boards {
		statuses := bd.derive(store.Roster)
		out = append(out, summarizeBoard(bd, statuses, store.suggestions(bd, statuses)))
	}
	writeJSON(w, out)
}

// GET /api/participation/recent?type=&limit= — the chooser on /participation/new:
// a type's most recent past occurrences, recorded or not.
func handleParticipationRecent(w http.ResponseWriter, r *http.Request) {
	if !canViewParticipation(getAuthUser(r)) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	typeID, _ := strconv.Atoi(r.URL.Query().Get("type"))
	if typeID == 0 {
		http.Error(w, "type is required", http.StatusBadRequest)
		return
	}
	rows, err := db.Query(`
		SELECT se.id, se.event_date, se.event_time, se.all_day, b.id IS NOT NULL,
		       CASE WHEN b.id IS NULL THEN 0 ELSE (SELECT COUNT(*) FROM participation_entries e WHERE e.board_id = b.id) END
		FROM schedule_events se
		LEFT JOIN participation_boards b ON b.schedule_event_id = se.id
		WHERE se.event_type_id = ? AND se.event_date <= ?
		ORDER BY se.event_date DESC, se.event_time DESC LIMIT 10`, typeID, gameDate())
	if err != nil {
		slog.Error("handleParticipationRecent: query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type recent struct {
		EventID   int    `json:"event_id"`
		EventDate string `json:"event_date"`
		EventTime string `json:"event_time"`
		AllDay    bool   `json:"all_day"`
		HasBoard  bool   `json:"has_board"`
		Rows      int    `json:"rows"`
	}
	out := []recent{}
	for rows.Next() {
		var x recent
		if err := rows.Scan(&x.EventID, &x.EventDate, &x.EventTime, &x.AllDay, &x.HasBoard, &x.Rows); err != nil {
			slog.Error("handleParticipationRecent: scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		out = append(out, x)
	}
	writeJSON(w, out)
}

// GET /api/participation/boards/{eventID}
func handleParticipationBoard(w http.ResponseWriter, r *http.Request) {
	if !canViewParticipation(getAuthUser(r)) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	id, ok := eventIDVar(r)
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	d, err := buildBoardDetail(db, id)
	if err != nil {
		slog.Error("handleParticipationBoard: load failed", "event_id", id, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if d == nil {
		http.Error(w, "That event no longer exists", http.StatusNotFound)
		return
	}
	writeJSON(w, d)
}

// GET /api/participation/boards/{eventID}/suggestions
func handleParticipationSuggestions(w http.ResponseWriter, r *http.Request) {
	id, ok := eventIDVar(r)
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	d, err := buildBoardDetail(db, id)
	if err != nil {
		slog.Error("handleParticipationSuggestions: load failed", "event_id", id, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if d == nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	writeJSON(w, d.Suggestions)
}

// ptMemberRow is one board as it concerns one member.
type ptMemberRow struct {
	EventID    int    `json:"event_id"`
	EventDate  string `json:"event_date"`
	TypeName   string `json:"type_name"`
	TypeShort  string `json:"type_short"`
	TypeIcon   string `json:"type_icon"`
	Rank       *int   `json:"rank"`
	Score      *int64 `json:"score"`
	ScoreLabel string `json:"score_label"`
	Status     string `json:"status"`
	Role       string `json:"role"`
	TaskForce  string `json:"task_force"`
	Reason     string `json:"reason"`
}

type ptMemberHistory struct {
	MemberID int           `json:"member_id"`
	Name     string        `json:"name"`
	Boards   int           `json:"boards"`
	Missed   int           `json:"missed"`
	Excused  int           `json:"excused"`
	Rows     []ptMemberRow `json:"rows"`
}

// memberParticipation is the member's derived status on every board that says
// something about them, newest first.
func memberParticipation(memberID int) (*ptMemberHistory, error) {
	var name string
	if err := db.QueryRow(`SELECT name FROM members WHERE id = ?`, memberID).Scan(&name); err != nil {
		return nil, err
	}
	store, err := loadPtStore(db)
	if err != nil {
		return nil, err
	}
	boards, err := loadBoards(db, store.Types, boardFilter{Recorded: true})
	if err != nil {
		return nil, err
	}
	h := &ptMemberHistory{MemberID: memberID, Name: name, Rows: []ptMemberRow{}}
	for _, bd := range boards {
		for _, st := range bd.derive(store.Roster) {
			if st.MemberID != memberID {
				continue
			}
			row := ptMemberRow{
				EventID: bd.Event.ID, EventDate: bd.Event.EventDate,
				TypeName: bd.Event.TypeName, TypeShort: bd.Event.TypeShort, TypeIcon: bd.Event.TypeIcon,
				Rank: st.Rank, Status: st.Status, Role: st.Role, TaskForce: st.TaskForce, Reason: st.Reason,
			}
			if key := bd.Type.primaryKey(); key != "" {
				row.Score = st.Values[key]
				row.ScoreLabel = bd.Type.Trackables[0].Label
			}
			h.Boards++
			switch st.Status {
			case "missed", "zero":
				h.Missed++
			case "excused":
				h.Excused++
			}
			h.Rows = append(h.Rows, row)
		}
	}
	return h, nil
}

// GET /api/participation/members/{id} — for the per-member Accountability page,
// which is itself gated on view_accountability, so that key opens it too: a 403
// inside a tab of a page the user may open is an unexplained empty section.
func handleParticipationMember(w http.ResponseWriter, r *http.Request) {
	u := getAuthUser(r)
	if !canViewParticipation(u) && !userHasPermission(u, "view_accountability") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	h, err := memberParticipation(id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleParticipationMember: load failed", "member_id", id, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, h)
}

// GET /api/participation/me — a member always sees their own; no permission. 404
// for an account with no linked member, which the Profile page explains in words.
func handleParticipationMe(w http.ResponseWriter, r *http.Request) {
	u := getAuthUser(r)
	if u == nil || u.MemberID == nil {
		http.Error(w, "Your account is not linked to a member", http.StatusNotFound)
		return
	}
	h, err := memberParticipation(*u.MemberID)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Your account is not linked to a member", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleParticipationMe: load failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, h)
}

// --- Name resolution -------------------------------------------------------------

// A mail prefixes names with the alliance tag, "[PoWr] Name". Stripped before
// lookup; the snapshot keeps what the board said.
var ptTagPrefix = regexp.MustCompile(`^\[[^\]]{1,12}\]\s*`)

// POST /api/participation/resolve {names:[]} — one read-only transaction, one
// folded index for the whole batch (namematch.go's no-rebuild rule).
func handleParticipationResolve(w http.ResponseWriter, r *http.Request) {
	u := getAuthUser(r)
	var body struct {
		Names []string `json:"names"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if len(body.Names) > 500 {
		http.Error(w, "At most 500 names at a time", http.StatusBadRequest)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleParticipationResolve: begin failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	idx, err := buildFoldedNameIndex(tx, u.ID)
	if err != nil {
		slog.Error("handleParticipationResolve: index failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	type resolved struct {
		Name       string `json:"name"`
		MemberID   *int   `json:"member_id"`
		MemberName string `json:"member_name"`
		MemberRank string `json:"member_rank"`
		How        string `json:"how"`
	}
	out := make([]resolved, 0, len(body.Names))
	for _, raw := range body.Names {
		name := strings.TrimSpace(ptTagPrefix.ReplaceAllString(strings.TrimSpace(raw), ""))
		res := resolved{Name: raw, How: "none"}
		if name != "" {
			m, how, err := resolveMemberAliasWithIndex(tx, name, u.ID, idx)
			if err == nil && m != nil {
				id := m.ID
				res.MemberID, res.MemberName, res.MemberRank, res.How = &id, m.Name, m.Rank, how
			}
		}
		out = append(out, res)
	}
	writeJSON(w, out)
}

// --- Writes ----------------------------------------------------------------------

type ptPutEntry struct {
	Rank     *int              `json:"rank"`
	Name     string            `json:"name"`
	MemberID *int              `json:"member_id"`
	Values   map[string]*int64 `json:"values"`
}

type ptPutRole struct {
	MemberID  int    `json:"member_id"`
	Role      string `json:"role"`
	TaskForce string `json:"task_force"`
}

type ptPutBody struct {
	Entries []ptPutEntry    `json:"entries"`
	Roles   []ptPutRole     `json:"roles"`
	Result  json.RawMessage `json:"result"`
	Notes   string          `json:"notes"`
}

// validateBoardPut is every rule a board write must pass that needs no database.
// A rejected board names what is wrong with it.
func validateBoardPut(ev ptEvent, t *ptType, today string, body *ptPutBody) string {
	if t == nil {
		return ev.TypeName + " does not track participation"
	}
	if ev.EventDate > today {
		return "This event is on " + ev.EventDate + ", which has not happened yet — a board can only be recorded after the event"
	}
	known := t.trackableIDs()
	ranks, members := map[int]bool{}, map[int]bool{}
	for i := range body.Entries {
		e := &body.Entries[i]
		e.Name = strings.TrimSpace(e.Name)
		row := "Row " + strconv.Itoa(i+1)
		if e.Name == "" {
			return row + " has no name"
		}
		if e.Rank != nil {
			if *e.Rank < 1 {
				return row + ": rank must be 1 or more"
			}
			if ranks[*e.Rank] {
				return "Rank " + strconv.Itoa(*e.Rank) + " appears twice"
			}
			ranks[*e.Rank] = true
		}
		if e.MemberID != nil {
			if members[*e.MemberID] {
				return row + " (" + e.Name + ") is matched to a member already on the board"
			}
			members[*e.MemberID] = true
		}
		for k, v := range e.Values {
			if _, ok := known[k]; !ok {
				return row + ": unknown value \"" + k + "\""
			}
			if v != nil && *v < 0 {
				return row + ": values cannot be negative"
			}
		}
		// A missing value is not a zero (decision 19), so a manual board cannot be
		// half-filled: under the 'zero' rule a blank would read as "was not asked".
		for _, tr := range t.Trackables {
			if v, ok := e.Values[tr.Key]; !ok || v == nil {
				return row + " (" + e.Name + ") needs a value for " + tr.Label
			}
		}
	}
	if len(body.Roles) > 0 && t.AbsenceRule != ruleRole {
		return t.Name + " does not use roles"
	}
	seen := map[int]bool{}
	for _, rl := range body.Roles {
		if rl.MemberID <= 0 {
			return "A role is missing its member"
		}
		if seen[rl.MemberID] {
			return "A member has two roles"
		}
		seen[rl.MemberID] = true
		if rl.Role != "starter" && rl.Role != "sub" {
			return "A role must be starter or sub"
		}
		if rl.TaskForce != "" && rl.TaskForce != "A" && rl.TaskForce != "B" {
			return "A task force must be A or B"
		}
	}
	if len(body.Result) > 0 && string(body.Result) != "null" {
		var obj map[string]any
		if json.Unmarshal(body.Result, &obj) != nil {
			return "The result must be an object"
		}
	}
	if len(body.Notes) > 4000 {
		return "Notes are limited to 4000 characters"
	}
	return ""
}

// clearBoardChildren deletes a board's rows explicitly, children first (values →
// entries → roles), since no cascade fires. Exceptions are kept: a re-save must
// not throw away an excuse or a dismissal.
func clearBoardChildren(tx *sql.Tx, boardID int, withExceptions bool) error {
	stmts := []string{
		`DELETE FROM participation_values WHERE entry_id IN (SELECT id FROM participation_entries WHERE board_id = ?)`,
		`DELETE FROM participation_entries WHERE board_id = ?`,
		`DELETE FROM participation_roles WHERE board_id = ?`,
	}
	if withExceptions {
		stmts = append(stmts, `DELETE FROM participation_exceptions WHERE board_id = ?`)
	}
	for _, q := range stmts {
		if _, err := tx.Exec(q, boardID); err != nil {
			return err
		}
	}
	return nil
}

// PUT /api/participation/boards/{eventID} — replace the board in one transaction.
func handleParticipationBoardPut(w http.ResponseWriter, r *http.Request) {
	u := getAuthUser(r)
	id, ok := eventIDVar(r)
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	var body ptPutBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleParticipationBoardPut: begin failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	types, err := loadParticipationTypes(tx)
	if err != nil {
		slog.Error("handleParticipationBoardPut: types failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	boards, err := loadBoards(tx, types, boardFilter{EventID: id})
	if err != nil {
		slog.Error("handleParticipationBoardPut: load failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if len(boards) == 0 {
		http.Error(w, "That event no longer exists", http.StatusNotFound)
		return
	}
	bd := boards[0]
	if msg := validateBoardPut(bd.Event, bd.Type, gameDate(), &body); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	// Every referenced member must exist — the rows would otherwise be orphans the
	// day they are written.
	ids := map[int]bool{}
	for _, e := range body.Entries {
		if e.MemberID != nil {
			ids[*e.MemberID] = true
		}
	}
	for _, rl := range body.Roles {
		ids[rl.MemberID] = true
	}
	for mid := range ids {
		var one int
		if err := tx.QueryRow(`SELECT 1 FROM members WHERE id = ?`, mid).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "A matched member no longer exists — reload and match again", http.StatusBadRequest)
				return
			}
			slog.Error("handleParticipationBoardPut: member check failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	result := "{}"
	if len(body.Result) > 0 && string(body.Result) != "null" {
		result = string(body.Result)
	}
	action := "updated"
	var boardID int
	if bd.Board == nil {
		action = "created"
		res, err := tx.Exec(`INSERT INTO participation_boards (schedule_event_id, source, result_json, notes, recorded_by)
			VALUES (?, 'manual', ?, ?, ?)`, id, result, body.Notes, u.ID)
		if err != nil {
			slog.Error("handleParticipationBoardPut: insert board failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		n, _ := res.LastInsertId()
		boardID = int(n)
	} else {
		boardID = bd.Board.ID
		if err := clearBoardChildren(tx, boardID, false); err != nil {
			slog.Error("handleParticipationBoardPut: clear failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if _, err := tx.Exec(`UPDATE participation_boards SET result_json = ?, notes = ?, recorded_by = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			result, body.Notes, u.ID, boardID); err != nil {
			slog.Error("handleParticipationBoardPut: update board failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	trackIDs := bd.Type.trackableIDs()
	matched := 0
	for _, e := range body.Entries {
		res, err := tx.Exec(`INSERT INTO participation_entries (board_id, member_id, name_snapshot, rank) VALUES (?, ?, ?, ?)`,
			boardID, e.MemberID, e.Name, e.Rank)
		if err != nil {
			slog.Error("handleParticipationBoardPut: insert entry failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if e.MemberID != nil {
			matched++
		}
		entryID, _ := res.LastInsertId()
		for k, v := range e.Values {
			if _, err := tx.Exec(`INSERT INTO participation_values (entry_id, trackable_id, value) VALUES (?, ?, ?)`,
				entryID, trackIDs[k], *v); err != nil {
				slog.Error("handleParticipationBoardPut: insert value failed", "error", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
		}
	}
	for _, rl := range body.Roles {
		var tf any
		if rl.TaskForce != "" {
			tf = rl.TaskForce
		}
		if _, err := tx.Exec(`INSERT INTO participation_roles (board_id, member_id, role, task_force) VALUES (?, ?, ?, ?)`,
			boardID, rl.MemberID, rl.Role, tf); err != nil {
			slog.Error("handleParticipationBoardPut: insert role failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handleParticipationBoardPut: commit failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	details := strconv.Itoa(len(body.Entries)) + " rows, " + strconv.Itoa(matched) + " matched"
	if len(body.Roles) > 0 {
		details += ", " + strconv.Itoa(len(body.Roles)) + " roles"
	}
	logActivity(u.ID, u.Username, action, "participation_board", bd.Event.TypeName+" "+bd.Event.EventDate, false, details)

	d, err := buildBoardDetail(db, id)
	if err != nil || d == nil {
		slog.Error("handleParticipationBoardPut: reload failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, d)
}

// DELETE /api/participation/boards/{eventID} — the board and all its rows.
func handleParticipationBoardDelete(w http.ResponseWriter, r *http.Request) {
	u := getAuthUser(r)
	id, ok := eventIDVar(r)
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	var boardID int
	var typeName, date string
	err := db.QueryRow(`SELECT b.id, t.name, se.event_date FROM participation_boards b
		JOIN schedule_events se ON se.id = b.schedule_event_id
		JOIN schedule_event_types t ON t.id = se.event_type_id
		WHERE b.schedule_event_id = ?`, id).Scan(&boardID, &typeName, &date)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No board is recorded for this event", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleParticipationBoardDelete: load failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleParticipationBoardDelete: begin failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	if err := clearBoardChildren(tx, boardID, true); err != nil {
		slog.Error("handleParticipationBoardDelete: children failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(`DELETE FROM participation_boards WHERE id = ?`, boardID); err != nil {
		slog.Error("handleParticipationBoardDelete: board failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handleParticipationBoardDelete: commit failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(u.ID, u.Username, "deleted", "participation_board", typeName+" "+date, false)
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/participation/occurrences — create a tracked occurrence nobody put on
// the schedule, through exactly the Schedule page's validation (#13 Q7). Does not
// need manage_schedule: it is the same checked create, reached from the screen
// the owner asked it be offered on.
func handleParticipationOccurrence(w http.ResponseWriter, r *http.Request) {
	var req scheduleEventCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	var tracked int
	if err := db.QueryRow(`SELECT COUNT(*) FROM participation_types WHERE event_type_id = ?`, req.EventTypeID).Scan(&tracked); err != nil {
		slog.Error("handleParticipationOccurrence: type check failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if tracked == 0 {
		http.Error(w, "That event type does not track participation", http.StatusBadRequest)
		return
	}
	if req.EventDate > gameDate() {
		http.Error(w, "A board can only be recorded after the event — pick a date that has already happened", http.StatusBadRequest)
		return
	}
	req.Notes = ""
	id, msg, code, err := insertScheduleEvent(getAuthUser(r), req)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if msg != "" {
		http.Error(w, msg, code)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

// strikeReason is the reason a confirmed suggestion's strike carries.
func strikeReason(bd *ptBoardData, st ptStatus) string {
	if st.Status == "zero" && len(bd.Type.Trackables) > 0 {
		return "0 " + strings.ToLower(bd.Type.Trackables[0].Label) + " in " + bd.Event.TypeName + " on " + bd.Event.EventDate
	}
	return "Absent from " + bd.Event.TypeName + " on " + bd.Event.EventDate
}

// POST /api/participation/boards/{eventID}/strikes {member_id} | {all:true}
//
// Suggestions are re-derived INSIDE the transaction and each strike is checked
// against existing ones in it too. With one connection the transaction is the
// serialisation point, so two officers confirming at once cannot both see "no
// strike yet" (the check-then-insert in handleStrikeCreate has no such guard).
func handleParticipationStrikes(w http.ResponseWriter, r *http.Request) {
	u := getAuthUser(r)
	id, ok := eventIDVar(r)
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	var body struct {
		MemberID int  `json:"member_id"`
		All      bool `json:"all"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if !body.All && body.MemberID <= 0 {
		http.Error(w, "member_id or all is required", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleParticipationStrikes: begin failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	store, err := loadPtStore(tx)
	if err != nil {
		slog.Error("handleParticipationStrikes: store failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	boards, err := loadBoards(tx, store.Types, boardFilter{EventID: id})
	if err != nil {
		slog.Error("handleParticipationStrikes: load failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if len(boards) == 0 || boards[0].Board == nil || boards[0].Type == nil {
		http.Error(w, "No board is recorded for this event", http.StatusNotFound)
		return
	}
	bd := boards[0]
	active, err := strikeTypeIsActive(tx, bd.Type.StrikeType)
	if err != nil {
		slog.Error("handleParticipationStrikes: category check failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if !active {
		http.Error(w, "The strike category \""+bd.Type.StrikeType+"\" is not active", http.StatusConflict)
		return
	}

	suggestions := store.suggestions(bd, bd.derive(store.Roster))
	var targets []ptStatus
	if body.All {
		targets = suggestions
	} else {
		for _, s := range suggestions {
			if s.MemberID == body.MemberID {
				targets = append(targets, s)
			}
		}
		if len(targets) == 0 {
			http.Error(w, "That member is no longer suggested for a strike on this board — it may already have been struck, excused or dismissed", http.StatusConflict)
			return
		}
	}

	type made struct{ name, reason string }
	var created []made
	for _, s := range targets {
		reason := strikeReason(bd, s)
		if _, err := tx.Exec(`INSERT INTO accountability_strikes (member_id, strike_type, reason, ref_date, created_by)
			VALUES (?, ?, ?, ?, ?)`, s.MemberID, bd.Type.StrikeType, reason, bd.Event.EventDate, u.ID); err != nil {
			slog.Error("handleParticipationStrikes: insert failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		created = append(created, made{s.Name, reason})
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handleParticipationStrikes: commit failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	for _, c := range created {
		logActivity(u.ID, u.Username, "created", "accountability_strike", c.name, false,
			"type: "+bd.Type.StrikeType+"; reason: "+c.reason)
	}
	writeJSON(w, map[string]int{"created": len(created)})
}

// POST /api/participation/boards/{eventID}/exceptions {member_id, kind, reason}
// DELETE /api/participation/boards/{eventID}/exceptions?member_id=
func handleParticipationException(w http.ResponseWriter, r *http.Request) {
	u := getAuthUser(r)
	id, ok := eventIDVar(r)
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	var boardID int
	var source, typeName, date string
	err := db.QueryRow(`SELECT b.id, b.source, t.name, se.event_date FROM participation_boards b
		JOIN schedule_events se ON se.id = b.schedule_event_id
		JOIN schedule_event_types t ON t.id = se.event_type_id
		WHERE b.schedule_event_id = ?`, id).Scan(&boardID, &source, &typeName, &date)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "No board is recorded for this event", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleParticipationException: board lookup failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	event := typeName + " " + date

	if r.Method == http.MethodDelete {
		memberID, _ := strconv.Atoi(r.URL.Query().Get("member_id"))
		var kind, name string
		err := db.QueryRow(`SELECT x.kind, COALESCE(m.name,'') FROM participation_exceptions x
			LEFT JOIN members m ON m.id = x.member_id WHERE x.board_id = ? AND x.member_id = ?`, boardID, memberID).Scan(&kind, &name)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Nothing to undo for that member", http.StatusNotFound)
			return
		}
		if err != nil {
			slog.Error("handleParticipationException: load failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if kind == "missed" {
			http.Error(w, "A miss carried over from the old Storm Attendance screen cannot be removed — excuse it instead", http.StatusConflict)
			return
		}
		// On a legacy board an excuse can only have been laid over a recorded absence
		// (there are no roles to derive one from), so undoing it restores that
		// absence rather than making the member vanish from the board.
		q := `DELETE FROM participation_exceptions WHERE board_id = ? AND member_id = ?`
		if source == "legacy" && kind == "excused" {
			var entered int
			db.QueryRow(`SELECT COUNT(*) FROM participation_entries WHERE board_id = ? AND member_id = ?`, boardID, memberID).Scan(&entered)
			if entered == 0 {
				q = `UPDATE participation_exceptions SET kind = 'missed', reason = '', recorded_by = NULL WHERE board_id = ? AND member_id = ?`
			}
		}
		if _, err := db.Exec(q, boardID, memberID); err != nil {
			slog.Error("handleParticipationException: delete failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		logActivity(u.ID, u.Username, "deleted", "participation_exception", name, false, kind+" on "+event)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var body struct {
		MemberID int    `json:"member_id"`
		Kind     string `json:"kind"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Kind != "excused" && body.Kind != "dismissed" {
		http.Error(w, "kind must be excused or dismissed", http.StatusBadRequest)
		return
	}
	if body.Kind == "excused" && body.Reason == "" {
		http.Error(w, "An excuse needs a reason", http.StatusBadRequest)
		return
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM members WHERE id = ?`, body.MemberID).Scan(&name); err != nil {
		http.Error(w, "Member not found", http.StatusBadRequest)
		return
	}
	var existing string
	db.QueryRow(`SELECT kind FROM participation_exceptions WHERE board_id = ? AND member_id = ?`, boardID, body.MemberID).Scan(&existing)
	if existing == "missed" && body.Kind == "dismissed" {
		// The stored miss IS this member's status on a legacy board (no role to derive
		// one from); dismissing would overwrite it and the member would vanish.
		http.Error(w, "This absence was recorded on the old Storm Attendance screen — excuse it with a reason if it should not count", http.StatusConflict)
		return
	}
	if _, err := db.Exec(`
		INSERT INTO participation_exceptions (board_id, member_id, kind, reason, recorded_by)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(board_id, member_id) DO UPDATE SET
			kind = excluded.kind, reason = excluded.reason,
			recorded_by = excluded.recorded_by, created_at = CURRENT_TIMESTAMP`,
		boardID, body.MemberID, body.Kind, body.Reason, u.ID); err != nil {
		slog.Error("handleParticipationException: upsert failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	details := body.Kind + " on " + event
	if body.Reason != "" {
		details += ": " + body.Reason
	}
	logActivity(u.ID, u.Username, "created", "participation_exception", name, false, details)
	w.WriteHeader(http.StatusNoContent)
}
