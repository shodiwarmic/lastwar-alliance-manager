// reports_weekly.go — the weekly summary: VS leaders and underperformers for the
// evaluated week, power growth, and members by accountability tag. It was the
// Accountability page's Report tab and /accountability/report until Project 19.
//
// Part of the reporting layer (CLAUDE.md → Reporting layer).

package app

import (
	"time"

	"lastwar-alliance/internal/gametime"
)

// ReportTopMember is one row of a weekly-summary list.
type ReportTopMember struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Rank  string `json:"rank"`
	Value int    `json:"value"`
}

// weeklySummaryParams: the report is the evaluated week by definition, so it takes no range.
type weeklySummaryParams struct{}

type weeklySummaryReport struct {
	WeekDate          string            `json:"week_date"`
	GeneratedAt       string            `json:"generated_at"`
	VSLeaders         []ReportTopMember `json:"vs_leaders"`
	VSUnderperformers []ReportTopMember `json:"vs_underperformers"`
	PowerGrowth       []ReportTopMember `json:"power_growth"`
	TagCounts         map[string]int    `json:"tag_counts"`
	TotalStrikes      int               `json:"total_strikes"`
	VSMin             int               `json:"vs_min"`
	VSMinSource       string            `json:"vs_min_source"`
	// Evaluated-week context for the notes (mirrors the dashboard page-config).
	Completed         int  `json:"completed"`
	ImportedCompleted int  `json:"imported_completed"`
	FallbackActive    bool `json:"fallback_active"`
}

// runWeeklySummaryReport builds the summary for the VS week evaluated at now (the current
// week, or last week on a Monday). Every read is on q; no cursor is held across another.
func runWeeklySummaryReport(q rowQueryer, p weeklySummaryParams, now time.Time) (*weeklySummaryReport, error) {
	_, flagDays := getVSAccountabilitySettingsQ(q)
	weekDate, completed, dayImported, importedCompleted, fallbackActive, err := vsEvalContextAt(q, now)
	if err != nil {
		return nil, err
	}
	thresholds := loadStrikeThresholdsQ(q)
	weekMin, err := vsMinimumForWeek(q, weekDate)
	if err != nil {
		return nil, err
	}

	rep := &weeklySummaryReport{
		WeekDate:          weekDate,
		GeneratedAt:       now.UTC().Format(time.RFC3339),
		VSLeaders:         []ReportTopMember{},
		VSUnderperformers: []ReportTopMember{},
		PowerGrowth:       []ReportTopMember{},
		TagCounts:         map[string]int{"At Risk": 0, "Needs Improvement": 0, "Reliable": 0},
		VSMin:             weekMin.Points,
		VSMinSource:       weekMin.Source,
		Completed:         completed,
		ImportedCompleted: importedCompleted,
		FallbackActive:    fallbackActive,
	}

	// VS leaders (by weekly total) and underperformers (the daily-minimum rule).
	vsRows, err := q.Query(`
		SELECT m.id, m.name, m.rank, COALESCE(m.joined_at, ''),
		       COALESCE(vp.monday+vp.tuesday+vp.wednesday+vp.thursday+vp.friday+vp.saturday, 0) AS vs_total,
		       COALESCE(vp.monday,0), COALESCE(vp.tuesday,0), COALESCE(vp.wednesday,0),
		       COALESCE(vp.thursday,0), COALESCE(vp.friday,0), COALESCE(vp.saturday,0)
		FROM members m
		LEFT JOIN vs_points vp ON vp.member_id = m.id AND vp.week_date = ?
		WHERE m.rank != 'EX'
		ORDER BY vs_total DESC
	`, weekDate)
	if err != nil {
		return nil, err
	}
	n := 0
	for vsRows.Next() {
		var m ReportTopMember
		var joinedAt string
		var scores [6]int
		if err := vsRows.Scan(&m.ID, &m.Name, &m.Rank, &joinedAt, &m.Value,
			&scores[0], &scores[1], &scores[2], &scores[3], &scores[4], &scores[5]); err != nil {
			vsRows.Close()
			return nil, err
		}
		if n < 5 {
			rep.VSLeaders = append(rep.VSLeaders, m)
		}
		n++
		if importedCompleted > 0 && vsDaysBelow(scores, joinedAt, weekDate, completed, dayImported, weekMin.Points) >= flagDays {
			rep.VSUnderperformers = append(rep.VSUnderperformers, m)
		}
	}
	vsRows.Close()
	if err := vsRows.Err(); err != nil {
		return nil, err
	}

	// Power growth: latest reading minus the latest reading 28 days before now.
	cutoff := now.UTC().AddDate(0, 0, -28).Format(gametime.SQLiteLayout)
	growthRows, err := q.Query(`
		SELECT m.id, m.name, m.rank,
		       COALESCE(latest.power, 0) - COALESCE(older.power, 0) AS growth
		FROM members m
		LEFT JOIN (
			SELECT member_id, power FROM power_history ph1
			WHERE recorded_at = (SELECT MAX(recorded_at) FROM power_history WHERE member_id = ph1.member_id)
		) latest ON latest.member_id = m.id
		LEFT JOIN (
			SELECT member_id, power FROM power_history ph2
			WHERE recorded_at = (
				SELECT MAX(recorded_at) FROM power_history
				WHERE member_id = ph2.member_id AND recorded_at <= ?
			)
		) older ON older.member_id = m.id
		WHERE m.rank != 'EX'
		ORDER BY growth DESC
		LIMIT 5
	`, cutoff)
	if err != nil {
		return nil, err
	}
	for growthRows.Next() {
		var m ReportTopMember
		if err := growthRows.Scan(&m.ID, &m.Name, &m.Rank, &m.Value); err != nil {
			growthRows.Close()
			return nil, err
		}
		rep.PowerGrowth = append(rep.PowerGrowth, m)
	}
	growthRows.Close()
	if err := growthRows.Err(); err != nil {
		return nil, err
	}

	// Members by tag, and active strikes.
	tagRows, err := q.Query(`
		SELECT COALESCE(SUM(CASE WHEN s.status='active' THEN 1 ELSE 0 END), 0) AS active_strikes
		FROM members m
		LEFT JOIN accountability_strikes s ON s.member_id = m.id
		WHERE m.rank != 'EX'
		GROUP BY m.id
	`)
	if err != nil {
		return nil, err
	}
	for tagRows.Next() {
		var strikes int
		if err := tagRows.Scan(&strikes); err != nil {
			tagRows.Close()
			return nil, err
		}
		rep.TagCounts[memberTag(strikes, thresholds)]++
		rep.TotalStrikes += strikes
	}
	tagRows.Close()
	return rep, tagRows.Err()
}
