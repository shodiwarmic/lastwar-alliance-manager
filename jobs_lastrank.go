package main

// jobs_lastrank.go — the LastRank extended sync as a background job.
//
// This replaces the browser-driven loop that used to live in static/lastrank.js:
// one POST per member, ~1/second, with the tab held open. A ~100-member roster
// took minutes to babysit, and closing the tab abandoned the run.

import (
	"context"
	"strconv"
	"strings"
)

func init() {
	registerJobKind(JobLastRankExtended, jobKind{
		Permission: "manage_members",
		Label:      "The LastRank extended sync",
		New:        func(a jobActor) jobRunner { return &lastRankExtendedJob{actor: a} },
	})
}

type lastRankExtendedJob struct{ actor jobActor }

// Plan lists members to sync, oldest-synced first so an interrupted run resumes
// where it stopped rather than restarting at the top. syncOneMember always
// advances lastrank_synced_at — even for a skipped member — which is what keeps
// this ordering moving instead of retrying the same row forever.
//
// Archived members are excluded. The old browser loop included any member holding
// a public id, so every sweep spent upstream requests refreshing people who had
// left the alliance.
func (j *lastRankExtendedJob) Plan(ctx context.Context) ([]jobItem, error) {
	q := `SELECT id, name FROM members
	      WHERE lastrank_public_id IS NOT NULL AND rank != 'EX'`
	args := []any{}

	// THE PRE-FILTER THAT MAKES THE CHEAP TICKS FREE. A scheduled run takes only
	// members actually due, so on a 6h/21h schedule three ticks in four plan ZERO
	// items and cost ZERO upstream requests. Without it every tick would spend a
	// full roster of GETs to discover there is nothing to do, and the tighter
	// interval would silently cost more than a looser one — see lastrank_schedule.go.
	//
	// Two conditions, two different jobs:
	//   enriched_at  — is the DATA stale enough to be worth re-pulling?
	//   attempted_at — have we already tried recently? Without this a member whose
	//                  enrich comes back permanently "gated" never advances
	//                  enriched_at, stays due forever, and is re-attempted every
	//                  single tick.
	// Manual runs ignore both: the officer asked for the whole roster.
	if j.actor.Scheduled {
		maxAge := loadLastRankScheduleConfig().EnrichMaxAgeHours
		cutoff := "-" + strconv.Itoa(maxAge) + " hours"
		q += ` AND (lastrank_enriched_at IS NULL OR lastrank_enriched_at < datetime('now', ?))
		       AND (lastrank_attempted_at IS NULL OR lastrank_attempted_at < datetime('now', ?))`
		args = append(args, cutoff, cutoff)
	}
	q += ` ORDER BY COALESCE(lastrank_synced_at, '') ASC, id ASC`

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []jobItem
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		items = append(items, jobItem{Seq: len(items), Label: name, RefID: id})
	}
	return items, rows.Err()
}

func (j *lastRankExtendedJob) Step(ctx context.Context, it jobItem) (jobStep, error) {
	res, err := syncOneMember(ctx, it.RefID)
	if err != nil {
		return jobStep{}, err
	}

	// Mirrors the wording the browser loop produced, so the progress list reads
	// the same as it always did.
	applied := map[string]bool{
		"kills":         res.KillsApplied,
		"power":         res.PowerApplied,
		"hero":          res.HeroApplied,
		"HQ":            res.HQApplied,
		"profession lv": res.ProfessionLevelApplied,
		"profession":    res.ProfessionChanged,
		"photo":         res.PhotoUpdated,
	}
	order := []string{"kills", "power", "hero", "HQ", "profession lv", "profession", "photo"}

	var parts []string
	counters := map[string]int{}
	for _, k := range order {
		if applied[k] {
			parts = append(parts, k)
			counters[k]++
		}
	}
	if len(parts) == 0 {
		switch res.SkipReason {
		case "no_id":
			return jobStep{State: "skip", Detail: "no LastRank id"}, nil
		case "stale":
			return jobStep{State: "skip", Detail: "your data is newer"}, nil
		default:
			return jobStep{State: "skip", Detail: "no change"}, nil
		}
	}
	counters["members"]++
	return jobStep{State: "done", Detail: "✓ " + strings.Join(parts, " + ") + " updated", Counters: counters}, nil
}

// Finish writes the single summary row for the whole run — one activity entry, not
// one per member. Counters are server-derived here; the browser loop used to post
// its own tallies, which were accepted unvalidated.
func (j *lastRankExtendedJob) Finish(ctx context.Context, counters map[string]int, processed int) {
	synced := counters["members"]
	if synced == 0 {
		return
	}
	details := "via LastRank — " + strconv.Itoa(synced) + " members synced; " +
		strconv.Itoa(counters["kills"]) + " kills, " +
		strconv.Itoa(counters["power"]) + " power, " +
		strconv.Itoa(counters["hero"]) + " hero, " +
		strconv.Itoa(counters["HQ"]) + " HQ, " +
		strconv.Itoa(counters["profession lv"]) + " profession-level, " +
		strconv.Itoa(counters["profession"]) + " profession, " +
		strconv.Itoa(counters["photo"]) + " photos updated"
	logActivity(j.actor.UserID, j.actor.Username, "imported", "lastrank_sync", "Extended sync", false, details)
}
