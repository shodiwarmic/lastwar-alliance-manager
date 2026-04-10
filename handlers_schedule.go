package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

var reHHMM = regexp.MustCompile(`^\d{2}:\d{2}$`)

// --- Shared helpers ---

// validateZSCooldown checks whether placing a ZS event at newDate/newTime violates the
// 71.5-hour cooldown from the most recent existing ZS event (excluding excludeID).
// Returns the next eligible game-time datetime if cooldown not met, or zero time if OK.
func validateZSCooldown(excludeID int, newDate, newTime string) (time.Time, error) {
	var lastDate, lastTime string
	err := db.QueryRow(`
		SELECT se.event_date, se.event_time
		FROM schedule_events se
		JOIN schedule_event_types t ON t.id = se.event_type_id
		WHERE t.short_name = 'ZS' AND se.id != ?
		  AND (se.event_date < ? OR (se.event_date = ? AND se.event_time < ?))
		ORDER BY se.event_date DESC, se.event_time DESC
		LIMIT 1`, excludeID, newDate, newDate, newTime).Scan(&lastDate, &lastTime)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}

	lastTime = strings.TrimSpace(lastTime)
	if !reHHMM.MatchString(lastTime) {
		lastTime = "00:00"
	}
	lastDT, err := time.Parse("2006-01-02 15:04", lastDate+" "+lastTime)
	if err != nil {
		return time.Time{}, err
	}
	newDT, err := time.Parse("2006-01-02 15:04", newDate+" "+newTime)
	if err != nil {
		return time.Time{}, err
	}

	const cooldown = 71*time.Hour + 30*time.Minute
	if newDT.Sub(lastDT) < cooldown {
		return lastDT.Add(cooldown), nil
	}
	return time.Time{}, nil
}

// getSystemBaseline returns the baseline level for MG or ZS from the settings singleton.
func getSystemBaseline(shortName string) (int, error) {
	col := "mg_baseline"
	if shortName == "ZS" {
		col = "zs_baseline"
	}
	var baseline int
	// #nosec G202 — col is only ever "mg_baseline" or "zs_baseline", not user input
	err := db.QueryRow("SELECT "+col+" FROM settings WHERE id=1").Scan(&baseline)
	return baseline, err
}

// --- Event Type Handlers ---

func getScheduleEventTypes(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT id, name, short_name, icon, is_system, active, sort_order, created_at
		FROM schedule_event_types ORDER BY sort_order, id`)
	if err != nil {
		slog.Error("getScheduleEventTypes query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	types := []ScheduleEventType{}
	for rows.Next() {
		var t ScheduleEventType
		var isSystem, active int
		if err := rows.Scan(&t.ID, &t.Name, &t.ShortName, &t.Icon, &isSystem, &active, &t.SortOrder, &t.CreatedAt); err != nil {
			slog.Error("getScheduleEventTypes scan", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		t.IsSystem = isSystem == 1
		t.Active = active == 1
		types = append(types, t)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types)
}

func createScheduleEventType(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		ShortName string `json:"short_name"`
		Icon      string `json:"icon"`
		SortOrder int    `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.ShortName = strings.TrimSpace(req.ShortName)
	if req.Name == "" || req.ShortName == "" {
		http.Error(w, "name and short_name are required", http.StatusBadRequest)
		return
	}
	if req.Icon == "" {
		req.Icon = "📅"
	}

	res, err := db.Exec(`
		INSERT INTO schedule_event_types (name, short_name, icon, is_system, active, sort_order)
		VALUES (?, ?, ?, 0, 1, ?)`,
		req.Name, req.ShortName, req.Icon, req.SortOrder)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			http.Error(w, "An event type with that name or short name already exists", http.StatusConflict)
			return
		}
		slog.Error("createScheduleEventType insert", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)
	logActivity(userID, username, "created", "schedule_event_type", req.Name, false)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func updateScheduleEventType(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var req struct {
		Name      string `json:"name"`
		ShortName string `json:"short_name"`
		Icon      string `json:"icon"`
		Active    *bool  `json:"active"`
		SortOrder int    `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var old ScheduleEventType
	var isSystem, active int
	err := db.QueryRow(`SELECT name, short_name, icon, is_system, active, sort_order FROM schedule_event_types WHERE id=?`, id).
		Scan(&old.Name, &old.ShortName, &old.Icon, &isSystem, &active, &old.SortOrder)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("updateScheduleEventType fetch", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	old.IsSystem = isSystem == 1
	old.Active = active == 1

	req.Name = strings.TrimSpace(req.Name)
	req.ShortName = strings.TrimSpace(req.ShortName)

	if old.IsSystem {
		if req.Name != "" && req.Name != old.Name {
			http.Error(w, "Cannot rename a system event type", http.StatusBadRequest)
			return
		}
		if req.ShortName != "" && req.ShortName != old.ShortName {
			http.Error(w, "Cannot change short_name of a system event type", http.StatusBadRequest)
			return
		}
		req.Name = old.Name
		req.ShortName = old.ShortName
	}
	if req.Name == "" {
		req.Name = old.Name
	}
	if req.ShortName == "" {
		req.ShortName = old.ShortName
	}
	if req.Icon == "" {
		req.Icon = old.Icon
	}

	newActive := old.Active
	if req.Active != nil {
		newActive = *req.Active
	}
	activeInt := 0
	if newActive {
		activeInt = 1
	}

	_, err = db.Exec(`
		UPDATE schedule_event_types SET name=?, short_name=?, icon=?, active=?, sort_order=? WHERE id=?`,
		req.Name, req.ShortName, req.Icon, activeInt, req.SortOrder, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			http.Error(w, "An event type with that name or short name already exists", http.StatusConflict)
			return
		}
		slog.Error("updateScheduleEventType update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var changes []string
	if old.Name != req.Name {
		changes = append(changes, "name: "+old.Name+" → "+req.Name)
	}
	if old.Icon != req.Icon {
		changes = append(changes, "icon: "+old.Icon+" → "+req.Icon)
	}
	if old.Active != newActive {
		changes = append(changes, fmt.Sprintf("active: %v → %v", old.Active, newActive))
	}

	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)
	logActivity(userID, username, "updated", "schedule_event_type", req.Name, false, strings.Join(changes, "; "))

	w.WriteHeader(http.StatusNoContent)
}

func deleteScheduleEventType(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	var name string
	var isSystem int
	err := db.QueryRow(`SELECT name, is_system FROM schedule_event_types WHERE id=?`, id).Scan(&name, &isSystem)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("deleteScheduleEventType fetch", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if isSystem == 1 {
		http.Error(w, "System event types cannot be deleted", http.StatusConflict)
		return
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM schedule_events WHERE event_type_id=?`, id).Scan(&count)
	if count > 0 {
		http.Error(w, "Cannot delete: event type is used by existing events", http.StatusConflict)
		return
	}

	if _, err = db.Exec(`DELETE FROM schedule_event_types WHERE id=?`, id); err != nil {
		slog.Error("deleteScheduleEventType delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)
	logActivity(userID, username, "deleted", "schedule_event_type", name, false)

	w.WriteHeader(http.StatusNoContent)
}

// --- Calendar Event Handlers ---

func getScheduleEvents(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		http.Error(w, "from and to are required", http.StatusBadRequest)
		return
	}
	fromT, err1 := time.Parse("2006-01-02", from)
	toT, err2 := time.Parse("2006-01-02", to)
	if err1 != nil || err2 != nil {
		http.Error(w, "from and to must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	if toT.Sub(fromT) > 42*24*time.Hour {
		http.Error(w, "Date range cannot exceed 42 days", http.StatusBadRequest)
		return
	}

	rows, err := db.Query(`
		SELECT se.id, se.event_date, se.event_type_id,
		       t.name, t.short_name, t.icon, t.is_system,
		       se.event_time, se.all_day, se.level, COALESCE(se.notes,''),
		       se.created_by, se.created_at, se.updated_at
		FROM schedule_events se
		JOIN schedule_event_types t ON t.id = se.event_type_id
		WHERE se.event_date >= ? AND se.event_date <= ?
		ORDER BY se.event_date, se.all_day DESC, se.event_time`, from, to)
	if err != nil {
		slog.Error("getScheduleEvents query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	events := []ScheduleEvent{}
	for rows.Next() {
		var ev ScheduleEvent
		var isSystem, allDay int
		if err := rows.Scan(
			&ev.ID, &ev.EventDate, &ev.EventTypeID,
			&ev.TypeName, &ev.TypeShort, &ev.TypeIcon, &isSystem,
			&ev.EventTime, &allDay, &ev.Level, &ev.Notes,
			&ev.CreatedBy, &ev.CreatedAt, &ev.UpdatedAt,
		); err != nil {
			slog.Error("getScheduleEvents scan", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		ev.IsSystem = isSystem == 1
		ev.AllDay = allDay == 1
		events = append(events, ev)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

func createScheduleEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EventDate   string `json:"event_date"`
		EventTypeID int    `json:"event_type_id"`
		EventTime   string `json:"event_time"`
		AllDay      bool   `json:"all_day"`
		Level       *int   `json:"level"`
		Notes       string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if _, err := time.Parse("2006-01-02", req.EventDate); err != nil {
		http.Error(w, "event_date must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	if req.AllDay {
		req.EventTime = "00:00"
	} else if !reHHMM.MatchString(req.EventTime) {
		http.Error(w, "event_time must be HH:MM", http.StatusBadRequest)
		return
	}
	if req.EventTypeID == 0 {
		http.Error(w, "event_type_id is required", http.StatusBadRequest)
		return
	}

	var typeName, typeShort string
	var isSystem int
	err := db.QueryRow(`SELECT name, short_name, is_system FROM schedule_event_types WHERE id=?`, req.EventTypeID).
		Scan(&typeName, &typeShort, &isSystem)
	if err == sql.ErrNoRows {
		http.Error(w, "event_type_id not found", http.StatusBadRequest)
		return
	}
	if err != nil {
		slog.Error("createScheduleEvent lookup type", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if isSystem == 1 {
		if typeShort == "MG" && req.EventTime >= "22:00" {
			http.Error(w, "MG must start by 21:59 ST", http.StatusBadRequest)
			return
		}
		if typeShort == "ZS" {
			nextEligible, err := validateZSCooldown(0, req.EventDate, req.EventTime)
			if err != nil {
				slog.Error("createScheduleEvent validateZSCooldown", "error", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			if !nextEligible.IsZero() {
				http.Error(w, "ZS cooldown not elapsed — next eligible: "+nextEligible.Format("2006-01-02 15:04")+" ST", http.StatusBadRequest)
				return
			}
		}
		if req.Level == nil {
			baseline, err := getSystemBaseline(typeShort)
			if err != nil {
				slog.Error("createScheduleEvent getSystemBaseline", "error", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			req.Level = &baseline
		}
	}

	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)
	now := time.Now().UTC().Format(time.RFC3339)

	allDayInt := 0
	if req.AllDay {
		allDayInt = 1
	}
	res, err := db.Exec(`
		INSERT INTO schedule_events (event_date, event_type_id, event_time, all_day, level, notes, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.EventDate, req.EventTypeID, req.EventTime, allDayInt, req.Level, req.Notes, userID, now, now)
	if err != nil {
		slog.Error("createScheduleEvent insert", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	logActivity(userID, username, "created", "schedule_event", typeName+" "+req.EventDate, false)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func updateScheduleEvent(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var req struct {
		EventDate   string `json:"event_date"`
		EventTypeID int    `json:"event_type_id"`
		EventTime   string `json:"event_time"`
		AllDay      bool   `json:"all_day"`
		Level       *int   `json:"level"`
		Notes       string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var old ScheduleEvent
	var isSystem, oldAllDay int
	err := db.QueryRow(`
		SELECT se.event_date, se.event_type_id, t.short_name, t.is_system,
		       se.event_time, se.all_day, se.level, COALESCE(se.notes,'')
		FROM schedule_events se
		JOIN schedule_event_types t ON t.id = se.event_type_id
		WHERE se.id=?`, id).
		Scan(&old.EventDate, &old.EventTypeID, &old.TypeShort, &isSystem,
			&old.EventTime, &oldAllDay, &old.Level, &old.Notes)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("updateScheduleEvent fetch", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	old.IsSystem = isSystem == 1
	old.AllDay = oldAllDay == 1

	if req.EventDate == "" {
		req.EventDate = old.EventDate
	} else if _, err := time.Parse("2006-01-02", req.EventDate); err != nil {
		http.Error(w, "event_date must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	if req.AllDay {
		req.EventTime = "00:00"
	} else if req.EventTime == "" {
		req.EventTime = old.EventTime
	} else if !reHHMM.MatchString(req.EventTime) {
		http.Error(w, "event_time must be HH:MM", http.StatusBadRequest)
		return
	}
	if req.EventTypeID == 0 {
		req.EventTypeID = old.EventTypeID
	}

	var typeName, typeShort string
	var newIsSystem int
	if err := db.QueryRow(`SELECT name, short_name, is_system FROM schedule_event_types WHERE id=?`, req.EventTypeID).
		Scan(&typeName, &typeShort, &newIsSystem); err != nil {
		http.Error(w, "event_type_id not found", http.StatusBadRequest)
		return
	}

	if newIsSystem == 1 {
		if typeShort == "MG" && req.EventTime >= "22:00" {
			http.Error(w, "MG must start by 21:59 ST", http.StatusBadRequest)
			return
		}
		if typeShort == "ZS" {
			nextEligible, err := validateZSCooldown(id, req.EventDate, req.EventTime)
			if err != nil {
				slog.Error("updateScheduleEvent validateZSCooldown", "error", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			if !nextEligible.IsZero() {
				http.Error(w, "ZS cooldown not elapsed — next eligible: "+nextEligible.Format("2006-01-02 15:04")+" ST", http.StatusBadRequest)
				return
			}
		}
		// Keep existing level when not provided
		if req.Level == nil {
			req.Level = old.Level
		}
	}

	newAllDayInt := 0
	if req.AllDay {
		newAllDayInt = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err = db.Exec(`
		UPDATE schedule_events SET event_date=?, event_type_id=?, event_time=?, all_day=?, level=?, notes=?, updated_at=? WHERE id=?`,
		req.EventDate, req.EventTypeID, req.EventTime, newAllDayInt, req.Level, req.Notes, now, id); err != nil {
		slog.Error("updateScheduleEvent update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var changes []string
	if old.EventDate != req.EventDate {
		changes = append(changes, "date: "+old.EventDate+" → "+req.EventDate)
	}
	if old.EventTime != req.EventTime {
		changes = append(changes, "time: "+old.EventTime+" → "+req.EventTime)
	}
	if old.Level != nil && req.Level != nil && *old.Level != *req.Level {
		changes = append(changes, fmt.Sprintf("level: %d → %d", *old.Level, *req.Level))
	}
	if old.Notes != req.Notes {
		changes = append(changes, "notes updated")
	}

	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)
	logActivity(userID, username, "updated", "schedule_event", typeName+" "+req.EventDate, false, strings.Join(changes, "; "))

	w.WriteHeader(http.StatusNoContent)
}

func deleteScheduleEvent(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	var typeName, eventDate string
	err := db.QueryRow(`
		SELECT t.name, se.event_date FROM schedule_events se
		JOIN schedule_event_types t ON t.id = se.event_type_id
		WHERE se.id=?`, id).Scan(&typeName, &eventDate)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("deleteScheduleEvent fetch", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if _, err = db.Exec(`DELETE FROM schedule_events WHERE id=?`, id); err != nil {
		slog.Error("deleteScheduleEvent delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)
	logActivity(userID, username, "deleted", "schedule_event", typeName+" "+eventDate, false)

	w.WriteHeader(http.StatusNoContent)
}

// --- Event Generation Handler ---

func generateScheduleEvents(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From  string   `json:"from"`
		To    string   `json:"to"`
		Types []string `json:"types"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	fromT, err1 := time.Parse("2006-01-02", req.From)
	toT, err2 := time.Parse("2006-01-02", req.To)
	if err1 != nil || err2 != nil {
		http.Error(w, "from and to must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	if toT.Before(fromT) {
		http.Error(w, "to must be on or after from", http.StatusBadRequest)
		return
	}
	if toT.Sub(fromT) > 90*24*time.Hour {
		http.Error(w, "Date range cannot exceed 90 days", http.StatusBadRequest)
		return
	}

	var s Settings
	err := db.QueryRow(`SELECT mg_baseline, zs_baseline, mg_default_time, zs_default_time,
		COALESCE(mg_anchor_date,''), zs_schedule_mode, zs_weekdays,
		COALESCE(zs_anchor_date,''), zs_anchor_time FROM settings WHERE id=1`).
		Scan(&s.MGBaseline, &s.ZSBaseline, &s.MGDefaultTime, &s.ZSDefaultTime,
			&s.MGAnchorDate, &s.ZSScheduleMode, &s.ZSWeekdays, &s.ZSAnchorDate, &s.ZSAnchorTime)
	if err != nil {
		slog.Error("generateScheduleEvents settings", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var mgTypeID, zsTypeID int
	db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name='MG'`).Scan(&mgTypeID)
	db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name='ZS'`).Scan(&zsTypeID)

	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)
	now := time.Now().UTC().Format(time.RFC3339)

	genTypes := map[string]bool{}
	for _, t := range req.Types {
		genTypes[strings.ToLower(t)] = true
	}

	mgCreated := 0
	zsCreated := 0

	// --- MG generation: every other day from anchor ---
	if genTypes["mg"] && mgTypeID > 0 && s.MGAnchorDate != "" {
		anchorT, err := time.Parse("2006-01-02", s.MGAnchorDate)
		if err == nil {
			// Find first valid date >= fromT in the every-other-day sequence from anchorT
			diffDays := int(fromT.Sub(anchorT) / (24 * time.Hour))
			remainder := ((diffDays % 2) + 2) % 2 // always 0 or 1, handles negative diffDays
			firstDate := fromT
			if remainder != 0 {
				firstDate = fromT.AddDate(0, 0, 1)
			}
			for d := firstDate; !d.After(toT); d = d.AddDate(0, 0, 2) {
				dateStr := d.Format("2006-01-02")
				var exists int
				db.QueryRow(`SELECT COUNT(*) FROM schedule_events WHERE event_type_id=? AND event_date=?`, mgTypeID, dateStr).Scan(&exists)
				if exists > 0 {
					continue
				}
				if _, err := db.Exec(`
					INSERT INTO schedule_events (event_date, event_type_id, event_time, level, notes, created_by, created_at, updated_at)
					VALUES (?, ?, ?, ?, '', ?, ?, ?)`,
					dateStr, mgTypeID, s.MGDefaultTime, s.MGBaseline, userID, now, now); err != nil {
					slog.Error("generateScheduleEvents MG insert", "error", err, "date", dateStr)
					continue
				}
				mgCreated++
			}
		}
	}

	// --- ZS generation ---
	if genTypes["zs"] && zsTypeID > 0 {
		switch s.ZSScheduleMode {
		case "weekdays":
			wdSet := map[int]bool{}
			for _, seg := range strings.Split(s.ZSWeekdays, ",") {
				if n, err := strconv.Atoi(strings.TrimSpace(seg)); err == nil && n >= 1 && n <= 7 {
					wdSet[n] = true
				}
			}
			for d := fromT; !d.After(toT); d = d.AddDate(0, 0, 1) {
				goWD := int(d.Weekday()) // Sun=0…Sat=6
				planWD := goWD
				if goWD == 0 {
					planWD = 7
				}
				if !wdSet[planWD] {
					continue
				}
				dateStr := d.Format("2006-01-02")
				var exists int
				db.QueryRow(`SELECT COUNT(*) FROM schedule_events WHERE event_type_id=? AND event_date=?`, zsTypeID, dateStr).Scan(&exists)
				if exists > 0 {
					continue
				}
				if _, err := db.Exec(`
					INSERT INTO schedule_events (event_date, event_type_id, event_time, level, notes, created_by, created_at, updated_at)
					VALUES (?, ?, ?, ?, '', ?, ?, ?)`,
					dateStr, zsTypeID, s.ZSDefaultTime, s.ZSBaseline, userID, now, now); err != nil {
					slog.Error("generateScheduleEvents ZS weekday insert", "error", err, "date", dateStr)
					continue
				}
				zsCreated++
			}

		case "asap":
			if s.ZSAnchorDate != "" {
				const zsCooldown = 71*time.Hour + 30*time.Minute
				anchorDT, err := time.Parse("2006-01-02 15:04", s.ZSAnchorDate+" "+s.ZSAnchorTime)
				if err == nil {
					// Advance to first occurrence on or after fromT
					cur := anchorDT
					if fromT.After(anchorDT) {
						steps := int64(fromT.Sub(anchorDT) / zsCooldown)
						cur = anchorDT.Add(time.Duration(steps) * zsCooldown)
						if cur.Before(fromT) {
							cur = cur.Add(zsCooldown)
						}
					}
					for ; ; cur = cur.Add(zsCooldown) {
						dateStr := cur.Format("2006-01-02")
						if dateStr > req.To {
							break
						}
						if dateStr < req.From {
							continue
						}
						var exists int
						db.QueryRow(`SELECT COUNT(*) FROM schedule_events WHERE event_type_id=? AND event_date=?`, zsTypeID, dateStr).Scan(&exists)
						if exists > 0 {
							continue
						}
						if _, err := db.Exec(`
							INSERT INTO schedule_events (event_date, event_type_id, event_time, level, notes, created_by, created_at, updated_at)
							VALUES (?, ?, ?, ?, '', ?, ?, ?)`,
							dateStr, zsTypeID, s.ZSDefaultTime, s.ZSBaseline, userID, now, now); err != nil {
							slog.Error("generateScheduleEvents ZS asap insert", "error", err, "date", dateStr)
							continue
						}
						zsCreated++
					}
				}
			}
		}
	}

	if mgCreated+zsCreated > 0 {
		logActivity(userID, username, "created", "schedule_event",
			fmt.Sprintf("%d events generated", mgCreated+zsCreated), false,
			fmt.Sprintf("MG: %d, ZS: %d", mgCreated, zsCreated))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"mg_created": mgCreated,
		"zs_created": zsCreated,
	})
}

// --- Server Event Handlers ---

func getServerEvents(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT id, name, short_name, icon, duration_days, repeat_type,
		       repeat_interval, repeat_weekday, COALESCE(anchor_date,''), active, sort_order, created_at, updated_at
		FROM server_events ORDER BY sort_order, id`)
	if err != nil {
		slog.Error("getServerEvents query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	events := []ServerEvent{}
	for rows.Next() {
		var ev ServerEvent
		var active int
		if err := rows.Scan(&ev.ID, &ev.Name, &ev.ShortName, &ev.Icon,
			&ev.DurationDays, &ev.RepeatType, &ev.RepeatInterval, &ev.RepeatWeekday,
			&ev.AnchorDate, &active, &ev.SortOrder, &ev.CreatedAt, &ev.UpdatedAt,
		); err != nil {
			slog.Error("getServerEvents scan", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		ev.Active = active == 1
		events = append(events, ev)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

func createServerEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string `json:"name"`
		ShortName      string `json:"short_name"`
		Icon           string `json:"icon"`
		DurationDays   int    `json:"duration_days"`
		RepeatType     string `json:"repeat_type"`
		RepeatInterval *int   `json:"repeat_interval"`
		RepeatWeekday  *int   `json:"repeat_weekday"`
		AnchorDate     string `json:"anchor_date"`
		Active         *bool  `json:"active"`
		SortOrder      int    `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.DurationDays < 1 {
		req.DurationDays = 1
	}
	if req.Icon == "" {
		req.Icon = "🌐"
	}
	validRepeat := map[string]bool{"none": true, "weekly": true, "biweekly": true, "every_n_days": true}
	if !validRepeat[req.RepeatType] {
		req.RepeatType = "none"
	}
	active := 1
	if req.Active != nil && !*req.Active {
		active = 0
	}
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := db.Exec(`
		INSERT INTO server_events (name, short_name, icon, duration_days, repeat_type, repeat_interval, repeat_weekday, anchor_date, active, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Name, req.ShortName, req.Icon, req.DurationDays, req.RepeatType,
		req.RepeatInterval, req.RepeatWeekday, req.AnchorDate, active, req.SortOrder, now, now)
	if err != nil {
		slog.Error("createServerEvent insert", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)
	logActivity(userID, username, "created", "server_event", req.Name, false)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func updateServerEvent(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var req struct {
		Name           string `json:"name"`
		ShortName      string `json:"short_name"`
		Icon           string `json:"icon"`
		DurationDays   int    `json:"duration_days"`
		RepeatType     string `json:"repeat_type"`
		RepeatInterval *int   `json:"repeat_interval"`
		RepeatWeekday  *int   `json:"repeat_weekday"`
		AnchorDate     string `json:"anchor_date"`
		Active         *bool  `json:"active"`
		SortOrder      int    `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var old ServerEvent
	var active int
	err := db.QueryRow(`
		SELECT name, short_name, icon, duration_days, repeat_type,
		       repeat_interval, repeat_weekday, COALESCE(anchor_date,''), active, sort_order
		FROM server_events WHERE id=?`, id).
		Scan(&old.Name, &old.ShortName, &old.Icon, &old.DurationDays, &old.RepeatType,
			&old.RepeatInterval, &old.RepeatWeekday, &old.AnchorDate, &active, &old.SortOrder)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("updateServerEvent fetch", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	old.Active = active == 1

	if req.Name == "" {
		req.Name = old.Name
	}
	if req.ShortName == "" {
		req.ShortName = old.ShortName
	}
	if req.Icon == "" {
		req.Icon = old.Icon
	}
	if req.DurationDays < 1 {
		req.DurationDays = old.DurationDays
	}
	if req.RepeatType == "" {
		req.RepeatType = old.RepeatType
	}
	if req.RepeatInterval == nil {
		req.RepeatInterval = old.RepeatInterval
	}
	if req.RepeatWeekday == nil {
		req.RepeatWeekday = old.RepeatWeekday
	}
	newActive := old.Active
	if req.Active != nil {
		newActive = *req.Active
	}
	activeInt := 0
	if newActive {
		activeInt = 1
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err = db.Exec(`
		UPDATE server_events SET name=?, short_name=?, icon=?, duration_days=?, repeat_type=?,
		repeat_interval=?, repeat_weekday=?, anchor_date=?, active=?, sort_order=?, updated_at=? WHERE id=?`,
		req.Name, req.ShortName, req.Icon, req.DurationDays, req.RepeatType,
		req.RepeatInterval, req.RepeatWeekday, req.AnchorDate, activeInt, req.SortOrder, now, id); err != nil {
		slog.Error("updateServerEvent update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var changes []string
	if old.Name != req.Name {
		changes = append(changes, "name: "+old.Name+" → "+req.Name)
	}
	if old.AnchorDate != req.AnchorDate {
		changes = append(changes, "anchor: "+old.AnchorDate+" → "+req.AnchorDate)
	}
	if old.Active != newActive {
		changes = append(changes, fmt.Sprintf("active: %v → %v", old.Active, newActive))
	}

	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)
	logActivity(userID, username, "updated", "server_event", req.Name, false, strings.Join(changes, "; "))

	w.WriteHeader(http.StatusNoContent)
}

func deleteServerEvent(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	var name string
	err := db.QueryRow(`SELECT name FROM server_events WHERE id=?`, id).Scan(&name)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("deleteServerEvent fetch", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if _, err = db.Exec(`DELETE FROM server_events WHERE id=?`, id); err != nil {
		slog.Error("deleteServerEvent delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)
	logActivity(userID, username, "deleted", "server_event", name, false)

	w.WriteHeader(http.StatusNoContent)
}
