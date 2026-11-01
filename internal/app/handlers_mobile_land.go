// handlers_mobile_land.go — the mobile API's season_land store (private-docs 275). The game
// shows which land the alliance holds; a phone that reads a capture, an abandonment or a loss
// appends it here. Routes, drops and the level table are the plan and have no mobile path.

package app

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// GET /api/mobile/season-hub/land (view_season_land) — the active season's Land rules, level
// table and current holdings.
func getMobileSeasonLand(w http.ResponseWriter, r *http.Request) {
	s, err := loadActiveSeason()
	if err != nil {
		slog.Error("getMobileSeasonLand: season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if s == nil {
		writeJSON(w, map[string]any{"season": nil, "config": nil, "levels": []any{}, "holdings": []any{}})
		return
	}
	cfg, err := loadLandConfig(db, s.ID)
	if err != nil {
		slog.Error("getMobileSeasonLand: config", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	levels, err := loadLandLevels(db, s.ID)
	if err != nil {
		slog.Error("getMobileSeasonLand: levels", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	events, err := loadLandEvents(db, s.ID)
	if err != nil {
		slog.Error("getMobileSeasonLand: events", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"season":   map[string]any{"id": s.ID, "name": s.Name, "season_number": s.SeasonNumber, "start_date": s.StartDate},
		"config":   cfg,
		"levels":   levels,
		"holdings": landHoldingsAt(events, time.Now()),
	})
}

// POST /api/mobile/season-hub/land/events (manage_season_land)
// {captured_at, events: [{area_id, name, kind, level, action, at}]} — appended to the active
// season with the web form's validation, source mobile. An event without `at` takes
// captured_at (when the phone read the screen), else now. Each event is reported: written,
// unchanged (the same area, action and instant is already recorded) or refused.
func postMobileSeasonLandEvents(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var body struct {
		CapturedAt string        `json:"captured_at"`
		Events     []landEventIn `json:"events"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if len(body.Events) == 0 {
		http.Error(w, "events is empty", http.StatusBadRequest)
		return
	}
	if len(body.Events) > 500 {
		http.Error(w, "at most 500 events per request", http.StatusBadRequest)
		return
	}
	s, err := loadActiveSeason()
	if err != nil {
		slog.Error("postMobileSeasonLandEvents: season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if s == nil {
		http.Error(w, "No active season", http.StatusConflict)
		return
	}
	if ok, msg := seasonAcceptsEdits(s.ID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	now := time.Now()
	type result struct {
		Index  int    `json:"index"`
		Status string `json:"status"` // written | unchanged | refused
		Error  string `json:"error,omitempty"`
	}
	results := make([]result, len(body.Events))
	type valid struct {
		i  int
		in landEventIn
		at time.Time
	}
	var ok []valid
	for i, ev := range body.Events {
		if ev.At == "" {
			ev.At = body.CapturedAt
		}
		in, at, msg := normaliseLandEvent(s.SeasonNumber, ev, now)
		if msg != "" {
			results[i] = result{Index: i, Status: "refused", Error: msg}
			continue
		}
		ok = append(ok, valid{i, in, at})
	}
	written := 0
	if len(ok) > 0 {
		tx, err := db.Begin()
		if err != nil {
			slog.Error("postMobileSeasonLandEvents: begin", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()
		for _, v := range ok {
			wrote, err := writeLandEventTx(tx, s.ID, v.in, v.at, "mobile", user.ID)
			if err != nil {
				slog.Error("postMobileSeasonLandEvents: write", "error", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			results[v.i] = result{Index: v.i, Status: "unchanged"}
			if wrote {
				results[v.i].Status = "written"
				written++
			}
		}
		if err := tx.Commit(); err != nil {
			slog.Error("postMobileSeasonLandEvents: commit", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if written > 0 {
		logActivity(user.ID, user.Username, "created", "season_land_event", seasonTagName(&s.ID), false,
			strconv.Itoa(written)+" land events · via mobile")
	}
	writeJSON(w, map[string]any{"season_id": s.ID, "written": written, "results": results})
}
