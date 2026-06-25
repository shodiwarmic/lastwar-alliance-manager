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
	LastSeenAt   string  `json:"last_seen_at"`
}

// --- Fetch ---

// lastRankGet performs a throttled GET against the lastrank API and decodes JSON
// into out. Returns errLastRankUpstream (wrapped with context) on any failure.
func lastRankGet(ctx context.Context, path string, out interface{}) error {
	if err := lastRankLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("%w: throttle wait: %v", errLastRankUpstream, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lastRankBaseURL+path, nil)
	if err != nil {
		return fmt.Errorf("%w: build request: %v", errLastRankUpstream, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "alliance-manager/1.0 (+enrichment)")

	resp, err := lastRankHTTP.Do(req)
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
	if err := lastRankGet(ctx, "/v1/alliances/"+allianceID, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func fetchLastRankPlayer(ctx context.Context, publicID int) (*lastrankPlayerResp, error) {
	var p lastrankPlayerResp
	if err := lastRankGet(ctx, "/v1/players/"+strconv.Itoa(publicID), &p); err != nil {
		return nil, err
	}
	return &p, nil
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
