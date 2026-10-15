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
)

// vsDayStatuses judges each of a week's six days for one member: a day is evaluated
// only when it is completed (i < completed), imported, and on or after the member's
// join date. Rank is not its business — callers choose the members. vsDaysBelow and
// the VS-misses report (reports_vs.go) both read it, so the two cannot drift.
func vsDayStatuses(scores [6]int, joinedAt, weekDate string, completed int, dayImported [6]bool, vsMin int) [6]vsDayStatus {
	var out [6]vsDayStatus
	for i := 0; i < completed && i < 6; i++ {
		if !dayImported[i] {
			continue // day not imported yet
		}
		if joinedAt != "" && dayDate(weekDate, i) < joinedAt {
			continue // before the member joined
		}
		if scores[i] < vsMin {
			out[i] = vsDayBelow
		} else {
			out[i] = vsDayMet
		}
	}
	return out
}

// vsDaysBelow counts how many completed, imported, post-join days the member scored
// below the daily minimum. Mirrors the JS countDaysBelowMinimum.
func vsDaysBelow(scores [6]int, joinedAt, weekDate string, completed int, dayImported [6]bool, vsMin int) int {
	n := 0
	for _, st := range vsDayStatuses(scores, joinedAt, weekDate, completed, dayImported, vsMin) {
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
	// Evaluated-week context for the page-config notes (matches the members API).
	weekDate, completed, _, importedCompleted, fallbackActive := vsEvalContext()
	data.VSCompletedDays = completed
	data.VSImportedCompleted = importedCompleted
	data.VSFallbackActive = fallbackActive
	if m, err := vsMinimumForWeek(db, weekDate); err == nil {
		data.VSMinimumPoints, data.VSMinimumSource = m.Points, m.Source
	}
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
}

func handleAccountabilityMembers(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "", "")
	if !data.IsAuthenticated || !data.Permissions.ViewAccountability {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	_, vsFlagDays := getVSAccountabilitySettings()
	weekDate, completed, dayImported, importedCompleted, _ := vsEvalContext()
	thresholds := loadStrikeThresholds()
	// The evaluated week's own minimum, read before the rows cursor opens.
	weekMin, err := vsMinimumForWeek(db, weekDate)
	if err != nil {
		slog.Error("VS minimum for week failed", "error", err, "week", weekDate)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	vsMin := weekMin.Points

	rows, err := db.Query(`
		SELECT
			m.id, m.name, m.rank, COALESCE(m.joined_at, ''),
			COALESCE(SUM(CASE WHEN s.status='active' THEN 1 ELSE 0 END), 0) AS active_strikes,
			COALESCE(vp.monday+vp.tuesday+vp.wednesday+vp.thursday+vp.friday+vp.saturday, 0) AS vs_total,
			COALESCE(vp.monday,0), COALESCE(vp.tuesday,0), COALESCE(vp.wednesday,0),
			COALESCE(vp.thursday,0), COALESCE(vp.friday,0), COALESCE(vp.saturday,0)
		FROM members m
		LEFT JOIN accountability_strikes s ON s.member_id = m.id
		LEFT JOIN vs_points vp ON vp.member_id = m.id AND vp.week_date = ?
		WHERE m.rank != 'EX'
		GROUP BY m.id
		ORDER BY active_strikes DESC, m.name ASC
	`, weekDate)
	if err != nil {
		slog.Error("handleAccountabilityMembers: query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	members := []AccountabilityMember{}
	for rows.Next() {
		var m AccountabilityMember
		var joinedAt string
		var scores [6]int
		if err := rows.Scan(&m.ID, &m.Name, &m.Rank, &joinedAt, &m.ActiveStrikes, &m.VSTotal,
			&scores[0], &scores[1], &scores[2], &scores[3], &scores[4], &scores[5]); err != nil {
			slog.Error("handleAccountabilityMembers: scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		m.Tag = memberTag(m.ActiveStrikes, thresholds)
		// Below the daily minimum on N+ completed, imported, post-join days. When no
		// completed day is imported yet, importedCompleted==0 → nobody is flagged.
		if importedCompleted > 0 {
			m.BelowThreshold = vsDaysBelow(scores, joinedAt, weekDate, completed, dayImported, vsMin) >= vsFlagDays
		}
		members = append(members, m)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(members)
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
	if err := db.QueryRow(`SELECT name, rank, COALESCE(notes,'') FROM members WHERE id = ?`, id).
		Scan(&profile.Name, &profile.Rank, &profile.Notes); err != nil {
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profile)
}

// --- API: dashboard summary ---

type AccountabilitySummary struct {
	AtRisk           int                    `json:"at_risk"`
	NeedsImprovement int                    `json:"needs_improvement"`
	Reliable         int                    `json:"reliable"`
	BelowVS          int                    `json:"below_vs"`
	TopAtRisk        []AccountabilityMember `json:"top_at_risk"`
}

func handleAccountabilitySummary(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "", "")
	if !data.IsAuthenticated || !data.Permissions.ViewAccountability {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	_, vsFlagDays := getVSAccountabilitySettings()
	weekDate, completed, dayImported, importedCompleted, _ := vsEvalContext()
	thresholds := loadStrikeThresholds()
	// The evaluated week's own minimum, read before the rows cursor opens.
	weekMin, err := vsMinimumForWeek(db, weekDate)
	if err != nil {
		slog.Error("VS minimum for week failed", "error", err, "week", weekDate)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	vsMin := weekMin.Points

	rows, err := db.Query(`
		SELECT
			m.id, m.name, m.rank, COALESCE(m.joined_at, ''),
			COALESCE(SUM(CASE WHEN s.status='active' THEN 1 ELSE 0 END), 0) AS active_strikes,
			COALESCE(vp.monday+vp.tuesday+vp.wednesday+vp.thursday+vp.friday+vp.saturday, 0) AS vs_total,
			COALESCE(vp.monday,0), COALESCE(vp.tuesday,0), COALESCE(vp.wednesday,0),
			COALESCE(vp.thursday,0), COALESCE(vp.friday,0), COALESCE(vp.saturday,0)
		FROM members m
		LEFT JOIN accountability_strikes s ON s.member_id = m.id
		LEFT JOIN vs_points vp ON vp.member_id = m.id AND vp.week_date = ?
		WHERE m.rank != 'EX'
		GROUP BY m.id
		ORDER BY active_strikes DESC
	`, weekDate)
	if err != nil {
		slog.Error("handleAccountabilitySummary: query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	summary := AccountabilitySummary{TopAtRisk: []AccountabilityMember{}}
	for rows.Next() {
		var m AccountabilityMember
		var joinedAt string
		var scores [6]int
		if err := rows.Scan(&m.ID, &m.Name, &m.Rank, &joinedAt, &m.ActiveStrikes, &m.VSTotal,
			&scores[0], &scores[1], &scores[2], &scores[3], &scores[4], &scores[5]); err != nil {
			slog.Error("handleAccountabilitySummary: scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		m.Tag = memberTag(m.ActiveStrikes, thresholds)
		if importedCompleted > 0 {
			m.BelowThreshold = vsDaysBelow(scores, joinedAt, weekDate, completed, dayImported, vsMin) >= vsFlagDays
		}
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
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

// --- API: strike CRUD ---

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

	// Prevent duplicate VS-below-threshold strikes for the same week
	if body.StrikeType == "vs_below_threshold" && body.RefDate != "" {
		var existing int
		db.QueryRow(`SELECT COUNT(*) FROM accountability_strikes
			WHERE member_id = ? AND strike_type = 'vs_below_threshold' AND ref_date = ?`,
			body.MemberID, body.RefDate).Scan(&existing)
		if existing > 0 {
			http.Error(w, "VS strike already exists for this member and week", http.StatusConflict)
			return
		}
	}

	res, err := db.Exec(`
		INSERT INTO accountability_strikes (member_id, strike_type, reason, ref_date, created_by)
		VALUES (?, ?, ?, NULLIF(?, ''), ?)
	`, body.MemberID, body.StrikeType, body.Reason, body.RefDate, userID)
	if err != nil {
		slog.Error("handleStrikeCreate: insert failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

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
