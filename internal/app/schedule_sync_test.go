package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// insertCalendarRow inserts one calendar row; extra is "col1, col2" with one value each.
func insertCalendarRow(t *testing.T, date, short, tm string, extra string, args ...any) int {
	t.Helper()
	cols, vals := "", ""
	if extra != "" {
		cols = ", " + extra
		vals = strings.Repeat(", ?", len(args))
	}
	res, err := db.Exec(`INSERT INTO schedule_events (event_date, event_type_id, event_time, created_by`+cols+`)
		VALUES (?, ?, ?, 1`+vals+`)`, append([]any{date, typeIDByShort(t, short), tm}, args...)...)
	if err != nil {
		t.Fatalf("insert %s %s: %v", short, date, err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func TestSeasonTimelineQueryAndVisibility(t *testing.T) {
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	// S2-like: eight weeks plus a post-season week 12 template row.
	seedSeasonRow(t, 1, 2, "2026-03-16", false, true)
	mustExecT(t, `INSERT INTO season_events (season_id, label, week_start, week_end, day_offset) VALUES (1, 'Champion Duel', 10, 12, 1)`)
	mustExecT(t, `INSERT INTO schedule_event_types (name, short_name, icon) VALUES ('Champion Duel', 'CD', 'x')`)
	early := insertCalendarRow(t, "2026-03-18", "CD", "12:00", "season_id", 1)
	late := insertCalendarRow(t, "2026-06-01", "CD", "12:00", "season_id", 1) // week 12, 77 days in
	insertCalendarRow(t, "2026-03-19", "CD", "12:00", "")                     // untagged
	secret := insertCalendarRow(t, "2026-03-20", "CD", "12:00", "season_id, leadership_only", 1, 1)

	list := func(actor *AuthUser, query string) []ScheduleEvent {
		req := shReq(http.MethodGet, "", nil, actor)
		req.URL.RawQuery = query
		rr := httptest.NewRecorder()
		getScheduleEvents(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", query, rr.Code, rr.Body.String())
		}
		var out struct {
			Events []ScheduleEvent `json:"events"`
		}
		if strings.HasPrefix(strings.TrimSpace(rr.Body.String()), "[") {
			json.Unmarshal(rr.Body.Bytes(), &out.Events)
		} else {
			json.Unmarshal(rr.Body.Bytes(), &out)
		}
		return out.Events
	}
	ids := func(evs []ScheduleEvent) map[int]bool {
		m := map[int]bool{}
		for _, e := range evs {
			m[e.ID] = true
		}
		return m
	}

	got := ids(list(nil, "season_id=1"))
	if !got[early] || !got[late] || !got[secret] || len(got) != 3 {
		t.Errorf("season timeline (manager) = %v, want the three tagged rows incl. week 12", got)
	}
	if n := len(list(nil, "season_id=1&scope=all")); n != 4 {
		t.Errorf("scope=all returned %d rows, want 4", n)
	}

	r3 := seedMember(t, "Member", "R3")
	mustExecT(t, `UPDATE rank_permissions SET permissions = json_set(permissions, '$.view_schedule', json('true'), '$.manage_schedule', json('false')) WHERE rank = 'R3'`)
	member := &AuthUser{ID: 3, Username: "m", Rank: "R3", MemberID: &r3}
	if got := ids(list(member, "season_id=1")); got[secret] {
		t.Error("a leadership-only row reached a member without manage_schedule (timeline)")
	}
	if got := ids(list(member, "from=2026-03-16&to=2026-03-25")); got[secret] {
		t.Error("a leadership-only row reached a member without manage_schedule (calendar)")
	}

	// Never in the announcement, whoever generates it.
	evs, err := loadAnnounceEvents("2026-03-20")
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 0 {
		t.Errorf("announcement carries %d leadership-only event(s)", len(evs))
	}
}

func TestCancelledRowsLeaveTheRuleChecks(t *testing.T) {
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	zs := typeIDByShort(t, "ZS")
	tr, _ := loadScheduleTypeRules(db, zs)
	insertCalendarRow(t, "2030-05-06", "ZS", "00:00", "status", "cancelled")
	if msg, err := validateEventRules(db, tr, eventCandidate{Date: "2030-05-08", Time: "00:00"}, 0); err != nil || msg != "" {
		t.Errorf("a cancelled ZS still blocks a siege two days later: %q %v", msg, err)
	}

	tf := "A"
	insertCalendarRow(t, "2030-05-10", "DS", "18:00", "task_force, status", tf, "cancelled")
	ds := typeIDByShort(t, "DS")
	dsr, _ := loadScheduleTypeRules(db, ds)
	if msg, _ := validateEventRules(db, dsr, eventCandidate{Date: "2030-05-10", Time: "18:00", TaskForce: &tf}, 0); msg != "" {
		t.Errorf("a cancelled DS TF A still blocks rebooking: %q", msg)
	}
	insertCalendarRow(t, "2030-05-10", "DS", "18:00", "task_force", tf) // the index lets it through
}

func TestSyncSourcedScheduleEvent(t *testing.T) {
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	seedSeasonRow(t, 1, 4, "2030-11-04", true, false)
	lc, err := sourcedTypeID(db, "Land Capture")
	if err != nil {
		t.Fatal(err)
	}
	sync := func(src sourcedEvent) syncResult {
		t.Helper()
		tx, _ := db.Begin()
		defer tx.Rollback()
		res, err := syncSourcedScheduleEvent(tx, src, 1)
		if err != nil {
			t.Fatal(err)
		}
		tx.Commit()
		return res
	}
	src := sourcedEvent{Kind: sourceLandCapture, ID: 7, SeasonID: 1, Date: "2030-11-06", Time: "15:00", TypeID: lc, Notes: "Cottage"}
	if r := sync(src); r.Action != "created" {
		t.Fatalf("first sync = %+v", r)
	}
	if r := sync(src); r.Action != "unchanged" {
		t.Errorf("second identical sync = %+v", r)
	}
	src.Time = "16:00"
	if r := sync(src); r.Action != "moved" {
		t.Errorf("moved sync = %+v", r)
	}
	src.Status = "cancelled"
	if r := sync(src); r.Action != "cancelled" {
		t.Errorf("cancel sync = %+v", r)
	}
	if n := countT(t, `SELECT COUNT(*) FROM schedule_events WHERE source_kind = 'land_capture' AND source_id = 7`); n != 1 {
		t.Errorf("%d rows for one source, want exactly 1", n)
	}
	if n := countT(t, `SELECT COUNT(*) FROM schedule_events WHERE source_id = 7 AND status = 'cancelled' AND event_time = '16:00'`); n != 1 {
		t.Error("cancelling deleted or reset the row")
	}

	// A refusal by the game rules writes nothing and says why.
	zs := typeIDByShort(t, "ZS")
	insertCalendarRow(t, "2030-11-10", "ZS", "00:00", "")
	r := sync(sourcedEvent{Kind: sourceTradePost, ID: 9, SeasonID: 1, Date: "2030-11-12", Time: "00:00", TypeID: zs})
	if r.Action != "refused" || !strings.Contains(r.Message, "ZS") {
		t.Errorf("refused sync = %+v", r)
	}
	if n := countT(t, `SELECT COUNT(*) FROM schedule_events WHERE source_id = 9`); n != 0 {
		t.Error("a refused sync wrote a row")
	}
}

func TestDetectSeasonConflicts(t *testing.T) {
	sid := 1
	events := []conflictEvent{
		{ID: 1, Date: "2030-11-06", Time: "12:00", TypeName: "Copper War", Short: "CPW", SeasonID: &sid},
		{ID: 2, Date: "2030-11-06", Time: "12:30", TypeName: "Marshal's Guard", Short: "MG", IsSystem: true},
		{ID: 3, Date: "2030-11-06", Time: "20:00", TypeName: "Rally", Short: "R", SeasonID: &sid},
	}
	got := detectSeasonConflicts(events, nil, 60)
	if len(got) != 1 || got[0].EventID != 1 || got[0].Suggestion != "13:30" {
		t.Fatalf("conflicts = %+v, want event 1 with 13:30 suggested", got)
	}
	if got[0].Conflicts[0].Minutes != 30 {
		t.Errorf("minutes apart = %d", got[0].Conflicts[0].Minutes)
	}
	// A window opening at the reset blocks an event in the first hour.
	early := []conflictEvent{{ID: 4, Date: "2030-11-07", Time: "00:30", TypeName: "Rally", Short: "R", SeasonID: &sid}}
	got = detectSeasonConflicts(early, []conflictWindow{{Name: "Zombie Invasion opens", Date: "2030-11-07"}}, 60)
	if len(got) != 1 || got[0].Suggestion != "01:00" {
		t.Errorf("window conflict = %+v", got)
	}
}
