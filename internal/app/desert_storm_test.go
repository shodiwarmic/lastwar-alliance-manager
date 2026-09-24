package app

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// Desert Storm occurrences (#142): the task-force rule lives in the shared
// validator, the generator produces one battle per participating task force per
// Friday, and 081 migrates storm_attendance onto participation boards.

func dsTypeID(t *testing.T) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name = 'DS'`).Scan(&id); err != nil {
		t.Fatalf("DS type: %v", err)
	}
	return id
}

func createEvent(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	createScheduleEvent(rr, ptReq(http.MethodPost, "/", body, nil, nil))
	return rr
}

func TestDesertStormTypeIsSeededOnceAsSystem(t *testing.T) {
	setupSettingsTestDB(t)
	var n, sys int
	db.QueryRow(`SELECT COUNT(*), MAX(is_system) FROM schedule_event_types WHERE short_name = 'DS'`).Scan(&n, &sys)
	if n != 1 || sys != 1 {
		t.Errorf("DS types = %d (system %d), want exactly one system row", n, sys)
	}
	var rule string
	db.QueryRow(`SELECT absence_rule FROM participation_types WHERE event_type_id = ?`, dsTypeID(t)).Scan(&rule)
	if rule != "role" {
		t.Errorf("DS absence rule = %q, want role", rule)
	}
}

func TestDesertStormTaskForceRule(t *testing.T) {
	setupSettingsTestDB(t)
	ds := dsTypeID(t)
	base := map[string]any{"event_type_id": ds, "event_date": "2026-09-25", "event_time": "18:00"}
	with := func(tf string) map[string]any {
		m := map[string]any{}
		for k, v := range base {
			m[k] = v
		}
		if tf != "" {
			m["task_force"] = tf
		}
		return m
	}

	if rr := createEvent(t, with("")); rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "needs a task force") {
		t.Errorf("DS without TF = %d %q", rr.Code, rr.Body.String())
	}
	if rr := createEvent(t, with("A")); rr.Code != http.StatusCreated {
		t.Fatalf("DS TF A = %d %q", rr.Code, rr.Body.String())
	}
	if rr := createEvent(t, with("A")); rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "Task Force A is already scheduled on 2026-09-25") {
		t.Errorf("second DS TF A = %d %q", rr.Code, rr.Body.String())
	}
	if rr := createEvent(t, with("B")); rr.Code != http.StatusCreated {
		t.Errorf("DS TF B same date = %d %q", rr.Code, rr.Body.String())
	}
	mg, _ := systemTypeIDs(t)
	if rr := createEvent(t, map[string]any{"event_type_id": mg, "event_date": "2026-09-01", "event_time": "20:00", "task_force": "A"}); rr.Code != http.StatusBadRequest {
		t.Errorf("TF on a non-DS type = %d, want 400", rr.Code)
	}

	// Update honours excludeID: re-saving TF A's own row is not a clash with itself.
	var idA int
	db.QueryRow(`SELECT id FROM schedule_events WHERE task_force = 'A'`).Scan(&idA)
	rr := httptest.NewRecorder()
	updateScheduleEvent(rr, ptReq(http.MethodPut, "/", map[string]any{"event_type_id": ds, "event_time": "19:00", "task_force": "A"},
		map[string]string{"id": strconv.Itoa(idA)}, nil))
	if rr.Code != http.StatusNoContent {
		t.Errorf("update own DS row = %d %q", rr.Code, rr.Body.String())
	}
	// ...but moving it onto TF B's slot is.
	rr = httptest.NewRecorder()
	updateScheduleEvent(rr, ptReq(http.MethodPut, "/", map[string]any{"event_type_id": ds, "task_force": "B"},
		map[string]string{"id": strconv.Itoa(idA)}, nil))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("update onto TF B = %d, want 400", rr.Code)
	}

	// A legacy row (no task force) stays editable without one.
	res, _ := db.Exec(`INSERT INTO schedule_events (event_date, event_type_id, event_time, all_day, notes, created_by) VALUES ('2026-08-07', ?, '00:00', 1, '', 1)`, ds)
	legacy, _ := res.LastInsertId()
	rr = httptest.NewRecorder()
	updateScheduleEvent(rr, ptReq(http.MethodPut, "/", map[string]any{"event_type_id": ds, "all_day": true, "notes": "battle vs XYZ"},
		map[string]string{"id": strconv.Itoa(int(legacy))}, nil))
	if rr.Code != http.StatusNoContent {
		t.Errorf("edit notes on legacy DS row = %d %q", rr.Code, rr.Body.String())
	}
}

type dsGenerateResponse struct {
	generateResponse
	DSCreated    int `json:"ds_created"`
	SkippedError int `json:"skipped_error"`
}

func runGenerateDS(t *testing.T, from, to string, types ...string) dsGenerateResponse {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"from": from, "to": to, "types": types})
	rr := httptest.NewRecorder()
	generateScheduleEvents(rr, scheduleTestActor(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))))
	if rr.Code != http.StatusOK {
		t.Fatalf("generate = %d %s", rr.Code, rr.Body.String())
	}
	var out dsGenerateResponse
	json.Unmarshal(rr.Body.Bytes(), &out)
	return out
}

func TestGeneratorCreatesDesertStormPerParticipatingTaskForce(t *testing.T) {
	setupSettingsTestDB(t)
	db.Exec(`DELETE FROM storm_tf_config`)
	db.Exec(`INSERT INTO storm_tf_config (task_force, time_slot, participating) VALUES ('A', 3, 1), ('B', 2, 0)`)
	db.Exec(`UPDATE storm_slot_times SET time_st = '23:00' WHERE slot = 3`)

	// 2026-09-21 (Mon) → 2026-10-04 (Sun): two Fridays.
	out := runGenerateDS(t, "2026-09-21", "2026-10-04", "ds")
	if out.DSCreated != 2 {
		t.Fatalf("ds_created = %d, want 2 (TF A only, two Fridays): %+v", out.DSCreated, out)
	}
	rows, _ := db.Query(`SELECT se.event_date, se.event_time, se.task_force FROM schedule_events se
		JOIN schedule_event_types t ON t.id = se.event_type_id WHERE t.short_name = 'DS' ORDER BY se.event_date`)
	var got []string
	for rows.Next() {
		var d, tm string
		var tf sql.NullString
		rows.Scan(&d, &tm, &tf)
		got = append(got, d+" "+tm+" "+tf.String)
	}
	rows.Close()
	if strings.Join(got, ",") != "2026-09-25 23:00 A,2026-10-02 23:00 A" {
		t.Errorf("DS rows = %v", got)
	}
	if again := runGenerateDS(t, "2026-09-21", "2026-10-04", "ds"); again.DSCreated != 0 || again.SkippedExisting != 2 {
		t.Errorf("re-run = %+v, want nothing new and 2 existing", again)
	}
}

// The existence check binds NULL for every non-DS candidate; `= NULL` is never
// true, so without `IS ?` a re-run would re-insert every MG and ZS.
func TestGeneratorRerunIsIdempotentForEveryType(t *testing.T) {
	setupSettingsTestDB(t)
	if _, err := db.Exec(`UPDATE settings SET mg_anchor_date='2026-09-07', mg_default_time='20:00',
	      zs_schedule_mode='weekdays', zs_weekdays='1,4', zs_default_time='23:00' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	first := runGenerateDS(t, "2026-09-07", "2026-09-20", "mg", "zs")
	if first.MGCreated+first.ZSCreated == 0 {
		t.Fatalf("first run generated nothing: %+v", first)
	}
	second := runGenerateDS(t, "2026-09-07", "2026-09-20", "mg", "zs")
	if second.MGCreated+second.ZSCreated != 0 {
		t.Errorf("second run created %d MG + %d ZS, want none", second.MGCreated, second.ZSCreated)
	}
	if second.SkippedExisting != first.MGCreated+first.ZSCreated {
		t.Errorf("skipped_existing = %d, want %d", second.SkippedExisting, first.MGCreated+first.ZSCreated)
	}
}

// The generator now runs the full validator, so a rule attached to the type — here
// a parent window — reaches generated rows exactly as it reaches a manual create.
func TestGeneratorAppliesTheWindowRule(t *testing.T) {
	setupSettingsTestDB(t)
	mg, _ := systemTypeIDs(t)
	gt := anchorServerEvent(t, "General's Trial", "2026-09-07") // 3-day window every 14 days
	db.Exec(`UPDATE schedule_event_types SET server_event_id = ? WHERE id = ?`, gt, mg)
	db.Exec(`UPDATE settings SET mg_anchor_date='2026-09-07', mg_default_time='20:00' WHERE id=1`)
	out := runGenerateDS(t, "2026-09-07", "2026-09-13", "mg")
	if out.MGCreated != 2 || out.SkippedInvalid != 2 {
		t.Errorf("generate under a 3-day window = %+v, want 2 created (09-07, 09-09) and 2 declined (09-11, 09-13)", out)
	}
}

func TestSeasonPushDeclinesDesertStormTemplateRows(t *testing.T) {
	setupSettingsTestDB(t)
	s, _, _ := seedPushableSeason(t, "2026-09-07")
	ds := dsTypeID(t)
	db.Exec(`UPDATE season_events SET event_type_id = ?, type_name = 'Desert Storm' WHERE is_server_event = 0`, ds)
	result, err := pushSeasonEventsToSchedule(s, 1, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if result.SkippedInvalid == 0 || len(result.Invalid) == 0 || !strings.Contains(result.Invalid[0].Reason, "needs a task force") {
		t.Errorf("push of a DS template = %+v, want it declined for want of a task force", result)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM schedule_events WHERE event_type_id = ?`, ds).Scan(&n)
	if n != 0 {
		t.Errorf("%d DS rows written with no task force", n)
	}
}

func TestDesertStormRolePrefillFollowsTheBattlesTaskForce(t *testing.T) {
	f := setupParticipationTestDB(t)
	_ = f
	ds := dsTypeID(t)
	db.Exec(`INSERT INTO storm_groups (id, task_force, name, instructions, sort_order) VALUES (1,'A','A1','',0),(2,'B','B1','',0)`)
	db.Exec(`INSERT INTO storm_group_members (group_id, member_id, is_sub, position) VALUES (1,1,0,0),(1,2,1,1),(2,3,0,0)`)
	res, _ := db.Exec(`INSERT INTO schedule_events (event_date, event_type_id, event_time, notes, created_by, task_force) VALUES ('2026-09-18', ?, '23:00', '', 1, 'A')`, ds)
	id, _ := res.LastInsertId()
	d, err := buildBoardDetail(db, int(id))
	if err != nil || d == nil {
		t.Fatalf("detail: %v", err)
	}
	var got []string
	for _, r := range d.RolesPrefill {
		got = append(got, strconv.Itoa(r.MemberID)+":"+r.Role+":"+r.TaskForce)
	}
	if strings.Join(got, ",") != "1:starter:A,2:sub:A" {
		t.Errorf("prefill = %v, want TF A's lineup only", got)
	}
}

// --- Migration 081 over legacy storm_attendance ---------------------------------

func legacyFixture(t *testing.T, conn *sql.DB, extra ...string) {
	t.Helper()
	stmts := append([]string{
		`INSERT INTO users (id, username, password) VALUES (7, 'officer', 'x')`,
		`INSERT INTO members (id, name, rank) VALUES (1,'Attender','R3'),(2,'NoShow','R3'),(3,'Excused','R3'),(4,'Enrolled','R3')`,
		`INSERT INTO storm_attendance (storm_date, member_id, status, excuse_reason, recorded_by) VALUES
		    ('2026-08-07', 1, 'attended', '', 7), ('2026-08-07', 2, 'no_show', '', 7),
		    ('2026-08-07', 3, 'excused', 'travelling', 7), ('2026-08-07', 4, 'not_enrolled', '', 7),
		    ('2026-08-07', 99, 'no_show', '', 7)`,
	}, extra...)
	for _, q := range stmts {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("fixture %q: %v", q, err)
		}
	}
}

func TestLegacyStormAttendanceMigrates(t *testing.T) {
	conn := migrateTo(t, 80)
	legacyFixture(t, conn)
	if err := goose.UpTo(conn, "migrations", 81); err != nil {
		t.Fatalf("goose.UpTo(81): %v", err)
	}
	q := func(sql string) int {
		var n int
		if err := conn.QueryRow(sql).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	if n := q(`SELECT COUNT(*) FROM schedule_events se JOIN schedule_event_types t ON t.id = se.event_type_id
		WHERE t.short_name = 'DS' AND se.event_date = '2026-08-07' AND se.task_force IS NULL AND se.all_day = 1 AND se.created_by = 7`); n != 1 {
		t.Errorf("legacy occurrences = %d, want one all-day, TF-less, authored by the recording officer", n)
	}
	if n := q(`SELECT COUNT(*) FROM participation_boards WHERE source = 'legacy' AND recorded_by = 7`); n != 1 {
		t.Errorf("legacy boards = %d, want 1", n)
	}
	if n := q(`SELECT COUNT(*) FROM participation_entries WHERE member_id = 1 AND rank IS NULL`); n != 1 {
		t.Errorf("attended → entries = %d, want 1", n)
	}
	if n := q(`SELECT COUNT(*) FROM participation_exceptions WHERE member_id = 2 AND kind = 'missed'`); n != 1 {
		t.Errorf("no_show → missed = %d, want 1", n)
	}
	if n := q(`SELECT COUNT(*) FROM participation_exceptions WHERE member_id = 3 AND kind = 'excused' AND reason = 'travelling'`); n != 1 {
		t.Errorf("excused → excused = %d, want 1", n)
	}
	if n := q(`SELECT COUNT(*) FROM participation_entries WHERE member_id = 4`) + q(`SELECT COUNT(*) FROM participation_exceptions WHERE member_id = 4`); n != 0 {
		t.Errorf("not_enrolled wrote %d rows, want none", n)
	}
	if n := q(`SELECT COUNT(*) FROM participation_exceptions WHERE member_id = 99`); n != 0 {
		t.Errorf("orphan member_id carried across (%d rows)", n)
	}
	if n := q(`SELECT COUNT(*) FROM participation_roles`); n != 0 {
		t.Errorf("roles guessed for legacy rows: %d", n)
	}
	if n := q(`SELECT COUNT(*) FROM storm_attendance`); n != 5 {
		t.Errorf("storm_attendance = %d rows, want it left in place", n)
	}
}

// An install that made a Desert Storm type by hand keeps its rows: the type is
// promoted in place, and a legacy date that already has a battle on it gets its
// board attached there rather than a duplicate occurrence.
func TestLegacyMigrationAttachesToAPromotedTypesRow(t *testing.T) {
	conn := migrateTo(t, 80)
	legacyFixture(t, conn,
		`INSERT INTO schedule_event_types (id, name, short_name, icon, is_system, active, sort_order) VALUES (90, 'Desert Storm', 'DSX', '🌪', 0, 1, 9)`,
		`INSERT INTO schedule_events (id, event_date, event_type_id, event_time, notes, created_by) VALUES (500, '2026-08-07', 90, '20:00', 'hand-made', 7)`)
	if err := goose.UpTo(conn, "migrations", 81); err != nil {
		t.Fatalf("goose.UpTo(81): %v", err)
	}
	var short string
	var sys int
	conn.QueryRow(`SELECT short_name, is_system FROM schedule_event_types WHERE id = 90`).Scan(&short, &sys)
	if short != "DS" || sys != 1 {
		t.Errorf("hand-made type = %s/%d, want promoted to system DS in place", short, sys)
	}
	var events, boardOn int
	conn.QueryRow(`SELECT COUNT(*) FROM schedule_events WHERE event_type_id = 90`).Scan(&events)
	conn.QueryRow(`SELECT schedule_event_id FROM participation_boards WHERE source = 'legacy'`).Scan(&boardOn)
	if events != 1 || boardOn != 500 {
		t.Errorf("events = %d, board on %d; want the existing row 500 reused", events, boardOn)
	}
}
