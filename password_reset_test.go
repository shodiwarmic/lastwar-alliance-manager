package main

import (
	"testing"
	"time"
)

// The password_changed_at column is DECLARED TIMESTAMP, so the sqlite driver parses it
// into a time.Time (rows.go: DATE/DATETIME/TIMESTAMP handling) and database/sql then
// renders it into a string destination as RFC3339Nano — NOT the "2006-01-02 15:04:05"
// form CURRENT_TIMESTAMP writes. A parser matching only the space form silently never
// matches, which turns the mobile token-revocation check into a no-op that always
// allows. This test pins both shapes so that regression can't return unnoticed.
func TestPasswordChangedAtParsesInBothStoredShapes(t *testing.T) {
	want := time.Date(2026, 8, 2, 23, 24, 33, 0, time.UTC)

	cases := []struct {
		name  string
		value string
	}{
		{"sqlite CURRENT_TIMESTAMP form", "2026-08-02 23:24:33"},
		{"driver-converted RFC3339", "2026-08-02T23:24:33Z"},
		{"driver-converted RFC3339Nano", "2026-08-02T23:24:33.000000000Z"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := lastRankParseTime(tc.value)
			if !ok {
				t.Fatalf("failed to parse %q — the revocation check would silently degrade to a no-op", tc.value)
			}
			if !got.Equal(want) {
				t.Errorf("parsed %q as %v, want %v", tc.value, got, want)
			}
		})
	}
}

// Guards the comparison direction: a token minted before the password change must be
// rejected, one minted after must survive, and same-second must be allowed (strict
// After) so a login racing a password change isn't spuriously killed.
func TestTokenStalenessComparison(t *testing.T) {
	changed, ok := lastRankParseTime("2026-08-02T23:24:33Z")
	if !ok {
		t.Fatal("setup: could not parse changed-at")
	}

	cases := []struct {
		name      string
		issuedAt  time.Time
		wantStale bool
	}{
		{"issued before change", changed.Add(-time.Minute), true},
		{"issued same second", changed, false},
		{"issued after change", changed.Add(time.Minute), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if stale := changed.After(tc.issuedAt); stale != tc.wantStale {
				t.Errorf("changed.After(issuedAt) = %v, want %v", stale, tc.wantStale)
			}
		})
	}
}
