package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mkArchiveDir creates <root>/<date>/<reqID>/ and, when withSentinel, a meta.json
// inside it (the marker the janitor keys on).
func mkArchiveDir(t *testing.T, root, date, reqID string, withSentinel bool) string {
	t.Helper()
	dir := filepath.Join(root, date, reqID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if withSentinel {
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte("{}"), 0o640); err != nil {
			t.Fatalf("write sentinel: %v", err)
		}
	}
	return filepath.Join(root, date)
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestPruneLocalArchive(t *testing.T) {
	root := t.TempDir()
	today := time.Now().UTC().Format(archiveDateLayout)

	staleSentineled := mkArchiveDir(t, root, "2020-01-01", "aaaa", true) // old + ours -> delete
	freshSentineled := mkArchiveDir(t, root, today, "bbbb", true)        // recent + ours -> keep
	oldNoSentinel := mkArchiveDir(t, root, "2019-01-01", "cccc", false)  // old but NOT ours -> keep
	notesDir := filepath.Join(root, "notes")                             // non-date -> keep
	if err := os.MkdirAll(notesDir, 0o750); err != nil {
		t.Fatal(err)
	}

	pruneLocalArchive(root, 7)

	if exists(staleSentineled) {
		t.Errorf("stale sentineled dir should have been pruned: %s", staleSentineled)
	}
	if !exists(freshSentineled) {
		t.Errorf("fresh dir should remain: %s", freshSentineled)
	}
	if !exists(oldNoSentinel) {
		t.Errorf("old dir without meta.json sentinel must NOT be deleted: %s", oldNoSentinel)
	}
	if !exists(notesDir) {
		t.Errorf("non-date dir must NOT be deleted: %s", notesDir)
	}
}

func TestPruneLocalArchiveRefusesDangerousRoot(t *testing.T) {
	// Must return without attempting to prune "/", ".", or "".
	for _, dir := range []string{"/", ".", ""} {
		pruneLocalArchive(dir, 7) // should no-op (logged), never panic
	}
}

func TestPruneLocalArchiveKeepsBoundaryDate(t *testing.T) {
	root := t.TempDir()
	// A dir dated exactly within the retention window (yesterday) must be kept.
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format(archiveDateLayout)
	keep := mkArchiveDir(t, root, yesterday, "dddd", true)
	pruneLocalArchive(root, 7)
	if !exists(keep) {
		t.Errorf("dir within retention window must be kept: %s", keep)
	}
}

func TestGenerateRequestID(t *testing.T) {
	id := generateRequestID()
	if len(id) != 16 {
		t.Errorf("expected 16 hex chars, got %d (%q)", len(id), id)
	}
	if id == generateRequestID() {
		t.Errorf("request IDs should not collide")
	}
}
