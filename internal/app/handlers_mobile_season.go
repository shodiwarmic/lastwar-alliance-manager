// handlers_mobile_season.go - the mobile Season Hub contributions store. The scanner
// already reads the Alliance Contribution screens, so it sends rows, not frames; the
// target, the name resolution and the upsert are the web import's own functions.

package app

import (
	"fmt"
	"net/http"
)

// GET /api/mobile/season-hub (view_season_hub): the active season or null, its
// trackables, and the categories it accepts — each trackable key with the import's
// _daily, _weekly or _season suffix.
func getMobileSeasonHub(w http.ResponseWriter, r *http.Request) {
	s, err := loadActiveSeason()
	if err != nil {
		dbError(w, "getMobileSeasonHub", err)
		return
	}
	if s == nil {
		writeJSON(w, map[string]any{"season": nil, "trackables": []any{}, "categories": []string{}})
		return
	}
	trackables := []map[string]string{}
	categories := []string{}
	for _, tr := range s.Trackables {
		trackables = append(trackables, map[string]string{"key": tr.Key, "label": tr.Label})
		for _, suffix := range []string{"_daily", "_weekly", "_season"} {
			categories = append(categories, tr.Key+suffix)
		}
	}
	writeJSON(w, map[string]any{
		"season": map[string]any{"id": s.ID, "name": s.Name, "season_number": s.SeasonNumber,
			"start_date": s.StartDate, "week_count": s.WeekCount},
		"trackables": trackables,
		"categories": categories,
	})
}

type mobileContributionTarget struct {
	SeasonID   int    `json:"season_id"`
	WeekNumber int    `json:"week_number"`
	Category   string `json:"category"`
}

// POST /api/mobile/season-hub/contributions/preview (manage_season_hub)
// Validates exactly as the web import does, and resolves the rows as it does. Read-only.
func mobileContributionsPreview(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var req struct {
		mobileContributionTarget
		Entries []struct {
			Name  string `json:"name"`
			Score int64  `json:"score"`
		} `json:"entries"`
	}
	if !decodeMobileJSON(w, r, &req) {
		return
	}
	target, status, msg := contributionTarget(req.SeasonID, req.WeekNumber, req.Category)
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	records := make([]OCRPlayer, 0, len(req.Entries))
	for _, e := range req.Entries {
		records = append(records, OCRPlayer{PlayerName: e.Name, Score: e.Score})
	}
	tx, err := db.Begin()
	if err != nil {
		dbError(w, "mobileContributionsPreview begin", err)
		return
	}
	defer tx.Rollback()
	matched, unresolved := resolveContributionRows(tx, records, user.ID)
	roster, err := loadMobileRoster(tx, user.ID, false)
	if err != nil {
		dbError(w, "mobileContributionsPreview roster", err)
		return
	}
	writeJSON(w, map[string]any{
		"matched":     matched,
		"unresolved":  unresolved,
		"week_number": target.WeekNumber,
		"category":    target.Category,
		"all_members": roster,
	})
}

// POST /api/mobile/season-hub/contributions/commit (manage_season_hub)
func mobileContributionsCommit(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var req struct {
		mobileContributionTarget
		Records []struct {
			MemberID     int    `json:"member_id"`
			OriginalName string `json:"original_name"`
			Score        int64  `json:"score"`
		} `json:"records"`
		SaveAliases []NewAliasMapping `json:"save_aliases"`
	}
	if !decodeMobileJSON(w, r, &req) {
		return
	}
	target, status, msg := contributionTarget(req.SeasonID, req.WeekNumber, req.Category)
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	// Resolved before the transaction (decision 3 of Project 15): a global alias needs
	// manage_members, and an OCR alias over another member's global one too.
	canMembers := userHasPermission(user, "manage_members")

	tx, err := db.Begin()
	if err != nil {
		dbError(w, "mobileContributionsCommit begin", err)
		return
	}
	defer tx.Rollback()

	active := map[int]bool{}
	rows, err := tx.Query(`SELECT id FROM members WHERE rank != 'EX'`)
	if err != nil {
		dbError(w, "mobileContributionsCommit members", err)
		return
	}
	for rows.Next() {
		var id int
		rows.Scan(&id)
		active[id] = true
	}
	rows.Close()

	errs := []string{}
	committed := 0
	for _, rec := range req.Records {
		if !active[rec.MemberID] {
			errs = append(errs, fmt.Sprintf("%s: member_id %d is not an active member", rec.OriginalName, rec.MemberID))
			continue
		}
		if rec.Score < 0 {
			errs = append(errs, fmt.Sprintf("%s: score %d is negative", rec.OriginalName, rec.Score))
			continue
		}
		if err := saveContributionTx(tx, target, rec.MemberID, rec.Score, user.ID); err != nil {
			dbError(w, "mobileContributionsCommit upsert", err)
			return
		}
		committed++
	}

	aliasesSaved := 0
	actor := aliasActor{UserID: user.ID, Username: user.Username, Via: "mobile"}
	for _, a := range req.SaveAliases {
		if a.Category == "global" && !canMembers {
			errs = append(errs, fmt.Sprintf("cannot save global alias %q: manage_members permission required", a.FailedAlias))
			continue
		}
		ch, err := saveAliasTx(tx, aliasWrite{MemberID: a.MemberID, Alias: a.FailedAlias, Category: a.Category,
			Actor: actor, MayOverrideGlobal: canMembers})
		if err != nil {
			if isAliasRefusal(err) {
				errs = append(errs, fmt.Sprintf("alias %q: %v", a.FailedAlias, err))
				continue
			}
			dbError(w, "mobileContributionsCommit alias", err)
			return
		}
		if ch != nil {
			aliasesSaved++
		}
	}

	if err := tx.Commit(); err != nil {
		dbError(w, "mobileContributionsCommit commit", err)
		return
	}
	if committed > 0 {
		logActivity(user.ID, user.Username, "imported", "season_contributions", target.activityName(), false,
			fmt.Sprintf("%d committed · via mobile", committed))
	}
	writeJSON(w, map[string]any{"committed": committed, "aliases_saved": aliasesSaved, "errors": errs})
}
