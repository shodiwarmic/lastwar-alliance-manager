package main

// jobs_nap.go — NAP ladder member counts as a background job.
//
// Member counts are not on the ladder endpoint, only on each alliance's detail
// record, so this costs one upstream request per alliance at ~1/second. That is
// why it was a browser loop, and why it belongs on the server instead.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func init() {
	registerJobKind(JobNAPMembers, jobKind{
		Permission: "manage_allies",
		Label:      "The NAP member-count gather",
		New:        func(a jobActor) jobRunner { return &napMembersJob{actor: a} },
	})
}

type napMembersJob struct {
	actor      jobActor
	server     int
	capturedAt string
	byID       map[int]string // job item RefID → lastrank_id
}

// Plan mirrors getNAP's assembly (registry ladder + our own row, truncated to the
// import limit) and keeps alliances with a LastRank id.
//
// Missing counts come first so an interrupted run resumes on what it still lacks
// rather than re-fetching what it already has — the same rule the browser loop used.
func (j *napMembersJob) Plan(ctx context.Context) ([]jobItem, error) {
	cfg := loadNAPConfig()
	if cfg.server == 0 {
		return nil, nil // no server configured — nothing to do, not an error
	}
	j.server = cfg.server

	alliances, err := loadRegistryLadder(cfg)
	if err != nil {
		return nil, err
	}
	if us, ok := loadOwnLadderRow(cfg.server); ok {
		alliances = append(alliances, us)
	}
	sort.SliceStable(alliances, func(i, k int) bool { return alliances[i].rank < alliances[k].rank })
	alliances = truncateKeepingUs(alliances, cfg.importLimit)

	// The capture date lives on the history series, not the registry. Read AFTER
	// the ladder cursor is closed — a query issued while it is open would deadlock
	// the single connection.
	if err := db.QueryRow(`SELECT COALESCE(MAX(recorded_at), '') FROM alliance_stats_history WHERE server = ?`,
		cfg.server).Scan(&j.capturedAt); err != nil {
		return nil, err
	}

	// Missing counts first, then ladder order.
	sort.SliceStable(alliances, func(i, k int) bool {
		return (alliances[i].MemberCount == nil) && (alliances[k].MemberCount != nil)
	})

	j.byID = map[int]string{}
	var items []jobItem
	for _, a := range alliances {
		if a.LastRankID == nil || strings.TrimSpace(*a.LastRankID) == "" {
			continue
		}
		label := "?"
		if a.Tag != nil && *a.Tag != "" {
			label = "[" + *a.Tag + "]"
			if a.Name != nil && *a.Name != "" {
				label += " " + *a.Name
			}
		} else if a.Name != nil {
			label = *a.Name
		}
		if a.IsUs {
			label += " — us"
		}
		seq := len(items)
		j.byID[seq] = strings.TrimSpace(*a.LastRankID)
		items = append(items, jobItem{Seq: seq, Label: label, RefID: a.ExternalID})
	}
	return items, nil
}

func (j *napMembersJob) Step(ctx context.Context, it jobItem) (jobStep, error) {
	lastrankID := j.byID[it.Seq]
	if lastrankID == "" {
		return jobStep{State: "skip", Detail: "no LastRank id"}, nil
	}

	// Bounded per item so one unresponsive alliance cannot stall the whole sweep.
	fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	a, err := fetchLastRankAlliance(fetchCtx, lastrankID)
	if err != nil {
		return jobStep{}, err
	}
	if a.CurMember <= 0 {
		return jobStep{State: "skip", Detail: "no member count"}, nil
	}

	if err := storeNAPMemberCount(lastrankID, j.capturedAt, a.CurMember, a.MaxMember); err != nil {
		return jobStep{}, err
	}
	max := a.MaxMember
	if max == 0 {
		max = 100
	}
	return jobStep{
		State:    "done",
		Detail:   fmt.Sprintf("✓ %d/%d members", a.CurMember, max),
		Counters: map[string]int{"members_synced": 1},
	}, nil
}

// Finish writes one activity row for the whole run, matching how the ladder
// refresh and the member sync defer their logging rather than logging per item.
func (j *napMembersJob) Finish(ctx context.Context, counters map[string]int, processed int) {
	synced := counters["members_synced"]
	if synced == 0 {
		return
	}
	logActivity(j.actor.UserID, j.actor.Username, "imported", "alliance_stats",
		fmt.Sprintf("server %d ladder", j.server), false,
		fmt.Sprintf("%d alliances checked; %d member counts; captured %s", processed, synced, j.capturedAt))
}
