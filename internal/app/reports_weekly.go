// reports_weekly.go — the weekly summary: VS leaders and underperformers for the
// evaluated week, power growth, and members by accountability tag. It was the
// Accountability page's Report tab and /accountability/report until Project 19.
//
// Part of the reporting layer (CLAUDE.md → Reporting layer).

package app

import (
	"sort"
	"time"
)

// ReportTopMember is one row of a weekly-summary list.
type ReportTopMember struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Rank  string `json:"rank"`
	Value int    `json:"value"`
}

// weeklySummaryParams: the report is the evaluated week by definition, so it takes no
// range; only the power-growth window (days, default 30).
type weeklySummaryParams struct {
	GrowthWindowDays int `json:"window,omitempty"`
}

type weeklySummaryReport struct {
	WeekDate          string            `json:"week_date"`
	GeneratedAt       string            `json:"generated_at"`
	VSLeaders         []ReportTopMember `json:"vs_leaders"`
	VSUnderperformers []ReportTopMember `json:"vs_underperformers"`
	PowerGrowth       []ReportTopMember `json:"power_growth"`
	// The growth window, and how many members had no reading that old (left out).
	GrowthWindowDays    int            `json:"growth_window_days"`
	PowerGrowthExcluded int            `json:"power_growth_excluded"`
	TagCounts           map[string]int `json:"tag_counts"`
	TotalStrikes        int            `json:"total_strikes"`
	VSMin               int            `json:"vs_min"`
	VSMinSource         string         `json:"vs_min_source"`
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
		       COALESCE(vp.thursday,0), COALESCE(vp.friday,0), COALESCE(vp.saturday,0),
		       vp.id IS NOT NULL AS has_row
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
		var hasRow bool
		if err := vsRows.Scan(&m.ID, &m.Name, &m.Rank, &joinedAt, &m.Value,
			&scores[0], &scores[1], &scores[2], &scores[3], &scores[4], &scores[5], &hasRow); err != nil {
			vsRows.Close()
			return nil, err
		}
		if n < 5 {
			rep.VSLeaders = append(rep.VSLeaders, m)
		}
		n++
		if importedCompleted > 0 && vsDaysBelow(scores, hasRow, joinedAt, weekDate, completed, dayImported, weekMin.Points) >= flagDays {
			rep.VSUnderperformers = append(rep.VSUnderperformers, m)
		}
	}
	vsRows.Close()
	if err := vsRows.Err(); err != nil {
		return nil, err
	}

	// Power growth over the window: the top five among members with a reading that old.
	window := p.GrowthWindowDays
	if window == 0 {
		window = growthWindowLongDefault
	}
	growth, err := powerGrowth(q, growthParams{Table: histPower, WindowDays: window, AsOf: now})
	if err != nil {
		return nil, err
	}
	rep.GrowthWindowDays = window
	var withBaseline []memberGrowth
	for _, g := range growth {
		if g.Growth == nil {
			rep.PowerGrowthExcluded++
			continue
		}
		withBaseline = append(withBaseline, g)
	}
	sort.SliceStable(withBaseline, func(i, j int) bool { return *withBaseline[i].Growth > *withBaseline[j].Growth })
	for i, g := range withBaseline {
		if i == 5 {
			break
		}
		rep.PowerGrowth = append(rep.PowerGrowth, ReportTopMember{ID: g.MemberID, Name: g.Name, Rank: g.Rank, Value: int(*g.Growth)})
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
