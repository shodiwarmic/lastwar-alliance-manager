package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBottomFive(t *testing.T) {
	mk := func(name string, pct float64) SeasonMember {
		return SeasonMember{Name: name, Rank: "R3", Standing: standing{Overall: categoryStanding{Applicable: true, Pct: pct}}}
	}
	members := []SeasonMember{mk("a", 90), mk("b", 10), mk("c", 20), mk("d", 30), mk("e", 40), mk("f", 40), mk("g", 40), mk("h", 50),
		{Name: "gone", Rank: "EX", Standing: standing{Overall: categoryStanding{Applicable: true, Pct: 0}}}}
	got := bottomFive(members)
	if len(got) != 6 || got[0].Name != "b" || got[5].Pct != 40 {
		t.Errorf("a tie at fifth returns six, former members excluded: %+v", got)
	}
	if got := bottomFive(members[:3]); len(got) != 3 {
		t.Errorf("fewer than five: %+v", got)
	}
}

func TestSeasonDashboardPermissions(t *testing.T) {
	seedAttendanceSeason(t)
	get := func(actor *AuthUser) map[string]any {
		t.Helper()
		req := shReq(http.MethodGet, "", nil, actor)
		req.URL.RawQuery = "season_id=1"
		rr := httptest.NewRecorder()
		handleSeasonDashboard(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("dashboard: %d %s", rr.Code, rr.Body.String())
		}
		var out struct {
			Cards []seasonLayoutItem `json:"cards"`
			Data  map[string]any     `json:"data"`
		}
		json.Unmarshal(rr.Body.Bytes(), &out)
		return out.Data
	}
	admin := get(nil)
	for _, k := range []string{"faction_allies", "leadership_flags", "season_schedule", "land_status", "war_record", "reward_tier_status"} {
		if _, ok := admin[k]; !ok {
			t.Errorf("admin missing %s", k)
		}
	}
	r1 := get(&AuthUser{ID: 2, Username: "r1", Rank: "R1"})
	for _, k := range []string{"leadership_flags", "land_status", "war_record", "reward_tier_status"} {
		if _, ok := r1[k]; ok {
			t.Errorf("an R1 got %s", k)
		}
	}
	if _, ok := r1["faction_allies"]; !ok {
		t.Error("an R1 lost the faction allies card")
	}
}

func TestSeasonDashboardArchivedNames(t *testing.T) {
	seedAttendanceSeason(t)
	mustExecT(t, `INSERT INTO season_participation (season_id, member_id, week_number, score) VALUES (1, 1, 1, 'absent')`)
	mustExecT(t, `INSERT INTO season_roster_snapshot (season_id, member_id, name, rank) VALUES (1, 1, 'Alpha Then', 'R3'), (1, 2, 'Bravo', 'R3')`)
	mustExecT(t, `UPDATE seasons SET archived_at = '2026-11-01 00:00:00', is_active = 0 WHERE id = 1`)
	mustExecT(t, `UPDATE members SET name = 'Alpha Now' WHERE id = 1`)
	req := shReq(http.MethodGet, "", nil, nil)
	req.URL.RawQuery = "season_id=1"
	rr := httptest.NewRecorder()
	handleSeasonDashboard(rr, req)
	var out struct {
		Data struct {
			LeadershipFlags struct {
				Bottom []dashboardFlagMember `json:"bottom"`
			} `json:"leadership_flags"`
		} `json:"data"`
	}
	json.Unmarshal(rr.Body.Bytes(), &out)
	found := false
	for _, m := range out.Data.LeadershipFlags.Bottom {
		if m.MemberID == 1 {
			found = m.Name == "Alpha Then"
		}
	}
	if !found {
		t.Errorf("archived season does not use the snapshot name: %s", rr.Body.String())
	}
}
