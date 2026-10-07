package demo

// The distributions the generator samples from. They describe the SHAPE of a real
// alliance and nothing else: each figure is an aggregate over a whole roster, taken by
// the read-only query in its comment against the dev database (a copy of production) and
// rounded to two significant figures; the generator adds jitter on top. No row, name or
// single player's value is in this file.
//
// Two deliberate departures from "what the query printed":
//
//   - The TOP decile is never measured. ntile's tenth bucket ends at the alliance's single
//     strongest player, which is one real, publicly searchable figure. Every tail is a
//     fixed multiple of the ninth decile instead (tailFactor).
//   - Per-rank figures are ratios of the median, not deciles. R5 is one person and R4 is
//     ten, so a per-rank decile would be close to a list of individuals.
//
// Refreshing: run each query with `sqlite3 -readonly "file:data/alliance.db?mode=ro"`,
// round to two significant figures, and review the diff in the PR — that review is the
// one step on data leaving a private source. Last taken 2026-10-06.

// latestPerActiveMember is the shape every roster-stat query below shares: the latest
// history row per non-EX member, split into ten equal buckets, each bucket's upper bound.
//
//	SELECT d, MAX(v) FROM (
//	  SELECT v, ntile(10) OVER (ORDER BY v) d FROM (
//	    SELECT (SELECT <col> FROM <table> p WHERE p.member_id = m.id
//	            ORDER BY recorded_at DESC LIMIT 1) v
//	    FROM members m WHERE m.rank != 'EX')
//	  WHERE v IS NOT NULL)
//	GROUP BY d ORDER BY d;
//
// Buckets 1–9 are kept; 10 is the tail (see above).

// powerDeciles: <col> = power, <table> = power_history.
var powerDeciles = []float64{160e6, 180e6, 190e6, 200e6, 220e6, 230e6, 240e6, 250e6, 270e6}

// heroDeciles: <col> = power, <table> = hero_power_history.
var heroDeciles = []float64{85e6, 110e6, 120e6, 130e6, 130e6, 140e6, 140e6, 150e6, 160e6}

// killDeciles: <col> = kills, <table> = kill_history.
var killDeciles = []float64{520e3, 900e3, 1.3e6, 1.9e6, 3.2e6, 4.5e6, 6.5e6, 8.6e6, 12e6}

// squadDeciles: <col> = power, <table> = squad_power_history.
var squadDeciles = []float64{32e6, 35e6, 38e6, 40e6, 42e6, 44e6, 46e6, 47e6, 51e6}

// hqDeciles: <col> = hq_level, <table> = hq_level_history. Levels are coarse already.
var hqDeciles = []float64{31, 32, 32, 33, 33, 33, 34, 34, 34}

// hqMax is the game's HQ cap (settings.max_hq_level default), the ceiling of the tail.
const hqMax = 35

// professionDeciles: <col> = profession_level, <table> = profession_level_history.
var professionDeciles = []float64{81, 87, 90, 92, 94, 96, 98, 100, 100}

// professionMax is the profession level cap.
const professionMax = 100

// tailFactor: the generated maximum is the ninth decile times this (invented — see the
// file comment). The floor is the first decile divided by it.
const tailFactor = 1.4

// rankPowerRatio is each rank's median power over the whole roster's median.
//
//	WITH l AS (SELECT m.rank r, (SELECT power FROM power_history p WHERE p.member_id = m.id
//	                            ORDER BY recorded_at DESC LIMIT 1) v
//	           FROM members m WHERE m.rank != 'EX'),
//	o AS (SELECT r, v, row_number() OVER (PARTITION BY r ORDER BY v) rn,
//	             count(*) OVER (PARTITION BY r) n FROM l WHERE v IS NOT NULL),
//	a AS (SELECT v, row_number() OVER (ORDER BY v) rn, count(*) OVER () n FROM l WHERE v IS NOT NULL)
//	SELECT o.r, round(1.0 * o.v / (SELECT v FROM a WHERE rn = (n + 1) / 2), 1)
//	FROM o WHERE rn = (n + 1) / 2;
//
// The dev roster has no R1 or R2 (the demo needs them to show the permission system), so
// those two are invented: newer, smaller accounts.
var rankPowerRatio = map[string]float64{"R5": 1.2, "R4": 1.2, "R3": 1.0, "R2": 0.8, "R1": 0.6, "EX": 0.9}

// vsDay is one VS weekday: the share of members who score nothing, and the median and
// 90th percentile of those who score.
type vsDay struct {
	ZeroRate float64
	Median   float64
	P90      float64
}

// vsDays, Monday to Saturday, over the last seven complete weeks:
//
//	WITH w AS (SELECT * FROM vs_points WHERE week_date >= date('now','-63 days')
//	                                     AND week_date <  date('now','-6 days')),
//	d AS (SELECT 1 k, monday v FROM w UNION ALL SELECT 2, tuesday FROM w
//	      UNION ALL SELECT 3, wednesday FROM w UNION ALL SELECT 4, thursday FROM w
//	      UNION ALL SELECT 5, friday FROM w UNION ALL SELECT 6, saturday FROM w),
//	z AS (SELECT k, round(avg(v = 0), 2) zr FROM d GROUP BY k),
//	nz AS (SELECT k, v, row_number() OVER (PARTITION BY k ORDER BY v) rn,
//	              count(*) OVER (PARTITION BY k) n FROM d WHERE v > 0)
//	SELECT nz.k, z.zr, max(CASE WHEN rn = (n + 1) / 2 THEN v END),
//	       max(CASE WHEN rn = (n * 9) / 10 THEN v END)
//	FROM nz JOIN z USING (k) GROUP BY nz.k ORDER BY nz.k;
//
// Saturday measured a 0.58 zero rate. That is Saturdays never imported, not members
// sitting the day out, so it is set to the other days' rate.
var vsDays = [6]vsDay{
	{0.07, 13e6, 40e6},
	{0.07, 11e6, 22e6},
	{0.07, 13e6, 31e6},
	{0.08, 21e6, 50e6},
	{0.06, 14e6, 31e6},
	{0.07, 8.4e6, 20e6},
}

// weeklyPowerGrowth is the median weekly power growth, used to walk each member's
// history backwards from their current figure:
//
//	WITH s AS (SELECT member_id, min(recorded_at) t0, max(recorded_at) t1 FROM power_history
//	           WHERE recorded_at >= datetime('now','-90 days') GROUP BY member_id
//	           HAVING julianday(t1) - julianday(t0) >= 28),
//	g AS (SELECT (SELECT power FROM power_history WHERE member_id = s.member_id AND recorded_at = s.t1) * 1.0
//	           / (SELECT power FROM power_history WHERE member_id = s.member_id AND recorded_at = s.t0) gr,
//	             julianday(t1) - julianday(t0) days FROM s),
//	r AS (SELECT power(gr, 7.0 / days) - 1 w FROM g WHERE gr > 0),
//	o AS (SELECT w, row_number() OVER (ORDER BY w) rn, count(*) OVER () n FROM r)
//	SELECT round(w, 3) FROM o WHERE rn = (n + 1) / 2;
const weeklyPowerGrowth = 0.016

// trainsPerWeek, by type, over the last eight weeks:
//
//	SELECT train_type, round(count(*) / 8.0, 1) FROM train_logs
//	WHERE date >= date('now','-56 days') GROUP BY train_type;
var trainsPerWeek = map[string]float64{"FREE": 6.4, "PURCHASED": 3.1}

// strikesPerMemberPer30Days, all types, over the last ninety days:
//
//	SELECT round(count(*) * 30.0 / 90 / (SELECT count(*) FROM members WHERE rank != 'EX'), 3)
//	FROM accountability_strikes WHERE created_at >= datetime('now','-90 days');
//
// Measured 0.02, nearly all of one custom type. The demo uses 0.08 across the system types
// so the Accountability page has a few rows of each to show.
const strikesPerMemberPer30Days = 0.08

// squadTypeShare and professionShare are the roster's mix:
//
//	SELECT squad_type, count(*) FROM members WHERE rank != 'EX' GROUP BY squad_type;
//	SELECT profession, count(*) FROM members WHERE rank != 'EX' GROUP BY profession;
var squadTypeShare = []weighted{{"Tank", 0.54}, {"Aircraft", 0.38}, {"Missile", 0.08}}
var professionShare = []weighted{{"Engineer", 0.86}, {"War Leader", 0.14}}

type weighted struct {
	Value  string
	Weight float64
}
