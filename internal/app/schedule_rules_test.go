package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	// The ZS rule itself changed with the move to a date gap; what is pinned here
	// is that the manual path still refuses, and names the date it compared with.
	if rr := create(zsTypeID, "2026-09-09", "23:00"); rr.Code != http.StatusBadRequest ||
		!strings.Contains(rr.Body.String(), "two clear days") ||
		!strings.Contains(rr.Body.String(), "2026-09-07") {
		t.Errorf("ZS inside the gap: %d %q", rr.Code, rr.Body.String())
	}
}

// --- ZS: two clear days between sieges (issue #74) --------------------------
//
// The rule is on DATES. The 71.5-hour figure it replaces was one observation
// from a 00:30 start generalised into an interval, so these cases sweep the
// start time across the day and assert it changes nothing.

func seedZS(t *testing.T, date, tm string) int {
	t.Helper()
	_, zsTypeID := systemTypeIDs(t)
	res, err := db.Exec(`INSERT INTO schedule_events (event_date, event_type_id, event_time, level, notes, created_by)
	      VALUES (?, ?, ?, 1, '', 1)`, date, zsTypeID, tm)
	if err != nil {
		t.Fatalf("seed ZS %s: %v", date, err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func TestZSGapIsAboutDatesNotHours(t *testing.T) {
	// Last ZS is Monday 2026-09-07. Tuesday and Wednesday are refused whatever
	// the times; Thursday is fine, including at 00:00 — under the old 71.5h rule
	// a Monday 23:00 siege pushed the next eligible moment to Thursday 22:30,
	// which refused a Thursday the game allows.
	for _, lastTime := range []string{"00:00", "00:30", "08:00", "20:00", "23:00"} {
		t.Run("last_at_"+lastTime, func(t *testing.T) {
			setupSettingsTestDB(t)
			seedZS(t, "2026-09-07", lastTime)

			for _, c := range []struct {
				date, tm string
				wantOK   bool
			}{
				{"2026-09-08", "00:00", false},
				{"2026-09-08", "23:00", false},
				{"2026-09-09", "00:00", false},
				{"2026-09-09", "23:59", false},
				{"2026-09-10", "00:00", true},
				{"2026-09-10", "23:00", true},
			} {
				msg, err := validateSystemEventRules(db, "ZS", c.date, c.tm, 0)
				if err != nil {
					t.Fatalf("validate %s %s: %v", c.date, c.tm, err)
				}
				if c.wantOK && msg != "" {
					t.Errorf("%s %s rejected: %s", c.date, c.tm, msg)
				}
				if !c.wantOK {
					if msg == "" {
						t.Errorf("%s %s accepted, want rejected", c.date, c.tm)
					} else if !strings.Contains(msg, "2026-09-07") {
						t.Errorf("%s %s: message must name the conflicting date, got %q", c.date, c.tm, msg)
					}
				}
			}
		})
	}
}

func TestZSGapLooksBothWays(t *testing.T) {
	setupSettingsTestDB(t)
	// An existing Thursday siege. A new one on the Tuesday or the Wednesday
	// BEFORE it is exactly as illegal as one after — the old backwards-only
	// check accepted both.
	seedZS(t, "2026-09-10", "23:00")

	for _, date := range []string{"2026-09-08", "2026-09-09", "2026-09-10", "2026-09-11", "2026-09-12"} {
		msg, err := validateSystemEventRules(db, "ZS", date, "23:00", 0)
		if err != nil {
			t.Fatalf("validate %s: %v", date, err)
		}
		if msg == "" {
			t.Errorf("%s accepted, want rejected as within two clear days of 2026-09-10", date)
		}
	}
	for _, date := range []string{"2026-09-07", "2026-09-13"} {
		msg, err := validateSystemEventRules(db, "ZS", date, "23:00", 0)
		if err != nil {
			t.Fatalf("validate %s: %v", date, err)
		}
		if msg != "" {
			t.Errorf("%s rejected: %s", date, msg)
		}
	}
}

func TestZSGapExcludesTheRowBeingEdited(t *testing.T) {
	setupSettingsTestDB(t)
	id := seedZS(t, "2026-09-07", "23:00")

	// Moving the only siege one day along must be accepted: it cannot conflict
	// with itself.
	msg, err := validateSystemEventRules(db, "ZS", "2026-09-08", "23:00", id)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if msg != "" {
		t.Errorf("moving the only ZS was rejected: %s", msg)
	}
	// Without the exclusion it is a conflict with itself.
	if msg, _ := validateSystemEventRules(db, "ZS", "2026-09-08", "23:00", 0); msg == "" {
		t.Error("excludeID 0 should still see the existing row")
	}
}

func TestZSAllDayEventNeedsNoSpecialCase(t *testing.T) {
	setupSettingsTestDB(t)
	// An all-day ZS stores event_time '00:00'. The date is the date.
	seedZS(t, "2026-09-07", "00:00")
	if msg, _ := validateSystemEventRules(db, "ZS", "2026-09-09", "00:00", 0); msg == "" {
		t.Error("all-day ZS two days later accepted, want rejected")
	}
	if msg, _ := validateSystemEventRules(db, "ZS", "2026-09-10", "00:00", 0); msg != "" {
		t.Errorf("all-day ZS on D+3 rejected: %s", msg)
	}
}

func TestASAPChainStepsWholeDaysWithoutDrift(t *testing.T) {
	setupSettingsTestDB(t)
	if _, err := db.Exec(`UPDATE settings SET zs_schedule_mode='asap', zs_anchor_date='2026-09-07',
	      zs_default_time='23:00' WHERE id=1`); err != nil {
		t.Fatalf("settings: %v", err)
	}

	out := runGenerate(t, "2026-09-07", "2026-10-07", "zs")
	if out.SkippedInvalid != 0 {
		t.Errorf("a date-stepped chain cannot violate its own rule, got %+v", out)
	}

	rows, err := db.Query(`SELECT e.event_date FROM schedule_events e
	      JOIN schedule_event_types t ON t.id = e.event_type_id
	      WHERE t.short_name='ZS' ORDER BY e.event_date`)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var dates []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatalf("scan: %v", err)
		}
		dates = append(dates, d)
	}
	rows.Close()

	if len(dates) < 8 {
		t.Fatalf("got %d dates over 30 days, want at least 8: %v", len(dates), dates)
	}
	for i := 1; i < len(dates); i++ {
		prev, _ := time.Parse("2006-01-02", dates[i-1])
		cur, _ := time.Parse("2006-01-02", dates[i])
		if gap := int(cur.Sub(prev).Hours() / 24); gap != zsGapDays {
			t.Errorf("gap %s → %s is %d days, want %d", dates[i-1], dates[i], gap, zsGapDays)
		}
	}
}

// --- MG: never on consecutive days (issue #77) ------------------------------
//
// Advertised in the event-form hint since the schedule was rewritten and
// enforced nowhere. The generator's every-other-day stepping happened to
// satisfy it, so only a manual create or edit could break it — which is the
// path officers actually use.

func seedMG(t *testing.T, date, tm string) int {
	t.Helper()
	mgTypeID, _ := systemTypeIDs(t)
	res, err := db.Exec(`INSERT INTO schedule_events (event_date, event_type_id, event_time, level, notes, created_by)
	      VALUES (?, ?, ?, 1, '', 1)`, date, mgTypeID, tm)
	if err != nil {
		t.Fatalf("seed MG %s: %v", date, err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func TestMGRejectsConsecutiveDaysBothDirections(t *testing.T) {
	setupSettingsTestDB(t)
	seedMG(t, "2026-09-09", "20:00")

	for _, date := range []string{"2026-09-08", "2026-09-09", "2026-09-10"} {
		msg, err := validateSystemEventRules(db, "MG", date, "20:00", 0)
		if err != nil {
			t.Fatalf("validate %s: %v", date, err)
		}
		if msg == "" {
			t.Errorf("%s accepted, want rejected as adjacent to 2026-09-09", date)
		} else if !strings.Contains(msg, "2026-09-09") {
			t.Errorf("%s: message must name the conflicting date, got %q", date, msg)
		}
	}
	// Every other day is the cadence the UI has always advertised.
	for _, date := range []string{"2026-09-07", "2026-09-11"} {
		msg, err := validateSystemEventRules(db, "MG", date, "20:00", 0)
		if err != nil {
			t.Fatalf("validate %s: %v", date, err)
		}
		if msg != "" {
			t.Errorf("%s rejected: %s", date, msg)
		}
	}
}

// TestMGCutoffAndGapDoNotInteract pins the reading this rule was written under:
// the 21:59 cutoff is about the start time of one event, the gap is about the
// dates of two. A late MG does not extend into the following day.
func TestMGCutoffAndGapDoNotInteract(t *testing.T) {
	setupSettingsTestDB(t)
	seedMG(t, "2026-09-09", "21:59")

	if msg, err := validateSystemEventRules(db, "MG", "2026-09-11", "00:30", 0); err != nil {
		t.Fatalf("validate: %v", err)
	} else if msg != "" {
		t.Errorf("MG two days after a 21:59 MG rejected: %s", msg)
	}
	// The cutoff still applies on its own terms.
	if msg, _ := validateSystemEventRules(db, "MG", "2026-09-13", "22:00", 0); msg != "MG must start by 21:59 ST" {
		t.Errorf("cutoff message = %q", msg)
	}
}

func TestMGGapExcludesTheRowBeingEdited(t *testing.T) {
	setupSettingsTestDB(t)
	id := seedMG(t, "2026-09-09", "20:00")

	if msg, err := validateSystemEventRules(db, "MG", "2026-09-10", "20:00", id); err != nil {
		t.Fatalf("validate: %v", err)
	} else if msg != "" {
		t.Errorf("moving the only MG one day along was rejected: %s", msg)
	}
}

// TestGeneratedMGCadenceAlreadySatisfiesTheRule guards the claim that the
// generator needed no change: its AddDate(0,0,2) stepping is exactly mgGapDays.
func TestGeneratedMGCadenceAlreadySatisfiesTheRule(t *testing.T) {
	setupSettingsTestDB(t)
	if _, err := db.Exec(`UPDATE settings SET mg_anchor_date='2026-09-07', mg_default_time='20:00' WHERE id=1`); err != nil {
		t.Fatalf("settings: %v", err)
	}

	out := runGenerate(t, "2026-09-07", "2026-10-07", "mg")
	if out.MGCreated == 0 {
		t.Fatalf("generated nothing: %+v", out)
	}
	if out.SkippedInvalid != 0 {
		t.Errorf("the every-other-day cadence must satisfy mgGapDays, got %+v", out)
	}
}
