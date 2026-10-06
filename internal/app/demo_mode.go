// demo_mode.go - DEMO_MODE=true: the public demo (docs/DEMO.md). The ordinary image,
// seeded at boot with the fictional alliance (internal/demo), where every visitor signs
// in through a "Try as …" button. What a visitor must not reach — other visitors' sign-in
// state, the volunteer LastRank service, the instance's disk, credentials — is blocked at
// route registration with demoBlock; everything else is the real app.

package app

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/time/rate"

	"lastwar-alliance/internal/demo"
)

// demoMode reports whether this process is the public demo. Read per call (it is one
// getenv) so tests can toggle it.
func demoMode() bool { return os.Getenv("DEMO_MODE") == "true" }

// demoBlockedMessage is the body of every refusal, and the sentence global.js's demo
// interceptor recognises to toast it in place of the page's own error.
const demoBlockedMessage = "This action is disabled in the demo."

// demoBlock refuses a route in demo mode and is transparent otherwise. Writes only: every
// read stays available, so a page still renders and only its action is refused.
func demoBlock(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if demoMode() {
			http.Error(w, demoBlockedMessage, http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// demoSecurityHeaders is deploy/Caddyfile's app-site header block, verbatim. The demo
// runs on Cloud Run with no Caddy in front, so in demo mode the app sets them itself.
// TestDemoHeadersMatchTheCaddyfile compares this list with the Caddyfile, so the two
// cannot drift; generalising it to every proxy-less install is a separate decision
// (https://github.com/shodiwarmic/lastwar-private-docs/issues/186). The Collabora
// placeholder becomes DEMO_FIXTURES_ORIGIN.
var demoSecurityHeaders = [][2]string{
	{"X-Content-Type-Options", "nosniff"},
	{"X-Frame-Options", "SAMEORIGIN"},
	{"X-XSS-Protection", "1; mode=block"},
	{"Referrer-Policy", "strict-origin-when-cross-origin"},
	{"Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload"},
	{"Content-Security-Policy", "default-src 'self'; script-src 'self' https://cdn.jsdelivr.net; style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://fonts.googleapis.com; img-src 'self' data: https://lastwar-cdn.akamaized.net https://lastwar-cdn.lastwarapp.net; font-src 'self' https://fonts.gstatic.com; connect-src 'self' https://__COLLABORA_DOMAIN__; frame-src https://__COLLABORA_DOMAIN__; frame-ancestors 'none';"},
}

// demoHeaderValue substitutes the fixtures origin for the Caddyfile's Collabora
// placeholder.
func demoHeaderValue(v, fixturesOrigin string) string {
	return strings.ReplaceAll(v, "https://__COLLABORA_DOMAIN__", fixturesOrigin)
}

// demoHeaders sets the security headers in demo mode.
func demoHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if demoMode() {
			origin := os.Getenv("DEMO_FIXTURES_ORIGIN")
			for _, h := range demoSecurityHeaders {
				w.Header().Set(h[0], demoHeaderValue(h[1], origin))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// demoResetHours is how long the demo runs before it restarts itself: DEMO_RESET_HOURS,
// default 6, 0 disables. A fraction is accepted (a test sets 0.05).
func demoResetHours() float64 {
	raw := strings.TrimSpace(os.Getenv("DEMO_RESET_HOURS"))
	if raw == "" {
		return 6
	}
	h, err := strconv.ParseFloat(raw, 64)
	if err != nil || h < 0 {
		slog.Error("DEMO_RESET_HOURS must be a non-negative number; using 6", "value", raw)
		return 6
	}
	return h
}

// startDemoReset is the reset: after the interval the process sends itself SIGTERM, the
// ordinary shutdown path drains, and the next request on Cloud Run starts a fresh
// instance on an empty disk, which seeds itself at boot. No in-process database swap
// under the single connection. With min-instances 0 an idle instance is recycled sooner,
// so the interval is a ceiling, not a cadence.
func startDemoReset() {
	h := demoResetHours()
	if h == 0 {
		slog.Info("Demo reset disabled (DEMO_RESET_HOURS=0)")
		return
	}
	d := time.Duration(h * float64(time.Hour))
	slog.Info("Demo resets itself", "after", d.String())
	time.AfterFunc(d, func() {
		slog.Info("Demo reset: restarting for a fresh database")
		syscall.Kill(os.Getpid(), syscall.SIGTERM)
	})
}

// seedDemoAtBoot seeds an empty database in demo mode. A failure is returned for Main to
// exit on: an unseeded demo would boot unclaimed and serve a setup page whose key nobody
// can read, where a failed revision is visible on Cloud Run.
func seedDemoAtBoot() error {
	seeded, _, err := demo.SeedIfEmpty(db.DB, demo.Options{
		UploadsDir: getStoragePath(),
		WorkerURL:  os.Getenv("DEMO_FIXTURES_URL"),
		Password:   os.Getenv("DEMO_PASSWORD"),
	})
	if err != nil {
		return err
	}
	if seeded {
		slog.Info("Demo database seeded", "alliance", demo.AllianceName)
	}
	return nil
}

// The demo login's own limiter: the same 12-second refill as the web login, but a burst
// of 12, so a visitor can try all six accounts in a minute.
const demoLoginBurst = 12

var demoLoginLimiters sync.Map

func getDemoLoginLimiter(ip string) *rate.Limiter {
	if l, ok := demoLoginLimiters.Load(ip); ok {
		return l.(*rate.Limiter)
	}
	l, _ := demoLoginLimiters.LoadOrStore(ip, rate.NewLimiter(rate.Every(loginLimiterInterval), demoLoginBurst))
	return l.(*rate.Limiter)
}

// demoLogin serves POST /api/demo/login {account}: signs the browser in as one of the
// six seeded accounts. Registered only in demo mode. The accounts' password is a
// deployment secret that appears in no page; these buttons are the only sign-in path a
// visitor is given.
func demoLogin(w http.ResponseWriter, r *http.Request) {
	if !getDemoLoginLimiter(getClientIP(r)).Allow() {
		http.Error(w, "Too many sign-ins. Please wait a moment.", http.StatusTooManyRequests)
		return
	}
	var body struct {
		Account string `json:"account"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	var acct *demo.Account
	for _, a := range demo.Manifest().Accounts {
		if a.Username == body.Account {
			acct = &a
			break
		}
	}
	if acct == nil {
		http.Error(w, "Unknown demo account", http.StatusBadRequest)
		return
	}
	user := loadUserFromDB(acct.UserID)
	if user == nil || user.Username != acct.Username {
		// The account was deactivated or removed in this instance; the next reset restores it.
		http.Error(w, "That demo account is unavailable until the demo resets.", http.StatusConflict)
		return
	}
	establishSession(w, r, user.ID, user.Username, user.MemberID, user.IsAdmin)
	writeJSON(w, map[string]string{"message": "Login successful", "username": user.Username})
}

// demoWhoami serves GET /api/demo/whoami: how the app resolves the caller's address,
// for setting TRUSTED_PROXY_COUNT on a new deploy (docs/DEMO.md). Demo mode only.
func demoWhoami(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"client_ip":           getClientIP(r),
		"remote_addr":         r.RemoteAddr,
		"x_forwarded_for":     r.Header.Get("X-Forwarded-For"),
		"trusted_proxy_count": trustedProxyCount,
	})
}
