package main

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func setupQueueTestDB(t *testing.T) {
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

func reconcile(t *testing.T, proposals []pendingProposal, capture string) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := reconcilePendingChanges(tx, proposals, capture); err != nil {
		tx.Rollback()
		t.Fatalf("reconcile: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func queueRow(t *testing.T, kind, subjectKey string) (status, fingerprint string, ok bool) {
	t.Helper()
	err := db.QueryRow(`SELECT status, fingerprint FROM lastrank_pending_changes
		WHERE kind = ? AND subject_key = ?`, kind, subjectKey).Scan(&status, &fingerprint)
	if err == sql.ErrNoRows {
		return "", "", false
	}
	if err != nil {
		t.Fatalf("queueRow: %v", err)
	}
	return status, fingerprint, true
}

func seedQueueMember(t *testing.T, name, rank string) int {
	t.Helper()
	res, err := db.Exec(`INSERT INTO members (name, rank) VALUES (?, ?)`, name, rank)
	if err != nil {
		t.Fatalf("seed member: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

// The collision the subject_key exists to prevent. lastrankAllianceMember.PublicID
// is a non-pointer int, so a missing upstream public_id decodes to 0 — a naive
// (kind, member_id, public_id) key would put both of these on ('unmatched', 0, 0)
// and the second would clobber the first.
func TestQueueUnmatchedWithoutPublicIDsDoNotCollide(t *testing.T) {
	setupQueueTestDB(t)
	reconcile(t, []pendingProposal{
		{Kind: PendingKindUnmatched, LastRankName: "AlphaPlayer", ProposedValue: "AlphaPlayer"},
		{Kind: PendingKindUnmatched, LastRankName: "BravoPlayer", ProposedValue: "BravoPlayer"},
	}, "2026-08-01T00:00:00Z")

	items, err := loadPendingChanges(true)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d queue rows, want 2 — the second unmatched name clobbered the first", len(items))
	}
}

// A re-accented name must not mint a duplicate row: the no-public-id subject key
// folds, so "Pàcha" and "Pacha" are the same subject.
func TestQueueUnmatchedNameKeyIsAccentFolded(t *testing.T) {
	setupQueueTestDB(t)
	reconcile(t, []pendingProposal{{Kind: PendingKindUnmatched, LastRankName: "Pàcha", ProposedValue: "Pàcha"}}, "2026-08-01T00:00:00Z")
	reconcile(t, []pendingProposal{{Kind: PendingKindUnmatched, LastRankName: "Pacha", ProposedValue: "Pacha"}}, "2026-08-02T00:00:00Z")

	items, _ := loadPendingChanges(true)
	if len(items) != 1 {
		t.Fatalf("got %d rows, want 1 — an accent variant minted a duplicate subject", len(items))
	}
}

// "Defer once" means not now, not never: it re-opens when a genuinely newer pull
// arrives, but NOT merely because someone clicked Fetch again.
func TestQueueDeferOnceReopensOnlyOnNewerCapture(t *testing.T) {
	setupQueueTestDB(t)
	id := seedQueueMember(t, "Warmic", "R3")
	prop := []pendingProposal{{Kind: PendingKindRank, MemberID: id, CurrentValue: "R3", ProposedValue: "R4"}}
	key := lastRankSubjectKey(PendingKindRank, id, 0, "")

	reconcile(t, prop, "2026-08-01T00:00:00Z")
	if _, err := deferPendingChanges([]int{queueID(t, key)}, PendingDeferOnce, 1); err != nil {
		t.Fatalf("defer: %v", err)
	}

	// Same capture date — clicking Fetch again must not undo the deferral.
	reconcile(t, prop, "2026-08-01T00:00:00Z")
	if status, _, _ := queueRow(t, PendingKindRank, key); status != PendingDeferOnce {
		t.Errorf("status = %q after a same-capture refresh, want it to stay deferred", status)
	}

	// A newer pull re-opens it.
	reconcile(t, prop, "2026-08-02T00:00:00Z")
	if status, _, _ := queueRow(t, PendingKindRank, key); status != PendingOpen {
		t.Errorf("status = %q after a newer capture, want open", status)
	}
}

// "Defer until it changes" survives any number of newer pulls proposing the SAME
// thing, and re-opens the moment the proposal itself differs.
func TestQueueDeferUntilChangedReopensOnlyOnNewProposal(t *testing.T) {
	setupQueueTestDB(t)
	id := seedQueueMember(t, "Warmic", "R3")
	key := lastRankSubjectKey(PendingKindRank, id, 0, "")

	reconcile(t, []pendingProposal{{Kind: PendingKindRank, MemberID: id, CurrentValue: "R3", ProposedValue: "R4"}}, "2026-08-01T00:00:00Z")
	if _, err := deferPendingChanges([]int{queueID(t, key)}, PendingDeferUnchange, 1); err != nil {
		t.Fatalf("defer: %v", err)
	}

	// Newer pull, same proposal → still hidden.
	reconcile(t, []pendingProposal{{Kind: PendingKindRank, MemberID: id, CurrentValue: "R3", ProposedValue: "R4"}}, "2026-08-05T00:00:00Z")
	if status, _, _ := queueRow(t, PendingKindRank, key); status != PendingDeferUnchange {
		t.Errorf("status = %q, want it to stay deferred for an unchanged proposal", status)
	}

	// Different proposal → back in front of the officer.
	reconcile(t, []pendingProposal{{Kind: PendingKindRank, MemberID: id, CurrentValue: "R3", ProposedValue: "R5"}}, "2026-08-06T00:00:00Z")
	if status, _, _ := queueRow(t, PendingKindRank, key); status != PendingOpen {
		t.Errorf("status = %q, want open once the proposed value changed", status)
	}
}

// Upstream withdrawing a proposal must clear it: leaving it would ask an officer
// to decide something that is no longer true.
func TestQueueWithdrawnProposalIsDeleted(t *testing.T) {
	setupQueueTestDB(t)
	id := seedQueueMember(t, "Warmic", "R3")
	reconcile(t, []pendingProposal{{Kind: PendingKindRank, MemberID: id, ProposedValue: "R4"}}, "2026-08-01T00:00:00Z")
	reconcile(t, nil, "2026-08-02T00:00:00Z")

	if items, _ := loadPendingChanges(false); len(items) != 0 {
		t.Errorf("got %d rows, want 0 — a withdrawn proposal stayed queued", len(items))
	}
}

// Apply re-validates. A rank an officer already set by hand must resolve as
// superseded, not as a phantom success.
func TestQueueApplySupersededWhenRealityAlreadyMatches(t *testing.T) {
	setupQueueTestDB(t)
	id := seedQueueMember(t, "Warmic", "R3")
	key := lastRankSubjectKey(PendingKindRank, id, 0, "")
	reconcile(t, []pendingProposal{{Kind: PendingKindRank, MemberID: id, CurrentValue: "R3", ProposedValue: "R4"}}, "2026-08-01T00:00:00Z")

	// The officer gets there first.
	if _, err := db.Exec(`UPDATE members SET rank = 'R4' WHERE id = ?`, id); err != nil {
		t.Fatalf("manual rank: %v", err)
	}

	res, err := applyPendingChanges(LastRankReviewActionRequest{
		IDs: []int{queueID(t, key)}, Action: "apply",
	}, &AuthUser{ID: 1, Username: "tester"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res["applied"].(int) != 0 || res["superseded"].(int) != 1 {
		t.Errorf("applied=%v superseded=%v, want 0/1", res["applied"], res["superseded"])
	}
	if items, _ := loadPendingChanges(false); len(items) != 0 {
		t.Error("a superseded item stayed in the queue")
	}
}

// Applying for real changes the member and clears the row.
func TestQueueApplyRankChange(t *testing.T) {
	setupQueueTestDB(t)
	id := seedQueueMember(t, "Warmic", "R3")
	key := lastRankSubjectKey(PendingKindRank, id, 0, "")
	reconcile(t, []pendingProposal{{Kind: PendingKindRank, MemberID: id, CurrentValue: "R3", ProposedValue: "R5"}}, "2026-08-01T00:00:00Z")

	res, err := applyPendingChanges(LastRankReviewActionRequest{
		IDs: []int{queueID(t, key)}, Action: "apply",
	}, &AuthUser{ID: 1, Username: "tester"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res["applied"].(int) != 1 {
		t.Errorf("applied = %v, want 1", res["applied"])
	}
	var rank string
	db.QueryRow(`SELECT rank FROM members WHERE id = ?`, id).Scan(&rank)
	if rank != "R5" {
		t.Errorf("rank = %q, want R5", rank)
	}
}

// One resolve/member_id can't be spread across several unmatched names — that
// would silently alias different LastRank players to the same member.
func TestQueueBulkUnmatchedApplyIsRejected(t *testing.T) {
	setupQueueTestDB(t)
	reconcile(t, []pendingProposal{
		{Kind: PendingKindUnmatched, LastRankName: "AlphaPlayer", ProposedValue: "AlphaPlayer"},
		{Kind: PendingKindUnmatched, LastRankName: "BravoPlayer", ProposedValue: "BravoPlayer"},
	}, "2026-08-01T00:00:00Z")

	items, _ := loadPendingChanges(true)
	ids := []int{items[0].ID, items[1].ID}
	_, err := applyPendingChanges(LastRankReviewActionRequest{
		IDs: ids, Action: "apply", Resolve: "alias", MemberID: 1,
	}, &AuthUser{ID: 1, Username: "tester"})
	if err == nil {
		t.Fatal("bulk-applying two unmatched names was allowed")
	}
}

func queueID(t *testing.T, subjectKey string) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`SELECT id FROM lastrank_pending_changes WHERE subject_key = ?`, subjectKey).Scan(&id); err != nil {
		t.Fatalf("queueID(%s): %v", subjectKey, err)
	}
	return id
}

// Switching from "not now" to "not until it changes" is a real edit. An
// open-only guard would silently no-op it and report success.
func TestQueueDeferCanBeUpgradedBetweenKinds(t *testing.T) {
	setupQueueTestDB(t)
	id := seedQueueMember(t, "Warmic", "R3")
	key := lastRankSubjectKey(PendingKindRank, id, 0, "")
	reconcile(t, []pendingProposal{{Kind: PendingKindRank, MemberID: id, ProposedValue: "R4"}}, "2026-08-01T00:00:00Z")
	rowID := queueID(t, key)

	if n, err := deferPendingChanges([]int{rowID}, PendingDeferOnce, 1); err != nil || n != 1 {
		t.Fatalf("first defer: n=%d err=%v", n, err)
	}
	n, err := deferPendingChanges([]int{rowID}, PendingDeferUnchange, 1)
	if err != nil {
		t.Fatalf("upgrade defer: %v", err)
	}
	if n != 1 {
		t.Errorf("upgrading a deferral reported %d changed, want 1", n)
	}
	if status, _, _ := queueRow(t, PendingKindRank, key); status != PendingDeferUnchange {
		t.Errorf("status = %q, want %q", status, PendingDeferUnchange)
	}

	// Re-applying the same deferral changes nothing, and should say so.
	if n, _ := deferPendingChanges([]int{rowID}, PendingDeferUnchange, 1); n != 0 {
		t.Errorf("re-applying the same deferral reported %d changed, want 0", n)
	}
}
