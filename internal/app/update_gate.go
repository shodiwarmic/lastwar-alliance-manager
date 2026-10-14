package app

// update_gate.go — the app's side of an update the host update agent is installing
// (scripts/lib/update-agent.sh): the spool it shares with the agent, and the write gate.
//
// ── The gate ─────────────────────────────────────────────────────────────────────────────────
// While the agent works on a request, nothing may change the database: if the update fails and a
// migration has run, the agent restores the pre-update backup, which is only safe when no write
// landed after that backup. So while the spool holds `running` and a `status` naming the same
// request — a lease the agent refreshes every 5 seconds — is under two minutes old, the gate is
// set (a noop request's status says kind=noop and gates nothing):
//
//   - every mutating request answers 503 {"error":"update in progress"}, /wopi/ included, except
//     /api/login (whose login-history goroutine writes nothing while gated) and /api/logout
//     (which only clears a cookie);
//   - job starts are refused, the LastRank scheduler and the release check stand down.
//
// On the transition to gated the app establishes a BARRIER before it says so: running jobs are
// cancelled, and only when every in-flight mutating request has finished and the job runner has
// actually exited (drainJobs) does it write gate-ack naming the request. The agent takes its
// backup only after reading that acknowledgement.
//
// If the lease lapses with `running` still there — a dead agent — the gate cannot hold forever:
// the app first writes and syncs gate-released naming the request, then lifts the gate. The
// marker tells a resumed agent that writes may have been accepted, so it must never restore the
// database for that request. A marker that cannot be written keeps the gate shut; the next tick
// tries again.
//
// The gate is initialised synchronously right after initDB, before anything that writes at boot,
// the scheduler, jobs or the listener — a container started mid-update boots gated.

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// updateProtocol is the spool format this app reads and writes (the agent's PROTOCOL).
const updateProtocol = 1

// The lease and the barrier's bounds; variables so the tests need not wait minutes.
var (
	updateLeaseStale   = 2 * time.Minute
	updateBarrierLimit = 30 * time.Second
	updateGateEvery    = 2 * time.Second
)

var (
	updateGated    atomic.Bool  // the gate itself: read on every mutating request
	updateInflight atomic.Int64 // mutating requests (and login-history writes) in progress
	updateGateMu   sync.Mutex   // serialises transitions
	updateGateID   string       // the request gated on (under updateGateMu)
	updateAckedID  string       // the request whose gate-ack is written (under updateGateMu)
)

var errUpdateInProgress = errors.New("an update is being installed")

var updateIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,40}$`)

// updateDir is the spool: data/update beside the database (the agent's $APP_DIR/data/update).
func updateDir() string { return filepath.Join(filepath.Dir(databasePath()), "update") }

// updateGateActive reports whether writes are being held off for an update.
func updateGateActive() bool { return updateGated.Load() }

// ── Spool files ──────────────────────────────────────────────────────────────────────────────

// spoolFile is one key=value file of the spool, its first line a "alliance-manager update
// <kind> <format>" header.
type spoolFile struct {
	Kind   string
	Format int
	Values map[string]string
}

func (f spoolFile) get(k string) string { return f.Values[k] }

// readSpool reads spool/name: a regular file of at most 8192 bytes. Missing → os.ErrNotExist.
func readSpool(name string) (spoolFile, error) {
	var out spoolFile
	path := filepath.Join(updateDir(), name)
	fi, err := os.Lstat(path)
	if err != nil {
		return out, err
	}
	if !fi.Mode().IsRegular() {
		return out, fmt.Errorf("%s is not a regular file", name)
	}
	fh, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer fh.Close()
	body, err := io.ReadAll(io.LimitReader(fh, 8193))
	if err != nil {
		return out, err
	}
	if len(body) > 8192 {
		return out, fmt.Errorf("%s is larger than 8192 bytes", name)
	}
	return parseSpool(body)
}

func parseSpool(body []byte) (spoolFile, error) {
	out := spoolFile{Values: map[string]string{}}
	sc := bufio.NewScanner(bytes.NewReader(body))
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			f := strings.Fields(line)
			if len(f) != 4 || f[0] != "alliance-manager" || f[1] != "update" {
				return out, errors.New("not a spool file (no header)")
			}
			n, err := strconv.Atoi(f[3])
			if err != nil {
				return out, errors.New("the header carries no format number")
			}
			out.Kind, out.Format = f[2], n
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			if _, seen := out.Values[k]; !seen {
				out.Values[k] = v
			}
		}
	}
	if first {
		return out, errors.New("empty")
	}
	return out, nil
}

// writeSpool writes spool/name through a synced temp file in the spool and a rename.
func writeSpool(name, body string) error {
	dir := updateDir()
	tmp, err := os.CreateTemp(dir, ".app-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

func spoolExists(name string) bool {
	_, err := os.Lstat(filepath.Join(updateDir(), name))
	return err == nil
}

// spoolEpoch reads a seconds-since-epoch value.
func spoolEpoch(v string) (time.Time, bool) {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}, false
	}
	return time.Unix(n, 0), true
}

// leaseState is what the spool says about a request in progress.
type leaseState struct {
	RunningID string    // the id in `running`, "" when nothing runs
	Status    spoolFile // the agent's status, when it names that id
	Fresh     bool      // the status is under updateLeaseStale old
	Noop      bool      // the status says kind=noop
}

func readLease() leaseState {
	var ls leaseState
	run, err := readSpool("running")
	if err != nil {
		return ls
	}
	ls.RunningID = run.get("id")
	if !updateIDPattern.MatchString(ls.RunningID) {
		ls.RunningID = ""
		return ls
	}
	st, err := readSpool("status")
	if err != nil || st.Kind != "status" || st.get("id") != ls.RunningID {
		return ls
	}
	ls.Status = st
	ls.Noop = st.get("kind") == "noop"
	if at, ok := spoolEpoch(st.get("updated_at")); ok {
		ls.Fresh = time.Since(at) < updateLeaseStale
	}
	return ls
}

// ── The gate ─────────────────────────────────────────────────────────────────────────────────

// beginWrite counts a writer in, unless the gate is set: increment, then check, so a barrier
// that sets the gate and then waits for zero can never miss one.
func beginWrite() bool {
	updateInflight.Add(1)
	if updateGated.Load() {
		updateInflight.Add(-1)
		return false
	}
	return true
}

func endWrite() { updateInflight.Add(-1) }

// updateGateMiddleware refuses mutating requests while the gate is set and counts the ones it
// lets through, so the barrier knows when they have finished.
func updateGateMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		switch r.URL.Path {
		case "/api/login", "/api/logout", "/api/health":
			next.ServeHTTP(w, r)
			return
		}
		if !beginWrite() {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusServiceUnavailable)
			writeJSON(w, map[string]string{"error": "update in progress"})
			return
		}
		defer endWrite()
		next.ServeHTTP(w, r)
	})
}

// updateGateTick reads the spool and moves the gate. Called synchronously at boot, then every
// updateGateEvery.
func updateGateTick() {
	updateGateMu.Lock()
	defer updateGateMu.Unlock()
	ls := readLease()
	switch {
	case ls.RunningID != "" && ls.Status.Kind != "" && !ls.Noop && ls.Fresh:
		if !updateGated.Load() || updateGateID != ls.RunningID {
			slog.Info("Update in progress: holding off every change", "request", ls.RunningID)
			updateGateID = ls.RunningID
			updateAckedID = ""
			updateGated.Store(true)
		}
		if updateAckedID != ls.RunningID {
			if err := establishBarrier(ls.RunningID); err != nil {
				slog.Warn("Update barrier not established yet", "request", ls.RunningID, "error", err)
				return
			}
			updateAckedID = ls.RunningID
		}
	case ls.RunningID != "" && ls.Status.Kind != "" && !ls.Noop && !ls.Fresh:
		// The agent stopped refreshing its lease. Revoke the restore first, then open.
		if !releasedFor(ls.RunningID) {
			body := fmt.Sprintf("alliance-manager update gate-released %d\nid=%s\nreleased_at=%d\n", updateProtocol, ls.RunningID, time.Now().Unix())
			if err := writeSpool("gate-released", body); err != nil {
				slog.Error("Update lease lapsed but gate-released could not be written; keeping changes held off", "request", ls.RunningID, "error", err)
				updateGated.Store(true)
				updateGateID = ls.RunningID
				return
			}
		}
		if updateGated.Load() {
			slog.Warn("The host update agent stopped refreshing its lease; accepting changes again", "request", ls.RunningID)
		}
		updateGated.Store(false)
		updateGateID = ""
	default:
		if updateGated.Load() {
			slog.Info("Update finished: accepting changes again", "request", updateGateID)
		}
		updateGated.Store(false)
		updateGateID = ""
	}
}

func releasedFor(id string) bool {
	f, err := readSpool("gate-released")
	return err == nil && f.get("id") == id
}

// establishBarrier stops every writer and says so: cancel the running job, wait — bounded —
// until no mutating request is in flight and the job runner has exited, then write gate-ack.
// A timeout writes nothing; the agent will not proceed without the acknowledgement.
func establishBarrier(id string) error {
	deadline := time.Now().Add(updateBarrierLimit)
	if !drainJobs(time.Until(deadline)) {
		return errors.New("a background job is still running")
	}
	for updateInflight.Load() > 0 {
		if time.Now().After(deadline) {
			return fmt.Errorf("%d change(s) still in flight", updateInflight.Load())
		}
		time.Sleep(20 * time.Millisecond)
	}
	body := fmt.Sprintf("alliance-manager update gate-ack %d\nid=%s\nacked_at=%d\n", updateProtocol, id, time.Now().Unix())
	if err := writeSpool("gate-ack", body); err != nil {
		return err
	}
	slog.Info("Update barrier established", "request", id)
	return nil
}

// initUpdateGate runs right after initDB: the spool directory, the gate's first decision (a
// stale lease's gate-released marker included), the boot-time read of finished results, the
// schema check, then the ticker.
func initUpdateGate() {
	if err := os.MkdirAll(updateDir(), 0o755); err != nil {
		slog.Warn("Update spool unavailable; updates from the Admin page are off", "dir", updateDir(), "error", err)
		return
	}
	updateGateTick()
	if err := consumeUpdateResults(); err != nil {
		slog.Warn("Update results could not be read at boot", "error", err)
	}
	checkSchemaAhead()
	go func() {
		t := time.NewTicker(updateGateEvery)
		defer t.Stop()
		for range t.C {
			updateGateTick()
		}
	}()
}

// ── An older binary on a newer schema ─────────────────────────────────────────────────────────

// schemaAhead is set when the database has migrations this binary does not know — an old image
// started on a database a newer one migrated. Logged at Error at boot and shown on the Admin page.
var schemaAhead atomic.Value // string

func checkSchemaAhead() {
	var dbMax int64
	if err := db.QueryRow(`SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied = 1`).Scan(&dbMax); err != nil {
		return
	}
	files, _ := filepath.Glob(filepath.Join("migrations", "*.sql"))
	var own int64
	for _, f := range files {
		base := filepath.Base(f)
		if i := strings.IndexByte(base, '_'); i > 0 {
			if n, err := strconv.ParseInt(base[:i], 10, 64); err == nil && n > own {
				own = n
			}
		}
	}
	if own > 0 && dbMax > own {
		msg := fmt.Sprintf("the database is at migration %d, newer than this build's %d — this build is older than the data it is running on", dbMax, own)
		slog.Error("Database schema is newer than this build", "database", dbMax, "build", own)
		schemaAhead.Store(msg)
		return
	}
	schemaAhead.Store("")
}

func schemaAheadMessage() string {
	s, _ := schemaAhead.Load().(string)
	return s
}

// ── Results ──────────────────────────────────────────────────────────────────────────────────

var updateResultName = regexp.MustCompile(`^result-([A-Za-z0-9-]{1,40})$`)

// updateRequestMu serialises everything that consumes results or writes a request.
var updateRequestMu sync.Mutex

// consumeUpdateResults turns every result-<id> into one update_outcomes row and, for an install
// or a failure, one activity row — in one transaction, through the error-returning writer — then
// renames it done-<id>. A crash between the commit and the rename renames on the next read
// without a second row (INSERT OR IGNORE affects nothing). The newest five done files are kept.
func consumeUpdateResults() error {
	updateRequestMu.Lock()
	defer updateRequestMu.Unlock()
	return consumeUpdateResultsLocked()
}

func consumeUpdateResultsLocked() error {
	entries, err := os.ReadDir(updateDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var firstErr error
	for _, e := range entries {
		m := updateResultName.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		if err := consumeOneResult(m[1]); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	pruneDoneResults()
	return firstErr
}

func consumeOneResult(id string) error {
	name := "result-" + id
	res, err := readSpool(name)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if res.Kind != "result" {
		return fmt.Errorf("%s is not a result", name)
	}
	if res.Format > updateProtocol {
		return fmt.Errorf("%s is format %d; this app reads format %d — update from a terminal", name, res.Format, updateProtocol)
	}
	outcome := res.get("outcome")
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	r, err := tx.Exec(`INSERT OR IGNORE INTO update_outcomes (request_id, outcome) VALUES (?, ?)`, id, outcome)
	if err != nil {
		tx.Rollback()
		return err
	}
	if n, _ := r.RowsAffected(); n == 1 {
		action := ""
		switch outcome {
		case "installed":
			action = "installed"
		case "ok":
		default:
			action = "failed"
		}
		if action != "" {
			detail := fmt.Sprintf("%s → %s: %s", res.get("from"), res.get("to"), res.get("message"))
			if outcome != "installed" && outcome != "failed" {
				detail = outcome + " — " + detail
			}
			if err := writeActivity(tx, 0, "System (host update agent)", action, "update", res.get("to"), true, detail); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return os.Rename(filepath.Join(updateDir(), name), filepath.Join(updateDir(), "done-"+id))
}

func pruneDoneResults() {
	entries, err := os.ReadDir(updateDir())
	if err != nil {
		return
	}
	type done struct {
		name string
		at   int64
	}
	var all []done
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "done-") {
			continue
		}
		f, err := readSpool(e.Name())
		var at int64
		if err == nil {
			at, _ = strconv.ParseInt(f.get("finished_at"), 10, 64)
		}
		all = append(all, done{e.Name(), at})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].at > all[j].at })
	for i := 5; i < len(all); i++ {
		os.Remove(filepath.Join(updateDir(), all[i].name))
	}
}

// pendingResults reports whether a result is waiting to be consumed.
func pendingResults() bool {
	entries, _ := os.ReadDir(updateDir())
	for _, e := range entries {
		if updateResultName.MatchString(e.Name()) {
			return true
		}
	}
	return false
}

// newestDone is the most recently finished request's result, if any.
func newestDone() (spoolFile, bool) {
	entries, _ := os.ReadDir(updateDir())
	var best spoolFile
	var bestAt int64 = -1
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "done-") {
			continue
		}
		f, err := readSpool(e.Name())
		if err != nil {
			continue
		}
		at, _ := strconv.ParseInt(f.get("finished_at"), 10, 64)
		if at > bestAt {
			best, bestAt = f, at
		}
	}
	return best, bestAt >= 0
}
