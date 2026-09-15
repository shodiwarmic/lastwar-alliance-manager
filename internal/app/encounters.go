package app

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Server-event occurrence arithmetic, server-side.
//
// `server_events` describes a repeating WINDOW — General's Trial every 14 days for
// 3 days, and so on. The calendar has always computed those windows in the browser
// (getServerEventOccurrencesInWeek, static/schedule.js) to draw its banners. Now
// that an event type can belong to a window, the server has to compute them too:
// the browser can draw whatever it likes, but the rule about what may be SAVED has
// to be enforced where the write happens.
//
// This is deliberately a mirror rather than a shared implementation — there is no
// shared language — so serverEventCoversDate is table-tested against dates the JS
// is known to produce. If the two ever disagree, the server is right.

const dateLayout = "2006-01-02"

// execer / queryExecer are the write-side counterparts of rowQuerier
// (handlers_lastrank.go): satisfied by both *sql.DB (through *guardedDB) and
// *sql.Tx, so a helper can be shared by an HTTP handler and a transaction inside
// another one without either knowing which it has.
type execer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

type queryExecer interface {
	execer
	Query(query string, args ...interface{}) (*sql.Rows, error)
}

// rowQueryer is rowQuerier plus Query, for helpers that need both a point read and
// a cursor.
type rowQueryer interface {
	rowQuerier
	Query(query string, args ...interface{}) (*sql.Rows, error)
}

// serverEventOccurrenceStarts returns the start dates of every occurrence of ev
// that could cover any date in [from, to], in ascending order.
//
// It walks from one step before the first candidate, because an occurrence
// starting before `from` can still run into the range: a 3-day window opening the
// day before `from` covers two days of it.
func serverEventOccurrenceStarts(ev ServerEvent, from, to time.Time) []time.Time {
	if !ev.Active || ev.AnchorDate == "" {
		return nil
	}
	anchor, err := time.Parse(dateLayout, ev.AnchorDate)
	if err != nil {
		return nil
	}

	if ev.RepeatType == "none" {
		return []time.Time{anchor}
	}

	step := 0
	first := anchor
	switch ev.RepeatType {
	case "every_n_days":
		step = 14
		if ev.RepeatInterval != nil && *ev.RepeatInterval > 0 {
			step = *ev.RepeatInterval
		}
	case "weekly", "biweekly":
		step = 7
		if ev.RepeatType == "biweekly" {
			step = 14
		}
		// repeat_weekday is Mon=0…Sun=6, the app's convention everywhere.
		target := 0
		if ev.RepeatWeekday != nil {
			target = *ev.RepeatWeekday
		}
		anchorDow := (int(anchor.Weekday()) + 6) % 7
		first = anchor.AddDate(0, 0, ((target-anchorDow)%7+7)%7)
	default:
		return nil
	}
	if step <= 0 {
		return nil
	}

	// Jump straight to the neighbourhood of `from` rather than stepping from the
	// anchor: an anchor two years back would otherwise cost hundreds of iterations
	// per candidate date, and the schedule validates one date at a time.
	n := int(from.Sub(first).Hours()/24) / step
	if n < 0 {
		n = 0
	}
	if n > 0 {
		n-- // one step back, for a window that opens before `from` and runs into it
	}

	var starts []time.Time
	for i := n; ; i++ {
		occ := first.AddDate(0, 0, i*step)
		if occ.After(to) {
			break
		}
		if !occ.AddDate(0, 0, ev.durationDays()-1).Before(from) {
			starts = append(starts, occ)
		}
	}
	return starts
}

func (ev ServerEvent) durationDays() int {
	if ev.DurationDays < 1 {
		return 1
	}
	return ev.DurationDays
}

// serverEventWindowKnown reports whether ev's occurrences can be computed at all.
//
// An inactive or unanchored window has NO computable occurrences, which is a
// different state from "this date is not one of them", and the difference has to
// be respected by every reader or the app contradicts itself. The write path
// already skips the rule for such a parent (validateEncounterWindow); a reader
// that asked serverEventCoversDate directly would get `false` and report every
// encounter on a fresh install as outside its window — badging as wrong the very
// event the save had just accepted.
//
// So: unknown is not "never". Ask this first, and treat false as "no opinion".
func serverEventWindowKnown(ev ServerEvent) bool {
	return ev.Active && ev.AnchorDate != ""
}

// serverEventOutsideWindow reports whether date is outside every occurrence of ev,
// for readers that must not guess when the window is unknowable. See
// serverEventWindowKnown.
func serverEventOutsideWindow(ev ServerEvent, date string) bool {
	if !serverEventWindowKnown(ev) {
		return false
	}
	return !serverEventCoversDate(ev, date)
}

// serverEventCoversDate reports whether ev's window is open on date.
func serverEventCoversDate(ev ServerEvent, date string) bool {
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return false
	}
	for _, start := range serverEventOccurrenceStarts(ev, d, d) {
		if !start.After(d) && !start.AddDate(0, 0, ev.durationDays()-1).Before(d) {
			return true
		}
	}
	return false
}

// serverEventNearestWindow returns the occurrence closest to date, as
// "<start> to <end>", or "" when the event has no occurrences to name.
//
// The rejection has to say WHICH window it wanted, not just that the date was
// wrong: an officer looking at a calendar of their own needs the app's answer to
// compare against, the same reason the schedule's gap rules name the date they
// conflicted with.
func serverEventNearestWindow(ev ServerEvent, date string) string {
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return ""
	}
	// A generous radius: at the 14-day cadence this always finds one either side.
	starts := serverEventOccurrenceStarts(ev, d.AddDate(0, 0, -40), d.AddDate(0, 0, 40))
	best := time.Time{}
	bestDist := 1 << 30
	for _, s := range starts {
		dist := int(s.Sub(d).Hours() / 24)
		if dist < 0 {
			dist = -dist
		}
		if dist < bestDist {
			bestDist, best = dist, s
		}
	}
	if best.IsZero() {
		return ""
	}
	return best.Format(dateLayout) + " to " + best.AddDate(0, 0, ev.durationDays()-1).Format(dateLayout)
}

// detachEncounterParents clears schedule_event_types.server_event_id for every
// type pointing at one of the given windows, and returns how many it cleared.
//
// EVERY path that deletes a server_events row must call this first.
// `foreign_keys` is off app-wide, so the schema's REFERENCES clause is
// documentation: nothing else will clear the link, and a type left pointing at a
// deleted window would have a rule the app cannot evaluate.
//
// The handler delete path is different: it REFUSES (409) rather than detaching,
// because there a human is asking to remove a window an encounter still uses and
// can be told so. The purge paths run as a consequence of deleting a SEASON, where
// the window is going whatever happens and refusing would strand the whole
// operation over a link the officer never mentioned.
func detachEncounterParents(x execer, ids []int) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	// #nosec G202 — the placeholder list is built from len(ids), not from input.
	res, err := x.Exec(`UPDATE schedule_event_types SET server_event_id = NULL
		WHERE server_event_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// detachEncounterParentsByAnchor is the same thing for the Season Hub's purges,
// which identify a materialised window by (name, anchor_date) rather than by id.
func detachEncounterParentsByAnchor(x queryExecer, name, anchorDate string) (int, error) {
	rows, err := x.Query(`SELECT id FROM server_events WHERE name = ? AND anchor_date = ?`, name, anchorDate)
	if err != nil {
		return 0, err
	}
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	// Close BEFORE the UPDATE: one connection, and a write issued while this cursor
	// is open waits forever for it.
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return detachEncounterParents(x, ids)
}

// loadServerEvent reads one window. q is db or a tx.
func loadServerEvent(q rowQuerier, id int) (ServerEvent, error) {
	var ev ServerEvent
	var active int
	err := q.QueryRow(`
		SELECT id, name, short_name, icon, duration_days, repeat_type,
		       repeat_interval, repeat_weekday, COALESCE(anchor_date,''), active, sort_order
		FROM server_events WHERE id = ?`, id).
		Scan(&ev.ID, &ev.Name, &ev.ShortName, &ev.Icon, &ev.DurationDays, &ev.RepeatType,
			&ev.RepeatInterval, &ev.RepeatWeekday, &ev.AnchorDate, &active, &ev.SortOrder)
	ev.Active = active == 1
	return ev, err
}

// validateEncounterWindow rejects an encounter placed outside its parent window.
//
// Applies to ANY type carrying a parent, system or custom — Glacieradon is a
// custom type with one. That is why the schedule's rule dispatch no longer gates
// on is_system: a rule that only ran for system types would leave this one
// bypassable on every write path.
func validateEncounterWindow(q rowQuerier, typeName string, parentID int, date string) (string, error) {
	parent, err := loadServerEvent(q, parentID)
	if err == sql.ErrNoRows {
		// The link points at a window that no longer exists. Every delete path
		// detaches its references, so this should be unreachable; refusing to
		// enforce a rule we cannot evaluate is better than refusing the save.
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !parent.Active {
		return fmt.Sprintf("%s belongs to %s, which is not active — reactivate it in Server Events first",
			typeName, parent.Name), nil
	}
	// An unanchored window has no computable occurrences, and the rule is SKIPPED
	// rather than failed.
	//
	// Migration 025 seeds the five repeating windows with no anchor_date (it omits
	// the column entirely); the operator fills each one in when they first see that
	// event in the game. Migration 075 links Sky Predator to General's Trial on
	// EVERY install regardless — so enforcing here would make Sky Predator
	// unschedulable, on upgrade, for anyone who had not set that anchor. A rule
	// introduced by this change that breaks an existing workflow over unrelated
	// missing configuration is worse than no rule.
	//
	// Nor could it explain itself: every other rejection in the schedule names the
	// rule AND the date it compared against, and this one would have neither
	// window nor date to name. The browser already agrees — the calendar draws no
	// banner for an unanchored window (getServerEventOccurrencesInWeek returns an
	// empty set), so treating it as "unknown" rather than "never" keeps the two
	// consistent.
	//
	// It is not silent: the event modal says the parent has no anchor date, which
	// is where an officer is standing when it matters.
	if parent.AnchorDate == "" {
		return "", nil
	}
	if serverEventCoversDate(parent, date) {
		return "", nil
	}
	msg := fmt.Sprintf("%s must fall inside a %s window", typeName, parent.Name)
	if nearest := serverEventNearestWindow(parent, date); nearest != "" {
		msg += " — nearest window " + nearest
	}
	return msg, nil
}
