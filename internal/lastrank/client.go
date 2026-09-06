// Package lastrank is a small, resilient client for the unofficial lastrank.fun
// JSON API (the `/v1/` paths).
//
// The API is undocumented and run by volunteers, so this package treats every
// response as best-effort enrichment: nullable fields are pointers, unknown fields
// are ignored, and every request shares one hard 1 req/sec throttle.
//
// # The invariant
//
// The base URL, the HTTP clients and the rate limiter are unexported and there is
// no way to issue a request that bypasses do — so no amount of code elsewhere in
// the application can talk to lastrank.fun outside the shared politeness budget.
// That is the reason this is a package rather than a file: in one `package main`
// the limiter was a package-level var any of eighty files could sidestep.
//
// Callers must never surface an error from here to an end user; log the detail and
// return a generic message.
package lastrank

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// baseURL, limiter and the two clients are unexported and package-level. Tests in
// this package (and only in this package) point baseURL at an httptest server and
// swap in a faster limiter; nothing outside can reach them.
var baseURL = "https://lastrank.fun"

// One global limiter shared across every lastrank call, so concurrent officer
// syncs still respect the 1 req/sec promise to the volunteer-run service.
var limiter = rate.NewLimiter(rate.Every(time.Second), 1)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// enrich re-derives the player from LastRank's most recent scan (NOT a live game
// query) and is much slower than a cached GET — give it a longer ceiling.
var enrichHTTP = &http.Client{Timeout: EnrichTimeout}

// EnrichTimeout is the ceiling on a single enrich request. Exported so a handler
// that wraps a bulk fetch in its own context can assert at COMPILE TIME that its
// deadline clears this one — a handler ceiling below it would cancel the very
// re-pull it asked for.
const EnrichTimeout = 25 * time.Second

// MinEnrichAge is the floor this package clamps PlayerBulk's maxAge to. The bulk
// path exists so a roster sweep is mostly cheap cached GETs; a caller passing a
// tiny (or zero) max age would turn it into an enrich per player, which is slow
// and abusive to a volunteer service. One enrich per player per hour is the most
// the bulk path will ever do, whatever it is asked for.
const MinEnrichAge = time.Hour

// ErrUpstream is returned for any non-200 / transport / decode failure.
// Handlers log the detail and return a generic message — never this error's text.
var ErrUpstream = errors.New("lastrank upstream error")

// ErrBadInput is returned when an id/URL cannot be parsed.
var ErrBadInput = errors.New("could not parse a LastRank alliance id/URL")

// --- Wire types ---

// AllianceMember is one member of an alliance record.
type AllianceMember struct {
	PublicID     int     `json:"public_id"`
	Name         string  `json:"name"`
	Country      *string `json:"country"`
	Power        int64   `json:"power"`
	HeroPower    *int64  `json:"hero_power"`
	AllianceRank *int    `json:"alliance_rank"`
	BaseLevel    *int    `json:"base_level"`
}

// Alliance is the GET /v1/alliances/{id} record.
//
// Members is SPARSE upstream and that is not a bug: it holds only players LastRank
// has a record for, a function of who has been looked up there rather than of the
// alliance's real size. Callers must handle an empty Members on an alliance whose
// CurMember is large.
type Alliance struct {
	AllianceID string  `json:"alliance_id"`
	Abbr       string  `json:"abbr"`
	Name       string  `json:"name"`
	ServerID   int     `json:"server_id"`
	Fightpower int64   `json:"fightpower"`
	ArmyKill   int64   `json:"army_kill"`
	CurMember  int     `json:"cur_member"`
	MaxMember  int     `json:"max_member"`
	Country    *string `json:"country"`
	// When lastrank scanned this alliance from the game (see Player.LastSeenAt).
	// The staleness guard and every history row's recorded_at key on it.
	LastSeenAt string           `json:"last_seen_at"`
	Members    []AllianceMember `json:"members"`
}

// Player is the per-player record returned by both GET /v1/players/{id} and
// POST /v1/players/{id}/enrich.
type Player struct {
	PublicID     int     `json:"public_id"`
	Name         string  `json:"name"`
	Country      *string `json:"country"`
	AllianceID   *string `json:"alliance_id"`
	AllianceAbbr *string `json:"alliance_abbr"`
	AllianceName *string `json:"alliance_name"`
	AllianceRank *int    `json:"alliance_rank"`
	HomeServerID int     `json:"home_server_id"`
	SrcServerID  int     `json:"src_server_id"`
	Power        int64   `json:"power"`
	HeroPower    *int64  `json:"hero_power"`
	ArmyKill     int64   `json:"army_kill"`
	BaseLevel    *int    `json:"base_level"`
	CareerLv     int     `json:"career_lv"`
	CareerType   int     `json:"career_type"` // profession code; maps via CareerTypeLabels
	// When lastrank SCANNED this player from the game — the as-of date of the data, and
	// what every history write uses as recorded_at. It is NOT the player's last login:
	// members of one alliance are scanned together, so their timestamps cluster within
	// seconds of each other and of the alliance's own. Never surface it as "last active";
	// doing so invites writing off a live player on the strength of scan scheduling.
	LastSeenAt string `json:"last_seen_at"`
	// When the enrich endpoint was last CALLED on this player — a record of our own
	// polling, not of the game. Only the freshness filter reads it.
	LastEnrichedAt string `json:"last_enriched_at"`
	// "cached" | "fetched" | "gated" | "unavailable". Only "fetched" means a live
	// re-pull actually happened — the scheduled sweep's freshness stamp keys on it,
	// because stamping on "gated" would starve that member out of future sweeps.
	EnrichStatus     string `json:"enrich_status"`
	PhotoURL         string `json:"photo_url"`
	PhotoURLFailover string `json:"photo_url_failover"`
}

// AllianceRow is one row of /v1/global/alliances (the search/list endpoint). It carries
// power/kills directly (unlike a SearchHit), so a picker can show them without a second call.
//
// PowerRank/KillsRank are the alliance's true position on its server's ladder. CapturedAt is
// when upstream snapshotted that ladder — every row in one response carries the same value,
// because a response IS one server-wide capture. Callers stamp history rows with it (never the
// sync time), so the series stays faithful and "stale never wins" falls out of an ordinary
// latest-by-recorded_at read.
type AllianceRow struct {
	AllianceID string  `json:"alliance_id"`
	Abbr       *string `json:"abbr"`
	Name       *string `json:"name"`
	ServerID   *int    `json:"server_id"`
	Power      *int64  `json:"power"`
	Kills      *int64  `json:"kills"`
	PowerRank  *int    `json:"power_rank"`
	KillsRank  *int    `json:"kills_rank"`
	CapturedAt *string `json:"captured_at"`
}

type alliancePage struct {
	Rows []AllianceRow `json:"rows"`
}

// SearchHit is one hit from /v1/search — the endpoint lastrank.fun's own search box
// uses. It is relevance-ranked on tag/name and spans EVERY server, which is what a scout
// lookup needs: VS Duel League opponents come from other servers, so filtering to ours
// would hide the very alliances an officer is trying to find.
//
// Deliberately a different endpoint from /v1/global/alliances, which substring-matches
// names and sorts by power — searching "cROw" there surfaces "Crowned Vengeance" and
// "NeCROWmancers" above the actual tag match. Documented as carrying no power/kills; the
// live response includes the keys as nulls, so they stay pointers and stay optional.
type SearchHit struct {
	Kind        string  `json:"kind"` // "alliance" | "player"
	ID          string  `json:"id"`
	Name        *string `json:"name"`
	Abbr        *string `json:"abbr"`
	ServerID    *int    `json:"server_id"`
	Power       *int64  `json:"power"`
	MemberCount *int    `json:"member_count"`
}

type searchResponse struct {
	Query string      `json:"query"`
	Hits  []SearchHit `json:"hits"`
}

// PlayerRow is one row of /v1/global/players. Verified live 2026-08-07:
// every field below is present in real responses, and kills_rank / thp / thp_rank
// / svip_level / country come back null for some players — hence pointers
// throughout. alliance_abbr / alliance_name are null for an unaffiliated player.
type PlayerRow struct {
	PublicID     int     `json:"public_id"`
	Name         string  `json:"name"`
	ServerID     *int    `json:"server_id"`
	AllianceAbbr *string `json:"alliance_abbr"`
	AllianceName *string `json:"alliance_name"`
	Country      *string `json:"country"`
	Power        *int64  `json:"power"`
	Kills        *int64  `json:"kills"`
	THP          *int64  `json:"thp"`
	PhotoURL     *string `json:"photo_url"`
	CapturedAt   *string `json:"captured_at"`
}

type playerPage struct {
	Rows []PlayerRow `json:"rows"`
}

// --- Transport ---

// do performs a throttled request against the lastrank API and decodes the JSON
// response into out. Every exported fetch in this package goes through it, which is
// what makes the 1 req/sec budget unbypassable. Returns ErrUpstream (wrapped) on
// failure.
func do(ctx context.Context, method, path string, out interface{}) error {
	if err := limiter.Wait(ctx); err != nil {
		return fmt.Errorf("%w: throttle wait: %v", ErrUpstream, err)
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("%w: build request: %v", ErrUpstream, err)
	}
	req.Header.Set("Accept", "application/json")
	// Careless probing without this trips Cloudflare's bot challenge, which answers
	// with an HTML "Just a moment…" page rather than JSON.
	req.Header.Set("User-Agent", "alliance-manager/1.0 (+enrichment)")

	// enrich (POST) does a live pull and is slow; give it the longer-timeout client.
	client := httpClient
	if method == http.MethodPost {
		client = enrichHTTP
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Drain a little so the body close is clean; ignore content.
		io.CopyN(io.Discard, resp.Body, 512)
		return fmt.Errorf("%w: status %d for %s", ErrUpstream, resp.StatusCode, path)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: decode %s: %v", ErrUpstream, path, err)
	}
	return nil
}

// --- Alliances ---

// FetchAlliance reads one alliance record by its 32-hex LastRank id.
func FetchAlliance(ctx context.Context, allianceID string) (*Alliance, error) {
	var a Alliance
	if err := do(ctx, http.MethodGet, "/v1/alliances/"+allianceID, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// SearchAllianceHits runs the site's own search across all servers and returns the raw
// hits. The caller filters and maps them (a player hit must never render as an
// alliance); see the app's mapLastRankSearchHits.
func SearchAllianceHits(ctx context.Context, query string) ([]SearchHit, error) {
	q := url.Values{}
	q.Set("q", query)
	q.Set("kind", "alliance")

	var resp searchResponse
	if err := do(ctx, http.MethodGet, "/v1/search?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	return resp.Hits, nil
}

// SearchAlliances finds alliances by fuzzy tag/name via /v1/global/alliances, optionally
// restricted to a single server (strict) — matching the picker's "strict server + fuzzy
// name" rule.
//
// An empty query is legal and returns the whole server, sorted by power desc — which is how
// the NAP sync reads the ladder. Since upstream sorts for us, the top-N IS the first N rows,
// so a caller asking for `limit` rows never fetches the tail of the server.
func SearchAlliances(ctx context.Context, query string, server *int, limit int) ([]AllianceRow, error) {
	if limit <= 0 {
		limit = 20
	}
	q := url.Values{}
	q.Set("search", query)
	q.Set("sort_by", "power")
	q.Set("sort_dir", "desc")
	q.Set("limit", strconv.Itoa(limit))
	if server != nil {
		q.Set("server_id", strconv.Itoa(*server))
	}
	var page alliancePage
	if err := do(ctx, http.MethodGet, "/v1/global/alliances?"+q.Encode(), &page); err != nil {
		return nil, err
	}
	return page.Rows, nil
}

// --- Players ---

// SearchPlayers finds players by fuzzy name via /v1/global/players, optionally restricted
// to a single server (strict). Search is cross-server when server is nil, which is what
// recruiting wants: a prospect is by definition somebody else's player, often on another
// server.
func SearchPlayers(ctx context.Context, query string, server *int, limit int) ([]PlayerRow, error) {
	if limit <= 0 {
		limit = 20
	}
	q := url.Values{}
	q.Set("search", query)
	q.Set("sort_by", "power")
	q.Set("sort_dir", "desc")
	q.Set("limit", strconv.Itoa(limit))
	if server != nil {
		q.Set("server_id", strconv.Itoa(*server))
	}
	var page playerPage
	if err := do(ctx, http.MethodGet, "/v1/global/players?"+q.Encode(), &page); err != nil {
		return nil, err
	}
	return page.Rows, nil
}

// getPlayer reads the cached player record (fast). Unexported: PlayerBulk and
// PlayerFresh are the two sanctioned strategies, and a third caller choosing its own
// mix is how a bulk path quietly becomes an enrich per player.
func getPlayer(ctx context.Context, publicID int) (*Player, error) {
	var p Player
	if err := do(ctx, http.MethodGet, "/v1/players/"+strconv.Itoa(publicID), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// enrichPlayer POSTs to the per-player /enrich endpoint, which asks lastrank to
// re-derive the player from its own most recent scan. It does NOT query the live game,
// so it cannot return anything newer than that scan — the freshness ceiling is their
// scan cadence, not how often we ask. Still worth it over the plain GET, which can serve
// an older cached copy. Slow — hence the two strategies below.
//
// The response is NOT a superset of the GET: it returns null for origin_server_id and
// power_detail even though the stored record keeps them (verified live 2026-09-03). Read
// those from a GET, never from this response.
func enrichPlayer(ctx context.Context, publicID int) (*Player, error) {
	var p Player
	if err := do(ctx, http.MethodPost, "/v1/players/"+strconv.Itoa(publicID)+"/enrich", &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// PlayerFresh enriches (live pull) for an on-demand SINGLE lookup, with a graceful
// fallback to the cached GET if enrich fails or times out — so the caller still gets data
// rather than an error. It enriches unconditionally: that is its documented purpose, and
// it is why it must never be used over a roster. The guard is on the bulk path.
func PlayerFresh(ctx context.Context, publicID int) (*Player, error) {
	p, err := enrichPlayer(ctx, publicID)
	if err == nil {
		return p, nil
	}
	slog.Error("lastrank enrich failed; falling back to cached GET", "error", err)
	return getPlayer(ctx, publicID)
}

// PlayerBulk reads the cheap cached GET, then upgrades to a live enrich only when the
// record's last_enriched_at is older than maxAge. A recently-enriched roster stays fast
// (GET only); stale records get refreshed. The GET is always available, so a slow or
// failed enrich falls back to it.
//
// maxAge is CLAMPED to at least MinEnrichAge. The alternative — letting the caller supply
// a predicate — makes `func(string) bool { return true }` a one-line way to turn every
// bulk sweep into an enrich per player. With the clamp, no caller can make this path
// enrich a given member more than hourly, which is a constraint this package actually
// owns rather than one every call site has to remember.
func PlayerBulk(ctx context.Context, publicID int, maxAge time.Duration) (*Player, error) {
	if maxAge < MinEnrichAge {
		maxAge = MinEnrichAge
	}
	p, err := getPlayer(ctx, publicID)
	if err != nil {
		return nil, err
	}
	if needsEnrich(p.LastEnrichedAt, maxAge) {
		if fresh, eerr := enrichPlayer(ctx, publicID); eerr == nil {
			return fresh, nil
		} else {
			slog.Error("lastrank bulk enrich failed; using cached GET", "error", eerr)
		}
	}
	return p, nil
}

// needsEnrich decides whether a cheap GET should be upgraded to a live enrich. An
// unknown or unparseable timestamp means "never enriched" → refresh.
//
// The layouts here cover only what this API emits. The package never sees SQLite-shaped
// timestamps; converting a capture date into one is the application's job.
func needsEnrich(lastEnrichedISO string, maxAge time.Duration) bool {
	s := strings.TrimSpace(lastEnrichedISO)
	if s == "" {
		return true
	}
	for _, l := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(l, s); err == nil {
			return time.Since(t.UTC()) > maxAge
		}
	}
	return true
}
