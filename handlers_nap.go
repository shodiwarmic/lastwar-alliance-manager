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
	AllyID           *int    `json:"ally_id"` // non-nil → already an ally
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
	rows, err := db.Query(`SELECT id, COALESCE(power_rank, ?) AS rank, lastrank_id, tag, name, power, kills
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
		if err := rows.Scan(&a.ExternalID, &a.rank, &a.LastRankID, &a.Tag, &a.Name, &a.Power, &a.Kills); err != nil {
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
	err := db.QueryRow(`SELECT tag, name, power, kills, power_rank, recorded_at
		FROM alliance_stats_history
		WHERE is_own = 1 AND server = ?
		ORDER BY recorded_at DESC LIMIT 1`, server).
		Scan(&a.Tag, &a.Name, &a.Power, &a.Kills, &rank, &recordedAt)
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

// refreshNAP pulls the ladder from LastRank once and writes it into the registry (current values)
// and alliance_stats_history (the time series).
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
		// sweep below would wipe every rank on the server while reporting success.
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

	recorded, scrubbed, err := applyNAPLadder(cfg, rowsIn, capturedAt, ourID, ourTag)
	if err != nil {
		slog.Error("refreshNAP: write failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	actor := getAuthUser(r)
	if scrubbed > 0 {
		logActivity(actor.ID, actor.Username, "deleted", "external_alliance", "our own alliance", false,
			"removed from the external alliance registry (an alliance registry must not contain us)")
	}
	logActivity(actor.ID, actor.Username, "imported", "alliance_stats", fmt.Sprintf("server %d ladder", cfg.server), false,
		fmt.Sprintf("%d alliances; %d new datapoints; captured %s", len(rowsIn), recorded, capturedAt))

	writeJSON(w, map[string]any{
		"alliances":   len(rowsIn),
		"recorded":    recorded,
		"captured_at": capturedAt,
	})
}

// applyNAPLadder writes one ladder capture. Shaped as read-all-then-write-all: the capture guard
// needs each row's existing lastrank_captured_at, and interleaving per-row reads with writes is
// exactly the single-connection deadlock noted above.
func applyNAPLadder(cfg napConfig, rowsIn []VSLeagueAllianceSearchResult, capturedAt, ourID, ourTag string) (recorded, scrubbed int, err error) {
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

		if isOwnLadderRow(row, ourID, ourTag) {
			// We are in the ladder but must not be in the registry. Record the datapoint, then make
			// sure no registry row for us survives — migration 058 purged the pre-existing one, but
			// an officer can re-add us at any time, so the invariant is re-asserted on every sync.
			n, ierr := insertOwnStats(tx, cfg.server, row, capturedAt)
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
		stats := &externalAllianceStats{LastRankID: &row.LastRankID, CapturedAt: &capturedAt}
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

		n, ierr := insertLadderStats(tx, eaID.Int64, cfg.server, row, capturedAt)
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
func insertOwnStats(tx *sql.Tx, server int, row VSLeagueAllianceSearchResult, capturedAt string) (int, error) {
	// Carry the full snapshot: the read splices our row with `is_own = 1 AND server = ?`, so a
	// minimal insert without the server would silently break it.
	res, err := tx.Exec(`INSERT OR IGNORE INTO alliance_stats_history
		(external_alliance_id, is_own, lastrank_id, server, tag, name, power, kills, power_rank, kills_rank, recorded_at, source)
		VALUES (NULL, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullStr(row.LastRankID), server, row.Tag, row.Name,
		row.Power, row.Kills, row.PowerRank, row.KillsRank, capturedAt, provenanceSource("lastrank"))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func insertLadderStats(tx *sql.Tx, eaID int64, server int, row VSLeagueAllianceSearchResult, capturedAt string) (int, error) {
	// member_count is deliberately absent: the ladder endpoint doesn't return it.
	res, err := tx.Exec(`INSERT OR IGNORE INTO alliance_stats_history
		(external_alliance_id, is_own, lastrank_id, server, tag, name, power, kills, power_rank, kills_rank, recorded_at, source)
		VALUES (?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		eaID, nullStr(row.LastRankID), server, row.Tag, row.Name,
		row.Power, row.Kills, row.PowerRank, row.KillsRank, capturedAt, provenanceSource("lastrank"))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}
