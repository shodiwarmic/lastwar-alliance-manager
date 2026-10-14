package app

// release_check.go — learn that a newer release of this app exists.
//
// One unauthenticated GET a day to GitHub's "latest release" endpoint — the same request
// `scripts/manage.sh update` already makes — cached in release_state (migration 083) and shown
// on Admin → Security & API, plus a once-per-session toast for admins. It is the operator's
// choice, so the switch is UPDATE_CHECK=off in .env (settable before the first boot, out of an
// officer's reach), and the demo never makes the request.
//
// ── Cadence ──────────────────────────────────────────────────────────────────────────────
// The checker wakes a minute after boot and then hourly, and calls GitHub only when the last
// ATTEMPT is over 23 hours old and any rate-limit reset GitHub named has passed. attempted_at is
// written before the request, so a failed check also waits a day — never "due again" on every
// restart of a crash loop (the LastRank scheduler's lesson, CLAUDE.md → "Three timestamps").
// A stored ETag makes the request conditional; a 304 counts as a check.
//
// Read → fetch → write: no database handle is held across the HTTP call (CLAUDE.md → THE RULE).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// releasesLatestURL is GitHub's newest-published-release endpoint for this repository. A
// variable so tests can point it at an httptest server.
var releasesLatestURL = "https://api.github.com/repos/shodiwarmic/lastwar-alliance-manager/releases/latest"

// The User-Agent carries no version: the request says that an install exists, not what it runs.
const releaseCheckUserAgent = "lastwar-alliance-manager"

var releaseHTTPClient = &http.Client{Timeout: 10 * time.Second}

// releaseCheckMu makes a check exclusive: the hourly checker and a "Check now" click can never
// run two requests at once (TryLock — the loser is told, not queued).
var releaseCheckMu sync.Mutex

// The checker's timing, variables so a test need not wait an hour.
var (
	releaseCheckFirstDelay = time.Minute
	releaseCheckInterval   = time.Hour
)

var (
	errReleaseCheckOff  = errors.New("release checks are off")
	errReleaseCheckBusy = errors.New("a release check is already running")
)

// releaseCheckMode is "demo" in demo mode, "off" under UPDATE_CHECK=off, else "on". Only "on"
// ever makes a request.
func releaseCheckMode() string {
	if demoMode() {
		return "demo"
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("UPDATE_CHECK")), "off") {
		return "off"
	}
	return "on"
}

// ReleaseState is the release_state row: what the last check learned, and when.
type ReleaseState struct {
	CheckedAt         string
	ETag              string
	LatestTag         string
	LatestName        string
	LatestURL         string
	LatestBody        string
	LatestPublishedAt string
	LastError         string
	AttemptedAt       string
	NextCheckAfter    string
}

// latestRelease reads the cached state.
func latestRelease() (ReleaseState, error) {
	var s ReleaseState
	err := db.QueryRow(`SELECT COALESCE(checked_at, ''), COALESCE(etag, ''), COALESCE(latest_tag, ''),
		COALESCE(latest_name, ''), COALESCE(latest_url, ''), COALESCE(latest_body, ''),
		COALESCE(latest_published_at, ''), COALESCE(last_error, ''), COALESCE(attempted_at, ''),
		COALESCE(next_check_after, '')
		FROM release_state WHERE id = 1`).Scan(&s.CheckedAt, &s.ETag, &s.LatestTag, &s.LatestName,
		&s.LatestURL, &s.LatestBody, &s.LatestPublishedAt, &s.LastError, &s.AttemptedAt, &s.NextCheckAfter)
	return s, err
}

// releaseLevel is the level of the step from the running release to latest — "major",
// "minor" or "patch", by the publish workflow's rule (the first component that differs) —
// or "" when either is not a vX.Y.Z release tag, or latest is not newer.
func releaseLevel(running, latest string) string {
	r, ok1 := parseReleaseTag(running)
	l, ok2 := parseReleaseTag(latest)
	if !ok1 || !ok2 {
		return ""
	}
	switch {
	case l[0] != r[0]:
		if l[0] > r[0] {
			return "major"
		}
	case l[1] != r[1]:
		if l[1] > r[1] {
			return "minor"
		}
	case l[2] > r[2]:
		return "patch"
	}
	return ""
}

// parseReleaseTag reads vX.Y.Z, the only shape a release is tagged with.
func parseReleaseTag(tag string) ([3]int, bool) {
	var v [3]int
	if !strings.HasPrefix(tag, "v") {
		return v, false
	}
	parts := strings.Split(tag[1:], ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		if p == "" || strings.TrimLeft(p, "0123456789") != "" {
			return v, false
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// githubRelease is the part of GitHub's release object the check keeps.
type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
}

// releaseBodyCap bounds the stored notes: more than the Admin page shows (releaseNotesCap),
// so it can tell that notes were cut and link to the release for the rest.
const releaseBodyCap = 2 * releaseNotesCap

// checkLatestRelease asks GitHub for the newest published release and records the answer.
// It is the one entry point — the hourly checker and POST /api/admin/update/check both come
// through here — so the opt-out is enforced in one place: it refuses before any request in
// demo mode and under UPDATE_CHECK=off. A failure is logged once at Warn and recorded in
// last_error, keeping the previously learned release.
func checkLatestRelease(ctx context.Context) error {
	if releaseCheckMode() != "on" {
		return errReleaseCheckOff
	}
	if !releaseCheckMu.TryLock() {
		return errReleaseCheckBusy
	}
	defer releaseCheckMu.Unlock()

	// Read, then stamp the attempt before the request: a check that hangs or crashes still
	// counts against the day.
	var etag string
	if err := db.QueryRow(`SELECT COALESCE(etag, '') FROM release_state WHERE id = 1`).Scan(&etag); err != nil {
		return fmt.Errorf("read release state: %w", err)
	}
	if _, err := db.Exec(`UPDATE release_state SET attempted_at = datetime('now') WHERE id = 1`); err != nil {
		return fmt.Errorf("record the attempt: %w", err)
	}

	// Fetch — no database handle held.
	rel, notModified, resetAt, fetchErr := fetchLatestRelease(ctx, etag)

	// Write.
	if fetchErr != nil {
		slog.Warn("Release check failed", "error", fetchErr)
		var next any
		if !resetAt.IsZero() {
			next = resetAt.UTC().Unix()
		}
		_, err := db.Exec(`UPDATE release_state SET last_error = ?,
			next_check_after = CASE WHEN ? IS NULL THEN NULL ELSE datetime(?, 'unixepoch') END
			WHERE id = 1`, fetchErr.Error(), next, next)
		if err != nil {
			slog.Error("Release check: could not record the failure", "error", err)
		}
		return fetchErr
	}
	if notModified {
		slog.Info("Release check: unchanged since the last check")
		_, err := db.Exec(`UPDATE release_state SET checked_at = datetime('now'), last_error = NULL,
			next_check_after = NULL WHERE id = 1`)
		return err
	}
	slog.Info("Release check", "latest", rel.release.TagName, "running", appVersion)
	body := rel.release.Body
	if r := []rune(body); len(r) > releaseBodyCap {
		body = string(r[:releaseBodyCap])
	}
	_, err := db.Exec(`UPDATE release_state SET checked_at = datetime('now'), last_error = NULL,
		next_check_after = NULL, etag = ?, latest_tag = ?, latest_name = ?, latest_url = ?,
		latest_body = ?, latest_published_at = ? WHERE id = 1`,
		rel.etag, rel.release.TagName, rel.release.Name, rel.release.HTMLURL, body, rel.release.PublishedAt)
	return err
}

type fetchedRelease struct {
	release githubRelease
	etag    string
}

// fetchLatestRelease makes the request. notModified means a 304 for the stored ETag. On a
// rate-limit refusal resetAt is the time GitHub said the limit resets.
func fetchLatestRelease(ctx context.Context, etag string) (rel fetchedRelease, notModified bool, resetAt time.Time, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesLatestURL, nil)
	if err != nil {
		return rel, false, resetAt, err
	}
	req.Header.Set("User-Agent", releaseCheckUserAgent)
	req.Header.Set("Accept", "application/vnd.github+json")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := releaseHTTPClient.Do(req)
	if err != nil {
		return rel, false, resetAt, fmt.Errorf("GitHub could not be reached: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotModified:
		return rel, true, resetAt, nil
	case resp.StatusCode == http.StatusOK:
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel.release); err != nil {
			return rel, false, resetAt, fmt.Errorf("GitHub's answer could not be read: %w", err)
		}
		if _, ok := parseReleaseTag(rel.release.TagName); !ok {
			return rel, false, resetAt, fmt.Errorf("GitHub named a latest release that is not vX.Y.Z: %q", rel.release.TagName)
		}
		rel.etag = resp.Header.Get("ETag")
		return rel, false, resetAt, nil
	case resp.StatusCode == http.StatusNotFound:
		return rel, false, resetAt, errors.New("GitHub has no published release for this app yet")
	case (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) &&
		resp.Header.Get("X-RateLimit-Remaining") == "0":
		if n, perr := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); perr == nil {
			resetAt = time.Unix(n, 0)
		}
		return rel, false, resetAt, errors.New("GitHub's rate limit for this address is used up")
	default:
		return rel, false, resetAt, fmt.Errorf("GitHub answered %s", resp.Status)
	}
}

// releaseCheckDue is the scheduled checker's gate: the last attempt is over 23 hours old and
// any rate-limit reset has passed.
func releaseCheckDue() (bool, error) {
	var due bool
	err := db.QueryRow(`SELECT (attempted_at IS NULL OR attempted_at <= datetime('now', '-23 hours'))
		AND (next_check_after IS NULL OR next_check_after <= datetime('now'))
		FROM release_state WHERE id = 1`).Scan(&due)
	return due, err
}

// runScheduledReleaseCheck is one wake of the checker.
func runScheduledReleaseCheck() {
	due, err := releaseCheckDue()
	if err != nil {
		slog.Warn("Release check: could not read its state", "error", err)
		return
	}
	if !due {
		return
	}
	// The result is recorded either way; a failure has already been logged.
	_ = checkLatestRelease(context.Background())
}

// startReleaseChecker starts the daily check, unless checks are off or this is the demo.
func startReleaseChecker() {
	if mode := releaseCheckMode(); mode != "on" {
		slog.Info("Release check disabled", "mode", mode)
		return
	}
	go func() {
		time.Sleep(releaseCheckFirstDelay)
		runScheduledReleaseCheck()
		ticker := time.NewTicker(releaseCheckInterval)
		defer ticker.Stop()
		for range ticker.C {
			runScheduledReleaseCheck()
		}
	}()
	slog.Info("Release check started (one request a day to GitHub; UPDATE_CHECK=off in .env turns it off)")
}
