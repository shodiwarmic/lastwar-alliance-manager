// handlers_accountability.go — Member accountability tracker

package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// currentMondayDate returns today's Monday in YYYY-MM-DD format (UTC).
func currentMondayDate() string {
	now := time.Now().UTC()
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7
	}
	monday := now.AddDate(0, 0, -(wd - 1))
	return monday.Format("2006-01-02")
}

// strikeThresholds holds the configured tag boundaries.
type strikeThresholds struct {
	NeedsImprovement int
	AtRisk           int
}

// loadStrikeThresholds reads configured thresholds from settings.
func loadStrikeThresholds() strikeThresholds {
	t := strikeThresholds{NeedsImprovement: 1, AtRisk: 3}
	db.QueryRow(`SELECT COALESCE(strike_needs_improvement_threshold, 1),
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

// vsMinimumPoints loads the configured VS minimum from settings.
func vsMinimumPoints() int {
	var v int
	if err := db.QueryRow("SELECT COALESCE(vs_minimum_points, 2500000) FROM settings WHERE id = 1").Scan(&v); err != nil {
		return 2500000
	}
	return v
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
	renderTemplate(w, r, "accountability.html", data)
}

func handleAccountabilityReport(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "Accountability Report - Alliance Manager", "accountability")
	if !data.IsAuthenticated {
		http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
		return
	}
	if !data.Permissions.ViewAccountability {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	renderTemplate(w, r, "accountability_report.html", data)
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

	vsMin := vsMinimumPoints()
	monday := currentMondayDate()
	thresholds := loadStrikeThresholds()

	rows, err := db.Query(`
		SELECT
			m.id, m.name, m.rank,
			COALESCE(SUM(CASE WHEN s.status='active' THEN 1 ELSE 0 END), 0) AS active_strikes,
			COALESCE(vp.monday+vp.tuesday+vp.wednesday+vp.thursday+vp.friday+vp.saturday, 0) AS vs_total
		FROM members m
		LEFT JOIN accountability_strikes s ON s.member_id = m.id
		LEFT JOIN vs_points vp ON vp.member_id = m.id AND vp.week_date = ?
		WHERE m.rank != 'EX'
		GROUP BY m.id
		ORDER BY active_strikes DESC, m.name ASC
	`, monday)
	if err != nil {
		slog.Error("handleAccountabilityMembers: query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	members := []AccountabilityMember{}
	for rows.Next() {
		var m AccountabilityMember
		if err := rows.Scan(&m.ID, &m.Name, &m.Rank, &m.ActiveStrikes, &m.VSTotal); err != nil {
			slog.Error("handleAccountabilityMembers: scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		m.Tag = memberTag(m.ActiveStrikes, thresholds)
		m.BelowThreshold = m.VSTotal < vsMin
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

type StormAttendanceRecord struct {
	ID           int    `json:"id"`
	StormDate    string `json:"storm_date"`
	Status       string `json:"status"`
	ExcuseReason string `json:"excuse_reason"`
	RecordedBy   string `json:"recorded_by"`
	CreatedAt    string `json:"created_at"`
}

type TrainLogRecord struct {
	ID            int    `json:"id"`
	Date          string `json:"date"`
	TrainType     string `json:"train_type"`
	ShowedUp      bool   `json:"showed_up"`
	CreatedAt     string `json:"created_at"`
}

type VSWeekRecord struct {
	WeekDate string `json:"week_date"`
	Total    int    `json:"total"`
}

type MemberProfileData struct {
	ID            int                     `json:"id"`
	Name          string                  `json:"name"`
	Rank          string                  `json:"rank"`
	Notes         string                  `json:"notes"`
	ActiveStrikes int                     `json:"active_strikes"`
	Tag           string                  `json:"tag"`
	Strikes       []AccountabilityStrike  `json:"strikes"`
	VSHistory     []VSWeekRecord          `json:"vs_history"`
	StormHistory  []StormAttendanceRecord `json:"storm_history"`
	TrainHistory  []TrainLogRecord        `json:"train_history"`
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
	thresholds := loadStrikeThresholds()
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

	// Storm attendance — last 10 entries
	stormRows, err := db.Query(`
		SELECT sa.id, sa.storm_date, sa.status, sa.excuse_reason,
		       COALESCE(u.username,''), sa.created_at
		FROM storm_attendance sa
		LEFT JOIN users u ON u.id = sa.recorded_by
		WHERE sa.member_id = ?
		ORDER BY sa.storm_date DESC
		LIMIT 10
	`, id)
	if err != nil {
		slog.Error("handleAccountabilityMemberProfile: storm query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer stormRows.Close()
	profile.StormHistory = []StormAttendanceRecord{}
	for stormRows.Next() {
		var s StormAttendanceRecord
		if err := stormRows.Scan(&s.ID, &s.StormDate, &s.Status, &s.ExcuseReason,
			&s.RecordedBy, &s.CreatedAt); err != nil {
			slog.Error("handleAccountabilityMemberProfile: storm scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		profile.StormHistory = append(profile.StormHistory, s)
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
	AtRisk          int                    `json:"at_risk"`
	NeedsImprovement int                   `json:"needs_improvement"`
	Reliable        int                    `json:"reliable"`
	BelowVS         int                    `json:"below_vs"`
	TopAtRisk       []AccountabilityMember `json:"top_at_risk"`
}

func handleAccountabilitySummary(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "", "")
	if !data.IsAuthenticated || !data.Permissions.ViewAccountability {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	vsMin := vsMinimumPoints()
	monday := currentMondayDate()
	thresholds := loadStrikeThresholds()

	rows, err := db.Query(`
		SELECT
			m.id, m.name, m.rank,
			COALESCE(SUM(CASE WHEN s.status='active' THEN 1 ELSE 0 END), 0) AS active_strikes,
			COALESCE(vp.monday+vp.tuesday+vp.wednesday+vp.thursday+vp.friday+vp.saturday, 0) AS vs_total
		FROM members m
		LEFT JOIN accountability_strikes s ON s.member_id = m.id
		LEFT JOIN vs_points vp ON vp.member_id = m.id AND vp.week_date = ?
		WHERE m.rank != 'EX'
		GROUP BY m.id
		ORDER BY active_strikes DESC
	`, monday)
	if err != nil {
		slog.Error("handleAccountabilitySummary: query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	summary := AccountabilitySummary{TopAtRisk: []AccountabilityMember{}}
	for rows.Next() {
		var m AccountabilityMember
		if err := rows.Scan(&m.ID, &m.Name, &m.Rank, &m.ActiveStrikes, &m.VSTotal); err != nil {
			slog.Error("handleAccountabilitySummary: scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		m.Tag = memberTag(m.ActiveStrikes, thresholds)
		m.BelowThreshold = m.VSTotal < vsMin
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

// --- API: report data ---

type ReportTopMember struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Rank  string `json:"rank"`
	Value int    `json:"value"`
}

type AccountabilityReportData struct {
	VSLeaders       []ReportTopMember `json:"vs_leaders"`
	VSUnderperformers []ReportTopMember `json:"vs_underperformers"`
	PowerGrowth     []ReportTopMember `json:"power_growth"`
	TagCounts       map[string]int    `json:"tag_counts"`
	TotalStrikes    int               `json:"total_strikes"`
	VSMin           int               `json:"vs_min"`
}

func handleAccountabilityReportData(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "", "")
	if !data.IsAuthenticated || !data.Permissions.ViewAccountability {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	vsMin := vsMinimumPoints()
	monday := currentMondayDate()
	thresholds := loadStrikeThresholds()

	report := AccountabilityReportData{
		VSLeaders:         []ReportTopMember{},
		VSUnderperformers: []ReportTopMember{},
		PowerGrowth:       []ReportTopMember{},
		TagCounts:         map[string]int{"At Risk": 0, "Needs Improvement": 0, "Reliable": 0},
		VSMin:             vsMin,
	}

	// VS leaders and underperformers this week
	vsRows, err := db.Query(`
		SELECT m.id, m.name, m.rank,
		       COALESCE(vp.monday+vp.tuesday+vp.wednesday+vp.thursday+vp.friday+vp.saturday, 0) AS vs_total
		FROM members m
		LEFT JOIN vs_points vp ON vp.member_id = m.id AND vp.week_date = ?
		WHERE m.rank != 'EX'
		ORDER BY vs_total DESC
	`, monday)
	if err != nil {
		slog.Error("handleAccountabilityReportData: vs query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer vsRows.Close()

	var allVS []ReportTopMember
	for vsRows.Next() {
		var m ReportTopMember
		if err := vsRows.Scan(&m.ID, &m.Name, &m.Rank, &m.Value); err != nil {
			slog.Error("handleAccountabilityReportData: vs scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		allVS = append(allVS, m)
	}
	for i, m := range allVS {
		if i < 5 {
			report.VSLeaders = append(report.VSLeaders, m)
		}
		if m.Value < vsMin {
			report.VSUnderperformers = append(report.VSUnderperformers, m)
		}
	}

	// Power growth: latest snapshot minus snapshot ~4 weeks ago
	growthRows, err := db.Query(`
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
				WHERE member_id = ph2.member_id AND recorded_at <= datetime('now', '-28 days')
			)
		) older ON older.member_id = m.id
		WHERE m.rank != 'EX'
		ORDER BY growth DESC
		LIMIT 5
	`)
	if err != nil {
		slog.Error("handleAccountabilityReportData: growth query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer growthRows.Close()
	for growthRows.Next() {
		var m ReportTopMember
		if err := growthRows.Scan(&m.ID, &m.Name, &m.Rank, &m.Value); err != nil {
			slog.Error("handleAccountabilityReportData: growth scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		report.PowerGrowth = append(report.PowerGrowth, m)
	}

	// Tag counts and total strikes
	tagRows, err := db.Query(`
		SELECT
			COALESCE(SUM(CASE WHEN s.status='active' THEN 1 ELSE 0 END), 0) AS active_strikes
		FROM members m
		LEFT JOIN accountability_strikes s ON s.member_id = m.id
		WHERE m.rank != 'EX'
		GROUP BY m.id
	`)
	if err != nil {
		slog.Error("handleAccountabilityReportData: tag query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tagRows.Close()
	for tagRows.Next() {
		var strikes int
		if err := tagRows.Scan(&strikes); err != nil {
			slog.Error("handleAccountabilityReportData: tag scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		report.TagCounts[memberTag(strikes, thresholds)]++
		report.TotalStrikes += strikes
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(report)
}

// --- API: strike CRUD ---

func handleStrikeCreate(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)

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
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)

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
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)

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

// --- API: storm attendance ---

func handleStormAttendanceUpsert(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)

	var body struct {
		StormDate string `json:"storm_date"`
		Records   []struct {
			MemberID     int    `json:"member_id"`
			Status       string `json:"status"`
			ExcuseReason string `json:"excuse_reason"`
		} `json:"records"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.StormDate == "" || len(body.Records) == 0 {
		http.Error(w, "storm_date and records are required", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO storm_attendance (storm_date, member_id, status, excuse_reason, recorded_by)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(storm_date, member_id) DO UPDATE SET
			status        = excluded.status,
			excuse_reason = excluded.excuse_reason,
			recorded_by   = excluded.recorded_by
	`)
	if err != nil {
		slog.Error("handleStormAttendanceUpsert: prepare failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	for _, rec := range body.Records {
		if _, err := stmt.Exec(body.StormDate, rec.MemberID, rec.Status, rec.ExcuseReason, userID); err != nil {
			slog.Error("handleStormAttendanceUpsert: exec failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		slog.Error("handleStormAttendanceUpsert: commit failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	logActivity(userID, username, "created", "storm_attendance", body.StormDate, false,
		strconv.Itoa(len(body.Records))+" records logged")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Saved"})
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

// --- API: storm attendance for a specific date ---

type StormAttendanceMember struct {
	MemberID     int    `json:"member_id"`
	MemberName   string `json:"member_name"`
	MemberRank   string `json:"member_rank"`
	Status       string `json:"status"`
	ExcuseReason string `json:"excuse_reason"`
	RecordID     int    `json:"record_id"` // 0 if not yet logged
}

func handleStormAttendanceForDate(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "", "")
	if !data.IsAuthenticated || !data.Permissions.ViewAccountability {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	date := r.URL.Query().Get("date")
	if date == "" {
		http.Error(w, "date query parameter required", http.StatusBadRequest)
		return
	}

	// All active members LEFT JOIN existing attendance for this date
	rows, err := db.Query(`
		SELECT m.id, m.name, m.rank,
		       COALESCE(sa.status, 'not_enrolled'),
		       COALESCE(sa.excuse_reason, ''),
		       COALESCE(sa.id, 0)
		FROM members m
		LEFT JOIN storm_attendance sa ON sa.member_id = m.id AND sa.storm_date = ?
		WHERE m.rank != 'EX'
		ORDER BY
			CASE COALESCE(sa.status,'not_enrolled')
				WHEN 'attended'     THEN 1
				WHEN 'no_show'      THEN 2
				WHEN 'excused'      THEN 3
				WHEN 'not_enrolled' THEN 4
			END,
			m.name ASC
	`, date)
	if err != nil {
		slog.Error("handleStormAttendanceForDate: query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	members := []StormAttendanceMember{}
	for rows.Next() {
		var m StormAttendanceMember
		if err := rows.Scan(&m.MemberID, &m.MemberName, &m.MemberRank,
			&m.Status, &m.ExcuseReason, &m.RecordID); err != nil {
			slog.Error("handleStormAttendanceForDate: scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		members = append(members, m)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(members)
}
