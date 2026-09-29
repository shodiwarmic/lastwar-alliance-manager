package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withProxyCount sets trustedProxyCount for one test and restores it afterwards.
func withProxyCount(t *testing.T, n int) {
	t.Helper()
	old := trustedProxyCount
	trustedProxyCount = n
	t.Cleanup(func() { trustedProxyCount = old })
}

func TestGetClientIP(t *testing.T) {
	cases := []struct {
		name   string
		count  int
		remote string
		xff    []string
		want   string
	}{
		{"no proxy ignores the header", 0, "198.51.100.7:5000", []string{"203.0.113.1"}, "198.51.100.7"},
		{"one proxy, one hop", 1, "172.18.0.1:5000", []string{"203.0.113.1"}, "203.0.113.1"},
		{"one proxy ignores a client-sent prefix", 1, "172.18.0.1:5000", []string{"6.6.6.6, 203.0.113.1"}, "203.0.113.1"},
		{"two proxies take the second from the right", 2, "172.18.0.1:5000", []string{"203.0.113.1, 192.0.2.50"}, "203.0.113.1"},
		{"two header lines are one list", 1, "172.18.0.1:5000", []string{"6.6.6.6", "203.0.113.1"}, "203.0.113.1"},
		{"empty entries are dropped", 1, "172.18.0.1:5000", []string{"6.6.6.6, , 203.0.113.1 ,"}, "203.0.113.1"},
		{"one proxy, no header falls back to the peer", 1, "172.18.0.1:5000", nil, "172.18.0.1"},
		{"fewer hops than proxies falls back to the peer", 3, "172.18.0.1:5000", []string{"203.0.113.1"}, "172.18.0.1"},
		{"bracketed IPv6 peer", 0, "[::1]:1234", nil, "::1"},
		{"X-Real-IP is never read", 0, "198.51.100.7:5000", nil, "198.51.100.7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withProxyCount(t, c.count)
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = c.remote
			for _, v := range c.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			r.Header.Set("X-Real-IP", "6.6.6.6")
			if got := getClientIP(r); got != c.want {
				t.Errorf("getClientIP = %q, want %q", got, c.want)
			}
		})
	}
}

func TestParseTrustedProxyCount(t *testing.T) {
	cases := []struct {
		raw        string
		production bool
		want       int
	}{
		{"", true, 1},
		{"", false, 0},
		{"0", true, 0},
		{"2", false, 2},
		{" 1 ", false, 1},
		{"-1", true, 1},
		{"two", false, 0},
	}
	for _, c := range cases {
		if got := parseTrustedProxyCount(c.raw, c.production); got != c.want {
			t.Errorf("parseTrustedProxyCount(%q, %v) = %d, want %d", c.raw, c.production, got, c.want)
		}
	}
}

// TestLoginLimiterIgnoresForgedForwardedFor reruns the reproduction from
// lastwar-private-docs#62: a script rotating the leftmost X-Forwarded-For entry used to get
// a fresh limiter bucket on every attempt. The limiter runs before the body is decoded, so
// an empty body is enough — five 400s, then 429s.
func TestLoginLimiterIgnoresForgedForwardedFor(t *testing.T) {
	cases := []struct {
		name   string
		count  int
		remote string
		xff    func(i int) string
		key    string
	}{
		{"no proxy", 0, "198.51.100.62:4000", func(i int) string { return fmt.Sprintf("10.0.0.%d", i) }, "198.51.100.62"},
		{"behind one proxy", 1, "172.18.0.62:4000", func(i int) string { return fmt.Sprintf("10.0.0.%d, 203.0.113.62", i) }, "203.0.113.62"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withProxyCount(t, c.count)
			loginLimiters.Delete(c.key)
			t.Cleanup(func() { loginLimiters.Delete(c.key) })
			for i := 1; i <= 8; i++ {
				r := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(""))
				r.RemoteAddr = c.remote
				r.Header.Set("X-Forwarded-For", c.xff(i))
				w := httptest.NewRecorder()
				login(w, r)
				want := http.StatusBadRequest
				if i > loginLimiterBurst {
					want = http.StatusTooManyRequests
				}
				if w.Code != want {
					t.Fatalf("request %d: status %d, want %d", i, w.Code, want)
				}
			}
		})
	}
}
