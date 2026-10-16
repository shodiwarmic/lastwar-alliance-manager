package app

import (
	"encoding/json"
	"net/http"
	"testing"
)

// AsOf 2026-10-15 12:00 UTC: the 30-day cutoff is 2026-09-15 12:00:00, the 7-day 10-08 12:00:00.
const growthNow = "2026-10-15T12:00:00Z"

func seedGrowth(t *testing.T) {
	t.Helper()
	setupSettingsTestDB(t)
	mustExec(t, `INSERT INTO members (id, name, rank, eligible, troop_level, squad_type) VALUES
		(1, 'Old', 'R3', 1, 11, 'Tank'),
		(2, 'New', 'R3', 1, 10, 'Missile'),
		(3, 'Shrink', 'R2', 1, 0, ''),
		(4, 'NotForTrains', 'R3', 0, 0, ''),
		(5, 'Former', 'EX', 1, 0, ''),
		(6, 'Edge', 'R1', 1, 0, ''),
		(7, 'Tie', 'R1', 1, 0, '')`)
	mustExec(t, `INSERT INTO power_history (id, member_id, power, recorded_at) VALUES
		(1, 1, 100, '2026-09-01 00:00:00'), (2, 1, 160, '2026-10-14 00:00:00'),
		(3, 2, 500, '2026-10-01 00:00:00'),
		(4, 3, 300, '2026-09-01 00:00:00'), (5, 3, 250, '2026-10-14 00:00:00'),
		(6, 4, 50, '2026-09-01 00:00:00'), (7, 4, 70, '2026-10-14 00:00:00'),
		(8, 5, 10, '2026-09-01 00:00:00'), (9, 5, 99, '2026-10-14 00:00:00'),
		(10, 6, 40, '2026-09-15 12:00:00'), (11, 6, 41, '2026-09-15 13:30:00'), (12, 6, 39, '2026-09-15 10:30:00'), (13, 6, 45, '2026-10-14 00:00:00'),
		(14, 7, 20, '2026-09-01 00:00:00'), (15, 7, 21, '2026-09-01 00:00:00'),
		(16, 7, 30, '2026-10-14 00:00:00'), (17, 7, 31, '2026-10-14 00:00:00')`)
}

func growthByName(t *testing.T, days int) map[string]memberGrowth {
	t.Helper()
	list, err := powerGrowth(db, growthParams{Table: histPower, WindowDays: days, AsOf: mustTime(t, growthNow)})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]memberGrowth{}
	for _, g := range list {
		if _, dup := by[g.Name]; dup {
			t.Errorf("%s appears twice", g.Name)
		}
		by[g.Name] = g
	}
	return by
}

func TestPowerGrowth(t *testing.T) {
	seedGrowth(t)
	by := growthByName(t, 30)
	val := func(name string) any {
		if g := by[name].Growth; g != nil {
			return *g
		}
		return nil
	}
	cases := map[string]any{
		"Old":          int64(60),
		"New":          nil,            // 241: newer than the window → unknown, not 500
		"Shrink":       int64(-50),     // negative growth is growth
		"NotForTrains": int64(20),      // 252: eligible = 0 is train eligibility, not activity
		"Edge":         int64(5),       // the reading exactly at the cutoff is the baseline
		"Tie":          int64(31 - 21), // two readings at one second: the later id wins, once
	}
	for name, want := range cases {
		if got := val(name); got != want {
			t.Errorf("%s growth = %v, want %v", name, got, want)
		}
	}
	if _, ok := by["Former"]; ok {
		t.Error("an EX member is listed")
	}
	if g := by["Old"]; g.TroopLevel != 11 || g.SquadType != "Tank" {
		t.Errorf("composition fields: %+v", g)
	}
	// The cutoff is UTC: 13:30 is after it, 10:30 before — Edge's baseline is the 12:00 reading.
	if b := by["Edge"].Baseline; b == nil || *b != 40 {
		t.Errorf("Edge baseline %v", b)
	}

	// 90 days on data at most ~45 days deep: every member unknown.
	for name, g := range growthByName(t, 90) {
		if g.Growth != nil {
			t.Errorf("90 days: %s has growth %d", name, *g.Growth)
		}
	}
	for _, days := range []int{0, 366} {
		if _, err := powerGrowth(db, growthParams{Table: histPower, WindowDays: days, AsOf: mustTime(t, growthNow)}); !isReportParamError(err) {
			t.Errorf("window %d: err %v", days, err)
		}
	}
	if _, err := powerGrowth(db, growthParams{Table: "members; --", WindowDays: 7, AsOf: mustTime(t, growthNow)}); !isReportParamError(err) {
		t.Errorf("unknown table accepted: %v", err)
	}
}

func TestRankingsRoute(t *testing.T) {
	setupPreviewTestDB(t)
	mustExec(t, `INSERT INTO power_history (member_id, power, recorded_at) VALUES (2, 500, datetime('now', '-1 day'))`)
	officer, admin := sessionCookie(t, 3, ""), sessionCookie(t, 1, "")
	mustExec(t, `UPDATE rank_permissions SET permissions = json_set(permissions, '$.view_rankings', json('false')) WHERE rank = 'R3'`)
	if w := serveRouter(t, "GET", "/api/rankings", "", officer); w.Code != http.StatusForbidden {
		t.Errorf("without view_rankings: %d", w.Code)
	}
	for q, want := range map[string]int{"": 200, "?short=1&long=365": 200, "?short=0": 400, "?long=366": 400, "?short=x": 400} {
		if w := serveRouter(t, "GET", "/api/rankings"+q, "", admin); w.Code != want {
			t.Errorf("%q: %d, want %d", q, w.Code, want)
		}
	}
	w := serveRouter(t, "GET", "/api/rankings?short=14&long=90", "", admin)
	var body struct {
		ShortDays  int              `json:"short_days"`
		LongDays   int              `json:"long_days"`
		GrowthData []map[string]any `json:"growth_data"`
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.ShortDays != 14 || body.LongDays != 90 {
		t.Errorf("windows echoed %d/%d", body.ShortDays, body.LongDays)
	}
	if len(body.GrowthData) == 0 {
		t.Fatal("no growth rows")
	}
	for _, row := range body.GrowthData {
		if v, ok := row["growth_short"]; !ok || v != nil {
			t.Errorf("no-baseline growth_short = %v (%v), want null", v, ok)
		}
	}
}
