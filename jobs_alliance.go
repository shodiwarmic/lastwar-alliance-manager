package main

// jobs_alliance.go — the scheduled Phase-1 alliance pull.
//
// ONE upstream request for the whole roster, which is what makes it cheap enough
// to run every tick. It is the flow that feeds the review queue: departures, rank
// changes, renames and unmatched names all originate here.
//
// TIERING. Stats (power / hero / HQ) apply automatically; decisions go to the
// queue. That split is not a shortcut — power is staleness-gated,
// provenance-stamped, append-only history, so there is nothing for a human to
// decide and queueing it would bury the four rows that matter under a hundred that
// don't. A rank change, a rename or an archive is never applied unattended.

import (
	"context"
	"strconv"
)

func init() {
	registerJobKind(JobLastRankAlliance, jobKind{
		Permission: "manage_members",
		Label:      "The LastRank alliance pull",
		New:        func(a jobActor) jobRunner { return &lastRankAllianceJob{actor: a} },
	})
}

const JobLastRankAlliance = "lastrank_alliance"

type lastRankAllianceJob struct {
	actor jobActor
	// Carried from Step to Finish for the activity summary.
	queued int
}

// Plan is a single item: the pull is one request covering every member, so there
// is nothing to iterate. Returning zero items when no alliance is configured makes
// the run a clean no-op rather than a failure an operator has to notice.
func (j *lastRankAllianceJob) Plan(ctx context.Context) ([]jobItem, error) {
	if lastRankAllianceID() == "" {
		return nil, nil
	}
	return []jobItem{{Seq: 0, Label: "Alliance roster", RefID: 0}}, nil
}

func (j *lastRankAllianceJob) Step(ctx context.Context, it jobItem) (jobStep, error) {
	allianceID := lastRankAllianceID()
	if allianceID == "" {
		return jobStep{State: "skip", Detail: "no alliance configured"}, nil
	}

	// Fetch with no database handle held — the transaction opens after.
	alliance, err := fetchLastRankAlliance(ctx, allianceID)
	if err != nil {
		return jobStep{}, err
	}

	tx, err := db.Begin()
	if err != nil {
		return jobStep{}, err
	}
	defer tx.Rollback()

	// userID 0: no personal aliases are visible to the scheduler, which is correct
	// — a private mapping belongs to the officer who made it, and resolving an
	// unattended sweep through one user's private aliases would make the result
	// depend on whose account happened to be involved.
	resp := lastRankBuildPreview(tx, alliance, 0)

	recordedAt, _ := lastRankCaptureToSQLite(alliance.LastSeenAt)
	powerN, heroN, hqN := 0, 0, 0

	for _, m := range resp.Matched {
		if m.MatchedMember == nil {
			continue
		}
		// Only what the preview already judged applicable: each diff carries the
		// staleness + unchanged gating, so this writes nothing a manual run wouldn't.
		if m.Power != nil && m.Power.Apply {
			if err := lastRankInsertHistory(tx, "power_history", "power", m.MatchedMember.ID, m.Power.New, recordedAt); err == nil {
				powerN++
			}
		}
		if m.HeroPower != nil && m.HeroPower.Apply {
			if err := lastRankInsertHistory(tx, "hero_power_history", "power", m.MatchedMember.ID, m.HeroPower.New, recordedAt); err == nil {
				heroN++
			}
		}
		if m.HQLevel != nil && m.HQLevel.Apply {
			if err := lastRankInsertHistory(tx, "hq_level_history", "hq_level", m.MatchedMember.ID, int64(m.HQLevel.New), recordedAt); err == nil {
				hqN++
			}
		}
		// Capture the durable public_id link — it survives an in-game rename and is
		// what stops the same player landing in "unmatched" every sync.
		//
		// Deliberately NOT stampLastRankIdentity: that also advances
		// lastrank_synced_at, which the extended sweep orders by. Bumping it for the
		// whole roster every 6h would scramble that ordering and starve the sweep.
		if m.LastRankPublicID != 0 {
			tx.Exec(`UPDATE members SET lastrank_public_id = ? WHERE id = ? AND COALESCE(lastrank_public_id, 0) != ?`,
				m.LastRankPublicID, m.MatchedMember.ID, m.LastRankPublicID)
		}
	}

	// Decisions go to the queue — never applied unattended.
	proposals := buildPendingProposals(resp)
	if err := reconcilePendingChanges(tx, proposals, alliance.LastSeenAt); err != nil {
		return jobStep{}, err
	}
	if err := tx.Commit(); err != nil {
		return jobStep{}, err
	}

	open, _, _ := countOpenPending()
	j.queued = open

	detail := "✓ " + strconv.Itoa(powerN) + " power, " + strconv.Itoa(heroN) + " hero, " +
		strconv.Itoa(hqN) + " HQ applied · " + strconv.Itoa(open) + " awaiting review"
	return jobStep{
		State:  "done",
		Detail: detail,
		Counters: map[string]int{
			"power": powerN, "hero": heroN, "hq": hqN,
			"queued": open, "matched": len(resp.Matched),
		},
	}, nil
}

func (j *lastRankAllianceJob) Finish(ctx context.Context, counters map[string]int, processed int) {
	// Worth a row even when no stats moved: the queue count is the actionable part,
	// and "the pull ran and found 3 decisions" is exactly what an officer wants to
	// see in the audit trail.
	details := "via LastRank — " + strconv.Itoa(counters["matched"]) + " members matched; " +
		strconv.Itoa(counters["power"]) + " power, " + strconv.Itoa(counters["hero"]) + " hero, " +
		strconv.Itoa(counters["hq"]) + " HQ applied; " +
		strconv.Itoa(counters["queued"]) + " change(s) awaiting review"
	logActivity(j.actor.UserID, j.actor.Username, "imported", "lastrank_sync", "Alliance pull", false, details)
}
