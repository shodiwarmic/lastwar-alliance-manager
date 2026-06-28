package main

import (
	"testing"
	"time"
)

// All anchors below use 2024 calendar facts:
//   2023-12-25 Mon, 2023-12-31 Sun, 2024-01-01 Mon, 2024-01-06 Sat,
//   2024-01-07 Sun, 2024-01-08 Mon. The game day = UTC-2, rolling at 02:00 UTC.

func utc(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, time.UTC)
}

func TestGameWeekStart(t *testing.T) {
	cases := []struct {
		name      string
		t         time.Time
		weeksBack int
		want      string
	}{
		// Wednesday game-time, mid-day — no boundary ambiguity.
		{"wed current", utc(2024, 1, 3, 14, 0), 0, "2024-01-01"},
		{"wed prev", utc(2024, 1, 3, 14, 0), 1, "2023-12-25"},
		// 01:30 UTC Monday = 23:30 Sunday game-time → week of that Sunday = 2023-12-25.
		{"mon 0130 utc is game-sunday", utc(2024, 1, 1, 1, 30), 0, "2023-12-25"},
		// 02:30 UTC Monday = 00:30 Monday game-time → this week.
		{"mon 0230 utc is game-monday", utc(2024, 1, 1, 2, 30), 0, "2024-01-01"},
	}
	for _, c := range cases {
		if got := gameWeekStart(c.t, c.weeksBack); got != c.want {
			t.Errorf("%s: gameWeekStart(%s,%d)=%q want %q", c.name, c.t.Format(time.RFC3339), c.weeksBack, got, c.want)
		}
	}
}

func TestGameWeekdayAt(t *testing.T) {
	cases := []struct {
		name string
		t    time.Time
		want int
	}{
		{"game-monday noon", utc(2024, 1, 1, 14, 0), 0},
		{"game-wednesday noon", utc(2024, 1, 3, 14, 0), 2},
		{"game-saturday noon", utc(2024, 1, 6, 14, 0), 5},
		{"game-sunday noon", utc(2024, 1, 7, 14, 0), 6},
		// Boundary: 01:30 UTC Monday is still Sunday game-time.
		{"mon 0130 utc -> game sunday", utc(2024, 1, 1, 1, 30), 6},
		// Boundary: 02:30 UTC Monday is Monday game-time.
		{"mon 0230 utc -> game monday", utc(2024, 1, 1, 2, 30), 0},
	}
	for _, c := range cases {
		if got := gameWeekdayAt(c.t); got != c.want {
			t.Errorf("%s: gameWeekdayAt(%s)=%d want %d", c.name, c.t.Format(time.RFC3339), got, c.want)
		}
	}
}

func TestEffectiveVSWeekAt(t *testing.T) {
	cases := []struct {
		name          string
		t             time.Time
		wantWeek      string
		wantCompleted int
	}{
		// Tue–Sat → current week, completed = weekday index.
		{"tuesday", utc(2024, 1, 2, 14, 0), "2024-01-01", 1},
		{"saturday", utc(2024, 1, 6, 14, 0), "2024-01-01", 5},
		// Sunday → current week, all 6 complete (no fallback).
		{"sunday", utc(2024, 1, 7, 14, 0), "2024-01-01", 6},
		// Monday game-time → fallback to previous complete week.
		{"monday fallback", utc(2024, 1, 8, 14, 0), "2024-01-01", 6},
		// Boundary: 02:30 UTC Monday = game Monday → fallback.
		{"monday 0230 utc fallback", utc(2024, 1, 8, 2, 30), "2024-01-01", 6},
		// Boundary: 01:30 UTC Monday = game Sunday → current week (the just-finished one).
		{"monday 0130 utc no fallback", utc(2024, 1, 8, 1, 30), "2024-01-01", 6},
	}
	for _, c := range cases {
		gotWeek, gotCompleted := effectiveVSWeekAt(c.t)
		if gotWeek != c.wantWeek || gotCompleted != c.wantCompleted {
			t.Errorf("%s: effectiveVSWeekAt(%s)=(%q,%d) want (%q,%d)",
				c.name, c.t.Format(time.RFC3339), gotWeek, gotCompleted, c.wantWeek, c.wantCompleted)
		}
	}
}

func TestDayDate(t *testing.T) {
	cases := []struct {
		base string
		add  int
		want string
	}{
		{"2024-01-01", 0, "2024-01-01"},
		{"2024-01-01", 5, "2024-01-06"},
		{"2024-01-31", 1, "2024-02-01"}, // month rollover
		{"bad-input", 1, "bad-input"},   // parse error returns base unchanged
	}
	for _, c := range cases {
		if got := dayDate(c.base, c.add); got != c.want {
			t.Errorf("dayDate(%q,%d)=%q want %q", c.base, c.add, got, c.want)
		}
	}
}

func TestNormalizeToGameWeekMonday(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"2024-01-01", "2024-01-01", false}, // already Monday → unchanged (no -2h shift)
		{"2024-01-03", "2024-01-01", false}, // Wednesday → its Monday
		{"2024-01-07", "2024-01-01", false}, // Sunday → that week's Monday
		{"2024-01-08", "2024-01-08", false}, // next Monday
		{"not-a-date", "", true},
	}
	for _, c := range cases {
		got, err := normalizeToGameWeekMonday(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("normalizeToGameWeekMonday(%q) expected error", c.in)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("normalizeToGameWeekMonday(%q)=(%q,%v) want (%q,nil)", c.in, got, err, c.want)
		}
	}
}
