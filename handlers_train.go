package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// Get train schedules (optionally filtered by date range)
func getTrainSchedules(w http.ResponseWriter, r *http.Request) {
	startDate := r.URL.Query().Get("start")
	endDate := r.URL.Query().Get("end")

	query := `
		SELECT 
			ts.id, ts.date, ts.conductor_id, m1.name as conductor_name,
			ts.conductor_score, ts.backup_id, m2.name as backup_name, m2.rank as backup_rank,
			ts.conductor_showed_up, ts.notes, ts.created_at
		FROM train_schedules ts
		JOIN members m1 ON ts.conductor_id = m1.id
		LEFT JOIN members m2 ON ts.backup_id = m2.id
	`

	var rows *sql.Rows
	var err error

	if startDate != "" && endDate != "" {
		query += " WHERE ts.date BETWEEN ? AND ? ORDER BY ts.date, ts.conductor_score DESC"
		rows, err = db.Query(query, startDate, endDate)
	} else {
		query += " ORDER BY ts.date, ts.conductor_score DESC"
		rows, err = db.Query(query)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	schedules := []TrainSchedule{}
	for rows.Next() {
		var ts TrainSchedule
		var showedUp sql.NullBool
		var notes sql.NullString
		var score sql.NullInt64
		var backupID sql.NullInt64
		var backupName sql.NullString
		var backupRank sql.NullString

		if err := rows.Scan(&ts.ID, &ts.Date, &ts.ConductorID, &ts.ConductorName,
			&score, &backupID, &backupName, &backupRank, &showedUp, &notes, &ts.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if backupID.Valid {
			ts.BackupID = int(backupID.Int64)
			ts.BackupName = backupName.String
			ts.BackupRank = backupRank.String
		}

		if showedUp.Valid {
			ts.ConductorShowedUp = &showedUp.Bool
		}
		if notes.Valid {
			ts.Notes = &notes.String
		}
		if score.Valid {
			scoreInt := int(score.Int64)
			ts.ConductorScore = &scoreInt
		}

		schedules = append(schedules, ts)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(schedules)
}

func createTrainSchedule(w http.ResponseWriter, r *http.Request) {
	var ts TrainSchedule
	if err := json.NewDecoder(r.Body).Decode(&ts); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if ts.BackupID > 0 {
		var backupRank string
		err := db.QueryRow("SELECT rank FROM members WHERE id = ?", ts.BackupID).Scan(&backupRank)
		if err != nil {
			http.Error(w, "Backup member not found", http.StatusBadRequest)
			return
		}

		if backupRank != "R4" && backupRank != "R5" {
			http.Error(w, "Backup must be an R4 or R5 member", http.StatusBadRequest)
			return
		}
	}

	result, err := db.Exec(
		"INSERT OR REPLACE INTO train_schedules (date, conductor_id, backup_id, conductor_score, conductor_showed_up, notes) VALUES (?, ?, ?, ?, ?, ?)",
		ts.Date, ts.ConductorID, ts.BackupID, ts.ConductorScore, ts.ConductorShowedUp, ts.Notes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	ts.ID = int(id)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(ts)
}

func updateTrainSchedule(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid schedule ID", http.StatusBadRequest)
		return
	}

	var ts TrainSchedule
	if err := json.NewDecoder(r.Body).Decode(&ts); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if ts.BackupID > 0 {
		var backupRank string
		err := db.QueryRow("SELECT rank FROM members WHERE id = ?", ts.BackupID).Scan(&backupRank)
		if err != nil {
			http.Error(w, "Backup member not found", http.StatusBadRequest)
			return
		}

		if backupRank != "R4" && backupRank != "R5" {
			http.Error(w, "Backup must be an R4 or R5 member", http.StatusBadRequest)
			return
		}
	}

	_, err = db.Exec(
		"UPDATE train_schedules SET date = ?, conductor_id = ?, backup_id = ?, conductor_score = ?, conductor_showed_up = ?, notes = ? WHERE id = ?",
		ts.Date, ts.ConductorID, ts.BackupID, ts.ConductorScore, ts.ConductorShowedUp, ts.Notes, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ts.ID = id
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ts)
}

func deleteTrainSchedule(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid schedule ID", http.StatusBadRequest)
		return
	}

	_, err = db.Exec("DELETE FROM train_schedules WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func autoSchedule(w http.ResponseWriter, r *http.Request) {
	var input struct {
		StartDate string `json:"start_date"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	scheduleDate, err := parseDate(input.StartDate)
	if err != nil {
		http.Error(w, "Invalid date format", http.StatusBadRequest)
		return
	}

	weekStart := getMondayOfWeek(scheduleDate)

	ctx, err := buildRankingContext(weekStart)
	if err != nil {
		http.Error(w, "Failed to load ranking context: "+err.Error(), http.StatusInternalServerError)
		return
	}

	rows, err := db.Query("SELECT id, name, rank, COALESCE(eligible, 1) FROM members WHERE COALESCE(eligible, 1) = 1 ORDER BY name")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var candidates []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.Name, &m.Rank, &m.Eligible); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		candidates = append(candidates, m)
	}

	if len(candidates) < 7 {
		http.Error(w, "Not enough members for weekly scheduling (need at least 7)", http.StatusBadRequest)
		return
	}

	type ScoredMember struct {
		Member Member
		Score  int
	}

	var scoredCandidates []ScoredMember
	for _, member := range candidates {
		score := calculateMemberScore(member, ctx)
		scoredCandidates = append(scoredCandidates, ScoredMember{
			Member: member,
			Score:  score,
		})
	}

	for i := 0; i < len(scoredCandidates); i++ {
		for j := i + 1; j < len(scoredCandidates); j++ {
			if scoredCandidates[j].Score > scoredCandidates[i].Score {
				scoredCandidates[i], scoredCandidates[j] = scoredCandidates[j], scoredCandidates[i]
			}
		}
	}

	plannedConductors := make(map[int]bool)
	for i := 0; i < 7 && i < len(scoredCandidates); i++ {
		plannedConductors[scoredCandidates[i].Member.ID] = true
	}

	var weekSchedules []TrainSchedule
	usedConductors := make(map[int]bool)
	usedBackups := make(map[int]bool)

	for day := 0; day < 7; day++ {
		currentDate := weekStart.AddDate(0, 0, day)
		dateStr := formatDateString(currentDate)

		var conductorID int
		var conductorScore int

		for _, sc := range scoredCandidates {
			if plannedConductors[sc.Member.ID] && !usedConductors[sc.Member.ID] {
				conductorID = sc.Member.ID
				conductorScore = sc.Score
				usedConductors[sc.Member.ID] = true
				break
			}
		}

		if conductorID == 0 {
			http.Error(w, "Unable to assign conductor for all days", http.StatusInternalServerError)
			return
		}

		var availableBackups []Member
		for _, sc := range scoredCandidates {
			if !plannedConductors[sc.Member.ID] &&
				!usedBackups[sc.Member.ID] &&
				(sc.Member.Rank == "R4" || sc.Member.Rank == "R5") {
				availableBackups = append(availableBackups, sc.Member)
			}
		}

		var backupID int
		if len(availableBackups) > 0 {
			randomIndex := time.Now().UnixNano() % int64(len(availableBackups))
			backupID = availableBackups[randomIndex].ID
			usedBackups[backupID] = true
		}

		var result sql.Result
		if backupID > 0 {
			result, err = db.Exec(
				"INSERT OR REPLACE INTO train_schedules (date, conductor_id, backup_id, conductor_score) VALUES (?, ?, ?, ?)",
				dateStr, conductorID, backupID, conductorScore,
			)
		} else {
			result, err = db.Exec(
				"INSERT OR REPLACE INTO train_schedules (date, conductor_id, backup_id, conductor_score) VALUES (?, ?, NULL, ?)",
				dateStr, conductorID, conductorScore,
			)
		}
		if err != nil {
			http.Error(w, "Failed to create schedule: "+err.Error(), http.StatusInternalServerError)
			return
		}

		scheduleID, _ := result.LastInsertId()

		var schedule TrainSchedule
		var score sql.NullInt64
		var backupName sql.NullString
		var backupRank sql.NullString

		if backupID > 0 {
			err = db.QueryRow(`
			SELECT 
				ts.id, ts.date, ts.conductor_id, 
				mc.name, ts.conductor_score, ts.backup_id, mb.name, mb.rank,
				ts.conductor_showed_up, ts.notes, ts.created_at
			FROM train_schedules ts
			JOIN members mc ON ts.conductor_id = mc.id
			LEFT JOIN members mb ON ts.backup_id = mb.id
			WHERE ts.id = ?
		`, scheduleID).Scan(
				&schedule.ID, &schedule.Date, &schedule.ConductorID,
				&schedule.ConductorName, &score, &schedule.BackupID, &backupName,
				&backupRank, &schedule.ConductorShowedUp, &schedule.Notes,
				&schedule.CreatedAt,
			)
		} else {
			err = db.QueryRow(`
			SELECT 
				ts.id, ts.date, ts.conductor_id, 
				mc.name, ts.conductor_score,
				ts.conductor_showed_up, ts.notes, ts.created_at
			FROM train_schedules ts
			JOIN members mc ON ts.conductor_id = mc.id
			WHERE ts.id = ?
		`, scheduleID).Scan(
				&schedule.ID, &schedule.Date, &schedule.ConductorID,
				&schedule.ConductorName, &score,
				&schedule.ConductorShowedUp, &schedule.Notes,
				&schedule.CreatedAt,
			)
			schedule.BackupID = 0
			schedule.BackupName = ""
			schedule.BackupRank = ""
		}

		if err != nil {
			http.Error(w, "Failed to retrieve schedule: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if backupID > 0 {
			if backupName.Valid {
				schedule.BackupName = backupName.String
			}
			if backupRank.Valid {
				schedule.BackupRank = backupRank.String
			}
		}

		if score.Valid {
			scoreInt := int(score.Int64)
			schedule.ConductorScore = &scoreInt
		}

		weekSchedules = append(weekSchedules, schedule)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":   "Week scheduled successfully",
		"schedules": weekSchedules,
	})
}

func generateWeeklyMessage(w http.ResponseWriter, r *http.Request) {
	startDate := r.URL.Query().Get("start")
	if startDate == "" {
		http.Error(w, "start date is required", http.StatusBadRequest)
		return
	}

	weekStart, err := parseDate(startDate)
	if err != nil {
		http.Error(w, "Invalid date format", http.StatusBadRequest)
		return
	}

	settings, err := loadSettings()
	if err != nil {
		http.Error(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	weekEnd := weekStart.AddDate(0, 0, 6)
	rows, err := db.Query(`
		SELECT 
			ts.date, m1.name as conductor_name, COALESCE(m2.name, 'None') as backup_name
		FROM train_schedules ts
		JOIN members m1 ON ts.conductor_id = m1.id
		LEFT JOIN members m2 ON ts.backup_id = m2.id
		WHERE ts.date >= ? AND ts.date <= ?
		ORDER BY ts.date
	`, formatDateString(weekStart), formatDateString(weekEnd))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var schedulesText strings.Builder
	for rows.Next() {
		var date, conductor, backup string
		if err := rows.Scan(&date, &conductor, &backup); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		dateObj, _ := parseDate(date)
		dayName := dateObj.Format("Monday")

		schedulesText.WriteString(dayName + ": " + conductor + " (Backup: " + backup + ")\n")
	}

	ctx, err := buildRankingContext(weekStart)
	if err != nil {
		http.Error(w, "Failed to load ranking context: "+err.Error(), http.StatusInternalServerError)
		return
	}

	memberRows, err := db.Query("SELECT id, name, rank FROM members WHERE eligible = 1 ORDER BY name")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer memberRows.Close()

	type ScoredMember struct {
		Name  string
		Score int
	}

	var scoredMembers []ScoredMember
	for memberRows.Next() {
		var m Member
		if err := memberRows.Scan(&m.ID, &m.Name, &m.Rank); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		score := calculateMemberScore(m, ctx)
		scoredMembers = append(scoredMembers, ScoredMember{
			Name:  m.Name,
			Score: score,
		})
	}

	for i := 0; i < len(scoredMembers); i++ {
		for j := i + 1; j < len(scoredMembers); j++ {
			if scoredMembers[j].Score > scoredMembers[i].Score {
				scoredMembers[i], scoredMembers[j] = scoredMembers[j], scoredMembers[i]
			}
		}
	}

	var next3Text strings.Builder
	limit := 3
	if len(scoredMembers) < 3 {
		limit = len(scoredMembers)
	}
	for i := 0; i < limit; i++ {
		next3Text.WriteString(scoredMembers[i].Name + "\n")
	}

	message := settings.ScheduleMessageTemplate
	message = strings.ReplaceAll(message, "{WEEK}", weekStart.Format("Jan 2, 2006"))
	message = strings.ReplaceAll(message, "{SCHEDULES}", schedulesText.String())
	message = strings.ReplaceAll(message, "{NEXT_3}", next3Text.String())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": message,
	})
}

func generateDailyMessage(w http.ResponseWriter, r *http.Request) {
	dateParam := r.URL.Query().Get("date")
	if dateParam == "" {
		http.Error(w, "date is required", http.StatusBadRequest)
		return
	}

	date, err := parseDate(dateParam)
	if err != nil {
		http.Error(w, "Invalid date format", http.StatusBadRequest)
		return
	}

	settings, err := loadSettings()
	if err != nil {
		http.Error(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var conductorName, conductorRank, backupName, backupRank string
	var backupNameNull sql.NullString
	var backupRankNull sql.NullString

	err = db.QueryRow(`
		SELECT 
			m1.name as conductor_name, m1.rank as conductor_rank,
			m2.name as backup_name, m2.rank as backup_rank
		FROM train_schedules ts
		JOIN members m1 ON ts.conductor_id = m1.id
		LEFT JOIN members m2 ON ts.backup_id = m2.id
		WHERE ts.date = ?
	`, formatDateString(date)).Scan(&conductorName, &conductorRank, &backupNameNull, &backupRankNull)

	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			http.Error(w, "No schedule found for this date", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	if backupNameNull.Valid {
		backupName = backupNameNull.String
		backupRank = backupRankNull.String
	} else {
		backupName = "None"
		backupRank = "N/A"
	}

	message := settings.DailyMessageTemplate
	message = strings.ReplaceAll(message, "{DATE}", date.Format("Monday, Jan 2, 2006"))
	message = strings.ReplaceAll(message, "{CONDUCTOR_NAME}", conductorName)
	message = strings.ReplaceAll(message, "{CONDUCTOR_RANK}", conductorRank)
	message = strings.ReplaceAll(message, "{BACKUP_NAME}", backupName)
	message = strings.ReplaceAll(message, "{BACKUP_RANK}", backupRank)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": message,
	})
}

func generateConductorMessages(w http.ResponseWriter, r *http.Request) {
	startDate := r.URL.Query().Get("start")
	if startDate == "" {
		http.Error(w, "start date is required", http.StatusBadRequest)
		return
	}

	weekStart, err := parseDate(startDate)
	if err != nil {
		http.Error(w, "Invalid date format", http.StatusBadRequest)
		return
	}

	weekEnd := weekStart.AddDate(0, 0, 6)
	rows, err := db.Query(`
		SELECT 
			ts.date, m1.name as conductor_name
		FROM train_schedules ts
		JOIN members m1 ON ts.conductor_id = m1.id
		WHERE ts.date >= ? AND ts.date <= ?
		ORDER BY ts.date
	`, formatDateString(weekStart), formatDateString(weekEnd))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	messageTemplates := []string{
		"Hi {NAME}! Just a reminder that you're the train conductor on {DAY}, {DATE}. Please be online around 15:00 ST / 17:00 UK / 18:00 CET and ask in alliance chat for the train to be assigned to you. If anything comes up, let us know early so we can coordinate with the backup. Please add a reminder in your phone so you don't forget. Thanks for helping keep the train golden!",
		"Hi {NAME}! You're scheduled as train conductor on {DAY}, {DATE}. Please be online at 15:00 ST / 17:00 UK / 18:00 CET and request the train in alliance chat. If your schedule changes, let us know in advance so we can coordinate with the backup. Add a reminder in your phone to make sure you're on time. Appreciate your support!",
		"Hi {NAME}! Just a heads-up that you're the train conductor on {DAY}, {DATE}. Please be online around 15:00 ST / 17:00 UK / 18:00 CET and ask for the train in alliance chat. If you need help or need to swap, reach out early. Set a phone reminder so you don't miss it. Thanks a lot!",
		"Hi {NAME}! You're assigned as train conductor on {DAY}, {DATE}. Please be online at 15:00 ST / 17:00 UK / 18:00 CET and request the train in alliance chat. If there are any timing issues, let us know so we can plan with the backup. Don't forget to add a reminder in your phone. Thanks for stepping up!",
		"Hi {NAME}! Reminder that you're the train conductor on {DAY}, {DATE}. Please be online around 15:00 ST / 17:00 UK / 18:00 CET and ask in alliance chat for the train assignment. Let us know early if anything changes. Make sure to set a phone reminder. Much appreciated!",
		"Hi {NAME}! You're scheduled as train conductor on {DAY}, {DATE}. Please be online at 15:00 ST / 17:00 UK / 18:00 CET and request the train in alliance chat. If you need assistance or a timing adjustment, just let us know. Add a phone reminder to help you remember. Thanks!",
		"Hi {NAME}! Just a reminder that you're the train conductor on {DAY}, {DATE}. Please be online around 15:00 ST / 17:00 UK / 18:00 CET and ask in alliance chat for the train to be assigned. If anything comes up, please reach out early. Set a reminder in your phone so you're prepared. Thanks for helping the alliance!",
	}

	type DayMessage struct {
		Day     string `json:"day"`
		Name    string `json:"name"`
		Message string `json:"message"`
	}

	var messages []DayMessage
	templateIndex := 0

	for rows.Next() {
		var date, conductor string
		if err := rows.Scan(&date, &conductor); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		dateObj, _ := parseDate(date)
		dayName := dateObj.Format("Monday")
		dateFormatted := dateObj.Format("2 Jan")

		template := messageTemplates[templateIndex]
		templateIndex = (templateIndex + 1) % len(messageTemplates)

		message := strings.ReplaceAll(template, "{NAME}", conductor)
		message = strings.ReplaceAll(message, "{DAY}", dayName)
		message = strings.ReplaceAll(message, "{DATE}", dateFormatted)

		messages = append(messages, DayMessage{
			Day:     dayName,
			Name:    conductor,
			Message: message,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": messages,
	})
}
