// reports_donations.go — the Donations report (Project 20): every member's donation weeks
// in a range of Monday–Sunday weeks, each judged against the minimums that applied to that
// week. The derivation and the judgement are donations.go's, so the report agrees with the
// Rankings tab, the profile and Accountability.

package app

import (
	"sort"
	"time"
)

type donationsParams struct {
	Range    reportRangeParams `json:"range"`
	MemberID int               `json:"member_id,omitempty"`
}

type donationsReportWeek struct {
	WeekDate         string           `json:"week_date"`
	Minimums         donationMinimums `json:"minimums"`
	Over             bool             `json:"over"`
	MembersRead      int              `json:"members_read"`
	MembersReadToEnd int              `json:"members_read_to_end"`
}

type donationsMemberWeek struct {
	WeekDate string            `json:"week_date"`
	Total    donationWeekTotal `json:"total"`
	Days     [7]donationDay    `json:"days"`
	donationJudgement
}

type donationsReportMember struct {
	MemberID         int                   `json:"member_id"`
	Name             string                `json:"name"`
	Rank             string                `json:"rank"`
	Weeks            []donationsMemberWeek `json:"weeks"`
	WeeksBelowWeekly int                   `json:"weeks_below_weekly"`
	WeeksBelowDaily  int                   `json:"weeks_below_daily"`
	WeeksNotRead     int                   `json:"weeks_not_read"` // over, judged weekly, no Sunday reading
}

type donationsCoverage struct {
	MembersEvaluated          int      `json:"members_evaluated"`
	WeeksWithoutRecord        []string `json:"weeks_without_record"`         // judged against today's settings
	WeeksWithoutSundayReading []string `json:"weeks_without_sunday_reading"` // over, and nobody read on Sunday
	MembersExcludedEX         int      `json:"members_excluded_ex"`          // former members with readings in range
}

type donationsReport struct {
	From        string                  `json:"from"`
	To          string                  `json:"to"`
	GeneratedAt string                  `json:"generated_at"`
	Weeks       []donationsReportWeek   `json:"weeks"`
	Members     []donationsReportMember `json:"members"`
	Coverage    donationsCoverage       `json:"coverage"`
}

// runDonationsReport judges every active member (or one) on every Monday–Sunday week the
// range touches, worst first: most weeks below either minimum, then most weeks not read to
// the end, then name.
func runDonationsReport(q rowQueryer, p donationsParams, now time.Time) (*donationsReport, error) {
	rng, err := resolveReportRangeFor(q, p.Range, now, reportWeekDonation)
	if err != nil {
		return nil, err
	}
	first, last := donationWeekMonday(rng.From), donationWeekMonday(rng.To)
	var weekDates []string
	for w := first; w <= last; w = addDays(w, 7) {
		weekDates = append(weekDates, w)
	}
	rep := &donationsReport{From: rng.From, To: rng.To, GeneratedAt: now.UTC().Format(time.RFC3339),
		Weeks: []donationsReportWeek{}, Members: []donationsReportMember{},
		Coverage: donationsCoverage{WeeksWithoutRecord: []string{}, WeeksWithoutSundayReading: []string{}}}

	mins, err := donationMinimumsForWeeks(q, weekDates)
	if err != nil {
		return nil, err
	}
	derived, err := loadDonationWeeksOf(q, p.MemberID, first, last, now)
	if err != nil {
		return nil, err
	}
	if err := q.QueryRow(`SELECT COUNT(DISTINCT m.id) FROM members m JOIN donation_readings d ON d.member_id = m.id
		WHERE m.rank = 'EX' AND d.game_date BETWEEN ? AND ?`, first, addDays(last, 6)).Scan(&rep.Coverage.MembersExcludedEX); err != nil {
		return nil, err
	}

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
	rows, err := q.Query(memberQuery+` ORDER BY name`, args...)
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
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if p.MemberID > 0 && len(members) == 0 {
		return nil, badReportParam("no such active member")
	}

	weekStats := make([]donationsReportWeek, len(weekDates))
	for i, w := range weekDates {
		weekStats[i] = donationsReportWeek{WeekDate: w, Minimums: mins[w], Over: !now.Before(donationWeekEnd(w))}
		if mins[w].Source == vsMinSourceCurrent {
			rep.Coverage.WeeksWithoutRecord = append(rep.Coverage.WeeksWithoutRecord, w)
		}
	}
	for _, m := range members {
		out := donationsReportMember{MemberID: m.id, Name: m.name, Rank: m.rank, Weeks: []donationsMemberWeek{}}
		for i, w := range weekDates {
			if m.jd != "" && m.jd > addDays(w, 6) {
				continue // joined after this week
			}
			dw, ok := derived[m.id][w]
			if !ok {
				dw = deriveDonationWeek(w, [7]donationDayReadings{}, now)
			}
			j := judgeDonationWeek(dw, mins[w], m.jd)
			out.Weeks = append(out.Weeks, donationsMemberWeek{WeekDate: w, Total: dw.Total, Days: dw.Days, donationJudgement: j})
			if dw.LastReadDay >= 0 {
				weekStats[i].MembersRead++
			}
			if dw.Total.Status == donationFinal {
				weekStats[i].MembersReadToEnd++
			}
			if j.BelowWeekly {
				out.WeeksBelowWeekly++
			}
			if j.BelowDaily {
				out.WeeksBelowDaily++
			}
			if j.NotReadToEnd {
				out.WeeksNotRead++
			}
		}
		rep.Members = append(rep.Members, out)
	}
	rep.Coverage.MembersEvaluated = len(rep.Members)
	for _, ws := range weekStats {
		if ws.Over && ws.MembersReadToEnd == 0 {
			rep.Coverage.WeeksWithoutSundayReading = append(rep.Coverage.WeeksWithoutSundayReading, ws.WeekDate)
		}
	}
	rep.Weeks = weekStats
	sort.SliceStable(rep.Members, func(i, j int) bool {
		a, b := rep.Members[i], rep.Members[j]
		if x, y := a.WeeksBelowWeekly+a.WeeksBelowDaily, b.WeeksBelowWeekly+b.WeeksBelowDaily; x != y {
			return x > y
		}
		if a.WeeksNotRead != b.WeeksNotRead {
			return a.WeeksNotRead > b.WeeksNotRead
		}
		return a.Name < b.Name
	})
	return rep, nil
}
