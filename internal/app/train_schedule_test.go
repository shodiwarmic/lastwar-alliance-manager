package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

func scheduleDate(days int) string {
	d, _ := time.Parse(dateLayout, gameDate())
	return d.AddDate(0, 0, days).Format(dateLayout)
}
func planBody(conductor int, date string) map[string]any {
	return map[string]any{"date": date, "train_type": "FREE", "conductor_id": conductor, "notes": "plan notes"}
}
func scheduleCall(t *testing.T, method, path string, body any, user int) *httptest.ResponseRecorder {
	t.Helper()
	data := ""
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		data = string(b)
	}
	var cookie *http.Cookie
	if user > 0 {
		cookie = sessionCookie(t, user, "")
	}
	return serveRouter(t, method, path, data, cookie)
}
func makePlan(t *testing.T, body map[string]any) TrainScheduleEntry {
	t.Helper()
	w := scheduleCall(t, "POST", "/api/train-schedule", body, 1)
	if w.Code != 201 {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	var s TrainScheduleEntry
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	return s
}
func schedulePath(s TrainScheduleEntry) string { return "/api/train-schedule/" + strconv.Itoa(s.ID) }
func storedPlan(t *testing.T, id int) TrainScheduleEntry {
	t.Helper()
	s, err := scanTrainSchedule(db.QueryRow(trainScheduleSelect+` WHERE s.id=?`, id))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func upcomingPlans(t *testing.T) []TrainScheduleEntry {
	t.Helper()
	w := scheduleCall(t, "GET", "/api/train-schedule", nil, 1)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct {
		Entries  []TrainScheduleEntry `json:"entries"`
		GameDate string               `json:"game_date"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Entries == nil || out.GameDate != gameDate() {
		t.Fatalf("bad schedule response: %s", w.Body.String())
	}
	return out.Entries
}

func TestTrainScheduleDefaultsAndPermissions(t *testing.T) {
	f := setupMobileTestDB(t)
	for _, rank := range []string{"R1", "R2", "R3", "R4", "R5"} {
		p := getRankPermissions(rank)
		want := rank == "R4" || rank == "R5"
		if p.ViewTrainSchedule != want || p.ManageTrainSchedule != want {
			t.Errorf("%s defaults: %+v", rank, p)
		}
	}
	for _, route := range []struct{ method, path string }{{"GET", "/api/train-schedule"}, {"POST", "/api/train-schedule"}, {"PUT", "/api/train-schedule/1"}, {"POST", "/api/train-schedule/1/cancel"}, {"POST", "/api/train-schedule/1/complete"}} {
		if w := scheduleCall(t, route.method, route.path, planBody(f.other, gameDate()), 0); w.Code != 401 {
			t.Errorf("unauth %v = %d", route, w.Code)
		}
		if w := scheduleCall(t, route.method, route.path, planBody(f.other, gameDate()), 3); w.Code != 403 {
			t.Errorf("R3 %v = %d", route, w.Code)
		}
	}
	setRankPerm(t, "R3", "view_train", false)
	setRankPerm(t, "R3", "view_train_schedule", true)
	if w := scheduleCall(t, "GET", "/api/train-schedule", nil, 3); w.Code != 200 {
		t.Fatal("granted viewer", w.Code)
	}
	if w := serveRouter(t, "GET", "/train", "", sessionCookie(t, 3, "")); w.Code != 200 || !strings.Contains(w.Body.String(), `data-tab="upcoming"`) || strings.Contains(w.Body.String(), `data-tab="logs"`) {
		t.Fatal("schedule-only page inaccessible/leaked history", w.Code)
	}
	if w := scheduleCall(t, "POST", "/api/train-schedule", planBody(f.other, gameDate()), 3); w.Code != 403 {
		t.Fatal("viewer can create")
	}
	setRankPerm(t, "R3", "manage_train_schedule", true)
	setRankPerm(t, "R3", "view_train_schedule", false)
	setRankPerm(t, "R3", "manage_train", false)
	w := scheduleCall(t, "POST", "/api/train-schedule", planBody(f.other, gameDate()), 3)
	if w.Code != 201 {
		t.Fatal("schedule manager cannot create", w.Code, w.Body.String())
	}
	var s TrainScheduleEntry
	json.Unmarshal(w.Body.Bytes(), &s)
	if w := scheduleCall(t, "PUT", schedulePath(s), planBody(f.r3Member, gameDate()), 3); w.Code != 200 {
		t.Fatal("schedule manager cannot edit", w.Code)
	}
	if w := scheduleCall(t, "POST", schedulePath(s)+"/complete", map[string]any{"conductor_id": f.other}, 3); w.Code != 403 {
		t.Fatal("completion did not require manage_train", w.Code)
	}
	if w := scheduleCall(t, "POST", schedulePath(s)+"/cancel", nil, 3); w.Code != 204 {
		t.Fatal("schedule manager cannot cancel", w.Code)
	}
	s = makePlan(t, planBody(f.other, gameDate()))
	for _, route := range []struct{ method, path string }{{"PUT", schedulePath(s)}, {"POST", schedulePath(s) + "/cancel"}, {"POST", schedulePath(s) + "/complete"}} {
		setRankPerm(t, "R3", "manage_train_schedule", false)
		if w := scheduleCall(t, route.method, route.path, planBody(f.other, gameDate()), 3); w.Code != 403 {
			t.Errorf("viewer mutated %v: %d", route, w.Code)
		}
	}
}

func TestTrainScheduleValidationAndLifecycle(t *testing.T) {
	f := setupMobileTestDB(t)
	for _, modify := range []func(map[string]any){
		func(b map[string]any) { b["date"] = scheduleDate(-1) }, func(b map[string]any) { b["date"] = "2026-02-30" },
		func(b map[string]any) { b["train_type"] = "UNKNOWN" }, func(b map[string]any) { b["conductor_id"] = f.former }, func(b map[string]any) { b["conductor_id"] = 99999 },
		func(b map[string]any) { b["vip_id"] = f.other }, func(b map[string]any) { b["vip_type"] = "SPECIAL_GUEST" },
		func(b map[string]any) { b["vip_id"] = f.former; b["vip_type"] = "SPECIAL_GUEST" }, func(b map[string]any) { b["vip_id"] = f.other; b["vip_type"] = "INVALID" },
	} {
		b := planBody(f.r4Member, scheduleDate(1))
		modify(b)
		if w := scheduleCall(t, "POST", "/api/train-schedule", b, 1); w.Code != 400 {
			t.Errorf("invalid %+v = %d %s", b, w.Code, w.Body.String())
		}
	}
	s := makePlan(t, planBody(f.r4Member, scheduleDate(1)))
	other := makePlan(t, planBody(f.other, s.Date))
	if other.ID == s.ID {
		t.Fatal("duplicate dates not supported")
	}
	b := planBody(f.other, scheduleDate(2))
	b["vip_id"] = f.r3Member
	b["vip_type"] = "GUARDIAN_DEFENDER"
	if w := scheduleCall(t, "PUT", schedulePath(s), b, 1); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	got := storedPlan(t, s.ID)
	if got.ConductorID == nil || *got.ConductorID != f.other || got.VIPType == nil || *got.VIPType != "GUARDIAN_DEFENDER" {
		t.Fatalf("edit: %+v", got)
	}
	if w := scheduleCall(t, "POST", schedulePath(s)+"/complete", b, 1); w.Code != 409 {
		t.Fatal("completed future plan", w.Code)
	}
	if w := scheduleCall(t, "POST", schedulePath(s)+"/cancel", nil, 1); w.Code != 204 {
		t.Fatal(w.Code)
	}
	for _, route := range []struct{ method, path string }{{"PUT", schedulePath(s)}, {"POST", schedulePath(s) + "/complete"}, {"POST", schedulePath(s) + "/cancel"}} {
		if w := scheduleCall(t, route.method, route.path, b, 1); w.Code != 409 {
			t.Errorf("closed %v=%d", route, w.Code)
		}
	}
	got = storedPlan(t, s.ID)
	if got.Status != "cancelled" || got.CancelledBy == nil {
		t.Fatal("cancel removed/failed", got)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM train_logs`); n != 0 {
		t.Fatal("plans changed historic counts", n)
	}
	// Overdue plans cannot be created, but time passing must never hide them.
	if _, err := db.Exec(`UPDATE train_schedule_entries SET date=? WHERE id=?`, scheduleDate(-2), other.ID); err != nil {
		t.Fatal(err)
	}
	if w := scheduleCall(t, "PUT", schedulePath(other), planBody(f.other, gameDate()), 1); w.Code != 409 {
		t.Fatal("overdue plan editable", w.Code)
	}
	found := false
	for _, p := range upcomingPlans(t) {
		if p.ID == other.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("overdue plan hidden")
	}
	if _, err := db.Exec(`UPDATE train_schedule_entries SET date=? WHERE id=?`, scheduleDate(-2), s.ID); err != nil {
		t.Fatal(err)
	}
	for _, p := range upcomingPlans(t) {
		if p.ID == s.ID {
			t.Fatal("old cancelled plan still in upcoming")
		}
	}
	// Schedule writes must not change the existing historic log's future-date rule.
	if w := scheduleCall(t, "POST", "/api/train-logs", planBody(f.other, scheduleDate(1)), 1); w.Code != 400 {
		t.Fatal("manual logs accept futures", w.Code)
	}
}

func TestTrainScheduleCompletionActualResult(t *testing.T) {
	f := setupMobileTestDB(t)
	b := planBody(f.r4Member, gameDate())
	b["vip_id"] = f.r3Member
	b["vip_type"] = "SPECIAL_GUEST"
	s := makePlan(t, b)
	actual := map[string]any{"conductor_id": f.other, "vip_id": f.r4Member, "vip_type": "GUARDIAN_DEFENDER", "notes": "actual notes", "date": scheduleDate(2), "train_type": "PURCHASED"}
	if _, err := db.Exec(`UPDATE settings SET train_free_daily_limit=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if w := scheduleCall(t, "POST", "/api/train-logs", planBody(f.r3Member, gameDate()), 1); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := scheduleCall(t, "POST", schedulePath(s)+"/complete", actual, 1)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct {
		Log     TrainLog `json:"train_log"`
		Warning bool     `json:"limit_warning"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Warning || out.Log.ConductorID != f.other || out.Log.VIPID == nil || *out.Log.VIPID != f.r4Member || out.Log.TrainType != "FREE" || out.Log.Date != gameDate() || out.Log.Notes != "actual notes" {
		t.Fatalf("actual result %+v", out)
	}
	got := storedPlan(t, s.ID)
	if got.Status != "completed" || got.CompletedLogID == nil || *got.CompletedLogID != out.Log.ID || *got.ConductorID != f.r4Member || *got.VIPID != f.r3Member || got.Notes != "plan notes" {
		t.Fatalf("plan changed %+v", got)
	}
	if w := scheduleCall(t, "POST", schedulePath(s)+"/complete", actual, 1); w.Code != 409 {
		t.Fatal("double completion", w.Code)
	}
	if w := scheduleCall(t, "PUT", schedulePath(s), b, 1); w.Code != 409 {
		t.Fatal("completed editable", w.Code)
	}
	if countRows(t, `SELECT COUNT(*) FROM train_logs`) != 2 {
		t.Fatal("duplicate completion log")
	}
	if countRows(t, `SELECT COUNT(*) FROM activity_log WHERE entity_type='train_schedule_entry' AND details LIKE '%actual conductor: Zed Player%'`) != 1 {
		t.Fatal("completion audit missing")
	}
	// Same ordinary log participates in history, no-show/accountability and deletion.
	w = scheduleCall(t, "PUT", "/api/train-logs/"+strconv.Itoa(out.Log.ID)+"/showed-up", map[string]any{"showed_up": 0}, 1)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if countRows(t, `SELECT COUNT(*) FROM accountability_strikes WHERE member_id=? AND strike_type='train_no_show'`, f.other) != 1 {
		t.Fatal("normal no-show strike missing")
	}
	if w := scheduleCall(t, "DELETE", "/api/train-logs/"+strconv.Itoa(out.Log.ID), nil, 1); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if got := storedPlan(t, s.ID); got.Status != "planned" || got.CompletedLogID != nil {
		t.Fatal("deleting log didn't reopen plan", got)
	}
}

func TestTrainScheduleCompletionAtomicAndConcurrent(t *testing.T) {
	f := setupMobileTestDB(t)
	s := makePlan(t, planBody(f.r4Member, gameDate()))
	actual := map[string]any{"conductor_id": f.other}
	if _, err := db.Exec(`UPDATE train_schedule_entries SET date=? WHERE id=?`, scheduleDate(-1), s.ID); err != nil {
		t.Fatal(err)
	}
	if w := scheduleCall(t, "POST", schedulePath(s)+"/complete", map[string]any{"conductor_id": f.former}, 1); w.Code != 400 {
		t.Fatal("invalid actual conductor", w.Code)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_plan_link BEFORE UPDATE OF completed_log_id ON train_schedule_entries BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if w := scheduleCall(t, "POST", schedulePath(s)+"/complete", actual, 1); w.Code != 500 {
		t.Fatal("injected failure", w.Code)
	}
	if countRows(t, `SELECT COUNT(*) FROM train_logs`) != 0 || storedPlan(t, s.ID).CompletedLogID != nil {
		t.Fatal("partial completion survived rollback")
	}
	if _, err := db.Exec(`DROP TRIGGER fail_plan_link`); err != nil {
		t.Fatal(err)
	}
	// Calls start together but the app's single DB connection serialises completion.
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	cookie := sessionCookie(t, 1, "")
	body, _ := json.Marshal(actual)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- serveRouter(t, "POST", schedulePath(s)+"/complete", string(body), cookie).Code
		}()
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for c := range codes {
		counts[c]++
	}
	if counts[200] != 1 || counts[409] != 1 || countRows(t, `SELECT COUNT(*) FROM train_logs`) != 1 {
		t.Fatal("concurrent completion", counts)
	}
	// An old completed plan is not in Upcoming, but its actual log remains.
	for _, p := range upcomingPlans(t) {
		if p.ID == s.ID {
			t.Fatal("old completed plan still shown")
		}
	}
}

func TestTrainSchedulePreservesLeavingAndDeletedMembers(t *testing.T) {
	f := setupMobileTestDB(t)
	b := planBody(f.other, gameDate())
	b["vip_id"] = f.r3Member
	b["vip_type"] = "SPECIAL_GUEST"
	s := makePlan(t, b)
	if _, err := db.Exec(`UPDATE members SET rank='EX' WHERE id=?`, f.other); err != nil {
		t.Fatal(err)
	}
	if got := storedPlan(t, s.ID); got.ConductorName != "Zed Player" || got.ConductorID == nil {
		t.Fatal("EX disappeared", got)
	}
	if w := scheduleCall(t, "POST", schedulePath(s)+"/complete", map[string]any{"conductor_id": f.other}, 1); w.Code != 400 {
		t.Fatal("EX completed", w.Code)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := deleteMemberTx(tx, f.other); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := storedPlan(t, s.ID); got.ConductorID != nil || got.ConductorName != "Zed Player" {
		t.Fatal("deleted planned identity lost", got)
	}
	if w := scheduleCall(t, "POST", schedulePath(s)+"/complete", map[string]any{"conductor_id": f.r4Member}, 1); w.Code != 200 {
		t.Fatal("replacement actual conductor", w.Code, w.Body.String())
	}
}

func TestTrainScheduleMigration(t *testing.T) {
	conn := migrateTo(t, 84)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`PRAGMA foreign_keys=ON`)
	exec(`INSERT INTO users(id,username,password) VALUES(7,'officer','x'),(8,'logger','x')`)
	exec(`INSERT INTO members(id,name,rank) VALUES(1,'Alice','R4'),(2,'Bob','R3')`)
	if err := goose.UpTo(conn, "migrations", 86); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO train_schedule_entries(date,train_type,conductor_id,conductor_name,vip_id,vip_name,vip_type,created_by) VALUES('2026-10-15','FREE',1,'Alice',2,'Bob','SPECIAL_GUEST',7),('2026-10-15','PURCHASED',1,'Alice',NULL,'',NULL,7)`)
	exec(`INSERT INTO train_logs(id,date,train_type,conductor_id,created_by) VALUES(1,'2026-10-15','FREE',1,8)`)
	exec(`UPDATE train_schedule_entries SET completed_log_id=1 WHERE id=1`)
	if _, err := conn.Exec(`UPDATE train_schedule_entries SET completed_log_id=1 WHERE id=2`); err == nil {
		t.Fatal("duplicate completion link allowed")
	}
	if _, err := conn.Exec(`UPDATE train_schedule_entries SET cancelled_at=CURRENT_TIMESTAMP WHERE id=1`); err == nil {
		t.Fatal("contradictory state allowed")
	}
	if _, err := conn.Exec(`UPDATE train_schedule_entries SET completed_log_id=999 WHERE id=2`); err == nil {
		t.Fatal("dangling completion allowed")
	}
	exec(`DELETE FROM train_logs WHERE id=1`)
	var link *int
	if err := conn.QueryRow(`SELECT completed_log_id FROM train_schedule_entries WHERE id=1`).Scan(&link); err != nil || link != nil {
		t.Fatal("FK log deletion", link, err)
	}
	exec(`UPDATE train_schedule_entries SET cancelled_at=CURRENT_TIMESTAMP,cancelled_by=7 WHERE id=2`)
	exec(`DELETE FROM users WHERE id=7`)
	var creator, canceller *int
	if err := conn.QueryRow(`SELECT created_by,cancelled_by FROM train_schedule_entries WHERE id=2`).Scan(&creator, &canceller); err != nil || creator != nil || canceller != nil {
		t.Fatal("user FK", err)
	}
	exec(`DELETE FROM members WHERE id IN (1,2)`)
	var cid, vid *int
	var name string
	if err := conn.QueryRow(`SELECT conductor_id,vip_id,conductor_name FROM train_schedule_entries WHERE id=1`).Scan(&cid, &vid, &name); err != nil || cid != nil || vid != nil || name != "Alice" {
		t.Fatal("identity FK", err)
	}
	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='train_schedules'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("obsolete table resurrected")
	}
	if err := goose.Down(conn, "migrations"); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='train_schedule_entries'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("Down kept new table")
	}
	if err := goose.UpTo(conn, "migrations", 86); err != nil {
		t.Fatal(err)
	}
}

func TestTrainScheduleUpdateGate(t *testing.T) {
	f := setupMobileTestDB(t)
	s := makePlan(t, planBody(f.other, gameDate()))
	cookie := sessionCookie(t, 1, "")
	previous := updateGated.Load()
	updateGated.Store(true)
	defer updateGated.Store(previous)
	for _, route := range []struct{ method, path string }{{"POST", "/api/train-schedule"}, {"PUT", schedulePath(s)}, {"POST", schedulePath(s) + "/cancel"}, {"POST", schedulePath(s) + "/complete"}} {
		req := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		updateGateMiddleware(buildRouter()).ServeHTTP(w, req)
		if w.Code != 503 {
			t.Errorf("gate %v = %d", route, w.Code)
		}
	}
	if countRows(t, `SELECT COUNT(*) FROM train_logs`) != 0 || storedPlan(t, s.ID).Status != "planned" {
		t.Fatal("writes bypassed gate")
	}
}

func TestTrainScheduleTemplatePermissions(t *testing.T) {
	for _, p := range []RankPermissions{{ViewTrain: true}, {ViewTrainSchedule: true}, {ManageTrainSchedule: true}} {
		tmpl, err := parseTemplates("templates/layout.html", "templates/train.html")
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, "layout.html", PageData{IsAuthenticated: true, Permissions: p}); err != nil {
			t.Fatal(err)
		}
		html := buf.String()
		want := p.ViewTrainSchedule || p.ManageTrainSchedule
		if strings.Contains(html, `data-tab="upcoming"`) != want || strings.Contains(html, `id="schedule-new"`) != p.ManageTrainSchedule {
			t.Fatal("tab permissions")
		}
	}
}

func TestTrainScheduleClearsDeletedUserAndGuest(t *testing.T) {
	f := setupMobileTestDB(t)
	b := planBody(f.other, gameDate())
	b["vip_id"] = f.r3Member
	b["vip_type"] = "SPECIAL_GUEST"
	w := scheduleCall(t, "POST", "/api/train-schedule", b, 2)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var s TrainScheduleEntry
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if w := scheduleCall(t, "POST", schedulePath(s)+"/cancel", nil, 2); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if _, err := db.Exec(`UPDATE users SET is_active=0 WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if w := scheduleCall(t, "DELETE", "/api/admin/users/2", nil, 1); w.Code != 200 && w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	got := storedPlan(t, s.ID)
	if got.CreatedBy != nil || got.CancelledBy != nil || got.Status != "cancelled" {
		t.Fatal("deleted user references", got)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := deleteMemberTx(tx, f.r3Member); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got = storedPlan(t, s.ID)
	if got.VIPID != nil || got.VIPName != "Member Three" || got.VIPType == nil {
		t.Fatal("deleted guest lost", got)
	}
}
