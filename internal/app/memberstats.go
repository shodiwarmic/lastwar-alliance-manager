// memberstats.go - the member-stat categories a scan can record, and their rules.

package app

import (
	"fmt"
	"strconv"
	"time"
)

// memberStat is one per-member history a scan writes: its table and value column, and
// the smallest value that is a reading rather than a misread.
type memberStat struct {
	Table  string
	Column string
	Min    int64
	// NoDecrease refuses a reading below the member's latest: the game can't lower it,
	// so a lower reading is a misread (HQ level).
	NoDecrease bool
}

// memberStats maps a commit category to the history it writes. Every write goes through
// recordHistoryIfChanged, so an unchanged reading adds no row.
var memberStats = map[string]memberStat{
	"power":            {Table: "power_history", Column: "power", Min: 1},
	"kills":            {Table: "kill_history", Column: "kills", Min: 0},
	"hero_power":       {Table: "hero_power_history", Column: "power", Min: 1},
	"squad_power":      {Table: "squad_power_history", Column: "power", Min: 1},
	"hq_level":         {Table: "hq_level_history", Column: "hq_level", Min: 1, NoDecrease: true},
	"profession_level": {Table: "profession_level_history", Column: "profession_level", Min: 1},
}

// checkMemberStat validates a reading against its category's rule, worded for errors[].
func checkMemberStat(category string, value int64) error {
	st, ok := memberStats[category]
	if !ok {
		return fmt.Errorf("unknown member-stat category %q", category)
	}
	if value < st.Min {
		return fmt.Errorf("%s %d is below %s", category, value, strconv.FormatInt(st.Min, 10))
	}
	return nil
}

// capturedAtMaxAhead and capturedAtMaxBack bound a client's captured_at: a reading
// dated further out is far more likely a phone clock set wrong than a real capture.
const (
	capturedAtMaxAhead = 10 * time.Minute
	capturedAtMaxBack  = 30 * 24 * time.Hour
)

// parseCapturedAt turns a client's RFC 3339 captured_at into the recorded_at to store
// (UTC, sqliteTimeLayout). "" means "now" and returns "". The error is worded for errors[].
func parseCapturedAt(raw string, now time.Time) (string, error) {
	if raw == "" {
		return "", nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return "", fmt.Errorf("captured_at %q is not an RFC 3339 time", raw)
	}
	switch {
	case t.After(now.Add(capturedAtMaxAhead)):
		return "", fmt.Errorf("captured_at %s is in the future — check the phone's clock", raw)
	case t.Before(now.Add(-capturedAtMaxBack)):
		return "", fmt.Errorf("captured_at %s is more than 30 days ago — check the phone's clock", raw)
	}
	return t.UTC().Format(sqliteTimeLayout), nil
}
