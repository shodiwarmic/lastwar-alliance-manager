package main

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

// Migration 068 is the riskiest kind of change in this schema: it rebuilds
// season_rewards to drop a CHECK constraint SQLite cannot ALTER away, seeds a
// new table from columns it then drops, and does all of it over live reward
// data. Nothing else in CI applies a migration to a database, so these tests
// are the only thing standing between a subtly wrong step and silent data loss.
//
// The approach is to migrate to 067, write realistic fixture rows, then apply
// 068 on top — exercising the same path a real deployment takes.

const preTierMigrationVersion = 67

func migrateTo(t *testing.T, version int64) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "migrate.db")
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetMaxOpenConns(1)

	goose.SetDialect("sqlite3")
	goose.SetLogger(goose.NopLogger())
	if err := goose.UpTo(conn, "migrations", version); err != nil {
		t.Fatalf("goose.UpTo(%d): %v", version, err)
	}
	return conn
}

// seedPreMigrationFixture writes two seasons with distinct tier counts and a
// reward in every tier, so the migration has real data to carry across.
func seedPreMigrationFixture(t *testing.T, conn *sql.DB) {
	t.Helper()

	exec := func(q string, args ...any) sql.Result {
		t.Helper()
		res, err := conn.Exec(q, args...)
		if err != nil {
			t.Fatalf("fixture exec %q: %v", q, err)
		}
		return res
	}

	exec(`INSERT INTO users (id, username, password) VALUES (1, 'officer', 'x')`)
	exec(`INSERT INTO members (id, name, rank) VALUES (1, 'ShodiWarmic', 'R5'), (2, 'Pàcha', 'R4'),
	      (3, 'Third', 'R3'), (4, 'Fourth', 'R2')`)

	// Season 1 keeps the stock counts; season 2 uses distinct ones so a seed that
	// hardcodes the defaults instead of reading each season's row is caught.
	exec(`INSERT INTO seasons (id, name, season_number, start_date, week_count,
	        key_event_name, key_event_required, tier_active_min_pct, tier_at_risk_min_pct,
	        tier_count_leader, tier_count_core, tier_count_elite, tier_count_valued, is_active)
	      VALUES (1, 'Season I', 1, '2026-01-05', 8, 'Rare Soil War', 4, 70, 60, 1, 10, 20, 69, 0)`)
	exec(`INSERT INTO seasons (id, name, season_number, start_date, week_count,
	        key_event_name, key_event_required, tier_active_min_pct, tier_at_risk_min_pct,
	        tier_count_leader, tier_count_core, tier_count_elite, tier_count_valued, is_active)
	      VALUES (2, 'Season II', 2, '2026-04-06', 8, 'Rare Soil War', 4, 75, 65, 2, 12, 25, 80, 1)`)

	exec(`INSERT INTO season_rewards (id, season_id, member_id, reward_tier, participation_pct, contribution_pct, note, logged_by, logged_at)
	      VALUES (1, 1, 1, 'alliance_leader', 98.5, 42.0, 'top of the board', 1, '2026-03-01 10:00:00')`)
	exec(`INSERT INTO season_rewards (id, season_id, member_id, reward_tier, participation_pct, contribution_pct, note, logged_by, logged_at)
	      VALUES (2, 1, 2, 'core', 91.25, NULL, '', 1, '2026-03-01 10:05:00')`)
	exec(`INSERT INTO season_rewards (id, season_id, member_id, reward_tier, participation_pct, contribution_pct, note, logged_by, logged_at)
	      VALUES (3, 2, 3, 'elite', 80.0, 12.5, 'solid season', 1, '2026-06-01 09:00:00')`)
	exec(`INSERT INTO season_rewards (id, season_id, member_id, reward_tier, participation_pct, contribution_pct, note, logged_by, logged_at)
	      VALUES (4, 2, 4, 'valued', 72.5, 3.25, '', 1, '2026-06-01 09:10:00')`)
}

func TestMigration068SeedsTiersFromPerSeasonCounts(t *testing.T) {
	conn := migrateTo(t, preTierMigrationVersion)
	seedPreMigrationFixture(t, conn)

	if err := goose.Up(conn, "migrations"); err != nil {
		t.Fatalf("goose.Up: %v", err)
	}

	type tier struct {
		key       string
		label     string
		slotCount int
		color     string
		sortOrder int
	}
	want := map[int][]tier{
		1: {
			{"alliance_leader", "Alliance Leader", 1, "purple", 0},
			{"core", "Core", 10, "info", 1},
			{"elite", "Elite", 20, "success", 2},
			{"valued", "Valued", 69, "neutral", 3},
		},
		2: {
			{"alliance_leader", "Alliance Leader", 2, "purple", 0},
			{"core", "Core", 12, "info", 1},
			{"elite", "Elite", 25, "success", 2},
			{"valued", "Valued", 80, "neutral", 3},
		},
	}

	for seasonID, expected := range want {
		rows, err := conn.Query(`SELECT key, label, slot_count, color, sort_order
			FROM season_reward_tiers WHERE season_id = ? ORDER BY sort_order ASC`, seasonID)
		if err != nil {
			t.Fatalf("season %d: query tiers: %v", seasonID, err)
		}
		var got []tier
		for rows.Next() {
			var g tier
			if err := rows.Scan(&g.key, &g.label, &g.slotCount, &g.color, &g.sortOrder); err != nil {
				rows.Close()
				t.Fatalf("season %d: scan: %v", seasonID, err)
			}
			got = append(got, g)
		}
		rows.Close()

		if len(got) != len(expected) {
			t.Fatalf("season %d: got %d tiers, want %d", seasonID, len(got), len(expected))
		}
		for i := range expected {
			if got[i] != expected[i] {
				t.Errorf("season %d tier %d: got %+v, want %+v", seasonID, i, got[i], expected[i])
			}
		}
	}
}

func TestMigration068PreservesRewardRowsExactly(t *testing.T) {
	conn := migrateTo(t, preTierMigrationVersion)
	seedPreMigrationFixture(t, conn)

	type reward struct {
		id       int
		seasonID int
		memberID int
		tier     string
		partPct  float64
		contrib  sql.NullFloat64
		note     string
		loggedBy int
		loggedAt string
	}
	read := func() []reward {
		t.Helper()
		rows, err := conn.Query(`SELECT id, season_id, member_id, reward_tier, participation_pct,
			contribution_pct, note, logged_by, logged_at FROM season_rewards ORDER BY id ASC`)
		if err != nil {
			t.Fatalf("read rewards: %v", err)
		}
		defer rows.Close()
		var out []reward
		for rows.Next() {
			var r reward
			if err := rows.Scan(&r.id, &r.seasonID, &r.memberID, &r.tier, &r.partPct,
				&r.contrib, &r.note, &r.loggedBy, &r.loggedAt); err != nil {
				t.Fatalf("scan reward: %v", err)
			}
			out = append(out, r)
		}
		return out
	}

	before := read()
	if len(before) != 4 {
		t.Fatalf("fixture: got %d rewards, want 4", len(before))
	}

	if err := goose.Up(conn, "migrations"); err != nil {
		t.Fatalf("goose.Up: %v", err)
	}

	after := read()
	if len(after) != len(before) {
		t.Fatalf("row count changed across the rebuild: got %d, want %d", len(after), len(before))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("reward %d changed across the rebuild:\n before %+v\n after  %+v",
				before[i].id, before[i], after[i])
		}
	}

	// UNIQUE(season_id, member_id) must survive — handleRewardSave's upsert
	// depends on it as its conflict target.
	_, err := conn.Exec(`INSERT INTO season_rewards (season_id, member_id, reward_tier, participation_pct, logged_by)
		VALUES (1, 1, 'core', 50.0, 1)`)
	if err == nil {
		t.Error("expected UNIQUE(season_id, member_id) to reject a duplicate, but the insert succeeded")
	}
}

func TestMigration068DropsTheRewardTierCheckConstraint(t *testing.T) {
	conn := migrateTo(t, preTierMigrationVersion)
	seedPreMigrationFixture(t, conn)

	// Before: the CHECK rejects anything outside the fixed four.
	if _, err := conn.Exec(`INSERT INTO season_rewards (season_id, member_id, reward_tier, participation_pct, logged_by)
		VALUES (1, 3, 'vanguard', 60.0, 1)`); err == nil {
		t.Fatal("fixture precondition failed: the pre-068 CHECK constraint did not reject an unknown tier")
	}

	if err := goose.Up(conn, "migrations"); err != nil {
		t.Fatalf("goose.Up: %v", err)
	}

	// After: a fifth tier is storable. A CHECK is invisible to pragma_table_info,
	// so this has to be proven behaviourally.
	if _, err := conn.Exec(`INSERT INTO season_rewards (season_id, member_id, reward_tier, participation_pct, logged_by)
		VALUES (1, 3, 'vanguard', 60.0, 1)`); err != nil {
		t.Fatalf("post-068 insert of a fifth tier failed — the CHECK was not dropped: %v", err)
	}
}

func TestMigration068DropsDeadTierCountColumns(t *testing.T) {
	conn := migrateTo(t, preTierMigrationVersion)
	seedPreMigrationFixture(t, conn)

	if err := goose.Up(conn, "migrations"); err != nil {
		t.Fatalf("goose.Up: %v", err)
	}

	for _, col := range []string{"tier_count_leader", "tier_count_core", "tier_count_elite", "tier_count_valued"} {
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('seasons') WHERE name = ?`, col).Scan(&n); err != nil {
			t.Fatalf("pragma_table_info: %v", err)
		}
		if n != 0 {
			t.Errorf("seasons.%s still exists — a second source of truth for tier counts", col)
		}
	}

	// The participation thresholds share the "tier" prefix but are a different
	// concept and must survive.
	for _, col := range []string{"tier_active_min_pct", "tier_at_risk_min_pct"} {
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('seasons') WHERE name = ?`, col).Scan(&n); err != nil {
			t.Fatalf("pragma_table_info: %v", err)
		}
		if n != 1 {
			t.Errorf("seasons.%s was dropped — it is a participation threshold, not a reward tier count", col)
		}
	}
}

func TestMigration068AddsRewardTiersDefaultSetting(t *testing.T) {
	conn := migrateTo(t, preTierMigrationVersion)
	seedPreMigrationFixture(t, conn)

	if err := goose.Up(conn, "migrations"); err != nil {
		t.Fatalf("goose.Up: %v", err)
	}

	var raw string
	if err := conn.QueryRow(`SELECT season_reward_tiers_default FROM settings WHERE id = 1`).Scan(&raw); err != nil {
		t.Fatalf("read season_reward_tiers_default: %v", err)
	}
	if raw == "" {
		t.Fatal("season_reward_tiers_default is empty")
	}

	// The stored default must round-trip through the struct the seeding path uses.
	tiers := decodeRewardTiersJSON(t, raw)
	if len(tiers) != 4 {
		t.Fatalf("got %d default tiers, want 4", len(tiers))
	}
	if tiers[0].Key != "alliance_leader" || tiers[3].Key != "valued" {
		t.Errorf("unexpected default tier order: %+v", tiers)
	}
	for _, tr := range tiers {
		if !validTierColors[tr.Color] {
			t.Errorf("default tier %q has colour %q, which is not a valid palette slot", tr.Key, tr.Color)
		}
	}
}

func decodeRewardTiersJSON(t *testing.T, raw string) []SeasonRewardTier {
	t.Helper()
	var tiers []SeasonRewardTier
	if err := json.Unmarshal([]byte(raw), &tiers); err != nil {
		t.Fatalf("unmarshal reward tiers %q: %v", raw, err)
	}
	return tiers
}

func TestValidTierKey(t *testing.T) {
	valid := []string{"core", "alliance_leader", "vanguard", "tier_5", "a"}
	for _, k := range valid {
		if !validTierKey(k) {
			t.Errorf("validTierKey(%q) = false, want true", k)
		}
	}
	invalid := []string{"", "Core", "alliance leader", "5th", "_leading", "tier-five", "ünicode"}
	for _, k := range invalid {
		if validTierKey(k) {
			t.Errorf("validTierKey(%q) = true, want false", k)
		}
	}
}
