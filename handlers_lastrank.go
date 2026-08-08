package main

// handlers_lastrank.go — HTTP handlers for the LastRank.fun enrichment feature.
// All upstream fetching/throttling lives in lastrank_client.go; this file does
// matching, the stale/changed gating, DB writes (with 'lastrank' provenance and
// the capture date as recorded_at), and activity logging.

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

// rowQuerier is satisfied by both *sql.DB and *sql.Tx.
type rowQuerier interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

// lastRankAllianceID returns the configured alliance id, or "" if unset.
func lastRankAllianceID() string {
	var id string
	db.QueryRow("SELECT COALESCE(lastrank_alliance_id, '') FROM settings WHERE id = 1").Scan(&id)
	return strings.TrimSpace(id)
}

// lastRankLatestHistory returns the most recent value + recorded_at for a member
// in one of the history tables. table/valueCol are code constants, never input.
func lastRankLatestHistory(q rowQuerier, table, valueCol string, memberID int) (int64, string, bool) {
	var v int64
	var at string
	err := q.QueryRow("SELECT "+valueCol+", recorded_at FROM "+table+" WHERE member_id = ? ORDER BY recorded_at DESC LIMIT 1", memberID).Scan(&v, &at)
	if err != nil {
		return 0, "", false
	}
	return v, at, true
}

// lastRankStatDiff builds a proposed power/hero-power update, applying the
// per-metric stale + unchanged gating.
func lastRankStatDiff(q rowQuerier, table, valueCol string, memberID int, newVal int64, captureISO string) *LastRankStatDiff {
	cur, at, ok := lastRankLatestHistory(q, table, valueCol, memberID)
	d := &LastRankStatDiff{New: newVal}
	if ok {
		c := cur
		d.Current = &c
	}
	switch {
	case !lastRankCaptureNewer(captureISO, at):
		d.SkipReason = "stale"
	case ok && cur == newVal:
		d.SkipReason = "unchanged"
	default:
		d.Apply = true
	}
	return d
}

// lastRankApplyPairedStats applies a LastRank entry's power/hero/HQ to a member
// — used when an unmatched name is paired to (or added as) a member, so the
// pairing also accepts that player's stats. Same gating as the matched path:
// per-metric skip unless the capture date is newer and the value changed; HQ
// only increases. Returns rows applied for the activity summary.
func lastRankApplyPairedStats(tx *sql.Tx, memberID int, power, hero *int64, baseLevel *int, recordedAt, captureISO string) (p, h, hq int) {
	if power != nil {
		cur, at, ok := lastRankLatestHistory(tx, "power_history", "power", memberID)
		if lastRankCaptureNewer(captureISO, at) && !(ok && cur == *power) {
			if err := lastRankInsertHistory(tx, "power_history", "power", memberID, *power, recordedAt); err == nil {
				p++
			}
		}
	}
	if hero != nil {
		cur, at, ok := lastRankLatestHistory(tx, "hero_power_history", "power", memberID)
		if lastRankCaptureNewer(captureISO, at) && !(ok && cur == *hero) {
			if err := lastRankInsertHistory(tx, "hero_power_history", "power", memberID, *hero, recordedAt); err == nil {
				h++
			}
		}
	}
	if baseLevel != nil && *baseLevel > 0 {
		// HQ level is history-only and never regresses: record only a higher value,
		// stamped with the capture date + 'lastrank' source.
		cur, _ := latestHistoryValue(tx, "hq_level_history", "hq_level", memberID)
		if *baseLevel > cur {
			if err := lastRankInsertHistory(tx, "hq_level_history", "hq_level", memberID, int64(*baseLevel), recordedAt); err == nil {
				hq++
			}
		}
	}
	return
}

// addGlobalAliasOverwritingOCR adds a global alias, first removing any OCR or
// stale global alias with the same name (member_aliases has no unique index, so
// INSERT OR IGNORE can't dedupe). Global is authoritative over background OCR;
// per-user personal aliases are left alone (they win via the resolution order).
func addGlobalAliasOverwritingOCR(tx *sql.Tx, memberID int, alias string) {
	tx.Exec("DELETE FROM member_aliases WHERE LOWER(alias) = LOWER(?) AND category IN ('ocr', 'global')", alias)
	tx.Exec("INSERT INTO member_aliases (member_id, user_id, category, alias) VALUES (?, NULL, 'global', ?)", memberID, alias)
}

// lastRankInsertHistory appends a 'lastrank'-sourced datapoint. recordedAtSQLite
// empty → fall back to CURRENT_TIMESTAMP (capture date was unparseable).
func lastRankInsertHistory(tx *sql.Tx, table, valueCol string, memberID int, value int64, recordedAtSQLite string) error {
	if recordedAtSQLite != "" {
		_, err := tx.Exec("INSERT INTO "+table+" ("+"member_id, "+valueCol+", recorded_at, source) VALUES (?, ?, ?, 'lastrank')", memberID, value, recordedAtSQLite)
		return err
	}
	_, err := tx.Exec("INSERT INTO "+table+" ("+"member_id, "+valueCol+", source) VALUES (?, ?, 'lastrank')", memberID, value)
	return err
}

// lastRankResolveMember matches a LastRank entry to a roster member, trying the
// stored lastrank_public_id (captured on a previous sync) FIRST: it's
// authoritative and survives an in-game name change, which exact/alias matching
// can't — a renamed member would otherwise land in the unmatched pile. An entry
// with no stored id (a first-time sync) falls through to the shared name/alias
// resolution unchanged.
//
// This is the one-shot form, for callers that resolve a single entry. The
// alliance preview instead runs the same priority as explicit passes over the
// whole roster (see lastRankPreview), so a public_id match can't be beaten to a
// member by another entry's name match.
// idx is the shared accent-folded index for tier 3; pass nil to have it built on
// demand (see resolveMemberAliasWithIndex).
func lastRankResolveMember(tx *sql.Tx, name string, publicID, userID int, idx *foldedNameIndex) (*Member, string, error) {
	if publicID != 0 {
		var m Member
		err := tx.QueryRow("SELECT id, name, rank FROM members WHERE lastrank_public_id = ?", publicID).Scan(&m.ID, &m.Name, &m.Rank)
		if err == nil {
			return &m, "public_id", nil
		}
	}
	return resolveMemberAliasWithIndex(tx, name, userID, idx)
}

// lastRankPlayerSearch backs the recruiting player picker. Mirrors
// searchExternalAlliancesLastRank: bounded context, generic client-facing errors,
// and a distinct 503 when upstream is merely slow so the UI can say "try again"
// rather than "broken".
//
// Server is optional here, unlike the alliance picker. A prospect is somebody
// else's player and is frequently on another server, so defaulting to a strict
// server filter would hide the very people recruiters are looking for.
func lastRankPlayerSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		http.Error(w, "Enter a player name to search", http.StatusBadRequest)
		return
	}
	var server *int
	if s := strings.TrimSpace(r.URL.Query().Get("server")); s != "" {
		n, ok := parseServerNumber(s)
		if !ok {
			http.Error(w, "server must be a number", http.StatusBadRequest)
			return
		}
		server = &n
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	results, err := searchLastRankPlayers(ctx, q, server, 20)
	if err != nil {
		slogLastRank("lastRankPlayerSearch failed", err)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			http.Error(w, "LastRank is busy right now — try again in a moment", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "Could not reach LastRank for that search", http.StatusBadGateway)
		return
	}
	writeJSON(w, results)
}

// --- Phase 1: alliance preview ---

func lastRankPreview(w http.ResponseWriter, r *http.Request) {
	userID := getAuthUser(r).ID

	allianceID := lastRankAllianceID()
	if allianceID == "" {
		http.Error(w, "Configure the LastRank alliance ID in Settings first.", http.StatusBadRequest)
		return
	}

	alliance, err := fetchLastRankAlliance(r.Context(), allianceID)
	if err != nil {
		slogLastRank("lastrank alliance fetch failed", err)
		http.Error(w, "Couldn't reach LastRank. Try again later.", http.StatusBadGateway)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	resp := LastRankSyncPreviewResponse{
		Alliance: LastRankAllianceMeta{
			AllianceID: alliance.AllianceID,
			Abbr:       alliance.Abbr,
			Name:       alliance.Name,
			ServerID:   alliance.ServerID,
			CurMember:  alliance.CurMember,
			MaxMember:  alliance.MaxMember,
			LastSeenAt: alliance.LastSeenAt,
		},
	}

	// Reconciliation tracking. A roster member is "confirmed active" only when a
	// *ranked* LastRank member resolves to them. Unranked LastRank members are
	// treated as likely-departed (EX) — they neither update nor confirm.
	confirmed := map[int]bool{}
	unrankedMatched := map[int]bool{}

	capture := alliance.LastSeenAt

	// Resolve every entry to a roster member BEFORE building diffs, in passes, so
	// a stored public_id always beats a name/alias match on a different entry:
	//   0. ranked entries with a stored lastrank_public_id (authoritative)
	//   1. ranked entries by name/alias, then accent-folded (first-time syncs,
	//      no stored id; folding rescues "Pàcha" vs "Pacha", which would
	//      otherwise land in BOTH unmatched and the archive candidates)
	//   2. unranked entries — resolved only to mark "likely left"; never claim,
	//      so an unranked entry can't take a member away from a ranked one.
	// A member is claimed by at most one entry; a later entry resolving to an
	// already-claimed member is reported as unmatched rather than proposing a
	// second, conflicting set of changes for the same member.
	// Built once for the whole reconciliation: resolveMemberAlias's tier-3 folded
	// fallback would otherwise rebuild it per entry, making this O(N²) queries on
	// the app's single connection. A failure here is not fatal — tiers 1 and 2
	// still work, we just lose accent tolerance for this run.
	foldIdx, err := buildFoldedNameIndex(tx, userID)
	if err != nil {
		slogLastRank("lastrank folded name index build failed; continuing without accent tolerance", err)
		foldIdx = nil
	}

	matches := make([]*Member, len(alliance.Members))
	matchTypes := make([]string, len(alliance.Members))
	ranked := make([]bool, len(alliance.Members))
	claimed := map[int]bool{}
	for i, lm := range alliance.Members {
		ranked[i] = lastRankRankToString(lm.AllianceRank) != ""
	}

	for i, lm := range alliance.Members {
		if !ranked[i] || lm.PublicID == 0 {
			continue
		}
		var m Member
		if err := tx.QueryRow("SELECT id, name, rank FROM members WHERE lastrank_public_id = ?", lm.PublicID).Scan(&m.ID, &m.Name, &m.Rank); err != nil || claimed[m.ID] {
			continue
		}
		matches[i], matchTypes[i], claimed[m.ID] = &m, "public_id", true
	}

	for i, lm := range alliance.Members {
		if !ranked[i] || matches[i] != nil {
			continue
		}
		m, mt, err := resolveMemberAliasWithIndex(tx, lm.Name, userID, foldIdx)
		if err != nil || m == nil || claimed[m.ID] {
			continue
		}
		matches[i], matchTypes[i], claimed[m.ID] = m, mt, true
	}

	for i, lm := range alliance.Members {
		if ranked[i] {
			continue
		}
		if m, _, err := lastRankResolveMember(tx, lm.Name, lm.PublicID, userID, foldIdx); err == nil && m != nil {
			matches[i] = m
		}
	}

	for i, lm := range alliance.Members {
		rankStr := lastRankRankToString(lm.AllianceRank)
		member, matchType := matches[i], matchTypes[i]

		// Unranked on LastRank ≈ left the alliance: don't update or confirm.
		if rankStr == "" {
			if member != nil {
				unrankedMatched[member.ID] = true
			}
			continue
		}

		if member == nil {
			resp.Unmatched = append(resp.Unmatched, LastRankUnmatched{
				LastRankName:     lm.Name,
				LastRankPublicID: lm.PublicID,
				Power:            int64Ptr(lm.Power),
				HeroPower:        lm.HeroPower,
				Rank:             rankStr,
				BaseLevel:        lm.BaseLevel,
			})
			continue
		}
		confirmed[member.ID] = true

		diff := LastRankMemberDiff{
			LastRankName:     lm.Name,
			LastRankPublicID: lm.PublicID,
			MatchedMember:    member,
			MatchType:        matchType,
			Power:            lastRankStatDiff(tx, "power_history", "power", member.ID, lm.Power, capture),
		}
		if lm.HeroPower != nil {
			diff.HeroPower = lastRankStatDiff(tx, "hero_power_history", "power", member.ID, *lm.HeroPower, capture)
		}
		if lm.BaseLevel != nil && *lm.BaseLevel > 0 {
			curLevel, _ := latestHistoryValue(tx, "hq_level_history", "hq_level", member.ID)
			hq := &LastRankHQDiff{Current: curLevel, New: *lm.BaseLevel}
			if *lm.BaseLevel > curLevel {
				hq.Apply = true
			} else {
				hq.SkipReason = "not_higher"
			}
			diff.HQLevel = hq
		}
		if newRank := lastRankRankToString(lm.AllianceRank); newRank != "" && newRank != member.Rank {
			diff.RankDiff = &LastRankRankDiff{Current: member.Rank, New: newRank}
		}
		// Matched via an alias or a stored public_id, but under a name that differs
		// from our primary → likely a rename. Surface it for review (case-only
		// differences are ignored).
		if !strings.EqualFold(lm.Name, member.Name) {
			diff.NameChange = &LastRankNameChange{Current: member.Name, New: lm.Name}
		}
		resp.Matched = append(resp.Matched, diff)
	}

	// Roster for the unmatched assign dropdown (active members only).
	rows, err := tx.Query("SELECT id, name, rank FROM members WHERE rank != 'EX' ORDER BY LOWER(name) ASC")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var m Member
			if err := rows.Scan(&m.ID, &m.Name, &m.Rank); err == nil {
				resp.AllMembers = append(resp.AllMembers, m)
			}
		}
	}

	// Active members LastRank didn't confirm — surface for optional archiving.
	for _, m := range resp.AllMembers {
		if confirmed[m.ID] {
			continue
		}
		reason := "Not in the LastRank roster"
		if unrankedMatched[m.ID] {
			reason = "Unranked on LastRank (likely left)"
		}
		resp.ArchiveCandidates = append(resp.ArchiveCandidates, LastRankArchiveCandidate{
			MemberID: m.ID, Name: m.Name, Rank: m.Rank, Reason: reason,
		})
	}

	// Persist the decisions this pull proposes, so they outlive the modal and a
	// deferral means something. Stats stay ephemeral on purpose: they are
	// staleness-gated append-only history and need no decision.
	//
	// The transaction was previously read-only (deferred rollback as a consistent
	// snapshot). It now has to commit — but only the queue is written here; nothing
	// this preview shows has been applied.
	if err := reconcilePendingChanges(tx, buildPendingProposals(resp), capture); err != nil {
		slog.Error("lastrank queue reconcile failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("lastrank preview commit failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if pending, err := loadPendingChanges(true); err == nil {
		resp.Pending = pending
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// buildPendingProposals turns a preview into the decisions worth queueing.
//
// Deliberately NOT the stat diffs: power / hero / HQ are staleness-gated,
// provenance-stamped and append-only, so there is nothing for an officer to
// decide. What lands here is what a human has to judge — a rank moved, a member
// renamed, a name we can't place, a member who looks gone.
func buildPendingProposals(resp LastRankSyncPreviewResponse) []pendingProposal {
	var out []pendingProposal

	for _, m := range resp.Matched {
		if m.MatchedMember == nil {
			continue
		}
		if m.RankDiff != nil {
			out = append(out, pendingProposal{
				Kind: PendingKindRank, MemberID: m.MatchedMember.ID, PublicID: m.LastRankPublicID,
				LastRankName: m.LastRankName,
				CurrentValue: m.RankDiff.Current, ProposedValue: m.RankDiff.New,
			})
		}
		if m.NameChange != nil {
			out = append(out, pendingProposal{
				Kind: PendingKindName, MemberID: m.MatchedMember.ID, PublicID: m.LastRankPublicID,
				LastRankName: m.LastRankName,
				CurrentValue: m.NameChange.Current, ProposedValue: m.NameChange.New,
			})
		}
	}
	for _, u := range resp.Unmatched {
		out = append(out, pendingProposal{
			Kind: PendingKindUnmatched, PublicID: u.LastRankPublicID,
			LastRankName: u.LastRankName, ProposedValue: u.LastRankName,
			Reason: "On LastRank as " + u.Rank + ", matched no roster member",
		})
	}
	for _, c := range resp.ArchiveCandidates {
		out = append(out, pendingProposal{
			Kind: PendingKindArchive, MemberID: c.MemberID,
			LastRankName: c.Name, CurrentValue: c.Rank, ProposedValue: "EX",
			Reason: c.Reason,
		})
	}
	return out
}

// --- Phase 1: commit ---

func lastRankCommit(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	var req LastRankCommitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Capture date for recorded_at; "" → inserts fall back to CURRENT_TIMESTAMP.
	recordedAt, _ := lastRankCaptureToSQLite(req.CaptureDate)

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var powerN, heroN, hqN, rankN, aliasN, renameN, addN, nameN int

	for _, m := range req.Members {
		if m.MemberID == 0 {
			continue
		}
		// Name change disposition (matched-via-alias rename). Shared with the
		// review-queue path so the two can never drift — see lastrank_apply.go.
		if m.NameNew != "" {
			if ok, err := applyNameChange(tx, m.MemberID, m.NameAction, m.NameNew); err != nil {
				dbError(w, "lastRankCommit name change", err)
				return
			} else if ok {
				nameN++
			}
		}
		if m.Power != nil {
			if err := lastRankInsertHistory(tx, "power_history", "power", m.MemberID, *m.Power, recordedAt); err == nil {
				powerN++
			}
		}
		if m.HeroPower != nil {
			if err := lastRankInsertHistory(tx, "hero_power_history", "power", m.MemberID, *m.HeroPower, recordedAt); err == nil {
				heroN++
			}
		}
		if m.HQLevel != nil {
			// HQ level is history-only; stamp with the capture date + 'lastrank' source.
			if err := lastRankInsertHistory(tx, "hq_level_history", "hq_level", m.MemberID, int64(*m.HQLevel), recordedAt); err == nil {
				hqN++
			}
		}
		if ok, err := applyRankChange(tx, m.MemberID, m.NewRank); err != nil {
			dbError(w, "lastRankCommit rank change", err)
			return
		} else if ok {
			rankN++
		}
		// Always capture the public_id + mark synced, even with no stat change.
		stampLastRankIdentity(tx, m.MemberID, m.LastRankPublicID)
	}

	for _, u := range req.Unmatched {
		out, err := applyUnmatchedAction(tx, u, recordedAt, req.CaptureDate)
		if err != nil {
			dbError(w, "lastRankCommit unmatched action", err)
			return
		}
		switch {
		case out.Aliased:
			aliasN++
		case out.Renamed:
			renameN++
		case out.Added:
			addN++
		}
		// Paired stats count toward the same totals as the matched path, so the
		// summary reflects everything the import actually wrote.
		powerN += out.PowerRows
		heroN += out.HeroRows
		hqN += out.HQRows
	}

	// Archive members the officer confirmed as departed (rank → EX).
	var archiveN int
	for _, mid := range req.Archive {
		ok, err := applyArchive(tx, mid)
		if err != nil {
			dbError(w, "lastRankCommit archive", err)
			return
		}
		if ok {
			archiveN++
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to save changes", http.StatusInternalServerError)
		return
	}

	details := "via LastRank — power (" + strconv.Itoa(powerN) + "), hero power (" + strconv.Itoa(heroN) + "), HQ (" + strconv.Itoa(hqN) + ") updated"
	if rankN > 0 {
		details += "; " + strconv.Itoa(rankN) + " rank changes"
	}
	if aliasN > 0 || renameN > 0 || addN > 0 {
		details += "; " + strconv.Itoa(aliasN) + " aliases, " + strconv.Itoa(renameN) + " renames, " + strconv.Itoa(addN) + " added"
	}
	if nameN > 0 {
		details += "; " + strconv.Itoa(nameN) + " name changes"
	}
	if archiveN > 0 {
		details += "; " + strconv.Itoa(archiveN) + " archived"
	}
	logActivity(user.ID, user.Username, "imported", "lastrank_sync", "Alliance roster", false, details)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"power_updated":    powerN,
		"hero_updated":     heroN,
		"hq_updated":       hqN,
		"rank_updated":     rankN,
		"aliases_saved":    aliasN,
		"members_renamed":  renameN,
		"members_added":    addN,
		"members_archived": archiveN,
	})
}

// --- Phase 2: per-player extended sync (browser-driven) ---
//
// One call per member. Writes are deferred-logged: the data writes here are
// summarized by a single lastRankFinish call at the end of the browser loop,
// rather than one activity row per member.

// errMemberNotFound lets syncOneMember's callers tell "no such member" apart from
// an upstream failure — the HTTP handler maps it to 404, the job runner to a
// skipped item.
var errMemberNotFound = errors.New("member not found")

func lastRankSyncPlayer(w http.ResponseWriter, r *http.Request) {
	var req LastRankPlayerSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	out, err := syncOneMember(r.Context(), req.MemberID)
	if err != nil {
		if errors.Is(err, errMemberNotFound) {
			http.Error(w, "Member not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, errLastRankUpstream) {
			slogLastRank("lastrank player fetch failed", err)
			http.Error(w, "Couldn't reach LastRank for this player.", http.StatusBadGateway)
			return
		}
		slog.Error("lastrank player sync failed", "member_id", req.MemberID, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, out)
}

// syncOneMember refreshes one member from their LastRank player record: kills,
// power, hero power, HQ, profession + profession level, and avatar — all free from
// the single fetch. Shared by the HTTP handler and the background job runner.
//
// Shaped for db.SetMaxOpenConns(1): the member row is read and the cursor closed,
// THEN the network call happens with no database handle held, THEN a transaction
// opens for the writes. Reordering these deadlocks the whole process silently.
func syncOneMember(ctx context.Context, memberID int) (LastRankPlayerSyncResponse, error) {
	var pubID sql.NullInt64
	var name, curPhoto string
	err := db.QueryRow("SELECT lastrank_public_id, name, COALESCE(lastrank_photo_url, '') FROM members WHERE id = ?", memberID).Scan(&pubID, &name, &curPhoto)
	if err != nil {
		return LastRankPlayerSyncResponse{MemberID: memberID}, errMemberNotFound
	}

	out := LastRankPlayerSyncResponse{MemberID: memberID, LastRankName: name}
	if !pubID.Valid {
		out.SkipReason = "no_id"
		return out, nil
	}

	// Bulk sync: cheap cached GET, upgraded to a live enrich only when the record
	// is stale (older than the freshness window). Recently-enriched members stay
	// fast; the GET is the fallback if a stale member's enrich is slow/fails.
	player, err := lastRankPlayerBulk(ctx, int(pubID.Int64))
	if err != nil {
		return out, err
	}

	kills := player.ArmyKill
	out.Kills = &kills
	out.LastRankName = player.Name
	out.CaptureDate = player.LastSeenAt
	out.PhotoUpdated = player.PhotoURL != "" && player.PhotoURL != curPhoto

	tx, err := db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()

	cur, at, ok := lastRankLatestHistory(tx, "kill_history", "kills", memberID)
	recordedAt, _ := lastRankCaptureToSQLite(player.LastSeenAt)
	switch {
	case !lastRankCaptureNewer(player.LastSeenAt, at):
		out.SkipReason = "stale"
	case ok && cur == kills:
		out.SkipReason = "unchanged"
	default:
		if err := lastRankInsertHistory(tx, "kill_history", "kills", memberID, kills, recordedAt); err == nil {
			out.KillsApplied = true
		}
	}

	// Power, hero power, and HQ ride along for free (already in the record).
	// Power/hero use per-metric staleness gating; HQ only ever increases.
	power := player.Power
	pN, hN, hqN := lastRankApplyPairedStats(tx, memberID, &power, player.HeroPower, player.BaseLevel, recordedAt, player.LastSeenAt)
	out.PowerApplied = pN > 0
	out.HeroApplied = hN > 0
	out.HQApplied = hqN > 0

	// Profession level (career_lv): history-only, per-metric staleness gate + dedup
	// (same shape as the kill history above).
	if player.CareerLv > 0 {
		cur, at, ok := lastRankLatestHistory(tx, "profession_level_history", "profession_level", memberID)
		if lastRankCaptureNewer(player.LastSeenAt, at) && !(ok && cur == int64(player.CareerLv)) {
			if err := lastRankInsertHistory(tx, "profession_level_history", "profession_level", memberID, int64(player.CareerLv), recordedAt); err == nil {
				out.ProfessionLevelApplied = true
			}
		}
	}

	// Career type → profession label. Only when the code is known and the mapped
	// label differs; an unset/unknown code must never clobber an existing profession.
	if label, ok := CareerTypeLabels[player.CareerType]; ok {
		var curProf string
		tx.QueryRow("SELECT COALESCE(profession, '') FROM members WHERE id = ?", memberID).Scan(&curProf)
		if label != curProf {
			if _, err := tx.Exec("UPDATE members SET profession = ? WHERE id = ?", label, memberID); err == nil {
				out.ProfessionChanged = true
			}
		}
	}

	// Always advance synced_at so the oldest-first Phase-2 ordering progresses
	// even when a member is skipped (keeps re-runs from re-fetching the same one).
	// Avatar URLs are refreshed here too (hotlinked from the game CDN).
	tx.Exec("UPDATE members SET lastrank_synced_at = strftime('%Y-%m-%d %H:%M:%f','now'), lastrank_public_id = ?, lastrank_photo_url = ?, lastrank_photo_failover = ? WHERE id = ?",
		pubID.Int64, player.PhotoURL, player.PhotoURLFailover, memberID)

	if err := tx.Commit(); err != nil {
		return out, err
	}
	out.SyncedAt = "just now"
	return out, nil
}

// refreshOneProspect fetches a prospect's LastRank record and persists power, hero
// power and avatar. Shared by the on-demand lookup handler and the bulk job.
//
// bulk picks the fetch strategy: a single on-demand lookup forces a fresh enrich
// (the recruiter is waiting and wants live numbers), while a bulk refresh uses the
// gentle GET-then-enrich-if-stale hybrid so a recruiter with many linked prospects
// doesn't trigger a live game pull for each one.
//
// Shaped for db.SetMaxOpenConns(1): the caller has already resolved pubID and
// closed its cursor, the fetch holds no database handle, and the writes follow.
func refreshOneProspect(ctx context.Context, prospectID, pubID int, bulk bool) (LastRankProspectLookupResponse, error) {
	out := LastRankProspectLookupResponse{ProspectID: prospectID, LastRankPublicID: pubID}

	var player *lastrankPlayerResp
	var err error
	if bulk {
		player, err = lastRankPlayerBulk(ctx, pubID)
	} else {
		player, err = lastRankPlayerFresh(ctx, pubID)
	}
	if err != nil {
		return out, err
	}

	heroVal := int64(0)
	if player.HeroPower != nil {
		heroVal = *player.HeroPower
	}
	// Wrapped so a partial write can't survive, per the standing convention. The
	// staleness gate this still lacks (a cached response can overwrite a fresher
	// hand-entered figure) needs prospects.lastrank_captured_at, which arrives with
	// the scheduler — it matters once this runs unattended, not on a click.
	tx, err := db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE prospects SET power = ?, hero_power = ?, lastrank_photo_url = ?, lastrank_photo_failover = ?
		WHERE id = ?`, player.Power, heroVal, player.PhotoURL, player.PhotoURLFailover, prospectID); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}

	out.LastRankName = player.Name
	out.Power = int64Ptr(player.Power)
	out.HeroPower = player.HeroPower
	out.ServerID = player.HomeServerID
	out.BaseLevel = player.BaseLevel
	out.Rank = lastRankRankToString(player.AllianceRank)
	out.CaptureDate = player.LastSeenAt
	out.Updated = true
	if player.AllianceAbbr != nil {
		out.AllianceAbbr = *player.AllianceAbbr
	}
	if player.AllianceName != nil {
		out.AllianceName = *player.AllianceName
	}
	return out, nil
}

// lastRankFinish logs the single summary row for a browser-driven batch.
func lastRankFinish(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var req LastRankFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	switch req.Kind {
	case "prospects":
		if req.ProspectsSynced > 0 {
			logActivity(user.ID, user.Username, "updated", "lastrank_sync", "Prospects",
				false, "via LastRank — refreshed "+strconv.Itoa(req.ProspectsSynced)+" prospects")
		}
	default: // "extended"
		if req.MembersSynced > 0 {
			details := "via LastRank — " + strconv.Itoa(req.MembersSynced) + " members synced; " +
				strconv.Itoa(req.KillRecords) + " kills, " + strconv.Itoa(req.PowerRecords) + " power, " +
				strconv.Itoa(req.HeroRecords) + " hero, " + strconv.Itoa(req.HQRecords) + " HQ, " +
				strconv.Itoa(req.ProfessionRecords) + " profession-level, " + strconv.Itoa(req.ProfessionChanges) + " profession, " +
				strconv.Itoa(req.PhotoRecords) + " photos updated"
			logActivity(user.ID, user.Username, "imported", "lastrank_sync", "Extended sync", false, details)
		}
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

// --- Recruiting: prospect lookup ---

func lastRankProspectLookup(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	var req LastRankProspectLookupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Resolve the public_id: from a freshly-pasted URL/id, else the stored one.
	var pubID int
	if strings.TrimSpace(req.LastRankInput) != "" {
		id, ok := parseLastRankPlayerID(req.LastRankInput)
		if !ok {
			http.Error(w, "Couldn't read a LastRank player ID or URL.", http.StatusBadRequest)
			return
		}
		pubID = id
		db.Exec("UPDATE prospects SET lastrank_public_id = ? WHERE id = ?", pubID, req.ProspectID)
	} else {
		var stored sql.NullInt64
		var pname string
		if err := db.QueryRow("SELECT lastrank_public_id, name FROM prospects WHERE id = ?", req.ProspectID).Scan(&stored, &pname); err != nil {
			http.Error(w, "Prospect not found", http.StatusNotFound)
			return
		}
		if !stored.Valid {
			http.Error(w, "No LastRank ID stored for this prospect. Paste a LastRank URL or ID first.", http.StatusBadRequest)
			return
		}
		pubID = int(stored.Int64)
	}

	out, err := refreshOneProspect(r.Context(), req.ProspectID, pubID, req.Bulk)
	if err != nil {
		slogLastRank("lastrank prospect lookup failed", err)
		http.Error(w, "Couldn't reach LastRank for this player.", http.StatusBadGateway)
		return
	}

	if !req.Bulk {
		var pname string
		db.QueryRow("SELECT name FROM prospects WHERE id = ?", req.ProspectID).Scan(&pname)
		logActivity(user.ID, user.Username, "updated", "prospect", pname, false, "via LastRank — power, hero power")
	}

	writeJSON(w, out)
}

// --- small helpers ---

func int64Ptr(v int64) *int64 { return &v }

// provenanceSource normalizes a client-declared import origin to the closed
// source vocabulary used by the history tables; unknown/empty → neutral "import".
func provenanceSource(s string) string {
	switch s {
	case "ocr", "csv", "mobile", "manual", "lastrank":
		return s
	default:
		return "import"
	}
}
