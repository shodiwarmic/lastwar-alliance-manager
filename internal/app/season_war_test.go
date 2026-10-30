package app

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMatchupScore(t *testing.T) {
	cfg := defaultWarConfig(1)
	theirs := sideFigures{Power: i64(1000), Top10: i64(1000), Top5: i64(1000), Top3: i64(1000)}
	// Pudgey's worked example: +8 / +6 / +2 / −5 → +3.85, Favorable.
	ours := sideFigures{Power: i64(1080), Top10: i64(1060), Top5: i64(1020), Top3: i64(950)}
	got := computeMatchup(ours, theirs, cfg)
	if got.Score == nil || *got.Score != 3.85 || got.Rating != "favorable" {
		t.Fatalf("worked example: %+v", got)
	}

	// One unknown category re-normalises 25/35/25 to 29.4/41.2/29.4.
	ours.Top3 = nil
	got = computeMatchup(ours, theirs, cfg)
	want := []float64{29.4, 41.2, 29.4}
	for i, w := range want {
		if math.Abs(got.Categories[i].Effective-w) > 0.05 {
			t.Errorf("category %s effective %.2f, want %.1f", got.Categories[i].Key, got.Categories[i].Effective, w)
		}
	}
	if got.Used != 3 || got.Score == nil {
		t.Errorf("three categories: %+v", got)
	}

	// Two unknown: withheld (minimum two categories... with two left it still rates; three
	// unknown is withheld). The season's minimum decides.
	ours.Top5, ours.Top10 = nil, nil
	if got = computeMatchup(ours, theirs, cfg); got.Score != nil || got.Rating != "insufficient_data" {
		t.Errorf("one category left: %+v", got)
	}
	cfg.MinCategories = 3
	ours = sideFigures{Power: i64(1080), Top10: i64(1060)}
	if got = computeMatchup(ours, theirs, cfg); got.Score != nil {
		t.Errorf("two categories under a minimum of three: %+v", got)
	}
	// An unknown is never a zero: theirs unknown drops the category too.
	cfg.MinCategories = 2
	if got = computeMatchup(sideFigures{Power: i64(1), Top10: i64(1)}, sideFigures{Power: i64(1)}, cfg); got.Score != nil {
		t.Errorf("their THP unknown still rated: %+v", got)
	}

	// Half-open bands.
	for _, c := range []struct {
		score float64
		want  string
	}{{10, "very_favorable"}, {9.99, "favorable"}, {3, "favorable"}, {2.99, "even"}, {-2.99, "even"},
		{-3, "unfavorable"}, {-9.99, "unfavorable"}, {-10, "very_unfavorable"}} {
		if got := matchupBand(c.score, cfg); got != c.want {
			t.Errorf("band(%v) = %s, want %s", c.score, got, c.want)
		}
	}
}

func TestOwnWarSideFromHeroPower(t *testing.T) {
	setupSettingsTestDB(t)
	for i := 1; i <= 12; i++ {
		mustExecT(t, `INSERT INTO members (id, name, rank) VALUES (?, ?, 'R3')`, i, "M"+string(rune('A'+i)))
		mustExecT(t, `INSERT INTO hero_power_history (member_id, power, recorded_at) VALUES (?, ?, '2026-10-01 10:00:00')`, i, i*100)
		mustExecT(t, `INSERT INTO hero_power_history (member_id, power, recorded_at) VALUES (?, ?, '2026-09-01 10:00:00')`, i, 1)
		mustExecT(t, `INSERT INTO power_history (member_id, power, recorded_at) VALUES (?, 1000, '2026-10-01 10:00:00')`, i)
	}
	mustExecT(t, `INSERT INTO members (id, name, rank) VALUES (13, 'Gone', 'EX')`)
	mustExecT(t, `INSERT INTO hero_power_history (member_id, power) VALUES (13, 999999)`)
	side, top, err := ownWarSide(db)
	if err != nil {
		t.Fatal(err)
	}
	// Top 3 = 1200 + 1100 + 1000; the former member is not counted.
	if side.Top3.Value == nil || *side.Top3.Value != 3300 || *side.Top10.Value != 7800-100-200 || len(top) != 10 {
		t.Errorf("own THP: top3 %v top10 %v (%d listed)", side.Top3.Value, side.Top10.Value, len(top))
	}
	if side.Power.Value == nil || *side.Power.Value != 12000 || *side.MemberCount.Value != 12 {
		t.Errorf("own power %v members %v", side.Power.Value, side.MemberCount.Value)
	}
	// Our own alliance-ranking row wins over the sum.
	mustExecT(t, `INSERT INTO alliance_stats_history (is_own, power, member_count, recorded_at, source) VALUES (1, 55555, 90, '2026-10-02 00:00:00', 'lastrank')`)
	if side, _, _ = ownWarSide(db); *side.Power.Value != 55555 {
		t.Errorf("own power with a ranking row: %v", *side.Power.Value)
	}
}

func seedWarSeason(t *testing.T) int {
	t.Helper()
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	seedSeasonRow(t, 1, 4, "2030-11-04", true, false)
	mustExecT(t, `INSERT INTO season_factions (id, season_id, key, label, is_ours, position) VALUES (1, 1, 'red', 'Red', 0, 0)`)
	mustExecT(t, `INSERT INTO external_alliances (id, tag, name, server) VALUES (5, 'ABC', 'Alpha', 1701), (6, 'ABC', 'Other', 1702), (7, 'XYZ', 'Xylo', 1701)`)
	mustExecT(t, `INSERT INTO season_alliances (id, season_id, external_alliance_id, relationship) VALUES (1, 1, 5, 'enemy')`)
	return 1
}

func TestMobileWarObservations(t *testing.T) {
	seedWarSeason(t)
	post := func(body string) map[string]any {
		t.Helper()
		rr := httptest.NewRecorder()
		postMobileSeasonWarObservations(rr, shReq(http.MethodPost, body, nil, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("post: %d %s", rr.Code, rr.Body.String())
		}
		var out map[string]any
		json.Unmarshal(rr.Body.Bytes(), &out)
		return out
	}
	out := post(`{"captured_at":"2026-10-01T12:00:00Z","alliances":[
		{"tag":"ABC","server":1701,"power":500000,"top3_thp":900},
		{"tag":"NOPE","server":1701,"power":1},
		{"tag":"XYZ","server":1701,"faction_key":"red","group_rank":4}]}`)
	if out["written"] != float64(2) || len(out["errors"].([]any)) != 1 {
		t.Fatalf("first post: %v", out)
	}
	if n := countT(t, `SELECT COUNT(*) FROM external_alliances WHERE tag = 'NOPE'`); n != 0 {
		t.Error("a phone minted a registry row")
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_alliances WHERE external_alliance_id = 7 AND faction_id = 1 AND group_rank = 4`); n != 1 {
		t.Error("the known alliance got no season row with its faction and rank")
	}
	// A repeat is unchanged; a bare tag is refused.
	out = post(`{"captured_at":"2026-10-01T12:00:00Z","alliances":[{"tag":"ABC","server":1701,"power":500000,"top3_thp":900},{"tag":"ABC","power":1}]}`)
	if out["written"] != float64(0) || len(out["errors"].([]any)) != 1 {
		t.Errorf("repeat: %v", out)
	}
}

func TestWarObservationsImportAndRead(t *testing.T) {
	seedWarSeason(t)
	csv := "Tag,Server,Power,Members,Rank,Top10 THP,Top5 THP,Top3 THP,Observed at\n" +
		"ABC,1701,1000,90,3,1000,1000,1000,2026-10-01 10:00\n" +
		"ABC,1702,5,,,,,,2026-10-01 10:00\n" +
		"ABC,1701,1100,,,,,,2026-10-02 10:00\n"
	call := func(h http.HandlerFunc) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]any{"season_id": 1, "kind": "opponent_observations", "csv": csv})
		rr := httptest.NewRecorder()
		h(rr, shReq(http.MethodPost, string(b), nil, nil))
		return rr
	}
	rr := call(handleSeasonImportPreview)
	if !strings.Contains(rr.Body.String(), `"invalid":1`) || !strings.Contains(rr.Body.String(), "add it on the War tab") {
		t.Errorf("preview (ABC on 1702 is not this season's): %s", rr.Body.String())
	}
	csv = strings.Replace(csv, "ABC,1702,5,,,,,,2026-10-01 10:00\n", "", 1)
	if rr := call(handleSeasonImportConfirm); rr.Code != http.StatusOK {
		t.Fatalf("confirm: %d %s", rr.Code, rr.Body.String())
	}
	// Our side: one typed own observation (no roster figures in this database).
	b := `{"season_id":1,"own":true,"observed_at":"2026-10-02 12:00","alliance_power":1210,"top10_thp":1050,"top5_thp":1020,"top3_thp":950}`
	rr = httptest.NewRecorder()
	handleSeasonWarObservationCreate(rr, shReq(http.MethodPost, b, nil, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("own observation: %d %s", rr.Code, rr.Body.String())
	}
	req := shReq(http.MethodGet, "", nil, nil)
	req.URL.RawQuery = "season_id=1"
	rr = httptest.NewRecorder()
	handleSeasonWarGet(rr, req)
	var out struct {
		Opponents []warOpponent `json:"opponents"`
	}
	json.Unmarshal(rr.Body.Bytes(), &out)
	if len(out.Opponents) != 1 {
		t.Fatalf("opponents: %s", rr.Body.String())
	}
	o := out.Opponents[0]
	// The latest power (1100, the second day) is compared; THP from the first row.
	if *o.Figures.Power.Value != 1100 || o.Figures.Power.At != "2026-10-02 12:00:00" || o.Matchup.Score == nil {
		t.Errorf("opponent figures %+v matchup %+v", o.Figures.Power, o.Matchup)
	}
	if *o.GroupRank != 3 {
		t.Errorf("group rank not taken from the observation: %v", o.GroupRank)
	}
	// The alliance can't be removed while it has observations.
	rr = httptest.NewRecorder()
	handleSeasonAllianceDelete(rr, shReq(http.MethodDelete, "", map[string]string{"id": "1"}, nil))
	if rr.Code != http.StatusConflict {
		t.Errorf("delete with observations: %d", rr.Code)
	}
}
