package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestTradePostsBoard(t *testing.T) {
	seedWarSeason(t)
	create := func(body string) int {
		t.Helper()
		rr := httptest.NewRecorder()
		handleTradePostCreate(rr, shReq(http.MethodPost, body, nil, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
		}
		var out struct {
			ID int `json:"id"`
		}
		json.Unmarshal(rr.Body.Bytes(), &out)
		return out.ID
	}
	// A map ID names a post from the Season 4 lookup.
	take := create(`{"season_id":1,"post_key":"170","planned_action":"take","planned_at":"2030-11-08 14:00"}`)
	hold := create(`{"season_id":1,"post_key":"171","planned_action":"hold","planned_at":"2030-11-08 14:00"}`)
	var name string
	db.QueryRow(`SELECT name FROM season_trade_posts WHERE id = ?`, take).Scan(&name)
	if name != "Trade Post 170" {
		t.Errorf("post named %q", name)
	}
	if n := countT(t, `SELECT COUNT(*) FROM schedule_events WHERE source_kind = 'trade_post' AND source_id = ? AND event_date = '2030-11-08'
		AND event_time = '14:00' AND status = 'confirmed'`, take); n != 1 {
		t.Error("no calendar row for take with a time")
	}
	if n := countT(t, `SELECT COUNT(*) FROM schedule_events WHERE source_kind = 'trade_post' AND source_id = ?`, hold); n != 0 {
		t.Error("hold put a row on the calendar")
	}
	// Clearing the plan cancels the row.
	rr := httptest.NewRecorder()
	handleTradePostUpdate(rr, shReq(http.MethodPut, `{"planned_action":""}`, map[string]string{"id": strconv.Itoa(take)}, nil))
	if n := countT(t, `SELECT COUNT(*) FROM schedule_events WHERE source_kind = 'trade_post' AND source_id = ? AND status = 'cancelled'`, take); n != 1 {
		t.Errorf("clearing the plan: %d %s", rr.Code, rr.Body.String())
	}

	// Owners: the latest observation wins; one the season does not list is kept as a label.
	obs := func(body string) {
		t.Helper()
		rr := httptest.NewRecorder()
		handleTradePostObservationCreate(rr, shReq(http.MethodPost, body, map[string]string{"id": strconv.Itoa(take)}, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("observation: %d %s", rr.Code, rr.Body.String())
		}
	}
	obs(`{"owner_season_alliance_id":1,"observed_at":"2026-10-01 10:00"}`)
	obs(`{"owner_label":"QQQ (S1234)","owner_faction_id":1,"observed_at":"2026-10-02 10:00"}`)
	posts, err := loadTradePosts(db, 1, nil)
	if err != nil || posts[0].Owner == nil || posts[0].Owner.Label != "QQQ (S1234)" || posts[0].Owner.FactionID == nil {
		t.Errorf("latest owner: %+v %v", posts[0].Owner, err)
	}
	csv := "Post,Owner tag,Owner server,Observed at\n170,ABC,1701,2026-10-03 10:00\nTrade Post 171,NEW,4321,2026-10-03 10:00\n"
	b, _ := json.Marshal(map[string]any{"season_id": 1, "kind": "trade_post_observations", "csv": csv})
	rr = httptest.NewRecorder()
	handleSeasonImportConfirm(rr, shReq(http.MethodPost, string(b), nil, nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"written":2`) {
		t.Fatalf("import: %d %s", rr.Code, rr.Body.String())
	}
	posts, _ = loadTradePosts(db, 1, nil)
	if posts[0].Owner.SeasonAllianceID == nil || *posts[0].Owner.SeasonAllianceID != 1 || posts[1].Owner.Label != "NEW (S4321)" {
		t.Errorf("after import: %+v / %+v", posts[0].Owner, posts[1].Owner)
	}
	// A post with owners is kept.
	rr = httptest.NewRecorder()
	handleTradePostDelete(rr, shReq(http.MethodDelete, "", map[string]string{"id": strconv.Itoa(take)}, nil))
	if rr.Code != http.StatusConflict {
		t.Errorf("deleting a post with owners: %d", rr.Code)
	}

	// The phone names a post by key and its owner by tag and server.
	rr = httptest.NewRecorder()
	postMobileSeasonWarObservations(rr, shReq(http.MethodPost, `{"trade_posts":[{"post_key":"171","owner_tag":"ABC","owner_server":1701},{"post_key":"999","owner_tag":"ABC","owner_server":1701}]}`, nil, nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"written":1`) || !strings.Contains(rr.Body.String(), "no Trade Post") {
		t.Errorf("mobile trade posts: %d %s", rr.Code, rr.Body.String())
	}
}
