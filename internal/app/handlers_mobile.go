package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// mobileCategory is one /api/mobile/commit category: what it writes and the permission
// each record needs — the permission of the web page that writes the same data.
type mobileCategory struct {
	Perm   string
	VSDay  bool // a vs_points day column, named by the category
	Weekly bool // the Weekly Rank total: Saturday = total − Mon..Fri
	Stat   bool // a member history, by memberStats[category]
}

var mobileCategories = map[string]mobileCategory{
	"monday":           {Perm: "manage_vs_points", VSDay: true},
	"tuesday":          {Perm: "manage_vs_points", VSDay: true},
	"wednesday":        {Perm: "manage_vs_points", VSDay: true},
	"thursday":         {Perm: "manage_vs_points", VSDay: true},
	"friday":           {Perm: "manage_vs_points", VSDay: true},
	"saturday":         {Perm: "manage_vs_points", VSDay: true},
	"weekly":           {Perm: "manage_vs_points", Weekly: true},
	"power":            {Perm: "manage_members", Stat: true},
	"kills":            {Perm: "manage_members", Stat: true},
	"hero_power":       {Perm: "manage_members", Stat: true},
	"squad_power":      {Perm: "manage_members", Stat: true},
	"hq_level":         {Perm: "manage_members", Stat: true},
	"profession_level": {Perm: "manage_members", Stat: true},
}

// mobileRosterQuerier is the subset of *sql.DB / *sql.Tx that
// loadMobileRoster needs, so the helper works against both.
type mobileRosterQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// loadMobileRoster returns active members with the aliases the given user is
// allowed to see (their own personals + all global + all OCR aliases).
// It mirrors the Exact → Personal → Global → OCR hierarchy described in
// lastwar-screen-definitions/README.md so the scanner's RosterAliasResolver
// can run the same lookup on-device.
func loadMobileRoster(q mobileRosterQuerier, userID int) ([]MobileMember, error) {
	rows, err := q.Query(`
		SELECT m.id, m.name, m.rank, a.alias, a.category
		FROM members m
		LEFT JOIN member_aliases a
		  ON a.member_id = m.id
		  AND (a.user_id IS NULL OR a.user_id = ?)
		WHERE m.rank != 'EX'
		ORDER BY m.name ASC, a.category ASC, a.alias ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := map[int]*MobileMember{}
	order := []int{}
	for rows.Next() {
		var (
			id            int
			name, rank    string
			alias, catSql sql.NullString
		)
		if err := rows.Scan(&id, &name, &rank, &alias, &catSql); err != nil {
			return nil, err
		}
		mm, ok := byID[id]
		if !ok {
			mm = &MobileMember{ID: id, Name: name, Rank: rank, Aliases: []MobileAlias{}}
			byID[id] = mm
			order = append(order, id)
		}
		if alias.Valid && catSql.Valid {
			mm.Aliases = append(mm.Aliases, MobileAlias{Alias: alias.String, Category: catSql.String})
		}
	}
	out := make([]MobileMember, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

// GET /api/mobile/members
// Returns active member list with aliases scoped to the current user, so the
// scanner's RosterAliasResolver can run Exact → Personal → Global → OCR locally.
func getMobileMembers(w http.ResponseWriter, r *http.Request) {
	claims := getMobileClaims(r)
	members, err := loadMobileRoster(db, claims.UserID)
	if err != nil {
		slog.Error("getMobileMembers: roster load failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(members)
}

// POST /api/mobile/preview
// Accepts structured OCR output, resolves aliases, returns matched/unresolved split.
// Read-only — does not write to the database.
func mobilePreview(w http.ResponseWriter, r *http.Request) {
	claims := getMobileClaims(r)

	var req MobilePreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("mobilePreview: begin tx failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Fetch all active members (with aliases scoped to the current user) for
	// the picker UI. Same shape as /api/mobile/members so the scanner can
	// reuse the cached roster — see RosterAliasResolver on the device.
	allMembers, err := loadMobileRoster(tx, claims.UserID)
	if err != nil {
		slog.Error("mobilePreview: roster load failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	resp := MobilePreviewResponse{
		WeekDate:   req.WeekDate,
		Matched:    []MobilePreviewMatch{},
		Unresolved: []MobilePreviewMatch{},
		AllMembers: allMembers,
	}

	// Built once for the whole batch — the tier-3 folded fallback inside
	// resolveMemberAlias would otherwise rebuild it per entry. A failure is
	// non-fatal: tiers 1 and 2 still resolve, we just lose accent tolerance.
	foldIdx, err := buildFoldedNameIndex(tx, claims.UserID)
	if err != nil {
		slog.Error("mobilePreview: folded name index build failed; continuing without accent tolerance", "error", err)
		foldIdx = nil
	}

	for _, entry := range req.Entries {
		match := MobilePreviewMatch{
			OriginalName: entry.Name,
			Category:     entry.Category,
			Score:        entry.Score,
			CapturedAt:   entry.CapturedAt, // echoed from the request, never re-read
		}
		member, matchType, err := resolveMemberAliasWithIndex(tx, entry.Name, claims.UserID, foldIdx)
		if err != nil {
			resp.Unresolved = append(resp.Unresolved, match)
		} else {
			match.MatchedMember = member
			match.MatchType = matchType
			resp.Matched = append(resp.Matched, match)
		}
	}

	resp.TotalSubmitted = len(req.Entries)
	resp.TotalMatched = len(resp.Matched)
	resp.TotalUnresolved = len(resp.Unresolved)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// POST /api/mobile/commit
// Persists confirmed scan data. Accepts resolution choices and optional alias mappings.
func mobileCommit(w http.ResponseWriter, r *http.Request) {
	claims := getMobileClaims(r)

	var req MobileCommitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate + snap to the game-time VS-week Monday so the client clock can't misfile the week.
	// TODO(mobile-scanner): the server is authoritative on week_date — it overwrites whatever the
	// client sends with the game-time Monday (UTC-2). Confirm the lastwar-android-scanner repo's own
	// week_date computation agrees, so previews and commits land in the same bucket.
	normWeek, err := normalizeToGameWeekMonday(req.WeekDate)
	if err != nil {
		http.Error(w, "Invalid week_date: must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	req.WeekDate = normWeek

	// Every permission a record can need is resolved before the transaction opens:
	// userHasPermission reads through db, and the pool's one connection belongs to the
	// transaction until it ends, so asking inside it would wait out the statement
	// ceiling and answer 500.
	user := getAuthUser(r)
	held := map[string]bool{
		"manage_vs_points": userHasPermission(user, "manage_vs_points"),
		"manage_members":   userHasPermission(user, "manage_members"),
	}
	canMembers := held["manage_members"]

	tx, err := db.Begin()
	if err != nil {
		slog.Error("mobileCommit: begin tx failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Pre-validate: build set of valid member IDs
	validMemberIDs := map[int]bool{}
	idRows, err := tx.Query("SELECT id FROM members WHERE rank != 'EX'")
	if err != nil {
		slog.Error("mobileCommit: member id query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	for idRows.Next() {
		var id int
		if err := idRows.Scan(&id); err == nil {
			validMemberIDs[id] = true
		}
	}
	idRows.Close()

	var commitErrors []string
	vsRecordsSaved := 0
	// Rows actually written, and readings equal to the member's latest row (skipped),
	// per member-stat category.
	recordsSaved := map[string]int{}
	recordsUnchanged := map[string]int{}
	now := time.Now()

	// Group VS records by member_id so we do one upsert per member.
	// vsFields[memberID] = map of day -> score; weekly[memberID] = the Weekly Rank total.
	vsFields := map[int]map[string]int{}
	vsNames := map[int]string{}
	weekly := map[int]int{}

	for _, rec := range req.Records {
		cat, ok := mobileCategories[rec.Category]
		if !ok {
			commitErrors = append(commitErrors, fmt.Sprintf("invalid category %q for %s", rec.Category, rec.OriginalName))
			continue
		}
		if !held[cat.Perm] {
			commitErrors = append(commitErrors, fmt.Sprintf("%s (%s): %s permission required", rec.OriginalName, rec.Category, cat.Perm))
			continue
		}
		if !validMemberIDs[rec.MemberID] {
			commitErrors = append(commitErrors, fmt.Sprintf("unrecognized member_id %d (%s): skipped", rec.MemberID, rec.OriginalName))
			continue
		}

		switch {
		case cat.Stat:
			st := memberStats[rec.Category]
			if err := checkMemberStat(rec.Category, rec.Score); err != nil {
				commitErrors = append(commitErrors, fmt.Sprintf("%s (%s): %v", rec.OriginalName, rec.Category, err))
				continue
			}
			if st.NoDecrease {
				if cur, ok := latestHistoryValue(tx, st.Table, st.Column, rec.MemberID); ok && rec.Score < int64(cur) {
					commitErrors = append(commitErrors, fmt.Sprintf("%s (%s): %d is below the stored %d, which the game can't lower — likely a misread",
						rec.OriginalName, rec.Category, rec.Score, cur))
					continue
				}
			}
			at, err := parseCapturedAt(rec.CapturedAt, now)
			if err != nil {
				commitErrors = append(commitErrors, fmt.Sprintf("%s (%s): %v", rec.OriginalName, rec.Category, err))
				continue
			}
			wrote, err := recordHistoryIfChanged(tx, st.Table, st.Column, rec.MemberID, rec.Score, "mobile", at)
			switch {
			case err != nil:
				slog.Error("mobileCommit: history write failed", "category", rec.Category, "error", err)
				commitErrors = append(commitErrors, fmt.Sprintf("%s insert failed for member_id %d: database error", rec.Category, rec.MemberID))
			case wrote:
				recordsSaved[rec.Category]++
			default:
				recordsUnchanged[rec.Category]++
			}
		case cat.Weekly:
			weekly[rec.MemberID] = int(rec.Score)
			vsNames[rec.MemberID] = rec.OriginalName
		default:
			if vsFields[rec.MemberID] == nil {
				vsFields[rec.MemberID] = map[string]int{}
			}
			vsNames[rec.MemberID] = rec.OriginalName
			vsFields[rec.MemberID][rec.Category] = int(rec.Score)
		}
	}

	// Weekly totals become Saturdays by the web preview's rule (saturdayFromTotal): never
	// from an incomplete week, and every member it can't derive is named. A Saturday sent
	// directly wins.
	saturdayDerived := 0
	for memberID, total := range weekly {
		fields := vsFields[memberID]
		if _, has := fields["saturday"]; has {
			continue
		}
		stored, err := storedWeekDays(tx, memberID, req.WeekDate)
		if err != nil {
			slog.Error("mobileCommit: reading the stored week failed", "member_id", memberID, "error", err)
			commitErrors = append(commitErrors, fmt.Sprintf("%s (weekly): the stored week could not be read", vsNames[memberID]))
			continue
		}
		sat, err := saturdayFromTotal(total, fields, stored)
		if err != nil {
			commitErrors = append(commitErrors, fmt.Sprintf("%s (weekly): %v", vsNames[memberID], err))
			continue
		}
		if fields == nil {
			fields = map[string]int{}
			vsFields[memberID] = fields
		}
		fields["saturday"] = sat
		saturdayDerived++
	}

	// Upsert VS records
	for memberID, fields := range vsFields {
		if len(fields) == 0 {
			continue
		}

		var existingID int
		err := tx.QueryRow("SELECT id FROM vs_points WHERE member_id = ? AND week_date = ?", memberID, req.WeekDate).Scan(&existingID)

		var vsErr error
		if err == sql.ErrNoRows {
			cols := []string{"member_id", "week_date"}
			placeholders := []string{"?", "?"}
			vals := []interface{}{memberID, req.WeekDate}
			for day, val := range fields {
				cols = append(cols, day)
				placeholders = append(placeholders, "?")
				vals = append(vals, val)
			}
			query := "INSERT INTO vs_points (" + strings.Join(cols, ", ") + ", updated_at) VALUES (" + strings.Join(placeholders, ", ") + ", CURRENT_TIMESTAMP)"
			_, vsErr = tx.Exec(query, vals...)
		} else if err == nil {
			var updates []string
			var vals []interface{}
			for day, val := range fields {
				updates = append(updates, day+" = ?")
				vals = append(vals, val)
			}
			vals = append(vals, memberID, req.WeekDate)
			query := "UPDATE vs_points SET " + strings.Join(updates, ", ") + ", updated_at = CURRENT_TIMESTAMP WHERE member_id = ? AND week_date = ?"
			_, vsErr = tx.Exec(query, vals...)
		} else {
			vsErr = err
		}

		if vsErr != nil {
			slog.Error("mobileCommit: vs upsert failed", "member_id", memberID, "error", vsErr)
			commitErrors = append(commitErrors, fmt.Sprintf("VS insert failed for member_id %d: database error", memberID))
		} else {
			vsRecordsSaved++
		}
	}

	// Process save_aliases through the one alias helper, which logs each change inside
	// this transaction. A global alias needs manage_members; an OCR mapping replaces
	// another member's global alias only with it too.
	aliasesSaved := 0
	actor := aliasActor{UserID: claims.UserID, Username: claims.Username, Via: "mobile"}
	for _, aliasReq := range req.SaveAliases {
		if aliasReq.Category == "global" && !canMembers {
			commitErrors = append(commitErrors, fmt.Sprintf("cannot save global alias %q: manage_members permission required", aliasReq.FailedAlias))
			continue
		}
		ch, err := saveAliasTx(tx, aliasWrite{MemberID: aliasReq.MemberID, Alias: aliasReq.FailedAlias,
			Category: aliasReq.Category, Actor: actor, MayOverrideGlobal: canMembers})
		if err != nil {
			if isAliasRefusal(err) {
				commitErrors = append(commitErrors, fmt.Sprintf("alias %q: %v", aliasReq.FailedAlias, err))
				continue
			}
			slog.Error("mobileCommit: alias save failed", "alias", aliasReq.FailedAlias, "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if ch != nil {
			aliasesSaved++
		}
	}

	powerRecordsSaved := recordsSaved["power"]
	killRecordsSaved := recordsSaved["kills"]

	if err := tx.Commit(); err != nil {
		slog.Error("mobileCommit: tx commit failed", "error", err)
		http.Error(w, "Failed to save changes", http.StatusInternalServerError)
		return
	}

	// Activity logging
	if vsRecordsSaved > 0 {
		details := fmt.Sprintf("%d members", vsRecordsSaved)
		if aliasesSaved > 0 {
			details += fmt.Sprintf(", %d aliases saved", aliasesSaved)
		}
		logActivity(claims.UserID, claims.Username, "imported", "vs_points", req.WeekDate, false, details)
	}
	// One power_records row for every member-stat category except kills, which keeps its
	// own kill_count row, with a count per category.
	var statParts []string
	for _, c := range []string{"power", "hero_power", "squad_power", "hq_level", "profession_level"} {
		if n := recordsSaved[c]; n > 0 {
			statParts = append(statParts, fmt.Sprintf("%s %d", strings.ReplaceAll(c, "_", " "), n))
		}
	}
	if len(statParts) > 0 {
		logActivity(claims.UserID, claims.Username, "imported", "power_records", req.WeekDate, false,
			strings.Join(statParts, ", ")+" records · via mobile")
	}
	if killRecordsSaved > 0 {
		logActivity(claims.UserID, claims.Username, "imported", "kill_count", req.WeekDate, false, fmt.Sprintf("%d records", killRecordsSaved))
	}

	msg := fmt.Sprintf("Import successful. Saved VS data for %d member(s), power data for %d member(s), kill data for %d member(s), registered %d new alias(es).",
		vsRecordsSaved, powerRecordsSaved, killRecordsSaved, aliasesSaved)

	resp := MobileCommitResponse{
		Message:           msg,
		VSRecordsSaved:    vsRecordsSaved,
		PowerRecordsSaved: powerRecordsSaved,
		KillRecordsSaved:  killRecordsSaved,
		AliasesSaved:      aliasesSaved,
		RecordsSaved:      recordsSaved,
		RecordsUnchanged:  recordsUnchanged,
		SaturdayDerived:   saturdayDerived,
		Errors:            commitErrors,
	}
	if resp.Errors == nil {
		resp.Errors = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
