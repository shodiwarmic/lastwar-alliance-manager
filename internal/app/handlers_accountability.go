// handlers_accountability.go — Member accountability tracker

package app

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"lastwar-alliance/internal/gametime"
)

// VS week selection + day import detection live in helpers.go (gameWeekStart,
// effectiveVSWeek) and vsEvalContext below.

// strikeThresholds holds the configured tag boundaries.
type strikeThresholds struct {
	NeedsImprovement int
	AtRisk           int
}

// loadStrikeThresholds reads configured thresholds from settings.
func loadStrikeThresholds() strikeThresholds { return loadStrikeThresholdsQ(db) }

// loadStrikeThresholdsQ is loadStrikeThresholds on q (the reporting layer's form).
func loadStrikeThresholdsQ(q rowQuerier) strikeThresholds {
	t := strikeThresholds{NeedsImprovement: 1, AtRisk: 3}
	q.QueryRow(`SELECT COALESCE(strike_needs_improvement_threshold, 1),
		COALESCE(strike_at_risk_threshold, 3) FROM settings WHERE id = 1`).
		Scan(&t.NeedsImprovement, &t.AtRisk)
	return t
}

// memberTag derives the accountability tag from active strike count and thresholds.
func memberTag(activeStrikes int, t strikeThresholds) string {
	switch {
	case activeStrikes >= t.AtRisk:
		return "At Risk"
	case activeStrikes >= t.NeedsImprovement:
		return "Needs Improvement"
	default:
		return "Reliable"
	}
}

// getVSAccountabilitySettings returns (today's daily minimum, flag-days threshold) in one
// read. A week's own minimum is vsMinimumForWeek's.
func getVSAccountabilitySettings() (min, flagDays int) { return getVSAccountabilitySettingsQ(db) }

// getVSAccountabilitySettingsQ is getVSAccountabilitySettings on q.
func getVSAccountabilitySettingsQ(q rowQuerier) (min, flagDays int) {
	if err := q.QueryRow(`SELECT COALESCE(vs_minimum_points, 2500000),
		COALESCE(vs_flag_days_threshold, 2) FROM settings WHERE id = 1`).Scan(&min, &flagDays); err != nil {
		return 2500000, 2
	}
	return min, flagDays
}

// vsEvalContext resolves the single VS week every flag/stat surface should evaluate,
// plus which of its days have actually been imported. weekDate/completed come from
// effectiveVSWeek (Monday → fallback to the previous complete week). dayImported[i] is
// true when any member has a non-zero score for day i that week; importedCompleted is
// how many completed days are imported; fallbackActive is true on the Monday fallback.
// On query error it logs and returns safe defaults (nothing imported → callers flag nobody).
// vsDayImportMask returns, for an arbitrary week_date, which of its six days have been
// imported (dayImported[i] true when any member has a non-zero score for day i that week).
// An all-zero day reads as "not imported" — never happens for a real VS day. On query error
// it logs and returns the zero mask (callers treat as nothing imported).
// Parameterized so the VS League participation view can evaluate any historical week, not
// just the current one (vsEvalContext, which is current-week-only, delegates here).
func vsDayImportMask(weekDate string) [6]bool {
	dayImported, err := vsDayImportMaskQ(db, weekDate)
	if err != nil {
		slog.Error("vsDayImportMask: day-import query failed", "error", err, "week", weekDate)
	}
	return dayImported
}

// vsDayImportMaskQ is vsDayImportMask on q, returning the error (the zero mask with it).
func vsDayImportMaskQ(q rowQuerier, weekDate string) ([6]bool, error) {
	var dayImported [6]bool
	var m [6]int
	if err := q.QueryRow(`SELECT
		COALESCE(MAX(monday),0), COALESCE(MAX(tuesday),0), COALESCE(MAX(wednesday),0),
		COALESCE(MAX(thursday),0), COALESCE(MAX(friday),0), COALESCE(MAX(saturday),0)
		FROM vs_points WHERE week_date = ?`, weekDate).
		Scan(&m[0], &m[1], &m[2], &m[3], &m[4], &m[5]); err != nil {
		return dayImported, err
	}
	for i := 0; i < 6; i++ {
		dayImported[i] = m[i] > 0
	}
	return dayImported, nil
}

func vsEvalContext() (weekDate string, completed int, dayImported [6]bool, importedCompleted int, fallbackActive bool) {
	weekDate, completed, dayImported, importedCompleted, fallbackActive, err := vsEvalContextAt(db, time.Now())
	if err != nil {
		slog.Error("vsEvalContext: day-import query failed", "error", err, "week", weekDate)
	}
	return weekDate, completed, dayImported, importedCompleted, fallbackActive
}

// vsEvalContextAt is vsEvalContext for the moment now, on q (the reporting layer's form).
func vsEvalContextAt(q rowQuerier, now time.Time) (weekDate string, completed int, dayImported [6]bool, importedCompleted int, fallbackActive bool, err error) {
	weekDate, completed = effectiveVSWeekAt(now)
	fallbackActive = gameWeekdayAt(now) == 0
	dayImported, err = vsDayImportMaskQ(q, weekDate)
	for i := 0; i < 6; i++ {
		if i < completed && dayImported[i] {
			importedCompleted++
		}
	}
	return weekDate, completed, dayImported, importedCompleted, fallbackActive, err
}

// vsDayStatus is one VS day's verdict for one member.
type vsDayStatus int

const (
	vsDaySkipped vsDayStatus = iota // not completed, not imported, or before the member joined
	vsDayMet                        // evaluated, at or above the minimum
	vsDayBelow                      // evaluated, below the minimum
	vsDayNoScore                    // would be evaluated, but the member has no row that week
)

// vsDayStatuses judges each of a week's six days for one member: a day is evaluated
// only when it is completed (i < completed), imported, and on or after the member's
// join date. Rank is not its business — callers choose the members. vsDaysBelow and
// the VS-misses report (reports_vs.go) both read it, so the two cannot drift.
//
// hasRow is whether the member has a vs_points row for the week at all. Without one,
// nothing was read for them — an import writes a row for every member it matched, a
// real 0 included (the results screens show zero scorers) — so their days are
// vsDayNoScore, never a miss at 0 (owner, 2026-10-15). A day inside a row that was
// never read still reads as the column's 0: the schema cannot say "blank".
func vsDayStatuses(scores [6]int, hasRow bool, joinedAt, weekDate string, completed int, dayImported [6]bool, vsMin int) [6]vsDayStatus {
	var out [6]vsDayStatus
	for i := 0; i < completed && i < 6; i++ {
		if !dayImported[i] {
			continue // day not imported yet
		}
		if joinedAt != "" && dayDate(weekDate, i) < joinedAt {
			continue // before the member joined
		}
		if !hasRow {
			out[i] = vsDayNoScore
		} else if scores[i] < vsMin {
			out[i] = vsDayBelow
		} else {
			out[i] = vsDayMet
		}
	}
	return out
}

// vsDaysBelow counts how many completed, imported, post-join days the member scored
// below the daily minimum; none without a row. Mirrors the JS countDaysBelowMinimum.
func vsDaysBelow(scores [6]int, hasRow bool, joinedAt, weekDate string, completed int, dayImported [6]bool, vsMin int) int {
	n := 0
	for _, st := range vsDayStatuses(scores, hasRow, joinedAt, weekDate, completed, dayImported, vsMin) {
		if st == vsDayBelow {
			n++
		}
	}
	return n
}

// --- Page handlers ---

func handleAccountability(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "Accountability - Alliance Manager", "accountability")
	if !data.IsAuthenticated {
		http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
		return
	}
	if !data.Permissions.ViewAccountability {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	// The evaluated weeks and their notes come with the members API's context, so they
	// cannot go stale while the page is open.
	renderTemplate(w, r, "accountability.html", data)
}

// handleAccountabilityReport: the weekly report moved to the Reports page (Project 19);
// the old address stays as a permanent redirect for bookmarks.
func handleAccountabilityReport(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/reports#weekly", http.StatusMovedPermanently)
}

func handleAccountabilityProfile(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "Member Profile - Alliance Manager", "accountability")
	if !data.IsAuthenticated {
		http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
		return
	}
	if !data.Permissions.ViewAccountability {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	data.MemberID = id
	renderTemplate(w, r, "accountability_profile.html", data)
}

// --- API: member list with accountability stats ---

type AccountabilityMember struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	Rank           string `json:"rank"`
	ActiveStrikes  int    `json:"active_strikes"`
	Tag            string `json:"tag"`
	VSTotal        int    `json:"vs_total"`
	BelowThreshold bool   `json:"below_threshold"`
	// Donations (Project 20). The total is the daily rule's week (context.donation_daily_week),
	// which the column names; below_weekly is about context.donation_weekly_week, the last
	// complete week. Status is final | so_far | blank.
	DonationTotal       int64  `json:"donation_total"`
	DonationTotalStatus string `json:"donation_total_status"`
	DonationBelowWeekly bool   `json:"donation_below_weekly"`
	DonationBelowDaily  bool   `json:"donation_below_daily"`
	DonationDaysBelow   int    `json:"donation_days_below"`
	// DonationStrikes are the strikes Add Donation Strikes files for this member: one
	// per judged week, dated by the server (private-docs 266).
	DonationStrikes []donationStrikeTarget `json:"donation_strikes"`
}

// accountabilityContext is what the members' verdicts were judged on. It travels with
// them, so a page left open across an upload or a reset can never pair fresh flags with
// stale weeks, and strikes take their week from here, never from the browser
// (private-docs 266).
type accountabilityContext struct {
	VSWeek              string `json:"vs_week"`
	VSCompleted         int    `json:"vs_completed"`
	VSImportedCompleted int    `json:"vs_imported_completed"`
	VSFallback          bool   `json:"vs_fallback"`
	VSMinimum           int    `json:"vs_minimum"`
	VSMinimumSource     string `json:"vs_minimum_source"`
	// The weekly donation rule judges the last complete Monday–Sunday week; the daily rule
	// the current week, or the last one while the current week has no judged day yet.
	DonationWeeklyWeek     string           `json:"donation_weekly_week"`
	DonationDailyWeek      string           `json:"donation_daily_week"`
	DonationWeeklyMinimums donationMinimums `json:"donation_weekly_minimums"`
	DonationDailyMinimums  donationMinimums `json:"donation_daily_minimums"`
	// DonationJudgedDays is the most days any member has judged in the daily week: 0 means
	// no exact day yet, and the page says why nobody is flagged.
	DonationJudgedDays int `json:"donation_judged_days"`
}

type accountabilityMembersResponse struct {
	Context accountabilityContext  `json:"context"`
	Members []AccountabilityMember `json:"members"`
}

// donationEvalWeeks are the two weeks the Accountability page judges donations on at now:
// the last complete Monday–Sunday week and the current one.
func donationEvalWeeks(now time.Time) (lastComplete, current string) {
	t, _ := time.Parse(gametime.DateLayout, gametime.DateOf(now))
	monday := gametime.MondayOf(t)
	return monday.AddDate(0, 0, -7).Format(gametime.DateLayout), monday.Format(gametime.DateLayout)
}

// loadAccountabilityMembers judges every active member at now, on q. Every read the
// verdicts need is made before the member cursor opens.
func loadAccountabilityMembers(q rowQueryer, now time.Time) (accountabilityMembersResponse, error) {
	var out accountabilityMembersResponse
	_, vsFlagDays := getVSAccountabilitySettingsQ(q)
	weekDate, completed, dayImported, importedCompleted, fallback, err := vsEvalContextAt(q, now)
	if err != nil {
		return out, err
	}
	thresholds := loadStrikeThresholdsQ(q)
	weekMin, err := vsMinimumForWeek(q, weekDate)
	if err != nil {
		return out, err
	}
	lastWeek, thisWeek := donationEvalWeeks(now)
	donWeeks, err := loadDonationWeeks(q, lastWeek, thisWeek, now)
	if err != nil {
		return out, err
	}
	donMins, err := donationMinimumsForWeeks(q, []string{lastWeek, thisWeek})
	if err != nil {
		return out, err
	}
	out.Context = accountabilityContext{
		VSWeek: weekDate, VSCompleted: completed, VSImportedCompleted: importedCompleted,
		VSFallback: fallback, VSMinimum: weekMin.Points, VSMinimumSource: weekMin.Source,
		DonationWeeklyWeek: lastWeek, DonationWeeklyMinimums: donMins[lastWeek],
	}

	rows, err := q.Query(`
		SELECT
			m.id, m.name, m.rank, COALESCE(m.joined_at, ''),
			COALESCE(SUM(CASE WHEN s.status='active' THEN 1 ELSE 0 END), 0) AS active_strikes,
			COALESCE(vp.monday+vp.tuesday+vp.wednesday+vp.thursday+vp.friday+vp.saturday, 0) AS vs_total,
			COALESCE(vp.monday,0), COALESCE(vp.tuesday,0), COALESCE(vp.wednesday,0),
			COALESCE(vp.thursday,0), COALESCE(vp.friday,0), COALESCE(vp.saturday,0),
			MAX(vp.id) IS NOT NULL AS has_row
		FROM members m
		LEFT JOIN accountability_strikes s ON s.member_id = m.id
		LEFT JOIN vs_points vp ON vp.member_id = m.id AND vp.week_date = ?
		WHERE m.rank != 'EX'
		GROUP BY m.id
		ORDER BY active_strikes DESC, m.name ASC
	`, weekDate)
	if err != nil {
		return out, err
	}
	members := []AccountabilityMember{}
	var joined []string
	for rows.Next() {
		var m AccountabilityMember
		var joinedAt string
		var scores [6]int
		var hasRow bool
		if err := rows.Scan(&m.ID, &m.Name, &m.Rank, &joinedAt, &m.ActiveStrikes, &m.VSTotal,
			&scores[0], &scores[1], &scores[2], &scores[3], &scores[4], &scores[5], &hasRow); err != nil {
			rows.Close()
			return out, err
		}
		m.Tag = memberTag(m.ActiveStrikes, thresholds)
		// Below the daily minimum on N+ completed, imported, post-join days. When no
		// completed day is imported yet, importedCompleted==0 → nobody is flagged.
		if importedCompleted > 0 {
			m.BelowThreshold = vsDaysBelow(scores, hasRow, joinedAt, weekDate, completed, dayImported, weekMin.Points) >= vsFlagDays
		}
		members = append(members, m)
		joined = append(joined, joinedAt)
	}
	if err := rows.Close(); err != nil {
		return out, err
	}

	// Donations: the week a member has no reading for derives blank.
	weekOf := func(id int, week string) donationWeek {
		if w, ok := donWeeks[id][week]; ok {
			return w
		}
		return deriveDonationWeek(week, [7]donationDayReadings{}, now)
	}
	// The daily rule's week: the current one once anybody has a judged day in it.
	dailyWeek := lastWeek
	for i, m := range members {
		if judgeDonationWeek(weekOf(m.ID, thisWeek), donMins[thisWeek], joined[i]).JudgedDays > 0 {
			dailyWeek = thisWeek
			break
		}
	}
	out.Context.DonationDailyWeek = dailyWeek
	out.Context.DonationDailyMinimums = donMins[dailyWeek]
	for i := range members {
		m := &members[i]
		weekly := weekOf(m.ID, lastWeek)
		daily := weekOf(m.ID, dailyWeek)
		jw := judgeDonationWeek(weekly, donMins[lastWeek], joined[i])
		jd := judgeDonationWeek(daily, donMins[dailyWeek], joined[i])
		m.DonationBelowWeekly = jw.BelowWeekly
		m.DonationBelowDaily, m.DonationDaysBelow = jd.BelowDaily, jd.DaysBelowDaily
		m.DonationStrikes = donationStrikeTargets(lastWeek, jw, weekly.Total.Amount, donMins[lastWeek].Weekly,
			dailyWeek, jd, donMins[dailyWeek].Daily)
		if m.DonationStrikes == nil {
			m.DonationStrikes = []donationStrikeTarget{}
		}
		m.DonationTotal, m.DonationTotalStatus = daily.Total.Amount, daily.Total.Status
		if jd.JudgedDays > out.Context.DonationJudgedDays {
			out.Context.DonationJudgedDays = jd.JudgedDays
		}
	}
	out.Members = members
	return out, nil
}

func handleAccountabilityMembers(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "", "")
	if !data.IsAuthenticated || !data.Permissions.ViewAccountability {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	// One read transaction, so the verdicts and the weeks they were judged on are one
	// snapshot.
	tx, err := db.Begin()
	if err != nil {
		dbError(w, "handleAccountabilityMembers: begin", err)
		return
	}
	defer tx.Rollback()
	resp, err := loadAccountabilityMembers(tx, time.Now())
	if err != nil {
		dbError(w, "handleAccountabilityMembers", err)
		return
	}
	writeJSON(w, resp)
}

// --- API: single member profile data ---

type AccountabilityStrike struct {
	ID            int    `json:"id"`
	StrikeType    string `json:"strike_type"`
	Reason        string `json:"reason"`
	RefDate       string `json:"ref_date"`
	Status        string `json:"status"`
	ExcusedBy     string `json:"excused_by"`
	ExcusedReason string `json:"excused_reason"`
	CreatedBy     string `json:"created_by"`
	CreatedAt     string `json:"created_at"`
}

type TrainLogRecord struct {
	ID        int    `json:"id"`
	Date      string `json:"date"`
	TrainType string `json:"train_type"`
	ShowedUp  bool   `json:"showed_up"`
	CreatedAt string `json:"created_at"`
}

type VSWeekRecord struct {
	WeekDate string `json:"week_date"`
	Total    int    `json:"total"`
}

type MemberProfileData struct {
	ID            int                    `json:"id"`
	Name          string                 `json:"name"`
	Rank          string                 `json:"rank"`
	Notes         string                 `json:"notes"`
	ActiveStrikes int                    `json:"active_strikes"`
	Tag           string                 `json:"tag"`
	Strikes       []AccountabilityStrike `json:"strikes"`
	VSHistory     []VSWeekRecord         `json:"vs_history"`
	TrainHistory  []TrainLogRecord       `json:"train_history"`
	// DonationHistory: the last eight weeks with any reading, newest first (Project 20).
	DonationHistory []donationHistoryWeek `json:"donation_history"`
}

func handleAccountabilityMemberProfile(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "", "")
	if !data.IsAuthenticated || !data.Permissions.ViewAccountability {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	var profile MemberProfileData
	profile.ID = id

	// Member base info
	var joinedAt string
	if err := db.QueryRow(`SELECT name, rank, COALESCE(notes,''), COALESCE(joined_at, '') FROM members WHERE id = ?`, id).
		Scan(&profile.Name, &profile.Rank, &profile.Notes, &joinedAt); err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	// Strikes
	thresholds := loadStrikeThresholds()
	strikeRows, err := db.Query(`
		SELECT s.id, s.strike_type, s.reason, COALESCE(s.ref_date,''), s.status,
		       COALESCE(eu.username,''), s.excused_reason,
		       COALESCE(cu.username,''), s.created_at
		FROM accountability_strikes s
		LEFT JOIN users eu ON eu.id = s.excused_by
		LEFT JOIN users cu ON cu.id = s.created_by
		WHERE s.member_id = ?
		ORDER BY s.created_at DESC
	`, id)
	if err != nil {
		slog.Error("handleAccountabilityMemberProfile: strikes query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer strikeRows.Close()
	profile.Strikes = []AccountabilityStrike{}
	for strikeRows.Next() {
		var s AccountabilityStrike
		if err := strikeRows.Scan(&s.ID, &s.StrikeType, &s.Reason, &s.RefDate, &s.Status,
			&s.ExcusedBy, &s.ExcusedReason, &s.CreatedBy, &s.CreatedAt); err != nil {
			slog.Error("handleAccountabilityMemberProfile: strike scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if s.Status == "active" {
			profile.ActiveStrikes++
		}
		profile.Strikes = append(profile.Strikes, s)
	}
	profile.Tag = memberTag(profile.ActiveStrikes, thresholds)

	// VS history — last 8 weeks
	vsRows, err := db.Query(`
		SELECT week_date,
		       COALESCE(monday,0)+COALESCE(tuesday,0)+COALESCE(wednesday,0)+
		       COALESCE(thursday,0)+COALESCE(friday,0)+COALESCE(saturday,0)
		FROM vs_points
		WHERE member_id = ?
		ORDER BY week_date DESC
		LIMIT 8
	`, id)
	if err != nil {
		slog.Error("handleAccountabilityMemberProfile: vs query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer vsRows.Close()
	profile.VSHistory = []VSWeekRecord{}
	for vsRows.Next() {
		var v VSWeekRecord
		if err := vsRows.Scan(&v.WeekDate, &v.Total); err != nil {
			slog.Error("handleAccountabilityMemberProfile: vs scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		profile.VSHistory = append(profile.VSHistory, v)
	}

	// Train history — last 10 entries as conductor
	trainRows, err := db.Query(`
		SELECT id, date, train_type, showed_up, created_at
		FROM train_logs
		WHERE conductor_id = ?
		ORDER BY date DESC
		LIMIT 10
	`, id)
	if err != nil {
		slog.Error("handleAccountabilityMemberProfile: train query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer trainRows.Close()
	profile.TrainHistory = []TrainLogRecord{}
	for trainRows.Next() {
		var t TrainLogRecord
		var showedUpInt int
		if err := trainRows.Scan(&t.ID, &t.Date, &t.TrainType, &showedUpInt, &t.CreatedAt); err != nil {
			slog.Error("handleAccountabilityMemberProfile: train scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		t.ShowedUp = showedUpInt == 1
		profile.TrainHistory = append(profile.TrainHistory, t)
	}
	trainRows.Close()

	// Donations — the last eight weeks with any reading, each against its own minimums.
	profile.DonationHistory, err = memberDonationHistory(db, id, joinedAt, 8, time.Now())
	if err != nil {
		slog.Error("handleAccountabilityMemberProfile: donation history failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profile)
}

// --- API: dashboard summary ---

type AccountabilitySummary struct {
	AtRisk           int                    `json:"at_risk"`
	NeedsImprovement int                    `json:"needs_improvement"`
	Reliable         int                    `json:"reliable"`
	BelowVS          int                    `json:"below_vs"`
	BelowDonation    int                    `json:"below_donation"`
	TopAtRisk        []AccountabilityMember `json:"top_at_risk"`
}

func handleAccountabilitySummary(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "", "")
	if !data.IsAuthenticated || !data.Permissions.ViewAccountability {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		dbError(w, "handleAccountabilitySummary: begin", err)
		return
	}
	defer tx.Rollback()
	resp, err := loadAccountabilityMembers(tx, time.Now())
	if err != nil {
		dbError(w, "handleAccountabilitySummary", err)
		return
	}

	summary := AccountabilitySummary{TopAtRisk: []AccountabilityMember{}}
	for _, m := range resp.Members { // ordered by active strikes, most first
		switch m.Tag {
		case "At Risk":
			summary.AtRisk++
			if len(summary.TopAtRisk) < 3 {
				summary.TopAtRisk = append(summary.TopAtRisk, m)
			}
		case "Needs Improvement":
			summary.NeedsImprovement++
		default:
			summary.Reliable++
		}
		if m.BelowThreshold {
			summary.BelowVS++
		}
		if m.DonationBelowWeekly || m.DonationBelowDaily {
			summary.BelowDonation++
		}
	}
	writeJSON(w, summary)
}

// --- API: strike CRUD ---

// perWeekStrikeTypes are the categories a member gets at most one of per week (ref_date
// is the week's Monday): the VS minimum, and the donation minimums (both rules, one strike).
var perWeekStrikeTypes = map[string]bool{"vs_below_threshold": true, "donation_below_minimum": true}

func handleStrikeCreate(w http.ResponseWriter, r *http.Request) {
	actor := getAuthUser(r)
	userID, username := actor.ID, actor.Username

	var body struct {
		MemberID   int    `json:"member_id"`
		StrikeType string `json:"strike_type"`
		Reason     string `json:"reason"`
		RefDate    string `json:"ref_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.MemberID == 0 || body.StrikeType == "" {
		http.Error(w, "member_id and strike_type are required", http.StatusBadRequest)
		return
	}

	var memberName string
	if err := db.QueryRow("SELECT name FROM members WHERE id = ?", body.MemberID).Scan(&memberName); err != nil {
		http.Error(w, "Member not found", http.StatusBadRequest)
		return
	}

	// Categories are a managed list (strike_types): a strike may only carry the key
	// of an active one. Typing a new category into the strike form is gone.
	active, err := strikeTypeIsActive(db, body.StrikeType)
	if err != nil {
		slog.Error("handleStrikeCreate: category lookup failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if !active {
		http.Error(w, "Unknown or inactive strike category", http.StatusBadRequest)
		return
	}

	// One strike per member and week for the per-week categories. The check and the
	// insert share one transaction — with one connection, that is the serialisation point,
	// so two officers clicking at once cannot file one week twice.
	tx, err := db.Begin()
	if err != nil {
		dbError(w, "handleStrikeCreate: begin", err)
		return
	}
	defer tx.Rollback()
	if perWeekStrikeTypes[body.StrikeType] && body.RefDate != "" {
		var existing int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM accountability_strikes
			WHERE member_id = ? AND strike_type = ? AND ref_date = ?`,
			body.MemberID, body.StrikeType, body.RefDate).Scan(&existing); err != nil {
			dbError(w, "handleStrikeCreate: duplicate check", err)
			return
		}
		if existing > 0 {
			http.Error(w, "A strike of this category already exists for this member and week", http.StatusConflict)
			return
		}
	}

	res, err := tx.Exec(`
		INSERT INTO accountability_strikes (member_id, strike_type, reason, ref_date, created_by)
		VALUES (?, ?, ?, NULLIF(?, ''), ?)
	`, body.MemberID, body.StrikeType, body.Reason, body.RefDate, userID)
	if err != nil {
		slog.Error("handleStrikeCreate: insert failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		dbError(w, "handleStrikeCreate: commit", err)
		return
	}

	logActivity(userID, username, "created", "accountability_strike", memberName, false,
		"type: "+body.StrikeType+"; reason: "+body.Reason)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func handleStrikeUpdate(w http.ResponseWriter, r *http.Request) {
	actor := getAuthUser(r)
	userID, username := actor.ID, actor.Username

	strikeID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	var body struct {
		Status        string `json:"status"`
		ExcusedReason string `json:"excused_reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.Status != "excused" {
		http.Error(w, "Only 'excused' status is valid", http.StatusBadRequest)
		return
	}

	// Fetch old status and member name for logging
	var oldStatus, memberName string
	var memberID int
	if err := db.QueryRow(`
		SELECT s.status, m.name, m.id FROM accountability_strikes s
		JOIN members m ON m.id = s.member_id
		WHERE s.id = ?
	`, strikeID).Scan(&oldStatus, &memberName, &memberID); err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	_, err = db.Exec(`
		UPDATE accountability_strikes
		SET status = ?, excused_by = ?, excused_reason = ?
		WHERE id = ?
	`, body.Status, userID, body.ExcusedReason, strikeID)
	if err != nil {
		slog.Error("handleStrikeUpdate: update failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var changes []string
	if oldStatus != body.Status {
		changes = append(changes, "status: "+oldStatus+" → "+body.Status)
	}
	if body.ExcusedReason != "" {
		changes = append(changes, "excused_reason: "+body.ExcusedReason)
	}
	logActivity(userID, username, "updated", "accountability_strike", memberName, false, strings.Join(changes, "; "))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Updated"})
}

func handleStrikeDelete(w http.ResponseWriter, r *http.Request) {
	actor := getAuthUser(r)
	userID, username := actor.ID, actor.Username

	strikeID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	var memberName, strikeType string
	if err := db.QueryRow(`
		SELECT m.name, s.strike_type FROM accountability_strikes s
		JOIN members m ON m.id = s.member_id
		WHERE s.id = ?
	`, strikeID).Scan(&memberName, &strikeType); err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	if _, err := db.Exec("DELETE FROM accountability_strikes WHERE id = ?", strikeID); err != nil {
		slog.Error("handleStrikeDelete: delete failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	logActivity(userID, username, "deleted", "accountability_strike", memberName, false, "type: "+strikeType)

	w.WriteHeader(http.StatusNoContent)
}

// --- API: all strikes (for the Strikes tab) ---

type StrikeWithMember struct {
	ID            int    `json:"id"`
	MemberID      int    `json:"member_id"`
	MemberName    string `json:"member_name"`
	MemberRank    string `json:"member_rank"`
	StrikeType    string `json:"strike_type"`
	Reason        string `json:"reason"`
	RefDate       string `json:"ref_date"`
	Status        string `json:"status"`
	ExcusedBy     string `json:"excused_by"`
	ExcusedReason string `json:"excused_reason"`
	CreatedBy     string `json:"created_by"`
	CreatedAt     string `json:"created_at"`
}

func handleAllStrikes(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "", "")
	if !data.IsAuthenticated || !data.Permissions.ViewAccountability {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	statusFilter := r.URL.Query().Get("status") // "active", "excused", or ""

	query := `
		SELECT s.id, s.member_id, m.name, m.rank,
		       s.strike_type, s.reason, COALESCE(s.ref_date,''), s.status,
		       COALESCE(eu.username,''), s.excused_reason,
		       COALESCE(cu.username,''), s.created_at
		FROM accountability_strikes s
		JOIN members m ON m.id = s.member_id
		LEFT JOIN users eu ON eu.id = s.excused_by
		LEFT JOIN users cu ON cu.id = s.created_by
	`
	args := []any{}
	if statusFilter == "active" || statusFilter == "excused" {
		query += " WHERE s.status = ?"
		args = append(args, statusFilter)
	}
	query += " ORDER BY s.created_at DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		slog.Error("handleAllStrikes: query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	strikes := []StrikeWithMember{}
	for rows.Next() {
		var s StrikeWithMember
		if err := rows.Scan(&s.ID, &s.MemberID, &s.MemberName, &s.MemberRank,
			&s.StrikeType, &s.Reason, &s.RefDate, &s.Status,
			&s.ExcusedBy, &s.ExcusedReason, &s.CreatedBy, &s.CreatedAt); err != nil {
			slog.Error("handleAllStrikes: scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		strikes = append(strikes, s)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(strikes)
}
