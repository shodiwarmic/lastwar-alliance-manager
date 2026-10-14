package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// setupUpdateTest gives a migrated database, an admin, and an empty spool beside the database,
// with the gate's global state reset before and after.
func setupUpdateTest(t *testing.T) string {
	t.Helper()
	setupPreviewTestDB(t)
	resetGate := func() {
		updateGateMu.Lock()
		updateGated.Store(false)
		updateInflight.Store(0)
		updateGateID, updateAckedID = "", ""
		updateGateMu.Unlock()
	}
	resetGate()
	t.Cleanup(resetGate)
	dir := updateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func spoolWrite(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// agentRunning makes the spool say the agent is working on id, with a lease updated `ago`.
func agentRunning(t *testing.T, dir, id, kind string, ago time.Duration) {
	t.Helper()
	spoolWrite(t, dir, "running", "alliance-manager update request 1\nid="+id+"\ntarget=alliance-manager v9.9.10\n")
	spoolWrite(t, dir, "status", "alliance-manager update status 1\nid="+id+"\nkind="+kind+"\nstate=claimed\nupdated_at="+
		strconv.FormatInt(time.Now().Add(-ago).Unix(), 10)+"\n")
}

func readAck(dir string) string {
	f, err := readSpool("gate-ack")
	if err != nil {
		return ""
	}
	return f.get("id")
}

func TestTheGateHoldsOffWritesAndAcknowledges(t *testing.T) {
	dir := setupUpdateTest(t)
	agentRunning(t, dir, "g1", "install", 0)
	updateGateTick()
	if !updateGateActive() {
		t.Fatal("a fresh lease should gate")
	}
	if got := readAck(dir); got != "g1" {
		t.Fatalf("gate-ack names %q, want g1", got)
	}
	h := updateGateMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "served") }))
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{"POST", "/api/members", http.StatusServiceUnavailable},
		{"PUT", "/api/members/1", http.StatusServiceUnavailable},
		{"POST", "/wopi/files/3/contents", http.StatusServiceUnavailable},
		{"POST", "/api/mobile/commit", http.StatusServiceUnavailable},
		{"GET", "/api/members", http.StatusOK},
		{"POST", "/api/login", http.StatusOK},
		{"POST", "/api/logout", http.StatusOK},
		{"GET", "/api/health", http.StatusOK},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != c.want {
			t.Errorf("%s %s: %d, want %d", c.method, c.path, rec.Code, c.want)
		}
		if c.want == http.StatusServiceUnavailable && !strings.Contains(rec.Body.String(), "update in progress") {
			t.Errorf("%s %s: body %q", c.method, c.path, rec.Body.String())
		}
	}
	// The run ends: running and status go, the gate lifts.
	os.Remove(filepath.Join(dir, "running"))
	os.Remove(filepath.Join(dir, "status"))
	updateGateTick()
	if updateGateActive() {
		t.Error("the gate should lift once running is gone")
	}
}

func TestANoopOrAMismatchedStatusDoesNotGate(t *testing.T) {
	dir := setupUpdateTest(t)
	agentRunning(t, dir, "n1", "noop", 0)
	updateGateTick()
	if updateGateActive() {
		t.Error("a noop raised the barrier")
	}
	agentRunning(t, dir, "m1", "install", 0)
	spoolWrite(t, dir, "status", "alliance-manager update status 1\nid=other\nkind=install\nupdated_at="+strconv.FormatInt(time.Now().Unix(), 10)+"\n")
	updateGateTick()
	if updateGateActive() {
		t.Error("a status naming another request gated")
	}
}

func TestALapsedLeaseWritesGateReleasedBeforeOpening(t *testing.T) {
	dir := setupUpdateTest(t)
	agentRunning(t, dir, "l1", "install", 0)
	updateGateTick()
	agentRunning(t, dir, "l1", "install", 3*time.Minute)

	// The marker cannot be written: the gate stays shut.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	updateGateTick()
	if !updateGateActive() {
		os.Chmod(dir, 0o755)
		t.Fatal("the gate opened without its gate-released marker")
	}
	os.Chmod(dir, 0o755)
	updateGateTick()
	if updateGateActive() {
		t.Fatal("the gate should open once the marker is written")
	}
	f, err := readSpool("gate-released")
	if err != nil || f.get("id") != "l1" {
		t.Errorf("gate-released = %+v, %v", f, err)
	}
}

func TestBootWithAStaleRunningWritesTheMarkerFirst(t *testing.T) {
	dir := setupUpdateTest(t)
	agentRunning(t, dir, "b1", "install", 10*time.Minute)
	initUpdateGate()
	if updateGateActive() {
		t.Error("a stale lease gated at boot")
	}
	if f, err := readSpool("gate-released"); err != nil || f.get("id") != "b1" {
		t.Errorf("no gate-released for b1 at boot: %v", err)
	}
}

func TestTheAcknowledgementWaitsForInFlightWritesAndJobs(t *testing.T) {
	dir := setupUpdateTest(t)
	prev := updateBarrierLimit
	updateBarrierLimit = 3 * time.Second
	t.Cleanup(func() { updateBarrierLimit = prev })

	// A mutating request parked inside its handler.
	release := make(chan struct{})
	entered := make(chan struct{})
	h := updateGateMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
	}))
	go h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/members", nil))
	<-entered

	// A job parked inside Step, before its commit.
	block := make(chan struct{})
	r := &fakeRunner{items: []jobItem{{Seq: 1, Label: "a"}}, stepErrOn: -1, block: block}
	registerFake(t, "gate_job", r)
	if _, err := startJob("gate_job", jobActor{Username: "t"}); err != nil {
		t.Fatal(err)
	}

	agentRunning(t, dir, "w1", "install", 0)
	done := make(chan struct{})
	go func() { updateGateTick(); close(done) }()
	time.Sleep(300 * time.Millisecond)
	if readAck(dir) != "" {
		t.Fatal("acknowledged with a request and a job still running")
	}
	// The job is cancelled by the barrier (its Step returns on ctx); the request finishes.
	close(release)
	<-done
	if readAck(dir) != "w1" {
		t.Fatalf("no gate-ack once both finished (ack %q)", readAck(dir))
	}
	if _, _, running := currentJobInfo(); running {
		t.Error("the job runner was still alive when the acknowledgement was written")
	}
}

func TestATimedOutBarrierWritesNoAcknowledgement(t *testing.T) {
	dir := setupUpdateTest(t)
	prev := updateBarrierLimit
	updateBarrierLimit = 200 * time.Millisecond
	t.Cleanup(func() { updateBarrierLimit = prev })
	updateInflight.Add(1) // a writer that never finishes
	agentRunning(t, dir, "t1", "install", 0)
	updateGateTick()
	if !updateGateActive() {
		t.Fatal("not gated")
	}
	if readAck(dir) != "" {
		t.Error("acknowledged with a write still in flight")
	}
	updateInflight.Add(-1)
	updateGateTick()
	if readAck(dir) != "t1" {
		t.Error("the next tick should acknowledge once the writer is gone")
	}
}

func TestJobStartsAndBackgroundWritersStandDownUnderTheGate(t *testing.T) {
	setupUpdateTest(t)
	registerFake(t, "gated_job", &fakeRunner{stepErrOn: -1})
	updateGated.Store(true)
	if _, err := startJob("gated_job", jobActor{Username: "t"}); !errors.Is(err, errUpdateInProgress) {
		t.Errorf("startJob under the gate: %v", err)
	}
	t.Setenv("UPDATE_CHECK", "")
	hits := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, fakeReleaseJSON) })
	if err := checkLatestRelease(context.Background()); !errors.Is(err, errUpdateInProgress) {
		t.Errorf("checkLatestRelease under the gate: %v", err)
	}
	if hits.Load() != 0 {
		t.Error("the release check made a request under the gate")
	}
	var before int
	db.QueryRow(`SELECT COUNT(*) FROM login_sessions`).Scan(&before)
	trackLogin(1, "admin", httptest.NewRequest("POST", "/api/login", nil), true)
	time.Sleep(100 * time.Millisecond)
	var after int
	db.QueryRow(`SELECT COUNT(*) FROM login_sessions`).Scan(&after)
	if after != before {
		t.Error("trackLogin wrote login history under the gate")
	}
}

// A drain racing a start never reports the runner finished while it is alive: admission and
// jobWG.Add happen under one hold of jobMu.
func TestADrainRacingAStartNeverMissesTheJob(t *testing.T) {
	setupUpdateTest(t)
	for i := 0; i < 20; i++ {
		block := make(chan struct{})
		kind := "race_job_" + strconv.Itoa(i)
		registerFake(t, kind, &fakeRunner{items: []jobItem{{Seq: 1, Label: "a"}}, stepErrOn: -1, block: block})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); startJob(kind, jobActor{Username: "t"}) }()
		wg.Wait()
		if drainJobs(2 * time.Second) {
			if _, _, running := currentJobInfo(); running {
				t.Fatal("drainJobs reported completion with the job still published")
			}
		}
		close(block)
		drainJobs(2 * time.Second)
	}
}

func TestSchemaAheadOfTheBuildIsReported(t *testing.T) {
	setupUpdateTest(t)
	checkSchemaAhead()
	if msg := schemaAheadMessage(); msg != "" {
		t.Fatalf("a current schema reported as ahead: %s", msg)
	}
	db.Exec(`INSERT INTO goose_db_version (version_id, is_applied) VALUES (999, 1)`)
	checkSchemaAhead()
	if msg := schemaAheadMessage(); !strings.Contains(msg, "migration 999") {
		t.Errorf("message = %q", msg)
	}
}

// ── Results ─────────────────────────────────────────────────────────────────────────────────

func writeResult(t *testing.T, dir, id, outcome string) {
	t.Helper()
	spoolWrite(t, dir, "result-"+id, "alliance-manager update result 1\nid="+id+"\nkind=install\noutcome="+outcome+
		"\nfrom=v9.9.9\nto=v9.9.10\nfinished_at="+strconv.FormatInt(time.Now().Unix(), 10)+"\nmessage=it went fine\n")
}

func updateActivityRows(t *testing.T) []string {
	t.Helper()
	rows, err := db.Query(`SELECT action FROM activity_log WHERE entity_type = 'update' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		rows.Scan(&a)
		out = append(out, a)
	}
	return out
}

func TestAResultIsLoggedExactlyOnce(t *testing.T) {
	dir := setupUpdateTest(t)
	writeResult(t, dir, "r1", "installed")
	if err := consumeUpdateResults(); err != nil {
		t.Fatal(err)
	}
	if !spoolExists("done-r1") || spoolExists("result-r1") {
		t.Fatal("result-r1 was not renamed to done-r1")
	}
	// A crash between the commit and the rename: the same result is seen again.
	writeResult(t, dir, "r1", "installed")
	if err := consumeUpdateResults(); err != nil {
		t.Fatal(err)
	}
	if got := updateActivityRows(t); len(got) != 1 || got[0] != "installed" {
		t.Errorf("activity rows = %v, want one installed", got)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM update_outcomes WHERE request_id = 'r1'`).Scan(&n)
	if n != 1 {
		t.Errorf("update_outcomes rows = %d", n)
	}

	writeResult(t, dir, "r2", "rolled_back")
	writeResult(t, dir, "r3", "ok")
	consumeUpdateResults()
	if got := updateActivityRows(t); len(got) != 2 || got[1] != "failed" {
		t.Errorf("activity rows = %v: a rollback logs failed, a noop nothing", got)
	}
}

func TestAResultThatCannotBeRecordedStaysAndBlocksANewRequest(t *testing.T) {
	dir := setupUpdateTest(t)
	writeResult(t, dir, "x1", "installed")
	db.Exec(`DROP TABLE update_outcomes`)
	if err := consumeUpdateResults(); err == nil {
		t.Fatal("expected an error")
	}
	if !spoolExists("result-x1") {
		t.Fatal("a result that was not recorded was renamed")
	}
	w := serveRouter(t, "POST", "/api/admin/update", `{"version":"noop"}`, sessionCookie(t, 1, ""))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("POST with an unconsumable result: %d %s", w.Code, w.Body.String())
	}
	if spoolExists("request") {
		t.Error("a request was written anyway")
	}
}

// ── Requests ────────────────────────────────────────────────────────────────────────────────

// installableHere makes the install one the Admin page may update: a release build, pinned to
// itself, the default database path, a fresh agent, and a newer release known.
func installableHere(t *testing.T, dir string) {
	t.Helper()
	prevV, prevP := appVersion, defaultDatabasePath
	appVersion = "v9.9.9"
	defaultDatabasePath = databasePath()
	t.Cleanup(func() { appVersion, defaultDatabasePath = prevV, prevP })
	t.Setenv("APP_VERSION", "v9.9.9")
	t.Setenv("UPDATE_CHECK", "")
	spoolWrite(t, dir, "agent", "alliance-manager update agent 1\nrelease=v9.9.9\nprotocol=1\nseen_at="+strconv.FormatInt(time.Now().Unix(), 10)+"\n")
	db.Exec(`UPDATE release_state SET latest_tag = 'v9.9.10'`)
}

func getUpdate(t *testing.T) map[string]any {
	t.Helper()
	w := serveRouter(t, "GET", "/api/admin/update", "", sessionCookie(t, 1, ""))
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

func TestTheInstallRefusalsEachSayWhy(t *testing.T) {
	dir := setupUpdateTest(t)
	installableHere(t, dir)
	if out := getUpdate(t); out["can_install"] != true {
		t.Fatalf("installable precondition: %v", out["install_reason"])
	}
	for _, c := range []struct {
		name  string
		setup func()
		undo  func()
		want  string
	}{
		{"no agent", func() { os.Rename(filepath.Join(dir, "agent"), filepath.Join(dir, "agent.x")) },
			func() { os.Rename(filepath.Join(dir, "agent.x"), filepath.Join(dir, "agent")) }, "no host update agent"},
		{"stale agent", func() {
			spoolWrite(t, dir, "agent", "alliance-manager update agent 1\nrelease=v9.9.9\nprotocol=1\nseen_at="+strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)+"\n")
		}, func() { installableHere(t, dir) }, "last seen"},
		{"newer protocol", func() {
			spoolWrite(t, dir, "agent", "alliance-manager update agent 2\nrelease=v9.9.9\nprotocol=2\nseen_at="+strconv.FormatInt(time.Now().Unix(), 10)+"\n")
		}, func() { installableHere(t, dir) }, "protocol 2"},
		{"needs operator", func() {
			spoolWrite(t, dir, "agent", "alliance-manager update agent 1\nrelease=v9.9.9\nprotocol=1\njournal_phase=needs-operator\nseen_at="+strconv.FormatInt(time.Now().Unix(), 10)+"\n")
		}, func() { installableHere(t, dir) }, "needs the operator"},
		{"in flight", func() { spoolWrite(t, dir, "request", "x") }, func() { os.Remove(filepath.Join(dir, "request")) }, "already in progress"},
		{"unconsumed", func() { db.Exec(`DROP TABLE update_outcomes`); writeResult(t, dir, "u1", "installed") },
			func() {
				os.Remove(filepath.Join(dir, "result-u1"))
				db.Exec(`CREATE TABLE update_outcomes (request_id TEXT PRIMARY KEY, outcome TEXT NOT NULL, logged_at TEXT)`)
			}, "has not been recorded"},
		{"dev build", func() { appVersion = "dev" }, func() { appVersion = "v9.9.9" }, "not a release"},
		{"pin differs", func() { t.Setenv("APP_VERSION", "latest") }, func() { t.Setenv("APP_VERSION", "v9.9.9") }, "APP_VERSION in .env"},
		{"checks off", func() { t.Setenv("UPDATE_CHECK", "off") }, func() { t.Setenv("UPDATE_CHECK", "") }, "UPDATE_CHECK=off"},
		{"newest", func() { db.Exec(`UPDATE release_state SET latest_tag = 'v9.9.9'`) },
			func() { db.Exec(`UPDATE release_state SET latest_tag = 'v9.9.10'`) }, "newest release"},
		{"database path", func() { defaultDatabasePath = "/app/data/alliance.db" },
			func() { defaultDatabasePath = databasePath() }, "DATABASE_PATH is"},
	} {
		c.setup()
		out := getUpdate(t)
		if out["can_install"] != false || !strings.Contains(out["install_reason"].(string), c.want) {
			t.Errorf("%s: can_install=%v reason=%q, want one containing %q", c.name, out["can_install"], out["install_reason"], c.want)
		}
		w := serveRouter(t, "POST", "/api/admin/update", `{"version":"v9.9.10"}`, sessionCookie(t, 1, ""))
		if w.Code != http.StatusConflict && w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: POST = %d", c.name, w.Code)
		}
		c.undo()
	}
	if w := serveRouter(t, "POST", "/api/admin/update", `{"version":"v9.9.8"}`, sessionCookie(t, 1, "")); w.Code != http.StatusConflict {
		t.Errorf("a version other than the latest: %d", w.Code)
	}
}

func TestARequestIsWrittenOnceAndLogged(t *testing.T) {
	dir := setupUpdateTest(t)
	installableHere(t, dir)
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = serveRouter(t, "POST", "/api/admin/update", `{"version":"v9.9.10"}`, sessionCookie(t, 1, "")).Code
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		if c == http.StatusOK {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("codes = %v: exactly one request should be written", codes)
	}
	f, err := readSpool("request")
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != "request" || f.Format != 1 || f.get("target") != "alliance-manager v9.9.10" || f.get("from") != "v9.9.9" ||
		!updateIDPattern.MatchString(f.get("id")) || f.get("requested_by") != "admin" {
		t.Errorf("request = %+v", f)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".request-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
	if got := updateActivityRows(t); len(got) != 1 || got[0] != "requested" {
		t.Errorf("activity = %v", got)
	}
	out := getUpdate(t)
	if out["can_install"] != false {
		t.Error("installable with a request waiting")
	}
}

func TestATestRequestNeedsOnlyAFreshAgent(t *testing.T) {
	dir := setupUpdateTest(t)
	installableHere(t, dir)
	db.Exec(`UPDATE release_state SET latest_tag = 'v9.9.9'`) // nothing newer
	out := getUpdate(t)
	if out["can_test"] != true || out["can_install"] != false {
		t.Fatalf("can_test=%v can_install=%v", out["can_test"], out["can_install"])
	}
	w := serveRouter(t, "POST", "/api/admin/update", `{"version":"noop"}`, sessionCookie(t, 1, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("noop: %d %s", w.Code, w.Body.String())
	}
	f, _ := readSpool("request")
	if f.get("target") != "noop" {
		t.Errorf("target = %q", f.get("target"))
	}
}

func TestGetShowsTheRunningRequestThenTheNewestDone(t *testing.T) {
	dir := setupUpdateTest(t)
	installableHere(t, dir)
	agentRunning(t, dir, "s1", "install", 0)
	spoolWrite(t, dir, "update-s1.log", "line one\nline two\n")
	out := getUpdate(t)
	req, _ := out["request"].(map[string]any)
	if req["active"] != true || req["id"] != "s1" {
		t.Fatalf("request = %v", req)
	}
	if log, _ := out["log"].([]any); len(log) != 2 || log[1] != "line two" {
		t.Errorf("log = %v", out["log"])
	}
	os.Remove(filepath.Join(dir, "running"))
	os.Remove(filepath.Join(dir, "status"))
	writeResult(t, dir, "s1", "installed")
	out = getUpdate(t)
	req, _ = out["request"].(map[string]any)
	if req["active"] != false || req["outcome"] != "installed" || req["id"] != "s1" {
		t.Errorf("after: request = %v", req)
	}
	out = getUpdate(t) // and again, across reloads
	if req, _ := out["request"].(map[string]any); req["outcome"] != "installed" {
		t.Error("the finished request vanished on reload")
	}
}

func TestARefusedUnclaimedRequestIsSurfacedAndCleared(t *testing.T) {
	dir := setupUpdateTest(t)
	spoolWrite(t, dir, "request", "garbage")
	spoolWrite(t, dir, "status", "alliance-manager update status 1\nid=invalid\nkind=install\nstate=refused\nstep=the request carries no valid id\n")
	out := getUpdate(t)
	if out["refused"] != "the request carries no valid id" {
		t.Errorf("refused = %v", out["refused"])
	}
	if spoolExists("request") || spoolExists("status") {
		t.Error("the refused request was not cleared")
	}
}
