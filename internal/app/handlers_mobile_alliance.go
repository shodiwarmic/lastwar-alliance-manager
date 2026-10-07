// handlers_mobile_alliance.go - the mobile alliance-stats store: power, kills, member
// count and rank of alliances on a server's ranking screen, as history datapoints
// (source 'mobile') plus registry identity. Mobile datapoints are history only — the NAP
// view stays built from LastRank captures (its three readers filter on source) until a
// provider-agnostic view exists (private-docs 154).

package app

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// allianceStatsWritable reports whether this install can accept a mobile observation:
// the phone recognises our own alliance only by its tag, so with no alliance tag set it
// would register us as an external alliance (Rule 2).
func allianceStatsWritable() bool {
	_, tag := ourAllianceIdentity()
	return tag != ""
}

type mobileAllianceObservation struct {
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	Power       *int64 `json:"power"`
	Kills       *int64 `json:"kills"`
	MemberCount *int   `json:"member_count"`
	PowerRank   *int   `json:"power_rank"`
}

func sameNullable(prev sql.NullInt64, cur *int64) bool {
	if cur == nil {
		return !prev.Valid
	}
	return prev.Valid && prev.Int64 == *cur
}

// POST /api/mobile/alliance-stats (manage_allies)
func postMobileAllianceStats(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var req struct {
		Server     int                         `json:"server"`
		CapturedAt string                      `json:"captured_at"`
		Alliances  []mobileAllianceObservation `json:"alliances"`
	}
	if !decodeMobileJSON(w, r, &req) {
		return
	}
	if req.Server <= 0 {
		badRequest(w, "server is required")
		return
	}
	at, err := parseCapturedAt(req.CapturedAt, time.Now())
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if at == "" {
		at = time.Now().UTC().Format(sqliteTimeLayout)
	}
	// Our identity goes through db, so it is read before the transaction.
	_, ourTag := ourAllianceIdentity()
	if ourTag == "" {
		http.Error(w, "Set the alliance tag in Settings first", http.StatusConflict)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		dbError(w, "postMobileAllianceStats begin", err)
		return
	}
	defer tx.Rollback()

	errs := []string{}
	recorded, unchanged := 0, 0
	for _, a := range req.Alliances {
		a.Tag, a.Name = strings.TrimSpace(a.Tag), strings.TrimSpace(a.Name)
		if a.Tag == "" {
			errs = append(errs, "an alliance has no tag")
			continue
		}
		if (a.Power != nil && *a.Power < 0) || (a.Kills != nil && *a.Kills < 0) ||
			(a.MemberCount != nil && *a.MemberCount < 0) || (a.PowerRank != nil && *a.PowerRank < 1) {
			errs = append(errs, fmt.Sprintf("%s: figures can't be negative and a rank starts at 1", a.Tag))
			continue
		}
		if a.Power == nil && a.Kills == nil && a.MemberCount == nil {
			errs = append(errs, fmt.Sprintf("%s: nothing to record", a.Tag))
			continue
		}
		isOwn := strings.EqualFold(a.Tag, ourTag)
		var extID any
		subject, subjectArg := `is_own = 1 AND server = ?`, any(req.Server)
		if !isOwn {
			eaID, err := upsertExternalAllianceIdentityTx(tx, a.Tag, a.Name, req.Server)
			if err != nil {
				dbError(w, "postMobileAllianceStats registry", err)
				return
			}
			// Asserted, never left to the insert: a datapoint without a valid subject would
			// fail the CHECK, and a swallowed failure would lose it silently.
			if !eaID.Valid {
				errs = append(errs, fmt.Sprintf("%s: no registry row could be made for it", a.Tag))
				continue
			}
			extID = eaID.Int64
			subject, subjectArg = `external_alliance_id = ?`, eaID.Int64
		}

		// Change-only, as the NAP detail path: skip a point whose power, kills and member
		// count all equal the subject's preceding row.
		var pPower, pKills, pMembers sql.NullInt64
		err := tx.QueryRow(`SELECT power, kills, member_count FROM alliance_stats_history
			WHERE `+subject+` AND datetime(recorded_at) < datetime(?)
			ORDER BY datetime(recorded_at) DESC LIMIT 1`, subjectArg, at).Scan(&pPower, &pKills, &pMembers)
		if err != nil && err != sql.ErrNoRows {
			dbError(w, "postMobileAllianceStats previous", err)
			return
		}
		var members *int64
		if a.MemberCount != nil {
			m := int64(*a.MemberCount)
			members = &m
		}
		if err == nil && sameNullable(pPower, a.Power) && sameNullable(pKills, a.Kills) && sameNullable(pMembers, members) {
			unchanged++
			continue
		}
		own := 0
		if isOwn {
			own = 1
		}
		// A collision with the history's unique keys — two phones, a re-upload, two uploads
		// in one second — is the same observation again: counted unchanged, never a 500.
		res, err := tx.Exec(`INSERT INTO alliance_stats_history
			(external_alliance_id, is_own, server, tag, name, power, kills, power_rank, kills_rank, member_count, recorded_at, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, 'mobile')
			ON CONFLICT DO NOTHING`,
			extID, own, req.Server, a.Tag, nullStr(a.Name), a.Power, a.Kills, a.PowerRank, a.MemberCount, at)
		if err != nil {
			dbError(w, "postMobileAllianceStats insert", err)
			return
		}
		if n, _ := res.RowsAffected(); n > 0 {
			recorded++
		} else {
			unchanged++
		}
	}
	if err := tx.Commit(); err != nil {
		dbError(w, "postMobileAllianceStats commit", err)
		return
	}
	if recorded > 0 {
		logActivity(user.ID, user.Username, "imported", "alliance_stats", fmt.Sprintf("Server %d", req.Server), false,
			fmt.Sprintf("%d recorded, %d unchanged · via mobile", recorded, unchanged))
	}
	writeJSON(w, map[string]any{"recorded": recorded, "unchanged": unchanged, "errors": errs})
}
