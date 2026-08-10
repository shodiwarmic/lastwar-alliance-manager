package main

import (
	"path/filepath"
	"testing"
	"time"
)

func setupScheduleTestDB(t *testing.T) {
	t.Helper()
	prev := db
	t.Setenv("DATABASE_PATH", filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("STORAGE_PATH", t.TempDir())
	t.Setenv("SESSION_KEY", "test-session-key-at-least-32-chars-long")
	if err := initDB(); err != nil {
		t.Fatalf("initDB: %v", err)
	}
	invalidateLastRankScheduleCache()
	t.Cleanup(func() {
		if db != nil {
			db.Close()
		}
		db = prev
		invalidateLastRankScheduleCache()
	})
}

// An interval that doesn't divide the day makes slots drift across midnight, so
// "the 04:00 run" stops meaning anything.
func TestValidScheduleInterval(t *testing.T) {
	for _, h := range []int{1, 2, 3, 4, 6, 8, 12, 24} {
		if !validScheduleInterval(h) {
			t.Errorf("interval %d should be valid", h)
		}
	}
	for _, h := range []int{0, 5, 7, 9, 13, 25, -6} {
		if validScheduleInterval(h) {
			t.Errorf("interval %d should be rejected — it doesn't divide 24", h)
		}
	}
}

// The band is a function of the interval, not a constant. This is the arithmetic
// the settings validation depends on.
func TestEnrichMaxAgeBandDependsOnInterval(t *testing.T) {
	cases := []struct{ interval, low, high int }{
		{6, 18, 23},  // 21 is legal
		{12, 12, 23}, // 21 also legal, band is wider
		{3, 21, 23},  // 21 is NOT legal here — it must exceed the low bound
		{24, 0, 23},
	}
	for _, c := range cases {
		low, high := enrichMaxAgeBand(c.interval)
		if low != c.low || high != c.high {
			t.Errorf("band(%d) = (%d,%d), want (%d,%d)", c.interval, low, high, c.low, c.high)
		}
	}
	// The documented default pairing must be legal.
	low, high := enrichMaxAgeBand(6)
	if !(21 > low && 21 <= high) {
		t.Errorf("the 6h/21h default is outside its own legal band (%d,%d]", low, high)
	}
	// And the pairing the plan warns about must not be.
	low, _ = enrichMaxAgeBand(3)
	if 21 > low {
		t.Error("21h should be rejected at a 3h interval — every other tick would enrich")
	}
}

// Slots fall at anchor, anchor+interval, … and the "current" slot is the most
// recent boundary at or before now.
func TestCurrentScheduleSlot(t *testing.T) {
	day := func(h, m int) time.Time {
		return time.Date(2026, 8, 7, h, m, 0, 0, time.UTC)
	}
	cases := []struct {
		now      time.Time
		wantHour int
	}{
		{day(4, 0), 4},   // exactly on a slot
		{day(5, 59), 4},  // just before the next
		{day(10, 1), 10}, // just after
		{day(23, 59), 22},
	}
	for _, c := range cases {
		got := currentScheduleSlot(c.now, 4, 6)
		if got.Hour() != c.wantHour || got.Day() != 7 {
			t.Errorf("slot at %v = %v, want hour %d on day 7", c.now, got, c.wantHour)
		}
	}
	// Before the day's first slot, the live slot belongs to yesterday.
	got := currentScheduleSlot(day(1, 0), 4, 6)
	if got.Day() != 6 {
		t.Errorf("slot before the first boundary = %v, want it to fall on the previous day", got)
	}
}

// A kind that has never run is due; one that ran after the current slot is not.
func TestScheduledSlotDue(t *testing.T) {
	setupScheduleTestDB(t)
	cfg := lastRankScheduleConfig{Hour: 4, IntervalHours: 6, EnrichMaxAgeHours: 21}

	if !scheduledSlotDue(JobLastRankAlliance, cfg) {
		t.Error("a kind that has never run should be due")
	}

	// A scheduled run that started just now covers the current slot.
	if _, err := db.Exec(`INSERT INTO background_jobs (kind, status, trigger_source, started_at)
		VALUES (?, 'done', 'scheduled', datetime('now'))`, JobLastRankAlliance); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if scheduledSlotDue(JobLastRankAlliance, cfg) {
		t.Error("a kind that just ran should not be due again in the same slot")
	}

	// A MANUAL run must not satisfy the schedule — otherwise one officer clicking
	// Fetch would silently cancel that day's automated pull.
	if _, err := db.Exec(`INSERT INTO background_jobs (kind, status, trigger_source, started_at)
		VALUES (?, 'done', 'manual', datetime('now'))`, JobNAPMembers); err != nil {
		t.Fatalf("seed manual: %v", err)
	}
	if !scheduledSlotDue(JobNAPMembers, cfg) {
		t.Error("a manual run satisfied the schedule — the automated pull would be skipped")
	}
}

// The scheduler must do nothing at all while the toggle is off.
func TestSchedulerNoOpWhenDisabled(t *testing.T) {
	setupScheduleTestDB(t)
	runScheduledLastRankTick()

	var n int
	db.QueryRow(`SELECT COUNT(*) FROM background_jobs`).Scan(&n)
	if n != 0 {
		t.Errorf("scheduler started %d job(s) while disabled", n)
	}
}

// Config reads are cached: a 100-member sweep consults the max age once per member
// and must not issue 100 settings queries on the single connection.
func TestScheduleConfigIsCached(t *testing.T) {
	setupScheduleTestDB(t)
	if _, err := db.Exec(`UPDATE settings SET lastrank_enrich_max_age_hours = 21 WHERE id = 1`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if got := loadLastRankScheduleConfig().EnrichMaxAgeHours; got != 21 {
		t.Fatalf("max age = %d, want 21", got)
	}

	// Change it behind the cache's back — the cached value should persist.
	if _, err := db.Exec(`UPDATE settings SET lastrank_enrich_max_age_hours = 19 WHERE id = 1`); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := loadLastRankScheduleConfig().EnrichMaxAgeHours; got != 21 {
		t.Errorf("max age = %d after an uncached change, want the cached 21", got)
	}

	// Explicit invalidation is what a settings save does, so the change lands now.
	invalidateLastRankScheduleCache()
	if got := loadLastRankScheduleConfig().EnrichMaxAgeHours; got != 19 {
		t.Errorf("max age = %d after invalidation, want 19", got)
	}
}

// THE COST-MODEL GUARD. On a 6h/21h schedule three ticks in four must plan ZERO
// items — that is the entire reason a tighter interval is cheaper than a looser
// one. If the pre-filter regresses, every tick costs a full roster of GETs.
func TestScheduledSweepPlansNothingWhenNobodyIsDue(t *testing.T) {
	setupScheduleTestDB(t)
	if _, err := db.Exec(`UPDATE settings SET lastrank_enrich_max_age_hours = 21 WHERE id = 1`); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	invalidateLastRankScheduleCache()

	// A member enriched and attempted two hours ago — comfortably inside the window.
	if _, err := db.Exec(`INSERT INTO members (name, rank, lastrank_public_id, lastrank_enriched_at, lastrank_attempted_at)
		VALUES ('Fresh', 'R3', 123, datetime('now','-2 hours'), datetime('now','-2 hours'))`); err != nil {
		t.Fatalf("seed fresh: %v", err)
	}

	scheduled := &lastRankExtendedJob{actor: jobActor{Scheduled: true}}
	items, err := scheduled.Plan(nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("scheduled sweep planned %d item(s) with nobody due — the off-ticks are no longer free", len(items))
	}

	// A manual run ignores the backoff: the officer asked for the whole roster.
	manual := &lastRankExtendedJob{actor: jobActor{UserID: 1}}
	items, err = manual.Plan(nil)
	if err != nil {
		t.Fatalf("manual Plan: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("manual sweep planned %d item(s), want 1 — manual must ignore the pre-filter", len(items))
	}
}

// A member past the window IS due, and a permanently-gated one (enriched_at never
// advances) is held off by attempted_at rather than being retried every tick.
func TestScheduledSweepBacksOffGatedMembers(t *testing.T) {
	setupScheduleTestDB(t)
	if _, err := db.Exec(`UPDATE settings SET lastrank_enrich_max_age_hours = 21 WHERE id = 1`); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	invalidateLastRankScheduleCache()

	// Due: both stamps are old.
	db.Exec(`INSERT INTO members (name, rank, lastrank_public_id, lastrank_enriched_at, lastrank_attempted_at)
		VALUES ('Due', 'R3', 1, datetime('now','-30 hours'), datetime('now','-30 hours'))`)
	// Gated: never successfully enriched, but attempted recently.
	db.Exec(`INSERT INTO members (name, rank, lastrank_public_id, lastrank_enriched_at, lastrank_attempted_at)
		VALUES ('Gated', 'R3', 2, NULL, datetime('now','-2 hours'))`)

	items, err := (&lastRankExtendedJob{actor: jobActor{Scheduled: true}}).Plan(nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(items) != 1 || items[0].Label != "Due" {
		t.Fatalf("planned %v, want exactly [Due] — a gated member is being retried every tick", items)
	}
}

// enableSchedule turns the scheduler on with the given per-domain toggles.
func enableSchedule(t *testing.T, nap, prospects bool) {
	t.Helper()
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	if _, err := db.Exec(`UPDATE settings SET lastrank_auto_sync_enabled = 1,
		lastrank_auto_sync_interval_hours = 6, lastrank_enrich_max_age_hours = 21,
		nap_auto_refresh_enabled = ?, prospect_auto_refresh_enabled = ? WHERE id = 1`,
		b(nap), b(prospects)); err != nil {
		t.Fatalf("enable: %v", err)
	}
	invalidateLastRankScheduleCache()
}

// markKindRan records a completed scheduled run, which is how the due check knows
// a kind has already had its turn in this slot.
func markKindRan(t *testing.T, kind string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO background_jobs (kind, status, trigger_source, started_at)
		VALUES (?, 'done', 'scheduled', datetime('now'))`, kind); err != nil {
		t.Fatalf("mark %s: %v", kind, err)
	}
}

// dueKinds reports which registered kinds the scheduler would still consider, in
// the order runScheduledLastRankTick evaluates them.
func dueKinds(t *testing.T) []string {
	t.Helper()
	cfg := loadLastRankScheduleConfig()
	order := []struct {
		kind    string
		enabled bool
	}{
		{JobLastRankAlliance, true},
		{JobLastRankExtended, true},
		{JobProspectRefreshTransfer, cfg.ProspectEnabled},
		{JobProspectRefreshProspect, cfg.ProspectEnabled},
		{JobNAPMembers, cfg.NAPEnabled},
	}
	var out []string
	for _, k := range order {
		if k.enabled && scheduledSlotDue(k.kind, cfg) {
			out = append(out, k.kind)
		}
	}
	return out
}

// The per-domain toggles gate their kinds. Prospects and NAP live behind different
// permissions from the roster sync, so an operator must be able to schedule one
// without the others.
func TestSchedulerTogglesGateTheirKinds(t *testing.T) {
	setupScheduleTestDB(t)

	enableSchedule(t, false, false)
	got := dueKinds(t)
	if len(got) != 2 || got[0] != JobLastRankAlliance || got[1] != JobLastRankExtended {
		t.Errorf("with both toggles off, due = %v; want only the two roster kinds", got)
	}

	enableSchedule(t, true, true)
	if got := dueKinds(t); len(got) != 5 {
		t.Errorf("with both toggles on, due = %v; want all 5 kinds", got)
	}

	enableSchedule(t, true, false)
	got = dueKinds(t)
	for _, k := range got {
		if k == JobProspectRefreshTransfer || k == JobProspectRefreshProspect {
			t.Errorf("prospect kind %q is due with its toggle off", k)
		}
	}
	if len(got) != 3 {
		t.Errorf("NAP-only = %v, want 3 kinds", got)
	}
}

// The tick starts ONE job (the slot is single-occupancy), so successive ticks must
// advance through the remaining kinds rather than retrying the first one forever.
func TestSchedulerAdvancesThroughKindsAcrossTicks(t *testing.T) {
	setupScheduleTestDB(t)
	enableSchedule(t, true, true)

	var order []string
	for i := 0; i < 6; i++ {
		due := dueKinds(t)
		if len(due) == 0 {
			break
		}
		order = append(order, due[0]) // what this tick would start
		markKindRan(t, due[0])        // and what the next tick therefore skips
	}

	want := []string{
		JobLastRankAlliance, JobLastRankExtended,
		JobProspectRefreshTransfer, JobProspectRefreshProspect, JobNAPMembers,
	}
	if len(order) != len(want) {
		t.Fatalf("cycled through %v, want all %d kinds — the scheduler is stuck on one", order, len(want))
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("tick %d started %q, want %q", i+1, order[i], want[i])
		}
	}
	if due := dueKinds(t); len(due) != 0 {
		t.Errorf("after every kind ran, %v is still due — the slot check is not holding", due)
	}
}
