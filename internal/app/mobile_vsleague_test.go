package app

import (
	"net/http"
	"strings"
	"testing"
)

// The mobile VS League store (Project 15, C11), and the week-id fix the shared upsert brings.

func TestLeagueWeekUpsertReturnsItsOwnID(t *testing.T) {
	setupMobileTestDB(t)
	admin := sessionCookie(t, 1, "")
	db.Exec(`INSERT INTO vs_league_seasons (id, season_number, is_active) VALUES (7, 34, 1)`)
	post := func(body string) float64 {
		w := serveRouter(t, "POST", "/api/vs-league/weeks", body, admin)
		if w.Code != http.StatusOK {
			t.Fatalf("week: %d %s", w.Code, w.Body.String())
		}
		return decodeObject(t, w)["id"].(float64)
	}
	first := post(`{"season_id":7,"week_date":"2026-10-05","opponent_tag":"ABC"}`)
	post(`{"season_id":7,"week_date":"2026-10-12"}`) // another week, so the last insert is elsewhere
	// The repeat upsert hits the UPDATE branch: it must answer the week's own id, not
	// last_insert_rowid() (an activity_log row on the single connection).
	if again := post(`{"season_id":7,"week_date":"2026-10-05","opponent_name":"Alpha"}`); again != first {
		t.Errorf("repeat upsert answered id %v, want %v", again, first)
	}
}

func TestMobileVSLeagueWeek(t *testing.T) {
	setupMobileTestDB(t)
	tok := mobileToken(t, 2)

	if w := serveMobile(t, "POST", "/api/mobile/vs-league/week", `{"week_date":"2026-10-05"}`, tok); w.Code != http.StatusConflict ||
		!strings.Contains(w.Body.String(), "No active VS League season") {
		t.Errorf("no season: %d %s", w.Code, w.Body.String())
	}
	db.Exec(`INSERT INTO vs_league_seasons (id, season_number, is_active, league_tier) VALUES (7, 34, 1, 'Gold')`)

	w := serveMobile(t, "POST", "/api/mobile/vs-league/week", `{"week_date":"2026-10-07",
		"week":{"week_number":3,"opponent_tag":"ABC","opponent_power":900,"our_power":800,"our_server":1701},
		"days":[{"day_number":1,"our_score":10,"opponent_score":5,"outcome":"win","mvp_is_ours":true,"mvp_name":"Zed Player"},
		        {"day_number":2,"our_score":1,"opponent_score":2}],
		"matchups":[{"match_index":1,"a_rank":1,"a_tag":"ABC","a_points":13,"b_rank":16,"b_points":0,"is_ours":true}],
		"strategy_label":"push","notes":"secret"}`, tok)
	if w.Code != http.StatusOK {
		t.Fatalf("week: %d %s", w.Code, w.Body.String())
	}
	out := decodeObject(t, w)
	if out["week_date"] != "2026-10-05" || out["opponent_tag"] != "ABC" || len(out["days"].([]any)) != 2 || len(out["matchups"].([]any)) != 1 {
		t.Errorf("week = %v", out)
	}
	var label, notes, oppSnap, ourSnap *string
	var mvp *int
	db.QueryRow(`SELECT strategy_label, notes, opponent_snapshot_at, our_snapshot_at FROM vs_league_weeks WHERE week_date = '2026-10-05'`).
		Scan(&label, &notes, &oppSnap, &ourSnap)
	if label != nil || notes != nil || oppSnap == nil || ourSnap == nil {
		t.Errorf("strategy %v notes %v snapshots %v %v", label, notes, oppSnap, ourSnap)
	}
	db.QueryRow(`SELECT mvp_member_id FROM vs_league_days WHERE day_number = 1`).Scan(&mvp)
	if mvp == nil {
		t.Error("MVP not resolved to our member")
	}
	var day2 string
	db.QueryRow(`SELECT outcome FROM vs_league_days WHERE day_number = 2`).Scan(&day2)
	if day2 != "loss" {
		t.Errorf("day 2 outcome = %q, want derived loss", day2)
	}

	// A field left out keeps its stored value; the GET shows the current week shape.
	serveMobile(t, "POST", "/api/mobile/vs-league/week", `{"week_date":"2026-10-05","week":{"league_rank":4}}`, tok)
	var tag string
	db.QueryRow(`SELECT opponent_tag FROM vs_league_weeks WHERE week_date = '2026-10-05'`).Scan(&tag)
	if tag != "ABC" {
		t.Errorf("tag lost: %q", tag)
	}
	cur := decodeObject(t, serveMobile(t, "GET", "/api/mobile/vs-league/current", "", tok))
	assertKeys(t, "current", cur, "season", "current_week_date", "week")

	// Refusals.
	for body, want := range map[string]int{
		`{"week_date":"2026-10-05","days":[{"day_number":1,"our_score":1,"opponent_score":5,"outcome":"win"}]}`: http.StatusBadRequest,
		`{"week_date":"2026-10-05","matchups":[{},{},{},{},{},{},{},{},{}]}`:                                   http.StatusBadRequest,
		`{"week_date":"2026-10-05","matchups":[{"is_ours":true},{"is_ours":true}]}`:                            http.StatusBadRequest,
		`{"week_date":"2026-10-12","week":{"week_number":3}}`:                                                  http.StatusConflict,
		`{"week_date":"someday"}`:                                                                              http.StatusBadRequest,
	} {
		if w := serveMobile(t, "POST", "/api/mobile/vs-league/week", body, tok); w.Code != want {
			t.Errorf("%s: %d, want %d (%s)", body, w.Code, want, w.Body.String())
		}
	}
	if n := countRows(t, `SELECT COUNT(*) FROM activity_log WHERE details LIKE '%via mobile' AND entity_name LIKE 'Week%'`); n != 2 {
		t.Errorf("activity rows = %d", n)
	}
}
