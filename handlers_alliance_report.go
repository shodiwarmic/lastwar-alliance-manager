package main

// handlers_alliance_report.go — the External Alliances "Scout Report".
//
// Turns a LastRank alliance link into a member-by-member report on an outside alliance,
// almost always a VS opponent. Two endpoints, deliberately asymmetric:
//
//   POST /api/external-alliances/report         one upstream request, the whole roster
//   GET  /api/external-alliances/report/player  one upstream request, one member
//
// THE MEMBER DATA IS NEVER PERSISTED. It is scouting data about players in somebody
// else's alliance; the app keeps no shadow roster of them. The rows live in the browser
// for the life of the tab and are exported from there. Only the ALLIANCE-level numbers
// are stored, and only when that alliance already has an external_alliances row — a free
// refresh of something we already track, never a reason to mint a new row.
//
// That asymmetry is also why the extended pass is a browser-driven loop rather than a
// background job: jobKind.New takes no per-run target (jobs.go), and background_job_items
// would persist one opponent player's name per row — exactly the data that must not land
// in the database. Pacing is still enforced server-side by the shared lastRankLimiter, so
// the loop cannot outrun the 1 req/sec politeness budget no matter what the client does.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// allianceReportTimeout bounds each upstream call. Matches refreshOneExternalAlliance:
// lastRankHTTP already caps at 10s and the shared limiter can add up to a second on top,
// so 15s is the envelope rather than a second, independent deadline.
const allianceReportTimeout = 15 * time.Second

// allianceReportEnrichTimeout bounds a per-member fetch, which may upgrade to a live enrich.
// lastRankEnrichHTTP caps that at 25s and the shared limiter can add a second, so the handler
// ceiling has to sit above both or it would cancel the very re-pull it asked for.
const allianceReportEnrichTimeout = 30 * time.Second

// allianceReport is the basic report: ONE GET /v1/alliances/{id}, which already carries
// every member's name, power, hero power, alliance rank and HQ level.
//
// POST rather than GET because this writes — registry stats, a history datapoint and an
// activity row. gorilla/csrf only covers POST/PUT/DELETE, and the sibling fetch-and-save
// endpoint (POST /api/external-alliances/lookup) sets the precedent.
func allianceReport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LastRankID string `json:"lastrank_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.LastRankID) == "" {
		badRequest(w, "Paste the alliance's lastrank.fun link or id")
		return
	}
	// The strict parser, not the lax parseLastRankAllianceID: this value comes straight
	// from an officer pasting something, and a URL from an unrelated site that merely
	// contains /a/<hex> should not be followed.
	id, ok := parseLastRankAllianceStrict(body.LastRankID)
	if !ok {
		badRequest(w, "That doesn't look like a lastrank.fun alliance link or id")
		return
	}

	// Single DB connection: the ENTIRE upstream call completes before any query runs.
	ctx, cancel := context.WithTimeout(r.Context(), allianceReportTimeout)
	defer cancel()
	a, err := fetchLastRankAlliance(ctx, id)
	if err != nil {
		slogLastRank("alliance scout report fetch failed", err)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			http.Error(w, "LastRank is busy right now — try again in a moment", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "Could not reach LastRank for that alliance", http.StatusBadGateway)
		return
	}

	out := AllianceReport{
		LastRankID:  a.AllianceID,
		Tag:         a.Abbr,
		Name:        a.Name,
		ServerID:    a.ServerID,
		Power:       a.Fightpower,
		Kills:       a.ArmyKill,
		MemberCount: a.CurMember,
		MaxMember:   a.MaxMember,
		LastSeenAt:  a.LastSeenAt,
		Members:     make([]AllianceReportMember, 0, len(a.Members)),
	}
	for _, m := range a.Members {
		out.Members = append(out.Members, AllianceReportMember{
			PublicID:     m.PublicID,
			Name:         m.Name,
			Country:      m.Country,
			Power:        m.Power,
			HeroPower:    m.HeroPower,
			AllianceRank: m.AllianceRank,
			BaseLevel:    m.BaseLevel,
		})
	}

	// Only now, with the network call finished and no handle held, touch the database.
	out.Registry = saveReportAllianceStats(getAuthUser(r), a)
	writeJSON(w, out)
}

// allianceReportPlayer is the extended report's per-member step: one member, nothing written.
//
// Uses lastRankPlayerBulk — the shared bulk strategy — so a record LastRank hasn't refreshed
// within lastRankEnrichMaxAge gets a live re-pull rather than handing back numbers that may be
// weeks old. Scouting an opponent on stale figures is worse than useless: it invites planning
// against a version of the alliance that no longer exists.
//
// The cost is real and variable: a well-known alliance is nearly all cheap cached GETs, while
// one nobody has looked at can need an enrich per member (up to 25s each, on top of the 1/sec
// limiter). That is why the pass is opt-in, cancellable, and reports enrich_status per row so
// the wait is explained rather than mysterious. A failed enrich still falls back to the cached
// GET inside lastRankPlayerBulk, so a slow upstream degrades to stale data rather than nothing.
func allianceReportPlayer(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("public_id"))
	publicID, err := strconv.Atoi(raw)
	if err != nil || publicID <= 0 {
		badRequest(w, "public_id is required")
		return
	}

	// An enrich is a live game re-pull behind a 25s client, and the shared limiter can add a
	// second on top — so this needs a longer ceiling than the roster fetch.
	ctx, cancel := context.WithTimeout(r.Context(), allianceReportEnrichTimeout)
	defer cancel()
	p, err := lastRankPlayerBulk(ctx, publicID)
	if err != nil {
		slogLastRank("alliance scout report player fetch failed", err)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			http.Error(w, "LastRank is busy right now — try again in a moment", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "Could not reach LastRank for that player", http.StatusBadGateway)
		return
	}

	// Comma-ok, not CareerTypeLabel: an unknown code must read as "we don't know", not as
	// a profession literally named "Unknown".
	profession := ""
	if label, ok := CareerTypeLabels[p.CareerType]; ok {
		profession = label
	}

	writeJSON(w, AllianceReportPlayer{
		PublicID:         p.PublicID,
		Name:             p.Name,
		Kills:            p.ArmyKill,
		Power:            p.Power,
		HeroPower:        p.HeroPower,
		BaseLevel:        p.BaseLevel,
		Profession:       profession,
		CareerLevel:      p.CareerLv,
		HomeServerID:     p.HomeServerID,
		SrcServerID:      p.SrcServerID,
		LastSeenAt:       p.LastSeenAt,
		PhotoURL:         nullableStr(p.PhotoURL),
		PhotoURLFailover: nullableStr(p.PhotoURLFailover),
		EnrichStatus:     p.EnrichStatus,
	})
}

func nullableStr(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// saveReportAllianceStats persists the ALLIANCE-level half of a report, and only that.
//
// It reuses storeNAPAllianceSnapshot rather than writing a second stats path, which buys
// the whole set of rules that write already obeys: the lastrank_seen_at staleness guard
// compared as parsed times, the change-only datapoint rule, capture-date-as-recorded_at,
// and source='lastrank'.
//
// capturedAt is deliberately "": there is no LADDER capture here, and supplying one would
// mix the two clocks migration 058 exists to keep apart.
//
// Best-effort throughout — a report is a read operation from the officer's point of view,
// so a failed bookkeeping write must never fail the report itself.
func saveReportAllianceStats(user *AuthUser, a *lastrankAllianceResp) AllianceReportRegistry {
	out := AllianceReportRegistry{}

	d := napDetailSnapshot{
		LastRankID:  a.AllianceID,
		Server:      a.ServerID,
		Tag:         a.Abbr,
		Name:        a.Name,
		Power:       a.Fightpower,
		Kills:       a.ArmyKill,
		MemberCount: a.CurMember,
		Seen:        a.LastSeenAt,
	}

	// Reporting on ourselves is legitimate — an officer can paste our own link. Rule 2
	// keeps us out of the registry, so our stats live only in the is_own history series.
	// Same semantic as the NAP gather; the registry is never touched.
	if isOwnAlliance(a.AllianceID, a.Abbr) {
		d.IsOwn = true
		out.IsOwn = true
		statsApplied, historyAdded, err := storeNAPAllianceSnapshot("", d)
		if err != nil {
			slog.Error("scout report: own-alliance stats write failed", "error", err)
			return out
		}
		out.StatsApplied, out.HistoryAdded = statsApplied, historyAdded
		logReportAllianceSave(user, a.Abbr, out)
		return out
	}

	eaID, ok := resolveReportRegistryRow(a.AllianceID, a.Abbr)
	if !ok {
		// No registry row. Save NOTHING and say so, so the UI can offer to add it.
		// Minting one here would make "look someone up" silently grow the registry —
		// and appendDetailDatapoint errors outright without a row to key on anyway.
		return out
	}
	out.InRegistry = true
	id := int(eaID)
	out.ExternalAllianceID = &id

	statsApplied, historyAdded, err := storeNAPAllianceSnapshot("", d)
	if err != nil {
		slog.Error("scout report: alliance stats write failed", "error", err)
		return out
	}
	out.StatsApplied, out.HistoryAdded = statsApplied, historyAdded
	logReportAllianceSave(user, a.Abbr, out)
	return out
}

// resolveReportRegistryRow finds this alliance's existing registry row WITHOUT creating
// one. It resolves by lastrank_id, then falls back to tag.
//
// The tag fallback must BACKFILL lastrank_id before returning, because
// storeNAPAllianceSnapshot resolves the row by lastrank_id alone — a backfill done
// afterwards would silently miss the very row we just matched, and the officer would see
// "saved" with nothing written. Backfill only when the column is empty, matching the
// COALESCE(lastrank_id, ?) never-overwrite rule in upsertExternalAllianceTx.
func resolveReportRegistryRow(lastrankID, tag string) (int64, bool) {
	if strings.TrimSpace(lastrankID) == "" {
		return 0, false
	}

	var id int64
	err := db.QueryRow(`SELECT id FROM external_alliances WHERE lastrank_id = ? COLLATE NOCASE
		ORDER BY updated_at DESC LIMIT 1`, lastrankID).Scan(&id)
	if err == nil {
		return id, true
	}
	if err != sql.ErrNoRows {
		slog.Error("scout report: registry lookup by lastrank_id failed", "error", err)
		return 0, false
	}

	if strings.TrimSpace(tag) == "" {
		return 0, false
	}
	var existing sql.NullString
	err = db.QueryRow(`SELECT id, lastrank_id FROM external_alliances WHERE tag = ? COLLATE NOCASE
		ORDER BY updated_at DESC LIMIT 1`, tag).Scan(&id, &existing)
	if err != nil {
		if err != sql.ErrNoRows {
			slog.Error("scout report: registry lookup by tag failed", "error", err)
		}
		return 0, false
	}
	if strings.TrimSpace(existing.String) == "" {
		if _, uerr := db.Exec(`UPDATE external_alliances SET lastrank_id = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ? AND (lastrank_id IS NULL OR TRIM(lastrank_id) = '')`, lastrankID, id); uerr != nil {
			slog.Error("scout report: lastrank_id backfill failed", "error", uerr)
			return 0, false
		}
		return id, true
	}
	// The row carries a DIFFERENT lastrank_id — same tag, different alliance (tags are
	// reusable). Overwriting it would retarget an unrelated registry row, so treat this
	// as "not in the registry" and let the officer add it explicitly.
	if !strings.EqualFold(strings.TrimSpace(existing.String), strings.TrimSpace(lastrankID)) {
		return 0, false
	}
	return id, true
}

// logReportAllianceSave writes the audit row — but only when something actually changed.
// A report on an alliance whose numbers haven't moved is a pure read, and logging it would
// fill the activity feed with noise every time an officer glances at an opponent.
func logReportAllianceSave(user *AuthUser, tag string, res AllianceReportRegistry) {
	if !res.StatsApplied && !res.HistoryAdded {
		return
	}
	if user == nil {
		return
	}
	name := tag
	if strings.TrimSpace(name) == "" {
		name = "(unknown tag)"
	}
	var parts []string
	if res.StatsApplied {
		parts = append(parts, "current power/kills/members refreshed")
	}
	if res.HistoryAdded {
		parts = append(parts, "new history datapoint recorded")
	}
	logActivity(user.ID, user.Username, "updated", "external_alliance", name, false,
		"via scout report: "+strings.Join(parts, "; "))
}
