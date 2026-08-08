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
	byID       map[int]napJobTarget // job item Seq → what to fetch and how to key it
}

// napJobTarget carries the identity Step needs, resolved once in Plan. IsOwn
// matters because our own alliance has no registry row (Rule 2) and so keys its
// history datapoint on is_own + server instead of external_alliance_id.
type napJobTarget struct {
	LastRankID string
	IsOwn      bool
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

	j.byID = map[int]napJobTarget{}
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
		j.byID[seq] = napJobTarget{LastRankID: strings.TrimSpace(*a.LastRankID), IsOwn: a.IsUs}
		items = append(items, jobItem{Seq: seq, Label: label, RefID: a.ExternalID})
	}
	return items, nil
}

func (j *napMembersJob) Step(ctx context.Context, it jobItem) (jobStep, error) {
	target := j.byID[it.Seq]
	if target.LastRankID == "" {
		return jobStep{State: "skip", Detail: "no LastRank id"}, nil
	}

	// Bounded per item so one unresponsive alliance cannot stall the whole sweep.
	fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	a, err := fetchLastRankAlliance(fetchCtx, target.LastRankID)
	if err != nil {
		return jobStep{}, err
	}
	if a.CurMember <= 0 {
		return jobStep{State: "skip", Detail: "no member count"}, nil
	}

	// Power and kills ride along free — they are already in this response, which we
	// were fetching for the member count anyway. Same idea as the member extended
	// sync taking power/hero/HQ off the one player record.
	statsApplied, historyAdded, err := storeNAPAllianceSnapshot(j.capturedAt, napDetailSnapshot{
		LastRankID:  target.LastRankID,
		IsOwn:       target.IsOwn,
		Server:      a.ServerID,
		Tag:         a.Abbr,
		Name:        a.Name,
		Power:       a.Fightpower,
		Kills:       a.ArmyKill,
		MemberCount: a.CurMember,
		Seen:        a.LastSeenAt,
	})
	if err != nil {
		return jobStep{}, err
	}

	max := a.MaxMember
	if max == 0 {
		max = 100
	}
	counters := map[string]int{"members_synced": 1}
	detail := fmt.Sprintf("✓ %d/%d members", a.CurMember, max)
	if statsApplied {
		counters["stats_synced"] = 1
		detail = fmt.Sprintf("✓ %s power · %s kills · %d/%d members",
			formatBigInt(a.Fightpower), formatBigInt(a.ArmyKill), a.CurMember, max)
	}
	if historyAdded {
		counters["datapoints"] = 1
		detail += " · +1 datapoint"
	}
	return jobStep{State: "done", Detail: detail, Counters: counters}, nil
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
		fmt.Sprintf("%d alliances checked; %d member counts; %d power/kills refreshed; %d new datapoints; captured %s",
			processed, synced, counters["stats_synced"], counters["datapoints"], j.capturedAt))
}
