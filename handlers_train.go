// handlers_train.go — Train Tracker: conductor log + eligibility rule engine

package main

import (
	"encoding/json"
	"log/slog"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gorilla/mux"
)

// gameDate returns the current date in game time (UTC-2) as "YYYY-MM-DD".
func gameDate() string {
	return time.Now().UTC().Add(-2 * time.Hour).Format("2006-01-02")
}

// gameWeekStart returns the Monday of the VS week that contains t (UTC-2 adjusted),
// going back `weeksBack` full weeks.  VS weeks run Mon–Sat.
func gameWeekStart(t time.Time, weeksBack int) string {
	gt := t.UTC().Add(-2 * time.Hour)
	// weekday: Monday=0 … Sunday=6
	dow := int(gt.Weekday()+6) % 7 // Mon=0, Tue=1, …, Sun=6
	monday := gt.AddDate(0, 0, -dow-weeksBack*7)
	return monday.Format("2006-01-02")
}

// dayColForDate returns the vs_points column name for the day before `today`
// within the current VS week (Mon–Sat).  Returns "" if yesterday is outside
// the VS window (e.g. today is Monday game-time → yesterday = Sunday).
func dayColForYesterday(today time.Time) string {
	gt := today.UTC().Add(-2 * time.Hour)
	yesterday := gt.AddDate(0, 0, -1)
	cols := []string{"", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	wd := int(yesterday.Weekday()) // Sun=0, Mon=1, …, Sat=6
	if wd == 0 {
		return "" // Sunday — outside VS range
	}
	return cols[wd]
}

// ── Train Logs ────────────────────────────────────────────────────────────────

func getTrainLogs(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")

	query := `
		SELECT tl.id, tl.date, tl.train_type,
		       tl.conductor_id, c.name,
		       tl.vip_id, v.name, tl.vip_type,
		       COALESCE(tl.notes,''), tl.created_by, tl.created_at, tl.updated_at
		FROM train_logs tl
		JOIN members c ON c.id = tl.conductor_id
		LEFT JOIN members v ON v.id = tl.vip_id`

	args := []interface{}{}
	if from != "" && to != "" {
		query += " WHERE tl.date BETWEEN ? AND ?"
		args = append(args, from, to)
	} else if from != "" {
		query += " WHERE tl.date >= ?"
		args = append(args, from)
	} else if to != "" {
		query += " WHERE tl.date <= ?"
		args = append(args, to)
	}
	query += " ORDER BY tl.date DESC, tl.id DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		slog.Error("getTrainLogs query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	logs := []TrainLog{}
	for rows.Next() {
		var tl TrainLog
		var vipName *string
		if err := rows.Scan(
			&tl.ID, &tl.Date, &tl.TrainType,
			&tl.ConductorID, &tl.ConductorName,
			&tl.VIPID, &vipName, &tl.VIPType,
			&tl.Notes, &tl.CreatedBy, &tl.CreatedAt, &tl.UpdatedAt,
		); err != nil {
			slog.Error("getTrainLogs scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		tl.VIPName = vipName
		logs = append(logs, tl)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}

func postTrainLog(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Date        string  `json:"date"`
		TrainType   string  `json:"train_type"`
		ConductorID int     `json:"conductor_id"`
		VIPID       *int    `json:"vip_id"`
		VIPType     *string `json:"vip_type"`
		Notes       string  `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := validateTrainLogRequest(req.Date, req.TrainType, req.ConductorID, req.VIPID, req.VIPType); err != "" {
		http.Error(w, err, http.StatusBadRequest)
		return
	}

	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := db.Exec(`
		INSERT INTO train_logs (date, train_type, conductor_id, vip_id, vip_type, notes, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Date, req.TrainType, req.ConductorID, req.VIPID, req.VIPType, req.Notes, userID, now, now,
	)
	if err != nil {
		slog.Error("postTrainLog insert failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	tl, fetchErr := fetchTrainLog(int(id))
	if fetchErr != nil {
		slog.Error("postTrainLog fetch failed", "error", fetchErr, "id", id)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	limitWarning := checkDailyLimit(req.Date, req.TrainType, int(id))

	username, _ := session.Values["username"].(string)
	logActivity(userID, username, "created", "train_log", req.Date+" "+req.TrainType, false, "conductor: "+tl.ConductorName)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"train_log":     tl,
		"limit_warning": limitWarning,
	})
}

func putTrainLog(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var req struct {
		Date        string  `json:"date"`
		TrainType   string  `json:"train_type"`
		ConductorID int     `json:"conductor_id"`
		VIPID       *int    `json:"vip_id"`
		VIPType     *string `json:"vip_type"`
		Notes       string  `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if errMsg := validateTrainLogRequest(req.Date, req.TrainType, req.ConductorID, req.VIPID, req.VIPType); errMsg != "" {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, dbErr := db.Exec(`
		UPDATE train_logs SET date=?, train_type=?, conductor_id=?, vip_id=?, vip_type=?, notes=?, updated_at=?
		WHERE id=?`,
		req.Date, req.TrainType, req.ConductorID, req.VIPID, req.VIPType, req.Notes, now, id,
	)
	if dbErr != nil {
		slog.Error("putTrainLog update failed", "error", dbErr, "id", id)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	tl, fetchErr := fetchTrainLog(id)
	if fetchErr != nil {
		slog.Error("putTrainLog fetch failed", "error", fetchErr, "id", id)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	limitWarning := checkDailyLimit(req.Date, req.TrainType, id)

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "updated", "train_log", req.Date+" "+req.TrainType, false, "conductor: "+tl.ConductorName)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"train_log":     tl,
		"limit_warning": limitWarning,
	})
}

func deleteTrainLog(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var logDate, trainType, conductorName string
	db.QueryRow(`SELECT tl.date, tl.train_type, m.name FROM train_logs tl JOIN members m ON m.id = tl.conductor_id WHERE tl.id = ?`, id).Scan(&logDate, &trainType, &conductorName)

	tx, err := db.Begin()
	if err != nil {
		slog.Error("deleteTrainLog begin tx failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM train_logs WHERE id = ?`, id); err != nil {
		slog.Error("deleteTrainLog exec failed", "error", err, "id", id)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		slog.Error("deleteTrainLog commit failed", "error", err, "id", id)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "deleted", "train_log", logDate+" "+trainType, false, "conductor: "+conductorName)

	w.WriteHeader(http.StatusNoContent)
}

// ── Eligibility Rules ─────────────────────────────────────────────────────────

func getEligibilityRules(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, name, selection_method, conditions, created_by, created_at, updated_at FROM eligibility_rules ORDER BY name`)
	if err != nil {
		slog.Error("getEligibilityRules query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	rules := []EligibilityRule{}
	for rows.Next() {
		var rule EligibilityRule
		var sm, cond string
		if err := rows.Scan(&rule.ID, &rule.Name, &sm, &cond, &rule.CreatedBy, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
			slog.Error("getEligibilityRules scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		rule.SelectionMethod = json.RawMessage(sm)
		rule.Conditions = json.RawMessage(cond)
		rules = append(rules, rule)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rules)
}

func postEligibilityRule(w http.ResponseWriter, r *http.Request) {
	rule, errMsg := decodeAndValidateRule(r)
	if errMsg != "" {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := db.Exec(`
		INSERT INTO eligibility_rules (name, selection_method, conditions, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		rule.Name, string(rule.SelectionMethod), string(rule.Conditions), userID, now, now,
	)
	if err != nil {
		slog.Error("postEligibilityRule insert failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	rule.ID = int(id)
	rule.CreatedBy = userID
	rule.CreatedAt = now
	rule.UpdatedAt = now

	username, _ := session.Values["username"].(string)
	logActivity(userID, username, "created", "eligibility_rule", rule.Name, false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rule)
}

func putEligibilityRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	rule, errMsg := decodeAndValidateRule(r)
	if errMsg != "" {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, dbErr := db.Exec(`
		UPDATE eligibility_rules SET name=?, selection_method=?, conditions=?, updated_at=? WHERE id=?`,
		rule.Name, string(rule.SelectionMethod), string(rule.Conditions), now, id,
	)
	if dbErr != nil {
		slog.Error("putEligibilityRule update failed", "error", dbErr, "id", id)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	rule.ID = id
	rule.UpdatedAt = now

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "updated", "eligibility_rule", rule.Name, false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rule)
}

func deleteEligibilityRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var ruleName string
	db.QueryRow(`SELECT name FROM eligibility_rules WHERE id = ?`, id).Scan(&ruleName)

	tx, err := db.Begin()
	if err != nil {
		slog.Error("deleteEligibilityRule begin tx failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM eligibility_rules WHERE id = ?`, id); err != nil {
		slog.Error("deleteEligibilityRule exec failed", "error", err, "id", id)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		slog.Error("deleteEligibilityRule commit failed", "error", err, "id", id)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "deleted", "eligibility_rule", ruleName, false)

	w.WriteHeader(http.StatusNoContent)
}

// runEligibilityRule evaluates a saved rule against all active members and
// returns the eligible members sorted by the rule's selection method.
func runEligibilityRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	// Load rule
	var smStr, condStr string
	err = db.QueryRow(`SELECT selection_method, conditions FROM eligibility_rules WHERE id = ?`, id).
		Scan(&smStr, &condStr)
	if err != nil {
		http.Error(w, "rule not found", http.StatusNotFound)
		return
	}

	// Parse conditions
	var condRoot struct {
		Groups []struct {
			Conditions []struct {
				Variable string      `json:"variable"`
				Op       string      `json:"op"`
				Value    interface{} `json:"value"`
			} `json:"conditions"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(condStr), &condRoot); err != nil {
		http.Error(w, "invalid conditions JSON", http.StatusInternalServerError)
		return
	}

	// Parse selection method
	var sm struct {
		Type  string `json:"type"`
		Field string `json:"field"`
	}
	if err := json.Unmarshal([]byte(smStr), &sm); err != nil {
		http.Error(w, "invalid selection_method JSON", http.StatusInternalServerError)
		return
	}

	// Compute game-time dates
	now := time.Now()
	todayStr := now.UTC().Add(-2 * time.Hour).Format("2006-01-02")
	curWeek := gameWeekStart(now, 0)
	prevWeek := gameWeekStart(now, 1)
	yesterdayCol := dayColForYesterday(now)

	// Build yesterday SELECT fragment
	yestSelect := "0"
	if yesterdayCol != "" {
		yestSelect = "COALESCE(vw." + yesterdayCol + ", 0)"
	}

	// Query all active members with their stats
	query := `
		SELECT
			m.id, m.name, m.rank,
			COALESCE(vw.monday,0)+COALESCE(vw.tuesday,0)+COALESCE(vw.wednesday,0)+
			  COALESCE(vw.thursday,0)+COALESCE(vw.friday,0)+COALESCE(vw.saturday,0) AS vs_total_week,
			` + yestSelect + ` AS vs_yesterday,
			COALESCE(vw.monday,0)   AS vs_day_monday,
			COALESCE(vw.tuesday,0)  AS vs_day_tuesday,
			COALESCE(vw.wednesday,0) AS vs_day_wednesday,
			COALESCE(vw.thursday,0) AS vs_day_thursday,
			COALESCE(vw.friday,0)   AS vs_day_friday,
			COALESCE(vw.saturday,0) AS vs_day_saturday,
			COALESCE(vp.monday,0)+COALESCE(vp.tuesday,0)+COALESCE(vp.wednesday,0)+
			  COALESCE(vp.thursday,0)+COALESCE(vp.friday,0)+COALESCE(vp.saturday,0) AS vs_total_prev_week,
			COALESCE(CAST(JULIANDAY(?) - JULIANDAY(MAX(CASE WHEN tl_f.train_type='FREE' THEN tl_f.date END)) AS REAL), 9999) AS days_since_free,
			COALESCE(CAST(JULIANDAY(?) - JULIANDAY(MAX(tl_a.date)) AS REAL), 9999) AS days_since_any
		FROM members m
		LEFT JOIN vs_points vw ON vw.member_id = m.id AND vw.week_date = ?
		LEFT JOIN vs_points vp ON vp.member_id = m.id AND vp.week_date = ?
		LEFT JOIN train_logs tl_f ON tl_f.conductor_id = m.id AND tl_f.train_type = 'FREE'
		LEFT JOIN train_logs tl_a ON tl_a.conductor_id = m.id
		WHERE m.eligible = 1
		GROUP BY m.id`

	rows, err := db.Query(query, todayStr, todayStr, curWeek, prevWeek)
	if err != nil {
		slog.Error("runEligibilityRule query failed", "error", err, "ruleID", id)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type memberStats struct {
		EligibleMember
	}
	var all []memberStats
	for rows.Next() {
		var ms memberStats
		if err := rows.Scan(
			&ms.MemberID, &ms.Name, &ms.Rank,
			&ms.VSTotalWeek, &ms.VSYesterday,
			&ms.VSDayMonday, &ms.VSDayTuesday, &ms.VSDayWednesday,
			&ms.VSDayThursday, &ms.VSDayFriday, &ms.VSDaySaturday,
			&ms.VSTotalPrevWeek,
			&ms.DaysSinceFreeConducted, &ms.DaysSinceAnyConducted,
		); err != nil {
			slog.Error("runEligibilityRule scan failed", "error", err, "ruleID", id)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		all = append(all, ms)
	}

	// Filter by conditions
	eligible := []memberStats{}
	for _, ms := range all {
		if condRoot.Groups == nil || len(condRoot.Groups) == 0 {
			eligible = append(eligible, ms)
			continue
		}
		// OR across groups
		passesAny := false
		for _, grp := range condRoot.Groups {
			passesAll := true
			for _, cond := range grp.Conditions {
				if !evalCondition(cond.Variable, cond.Op, cond.Value, &ms.EligibleMember) {
					passesAll = false
					break
				}
			}
			if passesAll {
				passesAny = true
				break
			}
		}
		if passesAny {
			eligible = append(eligible, ms)
		}
	}

	// Sort by selection method
	switch sm.Type {
	case "RANDOM":
		rand.Shuffle(len(eligible), func(i, j int) { eligible[i], eligible[j] = eligible[j], eligible[i] })
	case "GREATEST":
		sort.Slice(eligible, func(i, j int) bool {
			return getField(&eligible[i].EligibleMember, sm.Field) > getField(&eligible[j].EligibleMember, sm.Field)
		})
	case "LEAST":
		sort.Slice(eligible, func(i, j int) bool {
			return getField(&eligible[i].EligibleMember, sm.Field) < getField(&eligible[j].EligibleMember, sm.Field)
		})
	}

	// Build response (unwrap EligibleMember)
	result := make([]EligibleMember, len(eligible))
	for i, ms := range eligible {
		result[i] = ms.EligibleMember
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func validateTrainLogRequest(date, trainType string, conductorID int, vipID *int, vipType *string) string {
	if date == "" {
		return "date is required"
	}
	if trainType != "FREE" && trainType != "PURCHASED" {
		return "train_type must be FREE or PURCHASED"
	}
	if conductorID == 0 {
		return "conductor_id is required"
	}
	// If vip_id is provided, vip_type must also be provided (and vice versa)
	if (vipID == nil) != (vipType == nil) {
		return "vip_id and vip_type must both be provided or both omitted"
	}
	if vipType != nil && *vipType != "SPECIAL_GUEST" && *vipType != "GUARDIAN_DEFENDER" {
		return "vip_type must be SPECIAL_GUEST or GUARDIAN_DEFENDER"
	}
	return ""
}

func fetchTrainLog(id int) (*TrainLog, error) {
	var tl TrainLog
	var vipName *string
	err := db.QueryRow(`
		SELECT tl.id, tl.date, tl.train_type,
		       tl.conductor_id, c.name,
		       tl.vip_id, v.name, tl.vip_type,
		       COALESCE(tl.notes,''), tl.created_by, tl.created_at, tl.updated_at
		FROM train_logs tl
		JOIN members c ON c.id = tl.conductor_id
		LEFT JOIN members v ON v.id = tl.vip_id
		WHERE tl.id = ?`, id).Scan(
		&tl.ID, &tl.Date, &tl.TrainType,
		&tl.ConductorID, &tl.ConductorName,
		&tl.VIPID, &vipName, &tl.VIPType,
		&tl.Notes, &tl.CreatedBy, &tl.CreatedAt, &tl.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	tl.VIPName = vipName
	return &tl, nil
}

// checkDailyLimit returns true if the count for the given date+type exceeds
// the configured soft limit, excluding the just-saved record.
func checkDailyLimit(date, trainType string, excludeID int) bool {
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM train_logs WHERE date=? AND train_type=? AND id != ?`,
		date, trainType, excludeID).Scan(&count)

	defaultLimit := 1
	col := "train_free_daily_limit"
	if trainType == "PURCHASED" {
		col = "train_purchased_daily_limit"
		defaultLimit = 2
	}
	var limit int
	if err := db.QueryRow(`SELECT COALESCE(`+col+`, ?) FROM settings WHERE id = 1`, defaultLimit).Scan(&limit); err != nil || limit < 1 {
		limit = defaultLimit
	}
	return count >= limit
}

func decodeAndValidateRule(r *http.Request) (EligibilityRule, string) {
	var rule EligibilityRule
	var req struct {
		Name            string          `json:"name"`
		SelectionMethod json.RawMessage `json:"selection_method"`
		Conditions      json.RawMessage `json:"conditions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return rule, "Invalid request body"
	}
	if req.Name == "" {
		return rule, "name is required"
	}
	if !json.Valid(req.SelectionMethod) {
		return rule, "selection_method must be valid JSON"
	}
	if !json.Valid(req.Conditions) {
		return rule, "conditions must be valid JSON"
	}
	rule.Name = req.Name
	rule.SelectionMethod = req.SelectionMethod
	rule.Conditions = req.Conditions
	return rule, ""
}

// rankValue maps R1–R5 to integers for comparison.
func rankValue(rank string) float64 {
	switch rank {
	case "R1":
		return 1
	case "R2":
		return 2
	case "R3":
		return 3
	case "R4":
		return 4
	case "R5":
		return 5
	}
	return 0
}

// getField returns the numeric value of a named variable for a member.
func getField(m *EligibleMember, field string) float64 {
	switch field {
	case "rank":
		return rankValue(m.Rank)
	case "vs_total_week":
		return float64(m.VSTotalWeek)
	case "vs_yesterday":
		return float64(m.VSYesterday)
	case "vs_total_prev_week":
		return float64(m.VSTotalPrevWeek)
	case "vs_day_monday":
		return float64(m.VSDayMonday)
	case "vs_day_tuesday":
		return float64(m.VSDayTuesday)
	case "vs_day_wednesday":
		return float64(m.VSDayWednesday)
	case "vs_day_thursday":
		return float64(m.VSDayThursday)
	case "vs_day_friday":
		return float64(m.VSDayFriday)
	case "vs_day_saturday":
		return float64(m.VSDaySaturday)
	case "days_since_free_conducted":
		return m.DaysSinceFreeConducted
	case "days_since_any_conducted":
		return m.DaysSinceAnyConducted
	}
	return 0
}

// evalCondition evaluates a single condition against a member's stats.
func evalCondition(variable, op string, rawValue interface{}, m *EligibleMember) bool {
	// Handle rank `in` operator separately
	if variable == "rank" && op == "in" {
		list, ok := toStringSlice(rawValue)
		if !ok {
			return false
		}
		for _, v := range list {
			if m.Rank == v {
				return true
			}
		}
		return false
	}

	memberVal := getField(m, variable)

	// For rank with non-in operators, compare numeric rank values
	var condVal float64
	if variable == "rank" {
		s, ok := rawValue.(string)
		if !ok {
			return false
		}
		condVal = rankValue(s)
	} else {
		condVal = toFloat64(rawValue)
	}

	switch op {
	case ">=":
		return memberVal >= condVal
	case "<=":
		return memberVal <= condVal
	case ">":
		return memberVal > condVal
	case "<":
		return memberVal < condVal
	case "==":
		return memberVal == condVal
	}
	return false
}

// toFloat64 converts JSON-decoded numbers (float64) or integers to float64.
func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}

// toStringSlice converts a JSON-decoded []interface{} to []string.
func toStringSlice(v interface{}) ([]string, bool) {
	arr, ok := v.([]interface{})
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}
