package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func getHealth(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	return rec
}

func TestHealthIsOKWithoutASession(t *testing.T) {
	setupNameMatchTestDB(t)
	rec := getHealth(t, buildRouter())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"status":"ok"}` {
		t.Errorf("body = %q, want exactly {\"status\":\"ok\"}", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
}

// A fresh install must report healthy before its first administrator exists: the
// HEALTHCHECK and install.sh both wait on this route, and the setup gate redirects every
// unlisted path to /setup.
func TestHealthAnswersOnAnUnclaimedInstall(t *testing.T) {
	setupSetupTestDB(t)
	if !installUnclaimed.Load() {
		t.Fatal("precondition: the install should be unclaimed")
	}
	rec := getHealth(t, setupGate(buildRouter()))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 through the setup gate (Location %q)", rec.Code, rec.Header().Get("Location"))
	}
}

func TestHealthIsDegradedWhenTheDatabaseIsGone(t *testing.T) {
	setupNameMatchTestDB(t)
	db.Close()
	rec := getHealth(t, buildRouter())
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"status":"degraded"}` {
		t.Errorf("body = %q, want exactly {\"status\":\"degraded\"}", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q (set before WriteHeader, or it is dropped)", got)
	}
}

func TestRunHealthcheckExitStatus(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			t.Errorf("probed %s", r.URL.Path)
		}
		io.WriteString(w, `{"status":"ok"}`)
	}))
	defer ok.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()

	var out strings.Builder
	if got := runHealthcheck(ok.URL+"/api/health", &out); got != 0 {
		t.Errorf("healthy server: exit %d, want 0 (%s)", got, out.String())
	}
	out.Reset()
	if got := runHealthcheck(bad.URL+"/api/health", &out); got != 1 {
		t.Errorf("503: exit %d, want 1", got)
	}
	if !strings.Contains(out.String(), "503") {
		t.Errorf("a failure should say why: %q", out.String())
	}
	addr := bad.URL
	bad.Close()
	if got := runHealthcheck(addr+"/api/health", io.Discard); got != 1 {
		t.Errorf("nothing listening: exit %d, want 1", got)
	}
}

func TestHealthcheckURLFollowsPORT(t *testing.T) {
	t.Setenv("PORT", "")
	if got := healthcheckURL(); got != "http://127.0.0.1:8080/api/health" {
		t.Errorf("default = %q", got)
	}
	t.Setenv("PORT", "9090")
	if got := healthcheckURL(); got != "http://127.0.0.1:9090/api/health" {
		t.Errorf("PORT=9090 gives %q", got)
	}
}
