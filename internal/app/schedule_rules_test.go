package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// schedule_events has four write paths — manual create, manual update, bulk
// generate and the Season Hub push — and until validateSystemEventRules existed
// only the two manual ones applied any rule. These tests exercise the seam: what
// the generator produces must pass the validator a manual create would have run,
// and the bulk paths must count what they decline rather than write it.

func scheduleTestActor(req *http.Request) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), authUserKey,
		&AuthUser{ID: 1, Username: "tester", IsAdmin: true}))
}

func systemTypeIDs(t *testing.T) (mg, zs int) {
	t.Helper()
	if err := db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name='MG'`).Scan(&mg); err != nil {
		t.Fatalf("MG type: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name='ZS'`).Scan(&zs); err != nil {
		t.Fatalf("ZS type: %v", err)
	}
	return mg, zs
}

type generateResponse struct {
	MGCreated       int `json:"mg_created"`
	ZSCreated       int `json:"zs_created"`
	SkippedExisting int `json:"skipped_existing"`
	SkippedInvalid  int `json:"skipped_invalid"`
	Invalid         []struct {
		Date   string `json:"date"`
		Type   string `json:"type"`
		Reason string `json:"reason"`
	} `json:"invalid"`
}

func runGenerate(t *testing.T, from, to string, types ...string) generateResponse {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"from": from, "to": to, "types": types})
	req := scheduleTestActor(httptest.NewRequest(http.MethodPost, "/api/schedule/events/generate", strings.NewReader(string(body))))
	rr := httptest.NewRecorder()
	generateScheduleEvents(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("generate status = %d (body %s)", rr.Code, rr.Body.String())
	}
	var out generateResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode generate response: %v", err)
	}
	return out
}

// TestGeneratedEventsAllPassTheManualValidator is the test issue #78 says could
// not be written before this change: it asserts the generator cannot produce a
// row a manual create would refuse.
func TestGeneratedEventsAllPassTheManualValidator(t *testing.T) {
	for _, mode := range []string{"weekdays", "asap"} {
		t.Run(mode, func(t *testing.T) {
			setupSettingsTestDB(t)
			// Mon+Wed is a deliberately illegal ZS weekday set under the 71.5h
			// cooldown: two sieges 48h apart.
			if _, err := db.Exec(`UPDATE settings SET mg_anchor_date='2026-09-07', mg_default_time='20:00',
			      zs_schedule_mode=?, zs_weekdays='1,3', zs_anchor_date='2026-09-07', zs_anchor_time='23:00',
			      zs_default_time='23:00' WHERE id=1`, mode); err != nil {
				t.Fatalf("settings: %v", err)
			}

			out := runGenerate(t, "2026-09-07", "2026-12-05", "mg", "zs")
			if out.MGCreated+out.ZSCreated == 0 {
				t.Fatalf("generated nothing: %+v", out)
			}

			// Re-run every produced row through the validator, excluding itself.
			rows, err := db.Query(`SELECT e.id, e.event_date, e.event_time, t.short_name
			      FROM schedule_events e JOIN schedule_event_types t ON t.id = e.event_type_id
			      ORDER BY e.event_date, e.event_time`)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			type row struct {
				id       int
				date, tm string
				short    string
			}
			var stored []row
			for rows.Next() {
				var r row
				if err := rows.Scan(&r.id, &r.date, &r.tm, &r.short); err != nil {
					t.Fatalf("scan: %v", err)
				}
				stored = append(stored, r)
			}
			rows.Close()

			for _, r := range stored {
				msg, err := validateSystemEventRules(db, r.short, r.date, r.tm, r.id)
				if err != nil {
					t.Fatalf("validate %s %s: %v", r.short, r.date, err)
				}
				if msg != "" {
					t.Errorf("generated %s on %s would be refused by a manual create: %s", r.short, r.date, msg)
				}
			}

			if mode == "weekdays" && out.SkippedInvalid == 0 {
				t.Errorf("Mon+Wed under the ZS cooldown should have skipped dates, got %+v", out)
			}
			for _, iv := range out.Invalid {
				if iv.Date == "" || iv.Reason == "" {
					t.Errorf("invalid entry missing date or reason: %+v", iv)
				}
			}
		})
	}
}

// TestGeneratorValidatesAgainstItsOwnBatch pins the one-row-at-a-time
// invariant. The schedule starts EMPTY, so the only thing a candidate can
// conflict with is a row this same run created — Mon+Wed puts two sieges two
// days apart, which no ZS rule allows. Under plan-then-bulk-insert every
// Wednesday would be written and skipped_invalid would be zero, so this test is
// what fails if anyone "optimises" the loop that way.
func TestGeneratorValidatesAgainstItsOwnBatch(t *testing.T) {
	setupSettingsTestDB(t)
	if _, err := db.Exec(`UPDATE settings SET zs_schedule_mode='weekdays', zs_weekdays='1,3',
	      zs_default_time='23:00' WHERE id=1`); err != nil {
		t.Fatalf("settings: %v", err)
	}
	var pre int
	db.QueryRow(`SELECT COUNT(*) FROM schedule_events`).Scan(&pre)
	if pre != 0 {
		t.Fatalf("fixture must start empty, found %d rows", pre)
	}

	// 2026-09-07 is a Monday; four Mondays and four Wednesdays fall in the range.
	out := runGenerate(t, "2026-09-07", "2026-10-04", "zs")
	if out.ZSCreated == 0 {
		t.Fatalf("generated nothing: %+v", out)
	}
	if out.SkippedInvalid == 0 {
		t.Fatalf("Mon+Wed over an empty schedule must self-conflict, got %+v", out)
	}
	if out.SkippedExisting != 0 {
		t.Errorf("SkippedExisting = %d, want 0 — nothing pre-existed", out.SkippedExisting)
	}
	for _, iv := range out.Invalid {
		if iv.Type != "ZS" || iv.Reason == "" {
			t.Errorf("invalid entry does not name the rule: %+v", iv)
		}
	}

	// Every surviving row is legal against every other one.
	rows, err := db.Query(`SELECT e.id, e.event_date, e.event_time FROM schedule_events e
	      JOIN schedule_event_types t ON t.id = e.event_type_id WHERE t.short_name='ZS'
	      ORDER BY e.event_date`)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	type row struct {
		id       int
		date, tm string
	}
	var stored []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.date, &r.tm); err != nil {
			t.Fatalf("scan: %v", err)
		}
		stored = append(stored, r)
	}
	rows.Close()

	if len(stored) != out.ZSCreated {
		t.Fatalf("stored %d rows, response said %d created", len(stored), out.ZSCreated)
	}
	for _, r := range stored {
		msg, err := validateSystemEventRules(db, "ZS", r.date, r.tm, r.id)
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		if msg != "" {
			t.Errorf("ZS on %s conflicts with a row the same run created: %s", r.date, msg)
		}
	}
}

func TestPushSkipsInvalidSystemEvents(t *testing.T) {
	setupSettingsTestDB(t)
	mgTypeID, _ := systemTypeIDs(t)

	if _, err := db.Exec(`INSERT INTO seasons (id, name, season_number, start_date, week_count,
	      key_event_name, key_event_required, tier_active_min_pct, tier_at_risk_min_pct, is_active)
	      VALUES (1, 'Season IX', 9, '2026-09-07', 8, 'Rare Soil War', 4, 70, 60, 1)`); err != nil {
		t.Fatalf("seed season: %v", err)
	}
	// An MG at 22:30 breaks the 21:59 cutoff a manual create enforces.
	if _, err := db.Exec(`INSERT INTO season_events (season_id, label, event_type_id, type_name,
	      day_offset, event_time, week_start, week_end, notes)
	      VALUES (1, 'Late Guard', ?, 'Marshal''s Guard', 1, '22:30', 1, 1, '')`, mgTypeID); err != nil {
		t.Fatalf("seed MG: %v", err)
	}
	// A custom type carries no rules and must still be written at the same time.
	res, err := db.Exec(`INSERT INTO schedule_event_types (name, short_name, icon, is_system, sort_order)
	      VALUES ('City Clash', 'CC', '🏙️', 0, 9)`)
	if err != nil {
		t.Fatalf("seed type: %v", err)
	}
	ccID, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO season_events (season_id, label, event_type_id, type_name,
	      day_offset, event_time, week_start, week_end, notes)
	      VALUES (1, 'Clash', ?, 'City Clash', 2, '22:30', 1, 1, '')`, ccID); err != nil {
		t.Fatalf("seed CC: %v", err)
	}

	s, err := loadSeasonByID(1)
	if err != nil {
		t.Fatalf("loadSeasonByID: %v", err)
	}
	out, err := pushSeasonEventsToSchedule(s, 1, "tester")
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if out.SkippedInvalid != 1 {
		t.Errorf("SkippedInvalid = %d, want 1 (%+v)", out.SkippedInvalid, out)
	}
	if out.Created != 1 {
		t.Errorf("Created = %d, want 1 — the custom type carries no rules", out.Created)
	}
	if len(out.Invalid) != 1 || !strings.Contains(out.Invalid[0].Reason, "21:59") {
		t.Errorf("Invalid = %+v, want the MG cutoff named", out.Invalid)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM schedule_events WHERE event_type_id = ?`, mgTypeID).Scan(&n)
	if n != 0 {
		t.Errorf("%d illegal MG rows written", n)
	}
}

// TestCreateAndUpdateStillRejectWhatTheyRejectedBefore pins the message strings
// across the move into the shared validator.
func TestCreateAndUpdateStillRejectWhatTheyRejectedBefore(t *testing.T) {
	setupSettingsTestDB(t)
	mgTypeID, zsTypeID := systemTypeIDs(t)

	create := func(typeID int, date, tm string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"event_date": date, "event_type_id": typeID, "event_time": tm, "notes": "",
		})
		req := scheduleTestActor(httptest.NewRequest(http.MethodPost, "/api/schedule/events", strings.NewReader(string(body))))
		rr := httptest.NewRecorder()
		createScheduleEvent(rr, req)
		return rr
	}

	if rr := create(mgTypeID, "2026-09-09", "22:00"); rr.Code != http.StatusBadRequest ||
		!strings.Contains(rr.Body.String(), "MG must start by 21:59 ST") {
		t.Errorf("MG at 22:00: %d %q", rr.Code, rr.Body.String())
	}
	if rr := create(mgTypeID, "2026-09-09", "21:59"); rr.Code != http.StatusCreated {
		t.Errorf("MG at 21:59 should be accepted: %d %q", rr.Code, rr.Body.String())
	}

	if rr := create(zsTypeID, "2026-09-07", "23:00"); rr.Code != http.StatusCreated {
		t.Fatalf("first ZS: %d %q", rr.Code, rr.Body.String())
	}
	if rr := create(zsTypeID, "2026-09-09", "23:00"); rr.Code != http.StatusBadRequest ||
		!strings.Contains(rr.Body.String(), "ZS cooldown not elapsed") {
		t.Errorf("ZS inside the cooldown: %d %q", rr.Code, rr.Body.String())
	}
}
