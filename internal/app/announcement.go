package app

// announcement.go — the nightly announcement.
//
// An officer used to read the day's events off the schedule grid and retype them
// into an alliance-wide post. This prints what the model already knows.
//
// The SELECTION runs here, on the server, not in the browser. It is the one piece
// of arithmetic in the feature that can misfire — a window that wraps past
// midnight has to reach into the next game day — and putting it in Go makes it a
// pure function under CI rather than something only a scratch browser fixture
// could exercise. The client renders lines; it decides nothing.

import (
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// announceWindow is the span of server time an announcement covers.
//
// Wraps is not configuration — it is derived: `end <= start` means the window runs
// past midnight, which is what an alliance posting at 18:00 for the night ahead
// actually wants.
type announceWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Wraps bool   `json:"wraps"`
}

func loadAnnounceWindow() announceWindow {
	w := announceWindow{Start: "00:00", End: "23:59"}
	db.QueryRow(`SELECT COALESCE(announce_window_start,'00:00'), COALESCE(announce_window_end,'23:59')
		FROM settings WHERE id = 1`).Scan(&w.Start, &w.End)
	w.Wraps = w.End <= w.Start
	return w
}

// announceEvent is one event as the announcement sees it.
type announceEvent struct {
	Date     string `json:"date"`
	Time     string `json:"time"`
	AllDay   bool   `json:"all_day"`
	TypeName string `json:"type_name"`
	Level    *int   `json:"level"`
	// TaskForce names a Desert Storm battle ("A"/"B"); two can share a date.
	TaskForce *string `json:"task_force,omitempty"`
	Announce  bool    `json:"-"`
}

// droppedEvent is an event the announcement left out, and why.
//
// Reported rather than silently omitted: a post that is quietly missing an event
// is indistinguishable from a correct one, and the officer has no way to tell
// which without re-reading the grid — the very work this replaces.
type droppedEvent struct {
	Date     string `json:"date"`
	Time     string `json:"time"`
	TypeName string `json:"type_name"`
	Reason   string `json:"reason"`
}

// selectAnnouncementEvents decides which events belong in the announcement for a
// day, given that day's events and the next day's.
//
// Pure, and deliberately so — every rule below is a table-test rather than
// something you have to open a browser to see.
//
// The rules:
//   - BOTH bounds are inclusive. The default end is 23:59, so an exclusive end
//     would drop an event at exactly 23:59 on every install that never touched the
//     setting.
//   - A window wraps when end <= start. Timed events on D from start onward are in,
//     plus timed events on D+1 up to and including end.
//   - All-day events are attached to the DATE, not to a time, so an all-day event
//     dated D is always in and one dated D+1 never is — whatever the window does.
//   - A type not flagged for announcement is dropped with that reason even when it
//     is in the window, so turning the flag off reads as a decision rather than as
//     a missing event.
//
// Times compare as HH:MM strings, which is safe because validHHMM guards every
// write into the columns they come from.
func selectAnnouncementEvents(w announceWindow, onD, onD1 []announceEvent) ([]announceEvent, []droppedEvent) {
	in := []announceEvent{}
	dropped := []droppedEvent{}

	windowLabel := w.Start + "–" + w.End

	consider := func(ev announceEvent, inWindow bool) {
		if !inWindow {
			// Only events dated D are reported as out-of-window. A D+1 event that
			// misses a wrapping window was never this announcement's business and
			// listing it would be noise.
			if ev.Date == "" || ev.Date == onDDate(onD) {
				dropped = append(dropped, droppedEvent{
					Date: ev.Date, Time: ev.Time, TypeName: ev.TypeName,
					Reason: "outside the " + windowLabel + " window",
				})
			}
			return
		}
		if !ev.Announce {
			dropped = append(dropped, droppedEvent{
				Date: ev.Date, Time: ev.Time, TypeName: ev.TypeName,
				Reason: "type not flagged",
			})
			return
		}
		in = append(in, ev)
	}

	for _, ev := range onD {
		if ev.AllDay {
			consider(ev, true)
			continue
		}
		if w.Wraps {
			consider(ev, ev.Time >= w.Start)
		} else {
			consider(ev, ev.Time >= w.Start && ev.Time <= w.End)
		}
	}

	if w.Wraps {
		for _, ev := range onD1 {
			if ev.AllDay {
				continue // belongs to tomorrow's announcement
			}
			if ev.Time <= w.End {
				consider(ev, true)
			}
		}
	}

	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Date != in[j].Date {
			return in[i].Date < in[j].Date
		}
		if in[i].AllDay != in[j].AllDay {
			return in[i].AllDay // all-day first, as the grid orders them
		}
		return in[i].Time < in[j].Time
	})
	sort.SliceStable(dropped, func(i, j int) bool {
		if dropped[i].Date != dropped[j].Date {
			return dropped[i].Date < dropped[j].Date
		}
		return dropped[i].Time < dropped[j].Time
	})
	return in, dropped
}

// onDDate reports the date the "day D" slice belongs to, or "" when it is empty.
func onDDate(onD []announceEvent) string {
	if len(onD) == 0 {
		return ""
	}
	return onD[0].Date
}

// loadAnnounceEvents reads one day's events with their type's announce flag.
func loadAnnounceEvents(date string) ([]announceEvent, error) {
	rows, err := db.Query(`
		SELECT se.event_date, se.event_time, se.all_day, t.name, se.level, t.announce, se.task_force
		FROM schedule_events se
		JOIN schedule_event_types t ON t.id = se.event_type_id
		WHERE se.event_date = ?
		ORDER BY se.all_day DESC, se.event_time, se.task_force`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []announceEvent{}
	for rows.Next() {
		var ev announceEvent
		var allDay, announce int
		if err := rows.Scan(&ev.Date, &ev.Time, &allDay, &ev.TypeName, &ev.Level, &announce, &ev.TaskForce); err != nil {
			return nil, err
		}
		ev.AllDay = allDay == 1
		ev.Announce = announce == 1
		out = append(out, ev)
	}
	return out, rows.Err()
}

// GET /api/schedule/announcement?date=YYYY-MM-DD
func getAnnouncement(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if date == "" {
		date = gameDate()
	}
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		http.Error(w, "date must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	next := d.AddDate(0, 0, 1).Format("2006-01-02")

	window := loadAnnounceWindow()

	// Two short reads, both fully drained before the next statement — one
	// connection, and a cursor left open while the second query runs would hang.
	onD, err := loadAnnounceEvents(date)
	if err != nil {
		slog.Error("getAnnouncement events", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	var onD1 []announceEvent
	if window.Wraps {
		if onD1, err = loadAnnounceEvents(next); err != nil {
			slog.Error("getAnnouncement next-day events", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	events, dropped := selectAnnouncementEvents(window, onD, onD1)

	// The starred-mission lists the template MAY use. Supplied whether or not the
	// template references them: copyWithVariables skips a prefilled name whose
	// placeholder is absent, so offering both gives "neither / today / tomorrow /
	// both" with no extra configuration surface.
	starredToday, starredTomorrow := announcementStarred(date, next)

	writeJSON(w, map[string]any{
		"date":             date,
		"window":           window,
		"events":           events,
		"dropped":          dropped,
		"starred_today":    starredToday,
		"starred_tomorrow": starredTomorrow,
	})
}

// announcementStarred returns the starred server lists for the two dates, as
// comma-separated strings. Empty when the sector is not configured — an empty
// string substitutes cleanly into a template, where a "not configured" marker
// would end up pasted into the game.
func announcementStarred(date, next string) (string, string) {
	start, end, ok := loadSector()
	if !ok {
		return "", ""
	}
	known, err := loadSectorOpenDates(start, end)
	if err != nil {
		return "", ""
	}
	forDate := func(d string) string {
		group, err := starredGroup(d)
		if err != nil {
			return ""
		}
		ids := []string{}
		for _, row := range known {
			if row.Group == group {
				ids = append(ids, strconv.Itoa(row.ServerID))
			}
		}
		return strings.Join(ids, ", ")
	}
	return forDate(date), forDate(next)
}

// slugPrefilledVars records what the app fills in for each slugged template.
//
// This is the AUTHORITATIVE list, deliberately not `required_vars` — the comms
// handler lets an officer edit that column, so it cannot also be the source of
// truth about what the generator supplies.
//
// The required/optional split matters. `required` feeds the warning on save;
// `optional` does not. The seeded announcement template deliberately omits the
// starred variables, so warning about them on every untouched save would teach
// officers to dismiss the one warning that means something.
var slugPrefilledVars = map[string]struct {
	required []string
	optional []string
}{
	"ds_battle_mail": {required: []string{"task_force", "battle_time", "group_assignments"}},
	"nightly_events": {
		required: []string{"events"},
		optional: []string{"starred_today", "starred_tomorrow"},
	},
}

// missingPrefilledVars lists the variables this slug's generator fills in that the
// saved content no longer references. The save still succeeds — the officer may
// genuinely want a template without them — but they are told.
func missingPrefilledVars(slug, content string) []string {
	spec, ok := slugPrefilledVars[slug]
	if !ok {
		return nil
	}
	missing := []string{}
	for _, name := range spec.required {
		if !strings.Contains(content, "{"+name+"}") {
			missing = append(missing, name)
		}
	}
	return missing
}
