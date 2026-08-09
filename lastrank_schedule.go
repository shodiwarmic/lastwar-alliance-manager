package main

// lastrank_schedule.go — opt-in scheduled LastRank retrieval.
//
// ── WHY THE TWO NUMBERS ARE COUPLED ────────────────────────────────────────────
// The tick INTERVAL decides how often we look. The enrich MAX AGE decides, per
// member, whether that tick actually re-pulls. At the 6h/21h default:
//
//	tick   age since last enrich   enrich?   extended-sweep cost
//	04:00  24h                     yes       ~200 reqs, 10–40 min
//	10:00   6h                     no        0 reqs (nobody due)
//	16:00  12h                     no        0 reqs
//	22:00  18h                     no        0 reqs
//
// Exactly one enrich per member per 24h — a hard "refreshed within a day"
// guarantee — while the 1-request alliance pull runs 4×/day, so departures and
// rank changes reach the review queue within 6h instead of 12.
//
// The legal band for the max age is a FUNCTION of the interval:
// 24 - interval < max_age <= 23. Below it an extra tick per day clears the
// threshold; above it a long run's own duration can push a member past the next
// day's slot and revive an alternate-day silent skip.
//
// DO NOT "simplify" this by dropping the lastrank_attempted_at pre-filter in
// jobs_lastrank.go. The three cheap ticks are only free because the sweep can tell
// in SQL that nobody is due; without it every tick costs a full roster of GETs and
// 6h silently becomes far more expensive than 12h.

import (
	"log/slog"
	"sync"
	"time"
)

type lastRankScheduleConfig struct {
	Enabled           bool
	Hour              int
	IntervalHours     int
	EnrichMaxAgeHours int
	NAPEnabled        bool
	ProspectEnabled   bool
}

// Cached because lastRankNeedsEnrich consults the max age once per member: a
// 100-member sweep would otherwise issue 100 settings reads on the single
// connection. Short TTL so a settings change takes effect without a restart, plus
// an explicit invalidation on save.
var (
	lrSchedMu    sync.RWMutex
	lrSchedCache *lastRankScheduleConfig
	lrSchedAt    time.Time
)

const lrSchedTTL = 60 * time.Second

func invalidateLastRankScheduleCache() {
	lrSchedMu.Lock()
	lrSchedCache = nil
	lrSchedMu.Unlock()
}

// loadLastRankScheduleConfig reads the scheduler settings, cached.
//
// On a read failure it returns the last known value (or defaults) rather than
// zeroes: a transient database hiccup must not silently reinterpret "every 6h" as
// "every 0h".
func loadLastRankScheduleConfig() lastRankScheduleConfig {
	lrSchedMu.RLock()
	if lrSchedCache != nil && time.Since(lrSchedAt) < lrSchedTTL {
		c := *lrSchedCache
		lrSchedMu.RUnlock()
		return c
	}
	prev := lrSchedCache
	lrSchedMu.RUnlock()

	c := lastRankScheduleConfig{Hour: 4, IntervalHours: 6, EnrichMaxAgeHours: 21}
	if prev != nil {
		c = *prev
	}
	var enabled, nap, prospect int
	err := db.QueryRow(`SELECT
		COALESCE(lastrank_auto_sync_enabled, 0),
		COALESCE(lastrank_auto_sync_hour, 4),
		COALESCE(lastrank_auto_sync_interval_hours, 6),
		COALESCE(lastrank_enrich_max_age_hours, 21),
		COALESCE(nap_auto_refresh_enabled, 0),
		COALESCE(prospect_auto_refresh_enabled, 0)
		FROM settings WHERE id = 1`).
		Scan(&enabled, &c.Hour, &c.IntervalHours, &c.EnrichMaxAgeHours, &nap, &prospect)
	if err != nil {
		slog.Error("lastrank schedule config read failed; using last known values", "error", err)
		return c
	}
	c.Enabled = enabled != 0
	c.NAPEnabled = nap != 0
	c.ProspectEnabled = prospect != 0

	lrSchedMu.Lock()
	lrSchedCache = &c
	lrSchedAt = time.Now()
	lrSchedMu.Unlock()
	return c
}

// lastRankEnrichMaxAge is the operator-set freshness window for upgrading a cached
// GET to a live enrich.
func lastRankEnrichMaxAge() time.Duration {
	h := loadLastRankScheduleConfig().EnrichMaxAgeHours
	if h <= 0 {
		return lastRankDefaultEnrichMaxAge
	}
	return time.Duration(h) * time.Hour
}

// validScheduleInterval reports whether an interval divides the day. Anything else
// makes slots drift across midnight, so "the 04:00 run" stops meaning anything.
func validScheduleInterval(h int) bool {
	switch h {
	case 1, 2, 3, 4, 6, 8, 12, 24:
		return true
	}
	return false
}

// enrichMaxAgeBand returns the legal (exclusive-low, inclusive-high) bounds for the
// max age given a tick interval. Exported shape so the settings UI can show the
// operator WHY 21 is legal at 6h but not at 3h.
func enrichMaxAgeBand(intervalHours int) (low, high int) {
	return 24 - intervalHours, 23
}

// ── Ticker ─────────────────────────────────────────────────────────────────────

// startLastRankScheduler launches the periodic check. Built on the same shape as
// startLocalArchiveJanitor. No-op at runtime while the settings toggle is off.
//
// Note for deployment: an in-process ticker double-fires if the app is ever scaled
// past one container. The compose setup runs a single app container, so this is
// safe today — README says so, so nobody scales it later and quietly doubles the
// load on a volunteer-run service.
func startLastRankScheduler() {
	go func() {
		// Coarse tick: the slot check is cheap, and a 15-minute granularity is
		// plenty for something that fires every few hours.
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			runScheduledLastRankTick()
		}
	}()
	slog.Info("lastrank scheduler started (checks every 15m; runs only when enabled in Settings)")
}

// runScheduledLastRankTick fires the due flows, one at a time.
//
// EVERY tick runs the same steps. The heavy/no-op difference emerges from the
// freshness pre-filters inside each job's Plan, not from the scheduler knowing
// which kind of tick this is — see the file header.
func runScheduledLastRankTick() {
	cfg := loadLastRankScheduleConfig()
	if !cfg.Enabled {
		return
	}
	if !validScheduleInterval(cfg.IntervalHours) {
		slog.Warn("lastrank scheduler: invalid interval, skipping", "interval_hours", cfg.IntervalHours)
		return
	}

	// A scheduled run holds the single job slot for as long as it takes; with a
	// heavy pass measured in tens of minutes an overlapping tick is a realistic
	// event, not a theoretical one.
	if kind, since, running := currentJobInfo(); running {
		slog.Info("lastrank scheduler: a job is already running, skipping this tick",
			"kind", kind, "running_for", since.Round(time.Minute).String())
		return
	}

	kinds := []struct {
		kind    string
		enabled bool
	}{
		{JobLastRankAlliance, true},
		{JobLastRankExtended, true},
		{JobProspectRefreshTransfer, cfg.ProspectEnabled},
		{JobProspectRefreshProspect, cfg.ProspectEnabled},
		{JobNAPMembers, cfg.NAPEnabled},
	}

	for _, k := range kinds {
		if !k.enabled {
			continue
		}
		if !scheduledSlotDue(k.kind, cfg) {
			continue
		}
		// One at a time: startJob returns errJobBusy if the previous one is still
		// going, and the next tick picks up whatever was skipped.
		if _, err := startJob(k.kind, jobActor{Username: "System (scheduled)", Scheduled: true}); err != nil {
			slog.Info("lastrank scheduler: could not start", "kind", k.kind, "error", err)
			return
		}
		return // one job per tick; the slot is single-occupancy anyway
	}
}

// scheduledSlotDue reports whether the current slot has been crossed since the last
// COMPLETED scheduled run of this kind.
//
// "Last run" is derived from background_jobs rather than a settings column, so it
// is self-healing across restarts with no extra state to keep in sync.
func scheduledSlotDue(kind string, cfg lastRankScheduleConfig) bool {
	slot := currentScheduleSlot(gameNow(), cfg.Hour, cfg.IntervalHours)

	var lastStarted string
	err := db.QueryRow(`SELECT COALESCE(MAX(started_at), '') FROM background_jobs
		WHERE kind = ? AND trigger_source = 'scheduled' AND status != 'failed'`, kind).Scan(&lastStarted)
	if err != nil {
		slog.Error("lastrank scheduler: due check failed", "kind", kind, "error", err)
		return false
	}
	if lastStarted == "" {
		return true
	}
	// Parsed, never string-compared: started_at is a declared TIMESTAMP and reads
	// back as RFC3339Nano while slot is a game-time value we formatted ourselves.
	last, ok := lastRankParseTime(lastStarted)
	if !ok {
		return true
	}
	return last.UTC().Before(slot.UTC())
}

// currentScheduleSlot returns the most recent slot boundary at or before now, where
// slots fall at anchor, anchor+interval, … in GAME time (UTC−2), consistent with
// every other clock in the app.
func currentScheduleSlot(now time.Time, anchorHour, intervalHours int) time.Time {
	if intervalHours <= 0 {
		intervalHours = 6
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var slot time.Time
	for h := anchorHour % intervalHours; h < 24; h += intervalHours {
		candidate := day.Add(time.Duration(h) * time.Hour)
		if candidate.After(now) {
			break
		}
		slot = candidate
	}
	if slot.IsZero() {
		// Before the day's first slot — the live one is yesterday's last.
		slot = day.Add(-time.Duration(intervalHours) * time.Hour).
			Add(time.Duration(anchorHour%intervalHours) * time.Hour)
	}
	return slot
}
