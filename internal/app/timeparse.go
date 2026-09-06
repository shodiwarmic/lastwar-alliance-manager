// timeparse.go — timestamp parsing and conversion shared across the app.
//
// These moved out of lastrank_client.go with the LastRank extraction. They are named
// lastRank* for historical reasons but are app-wide: middleware.go, the password-reset
// flow and the NAP snapshot all use them. They stay on this side of the boundary
// because they translate between an upstream timestamp and the SQLite TEXT shape this
// application stores — a database concern the client package has no business knowing.

package app

import (
	"strings"
	"time"
)

// sqliteTimeLayout is the shape CURRENT_TIMESTAMP writes (UTC, space-separated).
// Never format a timestamp for a column with time.RFC3339.
const sqliteTimeLayout = "2006-01-02 15:04:05"

// lastRankParseTime parses an ISO-8601 timestamp from the API — or one read back out of
// SQLite. Both shapes matter: a column DECLARED TIMESTAMP/DATETIME/DATE is parsed by the
// driver and re-rendered into a string destination as RFC3339Nano, so a value written by
// CURRENT_TIMESTAMP as "2026-08-02 23:24:33" scans back as "2026-08-02T23:24:33Z".
// Parsing with a single layout silently never matches.
func lastRankParseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		sqliteTimeLayout,
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// lastRankCaptureToSQLite converts an API capture timestamp to the SQLite UTC
// string we store in *_history.recorded_at. ok=false means "couldn't parse —
// caller should fall back to CURRENT_TIMESTAMP".
func lastRankCaptureToSQLite(captureISO string) (string, bool) {
	t, ok := lastRankParseTime(captureISO)
	if !ok {
		return "", false
	}
	return t.Format(sqliteTimeLayout), true
}

// lastRankCaptureNewer reports whether LastRank's capture date is strictly newer
// than our latest stored recorded_at for a metric. Used for the per-metric stale
// skip. Conservative on ambiguity: an unparseable capture date is treated as not
// newer (skip); an empty/unparseable existing date means we have no fresher data
// (apply). Both inputs are compared as time, never as strings (the ISO 'T'/'Z'
// vs SQLite space forms would mis-sort lexically).
func lastRankCaptureNewer(captureISO, ourRecordedAt string) bool {
	capture, ok := lastRankParseTime(captureISO)
	if !ok {
		return false
	}
	ourRecordedAt = strings.TrimSpace(ourRecordedAt)
	if ourRecordedAt == "" {
		return true
	}
	ours, ok := lastRankParseTime(ourRecordedAt)
	if !ok {
		return true
	}
	return capture.After(ours)
}
