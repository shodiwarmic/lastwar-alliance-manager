package main

// jobs_external.go — the external-alliance gather as a background job.
//
// Re-pulls power / kills / member count for every registry row that carries a
// LastRank link, one upstream request each at ~1/second.
//
// This flow also gains an activity row it never had: as a browser loop it wrote
// data on every iteration and logged nothing, which is the one thing CLAUDE.md
// says every write path must do.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

func init() {
	registerJobKind(JobExtAllianceGather, jobKind{
		Permission: "manage_external_alliances",
		Label:      "The external-alliance gather",
		New:        func(a jobActor) jobRunner { return &extGatherJob{actor: a} },
	})
}

type extGatherJob struct{ actor jobActor }

// Plan takes registry rows with a LastRank link, oldest-updated first so an
// interrupted run resumes on the stalest data rather than restarting at the top.
func (j *extGatherJob) Plan(ctx context.Context) ([]jobItem, error) {
	rows, err := db.Query(`
		SELECT id, COALESCE(tag, ''), COALESCE(name, '')
		FROM external_alliances
		WHERE lastrank_id IS NOT NULL AND TRIM(lastrank_id) != ''
		ORDER BY COALESCE(updated_at, '') ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []jobItem
	for rows.Next() {
		var id int
		var tag, name string
		if err := rows.Scan(&id, &tag, &name); err != nil {
			return nil, err
		}
		label := name
		if tag != "" {
			label = "[" + tag + "]"
			if name != "" {
				label += " " + name
			}
		}
		if label == "" {
			label = "#" + strconv.Itoa(id)
		}
		items = append(items, jobItem{Seq: len(items), Label: label, RefID: id})
	}
	return items, rows.Err()
}

func (j *extGatherJob) Step(ctx context.Context, it jobItem) (jobStep, error) {
	snap, err := refreshOneExternalAlliance(ctx, it.RefID)
	if err != nil {
		// A row whose link was cleared between Plan and Step is a skip, not a
		// failure — nothing is broken and nothing needs an officer's attention.
		if errors.Is(err, errNoLastRankLink) {
			return jobStep{State: "skip", Detail: "no LastRank link"}, nil
		}
		return jobStep{}, err
	}
	members := "?"
	if snap.MemberCount > 0 {
		members = strconv.Itoa(snap.MemberCount)
	}
	return jobStep{
		State: "done",
		Detail: fmt.Sprintf("✓ %s power · %s kills · %s/100",
			formatBigInt(snap.Power), formatBigInt(snap.Kills), members),
		Counters: map[string]int{"refreshed": 1},
	}, nil
}

func (j *extGatherJob) Finish(ctx context.Context, counters map[string]int, processed int) {
	refreshed := counters["refreshed"]
	if refreshed == 0 {
		return
	}
	logActivity(j.actor.UserID, j.actor.Username, "imported", "external_alliance", "LastRank gather", false,
		fmt.Sprintf("refreshed %d of %d alliances from LastRank", refreshed, processed))
}

// formatBigInt renders a large count compactly for the progress line, matching the
// fmtBig helper the browser loop used.
func formatBigInt(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return strconv.FormatInt(n, 10)
	}
}
