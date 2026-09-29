package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
)

const setupTestPassword = "Setup-Pass-2026!"

// setupSetupTestDB gives each test a fresh migrated database in its own directory — the setup
// key lives beside the database — with no users, and runs initSetupState as Main() does.
func setupSetupTestDB(t *testing.T) (keyPath string) {
	t.Helper()
	setupNameMatchTestDB(t)
	if store == nil {
		initSessionStore()
	}
	prev := installUnclaimed.Load()
	t.Cleanup(func() { installUnclaimed.Store(prev) })
	if err := initSetupState(); err != nil {
		t.Fatalf("initSetupState: %v", err)
	}
	return setupKeyPath()
}

func readSetupKey(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read setup key: %v", err)
	}
	return strings.TrimSpace(string(b))
}

// setupClaimRequest builds a claim from loopback, so trackLogin's geolocation short-circuits
// instead of calling ip-api.com, and from a peer of its own so the shared login limiter
// never carries over between tests.
func setupClaimRequest(key, username string, peer string) *http.Request {
	return setupClaimRequestWith(map[string]any{"setup_key": key, "username": username}, peer)
}

// setupClaimRequestWith fills in a valid password and alliance identity, then applies fields.
func setupClaimRequestWith(fields map[string]any, peer string) *http.Request {
	payload := map[string]any{
		"password": setupTestPassword, "confirm_password": setupTestPassword,
		"alliance_name": "Setup Test Alliance", "alliance_tag": "STST", "server_number": 1712,
	}
	for k, v := range fields {
		payload[k] = v
	}
	body, _ := json.Marshal(payload)
	r := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(string(body)))
	r.RemoteAddr = peer
	loginLimiters.Delete(strings.Split(peer, ":")[0])
	return r
}

// waitForLoginRow waits for trackLogin's goroutine, which writes after the handler returns;
// letting the test end first would close the database under it.
func waitForLoginRow(t *testing.T, username string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM login_sessions WHERE username = ?`, username).Scan(&n)
		if n > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no login_sessions row for %q", username)
}

func TestInitSetupStateWritesAKeyOnAnEmptyInstall(t *testing.T) {
	path := setupSetupTestDB(t)
	if !installUnclaimed.Load() {
		t.Fatal("an install with no users is not marked unclaimed")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("no setup key written: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("setup key mode %v, want 0600", info.Mode().Perm())
	}
	if key := readSetupKey(t, path); !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(key) {
		t.Errorf("setup key %q is not 64 hex characters", key)
	}

	// A restart inside the TTL keeps the key install.sh printed; one after it replaces it.
	first := readSetupKey(t, path)
	if err := initSetupState(); err != nil {
		t.Fatal(err)
	}
	if readSetupKey(t, path) != first {
		t.Error("a restart inside the TTL replaced a live setup key")
	}
	old := time.Now().Add(-25 * time.Hour)
	os.Chtimes(path, old, old)
	if err := initSetupState(); err != nil {
		t.Fatal(err)
	}
	if readSetupKey(t, path) == first {
		t.Error("a restart after the TTL kept an expired setup key")
	}
}

func TestSetupKeyLivesBesideTheDatabase(t *testing.T) {
	t.Setenv("DATABASE_PATH", "/app/data/alliance.db")
	if got := setupKeyPath(); got != "/app/data/setup-key" {
		t.Errorf("setupKeyPath = %q", got)
	}
	t.Setenv("DATABASE_PATH", "")
	if got, want := setupKeyPath(), filepath.Dir(databasePath()); filepath.Dir(got) != want {
		t.Errorf("with DATABASE_PATH unset the key is at %q, not beside the database in %q", got, want)
	}
}

func TestInitSetupStateRemovesAStaleKeyOnceClaimed(t *testing.T) {
	path := setupSetupTestDB(t)
	if _, err := db.Exec(`INSERT INTO users (username, password, is_admin) VALUES ('owner', 'x', 1)`); err != nil {
		t.Fatal(err)
	}
	if err := initSetupState(); err != nil {
		t.Fatal(err)
	}
	if installUnclaimed.Load() {
		t.Error("an install with a user is still marked unclaimed")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("leftover setup key not removed: %v", err)
	}
}

func TestSetupGate(t *testing.T) {
	setupSetupTestDB(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	gate := setupGate(next)

	serve := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		gate.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}

	for _, p := range []string{"/", "/members", "/login", "/api/members"} {
		if w := serve(p); w.Code != http.StatusFound || w.Header().Get("Location") != "/setup" {
			t.Errorf("unclaimed %s: %d → %q, want 302 → /setup", p, w.Code, w.Header().Get("Location"))
		}
	}
	for _, p := range []string{"/setup", "/api/setup", "/styles.css", "/setup.js"} {
		if w := serve(p); w.Code != http.StatusTeapot {
			t.Errorf("unclaimed %s was not passed through (status %d)", p, w.Code)
		}
	}
	if w := serve("/api/mobile/members"); w.Code != http.StatusServiceUnavailable ||
		!strings.Contains(w.Body.String(), "setup required") {
		t.Errorf("unclaimed mobile API: %d %q, want 503 with a JSON error", w.Code, w.Body.String())
	}
	// A traversal attempt is not a static file, so it is redirected like any other page.
	if w := serve("/../migrations/001_baseline.sql"); w.Code != http.StatusFound {
		t.Errorf("traversal path passed the gate (status %d)", w.Code)
	}

	installUnclaimed.Store(false)
	for _, p := range []string{"/", "/members", "/api/mobile/members"} {
		if w := serve(p); w.Code != http.StatusTeapot {
			t.Errorf("claimed %s was not passed through (status %d)", p, w.Code)
		}
	}
}

func TestSetupPageRedirectsOnceClaimed(t *testing.T) {
	setupSetupTestDB(t)
	installUnclaimed.Store(false)
	w := httptest.NewRecorder()
	showSetupPage(w, httptest.NewRequest(http.MethodGet, "/setup", nil))
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/login" {
		t.Errorf("claimed /setup: %d → %q, want 302 → /login", w.Code, w.Header().Get("Location"))
	}
}

func TestSetupClaimCreatesTheFirstAdministrator(t *testing.T) {
	path := setupSetupTestDB(t)
	key := readSetupKey(t, path)

	w := httptest.NewRecorder()
	// Surrounding whitespace, as a paste from a terminal carries.
	claimSetup(w, setupClaimRequest("  "+key+"\n", "founder", "127.0.0.1:7001"))
	if w.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", w.Code, w.Body.String())
	}
	waitForLoginRow(t, "founder")

	var isAdmin, isActive, force int
	if err := db.QueryRow(`SELECT is_admin, is_active, force_password_change FROM users WHERE username = 'founder'`).
		Scan(&isAdmin, &isActive, &force); err != nil {
		t.Fatalf("no user created: %v", err)
	}
	if isAdmin != 1 || isActive != 1 || force != 0 {
		t.Errorf("is_admin=%d is_active=%d force_password_change=%d, want 1 1 0", isAdmin, isActive, force)
	}
	var history int
	db.QueryRow(`SELECT COUNT(*) FROM password_history ph JOIN users u ON u.id = ph.user_id WHERE u.username = 'founder'`).Scan(&history)
	if history != 1 {
		t.Errorf("password_history rows = %d, want 1", history)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("setup key not deleted after the claim: %v", err)
	}
	if installUnclaimed.Load() {
		t.Error("the gate is still closed after the claim")
	}
	var sensitive int
	if err := db.QueryRow(`SELECT is_sensitive FROM activity_log WHERE entity_type = 'user' AND action = 'created' AND entity_name = 'founder'`).
		Scan(&sensitive); err != nil || sensitive != 1 {
		t.Errorf("activity row: sensitive=%d err=%v, want a sensitive 'created user' row", sensitive, err)
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), "session=") {
		t.Error("the claim did not log the new administrator in")
	}

	// The key is single-use: a second claim finds the install set up.
	w2 := httptest.NewRecorder()
	claimSetup(w2, setupClaimRequest(key, "second", "127.0.0.1:7002"))
	if w2.Code != http.StatusConflict {
		t.Errorf("second claim: %d, want 409", w2.Code)
	}
}

func TestSetupClaimRejectsAWrongKey(t *testing.T) {
	path := setupSetupTestDB(t)
	w := httptest.NewRecorder()
	claimSetup(w, setupClaimRequest(strings.Repeat("0", 64), "intruder", "127.0.0.1:7003"))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong key: %d, want 401", w.Code)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("a failed claim removed the setup key: %v", err)
	}
	var users int
	db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users)
	if users != 0 || !installUnclaimed.Load() {
		t.Errorf("a failed claim changed state: users=%d unclaimed=%v", users, installUnclaimed.Load())
	}
}

func TestSetupClaimRefusesAnExpiredOrMissingKey(t *testing.T) {
	path := setupSetupTestDB(t)
	key := readSetupKey(t, path)
	old := time.Now().Add(-25 * time.Hour)
	os.Chtimes(path, old, old)

	w := httptest.NewRecorder()
	claimSetup(w, setupClaimRequest(key, "late", "127.0.0.1:7004"))
	if w.Code != http.StatusGone || !strings.Contains(w.Body.String(), "expired") {
		t.Errorf("expired key: %d %q, want 410 naming expiry", w.Code, w.Body.String())
	}

	os.Remove(path)
	w = httptest.NewRecorder()
	claimSetup(w, setupClaimRequest(key, "late", "127.0.0.1:7005"))
	if w.Code != http.StatusGone || !strings.Contains(w.Body.String(), "Restart") {
		t.Errorf("missing key: %d %q, want 410 telling the operator to restart", w.Code, w.Body.String())
	}
}

// The setup POST sits inside the CSRF-protected router (only WOPI and the mobile API bypass
// it), so the page has to carry the token and csrf.js has to find it. A miss would be a 403
// on the only way into a fresh install, so this runs the real round trip: render the page,
// lift the token and cookie off it, post with them.
func TestSetupClaimPassesCSRF(t *testing.T) {
	path := setupSetupTestDB(t)
	key := readSetupKey(t, path)

	router := mux.NewRouter()
	router.HandleFunc("/setup", showSetupPage).Methods("GET")
	router.HandleFunc("/api/setup", claimSetup).Methods("POST")
	handler := setupGate(csrf.Protect([]byte("0123456789abcdef0123456789abcdef"), csrf.Secure(false), csrf.Path("/"))(router))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/setup")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	m := regexp.MustCompile(`name="gorilla.csrf.Token" value="([^"]+)"`).FindSubmatch(page)
	if m == nil {
		t.Fatalf("the setup page carries no CSRF token:\n%s", page)
	}
	token := strings.ReplaceAll(string(m[1]), "&#43;", "+")

	body, _ := json.Marshal(map[string]any{
		"setup_key": key, "username": "csrfowner",
		"password": setupTestPassword, "confirm_password": setupTestPassword,
		"alliance_name": "Setup Test Alliance", "alliance_tag": "STST",
	})
	loginLimiters.Delete("127.0.0.1")
	origin := "https://" + strings.TrimPrefix(srv.URL, "http://")

	// Without the token the claim is refused before it reaches the handler.
	bare, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/setup", strings.NewReader(string(body)))
	bare.Header.Set("Origin", origin)
	for _, c := range resp.Cookies() {
		bare.AddCookie(c)
	}
	refused, err := http.DefaultClient.Do(bare)
	if err != nil {
		t.Fatal(err)
	}
	refused.Body.Close()
	if refused.StatusCode != http.StatusForbidden {
		t.Fatalf("claim without a CSRF token: %d, want 403", refused.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/setup", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", token)
	// What a browser sends behind the Caddy install.sh sets up: TLS ends at the proxy, so the
	// app sees plain HTTP while the Origin says https — which is what gorilla/csrf assumes.
	req.Header.Set("Origin", origin)
	for _, c := range resp.Cookies() {
		req.AddCookie(c)
	}
	post, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := io.ReadAll(post.Body)
	post.Body.Close()
	if post.StatusCode != http.StatusOK {
		t.Fatalf("claim through CSRF: %d %s", post.StatusCode, msg)
	}
	waitForLoginRow(t, "csrfowner")
}

func TestSetupClaimRecordsTheAllianceIdentity(t *testing.T) {
	path := setupSetupTestDB(t)
	key := readSetupKey(t, path)

	w := httptest.NewRecorder()
	claimSetup(w, setupClaimRequestWith(map[string]any{
		"setup_key": key, "username": "identity",
		"alliance_name": "  Warmic  ", "alliance_tag": "WARMC", "server_number": 1712,
	}, "127.0.0.1:7010"))
	if w.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", w.Code, w.Body.String())
	}
	waitForLoginRow(t, "identity")
	var redirect map[string]string
	json.Unmarshal(w.Body.Bytes(), &redirect)
	if redirect["redirect"] != "/settings" {
		t.Errorf("redirect = %q, want /settings (game limits come next)", redirect["redirect"])
	}

	var name, tag string
	var server int
	if err := db.QueryRow(`SELECT alliance_name, alliance_tag, COALESCE(our_server_id, 0) FROM settings WHERE id = 1`).
		Scan(&name, &tag, &server); err != nil {
		t.Fatal(err)
	}
	if name != "Warmic" || tag != "WARMC" || server != 1712 {
		t.Errorf("settings = (%q, %q, %d), want (Warmic, WARMC, 1712)", name, tag, server)
	}
	// Rule 2 holds from the first minute: the tag just entered is recognised as ours.
	if !isOwnAlliance("", "warmc") {
		t.Error("isOwnAlliance does not recognise the tag entered at setup")
	}
	var logged int
	db.QueryRow(`SELECT COUNT(*) FROM activity_log WHERE entity_type = 'settings' AND action = 'updated' AND is_sensitive = 1`).Scan(&logged)
	if logged != 1 {
		t.Errorf("settings activity rows = %d, want 1", logged)
	}
}

func TestSetupClaimLeavesTheServerUnsetWhenBlank(t *testing.T) {
	path := setupSetupTestDB(t)
	w := httptest.NewRecorder()
	claimSetup(w, setupClaimRequestWith(map[string]any{
		"setup_key": readSetupKey(t, path), "username": "noserver", "server_number": 0,
	}, "127.0.0.1:7011"))
	if w.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", w.Code, w.Body.String())
	}
	waitForLoginRow(t, "noserver")
	var server any
	db.QueryRow(`SELECT our_server_id FROM settings WHERE id = 1`).Scan(&server)
	if server != nil {
		t.Errorf("our_server_id = %v, want NULL", server)
	}
}

func TestSetupClaimRequiresNameAndTag(t *testing.T) {
	path := setupSetupTestDB(t)
	key := readSetupKey(t, path)
	for _, c := range []struct {
		field, value string
	}{
		{"alliance_tag", ""},
		{"alliance_tag", "   "},
		{"alliance_name", ""},
		{"alliance_tag", strings.Repeat("T", setupAllianceTagMax+1)},
	} {
		w := httptest.NewRecorder()
		claimSetup(w, setupClaimRequestWith(map[string]any{"setup_key": key, "username": "partial", c.field: c.value}, "127.0.0.1:7012"))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s=%q: %d, want 400", c.field, c.value, w.Code)
		}
	}
	var users int
	db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users)
	if users != 0 {
		t.Errorf("a refused claim created %d user(s)", users)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("a refused claim removed the setup key: %v", err)
	}
}
