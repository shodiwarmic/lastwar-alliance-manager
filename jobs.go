package main

// jobs.go — the generic background job runner.
//
// Four bulk flows (LastRank extended sync, NAP member counts, external-alliance
// gather, prospect refresh) used to run as browser-driven loops: one request per
// second for as long as the run took, with the tab held open. Closing the tab
// stopped the run. They now run here, server-side, with progress persisted so any
// browser can watch — including runs the scheduler starts with no browser at all.
//
// ── THE ONE RULE ────────────────────────────────────────────────────────────────
// database.go sets db.SetMaxOpenConns(1). A Step that holds a rows cursor or an
// open transaction across its upstream HTTP call waits forever for a connection
// that can never be freed, and it hangs the WHOLE PROCESS — silently. No error, no
// log, no panic. Every Step must be shaped:
//
//	read what you need (short query, cursor closed)
//	  → upstream fetch (NO db handle held, no open tx)
//	    → db.Begin() → write → Commit()
//
// The runner holds no DB handle while Step runs, and never queries between
// acquiring a cursor and closing it. Keep it that way.
//
// Concurrency is deliberately one-job-at-a-time, process-wide: every flow shares
// the same 1 req/sec upstream limiter, so running two concurrently just halves
// each one's rate while doubling DB contention on that single connection.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Job kinds. These strings are the API contract with the frontend and the
// scheduler; changing one orphans in-flight rows and any bookmarked progress poll.
const (
	JobLastRankExtended  = "lastrank_extended"
	JobNAPMembers        = "nap_members"
	JobExtAllianceGather = "ext_alliance_gather"
	JobProspectRefresh   = "prospect_refresh"
)

// jobsRetainPerKind caps how much history each kind keeps. Pruned at job start —
// the FK's ON DELETE CASCADE never fires (foreign_keys is off app-wide), so items
// are deleted explicitly.
const jobsRetainPerKind = 10

var errJobBusy = errors.New("another job is already running")

// jobItem is one unit of work. Seq is the display order and the item's key within
// the job; RefID is the domain id (member, alliance, prospect) for the UI to link.
type jobItem struct {
	Seq   int
	Label string
	RefID int
}

// jobStep is the outcome of processing one item.
//
// State is one of "done", "skip" or "err". A Step returning an error is recorded
// as "err" and the run CONTINUES — one member failing must never sink a
// hundred-item sweep, which is how all four flows behaved as browser loops.
type jobStep struct {
	State    string
	Detail   string
	Counters map[string]int
}

// jobRunner is what each flow implements.
//
// Plan must fully drain any cursor it opens before returning — the runner writes
// to the database immediately afterwards. Returning zero items is a SUCCESS, not
// an error: it means nothing was due, which is the normal case for three of every
// four scheduler ticks.
type jobRunner interface {
	Plan(ctx context.Context) ([]jobItem, error)
	Step(ctx context.Context, it jobItem) (jobStep, error)
	// Finish writes the single summary activity row for the run. Skipped entirely
	// when no items were processed, so idle scheduler ticks leave no audit noise.
	Finish(ctx context.Context, counters map[string]int, processed int)
}

// jobActor is who a run is attributed to. UserID 0 + Scheduled means the
// scheduler: logActivity maps a non-positive id to NULL rather than a dangling
// users(id) reference.
type jobActor struct {
	UserID    int
	Username  string
	Scheduled bool
}

type jobKind struct {
	Permission string
	Label      string
	New        func(actor jobActor) jobRunner
	// Allow overrides Permission when the flow's gate can't be written as one
	// permission key. Some HTTP surfaces gate on a DISJUNCTION — the external-alliance
	// registry is writable by manage_allies OR manage_vs_points — and a single string
	// silently can't express that: userHasPermission resolves an unknown key to false,
	// so the button renders and the start 403s. Set this to the same predicate the
	// handler and template use, and the three gates cannot drift apart.
	Allow func(user *AuthUser) bool
}

// jobRegistry is populated by each flow's init(). Keeping registration next to the
// implementation means adding a flow touches one file, not this one.
var jobRegistry = map[string]jobKind{}

func registerJobKind(kind string, def jobKind) { jobRegistry[kind] = def }

// ── Single-slot scheduling ──────────────────────────────────────────────────────

type runningJob struct {
	ID        int64
	Kind      string
	StartedAt time.Time
	cancel    context.CancelFunc
	cancelled bool // user-requested, as opposed to shutdown
}

var (
	jobMu      sync.Mutex
	jobCurrent *runningJob
	jobWG      sync.WaitGroup
)

// currentJobInfo reports the running job, if any, for the busy message.
func currentJobInfo() (kind string, since time.Duration, ok bool) {
	jobMu.Lock()
	defer jobMu.Unlock()
	if jobCurrent == nil {
		return "", 0, false
	}
	return jobCurrent.Kind, time.Since(jobCurrent.StartedAt), true
}

// cancelJob asks the running job to stop after its current item. Returns false if
// that id isn't the running job (already finished, or never started).
func cancelJob(id int64) bool {
	jobMu.Lock()
	defer jobMu.Unlock()
	if jobCurrent == nil || jobCurrent.ID != id {
		return false
	}
	jobCurrent.cancelled = true
	jobCurrent.cancel()
	return true
}

// startJob claims the single slot and launches the run in the background.
// Returns errJobBusy if a job is already running.
func startJob(kind string, actor jobActor) (int64, error) {
	def, ok := jobRegistry[kind]
	if !ok {
		return 0, fmt.Errorf("unknown job kind %q", kind)
	}

	jobMu.Lock()
	if jobCurrent != nil {
		jobMu.Unlock()
		return 0, errJobBusy
	}

	trigger := "manual"
	if actor.Scheduled {
		trigger = "scheduled"
	}
	id, err := insertJobRow(kind, trigger, actor)
	if err != nil {
		jobMu.Unlock()
		return 0, err
	}

	// Not r.Context(): the run outlives the HTTP request that started it. That is
	// the whole point — the officer closes the tab and the sweep keeps going.
	ctx, cancel := context.WithCancel(context.Background())
	jobCurrent = &runningJob{ID: id, Kind: kind, StartedAt: time.Now(), cancel: cancel}
	jobMu.Unlock()

	jobWG.Add(1)
	go func() {
		defer jobWG.Done()
		defer cancel()
		runJob(ctx, id, kind, def.New(actor))

		jobMu.Lock()
		jobCurrent = nil
		jobMu.Unlock()
	}()

	return id, nil
}

// runJob drives one run to completion. Every database touch here happens between
// Steps, never during one — see the file header.
func runJob(ctx context.Context, id int64, kind string, r jobRunner) {
	items, err := r.Plan(ctx)
	if err != nil {
		slog.Error("job plan failed", "kind", kind, "job_id", id, "error", err)
		finishJobRow(id, "failed", "Could not build the work list", nil, 0)
		return
	}

	// Nothing due is a clean success. No items, no activity row — on a 6h
	// scheduler three ticks in four legitimately have nothing to do, and logging
	// each would bury the runs that mattered.
	if len(items) == 0 {
		finishJobRow(id, "done", "", nil, 0)
		return
	}

	if err := insertJobItems(id, items); err != nil {
		slog.Error("job item insert failed", "kind", kind, "job_id", id, "error", err)
		finishJobRow(id, "failed", "Could not record the work list", nil, 0)
		return
	}

	counters := map[string]int{}
	processed := 0
	for _, it := range items {
		if ctx.Err() != nil {
			jobMu.Lock()
			userCancelled := jobCurrent != nil && jobCurrent.ID == id && jobCurrent.cancelled
			jobMu.Unlock()
			status := "interrupted"
			if userCancelled {
				status = "cancelled"
			}
			finishJobRow(id, status, "", counters, processed)
			return
		}

		setItemState(id, it.Seq, "active", "")

		res, stepErr := r.Step(ctx, it)
		if stepErr != nil {
			// Logged server-side with the real cause; the UI gets a generic label.
			slog.Error("job step failed", "kind", kind, "job_id", id, "item", it.Label, "error", stepErr)
			res = jobStep{State: "err", Detail: "error — skipped"}
		}
		if res.State == "" {
			res.State = "done"
		}

		processed++
		setItemState(id, it.Seq, res.State, res.Detail)
		setJobProcessed(id, processed)
		for k, v := range res.Counters {
			counters[k] += v
		}
	}

	r.Finish(ctx, counters, processed)
	finishJobRow(id, "done", "", counters, processed)
}

// ── Persistence ─────────────────────────────────────────────────────────────────

func insertJobRow(kind, trigger string, actor jobActor) (int64, error) {
	pruneOldJobs(kind)

	var userID any
	if actor.UserID > 0 {
		userID = actor.UserID
	}
	res, err := db.Exec(`
		INSERT INTO background_jobs (kind, status, trigger_source, started_by_user_id, started_by_username)
		VALUES (?, 'running', ?, ?, ?)`, kind, trigger, userID, actor.Username)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// pruneOldJobs keeps the newest jobsRetainPerKind runs of a kind. Items go first:
// once the job rows are gone the subquery can no longer find their children, and
// ON DELETE CASCADE does not fire because foreign_keys is off app-wide.
func pruneOldJobs(kind string) {
	const doomed = `SELECT id FROM background_jobs WHERE kind = ? ORDER BY started_at DESC, id DESC LIMIT -1 OFFSET ?`
	if _, err := db.Exec(`DELETE FROM background_job_items WHERE job_id IN (`+doomed+`)`, kind, jobsRetainPerKind); err != nil {
		slog.Error("job item prune failed", "kind", kind, "error", err)
		return
	}
	if _, err := db.Exec(`DELETE FROM background_jobs WHERE id IN (`+doomed+`)`, kind, jobsRetainPerKind); err != nil {
		slog.Error("job prune failed", "kind", kind, "error", err)
	}
}

func insertJobItems(jobID int64, items []jobItem) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO background_job_items (job_id, seq, label, ref_id, state) VALUES (?, ?, ?, ?, 'queued')`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, it := range items {
		if _, err := stmt.Exec(jobID, it.Seq, it.Label, it.RefID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE background_jobs SET total = ? WHERE id = ?`, len(items), jobID); err != nil {
		return err
	}
	return tx.Commit()
}

func setItemState(jobID int64, seq int, state, detail string) {
	if _, err := db.Exec(`UPDATE background_job_items SET state = ?, detail = ? WHERE job_id = ? AND seq = ?`,
		state, detail, jobID, seq); err != nil {
		slog.Error("job item update failed", "job_id", jobID, "seq", seq, "error", err)
	}
}

func setJobProcessed(jobID int64, processed int) {
	if _, err := db.Exec(`UPDATE background_jobs SET processed = ? WHERE id = ?`, processed, jobID); err != nil {
		slog.Error("job progress update failed", "job_id", jobID, "error", err)
	}
}

// finishJobRow closes out a run. finished_at is computed in SQL — binding a Go
// time.Time would write a local-zone string that sorts against CURRENT_TIMESTAMP's
// UTC form lexically, which is the trap documented in CLAUDE.md → Timestamps.
func finishJobRow(jobID int64, status, errMsg string, counters map[string]int, processed int) {
	blob := "{}"
	if len(counters) > 0 {
		if b, err := json.Marshal(counters); err == nil {
			blob = string(b)
		}
	}
	if _, err := db.Exec(`
		UPDATE background_jobs
		SET status = ?, error = ?, counters = ?, processed = ?, finished_at = CURRENT_TIMESTAMP
		WHERE id = ?`, status, errMsg, blob, processed, jobID); err != nil {
		slog.Error("job finish update failed", "job_id", jobID, "error", err)
	}
}

// ── Lifecycle ───────────────────────────────────────────────────────────────────

// reconcileInterruptedJobs runs once at boot. A row still marked 'running' cannot
// be real — the only process that could have been running it just died. No resume
// machinery is needed: every flow plans oldest-touched-first, so re-running IS
// resuming.
func reconcileInterruptedJobs() {
	res, err := db.Exec(`
		UPDATE background_jobs
		SET status = 'interrupted', finished_at = CURRENT_TIMESTAMP,
		    error = 'interrupted by a restart'
		WHERE status = 'running'`)
	if err != nil {
		slog.Error("job reconcile failed", "error", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		slog.Info("marked interrupted jobs from a previous run", "count", n)
	}
}

// drainJobs cancels the running job and waits, bounded, for it to notice.
//
// The wait is bounded because an in-flight enrich has its own 25s ceiling and we
// cannot always outlast it. reconcileInterruptedJobs is the backstop: whatever is
// still running when the process exits gets marked interrupted on the next boot.
func drainJobs(timeout time.Duration) {
	jobMu.Lock()
	if jobCurrent != nil {
		slog.Info("cancelling running job for shutdown", "kind", jobCurrent.Kind, "job_id", jobCurrent.ID)
		jobCurrent.cancel()
	}
	jobMu.Unlock()

	done := make(chan struct{})
	go func() { jobWG.Wait(); close(done) }()
	select {
	case <-done:
		slog.Info("background jobs drained")
	case <-time.After(timeout):
		slog.Warn("background job drain timed out; it will be marked interrupted on next boot")
	}
}

// ── Reads (for the progress poll) ───────────────────────────────────────────────

// loadLatestJob returns the most recent job of a kind with its items, or nil.
//
// Read-all-into-memory shape: the items cursor is fully drained and closed before
// anything else touches the database.
func loadLatestJob(kind string) (*BackgroundJob, error) {
	var j BackgroundJob
	var finishedAt sql.NullString
	var username sql.NullString
	err := db.QueryRow(`
		SELECT id, kind, status, trigger_source, COALESCE(started_by_username, ''),
		       total, processed, counters, error, started_at, finished_at
		FROM background_jobs WHERE kind = ? ORDER BY started_at DESC, id DESC LIMIT 1`, kind).
		Scan(&j.ID, &j.Kind, &j.Status, &j.Trigger, &username, &j.Total, &j.Processed,
			&j.Counters, &j.Error, &j.StartedAt, &finishedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	j.StartedBy = username.String
	j.FinishedAt = finishedAt.String

	rows, err := db.Query(`
		SELECT seq, label, ref_id, state, detail
		FROM background_job_items WHERE job_id = ? ORDER BY seq ASC`, j.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it BackgroundJobItem
		if err := rows.Scan(&it.Seq, &it.Label, &it.RefID, &it.State, &it.Detail); err != nil {
			return nil, err
		}
		j.Items = append(j.Items, it)
	}
	return &j, rows.Err()
}
