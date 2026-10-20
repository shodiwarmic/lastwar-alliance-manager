package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// seedSeasonRow inserts a season directly. archived marks it archived and inactive.
func seedSeasonRow(t *testing.T, id, number int, start string, active, archived bool) {
	t.Helper()
	archivedAt := any(nil)
	if archived {
		archivedAt = "2026-01-01 00:00:00"
	}
	if _, err := db.Exec(`INSERT INTO seasons (id, name, season_number, start_date, week_count,
		key_event_name, key_event_required, tier_active_min_pct, tier_at_risk_min_pct, is_active, archived_at)
		VALUES (?, ?, ?, ?, 8, 'Copper War', 4, 70, 60, ?, ?)`,
		id, "Season "+strconv.Itoa(number), number, start, boolToInt(active), archivedAt); err != nil {
		t.Fatalf("seed season: %v", err)
	}
}

func mustExecT(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func countT(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func createSeason(t *testing.T, body string) (int, *httptest.ResponseRecorder) {
	t.Helper()
	rr := httptest.NewRecorder()
	handleSeasonCreate(rr, shReq(http.MethodPost, body, nil, nil))
	var out struct {
		ID int `json:"id"`
	}
	json.Unmarshal(rr.Body.Bytes(), &out)
	return out.ID, rr
}

func TestSeasonCreateModes(t *testing.T) {
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	m := seedMember(t, "Alpha", "R3")

	// Blank: default levels and tiers and the layout, nothing else.
	blank, rr := createSeason(t, `{"mode":"blank","name":"Test","season_number":40,"week_count":6,"start_date":"2031-01-06"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("blank: %d %s", rr.Code, rr.Body.String())
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_events WHERE season_id = ?`, blank); n != 0 {
		t.Errorf("blank season has %d events", n)
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_trackables WHERE season_id = ?`, blank); n != 0 {
		t.Errorf("blank season has %d trackables", n)
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_reward_tiers WHERE season_id = ?`, blank); n != 4 {
		t.Errorf("blank season has %d tiers, want the global four", n)
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_tabs WHERE season_id = ?`, blank); n != len(seasonTabKeys) {
		t.Errorf("blank season has %d tab rows", n)
	}
	if n := countT(t, `SELECT week_count FROM seasons WHERE id = ?`, blank); n != 6 {
		t.Errorf("blank week_count = %d", n)
	}

	// Template: S4's rows, tiers and new defaults.
	var tmplID int
	db.QueryRow(`SELECT id FROM season_templates WHERE season_number = 4`).Scan(&tmplID)
	tmpl, rr := createSeason(t, `{"template_id":`+strconv.Itoa(tmplID)+`,"start_date":"2031-03-03"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("template: %d %s", rr.Code, rr.Body.String())
	}
	var total int
	var resource string
	db.QueryRow(`SELECT key_event_total, resource_name FROM seasons WHERE id = ?`, tmpl).Scan(&total, &resource)
	if total != 8 || resource != "Copper" {
		t.Errorf("template season: key_event_total %d resource %q", total, resource)
	}
	mustExecT(t, `UPDATE seasons SET war_active_min_pct = 80, schedule_buffer_minutes = 45 WHERE id = ?`, tmpl)
	mustExecT(t, `UPDATE season_tabs SET label = 'Wars', position = 0 WHERE season_id = ? AND tab_key = 'war'`, tmpl)
	// Records the copy must not take.
	mustExecT(t, `INSERT INTO season_participation (season_id, member_id, week_number, score) VALUES (?, ?, 1, 'full')`, tmpl, m)
	mustExecT(t, `INSERT INTO season_rewards (season_id, member_id, reward_tier, participation_pct, logged_by) VALUES (?, ?, 'core', 90, 1)`, tmpl, m)

	// Copy: the configuration, never the records.
	cp, rr := createSeason(t, `{"mode":"copy","source_season_id":`+strconv.Itoa(tmpl)+`,"name":"Copied","season_number":41,"start_date":"2031-06-02"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("copy: %d %s", rr.Code, rr.Body.String())
	}
	for _, q := range []string{
		`SELECT COUNT(*) FROM season_events WHERE season_id = ?`,
		`SELECT COUNT(*) FROM season_trackables WHERE season_id = ?`,
		`SELECT COUNT(*) FROM season_reward_tiers WHERE season_id = ?`,
		`SELECT COUNT(*) FROM season_score_levels WHERE season_id = ?`,
	} {
		if a, b := countT(t, q, tmpl), countT(t, q, cp); a != b || a == 0 {
			t.Errorf("%s: source %d, copy %d", q, a, b)
		}
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_participation WHERE season_id = ?`, cp); n != 0 {
		t.Errorf("copy took %d participation rows", n)
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_rewards WHERE season_id = ?`, cp); n != 0 {
		t.Errorf("copy took %d rewards", n)
	}
	var warFloor, buffer int
	db.QueryRow(`SELECT war_active_min_pct, schedule_buffer_minutes FROM seasons WHERE id = ?`, cp).Scan(&warFloor, &buffer)
	if warFloor != 80 || buffer != 45 {
		t.Errorf("copy floors/buffer = %d/%d, want 80/45", warFloor, buffer)
	}
	var label string
	db.QueryRow(`SELECT label FROM season_tabs WHERE season_id = ? AND tab_key = 'war'`, cp).Scan(&label)
	if label != "Wars" {
		t.Errorf("copied tab label %q, want Wars", label)
	}

	// A duplicate number is refused in every mode.
	if _, rr := createSeason(t, `{"mode":"blank","name":"Dup","season_number":40,"week_count":8,"start_date":"2031-09-01"}`); rr.Code != http.StatusConflict {
		t.Errorf("duplicate season number: %d", rr.Code)
	}
}

// The archive service freezes the roster on every path that archives a season.
func TestArchiveFreezesTheRosterOnEveryPath(t *testing.T) {
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	alpha := seedMember(t, "Alpha", "R4")
	gone := seedMember(t, "Gone", "EX")
	seedMember(t, "Bystander", "EX") // left, no rows: not on the roster

	snapshot := func(seasonID int) map[int]string {
		rows, _ := db.Query(`SELECT member_id, name FROM season_roster_snapshot WHERE season_id = ?`, seasonID)
		defer rows.Close()
		out := map[int]string{}
		for rows.Next() {
			var id int
			var n string
			rows.Scan(&id, &n)
			out[id] = n
		}
		return out
	}

	// 1. The Archive button.
	seedSeasonRow(t, 1, 1, "2026-01-05", true, false)
	mustExecT(t, `INSERT INTO season_participation (season_id, member_id, week_number, score) VALUES (1, ?, 1, 'full')`, gone)
	rr := httptest.NewRecorder()
	handleSeasonArchive(rr, shReq(http.MethodPost, "", map[string]string{"id": "1"}, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("archive: %d %s", rr.Code, rr.Body.String())
	}
	if snap := snapshot(1); len(snap) != 2 || snap[alpha] != "Alpha" || snap[gone] != "Gone" {
		t.Errorf("archive button snapshot = %v, want Alpha (current) and Gone (has rows)", snap)
	}

	// 2. A season created to start today archives the active one.
	seedSeasonRow(t, 2, 2, "2026-03-02", true, false)
	if _, rr := createSeason(t, `{"mode":"blank","name":"Now","season_number":3,"week_count":8,"start_date":"`+gameDate()+`"}`); rr.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	if n := countT(t, `SELECT COUNT(*) FROM seasons WHERE id = 2 AND archived_at IS NOT NULL AND is_active = 0`); n != 1 {
		t.Error("create did not archive the active season")
	}
	if snap := snapshot(2); snap[alpha] != "Alpha" {
		t.Errorf("create's archive snapshot = %v", snap)
	}

	// 3. Activation on a read archives the active one too.
	activeID := activeSeasonID(db)
	seedSeasonRow(t, 9, 9, "2026-01-01", false, false) // pending, start passed
	autoActivateUpcomingSeason()
	if n := countT(t, `SELECT is_active FROM seasons WHERE id = 9`); n != 1 {
		t.Fatal("pending season not activated")
	}
	if snap := snapshot(activeID); snap[alpha] != "Alpha" {
		t.Errorf("auto-activation's archive snapshot = %v", snap)
	}
}

// A copy that fails archives nothing: the season stays open, no snapshot is written.
func TestArchiveRefusedWhenAFileCopyFails(t *testing.T) {
	setupSettingsTestDB(t)
	seedMember(t, "Alpha", "R4")
	seedSeasonRow(t, 1, 1, "2026-01-05", true, false)

	prevList, prevCopy := seasonFilesToCopy, copySeasonFile
	t.Cleanup(func() { seasonFilesToCopy, copySeasonFile = prevList, prevCopy })
	seasonFilesToCopy = func(int) ([]seasonFileCopy, error) {
		return []seasonFileCopy{{FileID: 7, Title: "Battle plan", FileName: "x.docx"}}, nil
	}
	copySeasonFile = func(string, seasonFileCopy) error { return errors.New("disk full") }

	rr := httptest.NewRecorder()
	handleSeasonArchive(rr, shReq(http.MethodPost, "", map[string]string{"id": "1"}, nil))
	if rr.Code != http.StatusInternalServerError || !strings.Contains(rr.Body.String(), "Battle plan") {
		t.Errorf("status %d %q, want a 500 naming the file", rr.Code, rr.Body.String())
	}
	if n := countT(t, `SELECT COUNT(*) FROM seasons WHERE id = 1 AND archived_at IS NULL AND is_active = 1`); n != 1 {
		t.Error("a failed copy still archived the season")
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_roster_snapshot`); n != 0 {
		t.Errorf("a failed copy wrote %d snapshot rows", n)
	}
}

func TestArchivedSeasonRefusesConfigurationWrites(t *testing.T) {
	setupSettingsTestDB(t)
	seedSeasonRow(t, 1, 1, "2026-01-05", false, true)
	mustExecT(t, `INSERT INTO season_trackables (id, season_id, key, label) VALUES (5, 1, 'siege', 'Siege')`)
	mustExecT(t, `INSERT INTO season_events (id, season_id, label) VALUES (6, 1, 'War')`)

	cases := []struct {
		name string
		h    http.HandlerFunc
		body string
		vars map[string]string
	}{
		{"season update", handleSeasonUpdate, `{"name":"X","season_number":1,"start_date":"2026-01-05","week_count":8}`, map[string]string{"id": "1"}},
		{"trackable create", handleSeasonTrackableCreate, `{"season_id":1,"key":"k","label":"L"}`, nil},
		{"trackable update", handleSeasonTrackableUpdate, `{"label":"L"}`, map[string]string{"id": "5"}},
		{"trackable delete", handleSeasonTrackableDelete, ``, map[string]string{"id": "5"}},
		{"event create", handleSeasonEventCreate, `{"season_id":1,"label":"New"}`, nil},
		{"event update", handleSeasonEventUpdate, `{"label":"Renamed"}`, map[string]string{"id": "6"}},
		{"event delete", handleSeasonEventDelete, ``, map[string]string{"id": "6"}},
		{"layout", handleSeasonLayoutPut, `{"tabs":[],"cards":[]}`, map[string]string{"id": "1"}},
	}
	for _, c := range cases {
		rr := httptest.NewRecorder()
		c.h(rr, shReq(http.MethodPost, c.body, c.vars, nil))
		if rr.Code != http.StatusConflict {
			t.Errorf("%s on an archived season: %d %s, want 409", c.name, rr.Code, rr.Body.String())
		}
	}
}

func TestLayoutReaderMergesWithoutWriting(t *testing.T) {
	setupSettingsTestDB(t)
	seedSeasonRow(t, 1, 1, "2026-01-05", true, false)
	// A season with two stored rows, out of default order, one renamed and hidden.
	mustExecT(t, `INSERT INTO season_tabs (season_id, tab_key, visible, label, position) VALUES (1, 'war', 1, 'Wars', 0), (1, 'rankings', 0, '', 1)`)
	l, err := loadSeasonLayout(db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Tabs) != len(seasonTabKeys) || len(l.Cards) != len(seasonCardKeys) {
		t.Fatalf("merged %d tabs / %d cards", len(l.Tabs), len(l.Cards))
	}
	if l.Tabs[0].Key != "war" || l.Tabs[0].Label != "Wars" || l.Tabs[1].Visible {
		t.Errorf("stored rows lost their order, label or visibility: %+v", l.Tabs[:2])
	}
	if l.Tabs[2].Key != "dashboard" || !l.Tabs[2].Visible {
		t.Errorf("a missing key should follow, in default order and visible: %+v", l.Tabs[2])
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_tabs`); n != 2 {
		t.Errorf("the reader wrote rows: %d stored", n)
	}
}

// An archived season reads its frozen roster: a rename since keeps the old name, a recruit
// since is not on it, and a deleted member's rows go with them.
func TestArchivedHubReadsTheFrozenRoster(t *testing.T) {
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	stay := seedMember(t, "Before", "R3")
	left := seedMember(t, "Leaver", "R2")
	doomed := seedMember(t, "Doomed", "R1")
	seedSeasonRow(t, 1, 1, "2026-01-05", true, false)
	for _, id := range []int{stay, left, doomed} {
		mustExecT(t, `INSERT INTO season_participation (season_id, member_id, week_number, score) VALUES (1, ?, 1, 'full')`, id)
	}
	if err := archiveSeason(1, "2026-03-01"); err != nil {
		t.Fatal(err)
	}
	mustExecT(t, `UPDATE members SET name = 'After' WHERE id = ?`, stay)
	mustExecT(t, `UPDATE members SET rank = 'EX' WHERE id = ?`, left)
	seedMember(t, "Recruit", "R1")
	tx, _ := db.Begin()
	if err := deleteMemberTx(tx, doomed); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	req := shReq(http.MethodGet, "", nil, nil)
	req.URL.RawQuery = "season_id=1"
	rr := httptest.NewRecorder()
	handleSeasonHubData(rr, req)
	var out struct {
		Members []SeasonMember `json:"members"`
	}
	json.Unmarshal(rr.Body.Bytes(), &out)
	names := map[string]SeasonMember{}
	for _, m := range out.Members {
		names[m.Name] = m
	}
	if m, ok := names["Before"]; !ok || m.CurrentName != "After" || m.ProfileID == nil {
		t.Errorf("renamed member: %+v (want frozen Before, now After, with a profile)", m)
	}
	if _, ok := names["Leaver"]; !ok {
		t.Error("a member who left after the season is missing")
	}
	if _, ok := names["Recruit"]; ok {
		t.Error("a recruit since the archive is on the archived roster")
	}
	if _, ok := names["Doomed"]; ok {
		t.Error("a deleted member is still on the roster")
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_participation WHERE member_id = ?`, doomed); n != 0 {
		t.Errorf("the deleted member's %d participation rows were left behind", n)
	}
}

// A season delete leaves no row naming it in any table with a season_id column.
func TestSeasonDeleteLeavesNoChildRows(t *testing.T) {
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	m := seedMember(t, "Alpha", "R3")
	var tmplID int
	db.QueryRow(`SELECT id FROM season_templates WHERE season_number = 4`).Scan(&tmplID)
	id, rr := createSeason(t, `{"template_id":`+strconv.Itoa(tmplID)+`,"start_date":"2031-03-03"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("create: %d", rr.Code)
	}
	mustExecT(t, `INSERT INTO season_participation (season_id, member_id, week_number, score) VALUES (?, ?, 1, 'full')`, id, m)
	var tk int
	db.QueryRow(`SELECT id FROM season_trackables WHERE season_id = ? LIMIT 1`, id).Scan(&tk)
	mustExecT(t, `INSERT INTO season_member_records (season_id, member_id, week_number, trackable_id, recorded_value) VALUES (?, ?, 0, ?, 5)`, id, m, tk)
	mustExecT(t, `INSERT INTO season_rewards (season_id, member_id, reward_tier, participation_pct, logged_by) VALUES (?, ?, 'core', 90, 1)`, id, m)
	if err := archiveSeason(id, "2031-05-01"); err != nil {
		t.Fatal(err)
	}

	rr = httptest.NewRecorder()
	handleSeasonDelete(rr, shReq(http.MethodDelete, "", map[string]string{"id": strconv.Itoa(id)}, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rr.Code, rr.Body.String())
	}
	assertNoSeasonRows(t, id)
}

// assertNoSeasonRows checks every table carrying a season_id column.
func assertNoSeasonRows(t *testing.T, seasonID int) {
	t.Helper()
	rows, err := db.Query(`SELECT m.name FROM sqlite_master m JOIN pragma_table_info(m.name) p
		WHERE m.type = 'table' AND p.name = 'season_id'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		tables = append(tables, n)
	}
	rows.Close()
	if len(tables) < 5 {
		t.Fatalf("only found %v", tables)
	}
	for _, tbl := range tables {
		if n := countT(t, `SELECT COUNT(*) FROM `+tbl+` WHERE season_id = ?`, seasonID); n != 0 {
			t.Errorf("%s still holds %d row(s) for the deleted season", tbl, n)
		}
	}
}
