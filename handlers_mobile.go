package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

var validMobileCategories = map[string]bool{
	"monday": true, "tuesday": true, "wednesday": true,
	"thursday": true, "friday": true, "saturday": true, "power": true,
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

	for _, entry := range req.Entries {
		match := MobilePreviewMatch{
			OriginalName: entry.Name,
			Category:     entry.Category,
			Score:        entry.Score,
		}
		member, matchType, err := resolveMemberAlias(tx, entry.Name, claims.UserID)
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

	// Validate week_date format
	if _, err := time.Parse("2006-01-02", req.WeekDate); err != nil {
		http.Error(w, "Invalid week_date: must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}

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
	powerRecordsSaved := 0

	// Group VS records by member_id so we do one upsert per member.
	// vsFields[memberID] = map of day -> score
	type vsKey struct {
		memberID int
		name     string
	}
	vsFields := map[int]map[string]int{}
	vsNames := map[int]string{}

	for _, rec := range req.Records {
		if !validCategories(rec.Category) {
			commitErrors = append(commitErrors, fmt.Sprintf("invalid category %q for %s", rec.Category, rec.OriginalName))
			continue
		}
		if !validMemberIDs[rec.MemberID] {
			commitErrors = append(commitErrors, fmt.Sprintf("unrecognized member_id %d (%s): skipped", rec.MemberID, rec.OriginalName))
			continue
		}

		if rec.Category == "power" {
			if _, err := tx.Exec("INSERT INTO power_history (member_id, power) VALUES (?, ?)", rec.MemberID, rec.Score); err != nil {
				slog.Error("mobileCommit: power insert failed", "error", err)
				commitErrors = append(commitErrors, fmt.Sprintf("power insert failed for member_id %d: database error", rec.MemberID))
			} else {
				powerRecordsSaved++
			}
		} else {
			if vsFields[rec.MemberID] == nil {
				vsFields[rec.MemberID] = map[string]int{}
				vsNames[rec.MemberID] = rec.OriginalName
			}
			vsFields[rec.MemberID][rec.Category] = int(rec.Score)
		}
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

	// Process save_aliases
	aliasesSaved := 0
	for _, aliasReq := range req.SaveAliases {
		if aliasReq.Category == "global" && !claims.ManageMembers && !claims.IsAdmin {
			commitErrors = append(commitErrors, fmt.Sprintf("cannot save global alias %q: manage_members permission required", aliasReq.FailedAlias))
			continue
		}

		var aliasErr error
		switch aliasReq.Category {
		case "global", "ocr":
			if _, err := tx.Exec("DELETE FROM member_aliases WHERE LOWER(alias) = LOWER(?)", aliasReq.FailedAlias); err != nil {
				commitErrors = append(commitErrors, fmt.Sprintf("failed to clear alias %q: database error", aliasReq.FailedAlias))
				continue
			}
			_, aliasErr = tx.Exec("INSERT INTO member_aliases (member_id, category, alias) VALUES (?, ?, ?)", aliasReq.MemberID, aliasReq.Category, aliasReq.FailedAlias)
		case "personal":
			if _, err := tx.Exec("DELETE FROM member_aliases WHERE LOWER(alias) = LOWER(?) AND user_id = ?", aliasReq.FailedAlias, claims.UserID); err != nil {
				commitErrors = append(commitErrors, fmt.Sprintf("failed to clear personal alias %q: database error", aliasReq.FailedAlias))
				continue
			}
			_, aliasErr = tx.Exec("INSERT INTO member_aliases (member_id, user_id, category, alias) VALUES (?, ?, 'personal', ?)", aliasReq.MemberID, claims.UserID, aliasReq.FailedAlias)
		default:
			commitErrors = append(commitErrors, fmt.Sprintf("invalid alias category %q for %q", aliasReq.Category, aliasReq.FailedAlias))
			continue
		}

		if aliasErr != nil {
			slog.Error("mobileCommit: alias insert failed", "alias", aliasReq.FailedAlias, "error", aliasErr)
			commitErrors = append(commitErrors, fmt.Sprintf("alias insert failed for %q: database error", aliasReq.FailedAlias))
		} else {
			aliasesSaved++
		}
	}

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
	if powerRecordsSaved > 0 {
		logActivity(claims.UserID, claims.Username, "imported", "power_records", req.WeekDate, false, fmt.Sprintf("%d records", powerRecordsSaved))
	}

	msg := fmt.Sprintf("Import successful. Saved VS data for %d member(s), power data for %d member(s), registered %d new alias(es).",
		vsRecordsSaved, powerRecordsSaved, aliasesSaved)

	resp := MobileCommitResponse{
		Message:           msg,
		VSRecordsSaved:    vsRecordsSaved,
		PowerRecordsSaved: powerRecordsSaved,
		AliasesSaved:      aliasesSaved,
		Errors:            commitErrors,
	}
	if resp.Errors == nil {
		resp.Errors = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// validCategories returns true if cat is one of the seven accepted category values.
func validCategories(cat string) bool {
	return validMobileCategories[cat]
}
