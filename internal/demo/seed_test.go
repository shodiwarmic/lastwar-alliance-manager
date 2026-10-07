package demo

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lastwar-alliance/internal/gametime"
)

var anchor = time.Date(2026, 10, 5, 0, 0, 0, 0, gametime.Loc)

func seedTemp(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.db")
	if _, err := SeedFile(path, "../../migrations", Options{UploadsDir: filepath.Join(dir, "up"), Today: anchor, Password: "test-pass"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn, dir
}

func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// seededTables is every table the generator fills.
var seededTables = []string{
	"members", "users", "member_aliases", "member_skills",
	"power_history", "hero_power_history", "kill_history", "squad_power_history",
	"hq_level_history", "profession_level_history", "vs_points",
	"vs_league_seasons", "vs_league_weeks", "vs_league_days", "vs_league_matchups",
	"schedule_events", "seasons", "season_score_levels", "season_reward_tiers", "season_trackables",
	"season_participation", "season_member_records", "season_rewards",
	"participation_boards", "participation_entries", "participation_values", "participation_roles",
	"participation_exceptions", "accountability_strikes", "train_logs", "eligibility_rules",
	"storm_registrations", "storm_groups", "storm_group_buildings", "storm_group_building_members",
	"storm_group_members", "prospects", "allies", "ally_agreements", "external_alliances",
	"poll_templates", "poll_instances", "poll_responses", "poll_anonymous_counts",
	"dyno_recommendations", "oc_categories", "oc_responsibilities", "oc_assignees",
	"files", "file_tags", "file_tag_map", "lastrank_pending_changes", "activity_log",
}

func TestSeedCoversEveryArea(t *testing.T) {
	db, _ := seedTemp(t)
	for _, tbl := range seededTables {
		if n := count(t, db, "SELECT COUNT(*) FROM "+tbl); n == 0 {
			t.Errorf("%s is empty", tbl)
		}
	}
	if n := count(t, db, `SELECT COUNT(*) FROM users`); n != 6 {
		t.Errorf("users = %d, want 6", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM members`); n != 100 {
		t.Errorf("members = %d, want 100", n)
	}
	if n := count(t, db, `SELECT COUNT(*) - COUNT(DISTINCT name) FROM members`); n != 0 {
		t.Errorf("%d duplicate member names", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM users u JOIN members m ON m.id = u.member_id WHERE m.rank = 'EX'`); n != 0 {
		t.Errorf("%d former members have an account", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM users WHERE force_password_change != 0`); n != 0 {
		t.Errorf("%d accounts would be forced to change password", n)
	}
	if n := count(t, db, `SELECT pwd_validity_days FROM settings WHERE id = 1`); n != 0 {
		t.Errorf("pwd_validity_days = %d, want 0", n)
	}
	// The sandworm cutover reads Season 3; it must exist and fall inside the schedule.
	var cutover string
	if err := db.QueryRow(`SELECT date(start_date, '+57 days') FROM seasons WHERE season_number = 3`).Scan(&cutover); err != nil {
		t.Fatalf("no Season 3: %v", err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM schedule_events se JOIN schedule_event_types t ON t.id = se.event_type_id
		WHERE t.short_name = 'LS' AND se.event_date >= ?`, cutover); n == 0 {
		t.Error("no Large Sandworm after the cutover")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM schedule_events se JOIN schedule_event_types t ON t.id = se.event_type_id
		WHERE t.short_name = 'MG' AND se.event_date < ?`, cutover); n == 0 {
		t.Error("no Marshal's Guard before the cutover")
	}
	// Rule 2: our own alliance is never in the registry.
	if n := count(t, db, `SELECT COUNT(*) FROM external_alliances WHERE tag = ?`, AllianceTag); n != 0 {
		t.Error("our own alliance is in external_alliances")
	}
}

// Nothing in a history table is dated after the anchor (the reference instant is the
// anchor's 20:00 game time, 22:00 UTC).
func TestSeedNeverDatesHistoryAfterTheAnchor(t *testing.T) {
	db, _ := seedTemp(t)
	limit := "2026-10-05 22:00:00"
	for tbl, col := range map[string]string{
		"power_history": "recorded_at", "kill_history": "recorded_at", "hq_level_history": "recorded_at",
		"activity_log": "created_at", "accountability_strikes": "created_at", "train_logs": "date",
		"participation_boards": "created_at", "season_participation": "created_at",
	} {
		if n := count(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s > ?`, tbl, col), limit); n != 0 {
			t.Errorf("%s: %d rows dated after the anchor", tbl, n)
		}
	}
	if n := count(t, db, `SELECT COUNT(*) FROM vs_points WHERE week_date > '2026-10-05'`); n != 0 {
		t.Errorf("%d VS weeks after the anchor's week", n)
	}
}

// fingerprint hashes each seeded table's rows, in rowid order. users.password is left
// out: bcrypt salts every hash.
func fingerprint(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	// Plus the seed's own rows in tables the migrations also fill (whose rows carry the
	// migration's CURRENT_TIMESTAMP, which no seed controls).
	extra := map[string]string{"schedule_event_types": "is_system = 0", "settings": "1", "storm_tf_config": "1"}
	for _, tbl := range append(seededTables, "settings", "schedule_event_types", "storm_tf_config") {
		h := sha256.New()
		where := "1"
		if w, ok := extra[tbl]; ok {
			where = w
		}
		rows, err := db.Query("SELECT * FROM " + tbl + " WHERE " + where + " ORDER BY rowid")
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			rows.Scan(ptrs...)
			for i, c := range cols {
				if tbl == "users" && c == "password" {
					continue
				}
				fmt.Fprintf(h, "%s.%s=%v;", tbl, c, vals[i])
			}
		}
		rows.Close()
		out[tbl] = fmt.Sprintf("%x", h.Sum(nil))
	}
	return out
}

func TestSeedIsDeterministic(t *testing.T) {
	a, _ := seedTemp(t)
	time.Sleep(1100 * time.Millisecond) // a CURRENT_TIMESTAMP default would now differ
	b, _ := seedTemp(t)
	fa, fb := fingerprint(t, a), fingerprint(t, b)
	for tbl := range fa {
		if fa[tbl] != fb[tbl] {
			t.Errorf("%s differs between two seeds with the same anchor", tbl)
		}
	}
}

func TestSeedIfEmptyLeavesASeededDatabaseAlone(t *testing.T) {
	db, dir := seedTemp(t)
	before := count(t, db, `SELECT COUNT(*) FROM activity_log`)
	seeded, _, err := SeedIfEmpty(db, Options{UploadsDir: filepath.Join(dir, "up"), Today: anchor})
	if err != nil || seeded {
		t.Fatalf("SeedIfEmpty on a seeded file: seeded=%v err=%v", seeded, err)
	}
	if after := count(t, db, `SELECT COUNT(*) FROM activity_log`); after != before {
		t.Errorf("SeedIfEmpty wrote %d rows", after-before)
	}
	if _, err := Seed(db, Options{UploadsDir: filepath.Join(dir, "up"), Today: anchor}); err != ErrNotEmpty {
		t.Errorf("Seed over a seeded file: err = %v, want ErrNotEmpty", err)
	}
}

func TestManifestMatchesTheSeed(t *testing.T) {
	db, _ := seedTemp(t)
	md := Manifest()
	for _, a := range md.Accounts {
		var user string
		var member sql.NullString
		if err := db.QueryRow(`SELECT u.username, m.name FROM users u LEFT JOIN members m ON m.id = u.member_id WHERE u.id = ?`, a.UserID).Scan(&user, &member); err != nil {
			t.Fatalf("account %s: %v", a.Username, err)
		}
		if user != a.Username || member.String != a.Member {
			t.Errorf("account %d: db has %s/%s, manifest %s/%s", a.UserID, user, member.String, a.Username, a.Member)
		}
	}
	for _, f := range md.Files {
		var title, kind string
		if err := db.QueryRow(`SELECT title, file_type FROM files WHERE id = ?`, f.ID).Scan(&title, &kind); err != nil || title != f.Title || kind != f.Kind {
			t.Errorf("file %d: db %q/%q, manifest %q/%q (%v)", f.ID, title, kind, f.Title, f.Kind, err)
		}
	}
	names := map[string]bool{}
	rows, _ := db.Query(`SELECT name FROM members WHERE rank != 'EX'`)
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names[n] = true
	}
	rows.Close()
	cats := append(append([]string{}, RankingCategories...), MailCategories...)
	if len(cats) != 26 {
		t.Fatalf("%d categories, want 26", len(cats))
	}
	for _, c := range cats {
		b := md.Boards[c]
		if len(b) == 0 {
			t.Errorf("no canned board for %s", c)
		}
		for i, row := range b {
			if !names[row.Name] {
				t.Errorf("%s: %q is not an active seeded member", c, row.Name)
			}
			if row.Rank != i+1 || (i > 0 && row.Score > b[i-1].Score) {
				t.Errorf("%s: row %d out of order", c, i)
			}
		}
	}
	// The seeded VS week the canned Monday board describes is the anchor's.
	top := md.Boards["power"][0]
	var power int64
	db.QueryRow(`SELECT p.power FROM power_history p JOIN members m ON m.id = p.member_id WHERE m.name = ? ORDER BY recorded_at DESC LIMIT 1`, top.Name).Scan(&power)
	if diff := float64(power-top.Score) / float64(top.Score); diff > 0.02 || diff < -0.02 {
		t.Errorf("power board top %s: %d, latest seeded %d", top.Name, top.Score, power)
	}
}

func TestNamesLookLikeInGameNames(t *testing.T) {
	names := generateNames(100)
	seen := map[string]bool{}
	folded := map[string]int{}
	for _, n := range names {
		if seen[n] {
			t.Errorf("duplicate %q", n)
		}
		seen[n] = true
		folded[foldKey(n)]++
		if strings.TrimSpace(n) != n || n == "" {
			t.Errorf("bad name %q", n)
		}
	}
	twins := 0
	for _, c := range folded {
		if c > 1 {
			twins++
		}
	}
	if twins != accentTwins {
		t.Errorf("%d accent-twin pairs, want %d", twins, accentTwins)
	}
}
