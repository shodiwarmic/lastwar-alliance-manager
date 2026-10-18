package app

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

// half builds a stored half read at clock (HH:MM) on date, UTC.
func half(amount int64, date, clock string) *donationHalf {
	return &donationHalf{Amount: amount, CapturedAt: date + " " + clock + ":00"}
}

// The plan's worked week (§4.1): Mon 2026-10-12 … Sun 10-18, every pair read together.
// Readings are at 20:00 UTC, which is the same game day.
func workedDonationWeek() [7]donationDayReadings {
	day := func(today, week int64, date string) donationDayReadings {
		return donationDayReadings{Today: half(today, date, "20:00"), Week: half(week, date, "20:00")}
	}
	return [7]donationDayReadings{
		day(1200, 1200, "2026-10-12"),
		day(500, 3000, "2026-10-13"),
		{},
		day(900, 4100, "2026-10-15"),
		day(1000, 5100, "2026-10-16"),
		day(900, 6000, "2026-10-17"),
		day(400, 6400, "2026-10-18"),
	}
}

func dayString(d donationDay) string {
	if d.Status == donationBlank {
		return "blank"
	}
	return d.Status + ":" + strconv.FormatInt(d.Amount, 10)
}

func TestDeriveDonationWeekWorkedExample(t *testing.T) {
	during := time.Date(2026, 10, 18, 22, 0, 0, 0, time.UTC) // Sunday evening, week not over
	w := deriveDonationWeek("2026-10-12", workedDonationWeek(), during)
	want := []string{"exact:2500", "so_far:500", "blank", "exact:900", "exact:1000", "exact:900", "so_far:400"}
	for d, wd := range want {
		if got := dayString(w.Days[d]); got != wd {
			t.Errorf("day %d = %s, want %s", d, got, wd)
		}
	}
	if w.Total != (donationWeekTotal{6400, donationSoFar}) || w.WeekOver || w.LastReadDay != 6 {
		t.Errorf("total %+v over %v last %d", w.Total, w.WeekOver, w.LastReadDay)
	}

	// Once the week is over (Monday 02:00 UTC), Sunday's last reading makes the total final.
	over := time.Date(2026, 10, 19, 2, 0, 0, 0, time.UTC)
	w = deriveDonationWeek("2026-10-12", workedDonationWeek(), over)
	if w.Total != (donationWeekTotal{6400, donationFinal}) || !w.WeekOver {
		t.Errorf("after the week: total %+v over %v", w.Total, w.WeekOver)
	}
	if dayString(w.Days[6]) != "so_far:400" {
		t.Errorf("Sunday after the week = %s, want so_far:400 (week_over carries the finality)", dayString(w.Days[6]))
	}
}

// Friday's week half re-read six hours after its today half: Friday's pair says nothing
// about Thursday's end, so Thursday and Friday drop to so far, Friday with a note; nothing
// else moves (phase 3, Codex F1).
func TestDeriveDonationWeekIncoherentPair(t *testing.T) {
	days := workedDonationWeek()
	days[4].Week = half(5100, "2026-10-17", "02:30") // Friday's game day, 6.5h after 20:00
	days[4].Today = half(1000, "2026-10-16", "20:00")
	w := deriveDonationWeek("2026-10-12", days, time.Date(2026, 10, 18, 22, 0, 0, 0, time.UTC))
	want := []string{"exact:2500", "so_far:500", "blank", "so_far:900", "so_far:1000", "exact:900", "so_far:400"}
	for d, wd := range want {
		if got := dayString(w.Days[d]); got != wd {
			t.Errorf("day %d = %s, want %s", d, got, wd)
		}
	}
	if w.Days[4].Note == "" || !strings.Contains(w.Days[4].Note, "apart") {
		t.Errorf("Friday's note = %q", w.Days[4].Note)
	}
}

func TestDeriveDonationWeekEdges(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	// Empty.
	w := deriveDonationWeek("2026-10-12", [7]donationDayReadings{}, now)
	for d := 0; d < 7; d++ {
		if w.Days[d].Status != donationBlank {
			t.Errorf("empty day %d = %+v", d, w.Days[d])
		}
	}
	if w.Total.Status != donationBlank || w.LastReadDay != -1 {
		t.Errorf("empty total %+v last %d", w.Total, w.LastReadDay)
	}

	// Monday only: so far, a so-far total even after the week (no Sunday reading).
	days := [7]donationDayReadings{{Today: half(700, "2026-10-12", "10:00"), Week: half(700, "2026-10-12", "10:00")}}
	w = deriveDonationWeek("2026-10-12", days, now)
	if dayString(w.Days[0]) != "so_far:700" || w.Total != (donationWeekTotal{700, donationSoFar}) {
		t.Errorf("Monday only: %s total %+v", dayString(w.Days[0]), w.Total)
	}

	// A conflicting pair (today above week) blanks its day with a note, and Monday then
	// has no end.
	days = workedDonationWeek()
	days[1] = donationDayReadings{Today: half(4000, "2026-10-13", "20:00"), Week: half(3000, "2026-10-13", "20:00")}
	w = deriveDonationWeek("2026-10-12", days, now)
	if w.Days[1].Status != donationBlank || w.Days[1].Note == "" {
		t.Errorf("conflicting Tuesday = %+v", w.Days[1])
	}
	if dayString(w.Days[0]) != "so_far:1200" {
		t.Errorf("Monday beside a conflicting Tuesday = %s", dayString(w.Days[0]))
	}

	// A day whose end is below its start counts 0, never negative.
	days = workedDonationWeek()
	days[2] = donationDayReadings{Today: half(100, "2026-10-14", "20:00"), Week: half(2000, "2026-10-14", "20:00")}
	w = deriveDonationWeek("2026-10-12", days, now)
	if dayString(w.Days[1]) != "exact:0" { // byEnd(Mon)=2500, byEnd(Tue)=1900
		t.Errorf("Tuesday with a lower end = %s", dayString(w.Days[1]))
	}
}

func TestDonationResetWindow(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	for raw, refused := range map[string]bool{
		"2026-10-14T01:59:59Z":      false,
		"2026-10-14T02:00:00Z":      true,
		"2026-10-14T02:09:59Z":      true,
		"2026-10-14T02:10:00Z":      false,
		"2026-10-14T00:05:00-02:00": true, // the same instant written in game time
	} {
		_, err := parseDonationCapturedAt(raw, now)
		if (err != nil) != refused {
			t.Errorf("%s: err = %v, want refused %v", raw, err, refused)
		}
	}
	if got, err := parseDonationCapturedAt("", now); err != nil || !got.Equal(now) {
		t.Errorf("absent captured_at = %v, %v; want now", got, err)
	}
	if _, err := parseDonationCapturedAt("2026-08-01T10:00:00Z", now); err == nil {
		t.Error("a capture 74 days back was accepted")
	}
	got := clampOutOfDonationResetWindow(time.Date(2026, 10, 14, 2, 5, 0, 0, time.UTC))
	if !got.Equal(time.Date(2026, 10, 14, 2, 10, 0, 0, time.UTC)) {
		t.Errorf("clamp = %v", got)
	}
}

func TestRefuseImpossibleDonationPairs(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 10, 14, h, 0, 0, 0, time.UTC) }
	in := []donationHalfIn{
		{MemberID: 1, Name: "A", Kind: donationKindToday, Amount: 900, Captured: at(10)},
		{MemberID: 1, Name: "A", Kind: donationKindWeek, Amount: 500, Captured: at(10)},
		// Today read hours after the week: may exceed it.
		{MemberID: 2, Name: "B", Kind: donationKindToday, Amount: 900, Captured: at(20)},
		{MemberID: 2, Name: "B", Kind: donationKindWeek, Amount: 500, Captured: at(10)},
		{MemberID: 3, Name: "C", Kind: donationKindToday, Amount: 100, Captured: at(10)},
	}
	kept, errs := refuseImpossibleDonationPairs(in)
	if len(kept) != 3 || len(errs) != 1 || !strings.Contains(errs[0], "A (donations") {
		t.Errorf("kept %d, errs %v", len(kept), errs)
	}
}

func TestRecordDonationReading(t *testing.T) {
	setupMobileTestDB(t)
	m := seedMember(t, "Giver", "R3")
	w := donationReadingWrite{MemberID: m, GameDate: "2026-10-14", Kind: donationKindToday, Amount: 500,
		CapturedAt: "2026-10-14 20:00:00", Source: "mobile"}
	write := func(w donationReadingWrite) bool {
		t.Helper()
		changed, err := recordDonationReading(db, w)
		if err != nil {
			t.Fatal(err)
		}
		return changed
	}
	stored := func() (int64, string, string) {
		var a int64
		var c, s string
		db.QueryRow(`SELECT amount, captured_at, source FROM donation_readings WHERE member_id = ?`, m).Scan(&a, &c, &s)
		return a, c, s
	}

	if !write(w) {
		t.Error("first reading not reported as changed")
	}
	// Later and different: wins.
	w2 := w
	w2.Amount, w2.CapturedAt, w2.Source = 800, "2026-10-14 22:00:00", "ocr"
	if !write(w2) {
		t.Error("later reading not reported as changed")
	}
	// Older: ignored.
	w3 := w
	w3.Amount, w3.CapturedAt = 650, "2026-10-14 21:00:00"
	if write(w3) {
		t.Error("older reading reported as changed")
	}
	if a, c, s := stored(); a != 800 || c != "2026-10-14 22:00:00" || s != "ocr" {
		t.Errorf("after an older reading: %d %s %s", a, c, s)
	}
	// Same amount, later: unchanged, but captured_at advances.
	w4 := w2
	w4.CapturedAt = "2026-10-14 23:30:00"
	if write(w4) {
		t.Error("a repeat reported as changed")
	}
	if _, c, _ := stored(); c != "2026-10-14 23:30:00" {
		t.Errorf("a repeat did not advance captured_at: %s", c)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM donation_readings`); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
}

func TestLoadDonationWeeks(t *testing.T) {
	setupMobileTestDB(t)
	m := seedMember(t, "Giver", "R3")
	for _, r := range []struct {
		date, kind string
		amount     int64
	}{
		{"2026-10-13", "today", 500}, {"2026-10-13", "week", 3000},
		{"2026-10-14", "today", 100}, {"2026-10-14", "week", 3400},
		{"2026-10-20", "today", 50}, // the next week
	} {
		mustExec(t, `INSERT INTO donation_readings (member_id, game_date, kind, amount, captured_at, source)
			VALUES (?, ?, ?, ?, ? || ' 20:00:00', 'ocr')`, m, r.date, r.kind, r.amount, r.date)
	}
	weeks, err := loadDonationWeeks(db, "2026-10-12", "2026-10-25", time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	w := weeks[m]["2026-10-12"]
	if dayString(w.Days[1]) != "exact:800" || w.Total != (donationWeekTotal{3400, donationSoFar}) {
		t.Errorf("week 1: Tue %s total %+v", dayString(w.Days[1]), w.Total)
	}
	if dayString(weeks[m]["2026-10-19"].Days[1]) != "so_far:50" {
		t.Errorf("week 2: %+v", weeks[m]["2026-10-19"])
	}
	avail, err := donationWeeksAvailable(db)
	if err != nil || strings.Join(avail, ",") != "2026-10-19,2026-10-12" {
		t.Errorf("weeks available = %v, %v", avail, err)
	}
}

// Migration 086 on an install that already holds a custom donation_below_minimum row (the
// strike-category route derives keys from labels): the row keeps its id and becomes the
// system row. The report key follows view_report_vs_misses, never overwriting a choice.
func TestDonationsMigrationPromotesAndSeeds(t *testing.T) {
	conn := migrateTo(t, 85)
	for _, q := range []string{
		`INSERT INTO strike_types (id, key, label, is_system, active, sort_order) VALUES (40, 'donation_below_minimum', 'Donation Below Minimum', 0, 0, 100)`,
		`UPDATE rank_permissions SET permissions = json_set(permissions, '$.view_report_vs_misses', json('true')) WHERE rank IN ('R4', 'R5')`,
		`UPDATE rank_permissions SET permissions = json_set(permissions, '$.view_report_vs_misses', json('false')) WHERE rank IN ('R1', 'R2', 'R3')`,
		`UPDATE rank_permissions SET permissions = json_set(permissions, '$.view_report_donations', json('false')) WHERE rank = 'R5'`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("fixture %q: %v", q, err)
		}
	}
	if err := goose.UpTo(conn, "migrations", 86); err != nil {
		t.Fatalf("goose.UpTo(86): %v", err)
	}
	var id, sys, active, n int
	conn.QueryRow(`SELECT COUNT(*), MAX(id), MAX(is_system), MAX(active) FROM strike_types WHERE key = 'donation_below_minimum'`).Scan(&n, &id, &sys, &active)
	if n != 1 || id != 40 || sys != 1 || active != 1 {
		t.Errorf("strike row: n %d id %d system %d active %d", n, id, sys, active)
	}
	perm := func(rank string) any {
		var v any
		conn.QueryRow(`SELECT json_extract(permissions, '$.view_report_donations') FROM rank_permissions WHERE rank = ?`, rank).Scan(&v)
		return v
	}
	if perm("R4") != int64(1) || perm("R3") != nil || perm("R5") != int64(0) {
		t.Errorf("report key: R4 %v R3 %v R5 %v", perm("R4"), perm("R3"), perm("R5"))
	}
	var weekly, daily, flag int
	conn.QueryRow(`SELECT donation_weekly_minimum, donation_daily_minimum, donation_flag_days_threshold FROM settings WHERE id = 1`).Scan(&weekly, &daily, &flag)
	if weekly != 0 || daily != 0 || flag != 2 {
		t.Errorf("settings defaults %d %d %d", weekly, daily, flag)
	}
	var rows int
	conn.QueryRow(`SELECT COUNT(*) FROM donation_week_minimums`).Scan(&rows)
	if rows != 0 {
		t.Errorf("a fresh 086 recorded %d minimum rows", rows)
	}
}

// A fresh install gets the strike category as a system row.
func TestDonationsMigrationFreshInstall(t *testing.T) {
	conn := migrateTo(t, 86)
	var sys int
	if err := conn.QueryRow(`SELECT is_system FROM strike_types WHERE key = 'donation_below_minimum'`).Scan(&sys); err != nil || sys != 1 {
		t.Errorf("strike row: %d, %v", sys, err)
	}
}

// The Settings save records the week's donation minimums, as it does VS's: an unchanged
// save writes nothing; two saves in one week leave one row; 0 is a real value; an omitted
// minimum keeps the stored one; the day count is 1–7; a failing history write rolls back.
func TestSettingsSaveRecordsTheWeeksDonationMinimums(t *testing.T) {
	setupSettingsTestDB(t)
	with := func(weekly, daily any, flag int) map[string]any {
		s := baseSettings()
		s["donation_weekly_minimum"], s["donation_daily_minimum"], s["donation_flag_days_threshold"] = weekly, daily, flag
		if weekly == nil {
			delete(s, "donation_weekly_minimum")
		}
		return s
	}
	if w := putSettings(t, with(0, 0, 2)); w.Code != http.StatusOK {
		t.Fatalf("unchanged: %d %s", w.Code, w.Body)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM donation_week_minimums`); n != 0 {
		t.Errorf("unchanged minimums wrote %d rows", n)
	}
	putSettings(t, with(40000, 2000, 3))
	putSettings(t, with(50000, 2000, 3))
	var n, weekly, daily int
	var week string
	db.QueryRow(`SELECT COUNT(*), MAX(week_date), MAX(weekly_minimum), MAX(daily_minimum) FROM donation_week_minimums`).Scan(&n, &week, &weekly, &daily)
	if n != 1 || week != currentVSWeekMonday() || weekly != 50000 || daily != 2000 {
		t.Errorf("rows %d week %s weekly %d daily %d", n, week, weekly, daily)
	}
	var details string
	db.QueryRow(`SELECT details FROM activity_log WHERE entity_type = 'donation_week_minimum' ORDER BY id DESC LIMIT 1`).Scan(&details)
	if details != "weekly: 40000 → 50000; daily: 2000 → 2000" {
		t.Errorf("activity details %q", details)
	}
	var flag int
	db.QueryRow(`SELECT donation_flag_days_threshold FROM settings WHERE id = 1`).Scan(&flag)
	if flag != 3 {
		t.Errorf("flag days = %d", flag)
	}

	// Omitted weekly minimum: kept. 0 daily: recorded as off.
	putSettings(t, with(nil, 0, 0))
	db.QueryRow(`SELECT weekly_minimum, daily_minimum FROM donation_week_minimums`).Scan(&weekly, &daily)
	db.QueryRow(`SELECT donation_flag_days_threshold FROM settings WHERE id = 1`).Scan(&flag)
	if weekly != 50000 || daily != 0 || flag != 3 {
		t.Errorf("after an omitted weekly and a 0 daily: weekly %d daily %d flag %d", weekly, daily, flag)
	}

	for _, bad := range []map[string]any{with(-1, 0, 2), with(0, -5, 2), with(0, 0, 8)} {
		if w := putSettings(t, bad); w.Code != http.StatusBadRequest {
			t.Errorf("%v: status %d, want 400", bad, w.Code)
		}
	}

	mustExec(t, `DROP TABLE donation_week_minimums`)
	if w := putSettings(t, with(60000, 0, 3)); w.Code != http.StatusInternalServerError {
		t.Errorf("failing history write: status %d", w.Code)
	}
	db.QueryRow(`SELECT donation_weekly_minimum FROM settings WHERE id = 1`).Scan(&weekly)
	if weekly != 50000 {
		t.Errorf("setting = %d after a failed save, want 50000", weekly)
	}
}

func TestDonationMinimumsResolve(t *testing.T) {
	setupSettingsTestDB(t)
	mustExec(t, `UPDATE settings SET donation_weekly_minimum = 9, donation_daily_minimum = 1, donation_flag_days_threshold = 4 WHERE id = 1`)
	mustExec(t, `INSERT INTO donation_week_minimums (week_date, weekly_minimum, daily_minimum) VALUES ('2026-09-28', 30000, 1000), ('2026-10-12', 0, 2000)`)
	got, err := donationMinimumsForWeeks(db, []string{"2026-09-21", "2026-10-05", "2026-10-12", "2026-10-19"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]donationMinimums{
		"2026-09-21": {9, 1, 4, vsMinSourceCurrent},
		"2026-10-05": {30000, 1000, 4, vsMinSourceRecorded},
		"2026-10-12": {0, 2000, 4, vsMinSourceRecorded},
		"2026-10-19": {0, 2000, 4, vsMinSourceRecorded},
	}
	for w, m := range want {
		if got[w] != m {
			t.Errorf("%s = %+v, want %+v", w, got[w], m)
		}
		one, _ := donationMinimumsForWeek(db, w)
		if one != m {
			t.Errorf("single %s = %+v, want %+v", w, one, m)
		}
	}
}

func TestJudgeDonationWeek(t *testing.T) {
	over := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	w := deriveDonationWeek("2026-10-12", workedDonationWeek(), over) // total 6,400 final; Mon 2500 Thu 900 Fri 1000 Sat 900 exact, Sun 400
	mins := donationMinimums{Weekly: 7000, Daily: 950, FlagDays: 3}

	j := judgeDonationWeek(w, mins, "2025-01-01")
	// Judged: Mon, Thu, Fri, Sat (exact) and Sunday (week over) = 5; below 950: Thu 900, Sat 900, Sun 400.
	if !j.BelowWeekly || j.NotReadToEnd || j.JudgedDays != 5 || j.DaysBelowDaily != 3 || !j.BelowDaily {
		t.Errorf("judgement %+v", j)
	}
	// Tuesday's so-far 500 is never judged; minimum 0 flags nobody.
	if j := judgeDonationWeek(w, donationMinimums{FlagDays: 1}, ""); j.BelowWeekly || j.BelowDaily || j.NotReadToEnd {
		t.Errorf("minimums off: %+v", j)
	}
	// Joined on the Thursday: not judged weekly; Mon is before joining.
	if j := judgeDonationWeek(w, mins, "2026-10-15"); j.BelowWeekly || !j.JoinedMidWeek || j.JudgedDays != 4 {
		t.Errorf("joined mid-week: %+v", j)
	}
	// No Sunday reading: not read to the end, never below.
	days := workedDonationWeek()
	days[6] = donationDayReadings{}
	w2 := deriveDonationWeek("2026-10-12", days, over)
	if j := judgeDonationWeek(w2, mins, ""); j.BelowWeekly || !j.NotReadToEnd {
		t.Errorf("no Sunday: %+v", j)
	}
	// Never read at all: neither below nor "not read to the end" (no row is no score).
	if j := judgeDonationWeek(deriveDonationWeek("2026-10-12", [7]donationDayReadings{}, over), mins, ""); j.BelowWeekly || j.NotReadToEnd || j.JudgedDays != 0 {
		t.Errorf("never read: %+v", j)
	}
	// The week still running: Sunday is not judged and the weekly rule waits.
	during := deriveDonationWeek("2026-10-12", workedDonationWeek(), time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC))
	if j := judgeDonationWeek(during, mins, ""); j.BelowWeekly || j.NotReadToEnd || j.JudgedDays != 4 {
		t.Errorf("week running: %+v", j)
	}
}

// donationDayAt stores both halves of one day, read at 20:00 UTC (that game day).
func donationDayAt(t *testing.T, member int, date string, today, week int64) {
	t.Helper()
	for _, h := range []struct {
		kind   string
		amount int64
	}{{"today", today}, {"week", week}} {
		mustExec(t, `INSERT INTO donation_readings (member_id, game_date, kind, amount, captured_at, source)
			VALUES (?, ?, ?, ?, ? || ' 20:00:00', 'ocr')`, member, date, h.kind, h.amount, date)
	}
}

// The members API judges both donation rules on the right weeks and says which, and the
// strike targets come with the verdicts (258, 266, decision 23).
func TestAccountabilityMembersJudgesDonations(t *testing.T) {
	setupSettingsTestDB(t)
	a := seedMember(t, "Alder", "R3")   // last week's total below the weekly minimum
	b := seedMember(t, "Birch", "R3")   // this week below the daily minimum on two days
	c := seedMember(t, "Cedar", "R3")   // last week never read on Sunday
	d := seedMember(t, "Dogwood", "R3") // nothing read at all
	mustExec(t, `UPDATE settings SET donation_flag_days_threshold = 2 WHERE id = 1`)
	mustExec(t, `INSERT INTO donation_week_minimums (week_date, weekly_minimum, daily_minimum) VALUES ('2026-10-05', 25000, 2000)`)
	donationDayAt(t, a, "2026-10-11", 1000, 20000)
	donationDayAt(t, b, "2026-10-11", 4000, 30000)
	donationDayAt(t, c, "2026-10-08", 3000, 12000)
	donationDayAt(t, b, "2026-10-12", 1000, 1000)
	donationDayAt(t, b, "2026-10-13", 500, 1500)
	donationDayAt(t, b, "2026-10-14", 300, 1800)

	wed := time.Date(2026, 10, 14, 21, 0, 0, 0, time.UTC)
	resp, err := loadAccountabilityMembers(db, wed)
	if err != nil {
		t.Fatal(err)
	}
	ctx := resp.Context
	if ctx.DonationWeeklyWeek != "2026-10-05" || ctx.DonationDailyWeek != "2026-10-12" || ctx.DonationJudgedDays != 2 {
		t.Errorf("context %+v", ctx)
	}
	if ctx.DonationWeeklyMinimums.Weekly != 25000 || ctx.DonationDailyMinimums.Daily != 2000 || ctx.DonationDailyMinimums.Source != vsMinSourceRecorded {
		t.Errorf("minimums %+v / %+v", ctx.DonationWeeklyMinimums, ctx.DonationDailyMinimums)
	}
	by := map[int]AccountabilityMember{}
	for _, m := range resp.Members {
		by[m.ID] = m
	}
	if m := by[a]; !m.DonationBelowWeekly || m.DonationBelowDaily || len(m.DonationStrikes) != 1 ||
		m.DonationStrikes[0].Week != "2026-10-05" || m.DonationStrikes[0].Reason != "weekly 20,000 < 25,000 (week of 5 Oct)" {
		t.Errorf("Alder %+v", m)
	}
	if m := by[b]; m.DonationBelowWeekly || !m.DonationBelowDaily || m.DonationDaysBelow != 2 || len(m.DonationStrikes) != 1 ||
		m.DonationStrikes[0].Week != "2026-10-12" || m.DonationStrikes[0].Reason != "daily below 2,000 on 2 days (week of 12 Oct)" ||
		m.DonationTotal != 1800 || m.DonationTotalStatus != donationSoFar {
		t.Errorf("Birch %+v", m)
	}
	if m := by[c]; m.DonationBelowWeekly || m.DonationBelowDaily || len(m.DonationStrikes) != 0 {
		t.Errorf("Cedar (not read to the end) %+v", m)
	}
	if m := by[d]; m.DonationTotalStatus != donationBlank || len(m.DonationStrikes) != 0 {
		t.Errorf("Dogwood %+v", m)
	}

	// On a game-Monday: the VS week is the fallback, the same week vsEvalContextAt gives,
	// and with no judged day yet this week the daily rule judges last week too — so a
	// member below both rules there gets one target naming both.
	mustExec(t, `DELETE FROM donation_readings WHERE game_date >= '2026-10-12'`)
	for i, day := range []string{"2026-10-05", "2026-10-06", "2026-10-07", "2026-10-08", "2026-10-09", "2026-10-10"} {
		donationDayAt(t, a, day, 100, int64(100*(i+1)))
	}
	mon := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	resp, err = loadAccountabilityMembers(db, mon)
	if err != nil {
		t.Fatal(err)
	}
	wantWeek, _, _, _, _, _ := vsEvalContextAt(db, mon)
	if resp.Context.VSWeek != wantWeek || wantWeek != "2026-10-05" || !resp.Context.VSFallback {
		t.Errorf("Monday VS week %q fallback %v, want %q", resp.Context.VSWeek, resp.Context.VSFallback, wantWeek)
	}
	if resp.Context.DonationDailyWeek != "2026-10-05" {
		t.Errorf("Monday daily week %q", resp.Context.DonationDailyWeek)
	}
	for _, m := range resp.Members {
		if m.ID != a {
			continue
		}
		if len(m.DonationStrikes) != 1 || strings.Join(m.DonationStrikes[0].Rules, ",") != "weekly,daily" ||
			!strings.Contains(m.DonationStrikes[0].Reason, "; daily below") {
			t.Errorf("Alder on Monday %+v", m.DonationStrikes)
		}
	}
}

func TestDonationStrikeTargets(t *testing.T) {
	below := donationJudgement{BelowWeekly: true}
	daily := donationJudgement{BelowDaily: true, DaysBelowDaily: 1}
	none := donationJudgement{}
	cases := []struct {
		name               string
		weekly, dailyJ     donationJudgement
		weeklyWeek, dailyW string
		want               []string // week:rules
	}{
		{"weekly only", below, none, "2026-10-05", "2026-10-12", []string{"2026-10-05:weekly"}},
		{"daily only", none, daily, "2026-10-05", "2026-10-12", []string{"2026-10-12:daily"}},
		{"both, one week", below, daily, "2026-10-05", "2026-10-05", []string{"2026-10-05:weekly+daily"}},
		{"both, two weeks", below, daily, "2026-10-05", "2026-10-12", []string{"2026-10-05:weekly", "2026-10-12:daily"}},
	}
	for _, c := range cases {
		var got []string
		for _, tg := range donationStrikeTargets(c.weeklyWeek, c.weekly, 1000, 2000, c.dailyW, c.dailyJ, 500) {
			got = append(got, tg.Week+":"+strings.Join(tg.Rules, "+"))
		}
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if s := donationStrikeTargets("2026-10-05", none, 0, 0, "2026-10-12", daily, 500)[0].Reason; s != "daily below 500 on 1 day (week of 12 Oct)" {
		t.Errorf("reason %q", s)
	}
}

// One donation strike per member and week: a second is 409, and two officers filing at
// once leave one row (the check and the insert share a transaction).
func TestStrikeCreateOnePerWeek(t *testing.T) {
	setupSettingsTestDB(t)
	member := seedMember(t, "Elm", "R3")
	post := func(key, week string) int {
		rr := httptest.NewRecorder()
		handleStrikeCreate(rr, strikeTypeRequest(http.MethodPost, "/", map[string]any{
			"member_id": member, "strike_type": key, "reason": "test", "ref_date": week,
		}, 0))
		return rr.Code
	}
	if c := post("donation_below_minimum", "2026-10-05"); c != http.StatusOK {
		t.Fatalf("first: %d", c)
	}
	if c := post("donation_below_minimum", "2026-10-05"); c != http.StatusConflict {
		t.Errorf("second for the week: %d, want 409", c)
	}
	if c := post("donation_below_minimum", "2026-10-12"); c != http.StatusOK {
		t.Errorf("another week: %d", c)
	}
	if c := post("vs_below_threshold", "2026-10-05"); c != http.StatusOK {
		t.Errorf("a VS strike the same week: %d", c)
	}

	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func(i int) { defer wg.Done(); codes[i] = post("vs_below_threshold", "2026-10-19") }(i)
	}
	wg.Wait()
	if n := countRows(t, `SELECT COUNT(*) FROM accountability_strikes WHERE strike_type = 'vs_below_threshold' AND ref_date = '2026-10-19'`); n != 1 {
		t.Errorf("concurrent creates left %d rows (codes %v)", n, codes)
	}
}
