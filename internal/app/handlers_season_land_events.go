// handlers_season_land_events.go — what happened to the season's land and what has to go
// (private-docs 275).
//
// Events are observations (captured, abandoned, lost at an instant) from the web form, a CSV
// import or a phone; holdings are derived from them, never stored. Drops are the plan for
// making room: a holding to abandon, the target it makes room for, and when the abandoning
// must start. A route save or activation keeps the calendar in step — one Land Capture row
// per planned target on the active route, one Land Abandonment row per open drop — through
// syncSourcedScheduleEvent.

package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

var validLandActions = map[string]bool{"captured": true, "abandoned": true, "lost": true}
var validDropStatuses = map[string]bool{"drop_required": true, "assigned": true, "abandoning": true, "complete": true, "cancelled": true}

// landEventIn is one event as a form, CSV row or phone sends it. At is server time
// ("YYYY-MM-DD HH:MM") or RFC 3339.
type landEventIn struct {
	AreaID string `json:"area_id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Level  *int   `json:"level"`
	Action string `json:"action"`
	At     string `json:"at"`
	Note   string `json:"note"`
}

// parseLandInstant reads an instant in either shape a client sends.
func parseLandInstant(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return parseLandST(s)
}

// normaliseLandEvent validates an event and fills its name, kind and level from the season's
// map lookup when it names an area by ID. The returned string is the refusal, "" when valid.
func normaliseLandEvent(seasonNumber int, in landEventIn, now time.Time) (landEventIn, time.Time, string) {
	in.AreaID, in.Name = strings.TrimSpace(in.AreaID), strings.TrimSpace(in.Name)
	in.Kind, in.Action = strings.ToLower(strings.TrimSpace(in.Kind)), strings.ToLower(strings.TrimSpace(in.Action))
	if in.AreaID != "" {
		if a, ok := landAreaByID(seasonNumber, in.AreaID); ok {
			if in.Name == "" {
				in.Name = a.Name
			}
			if in.Kind == "" {
				in.Kind = a.Kind
			}
			if in.Level == nil {
				lv := a.Level
				in.Level = &lv
			}
		}
	}
	if in.Name == "" {
		return in, time.Time{}, "an event needs an area name or a map ID"
	}
	if in.Kind == "" {
		in.Kind = "city"
	}
	if !validLandKinds[in.Kind] {
		return in, time.Time{}, "kind must be city, stronghold, capitol, trade_post or landmark"
	}
	if !validLandActions[in.Action] {
		return in, time.Time{}, "action must be captured, abandoned or lost"
	}
	if in.Level != nil && (*in.Level < 1 || *in.Level > 20) {
		return in, time.Time{}, "level must be from 1 to 20"
	}
	at := now
	if strings.TrimSpace(in.At) != "" {
		t, err := parseLandInstant(in.At)
		if err != nil {
			return in, time.Time{}, "the time must be YYYY-MM-DD HH:MM (server time)"
		}
		at = t
	}
	if at.After(now.Add(10 * time.Minute)) {
		return in, time.Time{}, "the time is in the future — an event records what already happened"
	}
	in.Note = strings.TrimSpace(in.Note)
	return in, at, ""
}

// writeLandEventTx appends one event; a row for the same area, action and instant already
// there makes it unchanged (false).
func writeLandEventTx(tx *sql.Tx, seasonID int, in landEventIn, at time.Time, source string, userID int) (bool, error) {
	stored := landUTC(at)
	var n int
	var err error
	if in.AreaID != "" {
		err = tx.QueryRow(`SELECT COUNT(*) FROM season_land_events WHERE season_id = ? AND area_id = ? AND action = ? AND at = ?`,
			seasonID, in.AreaID, in.Action, stored).Scan(&n)
	} else {
		err = tx.QueryRow(`SELECT COUNT(*) FROM season_land_events WHERE season_id = ? AND area_id = '' AND kind = ?
			AND LOWER(name) = LOWER(?) AND action = ? AND at = ?`, seasonID, in.Kind, in.Name, in.Action, stored).Scan(&n)
	}
	if err != nil || n > 0 {
		return false, err
	}
	var by any
	if userID > 0 {
		by = userID
	}
	_, err = tx.Exec(`INSERT INTO season_land_events (season_id, area_id, name, kind, level, action, at, source, recorded_by, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, seasonID, in.AreaID, in.Name, in.Kind, in.Level, in.Action, stored,
		source, by, in.Note)
	return err == nil, err
}

// loadLandEvents reads a season's events, oldest first.
func loadLandEvents(q rowQueryer, seasonID int) ([]landEvent, error) {
	rows, err := q.Query(`SELECT id, area_id, name, kind, level, action, at, source, note FROM season_land_events
		WHERE season_id = ? ORDER BY at, id`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []landEvent{}
	for rows.Next() {
		var e landEvent
		var at string
		if err := rows.Scan(&e.ID, &e.AreaID, &e.Name, &e.Kind, &e.Level, &e.Action, &at, &e.Source, &e.Note); err != nil {
			return nil, err
		}
		if t := parseLandStored(at); t != nil {
			e.At = *t
			out = append(out, e)
		}
	}
	return out, rows.Err()
}

// landEventView is an event as the API carries it.
type landEventView struct {
	landEvent
	AtST string `json:"at"`
}

// landDrop is a planned abandonment, instants in server time.
type landDrop struct {
	ID                  int    `json:"id"`
	HoldingAreaID       string `json:"holding_area_id"`
	HoldingName         string `json:"holding_name"`
	Kind                string `json:"kind"`
	Level               *int   `json:"level"`
	ReplacementTargetID *int   `json:"replacement_target_id"`
	ReplacementName     string `json:"replacement_name"`
	ReplacementAt       string `json:"replacement_at"`
	MustStartBy         string `json:"must_start_by"`
	Status              string `json:"status"`
	AssignedMemberID    *int   `json:"assigned_member_id"`
	AssignedMemberName  string `json:"assigned_member_name"`
	Notes               string `json:"notes"`
	ScheduleEventID     *int   `json:"schedule_event_id"`
	mustStartBy         *time.Time
}

func loadLandDrops(q rowQueryer, seasonID int) ([]landDrop, error) {
	rows, err := q.Query(`SELECT d.id, d.holding_area_id, d.holding_name, d.kind, d.level, d.replacement_target_id,
		COALESCE(t.name, ''), COALESCE(d.replacement_at, ''), COALESCE(d.must_start_by, ''), d.status,
		d.assigned_member_id, COALESCE(m.name, ''), d.notes, d.schedule_event_id
		FROM season_land_drops d
		LEFT JOIN season_land_targets t ON t.id = d.replacement_target_id
		LEFT JOIN members m ON m.id = d.assigned_member_id
		WHERE d.season_id = ? ORDER BY d.status = 'cancelled', d.must_start_by IS NULL, d.must_start_by, d.id`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []landDrop{}
	for rows.Next() {
		var d landDrop
		var repl, must string
		if err := rows.Scan(&d.ID, &d.HoldingAreaID, &d.HoldingName, &d.Kind, &d.Level, &d.ReplacementTargetID,
			&d.ReplacementName, &repl, &must, &d.Status, &d.AssignedMemberID, &d.AssignedMemberName, &d.Notes,
			&d.ScheduleEventID); err != nil {
			return nil, err
		}
		d.ReplacementAt = landST(parseLandStored(repl))
		d.mustStartBy = parseLandStored(must)
		d.MustStartBy = landST(d.mustStartBy)
		out = append(out, d)
	}
	return out, rows.Err()
}

// loadLandExtras is the Land tab's holdings, events, drops and daily cards.
func loadLandExtras(q rowQueryer, s *Season, cfg landConfig, routes []landRoute, now time.Time) (map[string]any, error) {
	events, err := loadLandEvents(q, s.ID)
	if err != nil {
		return nil, err
	}
	drops, err := loadLandDrops(q, s.ID)
	if err != nil {
		return nil, err
	}
	held := landHoldingsAt(events, now)
	var captures []landTarget
	for _, r := range routes {
		if r.IsActive {
			for _, tv := range r.Targets {
				captures = append(captures, tv.landTarget)
			}
		}
	}
	var planned []landPlannedDrop
	for _, d := range drops {
		if d.Status != "complete" && d.Status != "cancelled" && d.mustStartBy != nil {
			planned = append(planned, landPlannedDrop{Kind: d.Kind, MustStartBy: *d.mustStartBy})
		}
	}
	doneToday := map[string]int{}
	today := now.In(gametimeLoc()).Format("2006-01-02")
	for _, e := range events {
		if e.Action == "captured" && e.At.In(gametimeLoc()).Format("2006-01-02") == today {
			if k := capacityKind(e.Kind, cfg.CapitolCountsTowardCap); k != "" {
				doneToday[k]++
			}
		}
	}
	recent := []landEventView{}
	for i := len(events) - 1; i >= 0 && len(recent) < 200; i-- {
		at := events[i].At
		recent = append(recent, landEventView{landEvent: events[i], AtST: landST(&at)})
	}
	return map[string]any{
		"holdings":    held,
		"events":      recent,
		"drops":       drops,
		"daily_cards": landDailyCards(held, captures, planned, doneToday, cfg, now),
		"now":         landST(&now),
	}, nil
}

// ── Calendar sync ──────────────────────────────────────────────────────────

// landCalendarStatus maps a target's status onto its calendar row's.
func landCalendarStatus(status string, planned *time.Time) string {
	if planned == nil {
		return "cancelled"
	}
	switch status {
	case "planned":
		return "tentative"
	case "declared":
		return "confirmed"
	case "captured":
		return "completed"
	}
	return "cancelled"
}

func dropCalendarStatus(status string, mustStartBy *time.Time) string {
	if mustStartBy == nil {
		return "cancelled"
	}
	switch status {
	case "drop_required":
		return "tentative"
	case "assigned", "abandoning":
		return "confirmed"
	case "complete":
		return "completed"
	}
	return "cancelled"
}

// stDateTime splits an instant into the calendar's game date and time.
func stDateTime(t *time.Time) (string, string) {
	if t == nil {
		return "", ""
	}
	st := t.In(gametimeLoc())
	return st.Format("2006-01-02"), st.Format("15:04")
}

// landCalendarResult is a sync's outcome named by what it was for, for the save's report.
type landCalendarResult struct {
	syncResult
	For string `json:"for"`
}

// syncLandCalendarTx brings every Land calendar row of a season into step: captured targets
// stamp their capture and write its event, each drop's must-start time follows its
// replacement, and the rows follow both. Only the active route reaches the calendar.
// Returns what changed (unchanged rows are left out).
func syncLandCalendarTx(tx *sql.Tx, s *Season, actorID int, now time.Time) ([]landCalendarResult, error) {
	cfg, err := loadLandConfig(tx, s.ID)
	if err != nil {
		return nil, err
	}
	routes, err := loadLandRoutes(tx, s.ID)
	if err != nil {
		return nil, err
	}
	captureType, err := sourcedTypeID(tx, "Land Capture")
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	dropType, err := sourcedTypeID(tx, "Land Abandonment")
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	var out []landCalendarResult
	keep := func(res syncResult, label string) {
		if res.Action != "unchanged" && res.Action != "none" {
			out = append(out, landCalendarResult{syncResult: res, For: label})
		}
	}
	for _, r := range routes {
		for _, tv := range r.Targets {
			t := tv.landTarget
			if t.Status == "captured" {
				at := now
				if t.ActualCaptureAt != nil {
					at = *t.ActualCaptureAt
				} else if _, err := tx.Exec(`UPDATE season_land_targets SET actual_capture_at = ? WHERE id = ?`, landUTC(now), t.ID); err != nil {
					return nil, err
				}
				ev := landEventIn{AreaID: t.AreaID, Name: t.Name, Kind: t.Kind, Level: t.Level, Action: "captured"}
				if _, err := writeLandEventTx(tx, s.ID, ev, at, "manual", actorID); err != nil {
					return nil, err
				}
			}
			status := landCalendarStatus(t.Status, t.PlannedCaptureAt)
			if !r.IsActive {
				status = "cancelled"
			}
			date, tm := stDateTime(t.PlannedCaptureAt)
			res, err := syncSourcedScheduleEvent(tx, sourcedEvent{Kind: sourceLandCapture, ID: int64(t.ID), SeasonID: s.ID,
				Date: date, Time: tm, TypeID: captureType, Notes: "Capture " + t.Name, Status: status}, actorID)
			if err != nil {
				return nil, err
			}
			keep(res, "Capture "+t.Name)
		}
	}
	drops, err := loadLandDrops(tx, s.ID)
	if err != nil {
		return nil, err
	}
	for _, d := range drops {
		var replAt *time.Time
		if d.ReplacementTargetID != nil {
			var planned sql.NullString
			if err := tx.QueryRow(`SELECT planned_capture_at FROM season_land_targets WHERE id = ?`, *d.ReplacementTargetID).Scan(&planned); err != nil && err != sql.ErrNoRows {
				return nil, err
			}
			replAt = parseLandStored(planned.String)
		}
		var must *time.Time
		if replAt != nil {
			m := replAt.Add(-time.Duration(cfg.AbandonMinutes) * time.Minute)
			must = &m
		}
		var replV, mustV any
		if replAt != nil {
			replV, mustV = landUTC(*replAt), landUTC(*must)
		}
		if landST(must) != d.MustStartBy || landST(replAt) != d.ReplacementAt {
			if _, err := tx.Exec(`UPDATE season_land_drops SET replacement_at = ?, must_start_by = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ?`, replV, mustV, d.ID); err != nil {
				return nil, err
			}
		}
		date, tm := stDateTime(must)
		notes := "Abandon " + d.HoldingName
		if d.ReplacementName != "" {
			notes += " for " + d.ReplacementName
		}
		if d.AssignedMemberName != "" {
			notes += " — " + d.AssignedMemberName
		}
		res, err := syncSourcedScheduleEvent(tx, sourcedEvent{Kind: sourceLandDrop, ID: int64(d.ID), SeasonID: s.ID,
			Date: date, Time: tm, TypeID: dropType, Notes: notes, Status: dropCalendarStatus(d.Status, must),
			LeadershipOnly: true}, actorID)
		if err != nil {
			return nil, err
		}
		if res.EventID != 0 && (d.ScheduleEventID == nil || int64(*d.ScheduleEventID) != res.EventID) {
			if _, err := tx.Exec(`UPDATE season_land_drops SET schedule_event_id = ? WHERE id = ?`, res.EventID, d.ID); err != nil {
				return nil, err
			}
		}
		keep(res, notes)
	}
	return out, nil
}

// afterLandRouteSaveTx keeps the calendar and the drops in step with a saved route.
func afterLandRouteSaveTx(tx *sql.Tx, s *Season, routeID, actorID int) ([]landCalendarResult, error) {
	return syncLandCalendarTx(tx, s, actorID, time.Now())
}

// landTargetRemovedTx cancels a removed target's calendar row.
func landTargetRemovedTx(tx *sql.Tx, id, actorID int) error {
	_, err := syncSourcedScheduleEvent(tx, sourcedEvent{Kind: sourceLandCapture, ID: int64(id), Status: "cancelled"}, actorID)
	return err
}

// landTargetInUseTx names what still points at a target, or "": an open drop's replacement.
func landTargetInUseTx(tx *sql.Tx, id int) (string, error) {
	var holding, target string
	err := tx.QueryRow(`SELECT d.holding_name, t.name FROM season_land_drops d JOIN season_land_targets t ON t.id = d.replacement_target_id
		WHERE d.replacement_target_id = ? AND d.status NOT IN ('complete', 'cancelled') LIMIT 1`, id).Scan(&holding, &target)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s is the replacement for the planned drop of %s — cancel or re-point that drop first", target, holding), nil
}

// ── Events ─────────────────────────────────────────────────────────────────

// POST /api/season-hub/land/events {season_id, area_id, name, kind, level, action, at, note}
func handleSeasonLandEventCreate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var body struct {
		SeasonID int `json:"season_id"`
		landEventIn
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	s := landSeason(w, body.SeasonID)
	if s == nil {
		return
	}
	if ok, msg := seasonAcceptsEdits(s.ID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	in, at, msg := normaliseLandEvent(s.SeasonNumber, body.landEventIn, time.Now())
	if msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleSeasonLandEventCreate: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	written, err := writeLandEventTx(tx, s.ID, in, at, "manual", user.ID)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		slog.Error("handleSeasonLandEventCreate: write", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if written {
		logActivity(user.ID, user.Username, "created", "season_land_event", in.Name+", "+seasonTagName(&s.ID), false,
			in.Action+" at "+landST(&at)+" ST")
	}
	writeJSON(w, map[string]any{"written": written})
}

// DELETE /api/season-hub/land/events/{id} — a mistaken event.
func handleSeasonLandEventDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}
	var seasonID int
	var name, action string
	if err := db.QueryRow(`SELECT season_id, name, action FROM season_land_events WHERE id = ?`, id).Scan(&seasonID, &name, &action); err == sql.ErrNoRows {
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	} else if err != nil {
		slog.Error("handleSeasonLandEventDelete: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if ok, msg := seasonAcceptsEdits(seasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	if _, err := db.Exec(`DELETE FROM season_land_events WHERE id = ?`, id); err != nil {
		slog.Error("handleSeasonLandEventDelete: delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "deleted", "season_land_event", name+", "+seasonTagName(&seasonID), false, action)
	writeJSON(w, map[string]string{"message": "Deleted"})
}

// ── Drops ──────────────────────────────────────────────────────────────────

type dropHoldingIn struct {
	AreaID string `json:"area_id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Level  *int   `json:"level"`
}

// landDropWriteTx runs a drop write and the calendar sync after it, then commits.
func landDropWriteTx(w http.ResponseWriter, s *Season, actorID int, write func(tx *sql.Tx) (string, error)) ([]landCalendarResult, bool) {
	tx, err := db.Begin()
	if err != nil {
		slog.Error("landDropWriteTx: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return nil, false
	}
	defer tx.Rollback()
	if msg, err := write(tx); err != nil {
		slog.Error("landDropWriteTx: write", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return nil, false
	} else if msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return nil, false
	}
	syncs, err := syncLandCalendarTx(tx, s, actorID, time.Now())
	if err != nil {
		slog.Error("landDropWriteTx: sync", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return nil, false
	}
	if err := tx.Commit(); err != nil {
		slog.Error("landDropWriteTx: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return nil, false
	}
	if syncs == nil {
		syncs = []landCalendarResult{}
	}
	return syncs, true
}

// replacementCheckTx refuses a replacement target that is not on one of the season's routes.
func replacementCheckTx(tx *sql.Tx, seasonID int, id *int) (string, error) {
	if id == nil {
		return "", nil
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM season_land_targets t JOIN season_land_routes r ON r.id = t.route_id
		WHERE t.id = ? AND r.season_id = ?`, *id, seasonID).Scan(&n); err != nil {
		return "", err
	}
	if n == 0 {
		return "the replacement target is not on one of this season's routes", nil
	}
	return "", nil
}

// POST /api/season-hub/land/drops {season_id, replacement_target_id, holdings: [...], notes} —
// one drop per holding picked.
func handleSeasonLandDropCreate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var body struct {
		SeasonID            int             `json:"season_id"`
		ReplacementTargetID *int            `json:"replacement_target_id"`
		Holdings            []dropHoldingIn `json:"holdings"`
		Notes               string          `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	s := landSeason(w, body.SeasonID)
	if s == nil {
		return
	}
	if ok, msg := seasonAcceptsEdits(s.ID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	if len(body.Holdings) == 0 {
		http.Error(w, "pick at least one holding to drop", http.StatusBadRequest)
		return
	}
	for i := range body.Holdings {
		h := &body.Holdings[i]
		h.Name, h.AreaID, h.Kind = strings.TrimSpace(h.Name), strings.TrimSpace(h.AreaID), strings.TrimSpace(h.Kind)
		if h.Name == "" {
			http.Error(w, "each holding needs a name", http.StatusBadRequest)
			return
		}
		if h.Kind == "" {
			h.Kind = "city"
		}
		if !validLandKinds[h.Kind] {
			http.Error(w, "a holding's kind is not a recognised land kind", http.StatusBadRequest)
			return
		}
	}
	syncs, ok := landDropWriteTx(w, s, user.ID, func(tx *sql.Tx) (string, error) {
		if msg, err := replacementCheckTx(tx, s.ID, body.ReplacementTargetID); msg != "" || err != nil {
			return msg, err
		}
		for _, h := range body.Holdings {
			if _, err := tx.Exec(`INSERT INTO season_land_drops (season_id, holding_area_id, holding_name, kind, level,
				replacement_target_id, notes, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
				s.ID, h.AreaID, h.Name, h.Kind, h.Level, body.ReplacementTargetID, strings.TrimSpace(body.Notes)); err != nil {
				return "", err
			}
		}
		return "", nil
	})
	if !ok {
		return
	}
	names := make([]string, 0, len(body.Holdings))
	for _, h := range body.Holdings {
		names = append(names, h.Name)
	}
	logActivity(user.ID, user.Username, "created", "season_land_drop", strings.Join(names, ", ")+", "+seasonTagName(&s.ID), false)
	writeJSON(w, map[string]any{"calendar": syncs})
}

// PUT /api/season-hub/land/drops/{id} {status, assigned_member_id, replacement_target_id, notes}.
// Complete writes the abandoned event; cancelled cancels the calendar row.
func handleSeasonLandDropUpdate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid drop ID", http.StatusBadRequest)
		return
	}
	var body struct {
		Status              string `json:"status"`
		AssignedMemberID    *int   `json:"assigned_member_id"`
		ReplacementTargetID *int   `json:"replacement_target_id"`
		Notes               string `json:"notes"`
		CompletedAt         string `json:"completed_at"` // ST; default now
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	var seasonID int
	var oldStatus, holding, areaID, kind string
	var level *int
	if err := db.QueryRow(`SELECT season_id, status, holding_name, holding_area_id, kind, level FROM season_land_drops WHERE id = ?`, id).
		Scan(&seasonID, &oldStatus, &holding, &areaID, &kind, &level); err == sql.ErrNoRows {
		http.Error(w, "Drop not found", http.StatusNotFound)
		return
	} else if err != nil {
		slog.Error("handleSeasonLandDropUpdate: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	s := landSeason(w, seasonID)
	if s == nil {
		return
	}
	if ok, msg := seasonAcceptsEdits(seasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	if body.Status == "" {
		body.Status = oldStatus
	}
	if !validDropStatuses[body.Status] {
		http.Error(w, "status must be drop_required, assigned, abandoning, complete or cancelled", http.StatusBadRequest)
		return
	}
	if body.Status == "drop_required" && body.AssignedMemberID != nil {
		body.Status = "assigned"
	}
	completedAt := time.Now()
	if body.Status == "complete" && oldStatus != "complete" && strings.TrimSpace(body.CompletedAt) != "" {
		t, err := parseLandInstant(body.CompletedAt)
		if err != nil {
			http.Error(w, "the completion time must be YYYY-MM-DD HH:MM (server time)", http.StatusBadRequest)
			return
		}
		completedAt = t
	}
	syncs, ok := landDropWriteTx(w, s, user.ID, func(tx *sql.Tx) (string, error) {
		if msg, err := replacementCheckTx(tx, s.ID, body.ReplacementTargetID); msg != "" || err != nil {
			return msg, err
		}
		if _, err := tx.Exec(`UPDATE season_land_drops SET status = ?, assigned_member_id = ?, replacement_target_id = ?,
			notes = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, body.Status, body.AssignedMemberID, body.ReplacementTargetID,
			strings.TrimSpace(body.Notes), id); err != nil {
			return "", err
		}
		if body.Status == "complete" && oldStatus != "complete" {
			ev := landEventIn{AreaID: areaID, Name: holding, Kind: kind, Level: level, Action: "abandoned", Note: "drop completed"}
			if _, err := writeLandEventTx(tx, s.ID, ev, completedAt, "manual", user.ID); err != nil {
				return "", err
			}
		}
		return "", nil
	})
	if !ok {
		return
	}
	detail := ""
	if body.Status != oldStatus {
		detail = "status: " + oldStatus + " → " + body.Status
	}
	logActivity(user.ID, user.Username, "updated", "season_land_drop", holding+", "+seasonTagName(&s.ID), false, detail)
	writeJSON(w, map[string]any{"calendar": syncs})
}

// ── CSV kinds ──────────────────────────────────────────────────────────────

type landEventImport struct {
	in landEventIn
	at time.Time
}

func init() {
	registerSeasonImportKind("land_events", importKind{
		Title: "Land events",
		Perms: []string{"manage_season_land"},
		Columns: []importColumn{
			{Key: "area", Label: "Area", Aliases: []string{"area_id", "map_id", "id"}},
			{Key: "name", Label: "Name", Aliases: []string{"area_name"}},
			{Key: "kind", Label: "Kind", Aliases: []string{"type"}},
			{Key: "level", Label: "Level", Aliases: []string{"lv", "lvl"}},
			{Key: "action", Label: "Action", Required: true},
			{Key: "at", Label: "At", Aliases: []string{"time", "when", "at_st"}, Required: true},
			{Key: "note", Label: "Note", Aliases: []string{"notes"}},
		},
		Parse: func(ctx *importCtx, rec map[string]string) (any, map[string]any, []string) {
			lv, err := optIntCell(rec["level"])
			if err != nil {
				return nil, map[string]any{"name": rec["name"]}, []string{"level is not a whole number"}
			}
			in, at, msg := normaliseLandEvent(ctx.Season.SeasonNumber, landEventIn{AreaID: rec["area"], Name: rec["name"],
				Kind: rec["kind"], Level: lv, Action: rec["action"], At: rec["at"], Note: rec["note"]}, time.Now())
			summary := map[string]any{"name": in.Name, "kind": in.Kind, "action": in.Action, "at": rec["at"]}
			if msg != "" {
				return nil, summary, []string{msg}
			}
			if strings.TrimSpace(rec["at"]) == "" {
				return nil, summary, []string{"At is empty"}
			}
			summary["at"] = landST(&at)
			return landEventImport{in: in, at: at}, summary, nil
		},
		Write: func(tx *sql.Tx, ctx *importCtx, items []any) (int, int, error) {
			written, unchanged := 0, 0
			for _, it := range items {
				e := it.(landEventImport)
				ok, err := writeLandEventTx(tx, ctx.Season.ID, e.in, e.at, "csv", ctx.User.ID)
				if err != nil {
					return 0, 0, err
				}
				if ok {
					written++
				} else {
					unchanged++
				}
			}
			return written, unchanged, nil
		},
	})

	registerSeasonImportKind("land_levels", importKind{
		Title: "Land levels",
		Perms: []string{"manage_season_land", "manage_season_config"},
		Columns: []importColumn{
			{Key: "kind", Label: "Kind", Aliases: []string{"type"}, Required: true},
			{Key: "level", Label: "Level", Aliases: []string{"lv", "lvl"}, Required: true},
			{Key: "name", Label: "Name"},
			{Key: "resource_per_hour", Label: "Output per hour", Aliases: []string{"output_per_hour", "per_hour", "resource_h", "output_h", "stone_h", "per_hr"}},
			{Key: "unlock_week", Label: "Unlock week", Aliases: []string{"week"}},
			{Key: "unlock_day", Label: "Unlock day", Aliases: []string{"day"}},
			{Key: "boss_level", Label: "Boss level"},
			{Key: "resistance", Label: "Resistance"},
			{Key: "suggested_power", Label: "Suggested power", Aliases: []string{"power"}},
			{Key: "source", Label: "Source"},
			{Key: "grade", Label: "Grade"},
		},
		Parse: func(ctx *importCtx, rec map[string]string) (any, map[string]any, []string) {
			var errs []string
			l := landLevel{Kind: strings.ToLower(rec["kind"]), Name: rec["name"], Source: rec["source"], Grade: strings.ToLower(rec["grade"])}
			summary := map[string]any{"kind": l.Kind, "level": rec["level"], "name": l.Name}
			if lv, err := strconv.Atoi(rec["level"]); err == nil {
				l.Level = lv
			} else {
				errs = append(errs, "Level is not a whole number")
			}
			cells := []struct {
				key string
				dst **int
			}{{"resource_per_hour", &l.ResourcePerHour}, {"unlock_week", &l.UnlockWeek}, {"unlock_day", &l.UnlockDay}}
			for _, c := range cells {
				v, err := optIntCell(rec[c.key])
				if err != nil {
					errs = append(errs, c.key+" is not a whole number")
				}
				*c.dst = v
			}
			extra := map[string]any{}
			for _, k := range []string{"boss_level", "resistance", "suggested_power"} {
				if v := rec[k]; v != "" {
					if n, err := optIntCell(v); err == nil && n != nil {
						extra[k] = *n
					} else {
						extra[k] = v
					}
				}
			}
			l.Extra, _ = json.Marshal(extra)
			if len(errs) == 0 {
				if msg := validateLandLevels([]landLevel{l}); msg != "" {
					errs = append(errs, strings.TrimPrefix(msg, "row 1: "))
				}
			}
			seen, _ := ctx.Data["seen"].(map[string]bool)
			if seen == nil {
				seen = map[string]bool{}
				ctx.Data["seen"] = seen
			}
			if k := l.Kind + strconv.Itoa(l.Level); len(errs) == 0 && seen[k] {
				errs = append(errs, "this kind and level is on an earlier row too")
			} else {
				seen[k] = true
			}
			if len(errs) > 0 {
				return nil, summary, errs
			}
			return l, summary, nil
		},
		Write: func(tx *sql.Tx, ctx *importCtx, items []any) (int, int, error) {
			written := 0
			for _, it := range items {
				l := it.(landLevel)
				if _, err := tx.Exec(`INSERT INTO season_land_levels (season_id, kind, level, name, resource_per_hour,
					unlock_week, unlock_day, extra, source, grade) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
					ON CONFLICT(season_id, kind, level) DO UPDATE SET name = excluded.name,
					resource_per_hour = excluded.resource_per_hour, unlock_week = excluded.unlock_week,
					unlock_day = excluded.unlock_day, extra = excluded.extra, source = excluded.source, grade = excluded.grade`,
					ctx.Season.ID, l.Kind, l.Level, strings.TrimSpace(l.Name), l.ResourcePerHour, l.UnlockWeek, l.UnlockDay,
					string(l.Extra), strings.TrimSpace(l.Source), normaliseEventGrade(l.Grade)); err != nil {
					return 0, 0, err
				}
				written++
			}
			return written, 0, nil
		},
	})
}
