// schedule_sync.go — calendar rows owned by a Season Hub record, and the season's conflict
// check (private-docs 273).
//
// Land captures, abandonments, wars and Trade Post actions each put one row on the calendar
// and keep it in step: syncSourcedScheduleEvent finds the row by (source_kind, source_id),
// creates it through the schedule's own validator, moves it when the source moves, and marks
// it cancelled — never deletes it — when the source is cancelled. A refusal by the validator
// is returned for the caller to show; it never fails the source's own write.

package app

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// The sourced kinds; each names the record table that owns the row.
const (
	sourceLandCapture = "land_capture"
	sourceLandDrop    = "land_drop"
	sourceWar         = "war"
	sourceTradePost   = "trade_post"
)

// sourcedEvent is what a Land or War record wants on the calendar.
type sourcedEvent struct {
	Kind     string
	ID       int64
	SeasonID int
	Date     string // game date, YYYY-MM-DD
	Time     string // HH:MM
	TypeID   int
	Notes    string
	// Status is the calendar status: tentative, confirmed or completed; cancelled cancels the
	// row (an absent row stays absent).
	Status         string
	LeadershipOnly bool
	// AdoptPushed lets the row adopt an unsourced, uncancelled occurrence of TypeID already on
	// Date (a pushed Copper War) instead of inserting a second one beside it: the row then
	// carries both origins, and the source moves it.
	AdoptPushed bool
}

// syncResult reports what the sync did: created, adopted, moved, updated, cancelled,
// unchanged, refused (Message says why) or none.
type syncResult struct {
	EventID int64  `json:"event_id,omitempty"`
	Action  string `json:"action"`
	Message string `json:"message,omitempty"`
}

// sourcedTypeID looks up one of the system types the sync writes (migration 089).
func sourcedTypeID(q rowQuerier, name string) (int, error) {
	var id int
	err := q.QueryRow(`SELECT id FROM schedule_event_types WHERE name = ?`, name).Scan(&id)
	return id, err
}

// syncSourcedScheduleEvent keeps a record's calendar row in step, inside the caller's
// transaction. The game rules run exactly as on a manual create or edit.
func syncSourcedScheduleEvent(tx *sql.Tx, src sourcedEvent, actorID int) (syncResult, error) {
	var (
		id                      int64
		date, tm, notes, status string
	)
	err := tx.QueryRow(`SELECT id, event_date, event_time, COALESCE(notes,''), status FROM schedule_events
		WHERE source_kind = ? AND source_id = ?`, src.Kind, src.ID).Scan(&id, &date, &tm, &notes, &status)
	found := err == nil
	if err != nil && err != sql.ErrNoRows {
		return syncResult{}, err
	}

	if src.Status == "cancelled" {
		if !found || status == "cancelled" {
			return syncResult{EventID: id, Action: "none"}, nil
		}
		if _, err := tx.Exec(`UPDATE schedule_events SET status = 'cancelled', updated_at = ? WHERE id = ?`,
			time.Now().UTC().Format(time.RFC3339), id); err != nil {
			return syncResult{}, err
		}
		return syncResult{EventID: id, Action: "cancelled"}, nil
	}
	if src.Status == "" {
		src.Status = "confirmed"
	}
	if src.TypeID == 0 {
		return syncResult{Action: "refused", Message: "no event type configured; nothing scheduled"}, nil
	}

	tr, err := loadScheduleTypeRules(tx, src.TypeID)
	if err != nil {
		return syncResult{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)

	if found {
		if date == src.Date && tm == src.Time && notes == src.Notes && status == src.Status {
			return syncResult{EventID: id, Action: "unchanged"}, nil
		}
		if date != src.Date || tm != src.Time || status == "cancelled" {
			cand := eventCandidate{Date: src.Date, Time: src.Time, DateUnchanged: date == src.Date}
			if msg, err := validateEventRules(tx, tr, cand, int(id)); err != nil {
				return syncResult{}, err
			} else if msg != "" {
				return syncResult{EventID: id, Action: "refused", Message: msg}, nil
			}
		}
		if _, err := tx.Exec(`UPDATE schedule_events SET event_date = ?, event_time = ?, notes = ?, status = ?,
			season_id = COALESCE(season_id, ?), updated_at = ? WHERE id = ?`,
			src.Date, src.Time, src.Notes, src.Status, src.SeasonID, now, id); err != nil {
			return syncResult{}, err
		}
		action := "updated"
		if date != src.Date || tm != src.Time {
			action = "moved"
		}
		return syncResult{EventID: id, Action: action}, nil
	}

	if src.AdoptPushed {
		var pushed int64
		err := tx.QueryRow(`SELECT id FROM schedule_events WHERE event_type_id = ? AND event_date = ?
			AND source_kind IS NULL AND status != 'cancelled' ORDER BY season_event_id IS NULL, id LIMIT 1`,
			src.TypeID, src.Date).Scan(&pushed)
		if err == nil {
			if _, err := tx.Exec(`UPDATE schedule_events SET source_kind = ?, source_id = ?, event_time = ?, notes = ?,
				status = ?, season_id = COALESCE(season_id, ?), updated_at = ? WHERE id = ?`,
				src.Kind, src.ID, src.Time, src.Notes, src.Status, src.SeasonID, now, pushed); err != nil {
				return syncResult{}, err
			}
			return syncResult{EventID: pushed, Action: "adopted"}, nil
		}
		if err != sql.ErrNoRows {
			return syncResult{}, err
		}
	}

	if msg, err := validateEventRules(tx, tr, eventCandidate{Date: src.Date, Time: src.Time}, 0); err != nil {
		return syncResult{}, err
	} else if msg != "" {
		return syncResult{Action: "refused", Message: msg}, nil
	}
	var level *int
	if tl, err := loadTypeLevels(tx, src.TypeID); err == nil && tl.HasLevel && tl.Baseline != nil {
		b := *tl.Baseline
		level = &b
	}
	res, err := tx.Exec(`INSERT INTO schedule_events (event_date, event_type_id, event_time, all_day, level, notes,
		created_by, created_at, updated_at, season_id, status, leadership_only, show_on_main, source_kind, source_id)
		VALUES (?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		src.Date, src.TypeID, src.Time, level, src.Notes, actorID, now, now, src.SeasonID, src.Status,
		boolToInt(src.LeadershipOnly), src.Kind, src.ID)
	if err != nil {
		return syncResult{}, err
	}
	newID, _ := res.LastInsertId()
	return syncResult{EventID: newID, Action: "created"}, nil
}

// ── Conflicts ──────────────────────────────────────────────────────────────

// conflictEvent is one timed calendar row the check considers.
type conflictEvent struct {
	ID       int
	Date     string
	Time     string // HH:MM
	TypeName string
	Short    string
	IsSystem bool
	SeasonID *int
}

// conflictWindow is one day a server-event window opens or closes.
type conflictWindow struct {
	Name    string
	Date    string
	Minutes int // 0 for an opening at the reset, 1440 for a close at the next reset
}

type conflictHit struct {
	Kind    string `json:"kind"` // event | window
	Name    string `json:"name"`
	Time    string `json:"time"`
	Minutes int    `json:"minutes_apart"`
}

type seasonConflict struct {
	EventID    int           `json:"event_id"`
	Date       string        `json:"date"`
	Time       string        `json:"time"`
	TypeName   string        `json:"type_name"`
	Conflicts  []conflictHit `json:"conflicts"`
	Suggestion string        `json:"suggestion"` // "" when no clear time that day
}

// scheduleConflictShorts are the system events a season event must keep clear of.
var scheduleConflictShorts = map[string]bool{"MG": true, "LS": true, "ZS": true, "DS": true}

func hhmmMinutes(s string) (int, bool) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

func minutesHHMM(m int) string { return fmt.Sprintf("%02d:%02d", m/60, m%60) }

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// detectSeasonConflicts lists, for each season-tagged event that is not itself a system
// event, the system events starting within buffer minutes of it on its date and the server-
// event windows opening or closing within the buffer, with the nearest clear time that day:
// the earliest clear start at or after the event's own (in 15-minute steps), else the latest
// clear start before it. It reports and suggests; it moves nothing. Pure.
func detectSeasonConflicts(events []conflictEvent, windows []conflictWindow, buffer int) []seasonConflict {
	out := []seasonConflict{}
	if buffer <= 0 {
		return out
	}
	type blocker struct {
		kind, name string
		m          int
	}
	byDate := map[string][]blocker{}
	for _, e := range events {
		if !e.IsSystem || !scheduleConflictShorts[e.Short] {
			continue
		}
		if m, ok := hhmmMinutes(e.Time); ok {
			byDate[e.Date] = append(byDate[e.Date], blocker{"event", e.TypeName, m})
		}
	}
	for _, w := range windows {
		byDate[w.Date] = append(byDate[w.Date], blocker{"window", w.Name, w.Minutes})
	}
	clear := func(date string, m int) bool {
		for _, b := range byDate[date] {
			if absInt(b.m-m) < buffer {
				return false
			}
		}
		return true
	}
	for _, e := range events {
		if e.SeasonID == nil || (e.IsSystem && scheduleConflictShorts[e.Short]) {
			continue
		}
		m, ok := hhmmMinutes(e.Time)
		if !ok {
			continue
		}
		var hits []conflictHit
		for _, b := range byDate[e.Date] {
			if d := absInt(b.m - m); d < buffer {
				t := minutesHHMM(b.m % 1440)
				if b.m == 1440 {
					t = "24:00"
				}
				hits = append(hits, conflictHit{Kind: b.kind, Name: b.name, Time: t, Minutes: d})
			}
		}
		if len(hits) == 0 {
			continue
		}
		sort.Slice(hits, func(i, j int) bool { return hits[i].Minutes < hits[j].Minutes })
		sug := ""
		for c := m - m%15 + 15; c < 1440; c += 15 {
			if clear(e.Date, c) {
				sug = minutesHHMM(c)
				break
			}
		}
		if sug == "" {
			for c := m - m%15; c >= 0; c -= 15 {
				if c != m && clear(e.Date, c) {
					sug = minutesHHMM(c)
					break
				}
			}
		}
		out = append(out, seasonConflict{EventID: e.ID, Date: e.Date, Time: e.Time, TypeName: e.TypeName,
			Conflicts: hits, Suggestion: sug})
	}
	return out
}

// GET /api/schedule/conflicts?season_id= — the season's conflicts over its timeline bounds.
func handleScheduleConflicts(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	sid, _ := strconv.Atoi(r.URL.Query().Get("season_id"))
	if sid == 0 {
		http.Error(w, "season_id is required", http.StatusBadRequest)
		return
	}
	var buffer int
	if err := db.QueryRow(`SELECT schedule_buffer_minutes FROM seasons WHERE id = ?`, sid).Scan(&buffer); err == sql.ErrNoRows {
		http.Error(w, "Season not found", http.StatusNotFound)
		return
	} else if err != nil {
		slog.Error("handleScheduleConflicts: season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	from, to, err := seasonTimelineBounds(db, sid)
	if err != nil {
		slog.Error("handleScheduleConflicts: bounds", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	events, windows, err := loadConflictInputs(db, user, from, to)
	if err != nil {
		slog.Error("handleScheduleConflicts: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	// Only this season's rows are checked; every timed row in range can block them.
	for i := range events {
		if events[i].SeasonID != nil && *events[i].SeasonID != sid {
			events[i].SeasonID = nil
		}
	}
	writeJSON(w, map[string]any{"buffer_minutes": buffer, "conflicts": detectSeasonConflicts(events, windows, buffer)})
}

// loadConflictInputs reads the timed, uncancelled rows the viewer may see in [from, to] and the
// days the active anchored server-event windows open and close there.
func loadConflictInputs(q rowQueryer, user *AuthUser, from, to string) ([]conflictEvent, []conflictWindow, error) {
	rows, err := q.Query(`SELECT se.id, se.event_date, se.event_time, t.name, t.short_name, t.is_system, se.season_id
		FROM schedule_events se JOIN schedule_event_types t ON t.id = se.event_type_id
		WHERE se.event_date >= ? AND se.event_date <= ? AND se.all_day = 0 AND se.status != 'cancelled'`+
		scheduleVisibility(user, "se"), from, to)
	if err != nil {
		return nil, nil, err
	}
	var events []conflictEvent
	for rows.Next() {
		var e conflictEvent
		var sys int
		if err := rows.Scan(&e.ID, &e.Date, &e.Time, &e.TypeName, &e.Short, &sys, &e.SeasonID); err != nil {
			rows.Close()
			return nil, nil, err
		}
		e.IsSystem = sys == 1
		events = append(events, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	srows, err := q.Query(`SELECT id FROM server_events WHERE active = 1 AND COALESCE(anchor_date, '') != ''`)
	if err != nil {
		return nil, nil, err
	}
	var ids []int
	for srows.Next() {
		var id int
		srows.Scan(&id)
		ids = append(ids, id)
	}
	srows.Close()
	fromT, _ := time.Parse("2006-01-02", from)
	toT, _ := time.Parse("2006-01-02", to)
	var windows []conflictWindow
	for _, id := range ids {
		ev, err := loadServerEvent(q, id)
		if err != nil {
			continue
		}
		for _, start := range serverEventOccurrenceStarts(ev, fromT, toT) {
			end := start.AddDate(0, 0, ev.durationDays()-1)
			if !start.Before(fromT) && !start.After(toT) {
				windows = append(windows, conflictWindow{Name: ev.Name + " opens", Date: start.Format("2006-01-02"), Minutes: 0})
			}
			if !end.Before(fromT) && !end.After(toT) {
				windows = append(windows, conflictWindow{Name: ev.Name + " closes", Date: end.Format("2006-01-02"), Minutes: 1440})
			}
		}
	}
	return events, windows, nil
}
