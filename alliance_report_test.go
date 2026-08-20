package main

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func setupReportTestDB(t *testing.T) {
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

func reportResp(lastrankID, tag string) *lastrankAllianceResp {
	return &lastrankAllianceResp{
		AllianceID: lastrankID,
		Abbr:       tag,
		Name:       "Test Alliance",
		ServerID:   1712,
		Fightpower: 900_000_000,
		ArmyKill:   40_000_000,
		CurMember:  87,
		LastSeenAt: "2026-08-17T04:00:00Z",
	}
}

func reportUser() *AuthUser {
	return &AuthUser{ID: 1, Username: "tester"}
}

func countAllianceHistory(t *testing.T) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM alliance_stats_history`).Scan(&n); err != nil {
		t.Fatalf("count history: %v", err)
	}
	return n
}

// A report on an alliance we don't track must save NOTHING and say so. Growing the
// registry as a side effect of looking someone up would fill it with every opponent an
// officer ever glanced at — and appendDetailDatapoint errors outright without a row to
// key the datapoint on, so an ungated call would also surface a spurious failure.
func TestReportSkipsAllianceNotInRegistry(t *testing.T) {
	setupReportTestDB(t)

	res := saveReportAllianceStats(reportUser(), reportResp("deadbeef00000000000000000000cafe", "OPP"))

	if res.InRegistry {
		t.Error("expected in_registry=false for an untracked alliance")
	}
	if res.StatsApplied || res.HistoryAdded {
		t.Error("expected nothing to be written for an untracked alliance")
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM external_alliances`).Scan(&rows); err != nil {
		t.Fatalf("count registry: %v", err)
	}
	if rows != 0 {
		t.Errorf("report minted %d registry row(s); it must never create one", rows)
	}
	if n := countAllianceHistory(t); n != 0 {
		t.Errorf("expected no history rows, got %d", n)
	}
}

// The happy path: a tracked alliance gets its current numbers refreshed and one datapoint
// appended, stamped with the upstream capture date.
func TestReportSavesStatsForTrackedAlliance(t *testing.T) {
	setupReportTestDB(t)
	if _, err := db.Exec(`INSERT INTO external_alliances (tag, name, server, lastrank_id)
		VALUES ('OPP', 'Test Alliance', 1712, 'aaaa1111bbbb2222cccc3333dddd4444')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res := saveReportAllianceStats(reportUser(), reportResp("aaaa1111bbbb2222cccc3333dddd4444", "OPP"))

	if !res.InRegistry || res.ExternalAllianceID == nil {
		t.Fatal("expected the alliance to resolve to its registry row")
	}
	if !res.StatsApplied {
		t.Error("expected the registry row's current stats to be refreshed")
	}
	if !res.HistoryAdded {
		t.Error("expected one history datapoint to be appended")
	}

	var power, kills, members sql.NullInt64
	var recordedAt, source string
	if err := db.QueryRow(`SELECT power, kills, member_count, recorded_at, source
		FROM alliance_stats_history ORDER BY id DESC LIMIT 1`).
		Scan(&power, &kills, &members, &recordedAt, &source); err != nil {
		t.Fatalf("read history: %v", err)
	}
	if power.Int64 != 900_000_000 || kills.Int64 != 40_000_000 || members.Int64 != 87 {
		t.Errorf("datapoint carried wrong values: power=%d kills=%d members=%d",
			power.Int64, kills.Int64, members.Int64)
	}
	// recorded_at must be the LastRank capture date, never the sync time — that is what
	// makes "stale never wins" fall out of the ordinary latest-by-recorded_at read.
	//
	// Compared as parsed time, never as a string: recorded_at is a DECLARED TIMESTAMP, so
	// the driver renders it back as RFC3339 ("…T04:00:00Z") no matter that it was written
	// in SQLite's space form. Asserting on the literal text would fail against correct code.
	got, ok := lastRankParseTime(recordedAt)
	if !ok {
		t.Fatalf("recorded_at %q did not parse", recordedAt)
	}
	want, _ := lastRankParseTime("2026-08-17T04:00:00Z")
	if !got.Equal(want) {
		t.Errorf("recorded_at = %v, want the upstream capture date %v", got, want)
	}
	if source != "lastrank" {
		t.Errorf("source = %q, want lastrank", source)
	}
}

// Running the same report twice with no upstream change must not inflate the series: a
// point identical to its predecessor carries no information.
func TestReportDoesNotDuplicateUnchangedDatapoint(t *testing.T) {
	setupReportTestDB(t)
	if _, err := db.Exec(`INSERT INTO external_alliances (tag, name, server, lastrank_id)
		VALUES ('OPP', 'Test Alliance', 1712, 'aaaa1111bbbb2222cccc3333dddd4444')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	resp := reportResp("aaaa1111bbbb2222cccc3333dddd4444", "OPP")

	saveReportAllianceStats(reportUser(), resp)
	first := countAllianceHistory(t)
	saveReportAllianceStats(reportUser(), resp)

	if got := countAllianceHistory(t); got != first {
		t.Errorf("second identical report added a datapoint: %d → %d", first, got)
	}
}

// A registry row added by hand carries no lastrank_id. storeNAPAllianceSnapshot resolves
// the row by lastrank_id ALONE, so the tag-matched fallback has to backfill it BEFORE the
// write — otherwise the officer is told "saved" and nothing lands.
func TestReportBackfillsLastRankIDOnTagMatch(t *testing.T) {
	setupReportTestDB(t)
	if _, err := db.Exec(`INSERT INTO external_alliances (tag, name, server)
		VALUES ('OPP', 'Test Alliance', 1712)`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res := saveReportAllianceStats(reportUser(), reportResp("aaaa1111bbbb2222cccc3333dddd4444", "OPP"))

	if !res.InRegistry {
		t.Fatal("expected the tag fallback to find the hand-added row")
	}
	var stored sql.NullString
	if err := db.QueryRow(`SELECT lastrank_id FROM external_alliances WHERE tag = 'OPP'`).Scan(&stored); err != nil {
		t.Fatalf("read registry: %v", err)
	}
	if stored.String != "aaaa1111bbbb2222cccc3333dddd4444" {
		t.Errorf("lastrank_id = %q, want it backfilled", stored.String)
	}
	if !res.StatsApplied {
		t.Error("expected stats to be applied after the backfill — a backfill done after " +
			"the write would silently miss the row")
	}
}

// Tags are reusable. A registry row whose tag matches but whose lastrank_id names a
// DIFFERENT alliance must not be retargeted — that would silently overwrite an unrelated
// alliance's stats with this one's.
func TestReportDoesNotRetargetRowWithDifferentLastRankID(t *testing.T) {
	setupReportTestDB(t)
	if _, err := db.Exec(`INSERT INTO external_alliances (tag, name, server, lastrank_id)
		VALUES ('OPP', 'Somebody Else', 1712, '99999999999999999999999999999999')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res := saveReportAllianceStats(reportUser(), reportResp("aaaa1111bbbb2222cccc3333dddd4444", "OPP"))

	if res.InRegistry {
		t.Error("expected a same-tag/different-id row to be treated as untracked")
	}
	var stored string
	if err := db.QueryRow(`SELECT lastrank_id FROM external_alliances WHERE tag = 'OPP'`).Scan(&stored); err != nil {
		t.Fatalf("read registry: %v", err)
	}
	if stored != "99999999999999999999999999999999" {
		t.Errorf("lastrank_id was overwritten to %q — an unrelated alliance was retargeted", stored)
	}
}

// Reporting on OUR OWN alliance is legitimate (an officer can paste our link). Rule 2 says
// it must never appear in external_alliances; its stats belong to the is_own series.
func TestReportOnOwnAllianceUsesIsOwnSeriesAndNoRegistryRow(t *testing.T) {
	setupReportTestDB(t)
	if _, err := db.Exec(`UPDATE settings SET lastrank_alliance_id = 'aaaa1111bbbb2222cccc3333dddd4444',
		alliance_tag = 'US' WHERE id = 1`); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	res := saveReportAllianceStats(reportUser(), reportResp("aaaa1111bbbb2222cccc3333dddd4444", "US"))

	if !res.IsOwn {
		t.Fatal("expected our own alliance to be recognised")
	}
	if res.InRegistry {
		t.Error("our own alliance must never resolve to a registry row (Rule 2)")
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM external_alliances`).Scan(&rows); err != nil {
		t.Fatalf("count registry: %v", err)
	}
	if rows != 0 {
		t.Errorf("report created %d registry row(s) for our own alliance", rows)
	}
	var isOwn int
	if err := db.QueryRow(`SELECT is_own FROM alliance_stats_history ORDER BY id DESC LIMIT 1`).Scan(&isOwn); err != nil {
		t.Fatalf("read history: %v", err)
	}
	if isOwn != 1 {
		t.Error("expected the datapoint to land in the is_own series")
	}
}

// The scout picker searches EVERY server: VS Duel League opponents are cross-server, so a
// search scoped to our own server would hide the alliances an officer is trying to find.
// The hit mapper is the safety layer over a volunteer service's response shape.
func TestSearchHitMappingDropsNonAlliancesAndIdlessRows(t *testing.T) {
	str := func(s string) *string { return &s }
	num := func(n int) *int { return &n }

	hits := []lastrankSearchHit{
		{Kind: "alliance", ID: "aaaa", Abbr: str("cROw"), Name: str("Black Crow Legion"), ServerID: num(1713)},
		{Kind: "player", ID: "9999", Name: str("SomePlayer")},         // wrong kind
		{Kind: "alliance", ID: "   ", Abbr: str("BAD")},               // no usable id
		{Kind: "alliance", ID: "bbbb", Abbr: str("CROW"), ServerID: num(915)},
	}

	got := mapLastRankSearchHits(hits, 20)

	if len(got) != 2 {
		t.Fatalf("got %d results, want 2 (the player hit and the id-less row must be dropped)", len(got))
	}
	if got[0].LastRankID != "aaaa" || got[1].LastRankID != "bbbb" {
		t.Errorf("unexpected ids: %q, %q", got[0].LastRankID, got[1].LastRankID)
	}
	// Server is the ONLY thing distinguishing same-tag alliances across servers, so it has
	// to survive the mapping.
	if got[0].Server == nil || *got[0].Server != 1713 {
		t.Error("server_id was dropped — same-tag alliances would be indistinguishable")
	}
	// /v1/search carries no power; that must read as "unknown", not as zero.
	if got[0].Power != nil {
		t.Error("power should stay nil rather than collapsing to 0")
	}
}

func TestSearchHitMappingRespectsLimit(t *testing.T) {
	hits := make([]lastrankSearchHit, 30)
	for i := range hits {
		hits[i] = lastrankSearchHit{Kind: "alliance", ID: string(rune('a' + i%26))}
	}
	if got := mapLastRankSearchHits(hits, 5); len(got) != 5 {
		t.Errorf("limit ignored: got %d, want 5", len(got))
	}
}

// The gather job used to name a permission key that is not a field on RankPermissions, so
// userHasPermission resolved it to false for every non-admin while the button rendered for
// both manage ranks. The Allow predicate ties the job gate to the same test the handler
// and template use.
func TestExtGatherJobGateMatchesTheHandlerGate(t *testing.T) {
	setupReportTestDB(t)

	def, ok := jobRegistry[JobExtAllianceGather]
	if !ok {
		t.Fatal("ext_alliance_gather is not registered")
	}
	if def.Allow == nil {
		t.Fatal("ext_alliance_gather must gate via Allow — a single permission key " +
			"cannot express manage_allies OR manage_vs_points")
	}

	// A VS officer: manage_vs_points only, no manage_allies. This is precisely the rank
	// the old string gate locked out.
	if _, err := db.Exec(`INSERT INTO rank_permissions (rank, permissions) VALUES
		('R4', '{"manage_vs_points":true}'),
		('R1', '{}')
		ON CONFLICT(rank) DO UPDATE SET permissions = excluded.permissions`); err != nil {
		t.Fatalf("seed rank permissions: %v", err)
	}
	memberID := 1
	vsOnly := &AuthUser{ID: 2, Username: "vsofficer", MemberID: &memberID, Rank: "R4"}

	if !def.Allow(vsOnly) {
		t.Error("a manage_vs_points officer can write the registry over HTTP but cannot " +
			"start the gather — the gates have drifted apart again")
	}
	if !userHasPermission(vsOnly, "manage_vs_points") {
		t.Fatal("test setup failed: the permission grant did not take")
	}

	noRights := &AuthUser{ID: 3, Username: "grunt", MemberID: &memberID, Rank: "R1"}
	if def.Allow(noRights) {
		t.Error("a rank with neither manage permission must not start the gather")
	}
}
