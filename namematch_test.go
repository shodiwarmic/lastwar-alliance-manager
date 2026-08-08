package main

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// setupNameMatchTestDB points the package-level db at a fresh migrated temp SQLite
// file and restores the previous handle on cleanup. Same shape as
// setupFileTagsTestDB.
func setupNameMatchTestDB(t *testing.T) {
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

func seedMember(t *testing.T, name, rank string) int {
	t.Helper()
	res, err := db.Exec(`INSERT INTO members (name, rank) VALUES (?, ?)`, name, rank)
	if err != nil {
		t.Fatalf("seed member %q: %v", name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return int(id)
}

func seedAlias(t *testing.T, memberID int, alias, category string, userID *int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO member_aliases (member_id, user_id, category, alias) VALUES (?, ?, ?, ?)`,
		memberID, userID, category, alias); err != nil {
		t.Fatalf("seed alias %q: %v", alias, err)
	}
}

func beginTx(t *testing.T) *sql.Tx {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { tx.Rollback() })
	return tx
}

func TestFoldName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Pàcha", "pacha"},
		{"PÀCHA", "pacha"},
		{"Pacha", "pacha"},
		{"  Pàcha  ", "pacha"},
		{"José", "jose"},
		{"Zoë", "zoe"},
		{"Ægis", "ægis"}, // Æ is a letter, not an accent — must survive
		{"", ""},
		{"   ", ""},
		{"ShodiWarmic", "shodiwarmic"},
		{"Ünïcödé Nâmé", "unicode name"},
	}
	for _, c := range cases {
		if got := foldName(c.in); got != c.want {
			t.Errorf("foldName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The headline fix: a roster name and an incoming name that differ only by an
// accent must resolve to each other. Before tier 3 this missed, and the member
// then appeared in BOTH "Unmatched names" and "Possibly left the alliance".
func TestResolveMemberAliasFoldedFallback(t *testing.T) {
	setupNameMatchTestDB(t)
	want := seedMember(t, "Pàcha", "R3")
	tx := beginTx(t)

	m, matchType, err := resolveMemberAlias(tx, "Pacha", 1)
	if err != nil {
		t.Fatalf("resolveMemberAlias: %v", err)
	}
	if m == nil || m.ID != want {
		t.Fatalf("got member %+v, want id %d", m, want)
	}
	if matchType != "folded" {
		t.Errorf("matchType = %q, want %q", matchType, "folded")
	}
}

// Folding must never displace an exact or alias hit — it is a fallback tier only.
func TestResolveMemberAliasExactAndAliasStillWin(t *testing.T) {
	setupNameMatchTestDB(t)
	exact := seedMember(t, "Pacha", "R3")
	aliased := seedMember(t, "Warmic", "R4")
	seedAlias(t, aliased, "Shodi", "global", nil)
	tx := beginTx(t)

	m, matchType, err := resolveMemberAlias(tx, "pacha", 1)
	if err != nil || m == nil || m.ID != exact {
		t.Fatalf("exact: got %+v (%v), want id %d", m, err, exact)
	}
	if matchType != "exact" {
		t.Errorf("exact matchType = %q, want %q", matchType, "exact")
	}

	m, matchType, err = resolveMemberAlias(tx, "shodi", 1)
	if err != nil || m == nil || m.ID != aliased {
		t.Fatalf("alias: got %+v (%v), want id %d", m, err, aliased)
	}
	if matchType != "global_alias" {
		t.Errorf("alias matchType = %q, want %q", matchType, "global_alias")
	}
}

// An accented alias must be reachable from its unaccented form too.
func TestResolveMemberAliasFoldedViaAlias(t *testing.T) {
	setupNameMatchTestDB(t)
	id := seedMember(t, "Warmic", "R4")
	seedAlias(t, id, "Shödi", "global", nil)
	tx := beginTx(t)

	m, matchType, err := resolveMemberAlias(tx, "Shodi", 1)
	if err != nil || m == nil || m.ID != id {
		t.Fatalf("got %+v (%v), want id %d", m, err, id)
	}
	if matchType != "folded" {
		t.Errorf("matchType = %q, want %q", matchType, "folded")
	}
}

// Two roster members folding to the same key must produce NO match. Guessing
// would silently attribute one player's stats to another; an unmatched row puts
// the decision in front of an officer instead.
func TestResolveMemberAliasFoldedAmbiguityRejected(t *testing.T) {
	setupNameMatchTestDB(t)
	seedMember(t, "Jösé", "R2")
	seedMember(t, "Jose", "R3")
	tx := beginTx(t)

	// "Jóse" misses tier 1 (SQLite LOWER is ASCII-only) and tier 2, then folds to
	// "jose" — which reaches two distinct members.
	m, matchType, err := resolveMemberAlias(tx, "Jóse", 1)
	if err == nil || m != nil {
		t.Fatalf("ambiguous fold resolved to %+v (err %v); want no match", m, err)
	}
	if matchType != "none" {
		t.Errorf("matchType = %q, want %q", matchType, "none")
	}
}

// A member reachable under several keys that all fold alike is ONE candidate, not
// several — otherwise their own alias would make them look ambiguous with
// themselves and the fallback would refuse to match anyone.
func TestFoldedIndexDedupesSameMember(t *testing.T) {
	setupNameMatchTestDB(t)
	id := seedMember(t, "Pàcha", "R3")
	seedAlias(t, id, "Pacha", "global", nil)
	seedAlias(t, id, "PÀCHA", "ocr", nil)
	tx := beginTx(t)

	idx, err := buildFoldedNameIndex(tx, 1)
	if err != nil {
		t.Fatalf("buildFoldedNameIndex: %v", err)
	}
	if got := len(idx.byFolded["pacha"]); got != 1 {
		t.Fatalf("byFolded[pacha] has %d candidates, want 1", got)
	}
	m, ok := idx.lookup("pacha")
	if !ok || m.ID != id {
		t.Fatalf("lookup = %+v (ok=%v), want id %d", m, ok, id)
	}
}

// Another user's personal alias must not leak into the index — same visibility
// rule as tier 2 in resolveMemberAlias.
func TestFoldedIndexRespectsAliasVisibility(t *testing.T) {
	setupNameMatchTestDB(t)
	id := seedMember(t, "Warmic", "R4")
	otherUser := 99
	seedAlias(t, id, "Sécret", "personal", &otherUser)
	tx := beginTx(t)

	idx, err := buildFoldedNameIndex(tx, 1) // user 1, not 99
	if err != nil {
		t.Fatalf("buildFoldedNameIndex: %v", err)
	}
	if _, ok := idx.lookup("secret"); ok {
		t.Error("another user's personal alias leaked into the folded index")
	}

	mineIdx, err := buildFoldedNameIndex(tx, otherUser)
	if err != nil {
		t.Fatalf("buildFoldedNameIndex: %v", err)
	}
	if _, ok := mineIdx.lookup("secret"); !ok {
		t.Error("own personal alias missing from the folded index")
	}
}

// Regression guard for the O(N²) trap: a caller-supplied index must be used
// as-is, never rebuilt per lookup. Proven by mutating the roster after the build
// — a passed index must not see the new member, while a nil index (which builds
// on demand) must.
func TestFoldedIndexPassedInIsNotRebuilt(t *testing.T) {
	setupNameMatchTestDB(t)
	seedMember(t, "Warmic", "R4")
	tx := beginTx(t)

	idx, err := buildFoldedNameIndex(tx, 1)
	if err != nil {
		t.Fatalf("buildFoldedNameIndex: %v", err)
	}

	// Added after the index was built. Accented so tiers 1 and 2 can't find it.
	if _, err := tx.Exec(`INSERT INTO members (name, rank) VALUES ('Zoë', 'R1')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if m, _, err := resolveMemberAliasWithIndex(tx, "Zoe", 1, idx); err == nil || m != nil {
		t.Error("prebuilt index saw a member added after it was built — it is being rebuilt per lookup")
	}
	if m, mt, err := resolveMemberAliasWithIndex(tx, "Zoe", 1, nil); err != nil || m == nil || mt != "folded" {
		t.Errorf("nil index should build on demand and match: got %+v (%s, %v)", m, mt, err)
	}
}
