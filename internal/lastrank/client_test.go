package lastrank

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// withServer points the package's unexported baseURL at a test server for one test.
// Only this package can do that, which is the point: the invariant the extraction
// exists to create is that no code outside can reach the transport at all.
func withServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	prev := baseURL
	baseURL = srv.URL
	t.Cleanup(func() {
		baseURL = prev
		srv.Close()
	})
	return srv
}

// withFastLimiter swaps the shared 1 req/sec limiter for an effectively unlimited one,
// for tests where pacing is not the subject.
func withFastLimiter(t *testing.T) {
	t.Helper()
	prev := limiter
	limiter = rate.NewLimiter(rate.Inf, 1)
	t.Cleanup(func() { limiter = prev })
}

// recorder counts requests by "METHOD path".
type recorder struct {
	mu   sync.Mutex
	hits []string
}

func (r *recorder) record(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hits = append(r.hits, req.Method+" "+req.URL.Path)
}

func (r *recorder) count(want string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, h := range r.hits {
		if h == want {
			n++
		}
	}
	return n
}

// playerHandler serves a player whose last_enriched_at is `age` in the past, and an
// enrich endpoint that returns the same player enriched.
func playerHandler(rec *recorder, age time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		p := Player{
			PublicID:       7,
			Name:           "Tester",
			LastEnrichedAt: time.Now().UTC().Add(-age).Format(time.RFC3339),
			EnrichStatus:   "cached",
		}
		if r.Method == http.MethodPost {
			p.EnrichStatus = "fetched"
		}
		json.NewEncoder(w).Encode(p)
	})
}

// TestPlayerBulkSkipsEnrichWhenFresh — the cheap path stays cheap. This is the cost
// model the scheduled sweep is built on: at the default 6h/21h settings three of the
// four daily ticks must cost zero enrich requests.
func TestPlayerBulkSkipsEnrichWhenFresh(t *testing.T) {
	withFastLimiter(t)
	rec := &recorder{}
	withServer(t, playerHandler(rec, 1*time.Hour))

	if _, err := PlayerBulk(context.Background(), 7, 21*time.Hour); err != nil {
		t.Fatalf("PlayerBulk: %v", err)
	}
	if got := rec.count("GET /v1/players/7"); got != 1 {
		t.Errorf("GET count = %d, want 1", got)
	}
	if got := rec.count("POST /v1/players/7/enrich"); got != 0 {
		t.Errorf("enrich count = %d, want 0 for a fresh record", got)
	}
}

// TestPlayerBulkEnrichesWhenStale — a record older than the window gets one live re-pull.
func TestPlayerBulkEnrichesWhenStale(t *testing.T) {
	withFastLimiter(t)
	rec := &recorder{}
	withServer(t, playerHandler(rec, 48*time.Hour))

	p, err := PlayerBulk(context.Background(), 7, 21*time.Hour)
	if err != nil {
		t.Fatalf("PlayerBulk: %v", err)
	}
	if p.EnrichStatus != "fetched" {
		t.Errorf("EnrichStatus = %q, want the enrich response", p.EnrichStatus)
	}
	if got := rec.count("GET /v1/players/7"); got != 1 {
		t.Errorf("GET count = %d, want 1", got)
	}
	if got := rec.count("POST /v1/players/7/enrich"); got != 1 {
		t.Errorf("enrich count = %d, want 1", got)
	}
}

// TestPlayerBulkClampsMaxAge is the guarantee that replaced the rejected predicate
// parameter: no caller — however it is configured — can make the bulk path enrich a
// member more often than MinEnrichAge.
func TestPlayerBulkClampsMaxAge(t *testing.T) {
	withFastLimiter(t)
	rec := &recorder{}
	withServer(t, playerHandler(rec, 10*time.Minute))

	if _, err := PlayerBulk(context.Background(), 7, 0); err != nil {
		t.Fatalf("PlayerBulk: %v", err)
	}
	if got := rec.count("POST /v1/players/7/enrich"); got != 0 {
		t.Errorf("enrich count = %d — maxAge 0 was not clamped to MinEnrichAge", got)
	}
}

// TestPlayerFreshEnrichesUnconditionally — the single-lookup strategy always re-pulls.
func TestPlayerFreshEnrichesUnconditionally(t *testing.T) {
	withFastLimiter(t)
	rec := &recorder{}
	withServer(t, playerHandler(rec, time.Minute))

	if _, err := PlayerFresh(context.Background(), 7); err != nil {
		t.Fatalf("PlayerFresh: %v", err)
	}
	if got := rec.count("POST /v1/players/7/enrich"); got != 1 {
		t.Errorf("enrich count = %d, want 1", got)
	}
	if got := rec.count("GET /v1/players/7"); got != 0 {
		t.Errorf("GET count = %d — a successful enrich needs no GET", got)
	}
}

// TestPlayerFreshFallsBackToGet — a failed enrich must still return data.
func TestPlayerFreshFallsBackToGet(t *testing.T) {
	withFastLimiter(t)
	rec := &recorder{}
	withServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		if r.Method == http.MethodPost {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(Player{PublicID: 7, Name: "Tester"})
	}))

	p, err := PlayerFresh(context.Background(), 7)
	if err != nil {
		t.Fatalf("PlayerFresh: %v", err)
	}
	if p.Name != "Tester" {
		t.Errorf("Name = %q, want the cached GET", p.Name)
	}
	if got := rec.count("POST /v1/players/7/enrich"); got != 1 {
		t.Errorf("enrich count = %d, want 1", got)
	}
	if got := rec.count("GET /v1/players/7"); got != 1 {
		t.Errorf("GET count = %d, want 1 fallback", got)
	}
}

// TestEveryRequestPathIsPaced proves that all three shapes of request — a cached GET, an
// enrich POST and a search — go through do and therefore through the shared limiter. A
// future code path that builds its own client fails this test rather than needing a grep
// to catch it.
func TestEveryRequestPathIsPaced(t *testing.T) {
	prev := limiter
	limiter = rate.NewLimiter(rate.Every(50*time.Millisecond), 1)
	t.Cleanup(func() { limiter = prev })

	rec := &recorder{}
	withServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		if r.URL.Path == "/v1/search" {
			json.NewEncoder(w).Encode(searchResponse{})
			return
		}
		json.NewEncoder(w).Encode(Player{PublicID: 7, LastEnrichedAt: "2000-01-01T00:00:00Z"})
	}))

	// Burn the limiter's initial token so all three calls below have to wait.
	if err := limiter.Wait(context.Background()); err != nil {
		t.Fatalf("prime limiter: %v", err)
	}

	start := time.Now()
	if _, err := PlayerBulk(context.Background(), 7, time.Hour); err != nil { // 1 GET + 1 enrich
		t.Fatalf("PlayerBulk: %v", err)
	}
	if _, err := SearchAllianceHits(context.Background(), "x"); err != nil { // 1 search
		t.Fatalf("SearchAllianceHits: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Errorf("three requests took %v — at one per 50ms they cannot be under 100ms, so something bypassed the limiter", elapsed)
	}
}

// TestRequestsCarryIdentifyingHeaders — probing lastrank.fun without a User-Agent trips
// Cloudflare's bot challenge, which answers with HTML rather than JSON.
func TestRequestsCarryIdentifyingHeaders(t *testing.T) {
	withFastLimiter(t)
	var ua, accept string
	withServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua, accept = r.Header.Get("User-Agent"), r.Header.Get("Accept")
		json.NewEncoder(w).Encode(Alliance{})
	}))

	if _, err := FetchAlliance(context.Background(), "abc"); err != nil {
		t.Fatalf("FetchAlliance: %v", err)
	}
	if ua != "alliance-manager/1.0 (+enrichment)" {
		t.Errorf("User-Agent = %q", ua)
	}
	if accept != "application/json" {
		t.Errorf("Accept = %q", accept)
	}
}

// TestUpstreamFailureIsWrapped — every non-200 must be identifiable as ErrUpstream so a
// handler can map it to a generic message.
func TestUpstreamFailureIsWrapped(t *testing.T) {
	withFastLimiter(t)
	withServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))

	_, err := FetchAlliance(context.Background(), "abc")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrUpstream) {
		t.Errorf("error %v does not wrap ErrUpstream", err)
	}
}
