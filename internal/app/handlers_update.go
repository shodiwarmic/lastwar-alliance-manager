package app

// handlers_update.go — Admin → Security & API → "About this install and updates": what this
// install runs, and what the release check (release_check.go) last learned.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// releaseNotesCap is how much of a release's notes the Admin page shows; the rest is a link.
const releaseNotesCap = 2000

// updateStatePayload is GET /api/admin/update's body, also returned by a manual check.
func updateStatePayload() (map[string]any, error) {
	st, err := latestRelease()
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"running": map[string]any{
			"version":     appVersion,
			"commit":      shortCommit(),
			"host_files":  os.Getenv("HOST_FILES_VERSION"),
			"app_version": os.Getenv("APP_VERSION"),
		},
		"check":        releaseCheckMode(),
		"checked_at":   st.CheckedAt,
		"attempted_at": st.AttemptedAt,
		"last_error":   st.LastError,
		"latest":       nil,
	}
	if st.LatestTag != "" {
		notes := []rune(st.LatestBody)
		truncated := len(notes) > releaseNotesCap
		if truncated {
			notes = notes[:releaseNotesCap]
		}
		out["latest"] = map[string]any{
			"tag":             st.LatestTag,
			"name":            st.LatestName,
			"url":             st.LatestURL,
			"published_at":    st.LatestPublishedAt,
			"notes":           string(notes),
			"notes_truncated": truncated,
			"level":           releaseLevel(appVersion, st.LatestTag),
		}
	}
	if err := consumeUpdateResults(); err != nil {
		slog.Warn("update state: a finished update's result could not be recorded", "error", err)
		out["result_error"] = err.Error()
	}
	updateRequestMu.Lock()
	v := readUpdateView(st.LatestTag)
	updateRequestMu.Unlock()
	out["agent"] = v.Agent
	out["request"] = v.Request
	out["log"] = v.Log
	out["can_install"] = v.CanInstall
	out["install_reason"] = v.InstallReason
	out["can_test"] = v.CanTest
	out["test_reason"] = v.TestReason
	out["refused"] = v.Refused
	out["gated"] = updateGateActive()
	out["schema_ahead"] = schemaAheadMessage()
	return out, nil
}

// getUpdateState is GET /api/admin/update. A read, so it stays open in demo mode.
func getUpdateState(w http.ResponseWriter, r *http.Request) {
	out, err := updateStatePayload()
	if err != nil {
		slog.Error("update state: read failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, out)
}

// checkForUpdateNow is POST /api/admin/update/check, the Admin page's "Check now". It goes
// through checkLatestRelease, so UPDATE_CHECK=off is refused here exactly as it is for the
// scheduled check, and at most one request a minute reaches GitHub from it.
func checkForUpdateNow(w http.ResponseWriter, r *http.Request) {
	if releaseCheckMode() != "on" {
		http.Error(w, "Release checks are off on this install (UPDATE_CHECK=off in .env).", http.StatusConflict)
		return
	}
	var recent bool
	if err := db.QueryRow(`SELECT COALESCE(attempted_at > datetime('now', '-1 minute'), 0)
		FROM release_state WHERE id = 1`).Scan(&recent); err != nil {
		slog.Error("update check: read failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if recent {
		http.Error(w, "Checked less than a minute ago — try again shortly.", http.StatusTooManyRequests)
		return
	}
	switch err := checkLatestRelease(r.Context()); {
	case errors.Is(err, errReleaseCheckBusy):
		http.Error(w, "A check is already running — try again shortly.", http.StatusTooManyRequests)
		return
	case errors.Is(err, errReleaseCheckOff):
		http.Error(w, "Release checks are off on this install (UPDATE_CHECK=off in .env).", http.StatusConflict)
		return
	case errors.Is(err, errUpdateInProgress):
		http.Error(w, "An update is being installed.", http.StatusServiceUnavailable)
		return
	}
	// Any other failure is recorded in last_error, which the payload carries.
	getUpdateState(w, r)
}

// updateAvailableFor is the newer release an admin's pages nudge about (PageData), or "":
// only for an administrator, only while checks are on, and only when a level can be derived —
// so a dev or edge build is never nudged.
func updateAvailableFor(isAdmin bool) string {
	if !isAdmin || releaseCheckMode() != "on" {
		return ""
	}
	var tag string
	if err := db.QueryRow(`SELECT COALESCE(latest_tag, '') FROM release_state WHERE id = 1`).Scan(&tag); err != nil {
		return ""
	}
	if releaseLevel(appVersion, tag) == "" {
		return ""
	}
	return tag
}

// ── Installing from the Admin page (the host update agent: scripts/lib/update-agent.sh) ──────

const updateAgentStale = 2 * time.Minute

// defaultDatabasePath is the Docker install's: the agent backs up and restores exactly this file.
// A variable only so the tests can stand in a temp path.
var defaultDatabasePath = "/app/data/alliance.db"

// updateView is everything the Admin page's update block shows, and the two decisions it acts
// on, each with the reason it is no.
type updateView struct {
	Agent         map[string]any
	Request       map[string]any
	Log           []string
	CanInstall    bool
	InstallReason string
	CanTest       bool
	TestReason    string
	Refused       string
}

func readUpdateView(latestTag string) updateView {
	var v updateView
	ls := readLease()
	hb, hbErr := readSpool("agent")
	present := hbErr == nil && hb.Kind == "agent"
	var lastSeen time.Time
	if present {
		lastSeen, _ = spoolEpoch(hb.get("seen_at"))
	}
	if ls.Status.Kind != "" {
		if at, ok := spoolEpoch(ls.Status.get("updated_at")); ok && at.After(lastSeen) {
			lastSeen = at
		}
	}
	stale := present && time.Since(lastSeen) > updateAgentStale
	protocol, _ := strconv.Atoi(hb.get("protocol"))
	needsOperator := present && hb.get("journal_phase") == "needs-operator"
	v.Agent = map[string]any{"present": present, "release": hb.get("release"), "protocol": protocol,
		"last_seen": lastSeen.Unix(), "stale": stale, "needs_operator": needsOperator}

	// A request the agent would not claim (no valid id, unreadable): say why, and clear it.
	if st, err := readSpool("status"); err == nil && st.get("id") == "invalid" && st.get("state") == "refused" {
		v.Refused = st.get("step")
		os.Remove(filepath.Join(updateDir(), "request"))
		os.Remove(filepath.Join(updateDir(), "status"))
	}

	inFlight := ls.RunningID != "" || spoolExists("request")
	var logID string
	if ls.RunningID != "" && ls.Status.Kind != "" {
		v.Request = map[string]any{"active": true, "id": ls.RunningID, "kind": ls.Status.get("kind"),
			"state": ls.Status.get("state"), "step": ls.Status.get("step"), "from": ls.Status.get("from"),
			"to": ls.Status.get("to"), "updated_at": ls.Status.get("updated_at")}
		logID = ls.RunningID
	} else if inFlight {
		v.Request = map[string]any{"active": true, "state": "waiting", "step": "Waiting for the host agent to pick the request up"}
	} else if d, ok := newestDone(); ok {
		v.Request = map[string]any{"active": false, "id": d.get("id"), "kind": d.get("kind"),
			"outcome": d.get("outcome"), "message": d.get("message"), "from": d.get("from"), "to": d.get("to"),
			"finished_at": d.get("finished_at"), "log_path": d.get("log")}
		logID = d.get("id")
	}
	if logID != "" && updateIDPattern.MatchString(logID) {
		v.Log = tailUpdateLog(logID, 40)
	}

	pending := pendingResults()
	agentOK := ""
	switch {
	case demoMode():
		agentOK = "the demo does not install updates"
	case !present:
		agentOK = "no host update agent was found — run ./scripts/manage.sh update on the server once; that installs it"
	case protocol != updateProtocol:
		agentOK = fmt.Sprintf("the host agent speaks protocol %d and this app %d — update from a terminal (./scripts/manage.sh update)", protocol, updateProtocol)
	case stale:
		agentOK = fmt.Sprintf("the host agent was last seen %s ago — check systemctl status alliance-manager-update.timer on the server", time.Since(lastSeen).Round(time.Minute))
	case needsOperator:
		agentOK = "the last update needs the operator — see docs/DEPLOYMENT.md → Recovering from a failed update"
	case inFlight:
		agentOK = "an update request is already in progress"
	case pending:
		agentOK = "the last update's result has not been recorded yet — reload the page"
	}
	v.TestReason = agentOK
	v.CanTest = agentOK == ""

	reason := agentOK
	if reason == "" {
		pin := os.Getenv("APP_VERSION")
		switch {
		case databasePath() != defaultDatabasePath:
			reason = "DATABASE_PATH is " + databasePath() + "; installing from the Admin page needs the Docker default, " + defaultDatabasePath
		case !isReleaseTag(appVersion):
			reason = "this install runs a build that is not a release (" + appVersion + "), so the app cannot tell what to move it from — update from a terminal"
		case pin != appVersion:
			reason = "APP_VERSION in .env is " + strconv.Quote(pin) + ", not this build's " + appVersion + " — update from a terminal"
		case releaseCheckMode() != "on":
			reason = "release checks are off (UPDATE_CHECK=off), so no newer release is known"
		case latestTag == "" || releaseLevel(appVersion, latestTag) == "":
			reason = "this is the newest release"
		}
	}
	v.InstallReason = reason
	v.CanInstall = reason == ""
	return v
}

func isReleaseTag(t string) bool { _, ok := parseReleaseTag(t); return ok }

// tailUpdateLog is the last n lines of the log tail the agent publishes for a request.
func tailUpdateLog(id string, n int) []string {
	b, err := os.ReadFile(filepath.Join(updateDir(), "update-"+id+".log"))
	if err != nil {
		return nil
	}
	if len(b) > 64<<10 {
		b = b[len(b)-64<<10:]
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// newUpdateRequestID is unique enough for one install's requests and fits the agent's
// ^[A-Za-z0-9-]{1,40}$.
func newUpdateRequestID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

// requestUpdate is POST /api/admin/update: {"version":"vX.Y.Z"} installs that release (it must
// be the latest the check knows), {"version":"noop"} tests the agent. The request is written
// to a temp file inside the spool — the same filesystem, so the link cannot fail with EXDEV — and
// claimed with os.Link, which fails if a request is already waiting: exactly one request at a
// time, without a lock the agent would have to share.
func requestUpdate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	updateRequestMu.Lock()
	defer updateRequestMu.Unlock()
	if err := consumeUpdateResultsLocked(); err != nil {
		slog.Error("update request: a finished update's result could not be recorded", "error", err)
		http.Error(w, "The last update's result could not be recorded, so no new request was written: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	st, err := latestRelease()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	v := readUpdateView(st.LatestTag)
	var target string
	if req.Version == "noop" {
		if !v.CanTest {
			http.Error(w, "The host agent cannot be tested now: "+v.TestReason, http.StatusConflict)
			return
		}
		target = "noop"
	} else {
		if !v.CanInstall {
			http.Error(w, "This update cannot be installed from here: "+v.InstallReason, http.StatusConflict)
			return
		}
		if req.Version != st.LatestTag {
			http.Error(w, "Only the newest release, "+st.LatestTag+", can be installed from here.", http.StatusConflict)
			return
		}
		target = "alliance-manager " + req.Version
	}

	id := newUpdateRequestID()
	body := fmt.Sprintf("alliance-manager update request %d\nid=%s\nfrom=%s\ntarget=%s\nrequested_by=%s\nrequested_at=%d\n",
		updateProtocol, id, os.Getenv("APP_VERSION"), target, strings.NewReplacer("\n", " ", "\r", " ").Replace(user.Username), time.Now().Unix())
	dir := updateDir()
	tmp, err := os.CreateTemp(dir, ".request-*")
	if err != nil {
		slog.Error("update request: temp file", "error", err)
		http.Error(w, "The update request could not be written.", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmp.Name())
	_, werr := tmp.WriteString(body)
	if werr == nil {
		werr = tmp.Chmod(0o644)
	}
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		slog.Error("update request: write", "error", werr)
		http.Error(w, "The update request could not be written.", http.StatusInternalServerError)
		return
	}
	reqPath := filepath.Join(dir, "request")
	if err := os.Link(tmp.Name(), reqPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			http.Error(w, "A request is already waiting for the host agent.", http.StatusConflict)
			return
		}
		slog.Error("update request: link", "error", err)
		http.Error(w, "The update request could not be written.", http.StatusInternalServerError)
		return
	}
	// The agent may have claimed another request between the checks and the link.
	if ls := readLease(); ls.RunningID != "" && ls.RunningID != id {
		if f, err := readSpool("request"); err == nil && f.get("id") == id {
			os.Remove(reqPath)
		}
		http.Error(w, "Another update request was claimed first.", http.StatusConflict)
		return
	}
	if target == "noop" {
		logActivity(user.ID, user.Username, "requested", "update", "host agent test", true, "a test request (noop)")
	} else {
		logActivity(user.ID, user.Username, "requested", "update", req.Version, true, appVersion+" → "+req.Version)
	}
	writeJSON(w, map[string]string{"id": id})
}
