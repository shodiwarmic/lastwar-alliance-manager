// reports_vs.go — the VS daily minimum per week, and the VS-misses report.
//
// Part of the reporting layer (CLAUDE.md → Reporting layer): every function takes its
// query interface and never reads the global db or the wall clock.

package app

import (
	"database/sql"
)

// Where a week's VS daily minimum came from (vsWeekMinimum.Source).
const (
	vsMinSourceOverride = "override"        // an officer's value for that week (VS League week modal)
	vsMinSourceRecorded = "recorded"        // the setting as recorded for that week or an earlier one
	vsMinSourceCurrent  = "current_setting" // no record: judged against today's setting
)

// defaultVSMinimum is the settings column's default, used only when the row is unreadable.
const defaultVSMinimum = 2500000

// vsWeekMinimum is the minimum a week is judged against, and why.
type vsWeekMinimum struct {
	WeekDate string `json:"week_date"`
	Points   int    `json:"points"`
	Source   string `json:"source"`
}

// currentVSMinimumSetting reads today's setting. A minimum is always at least 1
// (Settings refuses less), so a stored 0 from before that rule falls back to the default.
func currentVSMinimumSetting(q rowQuerier) (int, error) {
	var v int
	if err := q.QueryRow(`SELECT COALESCE(vs_minimum_points, ?) FROM settings WHERE id = 1`, defaultVSMinimum).Scan(&v); err != nil {
		if err == sql.ErrNoRows {
			return defaultVSMinimum, nil
		}
		return 0, err
	}
	if v < 1 {
		v = defaultVSMinimum
	}
	return v, nil
}

// vsMinimumForWeek resolves week W's minimum: W's override; else the setting row with
// the greatest week_date <= W; else today's setting, labelled as such.
func vsMinimumForWeek(q rowQuerier, weekDate string) (vsWeekMinimum, error) {
	m := vsWeekMinimum{WeekDate: weekDate}
	err := q.QueryRow(`SELECT minimum_points FROM vs_week_minimums WHERE week_date = ? AND kind = 'override'`, weekDate).Scan(&m.Points)
	if err == nil {
		m.Source = vsMinSourceOverride
		return m, nil
	}
	if err != sql.ErrNoRows {
		return m, err
	}
	err = q.QueryRow(`SELECT minimum_points FROM vs_week_minimums WHERE kind = 'setting' AND week_date <= ?
		ORDER BY week_date DESC LIMIT 1`, weekDate).Scan(&m.Points)
	if err == nil {
		m.Source = vsMinSourceRecorded
		return m, nil
	}
	if err != sql.ErrNoRows {
		return m, err
	}
	if m.Points, err = currentVSMinimumSetting(q); err != nil {
		return m, err
	}
	m.Source = vsMinSourceCurrent
	return m, nil
}

// vsMinimumsForWeeks resolves many weeks at once: the table is one row per week at most
// per kind, so it is read whole and resolved in Go rather than queried per week.
func vsMinimumsForWeeks(q rowQueryer, weekDates []string) (map[string]vsWeekMinimum, error) {
	current, err := currentVSMinimumSetting(q)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT week_date, kind, minimum_points FROM vs_week_minimums ORDER BY week_date`)
	if err != nil {
		return nil, err
	}
	overrides := map[string]int{}
	type settingRow struct {
		week   string
		points int
	}
	var settings []settingRow // ascending by week
	for rows.Next() {
		var week, kind string
		var pts int
		if err := rows.Scan(&week, &kind, &pts); err != nil {
			rows.Close()
			return nil, err
		}
		if kind == vsMinSourceOverride {
			overrides[week] = pts
		} else {
			settings = append(settings, settingRow{week, pts})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make(map[string]vsWeekMinimum, len(weekDates))
	for _, w := range weekDates {
		m := vsWeekMinimum{WeekDate: w, Points: current, Source: vsMinSourceCurrent}
		if pts, ok := overrides[w]; ok {
			m.Points, m.Source = pts, vsMinSourceOverride
		} else {
			for i := len(settings) - 1; i >= 0; i-- {
				if settings[i].week <= w {
					m.Points, m.Source = settings[i].points, vsMinSourceRecorded
					break
				}
			}
		}
		out[w] = m
	}
	return out, nil
}

// nullUserID stores 0 (no acting user) as NULL.
func nullUserID(userID int) any {
	if userID <= 0 {
		return nil
	}
	return userID
}

// recordVSMinimumSetting records the setting as of weekDate, replacing that week's row.
// The Settings save calls it for the current game week, in its own transaction.
func recordVSMinimumSetting(q historyExecer, weekDate string, points, userID int) error {
	_, err := q.Exec(`INSERT INTO vs_week_minimums (week_date, kind, minimum_points, set_by, set_at)
		VALUES (?, 'setting', ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(week_date, kind) DO UPDATE SET minimum_points = excluded.minimum_points,
			set_by = excluded.set_by, set_at = CURRENT_TIMESTAMP`, weekDate, points, nullUserID(userID))
	return err
}

// setVSWeekOverride sets weekDate's override, or deletes it when points is nil or 0.
func setVSWeekOverride(q historyExecer, weekDate string, points *int, userID int) error {
	if points == nil || *points == 0 {
		_, err := q.Exec(`DELETE FROM vs_week_minimums WHERE week_date = ? AND kind = 'override'`, weekDate)
		return err
	}
	_, err := q.Exec(`INSERT INTO vs_week_minimums (week_date, kind, minimum_points, set_by, set_at)
		VALUES (?, 'override', ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(week_date, kind) DO UPDATE SET minimum_points = excluded.minimum_points,
			set_by = excluded.set_by, set_at = CURRENT_TIMESTAMP`, weekDate, *points, nullUserID(userID))
	return err
}
