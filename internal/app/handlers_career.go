package app

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

// historyExecer is a historyQuerier that can also write: *sql.Tx, or db.
type historyExecer interface {
	historyQuerier
	Exec(string, ...any) (sql.Result, error)
}

// recordHistoryIfChanged is the one change-only writer for the member history tables
// (private-docs 203): it appends a datapoint stamped with source unless the value equals
// the member's latest row, and reports whether it wrote. Callers validate the value
// first (memberStats holds each category's rule). table and valueCol are fixed code
// constants, never user input.
//
// at is when the value was read (private-docs 202), in sqliteTimeLayout, UTC; "" means
// now. With a time, the comparison is against the latest row at or before it, the row is
// inserted with that recorded_at, and rows dated after it are left alone. Times are
// compared in SQL — every history writer stores SQLite's layout — never as strings read
// back, which the driver returns as RFC 3339.
func recordHistoryIfChanged(q historyExecer, table, valueCol string, memberID int, value int64, source, at string) (bool, error) {
	var cur int64
	var err error
	if at == "" {
		err = q.QueryRow("SELECT "+valueCol+" FROM "+table+" WHERE member_id = ? ORDER BY recorded_at DESC LIMIT 1",
			memberID).Scan(&cur)
	} else {
		err = q.QueryRow("SELECT "+valueCol+" FROM "+table+" WHERE member_id = ? AND datetime(recorded_at) <= datetime(?)"+
			" ORDER BY datetime(recorded_at) DESC LIMIT 1", memberID, at).Scan(&cur)
	}
	switch {
	case err == nil && cur == value:
		return false, nil
	case err != nil && err != sql.ErrNoRows:
		return false, err
	}
	if at == "" {
		_, err = q.Exec("INSERT INTO "+table+" (member_id, "+valueCol+", source) VALUES (?, ?, ?)",
			memberID, value, source)
	} else {
		_, err = q.Exec("INSERT INTO "+table+" (member_id, "+valueCol+", source, recorded_at) VALUES (?, ?, ?, ?)",
			memberID, value, source, at)
	}
	if err != nil {
		return false, err
	}
	return true, nil
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
