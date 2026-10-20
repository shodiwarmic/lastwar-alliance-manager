// season_lifecycle.go — a season's layout, archive, configuration copy and frozen roster.
//
// Three rules hold everything here together (Project 21, private-docs 270):
//
//   - An archived season never moves. Every write to a season's configuration checks
//     seasonAcceptsEdits, and every archive — the Archive button, a season created to start at
//     once, and autoActivateUpcomingSeason — goes through one service that freezes the roster
//     (and, from private-docs 272, copies the season's files) in the same act.
//   - No write on a read. Layout rows are seeded by migration and at create; a reader merges
//     defaults in memory for any key a season lacks.
//   - foreign_keys is off, so the season delete names every child table (seasonChildTables).

package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gorilla/mux"
)

// seasonLayoutKey is one tab or card key with its default label, in default order.
type seasonLayoutKey struct {
	Key   string
	Label string
}

// seasonTabKeys are the Hub's tabs in default order. A tab whose panel the viewer may not see
// is still listed; the page shows only the panels its template rendered.
var seasonTabKeys = []seasonLayoutKey{
	{"dashboard", "Dashboard"},
	{"rankings", "Rankings"},
	{"participation", "Participation"},
	{"contributions", "Contributions"},
	{"rewards", "Rewards"},
	{"land", "Land"},
	{"war", "War"},
	{"schedule", "Schedule"},
	{"comms", "Comms"},
	{"files", "Files"},
}

// seasonCardKeys are the season dashboard's cards in default order.
var seasonCardKeys = []seasonLayoutKey{
	{"faction_allies", "Faction Allies and Agreements"},
	{"leadership_flags", "Leadership Flags"},
	{"season_schedule", "Season Schedule"},
	{"land_status", "Land Path Status"},
	{"war_record", "War Record"},
	{"reward_tier_status", "Reward Tier Status"},
}

// seasonLayoutItem is one tab or card as a season shows it.
type seasonLayoutItem struct {
	Key          string `json:"key"`
	Label        string `json:"label"` // the officer's label; empty means the default
	DefaultLabel string `json:"default_label"`
	Visible      bool   `json:"visible"`
	Position     int    `json:"position"`
}

type seasonLayout struct {
	Tabs  []seasonLayoutItem `json:"tabs"`
	Cards []seasonLayoutItem `json:"cards"`
}

// seedSeasonLayoutTx writes the default tab and card rows for a new season.
func seedSeasonLayoutTx(tx *sql.Tx, seasonID int64) error {
	for i, k := range seasonTabKeys {
		if _, err := tx.Exec(`INSERT INTO season_tabs (season_id, tab_key, visible, position) VALUES (?, ?, 1, ?)
			ON CONFLICT(season_id, tab_key) DO NOTHING`, seasonID, k.Key, i); err != nil {
			return err
		}
	}
	for i, k := range seasonCardKeys {
		if _, err := tx.Exec(`INSERT INTO season_dashboard_cards (season_id, card_key, visible, position) VALUES (?, ?, 1, ?)
			ON CONFLICT(season_id, card_key) DO NOTHING`, seasonID, k.Key, i); err != nil {
			return err
		}
	}
	return nil
}

// mergeLayout orders a season's stored rows by position and appends, in default order, any
// known key the season has no row for. Rows naming a key this release does not know are dropped.
func mergeLayout(keys []seasonLayoutKey, stored map[string]seasonLayoutItem) []seasonLayoutItem {
	var have, missing []seasonLayoutItem
	for _, k := range keys {
		if it, ok := stored[k.Key]; ok {
			it.DefaultLabel = k.Label
			have = append(have, it)
		} else {
			missing = append(missing, seasonLayoutItem{Key: k.Key, DefaultLabel: k.Label, Visible: true})
		}
	}
	// Stable insertion sort by stored position (lists are ten items long).
	for i := 1; i < len(have); i++ {
		for j := i; j > 0 && have[j].Position < have[j-1].Position; j-- {
			have[j], have[j-1] = have[j-1], have[j]
		}
	}
	out := append(have, missing...)
	for i := range out {
		out[i].Position = i
	}
	return out
}

// loadSeasonLayout reads a season's tabs and cards, merged with the defaults. Read-only.
func loadSeasonLayout(q rowQueryer, seasonID int) (seasonLayout, error) {
	read := func(query string) (map[string]seasonLayoutItem, error) {
		rows, err := q.Query(query, seasonID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := map[string]seasonLayoutItem{}
		for rows.Next() {
			var it seasonLayoutItem
			var vis int
			if err := rows.Scan(&it.Key, &vis, &it.Label, &it.Position); err != nil {
				return nil, err
			}
			it.Visible = vis == 1
			out[it.Key] = it
		}
		return out, rows.Err()
	}
	tabs, err := read(`SELECT tab_key, visible, label, position FROM season_tabs WHERE season_id = ?`)
	if err != nil {
		return seasonLayout{}, err
	}
	cards, err := read(`SELECT card_key, visible, label, position FROM season_dashboard_cards WHERE season_id = ?`)
	if err != nil {
		return seasonLayout{}, err
	}
	return seasonLayout{Tabs: mergeLayout(seasonTabKeys, tabs), Cards: mergeLayout(seasonCardKeys, cards)}, nil
}

// GET /api/season-hub/seasons/{id}/layout
func handleSeasonLayoutGet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid season ID", http.StatusBadRequest)
		return
	}
	layout, err := loadSeasonLayout(db, id)
	if err != nil {
		slog.Error("handleSeasonLayoutGet", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, layout)
}

// PUT /api/season-hub/seasons/{id}/layout — the whole layout as one body: each list in the
// order to show it, with visibility and label. Unknown keys are refused.
func handleSeasonLayoutPut(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid season ID", http.StatusBadRequest)
		return
	}
	var body seasonLayout
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if ok, msg := seasonAcceptsEdits(id); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	check := func(keys []seasonLayoutKey, items []seasonLayoutItem) string {
		known := map[string]bool{}
		for _, k := range keys {
			known[k.Key] = true
		}
		seen := map[string]bool{}
		for _, it := range items {
			if !known[it.Key] {
				return "unknown layout key: " + it.Key
			}
			if seen[it.Key] {
				return "duplicate layout key: " + it.Key
			}
			seen[it.Key] = true
			if len(it.Label) > 60 {
				return "a label is limited to 60 characters"
			}
		}
		return ""
	}
	if msg := check(seasonTabKeys, body.Tabs); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	if msg := check(seasonCardKeys, body.Cards); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	var name string
	db.QueryRow(`SELECT name FROM seasons WHERE id = ?`, id).Scan(&name)

	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleSeasonLayoutPut: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	write := func(table, keyCol string, items []seasonLayoutItem) error {
		for i, it := range items {
			if _, err := tx.Exec(`INSERT INTO `+table+` (season_id, `+keyCol+`, visible, label, position) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT(season_id, `+keyCol+`) DO UPDATE SET visible = excluded.visible, label = excluded.label,
				position = excluded.position`, id, it.Key, boolToInt(it.Visible), it.Label, i); err != nil {
				return err
			}
		}
		return nil
	}
	if err := write("season_tabs", "tab_key", body.Tabs); err != nil {
		slog.Error("handleSeasonLayoutPut: tabs", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := write("season_dashboard_cards", "card_key", body.Cards); err != nil {
		slog.Error("handleSeasonLayoutPut: cards", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handleSeasonLayoutPut: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "updated", "season_config", name, false, "layout")
	writeJSON(w, map[string]string{"message": "Layout saved"})
}

// seasonAcceptsEdits reports whether the season exists and is open to changes. An archived
// season is frozen: its configuration and every record hanging off it.
func seasonAcceptsEdits(seasonID int) (bool, string) {
	var archivedAt sql.NullString
	err := db.QueryRow(`SELECT archived_at FROM seasons WHERE id = ?`, seasonID).Scan(&archivedAt)
	if err == sql.ErrNoRows {
		return false, "Season not found"
	}
	if err != nil {
		slog.Error("seasonAcceptsEdits: load", "error", err)
		return false, "Database error"
	}
	if archivedAt.Valid && archivedAt.String != "" {
		return false, "Season is archived and cannot be modified"
	}
	return true, ""
}

// seasonEditRefusal writes seasonAcceptsEdits' refusal with its status.
func seasonEditRefusal(w http.ResponseWriter, msg string) {
	status := http.StatusConflict
	switch msg {
	case "Season not found":
		status = http.StatusNotFound
	case "Database error":
		status = http.StatusInternalServerError
	}
	http.Error(w, msg, status)
}

// ── Archive ────────────────────────────────────────────────────────────────

// seasonMemberRowsSQL selects the member ids with any row for season ?1 — the roster an
// archive freezes beyond the current members. Every per-member season table belongs here.
const seasonMemberRowsSQL = `
	SELECT member_id FROM season_participation WHERE season_id = ?1
	UNION SELECT member_id FROM season_member_records WHERE season_id = ?1
	UNION SELECT member_id FROM season_rewards WHERE season_id = ?1`

// seasonFileCopy is one file copied for an archive, before the archive commits.
type seasonFileCopy struct {
	FileID           int
	Title            string
	FileName         string
	FileType         string
	EffectiveMinRank string
	OwnerUserID      int
}

// seasonArchivePrep is an archive's file copies, made before its transaction opens (network
// and disk work never hold the one connection). discard removes them if the archive fails.
type seasonArchivePrep struct {
	SeasonID int
	Dir      string
	Files    []seasonFileCopy
}

func (p *seasonArchivePrep) discard() {
	if p == nil || p.Dir == "" {
		return
	}
	for _, f := range p.Files {
		os.Remove(filepath.Join(p.Dir, f.FileName))
	}
	os.Remove(p.Dir) // only if now empty
}

// seasonArchiveDir is where an archived season's file copies live.
func seasonArchiveDir(seasonID int) string {
	return filepath.Join(getStoragePath(), fmt.Sprintf("season-%d", seasonID))
}

// prepareSeasonArchive copies every file tagged to the season (seasonFilesToCopy) into the
// season's archive directory and verifies each copy. A failure removes what it copied and
// names the file: the season is then not archived at all.
func prepareSeasonArchive(seasonID int) (*seasonArchivePrep, error) {
	prep := &seasonArchivePrep{SeasonID: seasonID, Dir: seasonArchiveDir(seasonID)}
	files, err := seasonFilesToCopy(seasonID)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if err := copySeasonFile(prep.Dir, f); err != nil {
			prep.discard()
			return nil, fmt.Errorf("copy %q: %w", f.Title, err)
		}
		prep.Files = append(prep.Files, f)
	}
	return prep, nil
}

// commitSeasonArchiveTx freezes the roster and the file manifest and marks the season
// archived, inside the caller's transaction. endDate replaces end_date when overwriteEnd,
// else fills it only if empty.
func commitSeasonArchiveTx(tx *sql.Tx, prep *seasonArchivePrep, endDate string, overwriteEnd bool) error {
	id := prep.SeasonID
	// The current members plus everyone with a row for the season, as named and ranked now.
	if _, err := tx.Exec(`INSERT INTO season_roster_snapshot (season_id, member_id, name, rank)
		SELECT ?1, m.id, m.name, m.rank FROM members m
		WHERE m.rank != 'EX' OR m.id IN (`+seasonMemberRowsSQL+`)
		ON CONFLICT(season_id, member_id) DO NOTHING`, id); err != nil {
		return fmt.Errorf("roster snapshot: %w", err)
	}
	for _, f := range prep.Files {
		if _, err := tx.Exec(`INSERT INTO season_file_snapshots
			(season_id, file_id, title, file_name, file_type, effective_min_rank, owner_user_id)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(season_id, file_id) DO UPDATE SET title = excluded.title, file_name = excluded.file_name,
			file_type = excluded.file_type, effective_min_rank = excluded.effective_min_rank, snapshot_at = CURRENT_TIMESTAMP`,
			id, f.FileID, f.Title, f.FileName, f.FileType, f.EffectiveMinRank, f.OwnerUserID); err != nil {
			return fmt.Errorf("file snapshot: %w", err)
		}
	}
	q := `UPDATE seasons SET is_active = 0, archived_at = CURRENT_TIMESTAMP, end_date = COALESCE(end_date, ?) WHERE id = ?`
	if overwriteEnd {
		q = `UPDATE seasons SET is_active = 0, archived_at = CURRENT_TIMESTAMP, end_date = ? WHERE id = ?`
	}
	if _, err := tx.Exec(q, endDate, id); err != nil {
		return fmt.Errorf("mark archived: %w", err)
	}
	return nil
}

// archiveSeason is the Archive button's whole act: copies first, then one transaction.
func archiveSeason(seasonID int, endDate string) error {
	prep, err := prepareSeasonArchive(seasonID)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		prep.discard()
		return err
	}
	defer tx.Rollback()
	if err := commitSeasonArchiveTx(tx, prep, endDate, true); err != nil {
		prep.discard()
		return err
	}
	if err := tx.Commit(); err != nil {
		prep.discard()
		return err
	}
	return nil
}

// activeSeasonID returns the active season's id, or 0.
func activeSeasonID(q rowQuerier) int {
	var id int
	q.QueryRow(`SELECT id FROM seasons WHERE is_active = 1 LIMIT 1`).Scan(&id)
	return id
}

// ── The frozen roster ──────────────────────────────────────────────────────

// seasonRosterMember is one member of a season's roster as a reader shows it.
type seasonRosterMember struct {
	ID   int
	Name string
	Rank string
	// ProfileID is the live member row's id when it still exists — the link to the current
	// profile. CurrentName is that row's name when it differs from the frozen one.
	ProfileID   *int
	CurrentName string
}

// loadSeasonRoster lists the members a season reader shows. An archived season reads its
// frozen snapshot (a recruit since is not on it; a rename since keeps the old name); an open
// season reads the live roster — current members plus former members with rows for it.
func loadSeasonRoster(q rowQueryer, s *Season) ([]seasonRosterMember, error) {
	var rows *sql.Rows
	var err error
	if s.ArchivedAt != "" {
		rows, err = q.Query(`SELECT r.member_id, r.name, r.rank, m.id, COALESCE(m.name, '')
			FROM season_roster_snapshot r LEFT JOIN members m ON m.id = r.member_id
			WHERE r.season_id = ? ORDER BY r.name`, s.ID)
	} else {
		rows, err = q.Query(`SELECT m.id, m.name, m.rank, m.id, m.name FROM members m
			WHERE m.rank != 'EX' OR m.id IN (`+seasonMemberRowsSQL+`)
			ORDER BY m.name`, s.ID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []seasonRosterMember
	for rows.Next() {
		var m seasonRosterMember
		var pid sql.NullInt64
		if err := rows.Scan(&m.ID, &m.Name, &m.Rank, &pid, &m.CurrentName); err != nil {
			return nil, err
		}
		if pid.Valid {
			v := int(pid.Int64)
			m.ProfileID = &v
		}
		if m.CurrentName == m.Name {
			m.CurrentName = ""
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ── Delete lists ───────────────────────────────────────────────────────────

// seasonChildTables are deleted with their season, after its calendar purge. foreign_keys is
// off app-wide, so a table missing here is left behind (season_events, season_trackables and
// season_member_records were, before migration 089).
var seasonChildTables = []string{
	"season_participation", "season_score_levels", "season_rewards", "season_reward_tiers",
	"season_member_records", "season_trackables", "season_events",
	"season_tabs", "season_dashboard_cards", "season_roster_snapshot", "season_file_snapshots",
}

// seasonMemberTables are deleted with a member (deleteMemberTx): a member delete removes the
// member's history everywhere, archived seasons included.
var seasonMemberTables = []string{
	"season_participation", "season_member_records", "season_rewards", "season_roster_snapshot",
}

// seasonUserColumns are nulled when a user is deleted (clearUserReferences); the rows stay.
var seasonUserColumns = []struct{ table, column string }{
	{"season_participation", "recorded_by"},
	{"season_member_records", "logged_by"},
}

// ── Configuration copy ─────────────────────────────────────────────────────

// copySeasonConfigTx copies a season's configuration — never its records — onto a new season
// (decision 21): score levels, trackables, reward tiers, season events and the layout. Later
// configuration tables join it as they are added. Nothing a member did is copied:
// participation, contributions, rewards, snapshots, Comms, Files and the calendar stay put.
func copySeasonConfigTx(tx *sql.Tx, fromID int, toID int64) error {
	steps := []string{
		`INSERT INTO season_score_levels (season_id, key, label, points, sort_order)
			SELECT ?2, key, label, points, sort_order FROM season_score_levels WHERE season_id = ?1`,
		`INSERT INTO season_trackables (season_id, key, label, sort_order)
			SELECT ?2, key, label, sort_order FROM season_trackables WHERE season_id = ?1`,
		`INSERT INTO season_reward_tiers (season_id, key, label, slot_count, color, sort_order)
			SELECT ?2, key, label, slot_count, color, sort_order FROM season_reward_tiers WHERE season_id = ?1`,
		`INSERT INTO season_events (season_id, label, event_type_id, type_name, day_offset, event_time, all_day,
			level, week_start, week_end, notes, is_server_event, duration_days, template_key, source, grade)
			SELECT ?2, label, event_type_id, type_name, day_offset, event_time, all_day,
			level, week_start, week_end, notes, is_server_event, duration_days, template_key, source, grade
			FROM season_events WHERE season_id = ?1`,
		`INSERT INTO season_tabs (season_id, tab_key, visible, label, position)
			SELECT ?2, tab_key, visible, label, position FROM season_tabs WHERE season_id = ?1`,
		`INSERT INTO season_dashboard_cards (season_id, card_key, visible, label, position)
			SELECT ?2, card_key, visible, label, position FROM season_dashboard_cards WHERE season_id = ?1`,
	}
	for _, q := range steps {
		if _, err := tx.Exec(q, fromID, toID); err != nil {
			return err
		}
	}
	// A source season from before the layout tables still gets the defaults.
	return seedSeasonLayoutTx(tx, toID)
}

// seasonFilesToCopy lists the files tagged to a season (private-docs 272 adds the tag).
// Before files carry a season there is nothing to copy.
var seasonFilesToCopy = func(seasonID int) ([]seasonFileCopy, error) { return nil, nil }

// copySeasonFile copies one file's bytes into dir (private-docs 272).
var copySeasonFile = func(dir string, f seasonFileCopy) error { return nil }

// optionalInt tells a JSON field that was absent from one sent as null: absent keeps the
// stored value, null clears it (a category floor falling back to the overall floor).
type optionalInt struct {
	Set   bool
	Value *int
}

func (o *optionalInt) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var v int
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}
