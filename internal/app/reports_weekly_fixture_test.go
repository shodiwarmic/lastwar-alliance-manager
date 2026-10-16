package app

import "testing"

// seedWeeklyFixture is the weekly summary's fixed world, in absolute dates: the week of
// Monday 2026-10-12 seen on Thursday 2026-10-15 (three VS days complete and imported).
// weeklyGoldenNow is the instant the golden file was captured from the old
// /api/accountability/report-data handler, which read the wall clock.
const weeklyGoldenNow = "2026-10-15T20:00:00Z"

func seedWeeklyFixture(t *testing.T) {
	t.Helper()
	for _, q := range []string{
		`UPDATE settings SET vs_minimum_points = 5000000, vs_flag_days_threshold = 2,
			strike_needs_improvement_threshold = 1, strike_at_risk_threshold = 3 WHERE id = 1`,
		`INSERT INTO members (id, name, rank, joined_at) VALUES
			(11, 'Alpha', 'R4', '2026-01-01'), (12, 'Bravo', 'R3', '2026-01-01'),
			(13, 'Charlie', 'R3', '2026-10-14'), (14, 'Delta', 'R2', '2026-01-01'),
			(15, 'Echo', 'R1', '2026-01-01'), (16, 'Gone', 'EX', '2026-01-01')`,
		`INSERT INTO vs_points (member_id, week_date, monday, tuesday, wednesday) VALUES
			(11, '2026-10-12', 9000000, 8000000, 7000000),
			(12, '2026-10-12', 4000000, 6000000, 3000000),
			(13, '2026-10-12', 0, 0, 6000000),
			(14, '2026-10-12', 1000000, 2000000, 0),
			(15, '2026-10-12', 5000000, 5000000, 5000000),
			(16, '2026-10-12', 99000000, 99000000, 99000000)`,
		`INSERT INTO accountability_strikes (member_id, strike_type, reason, status) VALUES
			(12, 'manual', 'x', 'active'), (14, 'manual', 'x', 'active'), (14, 'manual', 'y', 'active'),
			(14, 'manual', 'z', 'active'), (15, 'manual', 'x', 'excused')`,
		`INSERT INTO power_history (member_id, power, recorded_at) VALUES
			(11, 100000000, '2026-09-01 10:00:00'), (11, 130000000, '2026-10-14 10:00:00'),
			(12, 200000000, '2026-09-01 10:00:00'), (12, 190000000, '2026-10-14 10:00:00'),
			(13, 150000000, '2026-10-14 10:00:00'),
			(14, 80000000, '2026-09-01 10:00:00'), (14, 85000000, '2026-10-14 10:00:00'),
			(15, 60000000, '2026-09-01 10:00:00'), (15, 61000000, '2026-10-14 10:00:00'),
			(16, 10000000, '2026-09-01 10:00:00'), (16, 900000000, '2026-10-14 10:00:00')`,
	} {
		mustExec(t, q)
	}
}
