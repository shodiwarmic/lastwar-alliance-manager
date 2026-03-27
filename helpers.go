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
