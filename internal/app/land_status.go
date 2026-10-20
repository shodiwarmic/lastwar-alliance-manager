// land_status.go — the Land tab's derived state (private-docs 274, 275). Pure: every function
// here takes rows and an instant and returns a judgement, so the rules are table-tested.
//
// Instants are stored in UTC (gametime.SQLiteLayout) and shown in server time (ST, UTC−2), the
// clock the game and the calendar use. landST / landUTC convert at the API boundary.

package app

import (
	"fmt"
	"sort"
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

// ── Holdings and daily capacity (private-docs 275) ─────────────────────────

// landEvent is one observed change to the season's land.
type landEvent struct {
	ID     int       `json:"id"`
	AreaID string    `json:"area_id"`
	Name   string    `json:"name"`
	Kind   string    `json:"kind"`
	Level  *int      `json:"level"`
	Action string    `json:"action"` // captured | abandoned | lost
	At     time.Time `json:"-"`
	Source string    `json:"source"`
	Note   string    `json:"note"`
}

// landHolding is one area held at an instant.
type landHolding struct {
	AreaID     string `json:"area_id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Level      *int   `json:"level"`
	CapturedAt string `json:"captured_at"` // ST
}

// landAreaKey identifies an area across events: its map ID, else its kind and name.
func landAreaKey(areaID, kind, name string) string {
	if areaID != "" {
		return "id:" + areaID
	}
	return "name:" + kind + ":" + strings.ToLower(strings.TrimSpace(name))
}

// landHoldingsAt derives what is held at instant: each area's latest event at or before it,
// held when that event is a capture. Events need not be sorted.
func landHoldingsAt(events []landEvent, instant time.Time) []landHolding {
	latest := map[string]landEvent{}
	for _, e := range events {
		if e.At.After(instant) {
			continue
		}
		k := landAreaKey(e.AreaID, e.Kind, e.Name)
		if prev, ok := latest[k]; !ok || !e.At.Before(prev.At) {
			latest[k] = e
		}
	}
	out := []landHolding{}
	for _, e := range latest {
		if e.Action == "captured" {
			at := e.At
			out = append(out, landHolding{AreaID: e.AreaID, Name: e.Name, Kind: e.Kind, Level: e.Level, CapturedAt: landST(&at)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].CapturedAt < out[j].CapturedAt
	})
	return out
}

// landKindCount is one kind's numbers on one day's card.
type landKindCount struct {
	Held          int  `json:"held"`
	Planned       int  `json:"planned"`
	DropsPlanned  int  `json:"drops_planned"`
	Cap           int  `json:"cap"`
	DropsRequired int  `json:"drops_required"`
	DailyLimit    int  `json:"daily_limit"`
	OverDaily     bool `json:"over_daily"`
}

// landDayCard is one game day's capacity: what is held at its start, what the active route
// plans to capture and drop that day, and how many more drops the caps require.
type landDayCard struct {
	Date        string        `json:"date"`
	Cities      landKindCount `json:"cities"`
	Strongholds landKindCount `json:"strongholds"`
}

// landPlannedDrop is a drop's kind and when it must start, for the capacity simulation.
type landPlannedDrop struct {
	Kind        string
	MustStartBy time.Time
}

// capacityKind maps an area kind onto the cap it counts against: cities, strongholds, or
// neither. The Capitol counts as a city only when the season says so.
func capacityKind(kind string, capitolCounts bool) string {
	switch kind {
	case "city":
		return "city"
	case "capitol":
		if capitolCounts {
			return "city"
		}
	case "stronghold":
		return "stronghold"
	}
	return ""
}

// landDailyCards simulates each game day from today's through the last day anything is planned
// (at least today). Inside a day, planned drops complete first and then captures land, so one
// drop frees one slot once and a slot is never free before its drop:
//
//	drops_required = max(0, held − drops planned + captures planned − cap)
//
// and the next day starts from held − drops + captures. Backup routes are never counted.
//
// doneToday counts today's captures already made, per capacity kind: they count toward today's
// daily limit but are already in held.
func landDailyCards(held []landHolding, captures []landTarget, drops []landPlannedDrop, doneToday map[string]int, cfg landConfig, now time.Time) []landDayCard {
	day := func(t time.Time) string { return t.In(gametimeLoc()).Format("2006-01-02") }
	count := map[string]int{}
	for _, h := range held {
		if k := capacityKind(h.Kind, cfg.CapitolCountsTowardCap); k != "" {
			count[k]++
		}
	}
	planned := map[string]map[string]int{}
	dropped := map[string]map[string]int{}
	last := day(now)
	add := func(m map[string]map[string]int, d, k string) {
		if m[d] == nil {
			m[d] = map[string]int{}
		}
		m[d][k]++
		if d > last {
			last = d
		}
	}
	today := day(now)
	for _, t := range captures {
		if t.PlannedCaptureAt == nil || t.Status == "captured" || t.Status == "skipped" || t.Status == "failed" {
			continue
		}
		k := capacityKind(t.Kind, cfg.CapitolCountsTowardCap)
		if d := day(*t.PlannedCaptureAt); k != "" && d >= today {
			add(planned, d, k)
		}
	}
	for _, dr := range drops {
		k := capacityKind(dr.Kind, cfg.CapitolCountsTowardCap)
		if d := day(dr.MustStartBy); k != "" && d >= today {
			add(dropped, d, k)
		}
	}
	var out []landDayCard
	d, _ := time.ParseInLocation("2006-01-02", today, gametimeLoc())
	for i := 0; i < 60; i++ {
		ds := d.Format("2006-01-02")
		if ds > last {
			break
		}
		card := landDayCard{Date: ds}
		for _, k := range []string{"city", "stronghold"} {
			c := landKindCount{Held: count[k], Planned: planned[ds][k], DropsPlanned: dropped[ds][k]}
			if k == "city" {
				c.Cap, c.DailyLimit = cfg.CityCap, cfg.CityDailyCaptures
			} else {
				c.Cap, c.DailyLimit = cfg.StrongholdCap, cfg.StrongholdDailyCaptures
			}
			if need := c.Held - c.DropsPlanned + c.Planned - c.Cap; need > 0 {
				c.DropsRequired = need
			}
			done := 0
			if ds == today {
				done = doneToday[k]
			}
			c.OverDaily = c.Planned+done > c.DailyLimit
			count[k] = c.Held - c.DropsPlanned + c.Planned
			if k == "city" {
				card.Cities = c
			} else {
				card.Strongholds = c
			}
		}
		out = append(out, card)
		d = d.AddDate(0, 0, 1)
	}
	return out
}

func gametimeLoc() *time.Location { return gametime.Loc }
