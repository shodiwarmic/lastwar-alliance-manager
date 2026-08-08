package main

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func setupNAPTestDB(t *testing.T) {
	t.Helper()
	prev := db
	t.Setenv("DATABASE_PATH", filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("STORAGE_PATH", t.TempDir())
	t.Setenv("SESSION_KEY", "test-session-key-at-least-32-chars-long")
	if err := initDB(); err != nil {
		t.Fatalf("initDB: %v", err)
	}
	t.Cleanup(func() {
		if db != nil {
			db.Close()
		}
		db = prev
	})
}

type extRow struct {
	power, kills            sql.NullInt64
	memberCount             sql.NullInt64
	seenAt, capturedAt      sql.NullString
	powerRank, killsRankVal sql.NullInt64
}

func readExtRow(t *testing.T, lastrankID string) extRow {
	t.Helper()
	var r extRow
	err := db.QueryRow(`SELECT power, kills, member_count, lastrank_seen_at, lastrank_captured_at, power_rank, kills_rank
		FROM external_alliances WHERE lastrank_id = ? COLLATE NOCASE`, lastrankID).
		Scan(&r.power, &r.kills, &r.memberCount, &r.seenAt, &r.capturedAt, &r.powerRank, &r.killsRankVal)
	if err != nil {
		t.Fatalf("read external_alliances: %v", err)
	}
	return r
}

func seedExtAlliance(t *testing.T, lastrankID string, power, kills int64, seenAt, capturedAt string, rank int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO external_alliances
		(tag, name, server, lastrank_id, power, kills, lastrank_seen_at, lastrank_captured_at, power_rank, kills_rank)
		VALUES ('TEST', 'Test Alliance', 1712, ?, ?, ?, ?, ?, ?, ?)`,
		lastrankID, power, kills, seenAt, capturedAt, rank, rank); err != nil {
		t.Fatalf("seed alliance: %v", err)
	}
}

// snap builds a detail snapshot for the seeded test alliance.
func snap(power, kills int64, members int, seen string) napDetailSnapshot {
	return napDetailSnapshot{
		LastRankID: "abc123", Server: 1712, Tag: "TEST", Name: "Test Alliance",
		Power: power, Kills: kills, MemberCount: members, Seen: seen,
	}
}

func countHistory(t *testing.T) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM alliance_stats_history`).Scan(&n); err != nil {
		t.Fatalf("count history: %v", err)
	}
	return n
}

// The gather already pays for the per-alliance detail request to get a member
// count; power and kills come back in the same response, so discarding them was
// pure waste.
func TestNAPSnapshotAppliesFreePowerAndKills(t *testing.T) {
	setupNAPTestDB(t)
	seedExtAlliance(t, "abc123", 100, 200, "2026-08-01T00:00:00Z", "2026-08-01 00:00:00", 5)

	applied, _, err := storeNAPAllianceSnapshot("", snap(999, 888, 87, "2026-08-06T00:00:00Z"))
	if err != nil {
		t.Fatalf("storeNAPAllianceSnapshot: %v", err)
	}
	if !applied {
		t.Fatal("stats were not applied despite a newer capture")
	}
	r := readExtRow(t, "abc123")
	if r.power.Int64 != 999 || r.kills.Int64 != 888 {
		t.Errorf("power/kills = %d/%d, want 999/888", r.power.Int64, r.kills.Int64)
	}
	if r.memberCount.Int64 != 87 {
		t.Errorf("member_count = %d, want 87", r.memberCount.Int64)
	}
}

// A stale detail response must not walk back fresher numbers — but it must still
// deliver its member count, which is the whole reason for the call.
func TestNAPSnapshotStaleDetailDoesNotWalkBackStats(t *testing.T) {
	setupNAPTestDB(t)
	seedExtAlliance(t, "abc123", 5000, 6000, "2026-08-06T00:00:00Z", "2026-08-06 00:00:00", 3)

	applied, _, err := storeNAPAllianceSnapshot("", snap(10, 20, 91, "2026-08-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("storeNAPAllianceSnapshot: %v", err)
	}
	if applied {
		t.Error("stale detail reported as applied")
	}
	r := readExtRow(t, "abc123")
	if r.power.Int64 != 5000 || r.kills.Int64 != 6000 {
		t.Errorf("stale response walked back power/kills to %d/%d", r.power.Int64, r.kills.Int64)
	}
	if r.memberCount.Int64 != 91 {
		t.Errorf("member_count = %d, want 91 — the count must land even when the stats are stale", r.memberCount.Int64)
	}
}

// Migration 058 is explicit: lastrank_captured_at is the LADDER clock and
// power_rank/kills_rank are positions WITHIN a ladder capture. A detail-endpoint
// write that touched them would let one per-alliance refresh permanently block
// ladder writes for that row, stranding its rank at NULL.
func TestNAPSnapshotLeavesLadderColumnsAlone(t *testing.T) {
	setupNAPTestDB(t)
	seedExtAlliance(t, "abc123", 100, 200, "2026-08-01T00:00:00Z", "2026-08-01 00:00:00", 7)

	if _, _, err := storeNAPAllianceSnapshot("", snap(999, 888, 50, "2026-08-06T00:00:00Z")); err != nil {
		t.Fatalf("storeNAPAllianceSnapshot: %v", err)
	}
	r := readExtRow(t, "abc123")
	// Compared as parsed times, never as strings: lastrank_captured_at is a
	// declared DATETIME, so the driver hands it back as RFC3339Nano even though
	// CURRENT_TIMESTAMP wrote the space form. A string compare here fails on
	// formatting while the instant is identical.
	got, ok := lastRankParseTime(r.capturedAt.String)
	want, _ := lastRankParseTime("2026-08-01 00:00:00")
	if !ok || !got.Equal(want) {
		t.Errorf("lastrank_captured_at = %q — the detail path must not touch the ladder clock", r.capturedAt.String)
	}
	if r.powerRank.Int64 != 7 || r.killsRankVal.Int64 != 7 {
		t.Errorf("ranks = %d/%d, want 7/7 — ranks belong to the ladder capture", r.powerRank.Int64, r.killsRankVal.Int64)
	}
}

// Our own alliance is deliberately absent from the registry (Rule 2), so there is
// no row to update. That must report "not applied" rather than claiming a write.
func TestNAPSnapshotReportsNoWriteForUnregisteredAlliance(t *testing.T) {
	setupNAPTestDB(t)
	d := snap(1, 2, 42, "2026-08-06T00:00:00Z")
	d.LastRankID = "not-in-registry"
	applied, _, err := storeNAPAllianceSnapshot("", d)
	// No registry row and not our own alliance: there is no valid subject key, and
	// INSERT OR IGNORE would have swallowed the CHECK violation silently.
	if err == nil {
		t.Fatal("expected an error rather than a silently-dropped datapoint")
	}
	if applied {
		t.Error("reported a write for an alliance with no registry row")
	}
}

// The rule: a datapoint identical to its predecessor carries no information. The
// series already says the value was that, and has been since — recording it again
// just inflates the history at whatever cadence the gather happens to run.
func TestDetailDatapointSkippedWhenUnchanged(t *testing.T) {
	setupNAPTestDB(t)
	seedExtAlliance(t, "abc123", 100, 200, "", "", 5)

	_, added, err := storeNAPAllianceSnapshot("", snap(500, 600, 80, "2026-08-01T00:00:00Z"))
	if err != nil || !added {
		t.Fatalf("first datapoint not recorded (added=%v, err=%v)", added, err)
	}
	if got := countHistory(t); got != 1 {
		t.Fatalf("history rows = %d, want 1", got)
	}

	// Same values, later capture → no new datapoint.
	_, added, err = storeNAPAllianceSnapshot("", snap(500, 600, 80, "2026-08-02T00:00:00Z"))
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if added {
		t.Error("recorded a duplicate datapoint identical to its predecessor")
	}
	if got := countHistory(t); got != 1 {
		t.Errorf("history rows = %d, want 1 — the series gained a redundant point", got)
	}

	// Any change at all → a new datapoint. Member count alone counts as a change.
	_, added, err = storeNAPAllianceSnapshot("", snap(500, 600, 81, "2026-08-03T00:00:00Z"))
	if err != nil {
		t.Fatalf("third call: %v", err)
	}
	if !added {
		t.Error("a changed member count should record a datapoint")
	}
	if got := countHistory(t); got != 2 {
		t.Errorf("history rows = %d, want 2", got)
	}
}

// Ranks are a position WITHIN a ladder capture; the detail endpoint has no ladder
// to rank against, so claiming one would be a fabricated number.
func TestDetailDatapointLeavesRanksNull(t *testing.T) {
	setupNAPTestDB(t)
	seedExtAlliance(t, "abc123", 100, 200, "", "", 5)
	if _, _, err := storeNAPAllianceSnapshot("", snap(1, 2, 3, "2026-08-01T00:00:00Z")); err != nil {
		t.Fatalf("storeNAPAllianceSnapshot: %v", err)
	}
	var pr, kr sql.NullInt64
	var source string
	if err := db.QueryRow(`SELECT power_rank, kills_rank, source FROM alliance_stats_history
		ORDER BY id DESC LIMIT 1`).Scan(&pr, &kr, &source); err != nil {
		t.Fatalf("read datapoint: %v", err)
	}
	if pr.Valid || kr.Valid {
		t.Error("detail-sourced datapoint claimed a ladder rank")
	}
	if source != "lastrank" {
		t.Errorf("source = %q, want lastrank", source)
	}
}
