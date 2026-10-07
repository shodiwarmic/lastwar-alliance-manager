package gametime

import (
	"testing"
	"time"
)

func TestWeekMonday(t *testing.T) {
	for in, want := range map[string]string{
		"2026-10-05": "2026-10-05", // Monday
		"2026-10-10": "2026-10-05", // Saturday
		"2026-10-11": "2026-10-05", // Sunday belongs to the week before
		"2026-10-12": "2026-10-12",
	} {
		got, err := WeekMonday(in)
		if err != nil || got != want {
			t.Errorf("WeekMonday(%s) = %s, %v; want %s", in, got, err, want)
		}
	}
	if _, err := WeekMonday("not a date"); err == nil {
		t.Error("garbage accepted")
	}
}

func TestDateOfRollsOverAtTwoUTC(t *testing.T) {
	before := time.Date(2026, 10, 6, 1, 59, 0, 0, time.UTC)
	after := time.Date(2026, 10, 6, 2, 0, 0, 0, time.UTC)
	if DateOf(before) != "2026-10-05" || DateOf(after) != "2026-10-06" {
		t.Errorf("DateOf: %s / %s", DateOf(before), DateOf(after))
	}
}
