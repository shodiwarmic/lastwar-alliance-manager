package app

import (
	"strconv"
	"strings"
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
		"2026-10-14T01:59:59Z": false,
		"2026-10-14T02:00:00Z": true,
		"2026-10-14T02:09:59Z": true,
		"2026-10-14T02:10:00Z": false,
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
