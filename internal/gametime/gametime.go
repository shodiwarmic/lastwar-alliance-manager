// Package gametime holds the game's clock and the date rules built on it, for every
// package that needs them: the app, and the demo generator (internal/demo), which must
// date its rows by exactly the rules the app reads them with. A leaf package — it imports
// nothing of ours — so both sides can depend on it without depending on each other.
package gametime

import "time"

// Loc is game time. The game day rolls over at a FIXED 02:00 UTC (10PM EDT / 9PM EST) —
// a fixed UTC−2 offset with NO daylight saving. Do NOT use a DST zone like
// America/New_York; the boundary is a constant UTC instant. VS weeks run Mon–Sat with
// week_date = the Monday.
var Loc = time.FixedZone("Game (UTC-2)", -2*3600)

// SQLiteLayout is the shape CURRENT_TIMESTAMP writes (UTC, space-separated). Never format
// a timestamp for a column with time.RFC3339.
const SQLiteLayout = "2006-01-02 15:04:05"

// DateLayout is a date-only column value.
const DateLayout = "2006-01-02"

// Now returns the current time in game time.
func Now() time.Time { return time.Now().In(Loc) }

// DateOf returns t's game date as "YYYY-MM-DD".
func DateOf(t time.Time) string { return t.In(Loc).Format(DateLayout) }

// MondayOf returns the Monday of the calendar week (Mon–Sun) containing date.
func MondayOf(date time.Time) time.Time {
	offset := int(time.Monday - date.Weekday())
	if offset > 0 {
		offset = -6
	}
	return date.AddDate(0, 0, offset)
}

// WeekMonday snaps a date-only week_date (YYYY-MM-DD) to the Monday of the calendar week
// (Mon–Sun) that contains it. It does NOT apply the game-time instant shift — the input is
// a calendar date with no time-of-day, so shifting it −2h would wrongly roll a Monday back
// to the previous week.
func WeekMonday(date string) (string, error) {
	t, err := time.Parse(DateLayout, date)
	if err != nil {
		return "", err
	}
	return MondayOf(t).Format(DateLayout), nil
}
