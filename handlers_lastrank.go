package main

// handlers_lastrank.go — HTTP handlers for the LastRank.fun enrichment feature.
// All upstream fetching/throttling lives in lastrank_client.go; this file does
// matching, the stale/changed gating, DB writes (with 'lastrank' provenance and
// the capture date as recorded_at), and activity logging.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
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
		var curLevel int
		tx.QueryRow("SELECT COALESCE(level, 0) FROM members WHERE id = ?", memberID).Scan(&curLevel)
		if *baseLevel > curLevel {
			if _, err := tx.Exec("UPDATE members SET level = ? WHERE id = ?", *baseLevel, memberID); err == nil {
				hq++
			}
		}
	}
	return
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
	for _, lm := range alliance.Members {
		rankStr := lastRankRankToString(lm.AllianceRank)
		member, matchType, mErr := resolveMemberAlias(tx, lm.Name, userID)
		matched := mErr == nil && member != nil

		// Unranked on LastRank ≈ left the alliance: don't update or confirm.
		if rankStr == "" {
			if matched {
				unrankedMatched[member.ID] = true
			}
			continue
		}

		if !matched {
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
			var curLevel int
			tx.QueryRow("SELECT COALESCE(level, 0) FROM members WHERE id = ?", member.ID).Scan(&curLevel)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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

	var powerN, heroN, hqN, rankN, aliasN, renameN, addN int

	for _, m := range req.Members {
		if m.MemberID == 0 {
			continue
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
			if _, err := tx.Exec("UPDATE members SET level = ? WHERE id = ?", *m.HQLevel, m.MemberID); err == nil {
				hqN++
			}
		}
		if m.NewRank != "" {
			if _, err := tx.Exec("UPDATE members SET rank = ? WHERE id = ?", m.NewRank, m.MemberID); err == nil {
				rankN++
			}
		}
		// Always capture the public_id + mark synced, even with no stat change.
		if m.LastRankPublicID != 0 {
			tx.Exec("UPDATE members SET lastrank_public_id = ?, lastrank_synced_at = CURRENT_TIMESTAMP WHERE id = ?", m.LastRankPublicID, m.MemberID)
		} else {
			tx.Exec("UPDATE members SET lastrank_synced_at = CURRENT_TIMESTAMP WHERE id = ?", m.MemberID)
		}
	}

	for _, u := range req.Unmatched {
		switch u.Action {
		case "alias":
			if u.MemberID == 0 {
				continue
			}
			// Global alias — clear any same-named global/ocr alias first.
			tx.Exec("DELETE FROM member_aliases WHERE LOWER(alias) = LOWER(?)", u.LastRankName)
			if _, err := tx.Exec("INSERT INTO member_aliases (member_id, category, alias) VALUES (?, 'global', ?)", u.MemberID, u.LastRankName); err == nil {
				aliasN++
				if u.LastRankPublicID != 0 {
					tx.Exec("UPDATE members SET lastrank_public_id = ?, lastrank_synced_at = CURRENT_TIMESTAMP WHERE id = ?", u.LastRankPublicID, u.MemberID)
				}
				if u.ApplyStats {
					p, h, hq := lastRankApplyPairedStats(tx, u.MemberID, u.Power, u.HeroPower, u.BaseLevel, recordedAt, req.CaptureDate)
					powerN += p
					heroN += h
					hqN += hq
				}
			}
		case "rename":
			if u.MemberID == 0 {
				continue
			}
			if _, err := tx.Exec("UPDATE members SET name = ? WHERE id = ?", u.LastRankName, u.MemberID); err == nil {
				renameN++
				if u.LastRankPublicID != 0 {
					tx.Exec("UPDATE members SET lastrank_public_id = ?, lastrank_synced_at = CURRENT_TIMESTAMP WHERE id = ?", u.LastRankPublicID, u.MemberID)
				}
				if u.ApplyStats {
					p, h, hq := lastRankApplyPairedStats(tx, u.MemberID, u.Power, u.HeroPower, u.BaseLevel, recordedAt, req.CaptureDate)
					powerN += p
					heroN += h
					hqN += hq
				}
			}
		case "add":
			rank := u.NewRank
			if rank == "" {
				rank = "R1"
			}
			var pubID interface{}
			if u.LastRankPublicID != 0 {
				pubID = u.LastRankPublicID
			}
			res, err := tx.Exec("INSERT INTO members (name, rank, level, eligible, lastrank_public_id) VALUES (?, ?, 0, 1, ?)", u.LastRankName, rank, pubID)
			if err == nil {
				addN++
				if u.ApplyStats {
					if newID, lerr := res.LastInsertId(); lerr == nil {
						p, h, hq := lastRankApplyPairedStats(tx, int(newID), u.Power, u.HeroPower, u.BaseLevel, recordedAt, req.CaptureDate)
						powerN += p
						heroN += h
						hqN += hq
					}
				}
			}
		}
	}

	// Archive members the officer confirmed as departed (rank → EX).
	var archiveN int
	for _, mid := range req.Archive {
		if mid == 0 {
			continue
		}
		res, err := tx.Exec("UPDATE members SET rank = 'EX', eligible = 0, leave_reason = ? WHERE id = ? AND rank != 'EX'", "Left alliance (via LastRank)", mid)
		if err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				archiveN++
			}
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
	if archiveN > 0 {
		details += "; " + strconv.Itoa(archiveN) + " archived"
	}
	logActivity(user.ID, user.Username, "imported", "lastrank_sync", "Alliance roster", false, details)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"power_updated":   powerN,
		"hero_updated":    heroN,
		"hq_updated":      hqN,
		"rank_updated":    rankN,
		"aliases_saved":   aliasN,
		"members_renamed": renameN,
		"members_added":   addN,
		"members_archived": archiveN,
	})
}

// --- Phase 2: per-player extended sync (browser-driven) ---
//
// One call per member. Writes are deferred-logged: the data writes here are
// summarized by a single lastRankFinish call at the end of the browser loop,
// rather than one activity row per member.

func lastRankSyncPlayer(w http.ResponseWriter, r *http.Request) {
	var req LastRankPlayerSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var pubID sql.NullInt64
	var name string
	err := db.QueryRow("SELECT lastrank_public_id, name FROM members WHERE id = ?", req.MemberID).Scan(&pubID, &name)
	if err != nil {
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}

	out := LastRankPlayerSyncResponse{MemberID: req.MemberID, LastRankName: name}
	if !pubID.Valid {
		out.SkipReason = "no_id"
		writeJSON(w, out)
		return
	}

	player, err := fetchLastRankPlayer(r.Context(), int(pubID.Int64))
	if err != nil {
		slogLastRank("lastrank player fetch failed", err)
		http.Error(w, "Couldn't reach LastRank for this player.", http.StatusBadGateway)
		return
	}

	kills := player.ArmyKill
	out.Kills = &kills
	out.LastRankName = player.Name
	out.CaptureDate = player.LastSeenAt

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	cur, at, ok := lastRankLatestHistory(tx, "kill_history", "kills", req.MemberID)
	recordedAt, _ := lastRankCaptureToSQLite(player.LastSeenAt)
	switch {
	case !lastRankCaptureNewer(player.LastSeenAt, at):
		out.SkipReason = "stale"
	case ok && cur == kills:
		out.SkipReason = "unchanged"
	default:
		if err := lastRankInsertHistory(tx, "kill_history", "kills", req.MemberID, kills, recordedAt); err == nil {
			out.KillsApplied = true
		}
	}

	// Always advance synced_at so the oldest-first Phase-2 ordering progresses
	// even when a member is skipped (keeps re-runs from re-fetching the same one).
	// Avatar URLs are refreshed here too (hotlinked from the game CDN).
	tx.Exec("UPDATE members SET lastrank_synced_at = CURRENT_TIMESTAMP, lastrank_public_id = ?, lastrank_photo_url = ?, lastrank_photo_failover = ? WHERE id = ?",
		pubID.Int64, player.PhotoURL, player.PhotoURLFailover, req.MemberID)

	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to save changes", http.StatusInternalServerError)
		return
	}
	out.SyncedAt = "just now"
	writeJSON(w, out)
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
			logActivity(user.ID, user.Username, "imported", "lastrank_sync", "Extended sync",
				false, "via LastRank — army kills updated for "+strconv.Itoa(req.MembersSynced)+" members ("+strconv.Itoa(req.KillRecords)+" records)")
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

	player, err := fetchLastRankPlayer(r.Context(), pubID)
	if err != nil {
		slogLastRank("lastrank prospect fetch failed", err)
		http.Error(w, "Couldn't reach LastRank for this player.", http.StatusBadGateway)
		return
	}

	// Persist the enrichment onto the prospect record.
	heroVal := int64(0)
	if player.HeroPower != nil {
		heroVal = *player.HeroPower
	}
	db.Exec("UPDATE prospects SET power = ?, hero_power = ?, lastrank_photo_url = ?, lastrank_photo_failover = ? WHERE id = ?",
		player.Power, heroVal, player.PhotoURL, player.PhotoURLFailover, req.ProspectID)

	out := LastRankProspectLookupResponse{
		ProspectID:       req.ProspectID,
		LastRankPublicID: pubID,
		LastRankName:     player.Name,
		Power:            int64Ptr(player.Power),
		HeroPower:        player.HeroPower,
		ServerID:         player.HomeServerID,
		BaseLevel:        player.BaseLevel,
		Rank:             lastRankRankToString(player.AllianceRank),
		CaptureDate:      player.LastSeenAt,
		Updated:          true,
	}
	if player.AllianceAbbr != nil {
		out.AllianceAbbr = *player.AllianceAbbr
	}
	if player.AllianceName != nil {
		out.AllianceName = *player.AllianceName
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
