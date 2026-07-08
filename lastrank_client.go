package main

// lastrank_client.go — a small, resilient client for the unofficial lastrank.fun
// JSON API (the `/v1/` paths). The API is undocumented and run by volunteers, so
// this client treats every response as best-effort enrichment: nullable fields are
// pointers, unknown fields are ignored, and all calls share one hard 1 req/sec
// throttle. Handlers must never surface a raw error from here to the client.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const lastRankBaseURL = "https://lastrank.fun"

// errLastRankUpstream is returned for any non-200 / transport / decode failure.
// Handlers log the detail and return a generic message — never this error's text.
var errLastRankUpstream = errors.New("lastrank upstream error")

// One global limiter shared across every lastrank call, so concurrent officer
// syncs still respect the 1 req/sec promise to the volunteer-run service.
var lastRankLimiter = rate.NewLimiter(rate.Every(time.Second), 1)

var lastRankHTTP = &http.Client{Timeout: 10 * time.Second}

// enrich forces a live game re-pull server-side, so it's much slower than a
// cached GET — give it a longer ceiling. Used only for single on-demand lookups.
var lastRankEnrichHTTP = &http.Client{Timeout: 25 * time.Second}

// --- Raw wire structs (never leave this file) ---

type lastrankAllianceMember struct {
	PublicID     int     `json:"public_id"`
	Name         string  `json:"name"`
	Country      *string `json:"country"`
	Power        int64   `json:"power"`
	HeroPower    *int64  `json:"hero_power"`
	AllianceRank *int    `json:"alliance_rank"`
	BaseLevel    *int    `json:"base_level"`
}

type lastrankAllianceResp struct {
	AllianceID string                   `json:"alliance_id"`
	Abbr       string                   `json:"abbr"`
	Name       string                   `json:"name"`
	ServerID   int                      `json:"server_id"`
	Fightpower int64                    `json:"fightpower"`
	ArmyKill   int64                    `json:"army_kill"`
	CurMember  int                      `json:"cur_member"`
	MaxMember  int                      `json:"max_member"`
	Country    *string                  `json:"country"`
	LastSeenAt string                   `json:"last_seen_at"`
	Members    []lastrankAllianceMember `json:"members"`
}

type lastrankPlayerResp struct {
	PublicID         int     `json:"public_id"`
	Name             string  `json:"name"`
	Country          *string `json:"country"`
	AllianceID       *string `json:"alliance_id"`
	AllianceAbbr     *string `json:"alliance_abbr"`
	AllianceName     *string `json:"alliance_name"`
	AllianceRank     *int    `json:"alliance_rank"`
	HomeServerID     int     `json:"home_server_id"`
	SrcServerID      int     `json:"src_server_id"`
	Power            int64   `json:"power"`
	HeroPower        *int64  `json:"hero_power"`
	ArmyKill         int64   `json:"army_kill"`
	BaseLevel        *int    `json:"base_level"`
	CareerLv         int     `json:"career_lv"`
	CareerType       int     `json:"career_type"`      // profession code; maps via CareerTypeLabels
	LastSeenAt       string  `json:"last_seen_at"`     // game-side "last active" (as-of date)
	LastEnrichedAt   string  `json:"last_enriched_at"` // when lastrank last re-pulled this record
	PhotoURL         string  `json:"photo_url"`
	PhotoURLFailover string  `json:"photo_url_failover"`
}

// --- Fetch ---

// lastRankDo performs a throttled request against the lastrank API and decodes
// the JSON response into out. Returns errLastRankUpstream (wrapped) on failure.
func lastRankDo(ctx context.Context, method, path string, out interface{}) error {
	if err := lastRankLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("%w: throttle wait: %v", errLastRankUpstream, err)
	}

	req, err := http.NewRequestWithContext(ctx, method, lastRankBaseURL+path, nil)
	if err != nil {
		return fmt.Errorf("%w: build request: %v", errLastRankUpstream, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "alliance-manager/1.0 (+enrichment)")

	// enrich (POST) does a live pull and is slow; give it the longer-timeout client.
	client := lastRankHTTP
	if method == http.MethodPost {
		client = lastRankEnrichHTTP
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", errLastRankUpstream, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Drain a little so the body close is clean; ignore content.
		io.CopyN(io.Discard, resp.Body, 512)
		return fmt.Errorf("%w: status %d for %s", errLastRankUpstream, resp.StatusCode, path)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: decode %s: %v", errLastRankUpstream, path, err)
	}
	return nil
}

func fetchLastRankAlliance(ctx context.Context, allianceID string) (*lastrankAllianceResp, error) {
	var a lastrankAllianceResp
	if err := lastRankDo(ctx, http.MethodGet, "/v1/alliances/"+allianceID, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// --- Alliance search wire structs (never leave this file) ---

// lastrankAllianceRow is one row of /v1/global/alliances (the search/list endpoint). It carries
// power/kills directly (unlike a /v1/search hit), so a picker can show them without a second call.
type lastrankAllianceRow struct {
	AllianceID string  `json:"alliance_id"`
	Abbr       *string `json:"abbr"`
	Name       *string `json:"name"`
	ServerID   *int    `json:"server_id"`
	Power      *int64  `json:"power"`
	Kills      *int64  `json:"kills"`
}

type lastrankAlliancePage struct {
	Rows []lastrankAllianceRow `json:"rows"`
}

// searchLastRankAlliances finds alliances by fuzzy tag/name via /v1/global/alliances, optionally
// restricted to a single server (strict) — matching the picker's "strict server + fuzzy name"
// rule. Uses the shared 1 req/sec limiter; the caller owns a bounded context.
func searchLastRankAlliances(ctx context.Context, query string, server *int) ([]VSLeagueAllianceSearchResult, error) {
	q := url.Values{}
	q.Set("search", query)
	q.Set("sort_by", "power")
	q.Set("sort_dir", "desc")
	q.Set("limit", "20")
	if server != nil {
		q.Set("server_id", strconv.Itoa(*server))
	}
	var page lastrankAlliancePage
	if err := lastRankDo(ctx, http.MethodGet, "/v1/global/alliances?"+q.Encode(), &page); err != nil {
		return nil, err
	}
	out := make([]VSLeagueAllianceSearchResult, 0, len(page.Rows))
	for _, row := range page.Rows {
		out = append(out, VSLeagueAllianceSearchResult{
			LastRankID: row.AllianceID,
			Tag:        row.Abbr,
			Name:       row.Name,
			Server:     row.ServerID,
			Power:      row.Power,
			Kills:      row.Kills,
		})
	}
	return out, nil
}

// getLastRankPlayer reads the cached player record (fast). Used by the bulk
// extended sync — enriching the whole roster would mean ~100 live game pulls,
// which is slow and abusive to the volunteer service.
func getLastRankPlayer(ctx context.Context, publicID int) (*lastrankPlayerResp, error) {
	var p lastrankPlayerResp
	if err := lastRankDo(ctx, http.MethodGet, "/v1/players/"+strconv.Itoa(publicID), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// enrichLastRankPlayer POSTs to the per-player /enrich endpoint, which forces
// lastrank to re-pull the player from the game CDN and returns the freshest
// record (the plain GET can serve a stale cached copy). Slow — reserve for
// single on-demand lookups, not bulk.
func enrichLastRankPlayer(ctx context.Context, publicID int) (*lastrankPlayerResp, error) {
	var p lastrankPlayerResp
	if err := lastRankDo(ctx, http.MethodPost, "/v1/players/"+strconv.Itoa(publicID)+"/enrich", &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// lastRankPlayerFresh enriches (live pull) for an on-demand single lookup, with
// a graceful fallback to the cached GET if enrich fails or times out — so the
// caller still gets data rather than an error.
func lastRankPlayerFresh(ctx context.Context, publicID int) (*lastrankPlayerResp, error) {
	if p, err := enrichLastRankPlayer(ctx, publicID); err == nil {
		return p, nil
	} else {
		slogLastRank("lastrank enrich failed; falling back to cached GET", err)
	}
	return getLastRankPlayer(ctx, publicID)
}

// lastRankEnrichMaxAge is how stale a record's last_enriched_at may be before the
// bulk sync upgrades a cheap GET to a live enrich. Tunable: lower = fresher data
// but more live pulls; higher = lighter on the volunteer service.
const lastRankEnrichMaxAge = 24 * time.Hour

func lastRankNeedsEnrich(lastEnrichedISO string) bool {
	t, ok := lastRankParseTime(lastEnrichedISO)
	if !ok {
		return true // unknown/never enriched → refresh
	}
	return time.Since(t) > lastRankEnrichMaxAge
}

// lastRankPlayerBulk reads the cheap cached GET, then upgrades to a live enrich
// only when the record's last_enriched_at is older than the freshness window.
// A recently-enriched roster stays fast (GET only); stale records get refreshed.
// The GET is always available, so a slow/failed enrich falls back to it.
func lastRankPlayerBulk(ctx context.Context, publicID int) (*lastrankPlayerResp, error) {
	p, err := getLastRankPlayer(ctx, publicID)
	if err != nil {
		return nil, err
	}
	if lastRankNeedsEnrich(p.LastEnrichedAt) {
		if fresh, eerr := enrichLastRankPlayer(ctx, publicID); eerr == nil {
			return fresh, nil
		} else {
			slogLastRank("lastrank bulk enrich failed; using cached GET", eerr)
		}
	}
	return p, nil
}

// --- Helpers ---

// lastRankRankToString maps lastrank's alliance_rank int (5=R5 … 1=R1, nil=unranked)
// to our TEXT rank values. Returns "" for unranked / out-of-range.
func lastRankRankToString(r *int) string {
	if r == nil {
		return ""
	}
	switch *r {
	case 5, 4, 3, 2, 1:
		return "R" + strconv.Itoa(*r)
	default:
		return ""
	}
}

var (
	reLastRankPlayerURL   = regexp.MustCompile(`/p/(\d+)`)
	reLastRankBareInt     = regexp.MustCompile(`^\s*(\d+)\s*$`)
	reLastRankAllianceURL = regexp.MustCompile(`/a/([0-9a-fA-F]{32})`)
	reLastRankBareHex     = regexp.MustCompile(`^\s*([0-9a-fA-F]{32})\s*$`)
)

// parseLastRankPlayerID accepts a full player URL (…/p/1585224) or a bare integer
// id and returns the numeric public_id.
func parseLastRankPlayerID(input string) (int, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return 0, false
	}
	if m := reLastRankPlayerURL.FindStringSubmatch(input); m != nil {
		if id, err := strconv.Atoi(m[1]); err == nil {
			return id, true
		}
	}
	if m := reLastRankBareInt.FindStringSubmatch(input); m != nil {
		if id, err := strconv.Atoi(m[1]); err == nil {
			return id, true
		}
	}
	return 0, false
}

// parseLastRankAllianceID accepts a full alliance URL (…/a/<32-hex>) or a bare
// 32-char hex id and returns the lowercased id.
func parseLastRankAllianceID(input string) (string, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", false
	}
	if m := reLastRankAllianceURL.FindStringSubmatch(input); m != nil {
		return strings.ToLower(m[1]), true
	}
	if m := reLastRankBareHex.FindStringSubmatch(input); m != nil {
		return strings.ToLower(m[1]), true
	}
	return "", false
}

// lastRankAllowedHosts are the only hosts a pasted opponent URL may come from (F-R05).
var lastRankAllowedHosts = map[string]bool{"lastrank.fun": true, "www.lastrank.fun": true}
var reLastRankStrictPath = regexp.MustCompile(`^/a/([0-9a-fA-F]{32})$`)

// parseLastRankAllianceStrict is the STRICT variant used only by the VS League opponent
// lookup: it accepts a bare 32-hex id, or a URL on an approved LastRank host whose path is
// exactly /a/<hex>. The shared parseLastRankAllianceID stays lax for its existing callers;
// this one avoids an officer pasting a URL from an unrelated site that merely contains /a/<hex>.
func parseLastRankAllianceStrict(input string) (string, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", false
	}
	if m := reLastRankBareHex.FindStringSubmatch(input); m != nil {
		return strings.ToLower(m[1]), true
	}
	u, err := url.Parse(input)
	if err != nil || !lastRankAllowedHosts[strings.ToLower(u.Hostname())] {
		return "", false
	}
	if m := reLastRankStrictPath.FindStringSubmatch(u.Path); m != nil {
		return strings.ToLower(m[1]), true
	}
	return "", false
}

// fetchLastRankOpponentSnapshot resolves a pasted URL/id to a point-in-time opponent
// snapshot for the VS League matchup card. Surfaces power (fightpower) and kills (army_kill),
// which the app-facing LastRankAllianceMeta drops. Uses the shared 1 req/sec limiter; the
// caller is responsible for a bounded context (the app runs on a single DB connection, so
// the handler must hold no DB handle across this call).
func fetchLastRankOpponentSnapshot(ctx context.Context, idOrURL string) (VSLeagueOpponentSnapshot, error) {
	id, ok := parseLastRankAllianceStrict(idOrURL)
	if !ok {
		return VSLeagueOpponentSnapshot{}, errLastRankBadInput
	}
	a, err := fetchLastRankAlliance(ctx, id)
	if err != nil {
		return VSLeagueOpponentSnapshot{}, err
	}
	return VSLeagueOpponentSnapshot{
		AllianceID:  a.AllianceID,
		Tag:         a.Abbr,
		Name:        a.Name,
		ServerID:    a.ServerID,
		Power:       a.Fightpower,
		Kills:       a.ArmyKill,
		MemberCount: a.CurMember,
		LastSeenAt:  a.LastSeenAt,
	}, nil
}

var errLastRankBadInput = errors.New("could not parse a LastRank alliance id/URL")

// --- Time / staleness ---

const sqliteTimeLayout = "2006-01-02 15:04:05"

// lastRankParseTime parses an ISO-8601 timestamp from the API. Tolerates a few
// shapes the volunteer service has been seen to emit.
func lastRankParseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		sqliteTimeLayout,
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// lastRankCaptureToSQLite converts an API capture timestamp to the SQLite UTC
// string we store in *_history.recorded_at. ok=false means "couldn't parse —
// caller should fall back to CURRENT_TIMESTAMP".
func lastRankCaptureToSQLite(captureISO string) (string, bool) {
	t, ok := lastRankParseTime(captureISO)
	if !ok {
		return "", false
	}
	return t.Format(sqliteTimeLayout), true
}

// lastRankCaptureNewer reports whether LastRank's capture date is strictly newer
// than our latest stored recorded_at for a metric. Used for the per-metric stale
// skip. Conservative on ambiguity: an unparseable capture date is treated as not
// newer (skip); an empty/unparseable existing date means we have no fresher data
// (apply). Both inputs are compared as time, never as strings (the ISO 'T'/'Z'
// vs SQLite space forms would mis-sort lexically).
func lastRankCaptureNewer(captureISO, ourRecordedAt string) bool {
	cap, ok := lastRankParseTime(captureISO)
	if !ok {
		return false
	}
	ourRecordedAt = strings.TrimSpace(ourRecordedAt)
	if ourRecordedAt == "" {
		return true
	}
	ours, ok := lastRankParseTime(ourRecordedAt)
	if !ok {
		return true
	}
	return cap.After(ours)
}

// slogLastRank is a tiny helper so every upstream failure is logged consistently
// server-side while the handler returns a generic message to the client.
func slogLastRank(msg string, err error) {
	slog.Error(msg, "error", err)
}
