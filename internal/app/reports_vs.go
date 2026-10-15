// reports_vs.go — the VS daily minimum per week, and the VS-misses report.
//
// Part of the reporting layer (CLAUDE.md → Reporting layer): every function takes its
// query interface and never reads the global db or the wall clock.

package app

import (
	"database/sql"
	"sort"
	"time"

	"lastwar-alliance/internal/gametime"
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

// --- The VS-misses report (249) ------------------------------------------------------

// vsMissesParams: a range, and optionally one member (0 = everyone).
type vsMissesParams struct {
	Range    reportRangeParams `json:"range"`
	MemberID int               `json:"member_id,omitempty"`
}

var vsWeekdayNames = [6]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// vsMissDay is one day a member scored below that week's minimum.
type vsMissDay struct {
	Date      string `json:"date"`
	Weekday   string `json:"weekday"`
	WeekDate  string `json:"week_date"`
	Points    int    `json:"points"`
	Minimum   int    `json:"minimum"`
	Shortfall int    `json:"shortfall"`
}

type vsMissesMember struct {
	MemberID      int         `json:"member_id"`
	Name          string      `json:"name"`
	Rank          string      `json:"rank"`
	DaysEvaluated int         `json:"days_evaluated"`
	Misses        int         `json:"misses"`
	AvgShortfall  int         `json:"avg_shortfall"`
	Worst         *vsMissDay  `json:"worst"`
	Days          []vsMissDay `json:"days"`
}

type vsMissesWeek struct {
	WeekDate      string        `json:"week_date"`
	Minimum       vsWeekMinimum `json:"minimum"`
	DaysImported  [6]bool       `json:"days_imported"`
	DaysEvaluated [6]bool       `json:"days_evaluated"` // in range, completed and imported
}

type vsMissesCoverage struct {
	MembersEvaluated   int      `json:"members_evaluated"`
	DaysEvaluated      int      `json:"days_evaluated"`
	DaysNotImported    []string `json:"days_not_imported"`    // in range, before today, no import
	WeeksWithoutRecord []string `json:"weeks_without_record"` // judged against today's setting
	MembersExcludedEX  int      `json:"members_excluded_ex"`  // former members with points in range
}

type vsMissesReport struct {
	From        string           `json:"from"`
	To          string           `json:"to"`
	GeneratedAt string           `json:"generated_at"`
	Weeks       []vsMissesWeek   `json:"weeks"`
	Members     []vsMissesMember `json:"members"`
	Coverage    vsMissesCoverage `json:"coverage"`
}

// runVSMissesReport lists every evaluated VS day in the range on which a member scored
// below the minimum that applied to that day's week. A day is evaluated iff it is in
// [from, to], before today's game date, imported (any member non-zero that day), on or
// after the member's join date, and the member is not EX (the app records no departure
// date, so a former member is not shown for any week). Members evaluated on at least
// one day are listed, those with no misses included, most misses first.
func runVSMissesReport(q rowQueryer, p vsMissesParams, now time.Time) (*vsMissesReport, error) {
	rng, err := resolveReportRange(q, p.Range, now)
	if err != nil {
		return nil, err
	}
	today := gametime.DateOf(now)
	rep := &vsMissesReport{
		From: rng.From, To: rng.To, GeneratedAt: now.UTC().Format(time.RFC3339),
		Weeks: []vsMissesWeek{}, Members: []vsMissesMember{},
		Coverage: vsMissesCoverage{DaysNotImported: []string{}, WeeksWithoutRecord: []string{}},
	}

	// The VS weeks the range touches, Monday to Monday.
	first, err := gametime.WeekMonday(rng.From)
	if err != nil {
		return nil, err
	}
	last, err := gametime.WeekMonday(rng.To)
	if err != nil {
		return nil, err
	}
	var weekDates []string
	for w := first; w <= last; w = dayDate(w, 7) {
		weekDates = append(weekDates, w)
	}
	mins, err := vsMinimumsForWeeks(q, weekDates)
	if err != nil {
		return nil, err
	}

	// Import masks for every week in one query (a per-week vsDayImportMask is N).
	masks := map[string][6]bool{}
	rows, err := q.Query(`SELECT week_date, MAX(monday), MAX(tuesday), MAX(wednesday), MAX(thursday), MAX(friday), MAX(saturday)
		FROM vs_points WHERE week_date BETWEEN ? AND ? GROUP BY week_date`, first, last)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var w string
		var m [6]int
		if err := rows.Scan(&w, &m[0], &m[1], &m[2], &m[3], &m[4], &m[5]); err != nil {
			rows.Close()
			return nil, err
		}
		var mask [6]bool
		for i := range m {
			mask[i] = m[i] > 0
		}
		masks[w] = mask
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Per week: which days count at all, whatever the member.
	type weekCtx struct {
		completed int
		inRange   [6]bool
	}
	ctx := map[string]weekCtx{}
	for _, w := range weekDates {
		var c weekCtx
		wk := vsMissesWeek{WeekDate: w, Minimum: mins[w], DaysImported: masks[w]}
		for i := 0; i < 6; i++ {
			d := dayDate(w, i)
			if d < today {
				c.completed = i + 1
			}
			c.inRange[i] = d >= rng.From && d <= rng.To
			if c.inRange[i] && d < today {
				if masks[w][i] {
					wk.DaysEvaluated[i] = true
					rep.Coverage.DaysEvaluated++
				} else {
					rep.Coverage.DaysNotImported = append(rep.Coverage.DaysNotImported, d)
				}
			}
		}
		ctx[w] = c
		rep.Weeks = append(rep.Weeks, wk)
		if mins[w].Source == vsMinSourceCurrent {
			rep.Coverage.WeeksWithoutRecord = append(rep.Coverage.WeeksWithoutRecord, w)
		}
	}

	// Members, then their points: two reads, each drained before the next.
	type member struct {
		id             int
		name, rank, jd string
	}
	memberQuery := `SELECT id, name, rank, COALESCE(joined_at, '') FROM members WHERE rank != 'EX'`
	args := []any{}
	if p.MemberID > 0 {
		memberQuery += ` AND id = ?`
		args = append(args, p.MemberID)
	}
	rows, err = q.Query(memberQuery+` ORDER BY name`, args...)
	if err != nil {
		return nil, err
	}
	var members []member
	for rows.Next() {
		var m member
		if err := rows.Scan(&m.id, &m.name, &m.rank, &m.jd); err != nil {
			rows.Close()
			return nil, err
		}
		members = append(members, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if p.MemberID > 0 && len(members) == 0 {
		return nil, badReportParam("no such active member")
	}

	points := map[int]map[string][6]int{}
	rows, err = q.Query(`SELECT member_id, week_date, monday, tuesday, wednesday, thursday, friday, saturday
		FROM vs_points WHERE week_date BETWEEN ? AND ?`, first, last)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int
		var w string
		var sc [6]int
		if err := rows.Scan(&id, &w, &sc[0], &sc[1], &sc[2], &sc[3], &sc[4], &sc[5]); err != nil {
			rows.Close()
			return nil, err
		}
		if points[id] == nil {
			points[id] = map[string][6]int{}
		}
		points[id][w] = sc
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := q.QueryRow(`SELECT COUNT(DISTINCT m.id) FROM members m JOIN vs_points vp ON vp.member_id = m.id
		WHERE m.rank = 'EX' AND vp.week_date BETWEEN ? AND ?`, first, last).Scan(&rep.Coverage.MembersExcludedEX); err != nil {
		return nil, err
	}

	for _, m := range members {
		out := vsMissesMember{MemberID: m.id, Name: m.name, Rank: m.rank, Days: []vsMissDay{}}
		total := 0
		for _, w := range weekDates {
			c := ctx[w]
			sc := points[m.id][w] // no row: every imported post-join day is a 0
			statuses := vsDayStatuses(sc, m.jd, w, c.completed, masks[w], mins[w].Points)
			for i, st := range statuses {
				if st == vsDaySkipped || !c.inRange[i] {
					continue
				}
				out.DaysEvaluated++
				if st == vsDayBelow {
					d := vsMissDay{Date: dayDate(w, i), Weekday: vsWeekdayNames[i], WeekDate: w,
						Points: sc[i], Minimum: mins[w].Points, Shortfall: mins[w].Points - sc[i]}
					out.Days = append(out.Days, d)
					total += d.Shortfall
					if out.Worst == nil || d.Shortfall > out.Worst.Shortfall {
						worst := d
						out.Worst = &worst
					}
				}
			}
		}
		if out.DaysEvaluated == 0 {
			continue
		}
		out.Misses = len(out.Days)
		if out.Misses > 0 {
			out.AvgShortfall = total / out.Misses
		}
		rep.Members = append(rep.Members, out)
	}
	sort.SliceStable(rep.Members, func(i, j int) bool {
		if rep.Members[i].Misses != rep.Members[j].Misses {
			return rep.Members[i].Misses > rep.Members[j].Misses
		}
		return rep.Members[i].Name < rep.Members[j].Name
	})
	rep.Coverage.MembersEvaluated = len(rep.Members)
	return rep, nil
}
