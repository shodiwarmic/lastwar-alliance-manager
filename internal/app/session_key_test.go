package app

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestResolveSessionKeyRefusesInProduction(t *testing.T) {
	if _, err := resolveSessionKey("", true); !errors.Is(err, errSessionKeyMissing) {
		t.Errorf("unset key in production: err = %v, want errSessionKeyMissing", err)
	}
	for _, prod := range []bool{true, false} {
		if _, err := resolveSessionKey("too-short", prod); err == nil {
			t.Errorf("short key (production=%v) accepted", prod)
		}
	}
	k, err := resolveSessionKey("", false)
	if err != nil {
		t.Fatalf("unset key in development: %v", err)
	}
	if len(k.store) != 32 || len(k.raw) < MinSessionKeyLen {
		t.Errorf("ephemeral key: store %d bytes, raw %d chars", len(k.store), len(k.raw))
	}
}

// Both key shapes resolve to what the old initSessionStore computed, and the token secret
// stays the raw string in both (O1).
func TestResolveSessionKeyShapes(t *testing.T) {
	want := bytes.Repeat([]byte{0xab}, 32)
	hexKey := hex.EncodeToString(want)
	k, err := resolveSessionKey(hexKey, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(k.store, want) {
		t.Errorf("hex key: store = %x, want the decoded 32 bytes", k.store)
	}
	if k.raw != hexKey {
		t.Errorf("hex key: raw = %q, want the variable verbatim", k.raw)
	}

	rawKey := "this-is-a-raw-session-key-of-forty-chars"
	k, err = resolveSessionKey(rawKey, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(k.store, []byte(rawKey)[:32]) {
		t.Errorf("raw key: store = %q, want its first 32 bytes", k.store)
	}
	if k.raw != rawKey {
		t.Errorf("raw key: raw = %q", k.raw)
	}
}

// A mobile token and a WOPI token signed the way the code signed them before the resolver
// existed — HS256 over []byte(os.Getenv("SESSION_KEY")) — still verify after it, so an
// upgrade invalidates no scanner or collector login and no open Collabora session.
func TestTokensSignedWithTheRawKeyStillVerify(t *testing.T) {
	setupNameMatchTestDB(t)
	hexKey := hex.EncodeToString(bytes.Repeat([]byte{0x5c}, 32))
	prev := sessionKeys
	t.Cleanup(func() { sessionKeys = prev })
	k, err := resolveSessionKey(hexKey, true)
	if err != nil {
		t.Fatal(err)
	}
	sessionKeys = k
	if _, err := db.Exec(`INSERT INTO users (id, username, password, is_active) VALUES (9, 'scanner', 'x', 1)`); err != nil {
		t.Fatal(err)
	}

	issued := jwt.NewNumericDate(time.Now().Add(time.Minute)) // after any password_changed_at
	mobile, err := jwt.NewWithClaims(jwt.SigningMethodHS256, MobileTokenClaims{
		UserID: 9, Username: "scanner",
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt: issued, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer: "lastwar-alliance-manager",
		},
	}).SignedString([]byte(hexKey))
	if err != nil {
		t.Fatal(err)
	}
	reached := false
	h := mobileBearerMiddleware(func(w http.ResponseWriter, r *http.Request) { reached = true })
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/members", nil)
	req.Header.Set("Authorization", "Bearer "+mobile)
	w := httptest.NewRecorder()
	h(w, req)
	if !reached {
		t.Errorf("mobile token signed with the raw key rejected: %d %s", w.Code, w.Body.String())
	}

	wopi, err := jwt.NewWithClaims(jwt.SigningMethodHS256, WOPIClaims{
		UserID: 9, Username: "scanner", FileID: 1,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	}).SignedString([]byte(hexKey))
	if err != nil {
		t.Fatal(err)
	}
	reached = false
	h = wopiAuthMiddleware(func(w http.ResponseWriter, r *http.Request) { reached = true })
	w = httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodGet, "/wopi/files/1?access_token="+wopi, nil))
	if !reached {
		t.Errorf("WOPI token signed with the raw key rejected: %d %s", w.Code, w.Body.String())
	}
}

// mobileLogin is behind the same per-IP limiter as the web login: burst of five.
func TestMobileLoginIsRateLimited(t *testing.T) {
	ip := "10.250.0.7" // its own bucket; a private address so nothing is geolocated
	var codes []int
	for i := 0; i < 6; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/mobile/login", strings.NewReader("not json"))
		req.RemoteAddr = ip + ":40000"
		w := httptest.NewRecorder()
		mobileLogin(w, req)
		codes = append(codes, w.Code)
	}
	for i, c := range codes[:5] {
		if c == http.StatusTooManyRequests {
			t.Fatalf("attempt %d already limited: %v", i+1, codes)
		}
	}
	if codes[5] != http.StatusTooManyRequests {
		t.Errorf("sixth attempt: %d, want 429 (%v)", codes[5], codes)
	}
}

// bootHelperArg marks the re-run test binary as the boot helper. An argument, not an
// environment variable: the env-var documentation check treats every os.Getenv key as
// app configuration.
const bootHelperArg = "app-boot-helper"

// TestBootHelperProcess is not a test: the boot tests below re-run the test binary with
// bootHelperArg so that Main() runs in a process of its own, where os.Exit is safe.
func TestBootHelperProcess(t *testing.T) {
	if !slices.Contains(os.Args, bootHelperArg) {
		t.Skip("helper process for the boot tests")
	}
	Main()
	os.Exit(0)
}

// bootEnv is the environment for a Main() subprocess: the parent's, with every variable
// the repository's .env might set given an explicit value — godotenv never overrides a
// variable that is already present, even an empty one.
func bootEnv(dir string, overrides map[string]string) []string {
	vals := map[string]string{
		"PORT":             "0",
		"SESSION_KEY":      "",
		"PRODUCTION":       "",
		"HTTPS":            "",
		"TRUSTED_ORIGINS":  "",
		"DATABASE_PATH":    filepath.Join(dir, "alliance.db"),
		"STORAGE_PATH":     filepath.Join(dir, "uploads"),
		"OCR_ARCHIVE_DIR":  "",
		"COLLABORA_DOMAIN": "",
		"DEMO_MODE":        "",
	}
	for k, v := range overrides {
		vals[k] = v
	}
	var env []string
	for _, kv := range os.Environ() {
		if _, ok := vals[strings.SplitN(kv, "=", 2)[0]]; !ok {
			env = append(env, kv)
		}
	}
	for k, v := range vals {
		env = append(env, k+"="+v)
	}
	return env
}

func bootCmd(t *testing.T, env []string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestBootHelperProcess$", "--", bootHelperArg)
	cmd.Env = env
	return cmd
}

func TestBootRefusesWithoutSessionKeyInProduction(t *testing.T) {
	for name, key := range map[string]string{"unset": "", "short": "short-key"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			out, err := bootCmd(t, bootEnv(dir, map[string]string{
				"PRODUCTION": "true", "SESSION_KEY": key,
			})).CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Fatalf("boot: err = %v, want exit status 1\n%s", err, out)
			}
			if !strings.Contains(string(out), "Refusing to start") {
				t.Errorf("no refusal line in the log:\n%s", out)
			}
			if _, err := os.Stat(filepath.Join(dir, "alliance.db")); !os.IsNotExist(err) {
				t.Errorf("the database was opened before the refusal (stat err = %v)", err)
			}
		})
	}
}

func TestBootWithoutProductionUsesAnEphemeralKey(t *testing.T) {
	if testing.Short() {
		t.Skip("boots the whole server")
	}
	port := freePort(t)
	cmd := bootCmd(t, bootEnv(t.TempDir(), map[string]string{"PORT": port}))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	listening := make(chan bool, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if strings.Contains(sc.Text(), "Server listening") {
				listening <- true
				break
			}
		}
		for sc.Scan() {
		}
		listening <- false
	}()
	select {
	case ok := <-listening:
		if !ok {
			cmd.Wait()
			t.Fatal("server exited before listening")
		}
	case <-time.After(60 * time.Second):
		cmd.Process.Kill()
		t.Fatal("server did not start within 60s")
	}
	// The log line is written as the server starts; signal only once it answers.
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := http.Get("http://127.0.0.1:" + port + "/login")
		if err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			cmd.Process.Kill()
			t.Fatalf("server never answered: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	cmd.Process.Signal(syscall.SIGTERM)
	if err := cmd.Wait(); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

// freePort returns a TCP port nothing is listening on at the moment of asking.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}
