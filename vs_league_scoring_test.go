package main

import "testing"

func TestVSDayMatchPoints(t *testing.T) {
	want := map[int]int{1: 1, 2: 2, 3: 2, 4: 2, 5: 2, 6: 4, 0: 0, 7: 0, -1: 0}
	total := 0
	for day := 1; day <= 6; day++ {
		total += vsDayMatchPoints(day)
	}
	if total != 13 {
		t.Fatalf("pool = %d, want 13", total)
	}
	for day, w := range want {
		if got := vsDayMatchPoints(day); got != w {
			t.Errorf("vsDayMatchPoints(%d) = %d, want %d", day, got, w)
		}
	}
}

func TestAwardMatchPoints(t *testing.T) {
	// Day 6 is worth 4.
	if o, p := awardMatchPoints("win", 6); o != 4 || p != 0 {
		t.Errorf("win day6 = (%d,%d), want (4,0)", o, p)
	}
	if o, p := awardMatchPoints("loss", 6); o != 0 || p != 4 {
		t.Errorf("loss day6 = (%d,%d), want (0,4)", o, p)
	}
	// tie & pending award nothing to either side.
	for _, oc := range []string{"tie", "pending", ""} {
		if o, p := awardMatchPoints(oc, 6); o != 0 || p != 0 {
			t.Errorf("%q day6 = (%d,%d), want (0,0)", oc, o, p)
		}
	}
}

func TestComputeWeekStanding(t *testing.T) {
	tests := []struct {
		name       string
		days       [6]string
		ourPts     int
		oppPts     int
		remaining  int
		outcome    string
		decided    bool
		clinchDay  int
	}{
		{
			name:      "7-0 clinched Thursday (mid-week, days 5-6 unplayed)",
			days:      [6]string{"win", "win", "win", "win", "pending", "pending"},
			ourPts:    7, oppPts: 0, remaining: 6, outcome: "win", decided: true, clinchDay: 4,
		},
		{
			name:      "0-7 eliminated Thursday (mirror)",
			days:      [6]string{"loss", "loss", "loss", "loss", "pending", "pending"},
			ourPts:    0, oppPts: 7, remaining: 6, outcome: "loss", decided: true, clinchDay: 4,
		},
		{
			name:      "7-6 decided on Day 6 (Enemy Buster)",
			days:      [6]string{"win", "loss", "win", "loss", "loss", "win"},
			ourPts:    7, oppPts: 6, remaining: 0, outcome: "win", decided: true, clinchDay: 6,
		},
		{
			name:      "tie day awards 0 to both and shrinks the pool",
			days:      [6]string{"tie", "win", "loss", "pending", "pending", "pending"},
			ourPts:    2, oppPts: 2, remaining: 8, outcome: "pending", decided: false, clinchDay: 0,
		},
		{
			name:      "6-6 weekly tie (tie day shrank pool to 12)",
			days:      [6]string{"tie", "win", "loss", "loss", "loss", "win"},
			ourPts:    6, oppPts: 6, remaining: 0, outcome: "tie", decided: true, clinchDay: 0,
		},
		{
			name:      "pending / live (still winnable, not clinched)",
			days:      [6]string{"win", "win", "pending", "pending", "pending", "pending"},
			ourPts:    3, oppPts: 0, remaining: 10, outcome: "pending", decided: false, clinchDay: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeWeekStanding(tc.days)
			if got.OurPoints != tc.ourPts || got.OpponentPoints != tc.oppPts {
				t.Errorf("points = %d-%d, want %d-%d", got.OurPoints, got.OpponentPoints, tc.ourPts, tc.oppPts)
			}
			if got.Remaining != tc.remaining {
				t.Errorf("remaining = %d, want %d", got.Remaining, tc.remaining)
			}
			if got.Outcome != tc.outcome {
				t.Errorf("outcome = %q, want %q", got.Outcome, tc.outcome)
			}
			if got.Decided != tc.decided {
				t.Errorf("decided = %v, want %v", got.Decided, tc.decided)
			}
			if got.ClinchDay != tc.clinchDay {
				t.Errorf("clinchDay = %d, want %d", got.ClinchDay, tc.clinchDay)
			}
		})
	}
}

// Editing a raw score later must re-derive the day outcome (F-R07). The normalizer lives
// in the handler, but the rule it enforces is: outcome = sign(our-opp) when both scores exist.
func TestDeriveDayOutcomeFromScores(t *testing.T) {
	cases := []struct {
		our, opp int
		want     string
	}{
		{100, 50, "win"},
		{50, 100, "loss"},
		{75, 75, "tie"},
		{0, 0, "tie"},
	}
	for _, c := range cases {
		got := deriveDayOutcome(c.our, c.opp)
		if got != c.want {
			t.Errorf("deriveDayOutcome(%d,%d) = %q, want %q", c.our, c.opp, got, c.want)
		}
	}
}
