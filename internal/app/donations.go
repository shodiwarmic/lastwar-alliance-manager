// donations.go - the members' tech donations: the reading store, its validation, and the
// derivation that turns a week of readings into day amounts (Project 20).
//
// The game shows two numbers per member, both "so far": today (since the 02:00 UTC reset)
// and week (since Monday, game time — the donation week is Monday–Sunday, unlike VS's
// Monday–Saturday). A reading is one of them, a "half". The store keeps the latest half
// per member, game day and kind; every surface reads days through deriveDonationWeek, so
// the rule lives once and no two pages can disagree.

package app

import (
	"database/sql"
	"fmt"
	"sort"
	"time"

	"lastwar-alliance/internal/gametime"
)

const (
	donationKindToday = "today"
	donationKindWeek  = "week"

	// donationPairTolerance: two halves of one day form a pair only when read within this
	// of each other. week − today means "given by the end of yesterday" only for two numbers
	// read at one moment, and the halves are refreshed independently.
	donationPairTolerance = 15 * time.Minute

	// donationResetWindow: a reading taken in the first ten minutes after the 02:00 UTC
	// reset is refused — lists in that window carry yesterday's "today" beside the new
	// week's 0.
	donationResetWindow = 10 * time.Minute

	// Day statuses and week-total statuses, as the API reports them.
	donationExact = "exact"
	donationSoFar = "so_far"
	donationBlank = "blank"
	donationFinal = "final"
)

// donationCategoryKinds maps the two commit categories to the half each carries.
var donationCategoryKinds = map[string]string{
	"donation_daily":  donationKindToday,
	"donation_weekly": donationKindWeek,
}

// --- the write --------------------------------------------------------------------------

// donationReadingWrite is one half to store. CapturedAt is UTC in gametime.SQLiteLayout.
type donationReadingWrite struct {
	MemberID   int
	GameDate   string
	Kind       string
	Amount     int64
	CapturedAt string
	Source     string
}

// recordDonationReading stores a half unless a later one is already stored for its member,
// day and kind: an older screenshot never overwrites a newer reading. The comparison is
// in SQL (both sides TEXT in SQLiteLayout). A repeat of the same amount still advances
// captured_at — Sunday's last reading is what finalises a week — but is reported as
// unchanged. changed is "no row before, or a different amount that was written".
func recordDonationReading(q historyExecer, w donationReadingWrite) (bool, error) {
	var stored int64
	err := q.QueryRow(`SELECT amount FROM donation_readings WHERE member_id = ? AND game_date = ? AND kind = ?`,
		w.MemberID, w.GameDate, w.Kind).Scan(&stored)
	had := err == nil
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	res, err := q.Exec(`
		INSERT INTO donation_readings (member_id, game_date, kind, amount, captured_at, source)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(member_id, game_date, kind) DO UPDATE SET
			amount = excluded.amount, captured_at = excluded.captured_at,
			source = excluded.source, updated_at = CURRENT_TIMESTAMP
		WHERE excluded.captured_at >= donation_readings.captured_at`,
		w.MemberID, w.GameDate, w.Kind, w.Amount, w.CapturedAt, w.Source)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0 && (!had || stored != w.Amount), nil
}

// --- validation -------------------------------------------------------------------------

// inDonationResetWindow reports whether t falls in the ten minutes after the 02:00 UTC
// reset.
func inDonationResetWindow(t time.Time) bool {
	u := t.UTC()
	reset := time.Date(u.Year(), u.Month(), u.Day(), 2, 0, 0, 0, time.UTC)
	return !u.Before(reset) && u.Before(reset.Add(donationResetWindow))
}

// parseDonationCapturedAt turns a client's RFC 3339 captured_at into the instant a half
// was read. Absent means now (the server's clock, never refused). A supplied time gets
// parseCapturedAt's window (10 minutes ahead, 30 days back) and the reset window. The
// error is worded for errors[].
func parseDonationCapturedAt(raw string, now time.Time) (time.Time, error) {
	if raw == "" {
		return now, nil
	}
	if _, err := parseCapturedAt(raw, now); err != nil {
		return time.Time{}, err
	}
	t, _ := time.Parse(time.RFC3339, raw)
	if inDonationResetWindow(t) {
		return time.Time{}, fmt.Errorf("read within ten minutes of the daily reset (02:00 UTC) — read the screen again")
	}
	return t, nil
}

// clampOutOfDonationResetWindow moves an instant the server made up (a web upload's
// "today" half, dated at the upload) past the reset window rather than refusing it: the
// window exists for what a screen showed, not for when an officer clicked.
func clampOutOfDonationResetWindow(t time.Time) time.Time {
	if !inDonationResetWindow(t) {
		return t
	}
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 2, 0, 0, 0, time.UTC).Add(donationResetWindow)
}

// parseDonationDate reads the web upload's "Donation screens taken on" game day:
// absent is today's, and it may be neither after today nor more than 30 days back. The
// error is worded for the page.
func parseDonationDate(raw string, now time.Time) (string, error) {
	today := gametime.DateOf(now)
	if raw == "" {
		return today, nil
	}
	d, err := time.Parse(gametime.DateLayout, raw)
	if err != nil {
		return "", fmt.Errorf("donation date %q is not a YYYY-MM-DD date", raw)
	}
	t, _ := time.Parse(gametime.DateLayout, today)
	switch {
	case d.After(t):
		return "", fmt.Errorf("donation date %s is after today's game date (%s)", raw, today)
	case d.Before(t.AddDate(0, 0, -30)):
		return "", fmt.Errorf("donation date %s is more than 30 days ago", raw)
	}
	return raw, nil
}

// donationUploadInstant is the captured_at a web upload's halves get, since nothing in
// an upload says when a screenshot was taken. Today: the upload instant, moved past the
// reset window. A past day: its earliest accepted instant, D 02:10:00 UTC — so any timed
// reading of that day (the scanner's) beats it, while an equal re-upload still replaces it.
func donationUploadInstant(gameDay string, now time.Time) time.Time {
	if gameDay == gametime.DateOf(now) {
		return clampOutOfDonationResetWindow(now)
	}
	d, _ := time.Parse(gametime.DateLayout, gameDay)
	return time.Date(d.Year(), d.Month(), d.Day(), 2, 0, 0, 0, time.UTC).Add(donationResetWindow)
}

// donationHalfIn is one half on its way in, validated and dated.
type donationHalfIn struct {
	MemberID int
	Name     string // for errors[]
	Category string // donation_daily | donation_weekly
	Kind     string
	Amount   int64
	Captured time.Time
}

// refuseImpossibleDonationPairs drops both halves of any member and day whose today
// exceeds its week when the today half was read no later than the week half — the week
// includes the day, so that pair cannot both be true. (A today read later than the week
// may legitimately exceed it.) It returns the halves kept and one error per refused pair.
func refuseImpossibleDonationPairs(halves []donationHalfIn) ([]donationHalfIn, []string) {
	type key struct {
		member int
		date   string
	}
	today := map[key]donationHalfIn{}
	week := map[key]donationHalfIn{}
	for _, h := range halves {
		k := key{h.MemberID, gametime.DateOf(h.Captured)}
		if h.Kind == donationKindToday {
			today[k] = h
		} else {
			week[k] = h
		}
	}
	refused := map[key]bool{}
	var errs []string
	for k, t := range today {
		w, ok := week[k]
		if ok && t.Amount > w.Amount && !t.Captured.After(w.Captured) {
			refused[k] = true
			errs = append(errs, fmt.Sprintf("%s (donations, %s): today's %d is above the week's %d — both refused",
				t.Name, k.date, t.Amount, w.Amount))
		}
	}
	sort.Strings(errs)
	kept := halves[:0:0]
	for _, h := range halves {
		if !refused[key{h.MemberID, gametime.DateOf(h.Captured)}] {
			kept = append(kept, h)
		}
	}
	return kept, errs
}

// --- the derivation ---------------------------------------------------------------------

// donationHalf is one stored half. CapturedAt is UTC in SQLiteLayout.
type donationHalf struct {
	Amount     int64
	CapturedAt string
}

// donationDayReadings is what is stored for one member on one game day: the two halves a
// screen gave, and the amount an officer entered by hand (donation_day_entries), if any.
type donationDayReadings struct {
	Today, Week *donationHalf
	Entered     *int64
}

// donationDay is one derived day. Note names an incoherent or conflicting pair.
type donationDay struct {
	Amount int64  `json:"amount"`
	Status string `json:"status"` // exact | so_far | blank
	Note   string `json:"note,omitempty"`
	// Entered marks an amount an officer typed or imported: exact, whatever the readings say.
	Entered bool `json:"entered,omitempty"`
}

// donationWeekTotal is the week's total: the latest week half of the week.
type donationWeekTotal struct {
	Amount int64  `json:"amount"`
	Status string `json:"status"` // final | so_far | blank
}

// donationWeek is one member's derived Monday–Sunday week.
type donationWeek struct {
	WeekDate    string
	Days        [7]donationDay
	Total       donationWeekTotal
	WeekOver    bool
	LastReadDay int // the last day index with any half; -1 when none
}

// donationWeekEnd is the instant week weekDate (its game-time Monday) ends: the next
// Monday's 00:00 game time, 02:00 UTC.
func donationWeekEnd(weekDate string) time.Time {
	t, err := time.ParseInLocation(gametime.DateLayout, weekDate, gametime.Loc)
	if err != nil {
		return time.Time{}
	}
	return t.AddDate(0, 0, 7)
}

func parseSQLiteUTC(s string) (time.Time, bool) {
	t, err := time.ParseInLocation(gametime.SQLiteLayout, s, time.UTC)
	return t, err == nil
}

// donationGap words the gap between an incoherent pair's two halves.
func donationGap(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	if d >= time.Hour {
		return fmt.Sprintf("halves %dh apart", int(d.Round(time.Hour)/time.Hour))
	}
	return fmt.Sprintf("halves %d min apart", int(d.Round(time.Minute)/time.Minute))
}

// deriveDonationWeek turns one member's week of halves into day amounts and a total.
//
// byEnd[d] is what the member had given by the end of day d: 0 before Monday, else
// week − today of day d+1's coherent pair (both halves, read within
// donationPairTolerance, week ≥ today). Day d is exact when byEnd[d−1] and byEnd[d] are
// both known, at max(0, byEnd[d] − byEnd[d−1]); else so far at its own today; else blank.
// A conflicting pair (today above week) blanks its day with a note; an incoherent one
// contributes nothing to byEnd and leaves its day so far with a note. Sunday can never be
// exact (Monday's week restarts at 0). The total is the week half of the latest day that
// has one: final when that day is Sunday and the week is over.
func deriveDonationWeek(weekDate string, days [7]donationDayReadings, now time.Time) donationWeek {
	out := donationWeek{WeekDate: weekDate, LastReadDay: -1}
	end := donationWeekEnd(weekDate)
	out.WeekOver = !end.IsZero() && !now.Before(end)

	// byEnd index i+1 holds byEnd[i]; index 0 is byEnd[−1] = 0.
	var byEnd [8]int64
	var known [8]bool
	byEnd[0], known[0] = 0, true
	conflict := [7]bool{}
	notes := [7]string{}
	for d := 0; d < 7; d++ {
		r := days[d]
		if r.Today == nil || r.Week == nil {
			continue
		}
		if r.Week.Amount < r.Today.Amount {
			conflict[d] = true
			notes[d] = "today above week"
			continue
		}
		ta, okA := parseSQLiteUTC(r.Today.CapturedAt)
		tb, okB := parseSQLiteUTC(r.Week.CapturedAt)
		if !okA || !okB {
			continue
		}
		if gap := ta.Sub(tb); gap > donationPairTolerance || gap < -donationPairTolerance {
			notes[d] = donationGap(gap)
			continue
		}
		// Day d's pair says what was given by the end of day d−1.
		if d > 0 {
			byEnd[d], known[d] = r.Week.Amount-r.Today.Amount, true
		}
	}

	for d := 0; d < 7; d++ {
		r := days[d]
		if r.Today != nil || r.Week != nil || r.Entered != nil {
			out.LastReadDay = d
		}
		day := donationDay{Status: donationBlank, Note: notes[d]}
		switch {
		case r.Entered != nil:
			// An officer's amount for the day: exact, and it replaces the derived one. The
			// readings' chain is untouched, so the days around it derive as before.
			day = donationDay{Amount: *r.Entered, Status: donationExact, Entered: true}
		case conflict[d]:
			// blank, with the note
		case d < 6 && known[d] && known[d+1]:
			day.Amount = byEnd[d+1] - byEnd[d]
			if day.Amount < 0 {
				day.Amount = 0
			}
			day.Status = donationExact
		case r.Today != nil:
			day.Amount, day.Status = r.Today.Amount, donationSoFar
		}
		out.Days[d] = day
	}

	out.Total = donationWeekTotal{Status: donationBlank}
	// A week entered by hand on every day totals its days: final once the week is over.
	entered, sum := 0, int64(0)
	for d := 0; d < 7; d++ {
		if days[d].Entered != nil {
			entered++
			sum += *days[d].Entered
		}
	}
	if entered == 7 {
		out.Total = donationWeekTotal{Amount: sum, Status: donationSoFar}
		if out.WeekOver {
			out.Total.Status = donationFinal
		}
		return out
	}
	for d := 6; d >= 0; d-- {
		if w := days[d].Week; w != nil {
			out.Total.Amount = w.Amount
			out.Total.Status = donationSoFar
			if d == 6 && out.WeekOver {
				out.Total.Status = donationFinal
			}
			break
		}
	}
	return out
}

// --- the read ---------------------------------------------------------------------------

// donationWeekMonday is the Monday of the Monday–Sunday week holding a game date.
func donationWeekMonday(gameDate string) string {
	m, err := gametime.WeekMonday(gameDate)
	if err != nil {
		return ""
	}
	return m
}

// addDays steps a YYYY-MM-DD date by n calendar days.
func addDays(date string, n int) string {
	t, err := time.Parse(gametime.DateLayout, date)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, n).Format(gametime.DateLayout)
}

// loadDonationWeeks reads every half between the Monday of from's week and the Sunday of
// to's in one query, and derives each member's weeks: member id → week Monday → week.
// Members with no reading in a week have no entry for it; callers show them blank. It is
// the one reader, so every surface derives alike.
func loadDonationWeeks(q rowQueryer, from, to string, now time.Time) (map[int]map[string]donationWeek, error) {
	return loadDonationWeeksOf(q, 0, from, to, now)
}

// loadDonationWeeksOf is loadDonationWeeks for one member (memberID 0: every member).
func loadDonationWeeksOf(q rowQueryer, memberID int, from, to string, now time.Time) (map[int]map[string]donationWeek, error) {
	first, last := donationWeekMonday(from), addDays(donationWeekMonday(to), 6)
	if first == "" || last == "" {
		return nil, fmt.Errorf("loadDonationWeeks: bad range %q–%q", from, to)
	}
	rows, err := q.Query(`SELECT member_id, game_date, kind, amount, captured_at FROM donation_readings
		WHERE game_date BETWEEN ? AND ? AND (? = 0 OR member_id = ?)`, first, last, memberID, memberID)
	if err != nil {
		return nil, err
	}
	readings := map[int]map[string]*[7]donationDayReadings{}
	for rows.Next() {
		var (
			memberID              int
			gameDate, kind, capAt string
			amount                int64
		)
		if err := rows.Scan(&memberID, &gameDate, &kind, &amount, &capAt); err != nil {
			rows.Close()
			return nil, err
		}
		week := donationWeekMonday(gameDate)
		d, err := daysBetween(week, gameDate)
		if err != nil || d < 0 || d > 6 {
			continue
		}
		if readings[memberID] == nil {
			readings[memberID] = map[string]*[7]donationDayReadings{}
		}
		w := readings[memberID][week]
		if w == nil {
			w = &[7]donationDayReadings{}
			readings[memberID][week] = w
		}
		h := &donationHalf{Amount: amount, CapturedAt: capAt}
		if kind == donationKindToday {
			w[d].Today = h
		} else {
			w[d].Week = h
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	entries, err := q.Query(`SELECT member_id, game_date, amount FROM donation_day_entries
		WHERE game_date BETWEEN ? AND ? AND (? = 0 OR member_id = ?)`, first, last, memberID, memberID)
	if err != nil {
		return nil, err
	}
	for entries.Next() {
		var memberID int
		var gameDate string
		var amount int64
		if err := entries.Scan(&memberID, &gameDate, &amount); err != nil {
			entries.Close()
			return nil, err
		}
		week := donationWeekMonday(gameDate)
		d, err := daysBetween(week, gameDate)
		if err != nil || d < 0 || d > 6 {
			continue
		}
		if readings[memberID] == nil {
			readings[memberID] = map[string]*[7]donationDayReadings{}
		}
		w := readings[memberID][week]
		if w == nil {
			w = &[7]donationDayReadings{}
			readings[memberID][week] = w
		}
		a := amount
		w[d].Entered = &a
	}
	if err := entries.Close(); err != nil {
		return nil, err
	}
	out := make(map[int]map[string]donationWeek, len(readings))
	for memberID, weeks := range readings {
		out[memberID] = make(map[string]donationWeek, len(weeks))
		for week, days := range weeks {
			out[memberID][week] = deriveDonationWeek(week, *days, now)
		}
	}
	return out, nil
}

// daysBetween is the number of calendar days from a to b (YYYY-MM-DD).
func daysBetween(a, b string) (int, error) {
	ta, err := time.Parse(gametime.DateLayout, a)
	if err != nil {
		return 0, err
	}
	tb, err := time.Parse(gametime.DateLayout, b)
	if err != nil {
		return 0, err
	}
	return int(tb.Sub(ta).Hours() / 24), nil
}

// donationWeeksAvailable lists the Mondays of every week holding a reading, newest first.
func donationWeeksAvailable(q rowQueryer) ([]string, error) {
	return donationWeeksAvailableOf(q, 0)
}

// donationWeeksAvailableOf is donationWeeksAvailable for one member (0: every member).
func donationWeeksAvailableOf(q rowQueryer, memberID int) ([]string, error) {
	rows, err := q.Query(`SELECT game_date FROM donation_readings WHERE ? = 0 OR member_id = ?
		UNION SELECT game_date FROM donation_day_entries WHERE ? = 0 OR member_id = ?`, memberID, memberID, memberID, memberID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var weeks []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return nil, err
		}
		if m := donationWeekMonday(d); m != "" && !seen[m] {
			seen[m] = true
			weeks = append(weeks, m)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(weeks)))
	return weeks, rows.Err()
}

// dateSpan names the game dates an upload covered, for an activity row: one date, or
// "first – last".
func dateSpan(dates []string) string {
	if len(dates) == 0 {
		return ""
	}
	s := append([]string(nil), dates...)
	sort.Strings(s)
	if s[0] == s[len(s)-1] {
		return s[0]
	}
	return s[0] + " – " + s[len(s)-1]
}

// --- minimums ---------------------------------------------------------------------------

// donationMinimums are the minimums a week is judged against. Source is "recorded" (a
// donation_week_minimums row at or before the week) or "current_setting" (none: today's
// settings). FlagDays is always today's setting: it is a dial, not history.
type donationMinimums struct {
	Weekly   int    `json:"weekly"`
	Daily    int    `json:"daily"`
	FlagDays int    `json:"flag_days"`
	Source   string `json:"source"`
}

// donationSettings reads today's donation settings.
func donationSettings(q rowQuerier) (weekly, daily, flagDays int, err error) {
	err = q.QueryRow(`SELECT COALESCE(donation_weekly_minimum, 0), COALESCE(donation_daily_minimum, 0),
		COALESCE(donation_flag_days_threshold, 2) FROM settings WHERE id = 1`).Scan(&weekly, &daily, &flagDays)
	if err == sql.ErrNoRows {
		return 0, 0, 2, nil
	}
	return weekly, daily, flagDays, err
}

// donationMinimumsForWeek resolves one week: the row with the greatest week_date <= W,
// else today's settings.
func donationMinimumsForWeek(q rowQuerier, week string) (donationMinimums, error) {
	weekly, daily, flag, err := donationSettings(q)
	if err != nil {
		return donationMinimums{}, err
	}
	m := donationMinimums{FlagDays: flag}
	err = q.QueryRow(`SELECT weekly_minimum, daily_minimum FROM donation_week_minimums WHERE week_date <= ?
		ORDER BY week_date DESC LIMIT 1`, week).Scan(&m.Weekly, &m.Daily)
	switch {
	case err == nil:
		m.Source = vsMinSourceRecorded
	case err == sql.ErrNoRows:
		m.Weekly, m.Daily, m.Source = weekly, daily, vsMinSourceCurrent
	default:
		return m, err
	}
	return m, nil
}

// donationMinimumsForWeeks resolves many weeks in two reads.
func donationMinimumsForWeeks(q rowQueryer, weeks []string) (map[string]donationMinimums, error) {
	weekly, daily, flag, err := donationSettings(q)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT week_date, weekly_minimum, daily_minimum FROM donation_week_minimums ORDER BY week_date`)
	if err != nil {
		return nil, err
	}
	type row struct {
		week          string
		weekly, daily int
	}
	var recorded []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.week, &r.weekly, &r.daily); err != nil {
			rows.Close()
			return nil, err
		}
		recorded = append(recorded, r)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	out := make(map[string]donationMinimums, len(weeks))
	for _, w := range weeks {
		m := donationMinimums{Weekly: weekly, Daily: daily, FlagDays: flag, Source: vsMinSourceCurrent}
		for _, r := range recorded { // ascending: the last one at or before w wins
			if r.week <= w {
				m.Weekly, m.Daily, m.Source = r.weekly, r.daily, vsMinSourceRecorded
			}
		}
		out[w] = m
	}
	return out, nil
}

// recordDonationMinimums records both minimums as of week, replacing that week's row. The
// Settings save calls it for the current game week, in its own transaction.
func recordDonationMinimums(q historyExecer, week string, weekly, daily, userID int) error {
	_, err := q.Exec(`INSERT INTO donation_week_minimums (week_date, weekly_minimum, daily_minimum, set_by, set_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(week_date) DO UPDATE SET weekly_minimum = excluded.weekly_minimum,
			daily_minimum = excluded.daily_minimum, set_by = excluded.set_by, set_at = CURRENT_TIMESTAMP`,
		week, weekly, daily, nullUserID(userID))
	return err
}

// --- the judgement ----------------------------------------------------------------------

// donationJudgement is one member's week judged against its minimums.
//
// The weekly rule judges only a final total (a Sunday reading, the week over): below the
// weekly minimum is a miss; a week read but not on Sunday is "not read to the end", never
// a miss, and a week never read is neither. The daily rule judges exact days, and Sunday once the week is over, on or after
// the member joined: below the daily minimum on FlagDays or more is a miss. A member who
// joined after the week's Monday is not judged on the weekly rule. A minimum of 0 is off.
type donationJudgement struct {
	BelowWeekly    bool `json:"below_weekly"`
	NotReadToEnd   bool `json:"not_read_to_end"`
	JoinedMidWeek  bool `json:"joined_mid_week"`
	DaysBelowDaily int  `json:"days_below_daily"`
	JudgedDays     int  `json:"judged_days"`
	BelowDaily     bool `json:"below_daily"`
}

// judgeDonationWeek judges w; joinedAt is the member's join date (YYYY-MM-DD, "" when
// unknown, which counts as before the week).
func judgeDonationWeek(w donationWeek, mins donationMinimums, joinedAt string) donationJudgement {
	var j donationJudgement
	j.JoinedMidWeek = joinedAt != "" && joinedAt > w.WeekDate
	if mins.Weekly > 0 && w.WeekOver && !j.JoinedMidWeek {
		switch {
		case w.Total.Status == donationFinal:
			j.BelowWeekly = w.Total.Amount < int64(mins.Weekly)
		case w.LastReadDay >= 0:
			// Read, but not on Sunday. A week with no reading at all was simply not
			// read — like a VS week with no row, it says nothing about the member.
			j.NotReadToEnd = true
		}
	}
	for d := 0; d < 7; d++ {
		day := w.Days[d]
		judged := day.Status == donationExact || (d == 6 && w.WeekOver && day.Status == donationSoFar)
		if !judged || (joinedAt != "" && addDays(w.WeekDate, d) < joinedAt) {
			continue
		}
		j.JudgedDays++
		if mins.Daily > 0 && day.Amount < int64(mins.Daily) {
			j.DaysBelowDaily++
		}
	}
	j.BelowDaily = mins.Daily > 0 && mins.FlagDays > 0 && j.DaysBelowDaily >= mins.FlagDays
	return j
}

// donationStrikeTarget is one strike Add Donation Strikes files: one per member and judged
// week, whatever rules the member failed that week (decision 23). Rules names them for the
// banner; Reason is the strike's text.
type donationStrikeTarget struct {
	Week   string   `json:"week"`
	Rules  []string `json:"rules"`
	Reason string   `json:"reason"`
}

// donationStrikeTargets builds a member's targets from its two verdicts: the weekly rule
// on weeklyWeek, the daily rule on dailyWeek. The two weeks coincide on a Monday and a
// Tuesday before a reading, and then both rules make one target.
func donationStrikeTargets(weeklyWeek string, weekly donationJudgement, weeklyTotal int64, weeklyMin int,
	dailyWeek string, daily donationJudgement, dailyMin int) []donationStrikeTarget {
	var out []donationStrikeTarget
	add := func(week, rule, reason string) {
		for i := range out {
			if out[i].Week == week {
				out[i].Rules = append(out[i].Rules, rule)
				out[i].Reason += "; " + reason
				return
			}
		}
		out = append(out, donationStrikeTarget{Week: week, Rules: []string{rule}, Reason: reason})
	}
	if weekly.BelowWeekly {
		add(weeklyWeek, "weekly", fmt.Sprintf("weekly %s < %s (week of %s)",
			groupDigits(weeklyTotal), groupDigits(int64(weeklyMin)), shortWeekName(weeklyWeek)))
	}
	if daily.BelowDaily {
		days := "days"
		if daily.DaysBelowDaily == 1 {
			days = "day"
		}
		add(dailyWeek, "daily", fmt.Sprintf("daily below %s on %d %s (week of %s)",
			groupDigits(int64(dailyMin)), daily.DaysBelowDaily, days, shortWeekName(dailyWeek)))
	}
	return out
}

// groupDigits writes n with thousands separators: 31000 → "31,000".
func groupDigits(n int64) string {
	s := fmt.Sprintf("%d", n)
	neg := n < 0
	if neg {
		s = s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}

// shortWeekName names a week by its Monday: "2026-10-06" → "6 Oct".
func shortWeekName(week string) string {
	t, err := time.Parse(gametime.DateLayout, week)
	if err != nil {
		return week
	}
	return t.Format("2 Jan")
}

// donationHistoryWeek is one week of a member's donation history (the profile tab, the
// report): the derived days and total, the minimums that applied to that week, and the
// verdict against them.
type donationHistoryWeek struct {
	WeekDate string            `json:"week_date"`
	Days     [7]donationDay    `json:"days"`
	Total    donationWeekTotal `json:"total"`
	WeekOver bool              `json:"week_over"`
	Minimums donationMinimums  `json:"minimums"`
	donationJudgement
}

// memberDonationHistory is a member's last `limit` weeks with any reading, newest first,
// each judged against its own week's minimums.
func memberDonationHistory(q rowQueryer, memberID int, joinedAt string, limit int, now time.Time) ([]donationHistoryWeek, error) {
	weeks, err := donationWeeksAvailableOf(q, memberID)
	if err != nil {
		return nil, err
	}
	out := []donationHistoryWeek{}
	if len(weeks) == 0 {
		return out, nil
	}
	if len(weeks) > limit {
		weeks = weeks[:limit]
	}
	derived, err := loadDonationWeeksOf(q, memberID, weeks[len(weeks)-1], weeks[0], now)
	if err != nil {
		return nil, err
	}
	mins, err := donationMinimumsForWeeks(q, weeks)
	if err != nil {
		return nil, err
	}
	for _, week := range weeks {
		w := derived[memberID][week]
		out = append(out, donationHistoryWeek{WeekDate: week, Days: w.Days, Total: w.Total, WeekOver: w.WeekOver,
			Minimums: mins[week], donationJudgement: judgeDonationWeek(w, mins[week], joinedAt)})
	}
	return out, nil
}

// --- entered days -------------------------------------------------------------------------

// donationEntryCell is one day cell from the Donations tab or a CSV: Amount nil clears the
// entry. Was is the entered amount the page loaded (nil: none); a manual save is refused for a
// cell whose stored entry no longer matches it, so an officer never overwrites a change made
// underneath. A CSV import passes CheckWas false: importing is an intent to replace.
type donationEntryCell struct {
	MemberID int    `json:"member_id"`
	GameDate string `json:"game_date"`
	Amount   *int64 `json:"amount"`
	Was      *int64 `json:"was"`
}

type donationEntryConflict struct {
	MemberID int    `json:"member_id"`
	GameDate string `json:"game_date"`
	Stored   *int64 `json:"stored"`
	Wanted   *int64 `json:"wanted"`
}

type donationEntryResult struct {
	Saved     int                     `json:"saved"`
	Cleared   int                     `json:"cleared"`
	Unchanged int                     `json:"unchanged"`
	Conflicts []donationEntryConflict `json:"conflicts"`
	Errors    []string                `json:"errors"`
}

func sameAmount(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// saveDonationEntriesTx writes entered days in one transaction the caller commits. Each cell is
// validated (an active member, a date no later than today's game date, an amount ≥ 0) and
// refused into Errors otherwise; the rest are written. source is manual or csv.
func saveDonationEntriesTx(tx historyExecer, cells []donationEntryCell, source string, checkWas bool, userID int, now time.Time) (donationEntryResult, error) {
	res := donationEntryResult{Conflicts: []donationEntryConflict{}, Errors: []string{}}
	today := gametime.DateOf(now)
	for _, c := range cells {
		if _, err := time.Parse(gametime.DateLayout, c.GameDate); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("member %d: %q is not a YYYY-MM-DD date", c.MemberID, c.GameDate))
			continue
		}
		if c.GameDate > today {
			res.Errors = append(res.Errors, fmt.Sprintf("member %d, %s: after today's game date", c.MemberID, c.GameDate))
			continue
		}
		if c.Amount != nil && *c.Amount < 0 {
			res.Errors = append(res.Errors, fmt.Sprintf("member %d, %s: %d is below 0", c.MemberID, c.GameDate, *c.Amount))
			continue
		}
		var rank string
		if err := tx.QueryRow(`SELECT rank FROM members WHERE id = ?`, c.MemberID).Scan(&rank); err != nil || rank == "EX" {
			if err != nil && err != sql.ErrNoRows {
				return res, err
			}
			res.Errors = append(res.Errors, fmt.Sprintf("member %d: not an active member", c.MemberID))
			continue
		}
		var stored *int64
		var v int64
		switch err := tx.QueryRow(`SELECT amount FROM donation_day_entries WHERE member_id = ? AND game_date = ?`,
			c.MemberID, c.GameDate).Scan(&v); err {
		case nil:
			stored = &v
		case sql.ErrNoRows:
		default:
			return res, err
		}
		if sameAmount(stored, c.Amount) {
			res.Unchanged++
			continue
		}
		if checkWas && !sameAmount(stored, c.Was) {
			res.Conflicts = append(res.Conflicts, donationEntryConflict{MemberID: c.MemberID, GameDate: c.GameDate, Stored: stored, Wanted: c.Amount})
			continue
		}
		if c.Amount == nil {
			if _, err := tx.Exec(`DELETE FROM donation_day_entries WHERE member_id = ? AND game_date = ?`, c.MemberID, c.GameDate); err != nil {
				return res, err
			}
			res.Cleared++
			continue
		}
		if _, err := tx.Exec(`INSERT INTO donation_day_entries (member_id, game_date, amount, source, entered_by)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(member_id, game_date) DO UPDATE SET amount = excluded.amount, source = excluded.source,
				entered_by = excluded.entered_by, updated_at = CURRENT_TIMESTAMP`,
			c.MemberID, c.GameDate, *c.Amount, source, nullUserID(userID)); err != nil {
			return res, err
		}
		res.Saved++
	}
	return res, nil
}
