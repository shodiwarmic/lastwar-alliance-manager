package main

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
)

// historyQuerier is satisfied by both *sql.DB and *sql.Tx, so the history
// helpers below work inside a transaction or against the pool.
type historyQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

// latestHistoryValue returns the most-recent value in a member history table.
// table and valueCol are fixed code constants (never user input); memberID is
// parameterised. ok=false means the member has no rows yet.
func latestHistoryValue(q historyQuerier, table, valueCol string, memberID int) (int, bool) {
	var v int
	if err := q.QueryRow(
		"SELECT "+valueCol+" FROM "+table+" WHERE member_id = ? ORDER BY recorded_at DESC LIMIT 1",
		memberID,
	).Scan(&v); err != nil {
		return 0, false
	}
	return v, true
}

// recordHistoryIfChanged appends a manual history datapoint when the submitted
// value differs from the latest stored one (dedup), skipping non-positive values.
// Mirrors the power/hero/kill change-detection in updateMember. source is the
// provenance to stamp ('manual' for UI edits, 'csv' for imports, etc.).
func recordHistoryIfChanged(q interface {
	historyQuerier
	Exec(string, ...any) (sql.Result, error)
}, table, valueCol string, memberID, value int, source string) bool {
	if value <= 0 {
		return false
	}
	if cur, ok := latestHistoryValue(q, table, valueCol, memberID); ok && cur == value {
		return false
	}
	_, err := q.Exec(
		"INSERT INTO "+table+" (member_id, "+valueCol+", source) VALUES (?, ?, ?)",
		memberID, value, source,
	)
	return err == nil
}

// getHQLevelHistory returns current HQ level + 7/30-day deltas for the Tracking
// page. Only members with at least one recorded datapoint appear. HQ moves
// slowly, so a nil delta (no baseline that far back) is expected.
func getHQLevelHistory(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT
			m.id, m.name, m.rank,
			COALESCE((SELECT hq_level FROM hq_level_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), 0) AS current_hq,
			COALESCE((SELECT hq_level FROM hq_level_history WHERE member_id = m.id AND recorded_at <= datetime('now', '-7 days')  ORDER BY recorded_at DESC LIMIT 1), 0) AS hq_7d,
			COALESCE((SELECT hq_level FROM hq_level_history WHERE member_id = m.id AND recorded_at <= datetime('now', '-30 days') ORDER BY recorded_at DESC LIMIT 1), 0) AS hq_30d,
			COALESCE((SELECT recorded_at FROM hq_level_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), '') AS last_recorded
		FROM members m
		WHERE m.rank != 'EX'
		  AND EXISTS (SELECT 1 FROM hq_level_history WHERE member_id = m.id)
		ORDER BY current_hq DESC, m.name
	`)
	if err != nil {
		slog.Error("Failed to query HQ level history", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	result := []HQLevelStat{}
	for rows.Next() {
		var s HQLevelStat
		var hq7d, hq30d int
		if err := rows.Scan(&s.MemberID, &s.MemberName, &s.MemberRank, &s.CurrentHQLevel, &hq7d, &hq30d, &s.LastRecordedAt); err != nil {
			slog.Error("Failed to scan HQ level history row", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if hq7d > 0 {
			d := s.CurrentHQLevel - hq7d
			s.Delta7d = &d
		}
		if hq30d > 0 {
			d := s.CurrentHQLevel - hq30d
			s.Delta30d = &d
		}
		result = append(result, s)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// getProfessionLevelHistory returns current profession level + 7/30-day deltas
// for the Tracking page. The Profession label comes from members.profession.
func getProfessionLevelHistory(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT
			m.id, m.name, m.rank, COALESCE(m.profession, ''),
			COALESCE((SELECT profession_level FROM profession_level_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), 0) AS current_pl,
			COALESCE((SELECT profession_level FROM profession_level_history WHERE member_id = m.id AND recorded_at <= datetime('now', '-7 days')  ORDER BY recorded_at DESC LIMIT 1), 0) AS pl_7d,
			COALESCE((SELECT profession_level FROM profession_level_history WHERE member_id = m.id AND recorded_at <= datetime('now', '-30 days') ORDER BY recorded_at DESC LIMIT 1), 0) AS pl_30d,
			COALESCE((SELECT recorded_at FROM profession_level_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), '') AS last_recorded
		FROM members m
		WHERE m.rank != 'EX'
		  AND EXISTS (SELECT 1 FROM profession_level_history WHERE member_id = m.id)
		ORDER BY current_pl DESC, m.name
	`)
	if err != nil {
		slog.Error("Failed to query profession level history", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	result := []ProfessionLevelStat{}
	for rows.Next() {
		var s ProfessionLevelStat
		var pl7d, pl30d int
		if err := rows.Scan(&s.MemberID, &s.MemberName, &s.MemberRank, &s.Profession, &s.CurrentProfessionLevel, &pl7d, &pl30d, &s.LastRecordedAt); err != nil {
			slog.Error("Failed to scan profession level history row", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if pl7d > 0 {
			d := s.CurrentProfessionLevel - pl7d
			s.Delta7d = &d
		}
		if pl30d > 0 {
			d := s.CurrentProfessionLevel - pl30d
			s.Delta30d = &d
		}
		result = append(result, s)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
