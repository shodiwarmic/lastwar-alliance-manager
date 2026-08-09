package main

// jobs_prospects.go — bulk prospect refresh as a background job.
//
// This flow had the weakest UI of the four: a single rolling "Refreshing i of N…"
// line and no per-item list at all. Moving it here gives it the same progress
// treatment as the others for free.
//
// Registered as two kinds rather than one parameterised kind, because the
// Recruiting page has a bulk button per tab and each should refresh its own tab.
// The single-slot guard still prevents both running at once.

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
)

const (
	JobProspectRefreshTransfer = "prospect_refresh_transfer"
	JobProspectRefreshProspect = "prospect_refresh_prospect"
)

func init() {
	for kind, pType := range map[string]string{
		JobProspectRefreshTransfer: "transfer",
		JobProspectRefreshProspect: "prospect",
	} {
		t := pType
		registerJobKind(kind, jobKind{
			Permission: "manage_recruiting",
			Label:      "The " + t + " LastRank refresh",
			New:        func(a jobActor) jobRunner { return &prospectRefreshJob{actor: a, prospectType: t} },
		})
	}
}

type prospectRefreshJob struct {
	actor        jobActor
	prospectType string
	pubIDs       map[int]int // job item Seq → lastrank public id
}

// Plan takes prospects of this type that already have a saved LastRank id.
// Declined prospects are excluded — they said no, so spending an upstream request
// on them every sweep buys nothing. Unqualified transfers ARE included: rising
// power is exactly what would requalify them.
func (j *prospectRefreshJob) Plan(ctx context.Context) ([]jobItem, error) {
	q := `SELECT id, name, lastrank_public_id FROM prospects
	      WHERE prospect_type = ? AND lastrank_public_id IS NOT NULL AND status != 'declined'`
	args := []any{j.prospectType}

	// Same pre-filter as the member sweep, for the same reason: on a schedule the
	// off-ticks must plan zero items and cost zero upstream requests. For prospects
	// synced_at IS the attempt stamp — nothing else writes it — so only members need
	// a separate attempted_at column.
	if j.actor.Scheduled {
		cutoff := "-" + strconv.Itoa(loadLastRankScheduleConfig().EnrichMaxAgeHours) + " hours"
		q += ` AND (lastrank_enriched_at IS NULL OR lastrank_enriched_at < datetime('now', ?))
		       AND (lastrank_synced_at IS NULL OR lastrank_synced_at < datetime('now', ?))`
		args = append(args, cutoff, cutoff)
	}
	q += ` ORDER BY COALESCE(lastrank_synced_at, '') ASC, LOWER(name) ASC`

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	j.pubIDs = map[int]int{}
	var items []jobItem
	for rows.Next() {
		var id int
		var name string
		var pub sql.NullInt64
		if err := rows.Scan(&id, &name, &pub); err != nil {
			return nil, err
		}
		if !pub.Valid {
			continue
		}
		seq := len(items)
		j.pubIDs[seq] = int(pub.Int64)
		items = append(items, jobItem{Seq: seq, Label: name, RefID: id})
	}
	return items, rows.Err()
}

func (j *prospectRefreshJob) Step(ctx context.Context, it jobItem) (jobStep, error) {
	pubID := j.pubIDs[it.Seq]
	if pubID == 0 {
		return jobStep{State: "skip", Detail: "no LastRank id"}, nil
	}
	// bulk=true: the cheap GET, upgraded to an enrich only when the record is
	// stale. A sweep must not force a live game pull per prospect.
	out, err := refreshOneProspect(ctx, it.RefID, pubID, true)
	if err != nil {
		return jobStep{}, err
	}

	detail := "✓ " + formatBigInt(derefInt64(out.Power)) + " power"
	if out.HeroPower != nil {
		detail += " · " + formatBigInt(*out.HeroPower) + " hero"
	}
	if out.AllianceAbbr != "" {
		detail += " · [" + out.AllianceAbbr + "]"
	}
	return jobStep{State: "done", Detail: detail, Counters: map[string]int{"refreshed": 1}}, nil
}

func (j *prospectRefreshJob) Finish(ctx context.Context, counters map[string]int, processed int) {
	refreshed := counters["refreshed"]
	if refreshed == 0 {
		return
	}
	noun := j.prospectType
	logActivity(j.actor.UserID, j.actor.Username, "updated", "lastrank_sync", "Prospects", false,
		fmt.Sprintf("via LastRank — refreshed %d %s(s)", refreshed, noun))
}

func derefInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
