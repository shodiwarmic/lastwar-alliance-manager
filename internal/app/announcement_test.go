package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

func ev(date, tm, name string, allDay, announce bool) announceEvent {
	return announceEvent{Date: date, Time: tm, TypeName: name, AllDay: allDay, Announce: announce}
}

// The selection rule, table-driven over the pure function. This is the one piece
// of arithmetic in the feature that can misfire, which is why it is in Go rather
// than behind a browser fixture.
func TestSelectAnnouncementEvents(t *testing.T) {
	const D, D1 = "2026-09-09", "2026-09-10"

	cases := []struct {
		name        string
		window      announceWindow
		onD, onD1   []announceEvent
		wantIn      []string // "<date> <time> <name>"
		wantDropped []string // "<name>: <reason>"
	}{
		{
			name:   "the default window includes both ends",
			window: announceWindow{Start: "00:00", End: "23:59"},
			onD: []announceEvent{
				ev(D, "00:00", "Midnight", false, true),
				ev(D, "23:59", "Last minute", false, true),
			},
			wantIn: []string{D + " 00:00 Midnight", D + " 23:59 Last minute"},
		},
		{
			name:   "a wrapping window reaches into the next day",
			window: announceWindow{Start: "18:00", End: "06:00", Wraps: true},
			// The literal pair the issue was filed from.
			onD: []announceEvent{
				ev(D, "00:30", "Large Sandworm", false, true),
				ev(D, "23:00", "Sky Predator", false, true),
			},
			onD1:   []announceEvent{ev(D1, "00:30", "Zombie Siege", false, true)},
			wantIn: []string{D + " 23:00 Sky Predator", D1 + " 00:30 Zombie Siege"},
			// The 00:30 dated D is before the window opens, and IS reported.
			wantDropped: []string{"Large Sandworm: outside the 18:00–06:00 window"},
		},
		{
			name:   "a next-day event at exactly the end is in",
			window: announceWindow{Start: "18:00", End: "06:00", Wraps: true},
			onD1:   []announceEvent{ev(D1, "06:00", "Dawn", false, true)},
			wantIn: []string{D1 + " 06:00 Dawn"},
		},
		{
			name:   "a next-day event past the end is out, and is not reported",
			window: announceWindow{Start: "18:00", End: "06:00", Wraps: true},
			onD1:   []announceEvent{ev(D1, "06:01", "Too late", false, true)},
			wantIn: []string{},
			// Deliberately empty: a D+1 event that misses the window was never this
			// announcement's business, and listing it would be noise.
			wantDropped: []string{},
		},
		{
			name:   "an all-day event on D is in; one on D+1 is not",
			window: announceWindow{Start: "18:00", End: "06:00", Wraps: true},
			onD:    []announceEvent{ev(D, "00:00", "All day today", true, true)},
			onD1:   []announceEvent{ev(D1, "00:00", "All day tomorrow", true, true)},
			wantIn: []string{D + " 00:00 All day today"},
		},
		{
			name:        "an unflagged type is dropped with that reason, not silently",
			window:      announceWindow{Start: "00:00", End: "23:59"},
			onD:         []announceEvent{ev(D, "12:00", "No Base Movement", false, false)},
			wantIn:      []string{},
			wantDropped: []string{"No Base Movement: type not flagged"},
		},
		{
			name:   "equal endpoints wrap, and take the whole day",
			window: announceWindow{Start: "12:00", End: "12:00", Wraps: true},
			onD: []announceEvent{
				ev(D, "11:59", "Before", false, true),
				ev(D, "12:00", "At", false, true),
			},
			onD1:        []announceEvent{ev(D1, "12:00", "Next at", false, true)},
			wantIn:      []string{D + " 12:00 At", D1 + " 12:00 Next at"},
			wantDropped: []string{"Before: outside the 12:00–12:00 window"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, dropped := selectAnnouncementEvents(tc.window, tc.onD, tc.onD1)

			var gotIn []string
			for _, e := range in {
				gotIn = append(gotIn, e.Date+" "+e.Time+" "+e.TypeName)
			}
			if !sameStrings(gotIn, tc.wantIn) {
				t.Errorf("included %v, want %v", gotIn, tc.wantIn)
			}

			if tc.wantDropped != nil {
				var gotDropped []string
				for _, d := range dropped {
					gotDropped = append(gotDropped, d.TypeName+": "+d.Reason)
				}
				if !sameStrings(gotDropped, tc.wantDropped) {
					t.Errorf("dropped %v, want %v", gotDropped, tc.wantDropped)
				}
			}
		})
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The template is seeded, carries its slug, and re-running the migration does not
// duplicate it.
func TestAnnouncementTemplateIsSeeded(t *testing.T) {
	setupSettingsTestDB(t)

	var count int
	var required, content string
	db.QueryRow(`SELECT COUNT(*) FROM comms_templates WHERE slug = 'nightly_events'`).Scan(&count)
	if count != 1 {
		t.Fatalf("nightly_events seeded %d times, want 1", count)
	}
	db.QueryRow(`SELECT required_vars, content FROM comms_templates WHERE slug = 'nightly_events'`).
		Scan(&required, &content)

	var vars []string
	if err := json.Unmarshal([]byte(required), &vars); err != nil {
		t.Fatalf("required_vars is not a JSON array: %v", err)
	}
	for _, want := range []string{"events", "starred_today", "starred_tomorrow"} {
		found := false
		for _, v := range vars {
			if v == want {
				found = true
			}
		}
		if !found {
			t.Errorf("required_vars is missing %q", want)
		}
	}
	// The SEED uses only {events}: the starred lines are an option the officer
	// places, not something every alliance's nightly post should carry.
	if !strings.Contains(content, "{events}") {
		t.Error("the seeded template does not use {events}")
	}

	runMigrationBody(t, "migrations/077_announcement.sql")
	db.QueryRow(`SELECT COUNT(*) FROM comms_templates WHERE slug = 'nightly_events'`).Scan(&count)
	if count != 1 {
		t.Errorf("re-running the migration produced %d rows, want 1 — it is not idempotent", count)
	}
}

// Saving a slugged template without a variable the app fills in reports it — and
// the report comes from the Go map, not from required_vars, which the same
// handler lets the officer edit.
func TestTemplateUpdateReportsMissingPrefilledVars(t *testing.T) {
	setupSettingsTestDB(t)

	var id int
	db.QueryRow(`SELECT id FROM comms_templates WHERE slug = 'nightly_events'`).Scan(&id)
	if id == 0 {
		t.Fatal("nightly_events not seeded")
	}

	// Drop {events} from the content AND from required_vars. If the handler read
	// required_vars it would now report nothing.
	missing := putTemplate(t, id, map[string]any{
		"title": "Daily events", "category": "Schedule",
		"content":       "Tonight, as usual.",
		"required_vars": `["starred_today"]`,
	})
	if !sameStrings(missing, []string{"events"}) {
		t.Errorf("missing_vars = %v, want [events]", missing)
	}

	// Putting it back clears the warning. The starred variables are OPTIONAL and
	// are never reported — warning on every untouched save would teach officers to
	// dismiss the one that matters.
	missing = putTemplate(t, id, map[string]any{
		"title": "Daily events", "category": "Schedule",
		"content":       "Today's events\n\n{events}",
		"required_vars": `["events","starred_today","starred_tomorrow"]`,
	})
	if len(missing) != 0 {
		t.Errorf("missing_vars = %v, want empty", missing)
	}

	// An unslugged template has no generator behind it and reports nothing.
	res, err := db.Exec(`INSERT INTO comms_templates (type, title, category, content, required_vars)
		VALUES ('mail','Plain','General','no variables here','[]')`)
	if err != nil {
		t.Fatalf("seed plain: %v", err)
	}
	plainID, _ := res.LastInsertId()
	missing = putTemplate(t, int(plainID), map[string]any{
		"title": "Plain", "category": "General", "content": "still nothing", "required_vars": "[]",
	})
	if len(missing) != 0 {
		t.Errorf("an unslugged template reported %v", missing)
	}
}

// reHHMM alone accepts 29:99. These values now take part in string comparisons
// that decide what goes into an alliance-wide post, so they must be parsed too.
func TestAnnounceWindowRejectsOutOfRangeTimes(t *testing.T) {
	setupSettingsTestDB(t)
	for _, bad := range []string{"24:00", "23:60", "29:99", "1:00", "noon"} {
		body := baseSettings()
		body["announce_window_start"] = bad
		body["announce_window_end"] = "23:59"
		if code := putSettings(t, body).Code; code != http.StatusBadRequest {
			t.Errorf("start %q accepted with %d", bad, code)
		}
	}
	body := baseSettings()
	body["announce_window_start"] = "18:00"
	body["announce_window_end"] = "23:59"
	if code := putSettings(t, body).Code; code != http.StatusOK {
		t.Errorf("a real window was rejected with %d", code)
	}

	var start, end string
	db.QueryRow(`SELECT announce_window_start, announce_window_end FROM settings WHERE id = 1`).Scan(&start, &end)
	if start != "18:00" || end != "23:59" {
		t.Errorf("stored %s–%s, want 18:00–23:59", start, end)
	}
}

// The same tightening applies to the event write paths, which shared the old
// shape-only check.
func TestEventTimeRejectsOutOfRangeTimes(t *testing.T) {
	setupSettingsTestDB(t)
	mg := typeIDByShort(t, "MG")
	for _, bad := range []string{"24:00", "23:60", "29:99"} {
		rr := postEvent(t, map[string]any{
			"event_date": "2026-09-09", "event_type_id": mg, "event_time": bad,
		})
		if rr.Code != http.StatusBadRequest {
			t.Errorf("event_time %q accepted with %d", bad, rr.Code)
		}
	}
	// An all-day event never reaches the parser — the all_day arm runs first.
	if rr := postEvent(t, map[string]any{
		"event_date": "2026-09-09", "event_type_id": mg, "all_day": true,
	}); rr.Code != http.StatusCreated {
		t.Errorf("an all-day event was rejected with %d: %s", rr.Code, rr.Body.String())
	}
}

// The flag round-trips on both the POST and the PUT, and defaults ON.
func TestAnnounceFlagRoundTrips(t *testing.T) {
	setupSettingsTestDB(t)

	// Every existing type is announced by default — the migration's whole point.
	var off int
	db.QueryRow(`SELECT COUNT(*) FROM schedule_event_types WHERE announce = 0`).Scan(&off)
	if off != 0 {
		t.Errorf("%d existing types arrived with announce off", off)
	}

	id := newCustomType(t, "Quiet Event", "QE", false)
	var announce int
	db.QueryRow(`SELECT announce FROM schedule_event_types WHERE id = ?`, id).Scan(&announce)
	if announce != 1 {
		t.Error("a newly created type defaulted to announce off")
	}

	if rr := putEventType(t, id, map[string]any{
		"name": "Quiet Event", "short_name": "QE", "icon": "📅", "active": true, "announce": false,
	}); rr.Code != http.StatusNoContent {
		t.Fatalf("PUT announce=false returned %d: %s", rr.Code, rr.Body.String())
	}
	db.QueryRow(`SELECT announce FROM schedule_event_types WHERE id = ?`, id).Scan(&announce)
	if announce != 0 {
		t.Error("announce=false did not persist")
	}

	// An ABSENT key leaves it alone — the has_level rule, for the same reason.
	if rr := putEventType(t, id, map[string]any{
		"name": "Quiet Event", "short_name": "QE", "icon": "📅", "active": true,
	}); rr.Code != http.StatusNoContent {
		t.Fatalf("PUT without announce returned %d", rr.Code)
	}
	db.QueryRow(`SELECT announce FROM schedule_event_types WHERE id = ?`, id).Scan(&announce)
	if announce != 0 {
		t.Error("an absent announce key reset the stored value")
	}
}

// --- helpers ---

// putTemplate drives the real comms-template PUT and returns the missing_vars
// the handler reported, so the test asserts on the response the browser gets.
func putTemplate(t *testing.T, id int, body map[string]any) []string {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/comms/templates/"+strconv.Itoa(id), strings.NewReader(string(b)))
	req = mux.SetURLVars(req, map[string]string{"id": strconv.Itoa(id)})
	req = req.WithContext(context.WithValue(req.Context(), authUserKey,
		&AuthUser{ID: 1, Username: "tester", IsAdmin: true}))
	rr := httptest.NewRecorder()
	handleCommsTemplateUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("template PUT returned %d: %s", rr.Code, rr.Body.String())
	}
	var out struct {
		MissingVars []string `json:"missing_vars"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out.MissingVars
}
