package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReleaseLevel(t *testing.T) {
	for _, c := range []struct{ running, latest, want string }{
		{"v2.4.0", "v3.0.0", "major"},
		{"v2.4.0", "v2.5.0", "minor"},
		{"v2.4.0", "v2.4.1", "patch"},
		{"v2.4.1", "v2.5.0", "minor"},
		{"v2.9.9", "v3.0.0", "major"},
		{"v2.4.0", "v2.4.0", ""},  // the same release
		{"v2.5.0", "v2.4.9", ""},  // latest is older (a withdrawn release)
		{"v3.0.0", "v2.9.9", ""},  // older major
		{"v2.4.10", "v2.4.9", ""}, // numeric, not lexical
		{"v2.4.9", "v2.4.10", "patch"},
		{"dev", "v3.0.0", ""},
		{"edge", "v3.0.0", ""},
		{"v2.4.0", "edge", ""},
		{"2.4.0", "v3.0.0", ""},
		{"v2.4", "v3.0.0", ""},
		{"v2.4.0-rc1", "v3.0.0", ""},
		{"v2.4.x", "v3.0.0", ""},
		{"v2..0", "v3.0.0", ""},
	} {
		if got := releaseLevel(c.running, c.latest); got != c.want {
			t.Errorf("releaseLevel(%q, %q) = %q, want %q", c.running, c.latest, got, c.want)
		}
	}
}

// fakeGitHub serves the latest-release endpoint from a handler, counting hits, with
// releasesLatestURL pointed at it for the test.
func fakeGitHub(t *testing.T, h http.HandlerFunc) *atomic.Int32 {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	prev := releasesLatestURL
	releasesLatestURL = srv.URL + "/repos/x/y/releases/latest"
	t.Cleanup(func() { releasesLatestURL = prev })
	return &hits
}

const fakeReleaseJSON = `{"tag_name":"v9.1.0","name":"v9.1.0 — the one","html_url":"https://github.com/x/y/releases/tag/v9.1.0",
	"body":"## Changed\n- things","published_at":"2026-10-14T00:00:00Z","draft":false}`

func TestCheckLatestReleaseStoresAndRevalidates(t *testing.T) {
	setupNameMatchTestDB(t)
	t.Setenv("UPDATE_CHECK", "")
	var sawUA, sawINM atomic.Value
	sawINM.Store("")
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		sawUA.Store(r.Header.Get("User-Agent"))
		if inm := r.Header.Get("If-None-Match"); inm != "" {
			sawINM.Store(inm)
			if inm == `"etag-1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		w.Header().Set("ETag", `"etag-1"`)
		io.WriteString(w, fakeReleaseJSON)
	})

	if err := checkLatestRelease(context.Background()); err != nil {
		t.Fatalf("first check: %v", err)
	}
	st, _ := latestRelease()
	if st.LatestTag != "v9.1.0" || st.LatestName != "v9.1.0 — the one" || st.ETag != `"etag-1"` ||
		st.LatestURL == "" || !strings.Contains(st.LatestBody, "things") || st.LatestPublishedAt == "" ||
		st.CheckedAt == "" || st.AttemptedAt == "" || st.LastError != "" {
		t.Fatalf("stored state: %+v", st)
	}
	if ua := sawUA.Load(); ua != "lastwar-alliance-manager" {
		t.Errorf("User-Agent = %q: it carries no version", ua)
	}

	// A 304 counts as a check, and changes nothing else.
	db.Exec(`UPDATE release_state SET checked_at = '2000-01-01 00:00:00', last_error = 'stale'`)
	if err := checkLatestRelease(context.Background()); err != nil {
		t.Fatalf("revalidation: %v", err)
	}
	if got := sawINM.Load(); got != `"etag-1"` {
		t.Errorf("If-None-Match = %q, want the stored ETag", got)
	}
	st2, _ := latestRelease()
	if st2.CheckedAt == "2000-01-01 00:00:00" || st2.LastError != "" {
		t.Errorf("a 304 should bump checked_at and clear last_error: %+v", st2)
	}
	if st2.LatestTag != st.LatestTag || st2.ETag != st.ETag || st2.LatestBody != st.LatestBody {
		t.Errorf("a 304 changed the stored release: %+v", st2)
	}
}

func TestCheckLatestReleaseFailuresKeepThePreviousRelease(t *testing.T) {
	setupNameMatchTestDB(t)
	t.Setenv("UPDATE_CHECK", "")
	fail := atomic.Bool{}
	slow := atomic.Bool{}
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if slow.Load() {
			time.Sleep(300 * time.Millisecond)
		}
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		io.WriteString(w, fakeReleaseJSON)
	})
	if err := checkLatestRelease(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, _ := latestRelease()

	fail.Store(true)
	if err := checkLatestRelease(context.Background()); err == nil {
		t.Fatal("a 500 should be an error")
	}
	st, _ := latestRelease()
	if !strings.Contains(st.LastError, "500") || st.LatestTag != "v9.1.0" || st.CheckedAt != before.CheckedAt {
		t.Errorf("after a 500: %+v", st)
	}

	fail.Store(false)
	slow.Store(true)
	prev := releaseHTTPClient
	releaseHTTPClient = &http.Client{Timeout: 50 * time.Millisecond}
	t.Cleanup(func() { releaseHTTPClient = prev })
	if err := checkLatestRelease(context.Background()); err == nil {
		t.Fatal("a timeout should be an error")
	}
	st, _ = latestRelease()
	if !strings.Contains(st.LastError, "could not be reached") || st.LatestTag != "v9.1.0" {
		t.Errorf("after a timeout: %+v", st)
	}
}

func TestCheckLatestReleaseRecordsARateLimitReset(t *testing.T) {
	setupNameMatchTestDB(t)
	t.Setenv("UPDATE_CHECK", "")
	reset := time.Now().Add(2 * time.Hour).Unix()
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		w.WriteHeader(http.StatusForbidden)
	})
	if err := checkLatestRelease(context.Background()); err == nil {
		t.Fatal("a rate-limit refusal should be an error")
	}
	st, _ := latestRelease()
	want := time.Unix(reset, 0).UTC().Format(sqliteTimeLayout)
	if st.NextCheckAfter != want || !strings.Contains(st.LastError, "rate limit") {
		t.Errorf("next_check_after = %q (want %q), last_error = %q", st.NextCheckAfter, want, st.LastError)
	}
	// Even with the attempt a day old, the named reset still holds the checker back.
	db.Exec(`UPDATE release_state SET attempted_at = datetime('now', '-2 days')`)
	if due, _ := releaseCheckDue(); due {
		t.Error("due before GitHub's reset")
	}
	db.Exec(`UPDATE release_state SET next_check_after = datetime('now', '-1 minute')`)
	if due, _ := releaseCheckDue(); !due {
		t.Error("not due once the reset has passed")
	}
}

// A failed check waits a day too: the gate is on attempted_at, written before the request,
// and it lives in the database, so a restart does not make the check due again.
func TestReleaseCheckIsDailyAfterAFailure(t *testing.T) {
	setupNameMatchTestDB(t)
	t.Setenv("UPDATE_CHECK", "")
	hits := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	if due, _ := releaseCheckDue(); !due {
		t.Fatal("a fresh install should be due")
	}
	runScheduledReleaseCheck()
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}
	// "Restarts": the checker wakes again and again.
	for i := 0; i < 3; i++ {
		runScheduledReleaseCheck()
	}
	if hits.Load() != 1 {
		t.Errorf("hits = %d after more wakes within the day, want 1", hits.Load())
	}
	db.Exec(`UPDATE release_state SET attempted_at = datetime('now', '-22 hours')`)
	runScheduledReleaseCheck()
	if hits.Load() != 1 {
		t.Error("checked again after 22 hours")
	}
	db.Exec(`UPDATE release_state SET attempted_at = datetime('now', '-23 hours', '-1 minute')`)
	runScheduledReleaseCheck()
	if hits.Load() != 2 {
		t.Errorf("hits = %d after 23 hours, want 2", hits.Load())
	}
}

func TestReleaseCheckMakesNoRequestWhenOffOrInTheDemo(t *testing.T) {
	setupNameMatchTestDB(t)
	hits := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, fakeReleaseJSON) })
	prevDelay := releaseCheckFirstDelay
	releaseCheckFirstDelay = 0
	t.Cleanup(func() { releaseCheckFirstDelay = prevDelay })

	for _, env := range []struct{ key, val, mode string }{
		{"UPDATE_CHECK", "off", "off"},
		{"UPDATE_CHECK", "OFF", "off"},
		{"DEMO_MODE", "true", "demo"},
	} {
		t.Run(env.key+"="+env.val, func(t *testing.T) {
			t.Setenv("UPDATE_CHECK", "")
			t.Setenv("DEMO_MODE", "")
			t.Setenv(env.key, env.val)
			if got := releaseCheckMode(); got != env.mode {
				t.Fatalf("mode = %q, want %q", got, env.mode)
			}
			if err := checkLatestRelease(context.Background()); !errors.Is(err, errReleaseCheckOff) {
				t.Errorf("checkLatestRelease = %v, want errReleaseCheckOff", err)
			}
			startReleaseChecker()
			runScheduledReleaseCheck()
			time.Sleep(50 * time.Millisecond)
			if hits.Load() != 0 {
				t.Errorf("%d requests reached GitHub", hits.Load())
			}
			if updateAvailableFor(true) != "" {
				t.Error("no nudge while checks are off")
			}
		})
	}
}

func TestConcurrentReleaseChecksMakeOneRequest(t *testing.T) {
	setupNameMatchTestDB(t)
	t.Setenv("UPDATE_CHECK", "")
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	hits := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		io.WriteString(w, fakeReleaseJSON)
	})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(1)
	go func() { defer wg.Done(); errs[0] = checkLatestRelease(context.Background()) }()
	<-entered // the first holds the lock, parked inside the request
	errs[1] = checkLatestRelease(context.Background())
	close(release)
	wg.Wait()
	if hits.Load() != 1 {
		t.Errorf("hits = %d, want 1", hits.Load())
	}
	if errs[0] != nil || !errors.Is(errs[1], errReleaseCheckBusy) {
		t.Errorf("errors = %v, want nil and busy", errs)
	}
}
