package main

import (
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// writeJSON sets Content-Type and encodes v as JSON. Encoding errors are logged
// but not propagated (the response headers are already sent by that point).
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to write JSON response", "error", err)
	}
}

func min(nums ...int) int {
	if len(nums) == 0 {
		return 0
	}
	minNum := nums[0]
	for _, n := range nums[1:] {
		if n < minNum {
			minNum = n
		}
	}
	return minNum
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Helper function to parse date string
func parseDate(dateStr string) (time.Time, error) {
	return time.Parse("2006-01-02", dateStr)
}

// Helper function to format date to string
func formatDateString(t time.Time) string {
	return t.Format("2006-01-02")
}

// Helper function to get Monday of a week
func getMondayOfWeek(date time.Time) time.Time {
	offset := int(time.Monday - date.Weekday())
	if offset > 0 {
		offset = -6
	}
	return date.AddDate(0, 0, offset)
}

// --- Game-time clock (single source of truth) ---
//
// The game day rolls over at a FIXED 02:00 UTC (10PM EDT / 9PM EST) — i.e. a fixed
// UTC-2 offset with NO daylight saving. Do NOT use a DST zone like America/New_York;
// the boundary is a constant UTC instant. VS weeks run Mon–Sat with week_date = the Monday.
var gameLoc = time.FixedZone("Game (UTC-2)", -2*3600)

// gameNow returns the current time in game time (UTC-2).
func gameNow() time.Time { return time.Now().In(gameLoc) }

// gameDate returns the current date in game time as "YYYY-MM-DD".
func gameDate() string { return gameNow().Format("2006-01-02") }

// gameWeekdayAt returns the game-time weekday of t with Mon=0 … Sun=6.
func gameWeekdayAt(t time.Time) int { return (int(t.In(gameLoc).Weekday()) + 6) % 7 }

// gameWeekday returns the current game-time weekday with Mon=0 … Sun=6.
func gameWeekday() int { return gameWeekdayAt(time.Now()) }

// completedVSDays returns how many VS days (Mon–Sat) have fully finished in the
// current game-time week. Mon=0 … Sat=5, Sun=6 (days strictly before today).
func completedVSDays() int { return gameWeekday() }

// gameWeekStart returns the Monday (YYYY-MM-DD) of the VS week containing t,
// going back weeksBack full weeks. VS weeks run Mon–Sat.
func gameWeekStart(t time.Time, weeksBack int) string {
	gt := t.In(gameLoc)
	dow := (int(gt.Weekday()) + 6) % 7 // Mon=0 … Sun=6
	monday := gt.AddDate(0, 0, -dow-weeksBack*7)
	return monday.Format("2006-01-02")
}

// currentVSWeekMonday returns the Monday of the current game-time VS week.
func currentVSWeekMonday() string { return gameWeekStart(time.Now(), 0) }

// effectiveVSWeekAt returns the VS week to evaluate for the moment `now` and how
// many of its days are complete. On Monday game-time (0 completed days) it falls
// back to the previous, fully-finished week so flag/stat views show real data
// instead of an empty week.
func effectiveVSWeekAt(now time.Time) (weekDate string, completedDays int) {
	if c := gameWeekdayAt(now); c > 0 {
		return gameWeekStart(now, 0), c
	}
	return gameWeekStart(now, 1), 6
}

// effectiveVSWeek is effectiveVSWeekAt for the current moment.
func effectiveVSWeek() (weekDate string, completedDays int) {
	return effectiveVSWeekAt(time.Now())
}

// dayDate returns the date (YYYY-MM-DD) `add` days after a YYYY-MM-DD base.
// Parsing is UTC (no DST), so AddDate increments exactly one calendar day.
func dayDate(base string, add int) string {
	t, err := parseDate(base)
	if err != nil {
		return base
	}
	return formatDateString(t.AddDate(0, 0, add))
}

// normalizeToGameWeekMonday snaps a client-supplied date-only week_date (YYYY-MM-DD)
// to the Monday of the calendar week (Mon–Sun) that contains it, so CSV/mobile/save
// writers can't misfile week_date. It does NOT apply the game-time instant shift —
// the input is a calendar date with no time-of-day, so shifting it -2h would wrongly
// roll a Monday back to the previous week.
func normalizeToGameWeekMonday(dateStr string) (string, error) {
	t, err := parseDate(dateStr)
	if err != nil {
		return "", err
	}
	return formatDateString(getMondayOfWeek(t)), nil
}

// Generate random alphanumeric password
func generateRandomPassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	password := make([]byte, length)
	for i := range password {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		password[i] = charset[num.Int64()]
	}
	return string(password), nil
}

// Normalize name for matching (remove common prefixes, spaces, special chars)
func normalizeName(name string) string {
	name = strings.ToLower(name)
	// Remove common prefixes
	name = strings.TrimPrefix(name, "the ")
	name = strings.TrimPrefix(name, "a ")
	name = strings.TrimPrefix(name, "an ")
	// Remove spaces and special characters
	name = strings.ReplaceAll(name, " ", "")
	name = strings.ReplaceAll(name, "_", "")
	name = strings.ReplaceAll(name, "-", "")
	return name
}

// Calculate string similarity (0-100) using improved algorithm
func calculateSimilarity(s1, s2 string) int {
	// Normalize both strings
	n1 := normalizeName(s1)
	n2 := normalizeName(s2)

	// If normalized strings are identical, perfect match
	if n1 == n2 {
		return 100
	}

	// If one contains the other after normalization, very high score
	if strings.Contains(n1, n2) || strings.Contains(n2, n1) {
		return 90
	}

	// Calculate Levenshtein distance using existing function (assuming it's elsewhere or add it here if it's missing)
	distance := levenshteinDistance(n1, n2)
	maxLen := len(n1)
	if len(n2) > maxLen {
		maxLen = len(n2)
	}

	if maxLen == 0 {
		return 0
	}

	// Convert distance to similarity percentage
	similarity := ((maxLen - distance) * 100) / maxLen

	return similarity
}

// Calculate Levenshtein distance between two strings
func levenshteinDistance(s1, s2 string) int {
	s1Lower := strings.ToLower(s1)
	s2Lower := strings.ToLower(s2)
	len1 := len(s1Lower)
	len2 := len(s2Lower)

	if len1 == 0 {
		return len2
	}
	if len2 == 0 {
		return len1
	}

	matrix := make([][]int, len1+1)
	for i := range matrix {
		matrix[i] = make([]int, len2+1)
		matrix[i][0] = i
	}
	for j := 0; j <= len2; j++ {
		matrix[0][j] = j
	}

	for i := 1; i <= len1; i++ {
		for j := 1; j <= len2; j++ {
			cost := 0
			if s1Lower[i-1] != s2Lower[j-1] {
				cost = 1
			}
			matrix[i][j] = min(matrix[i-1][j]+1, matrix[i][j-1]+1, matrix[i-1][j-1]+cost)
		}
	}
	return matrix[len1][len2]
}

// Check if two names are similar (case-insensitive)
func areSimilar(name1, name2 string) bool {
	if strings.EqualFold(name1, name2) {
		return false // Exact match, not similar but same
	}

	// Calculate similarity (case-insensitive)
	lower1 := strings.ToLower(name1)
	lower2 := strings.ToLower(name2)
	dist := levenshteinDistance(lower1, lower2)
	maxLen := max(len(lower1), len(lower2))
	similarity := 1.0 - float64(dist)/float64(maxLen)

	// Consider similar if:
	// 1. Similarity >= 70%
	// 2. Distance <= 3 characters
	// 3. One name contains the other (for abbreviations like IRA vs IRAQ Army)
	if similarity >= 0.7 || dist <= 3 {
		return true
	}

	// Check if one name contains significant part of another
	name1Lower := strings.ToLower(name1)
	name2Lower := strings.ToLower(name2)
	if strings.Contains(name1Lower, name2Lower) || strings.Contains(name2Lower, name1Lower) {
		if len(name1) >= 3 && len(name2) >= 3 {
			return true
		}
	}

	return false
}
