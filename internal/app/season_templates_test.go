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
