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

func int64p(v int64) *int64 { return &v }

// The gather already pays for the per-alliance detail request to get a member
// count; power and kills come back in the same response, so discarding them was
// pure waste.
func TestNAPSnapshotAppliesFreePowerAndKills(t *testing.T) {
	setupNAPTestDB(t)
	seedExtAlliance(t, "abc123", 100, 200, "2026-08-01T00:00:00Z", "2026-08-01 00:00:00", 5)

	applied, err := storeNAPAllianceSnapshot("abc123", "", 87, int64p(999), int64p(888), "2026-08-06T00:00:00Z")
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

	applied, err := storeNAPAllianceSnapshot("abc123", "", 91, int64p(10), int64p(20), "2026-08-01T00:00:00Z")
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

	if _, err := storeNAPAllianceSnapshot("abc123", "", 50, int64p(999), int64p(888), "2026-08-06T00:00:00Z"); err != nil {
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
	applied, err := storeNAPAllianceSnapshot("not-in-registry", "", 42, int64p(1), int64p(2), "2026-08-06T00:00:00Z")
	if err != nil {
		t.Fatalf("storeNAPAllianceSnapshot: %v", err)
	}
	if applied {
		t.Error("reported a write for an alliance with no registry row")
	}
}
