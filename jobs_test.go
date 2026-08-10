package main

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func setupJobsTestDB(t *testing.T) {
	t.Helper()
	prev := db
	t.Setenv("DATABASE_PATH", filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("STORAGE_PATH", t.TempDir())
	t.Setenv("SESSION_KEY", "test-session-key-at-least-32-chars-long")
	if err := initDB(); err != nil {
		t.Fatalf("initDB: %v", err)
	}
	t.Cleanup(func() {
		if db != nil {
			db.Close()
		}
		db = prev
	})
}

// fakeRunner is a jobRunner with no network and controllable behaviour.
type fakeRunner struct {
	items      []jobItem
	planErr    error
	stepErrOn  int // Seq that returns an error; -1 for none
	block      chan struct{}
	mu         sync.Mutex
	stepped    []string
	finishCall int
	finishCtr  map[string]int
}

func (f *fakeRunner) Plan(ctx context.Context) ([]jobItem, error) {
	return f.items, f.planErr
}

func (f *fakeRunner) Step(ctx context.Context, it jobItem) (jobStep, error) {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return jobStep{}, ctx.Err()
		}
	}
	f.mu.Lock()
	f.stepped = append(f.stepped, it.Label)
	f.mu.Unlock()
	if it.Seq == f.stepErrOn {
		return jobStep{}, errors.New("boom")
	}
	return jobStep{State: "done", Detail: "ok", Counters: map[string]int{"widgets": 1}}, nil
}

func (f *fakeRunner) Finish(ctx context.Context, counters map[string]int, processed int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finishCall++
	f.finishCtr = counters
}

func (f *fakeRunner) steps() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.stepped...)
}

// registerFake registers a throwaway kind and removes it on cleanup so the global
// registry doesn't leak between tests.
func registerFake(t *testing.T, kind string, r jobRunner) {
	t.Helper()
	registerJobKind(kind, jobKind{
		Permission: "manage_members",
		Label:      "Test job",
		New:        func(a jobActor) jobRunner { return r },
	})
	t.Cleanup(func() { delete(jobRegistry, kind) })
}

// waitForJob polls until the job reaches a terminal status.
func waitForJob(t *testing.T, kind string) *BackgroundJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := loadLatestJob(kind)
		if err != nil {
			t.Fatalf("loadLatestJob: %v", err)
		}
		if job != nil && job.Status != "running" {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish within 5s")
	return nil
}

func TestJobRunsEveryItemAndAggregatesCounters(t *testing.T) {
	setupJobsTestDB(t)
	f := &fakeRunner{stepErrOn: -1, items: []jobItem{
		{Seq: 0, Label: "alpha", RefID: 1},
		{Seq: 1, Label: "bravo", RefID: 2},
		{Seq: 2, Label: "charlie", RefID: 3},
	}}
	registerFake(t, "test_basic", f)

	if _, err := startJob("test_basic", jobActor{UserID: 1, Username: "tester"}); err != nil {
		t.Fatalf("startJob: %v", err)
	}
	job := waitForJob(t, "test_basic")

	if job.Status != "done" {
		t.Errorf("status = %q, want done", job.Status)
	}
	if job.Total != 3 || job.Processed != 3 {
		t.Errorf("total/processed = %d/%d, want 3/3", job.Total, job.Processed)
	}
	if got := f.steps(); len(got) != 3 || got[0] != "alpha" || got[2] != "charlie" {
		t.Errorf("steps ran %v, want alpha,bravo,charlie in order", got)
	}
	if f.finishCall != 1 {
		t.Errorf("Finish called %d times, want 1", f.finishCall)
	}
	if f.finishCtr["widgets"] != 3 {
		t.Errorf("counters[widgets] = %d, want 3", f.finishCtr["widgets"])
	}
	if len(job.Items) != 3 || job.Items[0].State != "done" || job.Items[0].Detail != "ok" {
		t.Errorf("items not recorded as done: %+v", job.Items)
	}
}

// One failing item must not sink the run — every flow behaved this way as a
// browser loop, and a single unreachable player shouldn't abandon 99 others.
func TestJobStepErrorDoesNotAbortRun(t *testing.T) {
	setupJobsTestDB(t)
	f := &fakeRunner{stepErrOn: 1, items: []jobItem{
		{Seq: 0, Label: "alpha"}, {Seq: 1, Label: "bravo"}, {Seq: 2, Label: "charlie"},
	}}
	registerFake(t, "test_err", f)

	if _, err := startJob("test_err", jobActor{UserID: 1, Username: "tester"}); err != nil {
		t.Fatalf("startJob: %v", err)
	}
	job := waitForJob(t, "test_err")

	if job.Status != "done" || job.Processed != 3 {
		t.Fatalf("status=%q processed=%d, want done/3", job.Status, job.Processed)
	}
	if job.Items[1].State != "err" {
		t.Errorf("item 1 state = %q, want err", job.Items[1].State)
	}
	if job.Items[2].State != "done" {
		t.Errorf("item 2 state = %q — the run stopped early", job.Items[2].State)
	}
}

// Nothing due is a clean success with no activity row: on a 6h scheduler three
// ticks in four legitimately have no work, and logging each would bury the runs
// that mattered.
func TestJobWithNoItemsSucceedsAndSkipsFinish(t *testing.T) {
	setupJobsTestDB(t)
	f := &fakeRunner{stepErrOn: -1}
	registerFake(t, "test_empty", f)

	if _, err := startJob("test_empty", jobActor{UserID: 1, Username: "tester"}); err != nil {
		t.Fatalf("startJob: %v", err)
	}
	job := waitForJob(t, "test_empty")

	if job.Status != "done" || job.Total != 0 {
		t.Errorf("status=%q total=%d, want done/0", job.Status, job.Total)
	}
	if f.finishCall != 0 {
		t.Error("Finish was called for a job with no items — that writes an empty activity row")
	}
}

// The slot is process-wide: every flow shares one upstream rate limiter, so a
// second concurrent run would just halve both rates while doubling DB contention.
func TestJobSlotIsSingleOccupancy(t *testing.T) {
	setupJobsTestDB(t)
	release := make(chan struct{})
	f := &fakeRunner{stepErrOn: -1, block: release, items: []jobItem{{Seq: 0, Label: "alpha"}}}
	registerFake(t, "test_busy", f)

	if _, err := startJob("test_busy", jobActor{UserID: 1, Username: "tester"}); err != nil {
		t.Fatalf("first startJob: %v", err)
	}
	defer close(release)

	if _, err := startJob("test_busy", jobActor{UserID: 1, Username: "tester"}); !errors.Is(err, errJobBusy) {
		t.Fatalf("second startJob err = %v, want errJobBusy", err)
	}
	kind, _, ok := currentJobInfo()
	if !ok || kind != "test_busy" {
		t.Errorf("currentJobInfo = %q/%v, want test_busy/true", kind, ok)
	}
}

// THE deadlock guard. database.go sets SetMaxOpenConns(1): if the runner held a
// cursor or an open transaction while a Step ran, every other query in the process
// would block forever waiting for a connection that can never be freed — silently,
// with no error and no log. That is the single worst failure mode in this design.
//
// Here a Step blocks (standing in for its upstream HTTP call) while the test issues
// an ordinary query. If the runner ever regresses to holding the connection across
// Step, this test hangs instead of passing, and `go test -timeout` reports it.
func TestRunnerDoesNotHoldTheConnectionDuringStep(t *testing.T) {
	setupJobsTestDB(t)
	release := make(chan struct{})
	f := &fakeRunner{stepErrOn: -1, block: release, items: []jobItem{{Seq: 0, Label: "alpha"}}}
	registerFake(t, "test_deadlock", f)

	if _, err := startJob("test_deadlock", jobActor{UserID: 1, Username: "tester"}); err != nil {
		t.Fatalf("startJob: %v", err)
	}
	defer close(release)

	// Wait until the Step is actually in flight, so we're querying mid-run.
	deadline := time.Now().Add(2 * time.Second)
	for len(f.steps()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	queried := make(chan error, 1)
	go func() {
		var n int
		queried <- db.QueryRow(`SELECT COUNT(*) FROM members`).Scan(&n)
	}()
	select {
	case err := <-queried:
		if err != nil {
			t.Fatalf("query during a running step failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a query blocked while a job step was running — the runner is holding the single DB connection across Step")
	}
}

// A row still marked 'running' after a restart belongs to a dead process.
func TestReconcileMarksRunningJobsInterrupted(t *testing.T) {
	setupJobsTestDB(t)
	if _, err := db.Exec(`INSERT INTO background_jobs (kind, status, trigger_source) VALUES ('test_stale', 'running', 'manual')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	reconcileInterruptedJobs()

	job, err := loadLatestJob("test_stale")
	if err != nil || job == nil {
		t.Fatalf("loadLatestJob: %v", err)
	}
	if job.Status != "interrupted" {
		t.Errorf("status = %q, want interrupted", job.Status)
	}
}

// Retention must delete item rows explicitly: foreign_keys is off app-wide, so the
// ON DELETE CASCADE in the schema never fires and items would be orphaned forever.
func TestPruneOldJobsDeletesItemsNotJustJobs(t *testing.T) {
	setupJobsTestDB(t)
	for i := 0; i < jobsRetainPerKind+3; i++ {
		res, err := db.Exec(`INSERT INTO background_jobs (kind, status, trigger_source) VALUES ('test_prune', 'done', 'manual')`)
		if err != nil {
			t.Fatalf("seed job: %v", err)
		}
		id, _ := res.LastInsertId()
		if _, err := db.Exec(`INSERT INTO background_job_items (job_id, seq, label) VALUES (?, 0, 'x')`, id); err != nil {
			t.Fatalf("seed item: %v", err)
		}
	}
	pruneOldJobs("test_prune")

	var jobs, items int
	db.QueryRow(`SELECT COUNT(*) FROM background_jobs WHERE kind = 'test_prune'`).Scan(&jobs)
	db.QueryRow(`SELECT COUNT(*) FROM background_job_items`).Scan(&items)
	if jobs != jobsRetainPerKind {
		t.Errorf("kept %d jobs, want %d", jobs, jobsRetainPerKind)
	}
	if items != jobsRetainPerKind {
		t.Errorf("kept %d items, want %d — orphaned item rows leak forever", items, jobsRetainPerKind)
	}
}

// The scheduler has no session, so its activity rows must record NULL — not a
// sentinel 0. activity_log.user_id references users(id), where 0 never exists, so
// a sentinel is a dangling reference that any future join or FK enforcement trips
// over. Caught in live testing: the mapping was documented but not implemented.
func TestLogActivityWritesNullForNonUserActor(t *testing.T) {
	setupJobsTestDB(t)

	logActivity(0, "System (scheduled)", "imported", "lastrank_sync", "Alliance pull", false, "detail")
	var uid sql.NullInt64
	var username string
	if err := db.QueryRow(`SELECT user_id, username FROM activity_log ORDER BY id DESC LIMIT 1`).
		Scan(&uid, &username); err != nil {
		t.Fatalf("read: %v", err)
	}
	if uid.Valid {
		t.Errorf("user_id = %d, want NULL — a sentinel is a dangling users(id) reference", uid.Int64)
	}
	if username != "System (scheduled)" {
		t.Errorf("username = %q, want the actor name to survive", username)
	}

	// A real user still records their id.
	if _, err := db.Exec(`INSERT INTO users (id, username, password, is_admin) VALUES (7, 'real', 'x', 0)`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	logActivity(7, "real", "updated", "member", "Someone", false)
	if err := db.QueryRow(`SELECT user_id FROM activity_log ORDER BY id DESC LIMIT 1`).Scan(&uid); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !uid.Valid || uid.Int64 != 7 {
		t.Errorf("user_id = %v, want 7", uid)
	}
}
