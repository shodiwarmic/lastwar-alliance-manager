package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLootScenarios(t *testing.T) {
	// Pudgey's example: 20M → 600K … 3M; a 2.2M gap → 27/55/82/109/136 %.
	gap := int64(2_200_000)
	sc := lootScenarios(20_000_000, 3, 15, &gap)
	wantAmount := []int64{600_000, 1_200_000, 1_800_000, 2_400_000, 3_000_000}
	wantPct := []float64{27.3, 54.5, 81.8, 109.1, 136.4}
	if len(sc) != 5 {
		t.Fatalf("%d scenarios", len(sc))
	}
	for i, s := range sc {
		if s.Amount != wantAmount[i] || s.OfGap == nil || *s.OfGap != wantPct[i] {
			t.Errorf("scenario %d: %+v (want %d, %.1f%%)", i, s, wantAmount[i], wantPct[i])
		}
	}
	past := int64(-5)
	if sc := lootScenarios(20_000_000, 3, 15, &past); sc[0].OfGap != nil {
		t.Error("a gap already closed still gets a percentage")
	}
	if sc := lootScenarios(20_000_000, 3, 15, nil); sc[0].OfGap != nil {
		t.Error("an unknown gap still gets a percentage")
	}
}

func TestResourceProjection(t *testing.T) {
	l6, l3, sh := 6, 3, 5
	levels := []landLevel{{Kind: "city", Level: 6, ResourcePerHour: ptrInt(1000)}, {Kind: "city", Level: 3, ResourcePerHour: ptrInt(400)},
		{Kind: "stronghold", Level: 5, ResourcePerHour: ptrInt(9999)}}
	held := []landHolding{{Name: "A", Kind: "city", Level: &l6}, {Name: "B", Kind: "city", Level: &l3},
		{Name: "S", Kind: "stronghold", Level: &sh}, {Name: "Mystery", Kind: "city"}}
	per, sources, unknown := productionFromHoldings(held, levels)
	if per != 1400 || len(sources) != 2 || len(unknown) != 1 {
		t.Errorf("production %d from %v, unknown %v (strongholds produce no Copper)", per, sources, unknown)
	}
	obs := *stTime(t, "2030-11-10 10:00")
	now := *stTime(t, "2030-11-10 20:00")
	if got := projectTotal(1_000_000, per, obs, now, nil); got != 1_014_000 {
		t.Errorf("ten hours: %d", got)
	}
	stop := stTime(t, "2030-11-10 12:00")
	if got := projectTotal(1_000_000, per, obs, now, stop); got != 1_002_800 {
		t.Errorf("capped at the stop: %d", got)
	}
	if r := nextGameReset(now); landST(&r) != "2030-11-11 00:00" {
		t.Errorf("next reset %s", landST(&r))
	}
}

func ptrInt(v int) *int { return &v }

func TestRewardTierStatus(t *testing.T) {
	p := func(v int64) *int64 { return &v }
	cut := p(10_000_000)
	cases := []struct {
		latest, projected *int64
		loot              int64
		want              string
	}{
		{p(9_000_000), p(10_100_000), 0, "on_track"},
		{p(9_000_000), p(9_500_000), 1_500_000, "at_risk"},
		{p(9_000_000), p(9_500_000), 500_000, "behind"},
		{nil, nil, 0, "unknown"},
	}
	for _, c := range cases {
		if got := rewardTierStatus(c.latest, c.projected, cut, c.loot); got != c.want {
			t.Errorf("%v/%v loot %d: %s, want %s", c.latest, c.projected, c.loot, got, c.want)
		}
	}
	if got := rewardTierStatus(p(1), p(1), nil, 0); got != "unknown" {
		t.Errorf("no cutoff: %s", got)
	}
	// A cutoff for a rank no group covers still counts; the group is simply none.
	groups := []rankGroup{{Label: "King", RankFrom: 1, RankTo: 1}, {Label: "Queen", RankFrom: 2, RankTo: 3}}
	if g := groupForRank(groups, ptrInt(7)); g != nil {
		t.Errorf("rank 7 in %v", g)
	}
	if g := groupForRank(groups, ptrInt(3)); g == nil || g.Label != "Queen" {
		t.Errorf("rank 3 in %v", g)
	}
}

func TestWarResourcesPanel(t *testing.T) {
	seedWarSeason(t)
	mustExecT(t, `UPDATE seasons SET target_war_rank = 3 WHERE id = 1`)
	mustExecT(t, `INSERT INTO season_rank_groups (season_id, label, rank_from, rank_to, position) VALUES (1, 'King', 1, 1, 0), (1, 'Queen', 2, 3, 1), (1, 'Rook', 4, 10, 2)`)
	post := func(h http.HandlerFunc, body string) {
		t.Helper()
		rr := httptest.NewRecorder()
		h(rr, shReq(http.MethodPost, body, nil, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", body, rr.Code, rr.Body.String())
		}
	}
	at := time.Now().Add(-time.Hour)
	st := landST(&at)
	post(handleSeasonWarResourceCreate, `{"season_id":1,"own":true,"resource_total":7800000,"war_rank":5,"observed_at":"`+st+`"}`)
	post(handleSeasonWarResourceCreate, `{"season_id":1,"season_alliance_id":1,"resource_total":20000000,"observed_at":"`+st+`"}`)
	post(handleSeasonWarCutoffCreate, `{"season_id":1,"rank":3,"resource_total":10000000,"observed_at":"`+st+`"}`)
	post(handleSeasonWarCutoffCreate, `{"season_id":1,"rank":4,"resource_total":8500000,"observed_at":"`+st+`"}`)
	req := shReq(http.MethodGet, "", nil, nil)
	req.URL.RawQuery = "season_id=1"
	rr := httptest.NewRecorder()
	handleSeasonWarGet(rr, req)
	var out struct {
		Resources warResources `json:"resources"`
	}
	json.Unmarshal(rr.Body.Bytes(), &out)
	r := out.Resources
	if r.Gap == nil || *r.Gap != 2_200_000 || r.Group == nil || r.Group.Label != "Rook" || r.TargetGroup.Label != "Queen" ||
		r.GapNextAbove == nil || *r.GapNextAbove != 700_000 {
		t.Fatalf("panel: %+v", r)
	}
	if len(r.Loot) != 1 || *r.Loot[0].Scenarios[3].OfGap != 109.1 || r.Status != "at_risk" {
		t.Errorf("loot %+v status %s", r.Loot, r.Status)
	}

	// The phone sends our total and a cutoff.
	rr = httptest.NewRecorder()
	postMobileSeasonWarObservations(rr, shReq(http.MethodPost, `{"own":{"resource_total":9000000,"war_rank":4},"cutoffs":[{"rank":2,"resource_total":15000000}]}`, nil, nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"written":2`) {
		t.Errorf("mobile own and cutoffs: %d %s", rr.Code, rr.Body.String())
	}
}
