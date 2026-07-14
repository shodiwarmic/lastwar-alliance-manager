// handlers_nap.go - Non-Aggression Pact tab (Allies page)
//
// The NAP is the top-N alliances on our own server, and we are one of them. The tab reads LOCALLY
// from the external_alliances registry (fast, view-gated, zero upstream traffic) and is topped up
// by a separate manual refresh that hits LastRank once. That split is the whole design: LastRank is
// volunteer-run, so every officer opening a tab must not cost it a request.
//
// Our own alliance is spliced in from alliance_stats_history rather than the registry, because it
// must never BE in the registry — see the Rule 2 notes in CLAUDE.md.

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
)

// unrankedSentinel stands in for a NULL power_rank so the value can live in a plain int through
// sorting. It must never reach the client: NAPAlliance.Rank is a *int and is nil for these.
//
// Coalescing only in ORDER BY would be a trap — a NULL scanned into an int becomes 0, and
// 0 <= nap_size is TRUE, so an unranked alliance would be silently marked as inside the pact.
const unrankedSentinel = 999999

type NAPAlliance struct {
	ExternalID       int     `json:"external_id"` // 0 for our own row — it has no registry row
	Rank             *int    `json:"rank"`        // true server rank; nil = unranked
	InNAP            bool    `json:"in_nap"`
	IsUs             bool    `json:"is_us"`
	LastRankID       *string `json:"lastrank_id"`
	Tag              *string `json:"tag"`
	Name             *string `json:"name"`
	Power            *int64  `json:"power"`
	Kills            *int64  `json:"kills"`
	MemberCount      *int    `json:"member_count"` // nil → never enriched; render as an em dash, not 0
	AllyID           *int    `json:"ally_id"`      // non-nil → already an ally
	AllyActive       bool    `json:"ally_active"`
	AgreementTypeIDs []int   `json:"agreement_type_ids"`

	rank int // sentinel-carrying working value; not serialized
}

type NAPResponse struct {
	ServerConfigured bool          `json:"server_configured"`
	Server           int           `json:"server"`
	NAPSize          int           `json:"nap_size"`
	CapturedAt       string        `json:"captured_at"` // age of the DATA, not of the last click
	Alliances        []NAPAlliance `json:"alliances"`
}

type napConfig struct {
	server      int
	size        int
	importLimit int
}

func loadNAPConfig() napConfig {
	var c napConfig
	db.QueryRow(`SELECT COALESCE(our_server_id, 0), COALESCE(nap_size, 10), COALESCE(nap_import_limit, 15)
		FROM settings WHERE id = 1`).Scan(&c.server, &c.size, &c.importLimit)
	return c
}

// getNAP renders the pact from cached data only. It never calls LastRank, which is what lets the
// tab stay usable when the volunteer service is down.
func getNAP(w http.ResponseWriter, r *http.Request) {
	cfg := loadNAPConfig()
	resp := NAPResponse{
		ServerConfigured: cfg.server > 0,
		Server:           cfg.server,
		NAPSize:          cfg.size,
		Alliances:        []NAPAlliance{},
	}
	// Same response shape whether or not a server is configured — the client never branches on type.
	if cfg.server == 0 {
		writeJSON(w, resp)
		return
	}

	alliances, err := loadRegistryLadder(cfg)
	if err != nil {
		slog.Error("getNAP: ladder query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if us, ok := loadOwnLadderRow(cfg.server); ok {
		alliances = append(alliances, us)
	}

	sort.SliceStable(alliances, func(i, j int) bool { return alliances[i].rank < alliances[j].rank })

	// The registry can hold rows for our server that the NAP never imported (someone looked one
	// up), so the LIMIT above can already be full before our own row is spliced in — which would
	// push the response past its own import limit. Trim, but never trim OURSELVES away: showing
	// where we stand is half the point of the tab, and it must not vanish the moment the answer is
	// unflattering. Drop the last non-us row instead.
	alliances = truncateKeepingUs(alliances, cfg.importLimit)

	if err := attachAllyLinks(alliances); err != nil {
		slog.Error("getNAP: ally link failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	for i := range alliances {
		alliances[i].InNAP = alliances[i].rank > 0 && alliances[i].rank <= cfg.size
		if alliances[i].rank != unrankedSentinel {
			rank := alliances[i].rank
			alliances[i].Rank = &rank
		}
	}

	// The capture date lives on the history series, not on the registry (which has no recorded_at).
	db.QueryRow(`SELECT COALESCE(MAX(recorded_at), '') FROM alliance_stats_history WHERE server = ?`,
		cfg.server).Scan(&resp.CapturedAt)

	resp.Alliances = alliances
	writeJSON(w, resp)
}

// loadRegistryLadder reads the cached ladder for our server, most-highly-ranked first.
func loadRegistryLadder(cfg napConfig) ([]NAPAlliance, error) {
	rows, err := db.Query(`SELECT id, COALESCE(power_rank, ?) AS rank, lastrank_id, tag, name, power, kills, member_count
		FROM external_alliances
		WHERE server = ?
		ORDER BY rank, power DESC
		LIMIT ?`, unrankedSentinel, cfg.server, cfg.importLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []NAPAlliance{}
	for rows.Next() {
		var a NAPAlliance
		if err := rows.Scan(&a.ExternalID, &a.rank, &a.LastRankID, &a.Tag, &a.Name, &a.Power, &a.Kills, &a.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// loadOwnLadderRow rebuilds our own row from the history series — we are deliberately absent from
// the registry. Source-agnostic: an OCR'd or hand-entered figure serves here just as well as a
// LastRank one.
func loadOwnLadderRow(server int) (NAPAlliance, bool) {
	var a NAPAlliance
	var rank sql.NullInt64
	var recordedAt string
	err := db.QueryRow(`SELECT tag, name, power, kills, power_rank, member_count, recorded_at
		FROM alliance_stats_history
		WHERE is_own = 1 AND server = ?
		ORDER BY recorded_at DESC LIMIT 1`, server).
		Scan(&a.Tag, &a.Name, &a.Power, &a.Kills, &rank, &a.MemberCount, &recordedAt)
	if err != nil {
		return a, false // never synced, or we sit below the import window — either way, not in the pact
	}

	a.IsUs = true
	a.rank = unrankedSentinel
	if rank.Valid {
		a.rank = int(rank.Int64)
	}

	// If our snapshot predates the newest ladder capture, our stored rank is a fossil: it means the
	// last refresh did not see us (we slipped below the import window), so no fresh row was written.
	// Rendering that old rank would park us above the NAP line at a position we no longer hold —
	// and no future refresh would ever correct it, because while we're outside the window we never
	// reappear in a response. Drop the rank; keep the row. Unranked-but-present is the honest answer.
	var newestLadder string
	db.QueryRow(`SELECT COALESCE(MAX(recorded_at), '') FROM alliance_stats_history
		WHERE server = ? AND is_own = 0 AND source = 'lastrank'`, server).Scan(&newestLadder)
	if newestLadder != "" && lastRankCaptureNewer(newestLadder, recordedAt) {
		a.rank = unrankedSentinel
	}

	return a, true
}

// truncateKeepingUs cuts the ladder to limit rows without ever dropping our own row.
func truncateKeepingUs(alliances []NAPAlliance, limit int) []NAPAlliance {
	if limit <= 0 || len(alliances) <= limit {
		return alliances
	}
	kept := alliances[:limit]
	for _, a := range kept {
		if a.IsUs {
			return kept
		}
	}
	// We were cut. Put us back by displacing the weakest row that isn't us.
	for _, a := range alliances[limit:] {
		if a.IsUs {
			return append(kept[:limit-1:limit-1], a)
		}
	}
	return kept
}

// attachAllyLinks marks the ladder rows we already have an agreement with.
//
// The tag fallback exists for allies whose external_alliance_id never resolved, but it must NOT be
// an OR'd LEFT JOIN: allies.tag has no uniqueness constraint, so a former inactive ally and a
// re-added active one sharing a tag would fan out into duplicate ladder rows. Resolve one ally per
// registry row deterministically (link first, then same-server tag; active before inactive), and
// require server equality — tags are reusable across servers (migration 057).
func attachAllyLinks(alliances []NAPAlliance) error {
	allyOf := map[int]*NAPAlliance{}
	for i := range alliances {
		alliances[i].AgreementTypeIDs = []int{}
		if alliances[i].ExternalID > 0 {
			allyOf[alliances[i].ExternalID] = &alliances[i]
		}
	}
	if len(allyOf) == 0 {
		return nil
	}

	rows, err := db.Query(`SELECT ea.id, a.id, a.active
		FROM external_alliances ea
		JOIN allies a ON a.id = (
			SELECT a2.id FROM allies a2
			WHERE a2.external_alliance_id = ea.id
			   OR (a2.external_alliance_id IS NULL
			       AND TRIM(COALESCE(a2.tag,'')) != ''
			       AND a2.tag = ea.tag COLLATE NOCASE
			       AND CAST(a2.server AS INTEGER) = ea.server)
			ORDER BY a2.active DESC, a2.id DESC
			LIMIT 1
		)
		WHERE ea.server IS NOT NULL`)
	if err != nil {
		return err
	}
	allyIDs := []any{}
	byAllyID := map[int]*NAPAlliance{}
	for rows.Next() {
		var eaID, allyID, active int
		if err := rows.Scan(&eaID, &allyID, &active); err != nil {
			rows.Close()
			return err
		}
		if target, ok := allyOf[eaID]; ok {
			id := allyID
			target.AllyID = &id
			target.AllyActive = active == 1
			allyIDs = append(allyIDs, allyID)
			byAllyID[allyID] = target
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(allyIDs) == 0 {
		return nil
	}

	// One query for all agreement types — not the per-ally N+1 loop that getAllies uses.
	q := `SELECT ally_id, agreement_type_id FROM ally_agreements WHERE ally_id IN (?` +
		strings.Repeat(",?", len(allyIDs)-1) + `) ORDER BY agreement_type_id`
	arows, err := db.Query(q, allyIDs...)
	if err != nil {
		return err
	}
	defer arows.Close()
	for arows.Next() {
		var allyID, typeID int
		if err := arows.Scan(&allyID, &typeID); err != nil {
			return err
		}
		if target, ok := byAllyID[allyID]; ok {
			target.AgreementTypeIDs = append(target.AgreementTypeIDs, typeID)
		}
	}
	return arows.Err()
}

// --- Refresh is a three-phase, browser-driven flow, mirroring the LastRank member sync ---
//
//	POST /api/allies/nap/refresh        -> one ladder call; writes the registry + history rows
//	POST /api/allies/nap/member (xN)    -> one alliance's member count, called in a loop by the client
//	POST /api/allies/nap/finish         -> writes the single activity row for the whole run
//
// Why not do it all in one request: member counts are NOT on the ladder endpoint, only on the
// per-alliance detail record, so enriching them costs one upstream call each. At the shared
// 1 req/sec limit a default import would block a single request for ~15s with nothing to show.
// Driving the loop from the browser gives per-alliance progress, lets an interrupted run resume
// (the counts already written stay written), and is the pattern the Members LastRank sync and the
// External Alliances gather already use. Phase-2 writes are deferred-logged; /finish logs once.

// NAPRefreshAlliance is one row of the queue the client then walks through in phase 2.
type NAPRefreshAlliance struct {
	ExternalID int     `json:"external_id"` // 0 for our own alliance — it has no registry row
	LastRankID string  `json:"lastrank_id"`
	Tag        *string `json:"tag"`
	Name       *string `json:"name"`
	IsUs       bool    `json:"is_us"`
}

// refreshNAP is phase 1: pull the server ladder in a single upstream call and write it. Fast.
func refreshNAP(w http.ResponseWriter, r *http.Request) {
	cfg := loadNAPConfig()
	if cfg.server == 0 {
		badRequest(w, "Set your server number in Settings first")
		return
	}

	ourID, ourTag := ourAllianceIdentity()
	if ourID == "" && ourTag == "" {
		// Without an identity we cannot tell which ladder row is us — and guessing would either
		// import ourselves into the registry or brand a foreign alliance as us. Refuse.
		badRequest(w, "Set your LastRank alliance ID or alliance tag in Settings first")
		return
	}

	// The whole network call happens BEFORE db.Begin(): SetMaxOpenConns(1) means a query issued
	// while a transaction is open deadlocks, and holding the single connection across a slow
	// upstream request would stall every other user.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	rowsIn, err := searchLastRankAlliances(ctx, "", &cfg.server, cfg.importLimit)
	if err != nil {
		slogLastRank("refreshNAP failed", err)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			http.Error(w, "LastRank is busy right now — try again in a moment", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "Could not reach LastRank for the server ladder", http.StatusBadGateway)
		return
	}
	if len(rowsIn) == 0 {
		// Bail before the transaction. An empty ladder has no captured_at to stamp history with,
		// and — because SQLite accepts an empty `NOT IN ()` and evaluates it TRUE — the stale-rank
		// sweep would wipe every rank on the server while reporting success.
		slog.Error("refreshNAP: empty ladder from LastRank", "server", cfg.server)
		http.Error(w, "LastRank returned no alliances for this server", http.StatusBadGateway)
		return
	}

	// recorded_at is the OBSERVATION date, never the sync time. If it won't parse, fail: falling
	// back to CURRENT_TIMESTAMP would restamp every row on every click, so INSERT OR IGNORE would
	// dedupe nothing and each refresh would quietly add junk to the series this exists to build.
	var capturedAt string
	if rowsIn[0].CapturedAt != nil {
		if s, ok := lastRankCaptureToSQLite(*rowsIn[0].CapturedAt); ok {
			capturedAt = s
		}
	}
	if capturedAt == "" {
		raw := ""
		if rowsIn[0].CapturedAt != nil {
			raw = *rowsIn[0].CapturedAt
		}
		slogLastRank("refreshNAP: unparseable captured_at", fmt.Errorf("value=%q", raw))
		http.Error(w, "LastRank returned an unreadable capture date", http.StatusBadGateway)
		return
	}

	// Member counts come later, one alliance at a time (phase 2) — nil for now.
	recorded, scrubbed, err := applyNAPLadder(cfg, rowsIn, capturedAt, ourID, ourTag, nil)
	if err != nil {
		slog.Error("refreshNAP: write failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// A scrub is a real deletion, so it is logged now rather than deferred to /finish.
	if scrubbed > 0 {
		actor := getAuthUser(r)
		logActivity(actor.ID, actor.Username, "deleted", "external_alliance", "our own alliance", false,
			"removed from the external alliance registry (an alliance registry must not contain us)")
	}

	// Hand the client the queue to walk. Registry ids are resolved here so phase 2 never has to
	// re-derive identity.
	queue := make([]NAPRefreshAlliance, 0, len(rowsIn))
	byLRID := loadRegistryIDsByLastRankID(cfg.server)
	for _, row := range rowsIn {
		item := NAPRefreshAlliance{LastRankID: row.LastRankID, Tag: row.Tag, Name: row.Name}
		if isOwnLadderRow(row, ourID, ourTag) {
			item.IsUs = true
		} else {
			item.ExternalID = byLRID[strings.ToLower(row.LastRankID)]
		}
		queue = append(queue, item)
	}

	writeJSON(w, map[string]any{
		"server":      cfg.server,
		"captured_at": capturedAt,
		"recorded":    recorded,
		"alliances":   queue,
	})
}

func loadRegistryIDsByLastRankID(server int) map[string]int {
	out := map[string]int{}
	rows, err := db.Query(`SELECT id, COALESCE(lastrank_id, '') FROM external_alliances WHERE server = ?`, server)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var lrid string
		if rows.Scan(&id, &lrid) == nil && lrid != "" {
			out[strings.ToLower(lrid)] = id
		}
	}
	return out
}

// napMemberCount is phase 2: fetch ONE alliance's member count and store it. The client calls this
// once per alliance in the queue, which is what makes per-alliance progress possible.
//
// Deferred-logged: a hundred activity rows for one refresh would be noise. /finish logs once.
func napMemberCount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LastRankID string `json:"lastrank_id"`
		CapturedAt string `json:"captured_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	req.LastRankID = strings.TrimSpace(req.LastRankID)
	if req.LastRankID == "" {
		badRequest(w, "lastrank_id is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	a, err := fetchLastRankAlliance(ctx, req.LastRankID)
	if err != nil {
		slogLastRank("napMemberCount: lookup failed", err)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			http.Error(w, "LastRank is busy right now", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "Could not reach LastRank for this alliance", http.StatusBadGateway)
		return
	}
	if a.CurMember <= 0 {
		writeJSON(w, map[string]any{"applied": false, "skip_reason": "no_member_count"})
		return
	}

	if err := storeNAPMemberCount(req.LastRankID, req.CapturedAt, a.CurMember, a.MaxMember); err != nil {
		slog.Error("napMemberCount: write failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"applied":      true,
		"member_count": a.CurMember,
		"max_member":   a.MaxMember,
	})
}

// storeNAPMemberCount writes a member count to the registry row (current) and backfills it onto the
// history row for this capture (the series).
//
// Our own alliance has NO registry row (Rule 2), so its only home is the is_own history row — which
// is exactly why the history backfill exists rather than relying on the registry.
func storeNAPMemberCount(lastrankID, capturedAt string, cur, max int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE external_alliances SET member_count = ?, updated_at = CURRENT_TIMESTAMP
		WHERE lastrank_id = ? COLLATE NOCASE`, cur, lastrankID); err != nil {
		return err
	}

	// Only fills a hole; never rewrites a datapoint we already recorded.
	if capturedAt != "" {
		if _, err := tx.Exec(`UPDATE alliance_stats_history SET member_count = ?
			WHERE member_count IS NULL AND recorded_at = ? AND source = ?
			  AND lastrank_id = ? COLLATE NOCASE`,
			cur, capturedAt, provenanceSource("lastrank"), lastrankID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// napFinish is phase 3: one activity row for the whole run, matching how the LastRank member sync
// defers its logging to /finish rather than logging every per-item write.
func napFinish(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Alliances  int    `json:"alliances"`
		Recorded   int    `json:"recorded"`
		Members    int    `json:"members_synced"`
		CapturedAt string `json:"captured_at"`
		Server     int    `json:"server"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	actor := getAuthUser(r)
	logActivity(actor.ID, actor.Username, "imported", "alliance_stats",
		fmt.Sprintf("server %d ladder", req.Server), false,
		fmt.Sprintf("%d alliances; %d new datapoints; %d member counts; captured %s",
			req.Alliances, req.Recorded, req.Members, req.CapturedAt))
	writeJSON(w, map[string]string{"message": "ok"})
}

// applyNAPLadder writes one ladder capture. Shaped as read-all-then-write-all: the capture guard
// needs each row's existing lastrank_captured_at, and interleaving per-row reads with writes is
// exactly the single-connection deadlock noted above.
func applyNAPLadder(cfg napConfig, rowsIn []VSLeagueAllianceSearchResult, capturedAt, ourID, ourTag string, members map[string]int) (recorded, scrubbed int, err error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	// Read every existing row for this server up front, then close the cursor before any write.
	type existing struct {
		id         int64
		capturedAt string
	}
	prior := map[string]existing{} // keyed by lowercased lastrank_id
	rows, err := tx.Query(`SELECT id, COALESCE(lastrank_id, ''), COALESCE(lastrank_captured_at, '')
		FROM external_alliances WHERE server = ?`, cfg.server)
	if err != nil {
		return 0, 0, err
	}
	for rows.Next() {
		var id int64
		var lrid, cap string
		if err := rows.Scan(&id, &lrid, &cap); err != nil {
			rows.Close()
			return 0, 0, err
		}
		if lrid != "" {
			prior[strings.ToLower(lrid)] = existing{id: id, capturedAt: cap}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}

	seen := make([]any, 0, len(rowsIn))
	for _, row := range rowsIn {
		tag, name := deref(row.Tag), deref(row.Name)

		// nil when the enrichment call failed or was cut short — COALESCE then preserves whatever
		// member count we already had rather than blanking it.
		var memberCount *int
		if n, ok := members[strings.ToLower(row.LastRankID)]; ok {
			memberCount = &n
		}

		if isOwnLadderRow(row, ourID, ourTag) {
			// We are in the ladder but must not be in the registry. Record the datapoint, then make
			// sure no registry row for us survives — migration 058 purged the pre-existing one, but
			// an officer can re-add us at any time, so the invariant is re-asserted on every sync.
			n, ierr := insertOwnStats(tx, cfg.server, row, capturedAt, memberCount)
			if ierr != nil {
				return 0, 0, ierr
			}
			recorded += n
			n, serr := scrubOwnAllianceFromRegistryTx(tx, row.LastRankID, tag, cfg.server)
			if serr != nil {
				return 0, 0, serr
			}
			scrubbed += n
			continue
		}

		seen = append(seen, strings.ToLower(row.LastRankID))

		// Never let a stale capture walk back a current value. A brand-new row, or one with no
		// recorded capture date, always applies.
		stats := &externalAllianceStats{LastRankID: &row.LastRankID, CapturedAt: &capturedAt, MemberCount: memberCount}
		if p, ok := prior[strings.ToLower(row.LastRankID)]; !ok || lastRankCaptureNewer(capturedAt, p.capturedAt) {
			stats.Power, stats.Kills = row.Power, row.Kills
			stats.PowerRank, stats.KillsRank = row.PowerRank, row.KillsRank
		} else {
			// Stale: refresh nothing but the identity. Don't advance lastrank_captured_at either.
			stats.CapturedAt = nil
		}

		serverStr := ""
		if row.Server != nil {
			serverStr = fmt.Sprintf("%d", *row.Server)
		}
		eaID, uerr := upsertExternalAllianceTx(tx, tag, name, serverStr, stats)
		if uerr != nil {
			return 0, 0, uerr
		}
		// INSERT OR IGNORE below suppresses EVERY constraint violation, including the CHECK — so an
		// invalid id here would silently drop the datapoint and the series would just quietly miss
		// this alliance. Don't let OR IGNORE be the thing that "handles" a bug.
		if !eaID.Valid {
			return 0, 0, fmt.Errorf("registry upsert returned no id for alliance %q", row.LastRankID)
		}

		n, ierr := insertLadderStats(tx, eaID.Int64, cfg.server, row, capturedAt, memberCount)
		if ierr != nil {
			return 0, 0, ierr
		}
		recorded += n
	}

	// A rank is a position WITHIN one capture, but we store it as a per-row attribute. An alliance
	// that slips from #12 to #30 falls out of the import window and stops appearing in responses —
	// leaving its rank 12 behind while the new #12 also writes 12, so the tab would show two #12
	// rows, the stale one indistinguishable. Clear the ranks of everyone the ladder didn't mention.
	//
	// `lastrank_id IS NULL OR ...` is load-bearing: a bare NOT IN is NULL (not TRUE) for a NULL id,
	// which would silently exempt exactly the rows most likely to be stale.
	if len(seen) > 0 {
		q := `UPDATE external_alliances SET power_rank = NULL, kills_rank = NULL
			WHERE server = ? AND (power_rank IS NOT NULL OR kills_rank IS NOT NULL)
			  AND (lastrank_id IS NULL OR LOWER(lastrank_id) NOT IN (?` +
			strings.Repeat(",?", len(seen)-1) + `))`
		args := append([]any{cfg.server}, seen...)
		if _, err := tx.Exec(q, args...); err != nil {
			return 0, 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return recorded, scrubbed, nil
}

// isOwnLadderRow decides whether a ladder row is us. Both sides of a comparison must be non-empty:
// an empty configured tag matching a blank upstream abbr would brand a FOREIGN alliance as us,
// which would corrupt our own series and leak a real alliance out of the registry.
func isOwnLadderRow(row VSLeagueAllianceSearchResult, ourID, ourTag string) bool {
	if ourID != "" && row.LastRankID != "" && strings.EqualFold(row.LastRankID, ourID) {
		return true
	}
	if ourTag != "" && row.Tag != nil && strings.TrimSpace(*row.Tag) != "" &&
		strings.EqualFold(strings.TrimSpace(*row.Tag), ourTag) {
		return true
	}
	return false
}

// insertOwnStats records OUR datapoint and reports whether it was actually new. Returning
// RowsAffected rather than assuming 1 is load-bearing: a re-run of the same capture is ignored by
// the partial unique index, and counting it anyway would make the UI claim an update that never
// happened.
func insertOwnStats(tx *sql.Tx, server int, row VSLeagueAllianceSearchResult, capturedAt string, memberCount *int) (int, error) {
	// Carry the full snapshot: the read splices our row with `is_own = 1 AND server = ?`, so a
	// minimal insert without the server would silently break it.
	res, err := tx.Exec(`INSERT OR IGNORE INTO alliance_stats_history
		(external_alliance_id, is_own, lastrank_id, server, tag, name, power, kills, power_rank, kills_rank, member_count, recorded_at, source)
		VALUES (NULL, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullStr(row.LastRankID), server, row.Tag, row.Name,
		row.Power, row.Kills, row.PowerRank, row.KillsRank, memberCount, capturedAt, provenanceSource("lastrank"))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()

	// Backfill a member count that arrived late. INSERT OR IGNORE leaves an existing row untouched,
	// and our own row has no registry entry to fall back on (Rule 2) — so without this, a member
	// count fetched after the capture was first recorded could never land, and a failed enrichment
	// would be stuck NULL forever. Only fills a hole; never overwrites a recorded value.
	if err := backfillMemberCount(tx, `is_own = 1 AND recorded_at = ? AND source = ?`,
		memberCount, capturedAt, provenanceSource("lastrank")); err != nil {
		return 0, err
	}
	return int(n), nil
}

// backfillMemberCount fills member_count on an already-recorded history row, and only when it is
// still NULL — a datapoint we already have is never rewritten.
func backfillMemberCount(tx *sql.Tx, where string, memberCount *int, args ...any) error {
	if memberCount == nil {
		return nil
	}
	q := `UPDATE alliance_stats_history SET member_count = ? WHERE member_count IS NULL AND ` + where
	return execIgnoreResult(tx, q, append([]any{*memberCount}, args...)...)
}

func execIgnoreResult(tx *sql.Tx, q string, args ...any) error {
	_, err := tx.Exec(q, args...)
	return err
}

func insertLadderStats(tx *sql.Tx, eaID int64, server int, row VSLeagueAllianceSearchResult, capturedAt string, memberCount *int) (int, error) {
	// member_count comes from the per-alliance detail call, not the ladder — nil when that lookup
	// failed, which the history row records honestly as NULL.
	res, err := tx.Exec(`INSERT OR IGNORE INTO alliance_stats_history
		(external_alliance_id, is_own, lastrank_id, server, tag, name, power, kills, power_rank, kills_rank, member_count, recorded_at, source)
		VALUES (?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		eaID, nullStr(row.LastRankID), server, row.Tag, row.Name,
		row.Power, row.Kills, row.PowerRank, row.KillsRank, memberCount, capturedAt, provenanceSource("lastrank"))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()

	// Same late-arrival backfill as our own row: an enrichment that failed on one refresh should
	// heal on the next, rather than leaving a permanent hole in the series for that capture.
	if err := backfillMemberCount(tx, `external_alliance_id = ? AND recorded_at = ? AND source = ?`,
		memberCount, eaID, capturedAt, provenanceSource("lastrank")); err != nil {
		return 0, err
	}
	return int(n), nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}
