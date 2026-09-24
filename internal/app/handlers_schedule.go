package app

import (
	"database/sql"
	"encoding/json"
	"errors"
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

// yesNo renders a flag for the activity log's change list.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// validHHMM is the shape check AND the range check.
//
// reHHMM alone accepts "29:99": it only says four digits with a colon. That was
// tolerable while such a value merely looked odd on a card, but times are now
// compared as strings to decide what goes into an alliance-wide announcement,
// and a nonsense bound would take part in those comparisons silently — sorting
// after every real time and quietly emptying a window. Parse it as well.
func validHHMM(s string) bool {
	if !reHHMM.MatchString(s) {
		return false
	}
	_, err := time.Parse("15:04", s)
	return err == nil
}

// --- Shared helpers ---

// invalidGeneratedEvent is one date the generator declined, reported back so the
// officer can see which dates the app refused and why rather than reading a
// smaller number than they expected and guessing.
type invalidGeneratedEvent struct {
	Date   string `json:"date"`
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

// switchedGeneratedEvent is one date on which the generator produced a different
// variant of the Alliance Exercise slot than the request's checkbox named. Same
// reasoning as invalidGeneratedEvent: a result the officer did not ask for, with
// no explanation attached, reads as a broken generator.
type switchedGeneratedEvent struct {
	Date   string `json:"date"`
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
}

// validateSystemEventRules applies every date/time rule for an MG/ZS candidate
// against what is in the database. q is db or a tx (rowQuerier,
// handlers_lastrank.go:23); excludeID skips the row being edited, and is 0 for a
// new one. Returns a user-facing message naming the rule it broke, or "" when
// the candidate is legal.
//
// This is the ONE place the date/time rules live. schedule_events has four write
// paths — manual create, manual update, bulk generate and the Season Hub push —
// and until this existed only the two manual ones applied any rule at all, so a
// generated or pushed event could sit on the calendar in a state the same
// officer would have been refused by hand.
//
// Bulk callers validate and INSERT one row at a time, in date order. Each
// accepted row is in the database before the next candidate is checked, so a
// generated chain is validated against itself with no separate bookkeeping.
// Never plan a batch and insert it afterwards without re-validating.
//
// The level rule stays in validateSystemLevel: its call timing is
// caller-specific, because the update path deliberately grandfathers a level the
// officer did not touch.
// scheduleTypeRules is everything the validator needs to know about an event's
// type. The caller loads it once; the VALIDATOR decides which rules apply.
//
// That division matters. The call sites used to gate on `is_system == 1` before
// calling in at all, which was fine while every rule was a system-type rule. It
// stopped being fine the moment a CUSTOM type could carry a parent window
// (Glacieradon): a gate on is_system would have left the window rule bypassable on
// create, on update and on the Season Hub push — three separate holes, one per
// gate. The gates are gone; add a rule here, not a condition out there.
type scheduleTypeRules struct {
	Name          string
	Short         string
	IsSystem      bool
	ServerEventID *int
}

// loadScheduleTypeRules reads one type's rule inputs. q is db or a tx.
func loadScheduleTypeRules(q rowQuerier, typeID int) (scheduleTypeRules, error) {
	var tr scheduleTypeRules
	var isSystem int
	err := q.QueryRow(`SELECT name, short_name, is_system, server_event_id FROM schedule_event_types WHERE id = ?`, typeID).
		Scan(&tr.Name, &tr.Short, &isSystem, &tr.ServerEventID)
	tr.IsSystem = isSystem == 1
	return tr, err
}

// validateEventRules applies every date/time rule for a candidate event. It is the
// single entry point for all four write paths into schedule_events.
//
// System-only rules (the Alliance Exercise family gap, the 21:59 cutoff, the
// variant cutover, the ZS gap) run when the type is a system type. The window rule
// runs whenever the type has a parent, system or not.
// eventCandidate is the row a write path proposes. A struct rather than loose
// strings so a rule that needs more than a date and a time — Desert Storm's task
// force — lives in the validator like every other rule, and all four write paths
// get it without a condition at the call site.
type eventCandidate struct {
	Date      string
	Time      string
	TaskForce *string // Desert Storm only: "A" or "B"
	// KeepMissingTaskForce lets an UPDATE of a legacy Desert Storm row (migrated
	// from storm_attendance with no task force) through without supplying one —
	// the level rule grandfathers an untouched value the same way. Never set on a
	// create.
	KeepMissingTaskForce bool
}

func validateEventRules(q rowQuerier, tr scheduleTypeRules, c eventCandidate, excludeID int) (string, error) {
	if c.TaskForce != nil && tr.Short != "DS" {
		return "Only Desert Storm events carry a task force", nil
	}
	if tr.IsSystem {
		if msg, err := validateSystemEventRules(q, tr.Short, c, excludeID); err != nil || msg != "" {
			return msg, err
		}
	}
	if tr.ServerEventID != nil {
		return validateEncounterWindow(q, tr.Name, *tr.ServerEventID, c.Date)
	}
	return "", nil
}

func validateSystemEventRules(q rowQuerier, short string, c eventCandidate, excludeID int) (string, error) {
	date, tm := c.Date, c.Time
	switch short {
	case "MG", "LS":
		if msg, err := validateAllianceExerciseVariant(q, short, date); err != nil || msg != "" {
			return msg, err
		}
		if tm >= "22:00" {
			return allianceExerciseName(short) + " must start by 21:59 ST", nil
		}
		return validateAllianceExerciseGap(q, excludeID, date)
	case "ZS":
		return validateZSGap(q, excludeID, date)
	case "DS":
		return validateDesertStorm(q, c, excludeID)
	}
	return "", nil
}

// validateDesertStorm: each task force fights its own battle, so a Desert Storm
// occurrence names its task force and there is at most one per task force per
// date. The partial unique index (081) is the race backstop; this is what names
// the rule. A legacy row with no task force is grandfathered on update only.
func validateDesertStorm(q rowQuerier, c eventCandidate, excludeID int) (string, error) {
	if c.TaskForce == nil {
		if c.KeepMissingTaskForce {
			return "", nil
		}
		return "Desert Storm needs a task force, A or B", nil
	}
	tf := *c.TaskForce
	if tf != "A" && tf != "B" {
		return "Desert Storm's task force must be A or B", nil
	}
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM schedule_events se
		JOIN schedule_event_types t ON t.id = se.event_type_id
		WHERE t.short_name = 'DS' AND se.event_date = ? AND se.task_force = ? AND se.id != ?`,
		c.Date, tf, excludeID).Scan(&n); err != nil {
		return "", err
	}
	if n > 0 {
		return "Desert Storm Task Force " + tf + " is already scheduled on " + c.Date, nil
	}
	return "", nil
}

// allianceExerciseShorts are the two variants of the ONE Alliance Exercise slot:
// Marshal's Guard up to Season 3 day 57, Large Sandworm from day 58.
//
// They are one slot in the game, so the cadence rule spans both — an MG on Monday
// blocks an LS on Tuesday exactly as it blocks another MG. Keeping the family as a
// Go constant rather than a column follows the same reasoning as the gap
// constants themselves (see standing-decisions.md): the alternative is a setting
// nobody can tell is wrong.
//
// Deliberately NOT expressed by renaming the stored type. `season_events.type_name`
// and `season_templates.events[].type_name` are stored strings that Sync Event
// Types re-links on, and migration 070 exists because those links broke once.
var allianceExerciseShorts = []string{"MG", "LS"}

func allianceExerciseName(short string) string {
	if short == "LS" {
		return "Large Sandworm"
	}
	return "Marshal's Guard"
}

// sandwormCutoverDays is Season 3 day 58 expressed as an offset from the season's
// start date: day 1 is the start date itself, so day 58 is start + 57.
const sandwormCutoverDays = 57

// sandwormCutover returns the first date on which the Alliance Exercise slot runs
// Large Sandworm instead of Marshal's Guard, derived from the seasons table.
//
// ok is false when there is no Season 3 row, and that is a real state, not an
// error: a server that has not reached Season 3 keeps running Marshal's Guard and
// no cutover applies. EVERY caller must test ok before comparing — a Go string
// compare against "" is true for every date, so an unguarded `date >= cutover`
// would push the whole calendar past a cutover that does not exist.
func sandwormCutover(q rowQuerier) (string, bool, error) {
	var cutover string
	err := q.QueryRow(`SELECT date(start_date, ?) FROM seasons WHERE season_number = 3`,
		fmt.Sprintf("+%d days", sandwormCutoverDays)).Scan(&cutover)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return cutover, cutover != "", nil
}

// validateAllianceExerciseVariant rejects the variant the game does not offer on
// that date, in BOTH directions.
//
// This is what stops the generator, the Season Hub push and both manual paths
// recreating the stranded rows migration 074 has just fixed. Without it the
// migration is a one-off tidy-up that the next 90-day generate undoes.
func validateAllianceExerciseVariant(q rowQuerier, short, date string) (string, error) {
	cutover, ok, err := sandwormCutover(q)
	if err != nil || !ok {
		return "", err
	}
	if short == "MG" && date >= cutover {
		return fmt.Sprintf("Marshal's Guard is not available from %s (Season 3 day %d) — schedule a Large Sandworm",
			cutover, sandwormCutoverDays+1), nil
	}
	if short == "LS" && date < cutover {
		return fmt.Sprintf("Large Sandworm does not start until %s (Season 3 day %d) — schedule a Marshal's Guard",
			cutover, sandwormCutoverDays+1), nil
	}
	return "", nil
}

// validateAllianceExerciseGap rejects an Alliance Exercise placed on the day
// before or the day after another one, of EITHER variant. The two share one slot
// in the game, so the cadence is a property of the slot, not of the type.
func validateAllianceExerciseGap(q rowQuerier, excludeID int, date string) (string, error) {
	conflict, name, err := nearestSystemEventWithin(q, allianceExerciseShorts, excludeID, date, mgGapDays-1)
	if err != nil || conflict == "" {
		return "", err
	}
	return fmt.Sprintf("Alliance Exercise cannot run on consecutive days — conflicts with the %s on %s", name, conflict), nil
}

// mgGapDays is the MG cadence: the game refuses a Marshal's Guard on the day
// after another one, so consecutive dates are illegal and D+2 is the minimum.
//
// This is a rule on DATES and is INDEPENDENT of the 21:59 start-time cutoff:
// the two never interact, so an MG that starts at 21:59 still permits one two
// days later at 00:30. The every-other-day cadence has been advertised in the
// event-form hint since the schedule was rewritten and was enforced nowhere;
// the generator's stepping happened to satisfy it, so nothing but a manual
// create or edit could break it — which is exactly the path officers use.
const mgGapDays = 2

// The Alliance Exercise cadence rule lives in validateAllianceExerciseGap, above:
// it looks both ways, includes the date itself, spans both variants of the slot,
// and names the event it compared against so a disagreement with the game is
// visible rather than inferred.

// zsGapDays is the ZS cadence: a siege may not fall within two clear game days
// of another siege, in either direction — the next one unlocks at the 00:00
// reset on D+3.
//
// This is a rule on DATES. The start time plays no part in it. The 71.5-hour
// figure it replaces was a single observation from a 00:30 start, generalised
// into an interval, and it was wrong in both directions: it let a 23:00 Monday
// siege be followed by one at 23:00 on the Thursday *and* on the Wednesday two
// days later if the times drifted far enough apart. An all-day ZS stores
// event_time '00:00' and needs no special case here — the date is the date.
const zsGapDays = 3

// validateZSGap rejects a ZS on date when another siege falls within
// zsGapDays-1 clear days of it, in EITHER direction: under a date rule a siege
// placed one day before an existing one is exactly as illegal as one placed the
// day after, and the old backwards-only check silently allowed the former. The
// window includes the date itself: two sieges on one day is the same rule
// broken harder, not a special case.
//
// The message names the rule and the date it compared against, so an officer who
// believes the game disagrees can see precisely what the app decided and report
// it. That is the alternative adopted instead of making the gap configurable —
// a wrong setting is worse than a wrong constant, because nobody can tell
// whether the app or the game is wrong.
func validateZSGap(q rowQuerier, excludeID int, date string) (string, error) {
	conflict, _, err := nearestSystemEventWithin(q, []string{"ZS"}, excludeID, date, zsGapDays-1)
	if err != nil || conflict == "" {
		return "", err
	}
	c, err := time.Parse("2006-01-02", conflict)
	if err != nil {
		return "", err
	}
	next := c.AddDate(0, 0, zsGapDays).Format("2006-01-02")
	return fmt.Sprintf("ZS needs two clear days between sieges — conflicts with the ZS on %s (next eligible date %s)",
		conflict, next), nil
}

// nearestSystemEventWithin returns the event_date and the type NAME of the closest
// event of any of the given system types lying within clearDays either side of
// date (inclusive of date itself), or "" when there is none. excludeID skips the
// row being edited.
//
// It takes a SET of short names because the Alliance Exercise slot has two
// variants (see allianceExerciseShorts) and its cadence spans both: an MG on
// Monday blocks a Large Sandworm on Tuesday. The name comes back so the rejection
// can say which event it compared against rather than just "an MG" — an officer
// who believes the game disagrees needs to see exactly what the app decided.
//
// Both date rules are the same query with a different radius, so they share it
// rather than drifting apart. Dates are stepped with AddDate — calendar
// arithmetic on y/m/d over time.Parse values, which are UTC, so neither the host
// timezone nor a DST transition can move the bounds.
func nearestSystemEventWithin(q rowQuerier, shorts []string, excludeID int, date string, clearDays int) (string, string, error) {
	if len(shorts) == 0 {
		return "", "", nil
	}
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return "", "", err
	}
	lo := d.AddDate(0, 0, -clearDays).Format("2006-01-02")
	hi := d.AddDate(0, 0, clearDays).Format("2006-01-02")

	args := make([]any, 0, len(shorts)+4)
	for _, s := range shorts {
		args = append(args, s)
	}
	args = append(args, excludeID, lo, hi, date)
	// #nosec G202 — the placeholder list is built from len(shorts), not from input.
	query := `
		SELECT se.event_date, t.name
		FROM schedule_events se
		JOIN schedule_event_types t ON t.id = se.event_type_id
		WHERE t.short_name IN (` + strings.TrimSuffix(strings.Repeat("?,", len(shorts)), ",") + `) AND se.id != ?
		  AND se.event_date >= ? AND se.event_date <= ?
		ORDER BY abs(julianday(se.event_date) - julianday(?)) ASC
		LIMIT 1`

	var conflict, name string
	err = q.QueryRow(query, args...).Scan(&conflict, &name)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	return conflict, name, nil
}

// maxEventLevelCeiling bounds the configurable ceiling itself. It is a sanity
// bound on operator input, not a statement about the game -- the whole point of
// migration 069 is that the app never has to know the game's real values.
const maxEventLevelCeiling = 999

// typeLevels is one event type's level configuration, read from the type row.
//
// It replaced two settings-column lookups behind a string switch that defaulted
// to MG's columns for anything that was not "ZS". That default was the hazard:
// a new system type inherited Marshal's Guard's baseline and ceiling with nothing
// configured for it and no sign that anything was wrong. Per-type columns make an
// unconfigured system type an ERROR instead, because there is no longer another
// type's numbers to silently borrow.
type typeLevels struct {
	HasLevel bool
	Baseline *int
	Max      *int
}

// loadTypeLevels reads a type's level configuration. q is db or a tx.
//
// A system type with has_level but a NULL baseline or ceiling is a DATA ERROR,
// reported as such by the callers rather than papered over with a default: the
// value would be one nobody chose, written onto a schedule the whole alliance
// reads. Migration 073 fills both for every system type that has the flag, so
// reaching this is a bug or hand-edited data, and the loud failure is how it gets
// found.
func loadTypeLevels(q rowQuerier, typeID int) (typeLevels, error) {
	var tl typeLevels
	var has int
	err := q.QueryRow(`SELECT has_level, baseline_level, max_level FROM schedule_event_types WHERE id = ?`, typeID).
		Scan(&has, &tl.Baseline, &tl.Max)
	tl.HasLevel = has == 1
	return tl, err
}

// validateEventLevel checks a submitted level against the type's own ceiling,
// returning a user-facing message when it is out of range.
//
// Shared by createScheduleEvent and updateScheduleEvent so the two cannot drift.
// Out-of-range is REJECTED rather than clamped: a schedule entry is read by the
// whole alliance, so silently storing a level the officer did not choose is worse
// than an error. Callers decide WHEN to call this -- the update path deliberately
// skips it for an unchanged level (see its call site).
//
// A custom type carrying a level has no ceiling (nothing in the app knows what
// that scale runs to), so only the floor of 1 applies.
func validateEventLevel(typeName string, tl typeLevels, level *int) string {
	if level == nil {
		return ""
	}
	if *level < 1 {
		return fmt.Sprintf("%s level must be at least 1", typeName)
	}
	if tl.Max != nil && *level > *tl.Max {
		return fmt.Sprintf("%s level must be between 1 and %d", typeName, *tl.Max)
	}
	return ""
}

// pushTypeRules is what the Season Hub push needs to know about one event type to
// apply the same rules a manual create applies. It exists so the push resolves a
// type once per distinct id rather than once per created row.
type pushTypeRules struct {
	Rules  scheduleTypeRules
	Levels typeLevels
}

// resolveEventLevel applies the level rules for a NEW event, substituting the
// baseline where one is due, and returns a message plus the status to send.
// It is the create path's half of the rules; the update path hand-rolls its own
// because it deliberately grandfathers a level the officer did not touch.
//
// Three cases, and the first is the one #85 was filed about:
//
//   - has_level = 0: a level is REFUSED. Until now nothing checked, so a level
//     could be carried onto a custom type simply by switching the modal's type
//     dropdown — the field hid but kept its value, and the request was accepted.
//   - has_level = 1, custom: the level is optional and there is no ceiling; a
//     blank stays blank. Nothing in the app knows what a custom scale runs to,
//     and inventing a baseline for it would be a number nobody chose.
//   - has_level = 1, system: an omitted level takes the type's baseline, and the
//     result is validated against the type's ceiling — after the substitution, so
//     a baseline left above a lowered ceiling is caught too.
func resolveEventLevel(level **int, typeName string, isSystem bool, tl typeLevels) (string, int) {
	if !tl.HasLevel {
		if *level != nil {
			return fmt.Sprintf("%s events do not carry a level", typeName), http.StatusBadRequest
		}
		return "", 0
	}
	if isSystem {
		if tl.Baseline == nil || tl.Max == nil {
			return fmt.Sprintf("system event type %q has has_level set but a NULL baseline_level or max_level", typeName),
				http.StatusInternalServerError
		}
		if *level == nil {
			baseline := *tl.Baseline
			*level = &baseline
		}
	}
	if msg := validateEventLevel(typeName, tl, *level); msg != "" {
		return msg, http.StatusBadRequest
	}
	return "", 0
}

// --- Event Type Handlers ---

func getScheduleEventTypes(w http.ResponseWriter, r *http.Request) {
	// last_level is the level of the most recent event of this type that carried
	// one, keyed on event_date rather than created_at: the modal offers it as the
	// placeholder for a custom type, and what an officer means by "the last one" is
	// the last one on the calendar, not the last one they happened to type in.
	//
	// It cannot be derived client-side from the loaded week — the last levelled
	// event of a type is routinely outside it — which is why it rides on the types
	// payload instead of needing an endpoint of its own.
	rows, err := db.Query(`
		SELECT t.id, t.name, t.short_name, t.icon, t.is_system, t.active, t.sort_order, t.created_at,
		       t.has_level, t.baseline_level, t.max_level, t.server_event_id, t.announce,
		       (SELECT e.level FROM schedule_events e
		         WHERE e.event_type_id = t.id AND e.level IS NOT NULL
		         ORDER BY e.event_date DESC LIMIT 1)
		FROM schedule_event_types t ORDER BY t.sort_order, t.id`)
	if err != nil {
		slog.Error("getScheduleEventTypes query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	types := []ScheduleEventType{}
	for rows.Next() {
		var t ScheduleEventType
		var isSystem, active, hasLevel, announce int
		if err := rows.Scan(&t.ID, &t.Name, &t.ShortName, &t.Icon, &isSystem, &active, &t.SortOrder, &t.CreatedAt,
			&hasLevel, &t.BaselineLevel, &t.MaxLevel, &t.ServerEventID, &announce, &t.LastLevel); err != nil {
			slog.Error("getScheduleEventTypes scan", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		t.IsSystem = isSystem == 1
		t.Active = active == 1
		t.HasLevel = hasLevel == 1
		t.Announce = announce == 1
		types = append(types, t)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types)
}

// getScheduleEventTypeCeilings feeds Settings -> Game Limits, and is gated
// manage_settings rather than view_schedule.
//
// It exists BECAUSE of that gate. The ceiling is the one level field that keeps
// the manage_settings permission (see updateScheduleEventTypeCeiling), and
// requirePermission takes a single key — so a user holding manage_settings
// without view_schedule reading the general types endpoint would get a 403 and an
// empty Game Limits section with nothing explaining it. The two permissions are
// independent by design: manage_settings defaults to no rank at all.
func getScheduleEventTypeCeilings(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT id, name, baseline_level, max_level
		FROM schedule_event_types
		WHERE has_level = 1 AND is_system = 1
		ORDER BY sort_order, id`)
	if err != nil {
		slog.Error("getScheduleEventTypeCeilings query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []ScheduleEventTypeCeiling{}
	for rows.Next() {
		var c ScheduleEventTypeCeiling
		if err := rows.Scan(&c.ID, &c.Name, &c.BaselineLevel, &c.MaxLevel); err != nil {
			slog.Error("getScheduleEventTypeCeilings scan", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		out = append(out, c)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// updateScheduleEventTypeCeiling is the ONLY writer of max_level, and it is gated
// manage_settings while every other field on the type row is manage_schedule.
//
// That split is deliberate and predates this change: the ceiling used to live in
// Settings -> Game Limits (manage_settings, granted to no rank by default) and the
// baseline in Schedule -> Settings (manage_schedule, R4+R5). Moving both onto the
// same row must not silently widen who can raise a ceiling, so the ceiling keeps
// its own endpoint and its own permission.
func updateScheduleEventTypeCeiling(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var req struct {
		MaxLevel *int `json:"max_level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.MaxLevel == nil {
		http.Error(w, "max_level is required", http.StatusBadRequest)
		return
	}

	var name string
	var hasLevel, isSystem int
	var baseline, oldMax *int
	err := db.QueryRow(`SELECT name, has_level, is_system, baseline_level, max_level FROM schedule_event_types WHERE id = ?`, id).
		Scan(&name, &hasLevel, &isSystem, &baseline, &oldMax)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("updateScheduleEventTypeCeiling fetch", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	// Only levelled SYSTEM types have a ceiling. A custom type's level is whatever
	// the officer types — the app has no idea what that scale runs to, and a bound
	// it invented would be a rule it does not know.
	if hasLevel != 1 {
		http.Error(w, name+" does not carry a level, so it has no ceiling", http.StatusBadRequest)
		return
	}
	if isSystem != 1 {
		http.Error(w, name+" is a custom event type; only system types have a level ceiling", http.StatusBadRequest)
		return
	}
	if *req.MaxLevel < 1 || *req.MaxLevel > maxEventLevelCeiling {
		http.Error(w, fmt.Sprintf("Maximum %s level must be between 1 and %d", name, maxEventLevelCeiling),
			http.StatusBadRequest)
		return
	}
	// The baseline is edited on a different page under a different permission, so
	// nothing in the UI stops one being walked past the other. Name the baseline so
	// the operator knows which number to move first.
	if baseline != nil && *req.MaxLevel < *baseline {
		http.Error(w, fmt.Sprintf("Maximum %s level cannot be below its baseline of %d", name, *baseline),
			http.StatusBadRequest)
		return
	}

	if _, err := db.Exec(`UPDATE schedule_event_types SET max_level = ? WHERE id = ?`, *req.MaxLevel, id); err != nil {
		slog.Error("updateScheduleEventTypeCeiling update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	detail := fmt.Sprintf("ceiling: %s → %d", nullableIntLabel(oldMax), *req.MaxLevel)
	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "updated", "schedule_event_type", name, false, detail)

	w.WriteHeader(http.StatusNoContent)
}

// nullableIntLabel renders an optional int for an activity-log diff.
func nullableIntLabel(v *int) string {
	if v == nil {
		return "unset"
	}
	return strconv.Itoa(*v)
}

// EVERY field the event-type modal carries must be accepted by BOTH the POST and
// the PUT. The modal creates new rows through this handler, so a field it sends
// that only the update path decodes is silently lost until the next edit — which
// looks exactly like the checkbox not working.
func createScheduleEventType(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		ShortName string `json:"short_name"`
		Icon      string `json:"icon"`
		SortOrder int    `json:"sort_order"`
		HasLevel  *bool  `json:"has_level"`
		// Every field the modal carries is accepted here too — see the note above.
		ServerEventID *int  `json:"server_event_id"`
		Announce      *bool `json:"announce"`
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
	if req.ServerEventID != nil {
		var exists int
		db.QueryRow(`SELECT COUNT(*) FROM server_events WHERE id = ?`, *req.ServerEventID).Scan(&exists)
		if exists == 0 {
			http.Error(w, "server_event_id not found", http.StatusBadRequest)
			return
		}
	}
	if req.Icon == "" {
		req.Icon = "📅"
	}
	// A type created here is always custom, so it never gets baseline/ceiling
	// numbers: those belong to system types, whose levels the app generates. A
	// custom type's level is whatever the officer types, floor 1, no ceiling.
	hasLevel := 0
	if req.HasLevel != nil && *req.HasLevel {
		hasLevel = 1
	}

	// Absent means ON. A new type joins the announcement unless someone says
	// otherwise — see the column's note in migration 077.
	announce := 1
	if req.Announce != nil && !*req.Announce {
		announce = 0
	}

	res, err := db.Exec(`
		INSERT INTO schedule_event_types (name, short_name, icon, is_system, active, sort_order, has_level, server_event_id, announce)
		VALUES (?, ?, ?, 0, 1, ?, ?, ?, ?)`,
		req.Name, req.ShortName, req.Icon, req.SortOrder, hasLevel, req.ServerEventID, announce)
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

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "created", "schedule_event_type", req.Name, false)

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
		HasLevel  *bool  `json:"has_level"`
		// max_level is deliberately NOT here: it is manage_settings, and this
		// endpoint is manage_schedule. See updateScheduleEventTypeCeiling.
		BaselineLevel *int `json:"baseline_level"`
		// ServerEventID is the window this type's events happen inside. Applies to
		// ANY type, system or custom — Glacieradon is a custom encounter. A JSON
		// null clears it; an absent key leaves it alone, which is why it is a
		// **int.
		ServerEventID **int `json:"server_event_id"`
		// Announce is a *bool for the same reason has_level is: an absent key must
		// leave the stored value alone, and `false` is a real value.
		Announce *bool `json:"announce"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var old ScheduleEventType
	var isSystem, active, oldHasLevel, oldAnnounce int
	err := db.QueryRow(`SELECT name, short_name, icon, is_system, active, sort_order,
		has_level, baseline_level, max_level, server_event_id, announce FROM schedule_event_types WHERE id=?`, id).
		Scan(&old.Name, &old.ShortName, &old.Icon, &isSystem, &active, &old.SortOrder,
			&oldHasLevel, &old.BaselineLevel, &old.MaxLevel, &old.ServerEventID, &oldAnnounce)
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
	old.HasLevel = oldHasLevel == 1

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

	// has_level is fixed ON for a system type: the app generates its events and
	// needs a level for each one. Only a custom type's flag is editable.
	newHasLevel := old.HasLevel
	if req.HasLevel != nil && !old.IsSystem {
		newHasLevel = *req.HasLevel
	}
	newBaseline := old.BaselineLevel
	if req.BaselineLevel != nil {
		if !old.IsSystem {
			http.Error(w, "Only system event types have a baseline level", http.StatusBadRequest)
			return
		}
		newBaseline = req.BaselineLevel
	}

	if newHasLevel && old.IsSystem {
		if newBaseline == nil {
			http.Error(w, "A system event type needs a baseline level", http.StatusBadRequest)
			return
		}
		if *newBaseline < 1 {
			http.Error(w, "Baseline level must be at least 1", http.StatusBadRequest)
			return
		}
		// The ceiling is raised on a different page under a different permission, so
		// name it rather than leaving the officer to guess which number is in the way.
		if old.MaxLevel != nil && *newBaseline > *old.MaxLevel {
			http.Error(w, fmt.Sprintf("%s baseline level must be between 1 and %d (its ceiling, set in Settings → Game Limits)",
				old.Name, *old.MaxLevel), http.StatusBadRequest)
			return
		}
	}

	// Turning the flag OFF on a type whose events already carry levels would strand
	// those values: they stay in the column, nothing renders them, and nothing can
	// edit them. Say how many rather than silently doing it.
	if old.HasLevel && !newHasLevel {
		var levelled int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schedule_events WHERE event_type_id = ? AND level IS NOT NULL`, id).
			Scan(&levelled); err != nil {
			slog.Error("updateScheduleEventType count levelled", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if levelled > 0 {
			http.Error(w, fmt.Sprintf("%d %s event(s) already carry a level — clear those levels first",
				levelled, old.Name), http.StatusConflict)
			return
		}
	}

	newParent := old.ServerEventID
	if req.ServerEventID != nil {
		newParent = *req.ServerEventID
		if newParent != nil {
			var exists int
			db.QueryRow(`SELECT COUNT(*) FROM server_events WHERE id = ?`, *newParent).Scan(&exists)
			if exists == 0 {
				http.Error(w, "server_event_id not found", http.StatusBadRequest)
				return
			}
		}
	}

	hasLevelInt := 0
	if newHasLevel {
		hasLevelInt = 1
	}
	newAnnounce := oldAnnounce
	if req.Announce != nil {
		newAnnounce = 0
		if *req.Announce {
			newAnnounce = 1
		}
	}

	_, err = db.Exec(`
		UPDATE schedule_event_types SET name=?, short_name=?, icon=?, active=?, sort_order=?,
		       has_level=?, baseline_level=?, server_event_id=?, announce=? WHERE id=?`,
		req.Name, req.ShortName, req.Icon, activeInt, req.SortOrder, hasLevelInt, newBaseline, newParent, newAnnounce, id)
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
	if old.HasLevel != newHasLevel {
		changes = append(changes, fmt.Sprintf("carries a level: %v → %v", old.HasLevel, newHasLevel))
	}
	if nullableIntLabel(old.BaselineLevel) != nullableIntLabel(newBaseline) {
		changes = append(changes, "baseline: "+nullableIntLabel(old.BaselineLevel)+" → "+nullableIntLabel(newBaseline))
	}
	if nullableIntLabel(old.ServerEventID) != nullableIntLabel(newParent) {
		changes = append(changes, "server event window: "+nullableIntLabel(old.ServerEventID)+" → "+nullableIntLabel(newParent))
	}
	if oldAnnounce != newAnnounce {
		changes = append(changes, "announce: "+yesNo(oldAnnounce == 1)+" → "+yesNo(newAnnounce == 1))
	}

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "updated", "schedule_event_type", req.Name, false, strings.Join(changes, "; "))

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

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "deleted", "schedule_event_type", name, false)

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
		       t.name, t.short_name, t.icon, t.is_system, t.server_event_id,
		       se.event_time, se.all_day, se.level, COALESCE(se.notes,''),
		       se.created_by, se.created_at, se.updated_at, se.task_force,
		       pt.event_type_id IS NOT NULL, b.id IS NOT NULL,
		       CASE WHEN b.id IS NULL THEN 0 ELSE
		         (SELECT COUNT(*) FROM participation_entries e WHERE e.board_id = b.id) END
		FROM schedule_events se
		JOIN schedule_event_types t ON t.id = se.event_type_id
		LEFT JOIN participation_types pt ON pt.event_type_id = se.event_type_id
		LEFT JOIN participation_boards b ON b.schedule_event_id = se.id
		WHERE se.event_date >= ? AND se.event_date <= ?
		ORDER BY se.event_date, se.all_day DESC, se.event_time, se.task_force`, from, to)
	if err != nil {
		slog.Error("getScheduleEvents query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	events := []ScheduleEvent{}
	parentIDs := map[int][]int{} // server_event_id -> indices into events
	for rows.Next() {
		var ev ScheduleEvent
		var isSystem, allDay int
		var parentID *int
		if err := rows.Scan(
			&ev.ID, &ev.EventDate, &ev.EventTypeID,
			&ev.TypeName, &ev.TypeShort, &ev.TypeIcon, &isSystem, &parentID,
			&ev.EventTime, &allDay, &ev.Level, &ev.Notes,
			&ev.CreatedBy, &ev.CreatedAt, &ev.UpdatedAt, &ev.TaskForce,
			&ev.TracksParticipation, &ev.HasBoard, &ev.BoardRows,
		); err != nil {
			rows.Close()
			slog.Error("getScheduleEvents scan", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		ev.IsSystem = isSystem == 1
		ev.AllDay = allDay == 1
		events = append(events, ev)
		if parentID != nil {
			parentIDs[*parentID] = append(parentIDs[*parentID], len(events)-1)
		}
	}
	// Close the cursor BEFORE loading the parent windows: one connection, and a
	// query issued while this is open waits forever.
	rows.Close()
	if err := rows.Err(); err != nil {
		slog.Error("getScheduleEvents rows", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Flag encounters sitting outside their parent's windows, so the calendar can
	// say so on the card. Bounded work: at most 42 days of events and a handful of
	// encounter types, one window read each.
	for parentID, idxs := range parentIDs {
		parent, err := loadServerEvent(db, parentID)
		if err != nil {
			continue
		}
		for _, i := range idxs {
			events[i].OutsideWindow = serverEventOutsideWindow(parent, events[i].EventDate)
			events[i].ParentName = parent.Name
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

// scheduleEventCreate is the body of a manual create, from either the Schedule
// page or the participation recording screen's "create the occurrence" path.
type scheduleEventCreate struct {
	EventDate   string  `json:"event_date"`
	EventTypeID int     `json:"event_type_id"`
	EventTime   string  `json:"event_time"`
	AllDay      bool    `json:"all_day"`
	Level       *int    `json:"level"`
	Notes       string  `json:"notes"`
	TaskForce   *string `json:"task_force"`
}

// normTaskForce treats an empty task force as none, so a form's blank select and
// an omitted field mean the same thing.
func normTaskForce(tf *string) *string {
	if tf == nil || *tf == "" {
		return nil
	}
	return tf
}

// insertScheduleEvent validates and inserts one manually created event. It is
// shared by createScheduleEvent and POST /api/participation/occurrences so that
// an occurrence created from the recording screen passes exactly the rules — and
// gets exactly the messages — a create on the Schedule page does (#13 Q7).
//
// userMsg/code carry a rejection meant for the officer; err is an internal failure
// already logged, for which the caller answers 500.
func insertScheduleEvent(actor *AuthUser, req scheduleEventCreate) (id int64, userMsg string, code int, err error) {
	if _, perr := time.Parse("2006-01-02", req.EventDate); perr != nil {
		return 0, "event_date must be YYYY-MM-DD", http.StatusBadRequest, nil
	}
	if req.AllDay {
		req.EventTime = "00:00"
	} else if !validHHMM(req.EventTime) {
		return 0, "event_time must be a real time in HH:MM", http.StatusBadRequest, nil
	}
	if req.EventTypeID == 0 {
		return 0, "event_type_id is required", http.StatusBadRequest, nil
	}

	tr, err := loadScheduleTypeRules(db, req.EventTypeID)
	if err == sql.ErrNoRows {
		return 0, "event_type_id not found", http.StatusBadRequest, nil
	}
	if err != nil {
		slog.Error("insertScheduleEvent lookup type", "error", err)
		return 0, "", 0, err
	}
	typeName := tr.Name
	req.TaskForce = normTaskForce(req.TaskForce)

	// No is_system gate: the validator decides what applies. A custom type with a
	// parent window has a rule too, and Desert Storm's task-force rule is there.
	cand := eventCandidate{Date: req.EventDate, Time: req.EventTime, TaskForce: req.TaskForce}
	if msg, err := validateEventRules(db, tr, cand, 0); err != nil {
		slog.Error("insertScheduleEvent validateEventRules", "error", err)
		return 0, "", 0, err
	} else if msg != "" {
		return 0, msg, http.StatusBadRequest, nil
	}

	tl, err := loadTypeLevels(db, req.EventTypeID)
	if err != nil {
		slog.Error("insertScheduleEvent loadTypeLevels", "error", err, "type_id", req.EventTypeID)
		return 0, "", 0, err
	}
	if msg, code := resolveEventLevel(&req.Level, typeName, tr.IsSystem, tl); msg != "" {
		if code == http.StatusInternalServerError {
			slog.Error("insertScheduleEvent level config", "type", typeName, "type_id", req.EventTypeID, "detail", msg)
			return 0, "", 0, errors.New(msg)
		}
		return 0, msg, code, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	allDayInt := 0
	if req.AllDay {
		allDayInt = 1
	}
	res, err := db.Exec(`
		INSERT INTO schedule_events (event_date, event_type_id, event_time, all_day, level, notes, created_by, created_at, updated_at, task_force)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.EventDate, req.EventTypeID, req.EventTime, allDayInt, req.Level, req.Notes, actor.ID, now, now, req.TaskForce)
	if isUniqueViolation(err) && req.TaskForce != nil {
		// Lost a race with an identical create; the index is the backstop.
		return 0, "Desert Storm Task Force " + *req.TaskForce + " is already scheduled on " + req.EventDate, http.StatusBadRequest, nil
	}
	if err != nil {
		slog.Error("insertScheduleEvent insert", "error", err)
		return 0, "", 0, err
	}
	id, _ = res.LastInsertId()

	name := typeName + " " + req.EventDate
	if req.TaskForce != nil {
		name = typeName + " TF " + *req.TaskForce + " " + req.EventDate
	}
	logActivity(actor.ID, actor.Username, "created", "schedule_event", name, false)
	return id, "", 0, nil
}

func createScheduleEvent(w http.ResponseWriter, r *http.Request) {
	var req scheduleEventCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	id, msg, code, err := insertScheduleEvent(getAuthUser(r), req)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if msg != "" {
		http.Error(w, msg, code)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func updateScheduleEvent(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var req struct {
		EventDate   string  `json:"event_date"`
		EventTypeID int     `json:"event_type_id"`
		EventTime   string  `json:"event_time"`
		AllDay      bool    `json:"all_day"`
		Level       *int    `json:"level"`
		Notes       string  `json:"notes"`
		TaskForce   *string `json:"task_force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req.TaskForce = normTaskForce(req.TaskForce)

	var old ScheduleEvent
	var isSystem, oldAllDay int
	err := db.QueryRow(`
		SELECT se.event_date, se.event_type_id, t.short_name, t.is_system,
		       se.event_time, se.all_day, se.level, COALESCE(se.notes,''), se.task_force
		FROM schedule_events se
		JOIN schedule_event_types t ON t.id = se.event_type_id
		WHERE se.id=?`, id).
		Scan(&old.EventDate, &old.EventTypeID, &old.TypeShort, &isSystem,
			&old.EventTime, &oldAllDay, &old.Level, &old.Notes, &old.TaskForce)
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
	} else if !validHHMM(req.EventTime) {
		http.Error(w, "event_time must be a real time in HH:MM", http.StatusBadRequest)
		return
	}
	if req.EventTypeID == 0 {
		req.EventTypeID = old.EventTypeID
	}

	tr, err := loadScheduleTypeRules(db, req.EventTypeID)
	if err != nil {
		http.Error(w, "event_type_id not found", http.StatusBadRequest)
		return
	}

	// A Desert Storm row keeps its task force unless the request names another; a
	// row of any other type has none. Only an untouched legacy row may stay blank.
	keepMissingTF := false
	if tr.Short == "DS" {
		if req.TaskForce == nil {
			req.TaskForce = old.TaskForce
			keepMissingTF = old.TaskForce == nil && req.EventTypeID == old.EventTypeID
		}
	} else if req.TaskForce != nil && req.EventTypeID == old.EventTypeID {
		// Stale client field on a non-DS row: the validator names the rule.
	} else {
		req.TaskForce = nil
	}
	tfChanged := (old.TaskForce == nil) != (req.TaskForce == nil) ||
		(old.TaskForce != nil && req.TaskForce != nil && *old.TaskForce != *req.TaskForce)

	// A board's values are keyed to its type's trackables and its roles to the task
	// force, so an event with a recorded board keeps both. Date, time, level and
	// notes stay editable: a corrected date is the same occurrence, and the board
	// should follow it.
	if req.EventTypeID != old.EventTypeID || tfChanged {
		var boards int
		if err := db.QueryRow(`SELECT COUNT(*) FROM participation_boards WHERE schedule_event_id = ?`, id).Scan(&boards); err != nil {
			slog.Error("updateScheduleEvent board check", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if boards > 0 {
			http.Error(w, "This event has a recorded participation board, so its type and task force cannot change — delete the board first", http.StatusConflict)
			return
		}
	}
	typeName, newIsSystem := tr.Name, 0
	if tr.IsSystem {
		newIsSystem = 1
	}

	// No is_system gate — see validateEventRules.
	cand := eventCandidate{Date: req.EventDate, Time: req.EventTime, TaskForce: req.TaskForce, KeepMissingTaskForce: keepMissingTF}
	if msg, err := validateEventRules(db, tr, cand, id); err != nil {
		slog.Error("updateScheduleEvent validateEventRules", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	} else if msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	tl, err := loadTypeLevels(db, req.EventTypeID)
	if err != nil {
		slog.Error("updateScheduleEvent loadTypeLevels", "error", err, "type_id", req.EventTypeID)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	clientSentLevel := req.Level != nil
	if !tl.HasLevel {
		// The client must not ask for one...
		if clientSentLevel {
			http.Error(w, typeName+" events do not carry a level", http.StatusBadRequest)
			return
		}
		// ...and a level carried over from the PREVIOUS type is dropped rather than
		// kept. Retyping an MG event to a custom one is a request to make it that
		// type, and the old type's level is not a property of the new one. This is
		// the update-path half of the leak #85 describes: the modal hides the field
		// on a type switch but does not clear it, so the old value used to ride
		// along.
		req.Level = nil
	} else {
		if req.Level == nil {
			req.Level = old.Level
		}
		if newIsSystem == 1 {
			if tl.Baseline == nil || tl.Max == nil {
				slog.Error("updateScheduleEvent level config", "type", typeName, "type_id", req.EventTypeID,
					"detail", "has_level set with a NULL baseline_level or max_level")
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			if req.Level == nil {
				baseline := *tl.Baseline
				req.Level = &baseline
			}
		}
		// Validate only a level the officer actually CHANGED. An event stored above a
		// ceiling the operator has since lowered must stay editable: the level input is
		// inside event-form, so rejecting an untouched legacy value would block edits to
		// that event's notes or time as well, for a value nobody chose in this request.
		// Typing a new out-of-range level is still rejected.
		//
		// A TYPE change counts as a change even when the number is identical: the
		// grandfathering exists to protect a value against its own type's moved
		// ceiling, and now that ceilings are per type, carrying 70 from a Large
		// Sandworm onto a Zombie Siege capped at 12 is a new value for that type,
		// not a legacy one.
		typeChanged := req.EventTypeID != old.EventTypeID
		levelChanged := req.Level != nil && (typeChanged || old.Level == nil || *req.Level != *old.Level)
		if levelChanged {
			if msg := validateEventLevel(typeName, tl, req.Level); msg != "" {
				http.Error(w, msg, http.StatusBadRequest)
				return
			}
		}
	}

	newAllDayInt := 0
	if req.AllDay {
		newAllDayInt = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err = db.Exec(`
		UPDATE schedule_events SET event_date=?, event_type_id=?, event_time=?, all_day=?, level=?, notes=?, task_force=?, updated_at=? WHERE id=?`,
		req.EventDate, req.EventTypeID, req.EventTime, newAllDayInt, req.Level, req.Notes, req.TaskForce, now, id); isUniqueViolation(err) && req.TaskForce != nil {
		http.Error(w, "Desert Storm Task Force "+*req.TaskForce+" is already scheduled on "+req.EventDate, http.StatusBadRequest)
		return
	} else if err != nil {
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
	if tfChanged {
		from, to := "none", "none"
		if old.TaskForce != nil {
			from = *old.TaskForce
		}
		if req.TaskForce != nil {
			to = *req.TaskForce
		}
		changes = append(changes, "task force: "+from+" → "+to)
	}

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "updated", "schedule_event", typeName+" "+req.EventDate, false, strings.Join(changes, "; "))

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

	// A recorded board is history about members, so the occurrence it hangs off is
	// not deleted underneath it — the same refusal deleteServerEvent gives for a
	// window with encounters pointing at it. foreign_keys is off, so nothing else
	// would stop the board being orphaned.
	var boards int
	if err := db.QueryRow(`SELECT COUNT(*) FROM participation_boards WHERE schedule_event_id = ?`, id).Scan(&boards); err != nil {
		slog.Error("deleteScheduleEvent board check", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if boards > 0 {
		http.Error(w, "This event has a recorded participation board — delete the board first", http.StatusConflict)
		return
	}

	if _, err = db.Exec(`DELETE FROM schedule_events WHERE id=?`, id); err != nil {
		slog.Error("deleteScheduleEvent delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "deleted", "schedule_event", typeName+" "+eventDate, false)

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
	// zs_anchor_time is deliberately not read: under a date rule the ASAP chain is
	// pure date arithmetic and every insert uses zs_default_time anyway.
	err := db.QueryRow(`SELECT mg_default_time, zs_default_time,
		COALESCE(mg_anchor_date,''), zs_schedule_mode, zs_weekdays,
		COALESCE(zs_anchor_date,'') FROM settings WHERE id=1`).
		Scan(&s.MGDefaultTime, &s.ZSDefaultTime,
			&s.MGAnchorDate, &s.ZSScheduleMode, &s.ZSWeekdays, &s.ZSAnchorDate)
	if err != nil {
		slog.Error("generateScheduleEvents settings", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Baselines come from the type row now, not from settings. Read both up front:
	// the generate loop below inserts as it goes, and a read issued mid-loop would
	// be a second statement on the single connection while nothing is open — legal,
	// but pointlessly repeated once per candidate date.
	var mgTypeID, lsTypeID, zsTypeID, dsTypeID int
	db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name='MG'`).Scan(&mgTypeID)
	db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name='LS'`).Scan(&lsTypeID)
	db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name='ZS'`).Scan(&zsTypeID)
	db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name='DS'`).Scan(&dsTypeID)

	// Each type's rule inputs, loaded once before the loop. The generator runs the
	// SAME validator as every other write path (validateEventRules), so a rule added
	// there — a parent window, Desert Storm's task force — reaches generated rows
	// too. It used to call validateSystemEventRules directly, which was safe only
	// while every generated type was a system type with no parent.
	typeRules := map[int]scheduleTypeRules{}
	for _, id := range []int{mgTypeID, lsTypeID, zsTypeID, dsTypeID} {
		if id == 0 {
			continue
		}
		tr, err := loadScheduleTypeRules(db, id)
		if err != nil {
			slog.Error("generateScheduleEvents type rules", "error", err, "type_id", id)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		typeRules[id] = tr
	}

	// Desert Storm's standing config, read up front for the same reason: one battle
	// per participating task force per Friday, at its slot's time. Changing the
	// config later does NOT move occurrences already generated — the same rule as
	// moving a server-event window.
	type dsSlot struct{ tf, time string }
	var dsSlots []dsSlot
	if dsTypeID != 0 {
		rows, err := db.Query(`SELECT c.task_force, s.time_st FROM storm_tf_config c
			JOIN storm_slot_times s ON s.slot = c.time_slot
			WHERE c.participating = 1 ORDER BY c.task_force`)
		if err != nil {
			slog.Error("generateScheduleEvents storm config", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		for rows.Next() {
			var sl dsSlot
			if err := rows.Scan(&sl.tf, &sl.time); err != nil {
				rows.Close()
				slog.Error("generateScheduleEvents storm config scan", "error", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			if validHHMM(sl.time) {
				dsSlots = append(dsSlots, sl)
			}
		}
		rows.Close()
	}

	// The Alliance Exercise slot switches variant at Season 3 day 58. ok is false
	// on a server with no Season 3 row, and then the slot is simply Marshal's Guard
	// throughout — every comparison below is guarded on it, because a Go string
	// compare against "" is true for every date.
	cutover, haveCutover, err := sandwormCutover(db)
	if err != nil {
		slog.Error("generateScheduleEvents cutover", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// A system type with no baseline is a data error, the same one the create path
	// reports. Generating with a made-up number would write it onto every row of a
	// 90-day run.
	baselineOf := func(typeID int, short string) (int, bool) {
		if typeID == 0 {
			return 0, false
		}
		tl, err := loadTypeLevels(db, typeID)
		if err != nil {
			slog.Error("generateScheduleEvents loadTypeLevels", "error", err, "type", short)
			return 0, false
		}
		if !tl.HasLevel || tl.Baseline == nil {
			slog.Error("generateScheduleEvents missing baseline", "type", short, "type_id", typeID)
			return 0, false
		}
		return *tl.Baseline, true
	}
	mgBaseline, mgOK := baselineOf(mgTypeID, "MG")
	lsBaseline, lsOK := baselineOf(lsTypeID, "LS")
	zsBaseline, zsOK := baselineOf(zsTypeID, "ZS")

	user := getAuthUser(r)
	now := time.Now().UTC().Format(time.RFC3339)

	genTypes := map[string]bool{}
	for _, t := range req.Types {
		genTypes[strings.ToLower(t)] = true
	}

	mgCreated := 0
	lsCreated := 0
	zsCreated := 0
	dsCreated := 0
	skippedError := 0
	skippedExisting := 0
	skippedInvalid := 0
	invalid := []invalidGeneratedEvent{}
	switched := 0
	switchedDetail := []switchedGeneratedEvent{}

	// tryCreate applies the same rules a manual create applies, then inserts.
	//
	// Validating and inserting ONE ROW AT A TIME, in date order, is what makes a
	// generated run check against itself: every accepted row is already in the
	// database when the next candidate is examined, so no separate list of
	// pending rows has to be kept in step. Do not turn this into plan-then-bulk-
	// insert — the batch would only be checked against what existed before it.
	tryCreate := func(typeID int, short, dateStr, tm string, level *int, tf *string) bool {
		// task_force IS ?, not = ?: every MG/LS/ZS candidate binds NULL there, and
		// = NULL is never true — the check would find nothing and re-insert.
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schedule_events WHERE event_type_id=? AND event_date=? AND task_force IS ?`,
			typeID, dateStr, tf).Scan(&exists); err != nil {
			// Unknown is not "absent": count it and move on rather than insert blind.
			slog.Error("generateScheduleEvents exists check", "error", err, "date", dateStr, "type", short)
			skippedError++
			return false
		}
		if exists > 0 {
			skippedExisting++
			return false
		}
		msg, err := validateEventRules(db, typeRules[typeID], eventCandidate{Date: dateStr, Time: tm, TaskForce: tf}, 0)
		if err != nil {
			slog.Error("generateScheduleEvents validate", "error", err, "date", dateStr, "type", short)
			skippedError++
			return false
		}
		if msg != "" {
			skippedInvalid++
			// Capped: the officer needs to see WHICH dates were declined and why,
			// not every one of them in a 90-day run.
			if len(invalid) < 20 {
				invalid = append(invalid, invalidGeneratedEvent{Date: dateStr, Type: short, Reason: msg})
			}
			return false
		}
		if _, err := db.Exec(`
			INSERT INTO schedule_events (event_date, event_type_id, event_time, level, notes, created_by, created_at, updated_at, task_force)
			VALUES (?, ?, ?, ?, '', ?, ?, ?, ?)`,
			dateStr, typeID, tm, level, user.ID, now, now, tf); err != nil {
			if isUniqueViolation(err) {
				skippedExisting++ // lost a race; the index did its job
				return false
			}
			slog.Error("generateScheduleEvents insert", "error", err, "date", dateStr, "type", short)
			skippedError++
			return false
		}
		return true
	}

	// --- Alliance Exercise generation: every other day from anchor ---
	//
	// One loop, two variants. Past the cutover the slot is Large Sandworm, and the
	// generator produces that rather than an MG the manual validator would now
	// refuse. The request value stays "mg": it is the FAMILY selector, and the wire
	// value is not worth breaking for a label change.
	if genTypes["mg"] && s.MGAnchorDate != "" {
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

				typeID, short, baseline, ok := mgTypeID, "MG", mgBaseline, mgOK
				isSandworm := haveCutover && dateStr >= cutover
				if isSandworm {
					typeID, short, baseline, ok = lsTypeID, "LS", lsBaseline, lsOK
				}
				if !ok {
					continue
				}
				if tryCreate(typeID, short, dateStr, s.MGDefaultTime, &baseline, nil) {
					if isSandworm {
						lsCreated++
						// Report the substitution rather than silently producing a
						// different event than the checkbox named: an officer who
						// ticked "Alliance Exercise" and got Large Sandworms needs
						// to see the rule that decided it.
						switched++
						if len(switchedDetail) < 20 {
							switchedDetail = append(switchedDetail, switchedGeneratedEvent{
								Date: dateStr, From: "MG", To: "LS",
								Reason: fmt.Sprintf("Alliance Exercise switched to Large Sandworm from %s (Season 3 day %d)",
									cutover, sandwormCutoverDays+1),
							})
						}
					} else {
						mgCreated++
					}
				}
			}
		}
	}

	// --- ZS generation ---
	if genTypes["zs"] && zsOK {
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
				if tryCreate(zsTypeID, "ZS", d.Format("2006-01-02"), s.ZSDefaultTime, &zsBaseline, nil) {
					zsCreated++
				}
			}

		case "asap":
			if s.ZSAnchorDate != "" {
				anchorT, err := time.Parse("2006-01-02", s.ZSAnchorDate)
				if err == nil {
					// Pure date stepping. The old chain advanced by 71.5 hours and
					// so drifted through the day, eventually proposing links two
					// days apart while every row was written at zs_default_time
					// regardless. Stepping whole days cannot drift, and a chain
					// spaced zsGapDays apart can never violate its own rule.
					cur := anchorT
					if fromT.After(anchorT) {
						steps := int(fromT.Sub(anchorT).Hours()/24) / zsGapDays
						cur = anchorT.AddDate(0, 0, steps*zsGapDays)
						for cur.Before(fromT) {
							cur = cur.AddDate(0, 0, zsGapDays)
						}
					}
					for ; !cur.After(toT); cur = cur.AddDate(0, 0, zsGapDays) {
						if tryCreate(zsTypeID, "ZS", cur.Format("2006-01-02"), s.ZSDefaultTime, &zsBaseline, nil) {
							zsCreated++
						}
					}
				}
			}
		}
	}

	// --- Desert Storm: one battle per participating task force, every Friday ---
	if genTypes["ds"] && dsTypeID != 0 {
		for d := fromT; !d.After(toT); d = d.AddDate(0, 0, 1) {
			if d.Weekday() != time.Friday {
				continue
			}
			for _, sl := range dsSlots {
				tf := sl.tf
				if tryCreate(dsTypeID, "DS", d.Format("2006-01-02"), sl.time, nil, &tf) {
					dsCreated++
				}
			}
		}
	}

	total := mgCreated + lsCreated + zsCreated + dsCreated
	if total > 0 {
		logActivity(user.ID, user.Username, "created", "schedule_event",
			fmt.Sprintf("%d events generated", total), false,
			fmt.Sprintf("MG: %d, LS: %d, ZS: %d, DS: %d", mgCreated, lsCreated, zsCreated, dsCreated))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"mg_created":       mgCreated,
		"ls_created":       lsCreated,
		"zs_created":       zsCreated,
		"ds_created":       dsCreated,
		"skipped_existing": skippedExisting,
		"skipped_error":    skippedError,
		"skipped_invalid":  skippedInvalid,
		"invalid":          invalid,
		"switched":         switched,
		"switched_detail":  switchedDetail,
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

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "created", "server_event", req.Name, false)

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

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "updated", "server_event", req.Name, false, strings.Join(changes, "; "))

	// Moving a window can leave encounters scheduled outside it. Report them; never
	// move or delete them. The officer knows why they moved the window and what the
	// encounters are for — the app does not, and silently relocating somebody's
	// schedule is a worse failure than telling them.
	stranded, err := strandedEncounters(db, id)
	if err != nil {
		// The save succeeded; a failure to compute the warning must not report it
		// as failed.
		slog.Error("updateServerEvent stranded encounters", "error", err, "server_event_id", id)
		stranded = nil
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"stranded": stranded})
}

// strandedEncounter is one future encounter left outside its parent's windows.
type strandedEncounter struct {
	ID   int    `json:"id"`
	Date string `json:"date"`
	Type string `json:"type"`
}

// strandedEncounters lists TODAY-or-later events of types parented to this window
// that no longer fall inside one of its occurrences.
//
// Deliberately future-only: a past event records what happened, and the window
// having since moved says nothing about it.
func strandedEncounters(q rowQueryer, serverEventID int) ([]strandedEncounter, error) {
	parent, err := loadServerEvent(q, serverEventID)
	if err != nil {
		return nil, err
	}

	rows, err := q.Query(`
		SELECT e.id, e.event_date, t.name
		FROM schedule_events e
		JOIN schedule_event_types t ON t.id = e.event_type_id
		WHERE t.server_event_id = ? AND e.event_date >= ?
		ORDER BY e.event_date`, serverEventID, gameDate())
	if err != nil {
		return nil, err
	}
	// Read everything before evaluating: the window arithmetic is pure Go, but
	// keeping the cursor open across anything else is the one-connection trap.
	var candidates []strandedEncounter
	for rows.Next() {
		var c strandedEncounter
		if err := rows.Scan(&c.ID, &c.Date, &c.Type); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Same rule as the calendar's badge: a window that cannot be computed strands
	// nothing. Otherwise clearing an anchor — or deactivating a window — would
	// report every future encounter as stranded, which is the opposite of true.
	out := []strandedEncounter{}
	for _, c := range candidates {
		if serverEventOutsideWindow(parent, c.Date) {
			out = append(out, c)
		}
	}
	return out, nil
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

	// Refuse rather than strand the link. foreign_keys is off app-wide, so deleting
	// the window would leave every encounter type pointing at a row that is gone —
	// the deleteExternalAlliance shape. The season purge paths detach instead
	// (detachEncounterParents): there the window is going whatever happens, and
	// refusing would block an operation over a link nobody mentioned.
	var encounters string
	db.QueryRow(`SELECT COALESCE(group_concat(name, ', '), '') FROM schedule_event_types WHERE server_event_id = ?`, id).
		Scan(&encounters)
	if encounters != "" {
		http.Error(w, "Cannot delete: "+encounters+" happen inside this window. Clear the link on those event types first.",
			http.StatusConflict)
		return
	}

	if _, err = db.Exec(`DELETE FROM server_events WHERE id=?`, id); err != nil {
		slog.Error("deleteServerEvent delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "deleted", "server_event", name, false)

	w.WriteHeader(http.StatusNoContent)
}
