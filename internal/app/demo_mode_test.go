package app

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// demoBlocked is the demo's block list, one row per route. Adding a route to the list
// means adding a row here.
var demoBlocked = []struct{ method, path string }{
	{"POST", "/api/change-password"},
	{"POST", "/api/force-change-password"},
	{"POST", "/api/admin/users"},
	{"PUT", "/api/admin/users/2"},
	{"DELETE", "/api/admin/users/2"},
	{"POST", "/api/admin/users/2/reset-link"},
	{"PUT", "/api/admin/users/2/deactivate"},
	{"PUT", "/api/admin/users/2/reactivate"},
	{"POST", "/api/admin/users/2/transfer-files"},
	{"POST", "/api/members/1/invite"},
	{"POST", "/api/members/1/reset-link"},
	{"POST", "/invite/abc"},
	{"POST", "/reset-password/abc"},
	{"PUT", "/api/admin/security/password-policy"},
	{"PUT", "/api/admin/security/cv-worker"},
	{"PUT", "/api/admin/security/ocr-archive"},
	{"PUT", "/api/admin/security/translation"},
	{"POST", "/api/admin/credentials"},
	{"DELETE", "/api/admin/credentials/gcp_vision"},
	{"POST", "/api/mobile/login"},
	{"GET", "/api/mobile/members"},
	{"POST", "/api/mobile/preview"},
	{"POST", "/api/mobile/commit"},
	{"POST", "/api/files/upload"},
	{"POST", "/api/files/create"},
	{"DELETE", "/api/files/1"},
	{"POST", "/wopi/files/1"},
	{"POST", "/wopi/files/1/contents"},
	{"DELETE", "/api/members/1"},
	{"POST", "/api/lastrank/preview"},
	{"POST", "/api/lastrank/commit"},
	{"POST", "/api/lastrank/player"},
	{"POST", "/api/lastrank/finish"},
	{"POST", "/api/lastrank/prospect"},
	{"POST", "/api/lastrank/prospect/finish"},
	{"GET", "/api/lastrank/player-search"},
	{"POST", "/api/jobs/start"},
	{"POST", "/api/allies/nap/refresh"},
	{"POST", "/api/allies/nap/member"},
	{"POST", "/api/allies/nap/finish"},
	{"POST", "/api/external-alliances/1/refresh"},
	{"POST", "/api/external-alliances/lookup"},
	{"GET", "/api/external-alliances/search"},
	{"POST", "/api/external-alliances/report"},
	{"GET", "/api/external-alliances/report/player"},
	{"POST", "/api/vs-league/opponent-lookup"},
	{"GET", "/api/vs-league/opponent-roster"},
	{"GET", "/api/vs-league/our-snapshot"},
}

func serveRouter(t *testing.T, method, path, body string, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if c != nil {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	buildRouter().ServeHTTP(w, req)
	return w
}

func TestDemoBlockList(t *testing.T) {
	setupDemoSeedTestDB(t)
	if store == nil {
		initSessionStore()
	}
	admin := sessionCookie(t, 1, "") // demo-admin: the block must hold even for an admin
	for _, rt := range demoBlocked {
		t.Setenv("DEMO_MODE", "true")
		w := serveRouter(t, rt.method, rt.path, "{}", admin)
		if w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != demoBlockedMessage {
			t.Errorf("demo mode: %s %s = %d %q, want 403 %q", rt.method, rt.path, w.Code, w.Body.String(), demoBlockedMessage)
		}
		// Outside demo mode the same route is the ordinary handler: unauthenticated, it
		// must answer something other than the demo refusal.
		t.Setenv("DEMO_MODE", "")
		w = serveRouter(t, rt.method, rt.path, "{}", nil)
		if strings.TrimSpace(w.Body.String()) == demoBlockedMessage {
			t.Errorf("without demo mode: %s %s still refused as the demo", rt.method, rt.path)
		}
	}
}

// Writes only: the reads the Admin page and the Dashboard make stay available in demo mode.
func TestDemoKeepsReadsOpen(t *testing.T) {
	setupDemoSeedTestDB(t)
	if store == nil {
		initSessionStore()
	}
	t.Setenv("DEMO_MODE", "true")
	admin := sessionCookie(t, 1, "")
	for _, path := range []string{"/api/admin/users", "/api/lastrank/review/summary", "/api/admin/security/translation/usage", "/api/members"} {
		if w := serveRouter(t, "GET", path, "", admin); w.Code != http.StatusOK {
			t.Errorf("GET %s in demo mode = %d %s", path, w.Code, w.Body.String())
		}
	}
}

func TestDemoLogin(t *testing.T) {
	setupDemoSeedTestDB(t)
	if store == nil {
		initSessionStore()
	}
	t.Setenv("DEMO_MODE", "")
	if w := serveRouter(t, "POST", "/api/demo/login", `{"account":"demo-r3"}`, nil); w.Code != http.StatusNotFound {
		t.Errorf("outside demo mode /api/demo/login = %d, want 404", w.Code)
	}
	if w := serveRouter(t, "GET", "/api/demo/whoami", "", nil); w.Code != http.StatusNotFound {
		t.Errorf("outside demo mode /api/demo/whoami = %d, want 404", w.Code)
	}

	t.Setenv("DEMO_MODE", "true")
	w := serveRouter(t, "POST", "/api/demo/login", `{"account":"demo-r3"}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("demo login: %d %s", w.Code, w.Body.String())
	}
	c := lastCookie(w, nil)
	if c == nil {
		t.Fatal("no session cookie")
	}
	if u := sessionUser(t, c); u == nil || u.Username != "demo-r3" || u.Rank != "R3" || u.IsAdmin {
		t.Errorf("signed in as %+v", u)
	}
	if w := serveRouter(t, "POST", "/api/demo/login", `{"account":"someone-else"}`, nil); w.Code != http.StatusBadRequest {
		t.Errorf("unknown account: %d", w.Code)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM login_sessions`).Scan(&n)
	if n != 0 {
		t.Errorf("demo login tracked %d login_sessions rows", n)
	}
}

// Its own limiter: a burst of twelve, so six buttons survive one click each; the
// thirteenth inside the window is refused.
func TestDemoLoginLimiter(t *testing.T) {
	setupDemoSeedTestDB(t)
	if store == nil {
		initSessionStore()
	}
	t.Setenv("DEMO_MODE", "true")
	var codes []int
	for i := 0; i < 13; i++ {
		req := httptest.NewRequest("POST", "/api/demo/login", strings.NewReader(`{"account":"demo-admin"}`))
		req.RemoteAddr = "10.251.0.9:5000"
		w := httptest.NewRecorder()
		demoLogin(w, req)
		codes = append(codes, w.Code)
	}
	for i, c := range codes[:12] {
		if c != http.StatusOK {
			t.Fatalf("attempt %d: %d (%v)", i+1, c, codes)
		}
	}
	if codes[12] != http.StatusTooManyRequests {
		t.Errorf("thirteenth: %d, want 429", codes[12])
	}
}

func TestDemoTrackLoginAndSchedulerAreOff(t *testing.T) {
	setupDemoSeedTestDB(t)
	t.Setenv("DEMO_MODE", "true")
	req := httptest.NewRequest("POST", "/api/login", nil)
	req.RemoteAddr = "10.252.0.1:1"
	trackLogin(1, "demo-admin", req, true)
	if _, err := db.Exec(`UPDATE settings SET lastrank_auto_sync_enabled = 1, nap_auto_refresh_enabled = 1,
		prospect_auto_refresh_enabled = 1, lastrank_alliance_id = '0123456789abcdef0123456789abcdef' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	runScheduledLastRankTick()
	time.Sleep(200 * time.Millisecond)
	var sessions, jobs int
	db.QueryRow(`SELECT COUNT(*) FROM login_sessions`).Scan(&sessions)
	db.QueryRow(`SELECT COUNT(*) FROM background_jobs`).Scan(&jobs)
	if sessions != 0 {
		t.Errorf("trackLogin wrote %d rows in demo mode", sessions)
	}
	if jobs != 0 {
		t.Errorf("the scheduler started %d jobs in demo mode", jobs)
	}
}

func TestDemoLoginPageHasNoBanner(t *testing.T) {
	setupDemoSeedTestDB(t)
	if store == nil {
		initSessionStore()
	}
	if _, err := db.Exec(`UPDATE settings SET login_message = 'VISITOR WROTE THIS' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEMO_MODE", "true")
	w := serveRouter(t, "GET", "/login", "", nil)
	if strings.Contains(w.Body.String(), "VISITOR WROTE THIS") {
		t.Error("the demo's login page shows the stored login_message")
	}
	if !strings.Contains(w.Body.String(), `data-account="demo-r1"`) || strings.Contains(w.Body.String(), `id="login-form"`) {
		t.Error("the demo's login page should offer the Try-as buttons instead of the password form")
	}
	t.Setenv("DEMO_MODE", "")
	if w := serveRouter(t, "GET", "/login", "", nil); !strings.Contains(w.Body.String(), "VISITOR WROTE THIS") {
		t.Error("outside demo mode the login_message is not shown")
	}
}

func TestDemoSettingsKeepTheLoginMessage(t *testing.T) {
	setupDemoSeedTestDB(t)
	if store == nil {
		initSessionStore()
	}
	t.Setenv("DEMO_MODE", "true")
	admin := sessionCookie(t, 1, "")
	w := serveRouter(t, "GET", "/api/settings", "", admin)
	var settings map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &settings); err != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	settings["login_message"] = "VISITOR WROTE THIS"
	settings["join_requirements"] = "changed by a visitor"
	body, _ := json.Marshal(settings)
	if w := serveRouter(t, "PUT", "/api/settings", string(body), admin); w.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings in demo mode: %d %s", w.Code, w.Body.String())
	}
	var msg, req string
	db.QueryRow(`SELECT COALESCE(login_message, ''), join_requirements FROM settings WHERE id = 1`).Scan(&msg, &req)
	if msg != "" {
		t.Errorf("login_message stored as %q in demo mode", msg)
	}
	if req != "changed by a visitor" {
		t.Errorf("the rest of the settings did not save (join_requirements = %q)", req)
	}
}

// The demo's headers are deploy/Caddyfile's app-site header block, placeholders
// substituted. Parsing the Caddyfile keeps the two from drifting.
func TestDemoHeadersMatchTheCaddyfile(t *testing.T) {
	raw, err := os.ReadFile("deploy/Caddyfile")
	if err != nil {
		t.Fatal(err)
	}
	site := string(raw)[strings.Index(string(raw), "__APP_DOMAIN__ {"):]
	block := site[strings.Index(site, "header {"):]
	block = block[:strings.Index(block, "\n\t}")]
	line := regexp.MustCompile(`(?m)^\s*([A-Za-z-]+)\s+"(.*)"\s*$`)
	var caddy [][2]string
	for _, m := range line.FindAllStringSubmatch(block, -1) {
		caddy = append(caddy, [2]string{m[1], m[2]})
	}
	if len(caddy) == 0 {
		t.Fatal("parsed no headers from the Caddyfile")
	}
	if len(caddy) != len(demoSecurityHeaders) {
		t.Fatalf("Caddyfile sets %d headers, demo mode %d", len(caddy), len(demoSecurityHeaders))
	}
	for i := range caddy {
		if caddy[i] != demoSecurityHeaders[i] {
			t.Errorf("header %d: Caddyfile %q, demo %q", i, caddy[i], demoSecurityHeaders[i])
		}
	}

	t.Setenv("DEMO_MODE", "true")
	t.Setenv("DEMO_FIXTURES_ORIGIN", "https://fixtures.example")
	w := httptest.NewRecorder()
	demoHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-src https://fixtures.example;") || !strings.Contains(csp, "connect-src 'self' https://fixtures.example;") || strings.Contains(csp, "__") {
		t.Errorf("CSP = %q", csp)
	}
	if w.Header().Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Error("X-Frame-Options missing")
	}
	t.Setenv("DEMO_MODE", "")
	w = httptest.NewRecorder()
	demoHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Header().Get("Content-Security-Policy") != "" {
		t.Error("headers set outside demo mode (the proxy sets them there)")
	}
}

func TestDemoResetHours(t *testing.T) {
	for raw, want := range map[string]float64{"": 6, "0": 0, "0.05": 0.05, "12": 12, "-1": 6, "soon": 6} {
		t.Setenv("DEMO_RESET_HOURS", raw)
		if got := demoResetHours(); got != want {
			t.Errorf("DEMO_RESET_HOURS=%q → %v, want %v", raw, got, want)
		}
	}
}

// In demo mode an empty database seeds itself at boot, and a seed that cannot run stops
// the boot instead of serving a setup page.
func TestDemoBootSeedsAndFailsLoudly(t *testing.T) {
	if testing.Short() {
		t.Skip("boots the whole server")
	}
	dir := t.TempDir()
	cmd := bootCmd(t, bootEnv(dir, map[string]string{"DEMO_MODE": "true", "DEMO_RESET_HOURS": "0"}))
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan bool, 1)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			if strings.Contains(sc.Text(), "Server listening") {
				ready <- true
				for sc.Scan() {
				}
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			cmd.Wait()
			t.Fatal("demo boot exited before listening")
		}
	case <-time.After(90 * time.Second):
		cmd.Process.Kill()
		t.Fatal("demo boot did not listen within 90s")
	}
	cmd.Process.Signal(os.Interrupt)
	cmd.Wait()
	n := countUsers(t, filepath.Join(dir, "alliance.db"))
	if n != 6 {
		t.Errorf("seeded database has %d users, want 6", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "uploads", "demo_2.xlsx")); err != nil {
		t.Errorf("sample files not written: %v", err)
	}

	// A storage path that is a file: the seed cannot write its sample files.
	dir2 := t.TempDir()
	blocker := filepath.Join(dir2, "not-a-dir")
	os.WriteFile(blocker, []byte("x"), 0644)
	outb, err := bootCmd(t, bootEnv(dir2, map[string]string{"DEMO_MODE": "true", "STORAGE_PATH": blocker})).CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !bytes.Contains(outb, []byte("Demo seed failed")) {
		t.Errorf("failing seed: err = %v\n%s", err, outb)
	}
}

func countUsers(t *testing.T, path string) int {
	t.Helper()
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var n int
	conn.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n
}
