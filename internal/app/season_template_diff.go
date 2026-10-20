// season_template_diff.go — season template provenance and the template-change notice.
//
// Migration 088 corrected the shipped templates, but never rewrites a season already made
// from one: the season's rows are the officer's plan. Edit Season instead shows what the
// template now says differently (GET /api/season-hub/seasons/{id}/template-diff), and the
// officer applies a row at a time through the ordinary season-event endpoints. Pushed
// calendar rows are not moved by an Apply; the next push reports any drift, as always.

package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

// validEventGrades are the provenance grades a template or season event row may carry
// (migration 088's header defines each). In-game observation outranks every one of them.
var validEventGrades = map[string]bool{
	"official":   true,
	"cpt_hedge":  true,
	"guide":      true,
	"owner":      true,
	"requester":  true,
	"conflict":   true,
	"unverified": true,
}

// normaliseEventGrade maps an empty or unknown grade to "unverified" — a row nobody
// graded is exactly that.
func normaliseEventGrade(g string) string {
	if validEventGrades[g] {
		return g
	}
	return "unverified"
}

// validateTemplateJSON refuses a template whose events, defaults or trackables blob is not
// the JSON shape every reader assumes. Season create and the diff json.Unmarshal these and
// carry on with nothing on failure, so a malformed blob would otherwise create an empty
// season without a word.
func validateTemplateJSON(events, defaults, trackables string) string {
	var arr []json.RawMessage
	if json.Unmarshal([]byte(events), &arr) != nil {
		return "events must be a JSON array"
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(defaults), &obj) != nil {
		return "defaults must be a JSON object"
	}
	if json.Unmarshal([]byte(trackables), &arr) != nil {
		return "trackables must be a JSON array"
	}
	return ""
}

// templateEvent is one row of season_templates.events.
type templateEvent struct {
	Key           string `json:"key"`
	Label         string `json:"label"`
	TypeName      string `json:"type_name"`
	DayOffset     *int   `json:"day_offset"`
	EventTime     string `json:"event_time"`
	WeekStart     int    `json:"week_start"`
	WeekEnd       int    `json:"week_end"`
	Notes         string `json:"notes"`
	IsServerEvent bool   `json:"is_server_event"`
	DurationDays  int    `json:"duration_days"`
	Level         *int   `json:"level"`
	Source        string `json:"source"`
	Grade         string `json:"grade"`
	// EventTypeID is resolved from TypeName for the response, so an Apply can send it.
	// Nil when no schedule_event_types row has that name yet.
	EventTypeID *int `json:"event_type_id"`
}

func (t templateEvent) normalised() templateEvent {
	if t.EventTime == "" {
		t.EventTime = "20:00"
	}
	if t.DurationDays < 1 {
		t.DurationDays = 1
	}
	t.Grade = normaliseEventGrade(t.Grade)
	return t
}

// templateDiffField is one value that differs between a season row and its template row.
type templateDiffField struct {
	Field    string `json:"field"`
	Season   string `json:"season"`
	Template string `json:"template"`
}

type templateDiffChanged struct {
	SeasonEventID int                 `json:"season_event_id"`
	Label         string              `json:"label"`
	Match         string              `json:"match"` // key | composite | type_slot | label
	Fields        []templateDiffField `json:"fields"`
	// ProvenanceOnly: nothing but the key, source or grade differs — the season's row
	// already reads as the template does, it just predates provenance.
	ProvenanceOnly bool          `json:"provenance_only"`
	Template       templateEvent `json:"template"`
}

type templateDiffRemoved struct {
	SeasonEventID int    `json:"season_event_id"`
	Label         string `json:"label"`
	WeekStart     int    `json:"week_start"`
	DayOffset     *int   `json:"day_offset"`
}

type templateDiffAmbiguous struct {
	SeasonEventID int             `json:"season_event_id"`
	Label         string          `json:"label"`
	Candidates    []templateEvent `json:"candidates"`
}

type templateDiff struct {
	Changed   []templateDiffChanged   `json:"changed"`
	Added     []templateEvent         `json:"added"`
	Removed   []templateDiffRemoved   `json:"removed"`
	Ambiguous []templateDiffAmbiguous `json:"ambiguous"`
}

func (d templateDiff) empty() bool {
	return len(d.Changed) == 0 && len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Ambiguous) == 0
}

func intPtrString(p *int) string {
	if p == nil {
		return ""
	}
	return strconv.Itoa(*p)
}

// diffSeasonTemplate compares a season's rows with its template's. Pure.
//
// A row stamped with a template_key is matched by key, and only by key. A row without one
// (a season created before migration 088) is matched by a fallback that tolerates exactly
// one kind of change at a time, in this order: the same label, type, first week and day;
// else the same type in the same week-and-day slot (a relabel — S4's Exclusive Weapons);
// else the same label and type (a move). A tier that finds two or more candidates reports
// the row as ambiguous for the officer to choose, rather than guessing between them.
func diffSeasonTemplate(tmpl []templateEvent, rows []SeasonEvent) templateDiff {
	diff := templateDiff{
		Changed: []templateDiffChanged{}, Added: []templateEvent{},
		Removed: []templateDiffRemoved{}, Ambiguous: []templateDiffAmbiguous{},
	}
	for i := range tmpl {
		tmpl[i] = tmpl[i].normalised()
	}
	used := make([]bool, len(tmpl))
	byKey := map[string]int{}
	for i, t := range tmpl {
		if t.Key != "" {
			if _, dup := byKey[t.Key]; !dup {
				byKey[t.Key] = i
			}
		}
	}

	matched := map[int]int{} // season row index → template index
	matchKind := map[int]string{}
	var unkeyed []int
	for ri, r := range rows {
		if r.TemplateKey == "" {
			unkeyed = append(unkeyed, ri)
			continue
		}
		if ti, ok := byKey[r.TemplateKey]; ok && !used[ti] {
			used[ti] = true
			matched[ri] = ti
			matchKind[ri] = "key"
			continue
		}
		diff.Removed = append(diff.Removed, templateDiffRemoved{SeasonEventID: r.ID, Label: r.Label,
			WeekStart: r.WeekStart, DayOffset: r.DayOffset})
	}

	tiers := []struct {
		name string
		same func(r SeasonEvent, t templateEvent) bool
	}{
		{"composite", func(r SeasonEvent, t templateEvent) bool {
			return r.Label == t.Label && r.TypeName == t.TypeName && r.WeekStart == t.WeekStart &&
				intPtrString(r.DayOffset) == intPtrString(t.DayOffset)
		}},
		{"type_slot", func(r SeasonEvent, t templateEvent) bool {
			return r.TypeName == t.TypeName && r.WeekStart == t.WeekStart &&
				intPtrString(r.DayOffset) == intPtrString(t.DayOffset)
		}},
		{"label", func(r SeasonEvent, t templateEvent) bool {
			return r.Label == t.Label && r.TypeName == t.TypeName
		}},
	}
	pending := unkeyed
	for _, tier := range tiers {
		var next []int
		for _, ri := range pending {
			var cands []int
			for ti, t := range tmpl {
				if !used[ti] && tier.same(rows[ri], t) {
					cands = append(cands, ti)
				}
			}
			switch len(cands) {
			case 0:
				next = append(next, ri)
			case 1:
				used[cands[0]] = true
				matched[ri] = cands[0]
				matchKind[ri] = tier.name
			default:
				amb := templateDiffAmbiguous{SeasonEventID: rows[ri].ID, Label: rows[ri].Label}
				for _, ti := range cands {
					amb.Candidates = append(amb.Candidates, tmpl[ti])
				}
				diff.Ambiguous = append(diff.Ambiguous, amb)
			}
		}
		pending = next
	}
	for _, ri := range pending {
		r := rows[ri]
		diff.Removed = append(diff.Removed, templateDiffRemoved{SeasonEventID: r.ID, Label: r.Label,
			WeekStart: r.WeekStart, DayOffset: r.DayOffset})
	}

	for ri, r := range rows {
		ti, ok := matched[ri]
		if !ok {
			continue
		}
		t := tmpl[ti]
		var fields []templateDiffField
		add := func(name, sv, tv string) {
			if sv != tv {
				fields = append(fields, templateDiffField{Field: name, Season: sv, Template: tv})
			}
		}
		add("label", r.Label, t.Label)
		add("type", r.TypeName, t.TypeName)
		add("week_start", strconv.Itoa(r.WeekStart), strconv.Itoa(t.WeekStart))
		add("week_end", strconv.Itoa(r.WeekEnd), strconv.Itoa(t.WeekEnd))
		add("day", intPtrString(r.DayOffset), intPtrString(t.DayOffset))
		add("time", r.EventTime, t.EventTime)
		add("server_event", strconv.FormatBool(r.IsServerEvent), strconv.FormatBool(t.IsServerEvent))
		add("duration_days", strconv.Itoa(r.DurationDays), strconv.Itoa(t.DurationDays))
		add("notes", r.Notes, t.Notes)
		substantive := len(fields)
		add("template_key", r.TemplateKey, t.Key)
		add("source", r.Source, t.Source)
		add("grade", r.Grade, t.Grade)
		if len(fields) == 0 {
			continue
		}
		diff.Changed = append(diff.Changed, templateDiffChanged{
			SeasonEventID: r.ID, Label: r.Label, Match: matchKind[ri], Fields: fields,
			ProvenanceOnly: substantive == 0, Template: t,
		})
	}
	for ti, t := range tmpl {
		if !used[ti] {
			diff.Added = append(diff.Added, t)
		}
	}
	return diff
}

// loadSeasonEventRows reads a season's rows in the shape the diff compares.
func loadSeasonEventRows(q rowQueryer, seasonID int) ([]SeasonEvent, error) {
	rows, err := q.Query(`SELECT id, label, type_name, day_offset, event_time, week_start, week_end,
		notes, is_server_event, duration_days, template_key, source, grade
		FROM season_events WHERE season_id = ? ORDER BY week_start, day_offset, id`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SeasonEvent
	for rows.Next() {
		var ev SeasonEvent
		var day sql.NullInt64
		var srv int
		if err := rows.Scan(&ev.ID, &ev.Label, &ev.TypeName, &day, &ev.EventTime, &ev.WeekStart, &ev.WeekEnd,
			&ev.Notes, &srv, &ev.DurationDays, &ev.TemplateKey, &ev.Source, &ev.Grade); err != nil {
			return nil, err
		}
		if day.Valid {
			v := int(day.Int64)
			ev.DayOffset = &v
		}
		ev.IsServerEvent = srv == 1
		if ev.DurationDays < 1 {
			ev.DurationDays = 1
		}
		ev.SeasonID = seasonID
		out = append(out, ev)
	}
	return out, rows.Err()
}

// GET /api/season-hub/seasons/{id}/template-diff — what the season's template (matched by
// season number, as migration 070 does) now says differently from the season's own rows.
// Read-only: the Apply is the ordinary season-event create, update and delete.
func handleSeasonTemplateDiff(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid season ID", http.StatusBadRequest)
		return
	}
	s, err := loadSeasonByID(id)
	if err == sql.ErrNoRows {
		http.Error(w, "Season not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleSeasonTemplateDiff: load season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var tmplID int
	var tmplName, eventsJSON string
	err = db.QueryRow(`SELECT id, template_name, events FROM season_templates
		WHERE season_number = ? ORDER BY id LIMIT 1`, s.SeasonNumber).Scan(&tmplID, &tmplName, &eventsJSON)
	if err == sql.ErrNoRows {
		writeJSON(w, map[string]any{"template": nil, "archived": s.ArchivedAt != "", "diff": nil})
		return
	}
	if err != nil {
		slog.Error("handleSeasonTemplateDiff: load template", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	var tmpl []templateEvent
	if err := json.Unmarshal([]byte(eventsJSON), &tmpl); err != nil {
		writeJSON(w, map[string]any{
			"template": map[string]any{"id": tmplID, "name": tmplName},
			"archived": s.ArchivedAt != "", "diff": nil,
			"error": "The template's events are not valid JSON; fix them in Settings → Season Templates.",
		})
		return
	}

	// Resolve type ids before reading the season's rows — one connection, no nesting.
	typeIDs := map[string]*int{}
	for _, t := range tmpl {
		if t.TypeName == "" {
			continue
		}
		if _, ok := typeIDs[t.TypeName]; ok {
			continue
		}
		var tid int
		if err := db.QueryRow(`SELECT id FROM schedule_event_types WHERE name = ?`, t.TypeName).Scan(&tid); err == nil {
			v := tid
			typeIDs[t.TypeName] = &v
		} else {
			typeIDs[t.TypeName] = nil
		}
	}
	for i := range tmpl {
		tmpl[i].EventTypeID = typeIDs[tmpl[i].TypeName]
	}

	rows, err := loadSeasonEventRows(db, id)
	if err != nil {
		slog.Error("handleSeasonTemplateDiff: load rows", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	diff := diffSeasonTemplate(tmpl, rows)
	writeJSON(w, map[string]any{
		"template": map[string]any{"id": tmplID, "name": tmplName},
		"archived": s.ArchivedAt != "",
		"empty":    diff.empty(),
		"diff":     diff,
	})
}

// templateKeySlug turns a label into the stem of a generated template key.
func templateKeySlug(label string) string {
	var b strings.Builder
	lastUnderscore := true
	for _, r := range strings.ToLower(label) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

// ensureTemplateEventKeys gives every template row without a key one generated from its
// label, unique within the template, so an officer's new rows get the same stable identity
// the shipped rows carry. Unknown keys on each row are kept as they were.
func ensureTemplateEventKeys(eventsJSON string) (string, error) {
	var evs []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(eventsJSON), &evs); err != nil {
		return "", err
	}
	seen := map[string]bool{}
	for _, e := range evs {
		var k string
		if raw, ok := e["key"]; ok {
			json.Unmarshal(raw, &k)
		}
		if k != "" {
			seen[k] = true
		}
	}
	changed := false
	for _, e := range evs {
		var k string
		if raw, ok := e["key"]; ok {
			json.Unmarshal(raw, &k)
		}
		if k != "" {
			continue
		}
		var label string
		json.Unmarshal(e["label"], &label)
		stem := "custom_" + templateKeySlug(label)
		if stem == "custom_" {
			stem = "custom_event"
		}
		key := stem
		for n := 2; seen[key]; n++ {
			key = fmt.Sprintf("%s_%d", stem, n)
		}
		seen[key] = true
		enc, _ := json.Marshal(key)
		e["key"] = enc
		changed = true
	}
	if !changed {
		return eventsJSON, nil
	}
	out, err := json.Marshal(evs)
	return string(out), err
}
