package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// withCeiling shrinks the statement ceiling for one test and restores it after.
func withCeiling(t *testing.T, d time.Duration) {
	t.Helper()
	prev := dbAcquireCeiling
	dbAcquireCeiling = d
	t.Cleanup(func() { dbAcquireCeiling = prev })
}

// captureSlog redirects the default logger into a buffer for the duration of a test.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// TestSelfDeadlockFailsLoudly is the whole point of the guard: a query issued while a
// cursor is still open used to hang the process forever. It must now return an error,
// and the connection must be usable again once the cursor is closed.
func TestSelfDeadlockFailsLoudly(t *testing.T) {
	setupJobsTestDB(t)
	withCeiling(t, 200*time.Millisecond)
	logs := captureSlog(t)

	rows, err := db.Query(`SELECT id FROM users`)
	if err != nil {
		t.Fatalf("open cursor: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		var n int
		done <- db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected the blocked query to fail, got nil")
		}
	case <-time.After(2 * time.Second):
		rows.Close()
		t.Fatal("the blocked query hung instead of failing")
	}

	if got := logs.String(); !strings.Contains(got, "dbguard_test.go") {
		t.Errorf("log line does not name the call site:\n%s", got)
	}

	rows.Close()

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatalf("connection did not recover after the cursor closed: %v", err)
	}
}

// TestConcurrentQueriesWaitInsteadOfFailing pins the "timeout, not counter" decision:
// a second request legitimately waiting its turn on the single connection must
// succeed, not be rejected as a deadlock.
func TestConcurrentQueriesWaitInsteadOfFailing(t *testing.T) {
	setupJobsTestDB(t)
	withCeiling(t, 2*time.Second)

	rows, err := db.Query(`SELECT id FROM users`)
	if err != nil {
		t.Fatalf("open cursor: %v", err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		rows.Close()
	}()

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatalf("a query waiting its turn was failed: %v", err)
	}
}

// TestOpenCursorOutlivesTheCeiling proves the disarm: the ceiling bounds the call, not
// the lifetime of the rows it returns.
func TestOpenCursorOutlivesTheCeiling(t *testing.T) {
	setupJobsTestDB(t)
	withCeiling(t, 100*time.Millisecond)

	if _, err := db.Exec(`INSERT INTO members (name, rank) VALUES ('a','R1'),('b','R1'),('c','R1')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rows, err := db.Query(`SELECT name FROM members ORDER BY name`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	time.Sleep(300 * time.Millisecond)

	var seen int
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan after the ceiling elapsed: %v", err)
		}
		seen++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err after the ceiling elapsed: %v", err)
	}
	if seen != 3 {
		t.Fatalf("got %d rows, want 3", seen)
	}
}

// TestSlowStatementIsCutOff documents the statement-ceiling contract rather than
// leaving it implicit: a single statement that genuinely runs past the ceiling is
// cut off too, and the connection recovers.
func TestSlowStatementIsCutOff(t *testing.T) {
	setupJobsTestDB(t)
	withCeiling(t, 100*time.Millisecond)
	captureSlog(t)

	var n int
	err := db.QueryRow(`
		WITH RECURSIVE counter(x) AS (
			SELECT 1 UNION ALL SELECT x + 1 FROM counter WHERE x < 50000000
		)
		SELECT COUNT(*) FROM counter`).Scan(&n)
	if err == nil {
		t.Fatal("expected the slow statement to be cut off")
	}

	if _, err := db.Exec(`SELECT 1`); err != nil {
		t.Fatalf("connection did not recover after the ceiling fired: %v", err)
	}
}

// TestParentCancellationIsNotReportedAsCeiling — a caller's own cancelled context must
// pass through untouched, unlogged, and not wrapped as a ceiling breach.
func TestParentCancellationIsNotReportedAsCeiling(t *testing.T) {
	setupJobsTestDB(t)
	withCeiling(t, 10*time.Second)
	logs := captureSlog(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := db.ExecContext(ctx, `SELECT 1`)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if errors.Is(err, errDBAcquireTimeout) {
		t.Fatal("a parent cancellation was misreported as a ceiling breach")
	}
	if got := logs.String(); got != "" {
		t.Errorf("a parent cancellation was logged:\n%s", got)
	}
}
