package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Every unauthenticated, token-bearing endpoint must be throttled per IP. All four guard a
// secret that is only as strong as the number of guesses an attacker can make, so an
// unthrottled one is brute-forceable at line speed.
//
// The two GET pages matter as much as the POSTs and are easy to overlook: they report whether
// a token is valid (rendering the member's/user's name, or an "expired" notice), which makes
// them guessing oracles — and CHEAPER to attack than the claims, because they pay no bcrypt
// cost and need no request body. Throttling only the POSTs would leave the cheap path open.
//
// The handlers are invoked here only on their throttled path, so this test needs no database
// and no templates: the limiter check is the first thing each one does, and a denied request
// returns before any other work.
func TestUnauthenticatedTokenEndpointsAreRateLimited(t *testing.T) {
	cases := []struct {
		name    string
		ip      string
		method  string
		path    string
		handler http.HandlerFunc
	}{
		{"invite page (GET)", "203.0.113.10", http.MethodGet, "/invite/deadbeef", showInvitePage},
		{"invite claim (POST)", "203.0.113.11", http.MethodPost, "/invite/deadbeef", claimInvite},
		{"reset page (GET)", "203.0.113.12", http.MethodGet, "/reset-password/deadbeef", showResetPasswordPage},
		{"reset claim (POST)", "203.0.113.13", http.MethodPost, "/reset-password/deadbeef", claimPasswordReset},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Each case uses its own IP: getLoginLimiter's map is package-level and persists
			// across tests, so sharing one would make these order-dependent.
			limiter := getLoginLimiter(tc.ip)
			for i := 0; i < loginLimiterBurst; i++ {
				if !limiter.Allow() {
					t.Fatalf("burst exhausted after %d of %d — limiter configuration changed", i, loginLimiterBurst)
				}
			}

			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("X-Forwarded-For", tc.ip)
			rr := httptest.NewRecorder()
			tc.handler(rr, req)

			if rr.Code != http.StatusTooManyRequests {
				t.Errorf("got status %d, want %d — this endpoint is not throttled, so its token is brute-forceable",
					rr.Code, http.StatusTooManyRequests)
			}
		})
	}
}

// The throttle is per IP, not global: exhausting one attacker's budget must not lock out
// everyone else trying to accept a legitimate invite at the same time.
func TestLoginLimiterIsPerIP(t *testing.T) {
	attacker, bystander := "203.0.113.20", "203.0.113.21"

	l := getLoginLimiter(attacker)
	for i := 0; i < loginLimiterBurst; i++ {
		l.Allow()
	}
	if l.Allow() {
		t.Fatalf("attacker IP still allowed after exhausting a burst of %d", loginLimiterBurst)
	}

	if !getLoginLimiter(bystander).Allow() {
		t.Error("a different IP was denied — the limiter is global, not per-IP, so one attacker " +
			"would deny service to every legitimate invite and reset in flight")
	}
}
