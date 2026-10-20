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
	"github.com/pressly/goose/v3"
)

func intp(v int) *int { return &v }

// shReq builds a Season Hub handler request: a raw JSON body (or none), mux vars and an
// actor (an admin when nil).
func shReq(method, body string, vars map[string]string, actor *AuthUser) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, "/", strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, "/", nil)
	}
	if actor == nil {
		actor = &AuthUser{ID: 1, Username: "tester", IsAdmin: true}
	}
	r = r.WithContext(context.WithValue(r.Context(), authUserKey, actor))
	if vars != nil {
		r = mux.SetURLVars(r, vars)
	}
	return r
}

// ── The template diff (pure) ────────────────────────────────────────────────

func TestTemplateDiffMatchesByKeyFirst(t *testing.T) {
	tmpl := []templateEvent{
		{Key: "a_wed", Label: "War", TypeName: "War", DayOffset: intp(3), EventTime: "12:30", WeekStart: 4, WeekEnd: 6, Grade: "official", Source: "cal"},
		{Key: "a_sat", Label: "War", TypeName: "War", DayOffset: intp(6), EventTime: "12:30", WeekStart: 4, WeekEnd: 6, Grade: "official", Source: "cal"},
	}
	// Two rows with the same label: only the key tells them apart.
	rows := []SeasonEvent{
		{ID: 10, Label: "War", TypeName: "War", DayOffset: intp(6), EventTime: "12:00", WeekStart: 4, WeekEnd: 6, DurationDays: 1, TemplateKey: "a_sat", Grade: "official", Source: "cal"},
		{ID: 11, Label: "War", TypeName: "War", DayOffset: intp(3), EventTime: "12:30", WeekStart: 4, WeekEnd: 6, DurationDays: 1, TemplateKey: "a_wed", Grade: "official", Source: "cal"},
	}
	d := diffSeasonTemplate(tmpl, rows)
	if len(d.Changed) != 1 || d.Changed[0].SeasonEventID != 10 || d.Changed[0].Match != "key" {
		t.Fatalf("changed = %+v, want only row 10 matched by key", d.Changed)
	}
	if f := d.Changed[0].Fields; len(f) != 1 || f[0].Field != "time" || f[0].Template != "12:30" {
		t.Errorf("fields = %+v, want only the time", f)
	}
	if len(d.Added)+len(d.Removed)+len(d.Ambiguous) != 0 {
		t.Errorf("unexpected added/removed/ambiguous: %+v", d)
	}
}

func TestTemplateDiffFallbackTiers(t *testing.T) {
	tmpl := []templateEvent{
		// Relabelled in the same slot.
		{Key: "ew1", Label: "Exclusive Weapon: Lucius (Aircraft)", TypeName: "Exclusive Weapon", DayOffset: intp(4), EventTime: "00:00", WeekStart: 1, WeekEnd: 1, IsServerEvent: true, DurationDays: 7, Grade: "official"},
		// Moved to another week, same label and type.
		{Key: "hm", Label: "Holy Mountain", TypeName: "Holy Mountain", DayOffset: intp(1), EventTime: "00:00", WeekStart: 8, WeekEnd: 8, IsServerEvent: true, DurationDays: 7, Grade: "official"},
		// Unchanged apart from provenance.
		{Key: "cc1", Label: "City Clash (L1)", TypeName: "City Clash", DayOffset: intp(3), EventTime: "12:00", WeekStart: 1, WeekEnd: 1, Grade: "cpt_hedge", Source: "Cpt Hedge"},
		// New.
		{Key: "fin_wed", Label: "Copper War Finals", TypeName: "Copper War", DayOffset: intp(3), EventTime: "12:30", WeekStart: 7, WeekEnd: 7, Grade: "cpt_hedge"},
	}
	rows := []SeasonEvent{
		{ID: 1, Label: "Exclusive Weapon: Williams (Tank)", TypeName: "Exclusive Weapon", DayOffset: intp(4), EventTime: "00:00", WeekStart: 1, WeekEnd: 1, IsServerEvent: true, DurationDays: 7, Grade: "unverified"},
		{ID: 2, Label: "Holy Mountain", TypeName: "Holy Mountain", DayOffset: intp(1), EventTime: "00:00", WeekStart: 6, WeekEnd: 6, IsServerEvent: true, DurationDays: 7, Grade: "unverified"},
		{ID: 3, Label: "City Clash (L1)", TypeName: "City Clash", DayOffset: intp(3), EventTime: "12:00", WeekStart: 1, WeekEnd: 1, DurationDays: 1, Grade: "unverified"},
		{ID: 4, Label: "Something the officer added", TypeName: "Rally", DayOffset: intp(2), EventTime: "20:00", WeekStart: 2, WeekEnd: 2, DurationDays: 1, Grade: "unverified"},
	}
	d := diffSeasonTemplate(tmpl, rows)
	byID := map[int]templateDiffChanged{}
	for _, c := range d.Changed {
		byID[c.SeasonEventID] = c
	}
	if c := byID[1]; c.Match != "type_slot" || c.ProvenanceOnly {
		t.Errorf("relabelled EW: %+v, want a type_slot match with a label change", c)
	}
	if c := byID[2]; c.Match != "label" || c.ProvenanceOnly {
		t.Errorf("moved Holy Mountain: %+v, want a label match with a week change", c)
	}
	if c := byID[3]; c.Match != "composite" || !c.ProvenanceOnly {
		t.Errorf("City Clash: %+v, want a composite match that only gains provenance", c)
	}
	if len(d.Added) != 1 || d.Added[0].Key != "fin_wed" {
		t.Errorf("added = %+v, want the Finals Wednesday", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0].SeasonEventID != 4 {
		t.Errorf("removed = %+v, want the officer's own row", d.Removed)
	}
}

func TestTemplateDiffAmbiguousIsNotGuessed(t *testing.T) {
	tmpl := []templateEvent{
		{Key: "w1", Label: "War", TypeName: "War", DayOffset: intp(3), WeekStart: 4, WeekEnd: 4},
		{Key: "w2", Label: "War", TypeName: "War", DayOffset: intp(6), WeekStart: 5, WeekEnd: 5},
	}
	// No slot match, two label matches.
	rows := []SeasonEvent{{ID: 9, Label: "War", TypeName: "War", DayOffset: intp(1), WeekStart: 2, WeekEnd: 2, DurationDays: 1}}
	d := diffSeasonTemplate(tmpl, rows)
	if len(d.Ambiguous) != 1 || len(d.Ambiguous[0].Candidates) != 2 {
		t.Fatalf("ambiguous = %+v, want the row offered two candidates", d.Ambiguous)
	}
	if len(d.Changed) != 0 || len(d.Removed) != 0 {
		t.Errorf("an ambiguous row was also matched or removed: %+v", d)
	}
	if len(d.Added) != 2 {
		t.Errorf("added = %d, want both template rows (neither was claimed)", len(d.Added))
	}
}

// ── Keys and validation on template writes ──────────────────────────────────

func TestEnsureTemplateEventKeys(t *testing.T) {
	in := `[{"key":"custom_war","label":"War"},{"label":"War","extra":{"x":1}},{"label":"War"},{"label":"!!"}]`
	out, err := ensureTemplateEventKeys(in)
	if err != nil {
		t.Fatal(err)
	}
	var evs []map[string]any
	json.Unmarshal([]byte(out), &evs)
	got := []string{}
	for _, e := range evs {
		got = append(got, e["key"].(string))
	}
	want := []string{"custom_war", "custom_war_2", "custom_war_3", "custom_event"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("keys = %v, want %v", got, want)
	}
	if evs[1]["extra"] == nil {
		t.Error("an unknown key was dropped")
	}
	same := `[{"key":"a","label":"A"}]`
	if out, _ := ensureTemplateEventKeys(same); out != same {
		t.Errorf("a fully keyed template was rewritten: %s", out)
	}
}

func TestTemplateUpdateRefusesMalformedJSON(t *testing.T) {
	setupSettingsTestDB(t)
	var id int
	if err := db.QueryRow(`SELECT id FROM season_templates WHERE season_number = 4`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"events":     `{"template_name":"X","season_number":4,"events":"[{","defaults":"{}","trackables":"[]"}`,
		"defaults":   `{"template_name":"X","season_number":4,"events":"[]","defaults":"[]","trackables":"[]"}`,
		"trackables": `{"template_name":"X","season_number":4,"events":"[]","defaults":"{}","trackables":"{"}`,
	} {
		rr := httptest.NewRecorder()
		handleSeasonTemplateUpdate(rr, shReq(http.MethodPut, body, map[string]string{"id": strconv.Itoa(id)}, nil))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, rr.Code)
		}
	}
	var events string
	db.QueryRow(`SELECT events FROM season_templates WHERE id = ?`, id).Scan(&events)
	if !json.Valid([]byte(events)) {
		t.Error("a refused update still wrote the template")
	}
}

// ── The corrected templates (migration 088) ─────────────────────────────────

// shippedTemplateEvents reads a template's events from a fresh, fully migrated database.
func shippedTemplateEvents(t *testing.T, seasonNumber int) []templateEvent {
	t.Helper()
	var raw string
	if err := db.QueryRow(`SELECT events FROM season_templates WHERE season_number = ?`, seasonNumber).Scan(&raw); err != nil {
		t.Fatalf("load S%d template: %v", seasonNumber, err)
	}
	var evs []templateEvent
	if err := json.Unmarshal([]byte(raw), &evs); err != nil {
		t.Fatalf("S%d events do not parse: %v", seasonNumber, err)
	}
	return evs
}

func TestS4TemplateCorrected(t *testing.T) {
	setupSettingsTestDB(t)
	evs := shippedTemplateEvents(t, 4)
	wars := 0
	for _, e := range evs {
		if e.TypeName != "Copper War" {
			continue
		}
		if e.EventTime != "12:30" {
			t.Errorf("%s (%s) at %s, want 12:30", e.Label, e.Key, e.EventTime)
		}
		if e.WeekStart < 4 || e.WeekEnd > 7 {
			t.Errorf("%s runs W%d–%d, outside W4–W7", e.Key, e.WeekStart, e.WeekEnd)
		}
		wars += e.WeekEnd - e.WeekStart + 1
	}
	if wars != 8 {
		t.Errorf("S4 schedules %d Copper Wars, want 8", wars)
	}
	labels := map[int]string{}
	for _, e := range evs {
		if e.TypeName == "Exclusive Weapon" {
			labels[e.WeekStart] = e.Label
		}
	}
	for wk, want := range map[int]string{1: "Lucius", 3: "Adam", 6: "Williams"} {
		if !strings.Contains(labels[wk], want) {
			t.Errorf("W%d Exclusive Weapon = %q, want %s", wk, labels[wk], want)
		}
	}
	var defaults struct {
		KeyEventTotal int    `json:"key_event_total"`
		ResourceName  string `json:"resource_name"`
	}
	var raw string
	db.QueryRow(`SELECT defaults FROM season_templates WHERE season_number = 4`).Scan(&raw)
	json.Unmarshal([]byte(raw), &defaults)
	if defaults.KeyEventTotal != 8 || defaults.ResourceName != "Copper" {
		t.Errorf("S4 defaults = %s, want key_event_total 8 and resource_name Copper", raw)
	}
}

// A season created from the S4 template as migration 034 shipped it, seen through the
// diff once 088 has corrected the template.
func TestTemplateDiffForASeasonFromTheOldS4(t *testing.T) {
	old := migrateTo(t, 87)
	var oldEvents string
	if err := old.QueryRow(`SELECT events FROM season_templates WHERE season_number = 4`).Scan(&oldEvents); err != nil {
		t.Fatal(err)
	}
	var oldRows []templateEvent
	if err := json.Unmarshal([]byte(oldEvents), &oldRows); err != nil {
		t.Fatal(err)
	}

	setupSettingsTestDB(t)
	if _, err := db.Exec(`INSERT INTO seasons (id, name, season_number, start_date, week_count,
		key_event_name, key_event_required, tier_active_min_pct, tier_at_risk_min_pct, is_active)
		VALUES (1, 'Evernight Isle', 4, '2026-11-02', 8, 'Copper War', 4, 70, 60, 1)`); err != nil {
		t.Fatal(err)
	}
	for _, e := range oldRows {
		if _, err := db.Exec(`INSERT INTO season_events (season_id, label, type_name, day_offset, event_time,
			week_start, week_end, notes, is_server_event, duration_days) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.Label, e.TypeName, e.DayOffset, e.EventTime, e.WeekStart, e.WeekEnd, e.Notes,
			boolToInt(e.IsServerEvent), e.DurationDays); err != nil {
			t.Fatal(err)
		}
	}

	rr := httptest.NewRecorder()
	handleSeasonTemplateDiff(rr, shReq(http.MethodGet, "", map[string]string{"id": "1"}, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Diff templateDiff `json:"diff"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	d := resp.Diff
	relabelled, retimed := 0, 0
	for _, c := range d.Changed {
		for _, f := range c.Fields {
			if f.Field == "label" && strings.HasPrefix(f.Template, "Exclusive Weapon:") {
				relabelled++
			}
			if f.Field == "time" && f.Template == "12:30" {
				retimed++
			}
		}
	}
	if relabelled != 3 {
		t.Errorf("%d Exclusive Weapons reported relabelled, want 3", relabelled)
	}
	// Both W4–W6 Copper War rows and the old W7 Saturday final, each matched to its own key.
	if retimed != 3 {
		t.Errorf("%d Copper War rows reported retimed, want 3", retimed)
	}
	added := map[string]bool{}
	for _, a := range d.Added {
		added[a.Key] = true
	}
	for _, k := range []string{"s4_copper_war_finals_wed", "s4_holy_mountain", "s4_transfer_surge"} {
		if !added[k] {
			t.Errorf("%s not reported as added", k)
		}
	}
	if len(d.Ambiguous) != 0 || len(d.Removed) != 0 {
		t.Errorf("ambiguous %+v / removed %+v, want none", d.Ambiguous, d.Removed)
	}
}

func TestSeasonCreateStampsTemplateProvenance(t *testing.T) {
	setupSettingsTestDB(t)
	var tmplID int
	db.QueryRow(`SELECT id FROM season_templates WHERE season_number = 4`).Scan(&tmplID)
	rr := httptest.NewRecorder()
	handleSeasonCreate(rr, shReq(http.MethodPost,
		`{"template_id":`+strconv.Itoa(tmplID)+`,"start_date":"2030-01-07"}`, nil, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var rows, keyed, graded int
	db.QueryRow(`SELECT COUNT(*), SUM(template_key != ''), SUM(grade != 'unverified' OR source != '')
		FROM season_events`).Scan(&rows, &keyed, &graded)
	if rows != len(shippedTemplateEvents(t, 4)) || keyed != rows || graded != rows {
		t.Errorf("rows %d, keyed %d, graded %d — every row should carry its key and provenance", rows, keyed, graded)
	}
}

func TestS5AndS6TemplatesCorrected(t *testing.T) {
	setupSettingsTestDB(t)
	s5 := shippedTemplateEvents(t, 5)
	palace := 0
	for _, e := range s5 {
		if e.TypeName == "Golden Palace" {
			palace++
			if e.Grade != "conflict" || *e.DayOffset != 6 || e.EventTime != "13:00" {
				t.Errorf("%s = day %d %s graded %s, want Saturday 13:00 graded conflict", e.Key, *e.DayOffset, e.EventTime, e.Grade)
			}
		}
		if e.TypeName == "Bank Capture" && e.Grade != "unverified" {
			t.Errorf("%s graded %s; the kept Bank Capture rows are unverified", e.Key, e.Grade)
		}
	}
	if palace != 3 {
		t.Errorf("S5 has %d Golden Palace rows, want W5, W6 and W7", palace)
	}

	s6 := shippedTemplateEvents(t, 6)
	clashes := 0
	for _, e := range s6 {
		if e.TypeName == "City Clash" && e.WeekStart > 2 {
			t.Errorf("S6 still schedules %s in W%d; the official calendar has L1–L3 only", e.Label, e.WeekStart)
		}
		if e.TypeName == "Faction Clash" {
			if e.WeekStart != 2 || e.WeekEnd != 7 {
				t.Errorf("%s runs W%d–%d, want W2–W7", e.Key, e.WeekStart, e.WeekEnd)
			}
			clashes += e.WeekEnd - e.WeekStart + 1
		}
	}
	if clashes != 12 {
		t.Errorf("S6 schedules %d Faction Clashes, want 12", clashes)
	}
}

// The existing-install rule: 088 corrects a template column only while it still reads as
// shipped, column by column, and leaves a malformed column alone instead of failing.
func TestSeedCorrectionsLeaveEditedColumnsAlone(t *testing.T) {
	conn := migrateTo(t, 87)
	exec := func(q string) {
		t.Helper()
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	// An operator relabelled one S4 row, changed S5's week count and broke S6's events.
	exec(`UPDATE season_templates SET events = replace(events, 'City Clash (L1)', 'City Clash (Level 1)') WHERE season_number = 4`)
	exec(`UPDATE season_templates SET defaults = '{"week_count":9,"key_event_name":"Finance Tycoon / Bank Capture","key_event_required":2}' WHERE season_number = 5`)
	exec(`UPDATE season_templates SET events = '[{"label": broken' WHERE season_number = 6`)

	read := func(sn int) (events, defaults string) {
		t.Helper()
		conn.QueryRow(`SELECT events, defaults FROM season_templates WHERE season_number = ?`, sn).Scan(&events, &defaults)
		return
	}
	s4Events, _ := read(4)
	_, s5Defaults := read(5)
	s6Events, s6Defaults := read(6)

	if err := goose.UpTo(conn, "migrations", 88); err != nil {
		t.Fatalf("088 failed on an edited install: %v", err)
	}

	e4, d4 := read(4)
	if e4 != s4Events {
		t.Error("S4's edited events were overwritten")
	}
	if !strings.Contains(d4, `"key_event_total":8`) {
		t.Errorf("S4's unedited defaults were not corrected: %s", d4)
	}
	e5, d5 := read(5)
	if d5 != s5Defaults {
		t.Error("S5's edited defaults were overwritten")
	}
	if !strings.Contains(e5, `"s5_golden_palace_w5"`) {
		t.Error("S5's unedited events were not corrected")
	}
	e6, d6 := read(6)
	if e6 != s6Events {
		t.Error("S6's malformed events were touched")
	}
	if d6 == s6Defaults || !strings.Contains(d6, `"key_event_total":12`) {
		t.Errorf("S6's unedited defaults were not corrected: %s", d6)
	}
}

// Every shipped template row is keyed, graded and sourced once 088 has run.
func TestEveryShippedTemplateRowHasProvenance(t *testing.T) {
	setupSettingsTestDB(t)
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM season_templates`).Scan(&n)
	if n != 6 {
		t.Fatalf("%d templates, want 6", n)
	}
	for sn := 1; sn <= 6; sn++ {
		seen := map[string]bool{}
		for _, e := range shippedTemplateEvents(t, sn) {
			if e.Key == "" || seen[e.Key] {
				t.Errorf("S%d %q: key %q missing or repeated", sn, e.Label, e.Key)
			}
			seen[e.Key] = true
			if !validEventGrades[e.Grade] {
				t.Errorf("S%d %s: grade %q", sn, e.Key, e.Grade)
			}
			if strings.TrimSpace(e.Source) == "" {
				t.Errorf("S%d %s: no source", sn, e.Key)
			}
		}
	}
}

func TestS1ToS3TemplatesCorrected(t *testing.T) {
	setupSettingsTestDB(t)
	var defaults, trackables string
	db.QueryRow(`SELECT defaults, trackables FROM season_templates WHERE season_number = 3`).Scan(&defaults, &trackables)
	if !strings.Contains(defaults, `"key_event_name":"Goldust Conquest"`) {
		t.Errorf("S3 key event: %s", defaults)
	}
	if !strings.Contains(trackables, `"key":"goldust_conquest"`) || strings.Contains(trackables, "spice_war") {
		t.Errorf("S3 trackables: %s", trackables)
	}
	for _, e := range shippedTemplateEvents(t, 3) {
		if strings.Contains(e.Label, "Spice") || e.TypeName == "Spice War" {
			t.Errorf("S3 still carries %q (%s)", e.Label, e.TypeName)
		}
	}
	for _, e := range shippedTemplateEvents(t, 2) {
		switch e.Key {
		case "s2_rare_soil_war_wed", "s2_rare_soil_war_sat":
			if e.WeekStart != 4 || e.WeekEnd != 5 {
				t.Errorf("%s runs W%d–%d, want the 20%% weeks W4–W5", e.Key, e.WeekStart, e.WeekEnd)
			}
		case "s2_rare_soil_showdown_wed", "s2_rare_soil_showdown_sat":
			if e.WeekStart != 6 || e.WeekEnd != 7 {
				t.Errorf("%s runs W%d–%d, want the 30%% weeks W6–W7", e.Key, e.WeekStart, e.WeekEnd)
			}
		case "s2_transfer_surge":
			if e.WeekStart != 9 || *e.DayOffset != 1 {
				t.Errorf("Transfer Surge at W%d day %d, want W9 day 1", e.WeekStart, *e.DayOffset)
			}
		}
	}
}
