package app

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// The mobile alliance-stats store (Project 15, C13). Mobile datapoints are history only:
// LastRank captures remain the NAP view's only source.

func setupAllianceStatsTestDB(t *testing.T) (tok string, admin *http.Cookie) {
	t.Helper()
	setupMobileTestDB(t)
	setRankPerm(t, "R4", "manage_allies", true)
	setRankPerm(t, "R4", "view_allies", true)
	ladder := time.Now().UTC().Add(-48 * time.Hour).Format(sqliteTimeLayout)
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE settings SET our_server_id = 1701, nap_size = 10, nap_import_limit = 15 WHERE id = 1`, nil},
		{`INSERT INTO external_alliances (id, tag, name, server, lastrank_id, power, kills, member_count, power_rank, lastrank_captured_at)
		  VALUES (5, 'TOP', 'Top Dogs', 1701, 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 900, 90, 100, 1, ?)`, []any{ladder}},
		{`INSERT INTO alliance_stats_history (external_alliance_id, is_own, server, tag, power, kills, power_rank, member_count, recorded_at, source)
		  VALUES (5, 0, 1701, 'TOP', 900, 90, 1, 100, ?, 'lastrank')`, []any{ladder}},
		{`INSERT INTO alliance_stats_history (external_alliance_id, is_own, server, tag, lastrank_id, power, kills, power_rank, member_count, recorded_at, source)
		  VALUES (NULL, 1, 1701, 'OURS', 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', 800, 80, 2, 99, ?, 'lastrank')`, []any{ladder}},
	} {
		if _, err := db.Exec(q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
	return mobileToken(t, 2), sessionCookie(t, 1, "")
}

func napView(t *testing.T, admin *http.Cookie) (capturedAt string, ourRank any, ourMembers any) {
	t.Helper()
	w := serveRouter(t, "GET", "/api/allies/nap", "", admin)
	if w.Code != http.StatusOK {
		t.Fatalf("nap: %d %s", w.Code, w.Body.String())
	}
	out := decodeObject(t, w)
	for _, a := range out["alliances"].([]any) {
		m := a.(map[string]any)
		if m["is_us"] == true {
			return out["captured_at"].(string), m["rank"], m["member_count"]
		}
	}
	return out["captured_at"].(string), nil, nil
}

func TestMobileAllianceStatsLeaveTheNAPViewAlone(t *testing.T) {
	tok, admin := setupAllianceStatsTestDB(t)

	// With no alliance tag the route refuses and capabilities say so.
	if w := serveMobile(t, "POST", "/api/mobile/alliance-stats", `{"server":1701,"alliances":[{"tag":"X","power":1}]}`, tok); w.Code != http.StatusConflict {
		t.Errorf("no tag: %d %s", w.Code, w.Body.String())
	}
	if c := capabilities(t, tok)["stores"].(map[string]any)["alliance_stats"].(map[string]any); c["write"] != false {
		t.Errorf("capabilities without a tag = %v", c)
	}
	db.Exec(`UPDATE settings SET alliance_tag = 'OURS' WHERE id = 1`)
	if c := capabilities(t, tok)["stores"].(map[string]any)["alliance_stats"].(map[string]any); c["write"] != true {
		t.Errorf("capabilities with a tag = %v", c)
	}

	beforeCapture, beforeRank, beforeMembers := napView(t, admin)
	if beforeRank == nil {
		t.Fatal("fixture: our row is missing from the NAP view")
	}
	var job napMembersJob
	if _, err := job.Plan(context.Background()); err != nil {
		t.Fatal(err)
	}
	beforeKey := job.capturedAt

	body := `{"server":1701,"alliances":[
		{"tag":"OURS","name":"Us","power":850,"kills":85,"member_count":98,"power_rank":2},
		{"tag":"TOP","name":"Top Dogs","power":950,"kills":95,"member_count":100,"power_rank":1},
		{"tag":"NEW","name":"Newcomers","power":10}]}`
	out := decodeObject(t, serveMobile(t, "POST", "/api/mobile/alliance-stats", body, tok))
	if out["recorded"] != float64(3) || out["unchanged"] != float64(0) {
		t.Fatalf("upload = %v", out)
	}

	// The NAP view, its capture key and the member-count backfill key are unchanged.
	afterCapture, afterRank, afterMembers := napView(t, admin)
	if afterCapture != beforeCapture || afterRank != beforeRank || afterMembers != beforeMembers {
		t.Errorf("NAP view moved: capture %q→%q, rank %v→%v, members %v→%v",
			beforeCapture, afterCapture, beforeRank, afterRank, beforeMembers, afterMembers)
	}
	job = napMembersJob{}
	job.Plan(context.Background())
	if job.capturedAt != beforeKey {
		t.Errorf("job key moved %q → %q", beforeKey, job.capturedAt)
	}

	// Our own alliance went to the is_own series; the registry's stats are untouched.
	if n := countRows(t, `SELECT COUNT(*) FROM alliance_stats_history WHERE is_own = 1 AND source = 'mobile' AND external_alliance_id IS NULL`); n != 1 {
		t.Errorf("own mobile rows = %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM external_alliances WHERE tag = 'OURS'`); n != 0 {
		t.Error("our alliance was registered (Rule 2)")
	}
	var power int64
	db.QueryRow(`SELECT power FROM external_alliances WHERE id = 5`).Scan(&power)
	if power != 900 {
		t.Errorf("registry power = %d, want LastRank's 900", power)
	}

	// An unchanged repeat and a same-second repeat write nothing and answer 200.
	w := serveMobile(t, "POST", "/api/mobile/alliance-stats", body, tok)
	if out := decodeObject(t, w); w.Code != http.StatusOK || out["recorded"] != float64(0) || out["unchanged"] != float64(3) {
		t.Errorf("repeat = %d %v", w.Code, out)
	}
	at := time.Now().UTC().Format(time.RFC3339)
	changed := `{"server":1701,"captured_at":"` + at + `","alliances":[{"tag":"NEW","power":20}]}`
	serveMobile(t, "POST", "/api/mobile/alliance-stats", changed, tok)
	w = serveMobile(t, "POST", "/api/mobile/alliance-stats", `{"server":1701,"captured_at":"`+at+`","alliances":[{"tag":"NEW","power":30}]}`, tok)
	if out := decodeObject(t, w); w.Code != http.StatusOK || out["recorded"] != float64(0) || out["unchanged"] != float64(1) {
		t.Errorf("same-second collision = %d %v", w.Code, out)
	}

	// The same tag on another server is another registry row; a re-observed one adds none.
	serveMobile(t, "POST", "/api/mobile/alliance-stats", `{"server":1702,"alliances":[{"tag":"NEW","power":5}]}`, tok)
	if n := countRows(t, `SELECT COUNT(*) FROM external_alliances WHERE tag = 'NEW'`); n != 2 {
		t.Errorf("NEW registry rows = %d, want one per server", n)
	}
	if w := serveMobile(t, "POST", "/api/mobile/alliance-stats", `{"alliances":[]}`, tok); w.Code != http.StatusBadRequest {
		t.Errorf("no server: %d", w.Code)
	}
}
