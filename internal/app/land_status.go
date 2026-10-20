// land_status.go — the Land tab's derived state (private-docs 274, 275). Pure: every function
// here takes rows and an instant and returns a judgement, so the rules are table-tested.
//
// Instants are stored in UTC (gametime.SQLiteLayout) and shown in server time (ST, UTC−2), the
// clock the game and the calendar use. landST / landUTC convert at the API boundary.

package app

import (
	"fmt"
	"strings"
	"time"

	"lastwar-alliance/internal/gametime"
)

const landSTLayout = "2006-01-02 15:04"

// parseLandST reads an officer's server-time instant ("2026-11-05 12:00", or with a T).
func parseLandST(s string) (time.Time, error) {
	s = strings.Replace(strings.TrimSpace(s), "T", " ", 1)
	if len(s) > len(landSTLayout) {
		s = s[:len(landSTLayout)]
	}
	return time.ParseInLocation(landSTLayout, s, gametime.Loc)
}

// landST formats an instant in server time; "" for nil.
func landST(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.In(gametime.Loc).Format(landSTLayout)
}

// landUTC is the stored form of an instant.
func landUTC(t time.Time) string { return t.UTC().Format(gametime.SQLiteLayout) }

// parseLandStored reads a stored instant (either shape the driver returns); nil when empty.
func parseLandStored(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, ok := lastRankParseTime(s)
	if !ok {
		return nil
	}
	return &t
}

// landUnlockAt is when a level unlocks: day (week−1)×7 + day of the season, at the game's
// reset plus the season's offset (S4: W1 day 3 at 12:00 ST).
func landUnlockAt(seasonStart string, week, day, offsetHours int) (time.Time, error) {
	start, err := time.ParseInLocation("2006-01-02", seasonStart, gametime.Loc)
	if err != nil {
		return time.Time{}, err
	}
	return start.AddDate(0, 0, (week-1)*7+(day-1)).Add(time.Duration(offsetHours) * time.Hour), nil
}

// landTarget is one step of a route, as the readiness rule reads it.
type landTarget struct {
	ID                   int        `json:"id"`
	Position             int        `json:"position"`
	AreaID               string     `json:"area_id"`
	Name                 string     `json:"name"`
	Kind                 string     `json:"kind"`
	Level                *int       `json:"level"`
	UnlockAt             *time.Time `json:"-"`
	PlannedCaptureAt     *time.Time `json:"-"`
	ActualCaptureAt      *time.Time `json:"-"`
	PrerequisiteTargetID *int       `json:"prerequisite_target_id"`
	Buff                 string     `json:"buff"`
	ResourcePerHour      *int       `json:"resource_per_hour"`
	Status               string     `json:"status"`    // planned | declared | captured | skipped | failed
	Connected            string     `json:"connected"` // touching | not_touching | unknown
	CoordinatorMemberID  *int       `json:"coordinator_member_id"`
	Notes                string     `json:"notes"`
}

// landReadiness is the Land Path Status: how far along the active route is and whether its
// next target will be reachable in time.
type landReadiness struct {
	RouteID     int              `json:"route_id"`
	RouteName   string           `json:"route_name"`
	Slot        string           `json:"slot"`
	Captured    int              `json:"captured"`
	Total       int              `json:"total"`
	Next        *readinessTarget `json:"next"`
	Status      string           `json:"status"` // none | completed | unknown | on_track | at_risk | behind
	Reasons     []string         `json:"reasons"`
	BufferHours int              `json:"buffer_hours"`
}

type readinessTarget struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Level     *int   `json:"level"`
	UnlockAt  string `json:"unlock_at"` // ST
	Countdown string `json:"countdown"` // "2d 4h", "" when unknown or passed
	Connected string `json:"connected"`
}

func countdown(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	return fmt.Sprintf("%dh %dm", hours, int(d.Minutes())%60)
}

var connectedLabels = map[string]string{"touching": "Touching", "not_touching": "Not Touching", "unknown": "Unknown"}

// computeLandReadiness applies the readiness rule (decision 15) to a route's targets in order:
//
//   - every target captured or skipped → completed;
//   - the next target's unlock unknown → unknown;
//   - the unlock has passed uncaptured → behind;
//   - connected unknown → unknown (the officer's field is authoritative; nothing is computed);
//   - Touching → on track;
//   - Not Touching → on track when its prerequisite target is captured, or planned to be
//     captured by unlock − buffer while that deadline is still ahead; else at risk.
func computeLandReadiness(routeID int, routeName, slot string, targets []landTarget, bufferHours int, now time.Time) landReadiness {
	out := landReadiness{RouteID: routeID, RouteName: routeName, Slot: slot, Total: len(targets),
		BufferHours: bufferHours, Reasons: []string{}}
	if routeID == 0 {
		out.Status = "none"
		out.Reasons = append(out.Reasons, "No active route")
		return out
	}
	byID := map[int]landTarget{}
	var next *landTarget
	for i := range targets {
		t := targets[i]
		byID[t.ID] = t
		if t.Status == "captured" {
			out.Captured++
		}
		if next == nil && t.Status != "captured" && t.Status != "skipped" {
			next = &targets[i]
		}
	}
	if next == nil {
		out.Status = "completed"
		if len(targets) == 0 {
			out.Status = "none"
			out.Reasons = append(out.Reasons, "The active route has no targets")
		} else {
			out.Reasons = append(out.Reasons, "Every target on the route is captured")
		}
		return out
	}
	rt := &readinessTarget{ID: next.ID, Name: next.Name, Level: next.Level, Connected: next.Connected,
		UnlockAt: landST(next.UnlockAt)}
	out.Next = rt
	conn := connectedLabels[next.Connected]
	if conn == "" {
		conn = "Unknown"
	}
	if next.UnlockAt == nil {
		out.Status = "unknown"
		out.Reasons = append(out.Reasons, "Unlock time not set — choose a level or type the unlock", "connected: "+conn)
		return out
	}
	unlock := *next.UnlockAt
	deadline := unlock.Add(-time.Duration(bufferHours) * time.Hour)
	rt.Countdown = countdown(unlock.Sub(now))
	base := fmt.Sprintf("unlock %s ST, buffer %d h, connected: %s", landST(&unlock), bufferHours, conn)
	switch {
	case !now.Before(unlock):
		out.Status = "behind"
		out.Reasons = append(out.Reasons, base, "The unlock has passed and the target is not captured")
	case next.Connected != "touching" && next.Connected != "not_touching":
		out.Status = "unknown"
		out.Reasons = append(out.Reasons, base, "Set whether our land touches this target")
	case next.Connected == "touching":
		out.Status = "on_track"
		out.Reasons = append(out.Reasons, base)
	default:
		var pre *landTarget
		if next.PrerequisiteTargetID != nil {
			if p, ok := byID[*next.PrerequisiteTargetID]; ok {
				pre = &p
			}
		}
		switch {
		case pre != nil && pre.Status == "captured":
			out.Status = "on_track"
			out.Reasons = append(out.Reasons, base, pre.Name+" is captured — mark this target Touching once it is")
		case pre != nil && pre.Status != "failed" && pre.PlannedCaptureAt != nil &&
			!pre.PlannedCaptureAt.After(deadline) && now.Before(deadline):
			out.Status = "on_track"
			out.Reasons = append(out.Reasons, base, fmt.Sprintf("%s is planned for %s ST, before %s ST",
				pre.Name, landST(pre.PlannedCaptureAt), landST(&deadline)))
		default:
			out.Status = "at_risk"
			why := "No planned capture connects it before " + landST(&deadline) + " ST"
			if pre != nil && pre.PlannedCaptureAt != nil {
				why = fmt.Sprintf("%s is planned for %s ST, after %s ST", pre.Name, landST(pre.PlannedCaptureAt), landST(&deadline))
			}
			out.Reasons = append(out.Reasons, base, why)
		}
	}
	return out
}
