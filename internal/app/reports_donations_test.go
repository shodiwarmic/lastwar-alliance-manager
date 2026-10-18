package app

import (
	"net/http"
	"strings"
	"testing"
)

func TestLastCompleteSunday(t *testing.T) {
	cases := map[string]string{
		"2026-10-15T12:00:00Z": "2026-10-11", // Thursday
		"2026-10-18T12:00:00Z": "2026-10-11", // Sunday: today's Sunday is not over
		"2026-10-19T01:00:00Z": "2026-10-11", // UTC Monday, game Sunday
		"2026-10-19T03:00:00Z": "2026-10-18", // game Monday: yesterday
		"2026-10-17T12:00:00Z": "2026-10-11", // Saturday
	}
	for now, want := range cases {
		if got := lastCompleteSunday(mustTime(t, now)); got != want {
			t.Errorf("%s: got %s, want %s", now, got, want)
		}
	}
}

func TestResolveReportRangeDonationWeeks(t *testing.T) {
	setupSettingsTestDB(t)
	thu := mustTime(t, "2026-10-15T12:00:00Z")
	cases := []struct {
		name string
		p    reportRangeParams
		want reportRange
	}{
		{"4 complete weeks", reportRangeParams{Weeks: 4}, reportRange{"2026-09-14", "2026-10-11"}},
		{"1 week", reportRangeParams{Weeks: 1}, reportRange{"2026-10-05", "2026-10-11"}},
		{"current", reportRangeParams{Weeks: 1, Current: true}, reportRange{"2026-10-05", "2026-10-15"}},
		{"explicit clamped", reportRangeParams{From: "2026-10-01", To: "2026-12-31"}, reportRange{"2026-10-01", "2026-10-15"}},
	}
	for _, c := range cases {
		got, err := resolveReportRangeFor(db, c.p, thu, reportWeekDonation)
		if err != nil || got != c.want {
			t.Errorf("%s: got %+v, %v; want %+v", c.name, got, err, c.want)
		}
	}
	if _, err := resolveReportRangeFor(db, reportRangeParams{All: true}, thu, reportWeekDonation); !isReportParamError(err) ||
		!strings.Contains(err.Error(), "no donation readings") {
		t.Errorf("all with no readings: %v", err)
	}
	m := seedMember(t, "Alder", "R3")
	donationDayAt(t, m, "2026-08-13", 10, 10) // a Thursday: all starts on its Monday
	if got, err := resolveReportRangeFor(db, reportRangeParams{All: true}, thu, reportWeekDonation); err != nil || got != (reportRange{"2026-08-10", "2026-10-11"}) {
		t.Errorf("all: %+v %v", got, err)
	}
	// The VS kind is untouched.
	if got, _ := resolveReportRange(db, reportRangeParams{Weeks: 1}, thu); got != (reportRange{"2026-10-05", "2026-10-10"}) {
		t.Errorf("VS 1 week: %+v", got)
	}
}

// Three weeks with a changed minimum between them, a member who joined mid-week, and a
// week nobody read on Sunday.
func TestDonationsReport(t *testing.T) {
	setupPreviewTestDB(t)
	a := seedMember(t, "Alder", "R3")
	b := seedMember(t, "Birch", "R3")
	mustExec(t, `UPDATE members SET joined_at = '2026-09-30' WHERE id = ?`, b) // a Wednesday
	gone := seedMember(t, "Gone", "EX")
	mustExec(t, `INSERT INTO donation_week_minimums (week_date, weekly_minimum, daily_minimum) VALUES ('2026-09-21', 10000, 0), ('2026-10-05', 30000, 0)`)
	// Weeks of 21 Sep, 28 Sep, 5 Oct. Alder reads 20,000 every Sunday; Birch only the last
	// week, on Sunday; nobody is read on the Sunday of 28 Sep but Alder on its Saturday.
	donationDayAt(t, a, "2026-09-27", 2000, 20000)
	donationDayAt(t, a, "2026-10-03", 2000, 18000)
	donationDayAt(t, a, "2026-10-11", 2000, 20000)
	donationDayAt(t, b, "2026-10-11", 9000, 40000)
	donationDayAt(t, gone, "2026-10-11", 1, 1)

	rep, err := runDonationsReport(db, donationsParams{Range: reportRangeParams{Weeks: 3}}, mustTime(t, "2026-10-15T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if rep.From != "2026-09-21" || rep.To != "2026-10-11" || len(rep.Weeks) != 3 {
		t.Fatalf("range %s–%s, %d weeks", rep.From, rep.To, len(rep.Weeks))
	}
	if rep.Weeks[0].Minimums.Weekly != 10000 || rep.Weeks[1].Minimums.Weekly != 10000 || rep.Weeks[2].Minimums.Weekly != 30000 {
		t.Errorf("minimums %+v", rep.Weeks)
	}
	if strings.Join(rep.Coverage.WeeksWithoutSundayReading, ",") != "2026-09-28" || rep.Coverage.MembersExcludedEX != 1 {
		t.Errorf("coverage %+v", rep.Coverage)
	}
	by := map[string]donationsReportMember{}
	for _, m := range rep.Members {
		by[m.Name] = m
	}
	// Alder: above 10,000 in week 1, not read to the end in week 2, below 30,000 in week 3.
	if m := by["Alder"]; m.WeeksBelowWeekly != 1 || m.WeeksNotRead != 1 || len(m.Weeks) != 3 || !m.Weeks[2].BelowWeekly || m.Weeks[0].BelowWeekly {
		t.Errorf("Alder %+v", m)
	}
	// Birch joined on Wednesday 30 Sep: no week 1, week 2 not judged weekly, week 3 above.
	if m := by["Birch"]; len(m.Weeks) != 2 || !m.Weeks[0].JoinedMidWeek || m.Weeks[0].NotReadToEnd || m.WeeksBelowWeekly != 0 {
		t.Errorf("Birch %+v", m)
	}
	if _, ok := by["Gone"]; ok || rep.Members[0].Name != "Alder" {
		t.Errorf("order/EX: %v", rep.Members)
	}

	// One member; and an unknown one is a parameter error.
	one, err := runDonationsReport(db, donationsParams{Range: reportRangeParams{Weeks: 3}, MemberID: b}, mustTime(t, "2026-10-15T12:00:00Z"))
	if err != nil || len(one.Members) != 1 || one.Members[0].Name != "Birch" {
		t.Errorf("one member: %+v %v", one, err)
	}
	if _, err := runDonationsReport(db, donationsParams{Range: reportRangeParams{Weeks: 1}, MemberID: gone}, mustTime(t, "2026-10-15T12:00:00Z")); !isReportParamError(err) {
		t.Errorf("former member: %v", err)
	}
}

func TestDonationsReportRoute(t *testing.T) {
	setupPreviewTestDB(t)
	officer := sessionCookie(t, 3, "")
	setReportKeys(t, false, false, false)
	mustExec(t, `UPDATE rank_permissions SET permissions = json_set(permissions, '$.view_report_donations', json('false')) WHERE rank = 'R3'`)
	if w := serveRouter(t, "GET", "/api/reports/donations?weeks=2", "", officer); w.Code != http.StatusForbidden {
		t.Errorf("without the key: %d", w.Code)
	}
	// A rank holding only the donations key sees the Reports link and the tab.
	mustExec(t, `UPDATE rank_permissions SET permissions = json_set(permissions, '$.view_report_donations', json('true')) WHERE rank = 'R3'`)
	if w := serveRouter(t, "GET", "/api/reports/donations?weeks=2", "", officer); w.Code != http.StatusOK {
		t.Errorf("with the key: %d %s", w.Code, w.Body)
	}
	w := serveRouter(t, "GET", "/reports", "", officer)
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, `href="/reports"`) || !strings.Contains(body, `data-tab="donations"`) || strings.Contains(body, `data-tab="weekly"`) {
		t.Errorf("reports page for a donations-only rank: %d (link %v, tab %v)", w.Code,
			strings.Contains(body, `href="/reports"`), strings.Contains(body, `data-tab="donations"`))
	}
}

// The Weekly summary's donations section names last week, not the VS evaluated week.
func TestWeeklySummaryDonations(t *testing.T) {
	setupPreviewTestDB(t)
	wed := mustTime(t, "2026-10-14T12:00:00Z") // VS week 12 Oct; last donation week 5 Oct
	rep, err := runWeeklySummaryReport(db, weeklySummaryParams{}, wed)
	if err != nil || rep.Donations != nil {
		t.Fatalf("no readings: %+v %v", rep.Donations, err)
	}
	names := []string{"A", "B", "C", "D", "E", "F", "G"}
	for i, n := range names {
		id := seedMember(t, n+"lder", "R3")
		donationDayAt(t, id, "2026-10-11", 100, int64(10000*(i+1)))
	}
	late := seedMember(t, "Late", "R3")
	donationDayAt(t, late, "2026-10-09", 100, 500) // read, not to the end
	rep, err = runWeeklySummaryReport(db, weeklySummaryParams{}, wed)
	if err != nil {
		t.Fatal(err)
	}
	d := rep.Donations
	if d == nil || d.WeekDate != "2026-10-05" || rep.WeekDate != "2026-10-12" || d.MembersRead != 8 || d.ReadToEnd != 7 {
		t.Fatalf("section %+v (VS week %s)", d, rep.WeekDate)
	}
	if len(d.Leaders) != 5 || d.Leaders[0].Value != 70000 || len(d.Below) != 0 {
		t.Errorf("leaders %v below %v", d.Leaders, d.Below)
	}
	mustExec(t, `INSERT INTO donation_week_minimums (week_date, weekly_minimum, daily_minimum) VALUES ('2026-10-05', 25000, 0)`)
	rep, _ = runWeeklySummaryReport(db, weeklySummaryParams{}, wed)
	if b := rep.Donations.Below; len(b) != 2 || b[0].Value != 10000 || b[1].Value != 20000 {
		t.Errorf("below, worst first: %v", b)
	}
}
