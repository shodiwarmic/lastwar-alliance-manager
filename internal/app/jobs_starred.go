package app

// jobs_starred.go — the server open-date sweep.
//
// Working out which starred-mission group a server is in needs the date it
// opened, and that is one upstream request per server. At 64 servers and the
// shared 1 req/sec limiter that is about a minute, which is exactly the shape the
// job framework exists for rather than a browser loop.
//
// The sweep is a ONE-OFF, not a refresh. A server's opening date never changes,
// so `Plan` only ever lists servers with no stored row. Three things follow for
// free: a manual correction is never overwritten, a cancelled run resumes where it
// stopped simply by being re-run, and a second run over a complete sector costs
// nothing at all.

import (
	"context"
	"fmt"
	"time"

	"lastwar-alliance/internal/lastrank"
)

// JobStarredOpenDates is the kind string. Like every other, it is the API
// contract with the frontend — changing it orphans in-flight rows.
const JobStarredOpenDates = "starred_open_dates"

func init() {
	registerJobKind(JobStarredOpenDates, jobKind{
		Permission: "manage_settings",
		Label:      "Server open-date sweep",
		New:        func(a jobActor) jobRunner { return &starredOpenDatesJob{actor: a} },
	})
}

type starredOpenDatesJob struct {
	actor jobActor
	// byID maps a job item's Seq to the server it fetches, so Step needs no
	// second read to work out what it is doing.
	byID map[int]int
}

// Plan lists sector servers with NO stored open date.
//
// Deliberately not "servers whose row is old": there is no such thing here. The
// opening date of a game server is a fact about the past, so a row once written is
// never stale, and re-fetching it would spend the volunteer service's budget to
// learn what we already know — as well as trampling an officer's manual correction.
func (j *starredOpenDatesJob) Plan(ctx context.Context) ([]jobItem, error) {
	start, end, ok := loadSector()
	if !ok {
		return nil, nil // sector not configured — nothing to do, not an error
	}

	// Read the whole set before planning: the runner writes immediately after
	// Plan returns, and a cursor left open would deadlock the single connection.
	rows, err := db.Query(`SELECT server_id FROM server_open_dates WHERE server_id BETWEEN ? AND ?`, start, end)
	if err != nil {
		return nil, err
	}
	have := map[int]bool{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		have[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	j.byID = map[int]int{}
	var items []jobItem
	for id := start; id <= end; id++ {
		if have[id] {
			continue
		}
		seq := len(items)
		j.byID[seq] = id
		items = append(items, jobItem{Seq: seq, Label: fmt.Sprintf("Server %d", id), RefID: id})
	}
	return items, nil
}

// Step is read → fetch → write, per THE RULE: nothing holds a database handle
// across the upstream call.
func (j *starredOpenDatesJob) Step(ctx context.Context, it jobItem) (jobStep, error) {
	serverID := j.byID[it.Seq]
	if serverID == 0 {
		return jobStep{State: "skip", Detail: "no server id"}, nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	health, err := lastrank.FetchServerHealth(fetchCtx, serverID)
	if err != nil {
		return jobStep{}, err
	}

	// A server LastRank holds no opening record for is a SKIP, not an error. The
	// sector is a range of numbers we assert, not a list the service confirms, so
	// a gap in it is ordinary — and the officer can fill one in by hand.
	if health.OpenTime == nil || *health.OpenTime == "" {
		return jobStep{State: "skip", Detail: "no opening date upstream"}, nil
	}
	openDate, err := gameDateOf(*health.OpenTime)
	if err != nil {
		return jobStep{State: "skip", Detail: "unreadable opening date"}, nil
	}

	if _, err := db.Exec(`
		INSERT INTO server_open_dates (server_id, open_date, source, fetched_at)
		VALUES (?, ?, 'lastrank', datetime('now'))
		ON CONFLICT(server_id) DO UPDATE SET
			open_date = excluded.open_date, source = 'lastrank', fetched_at = excluded.fetched_at
		WHERE server_open_dates.source <> 'manual'`, serverID, openDate); err != nil {
		return jobStep{}, err
	}

	group, _ := starredGroup(openDate)
	return jobStep{
		State:    "done",
		Detail:   fmt.Sprintf("✓ opened %s — group %d", openDate, group),
		Counters: map[string]int{"fetched": 1},
	}, nil
}

func (j *starredOpenDatesJob) Finish(ctx context.Context, counters map[string]int, processed int) {
	fetched := counters["fetched"]
	if fetched == 0 {
		return
	}
	start, end, _ := loadSector()
	logActivity(j.actor.UserID, j.actor.Username, "imported", "starred_servers",
		fmt.Sprintf("servers %d–%d", start, end), false,
		fmt.Sprintf("%d checked; %d opening dates stored", processed, fetched))
}
