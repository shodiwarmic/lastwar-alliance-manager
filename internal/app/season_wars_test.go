package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWarRecord(t *testing.T) {
	rows := []warRecordRow{
		{"completed", "attack", "win", "2030-11-07"},
		{"completed", "attack", "loss", "2030-11-14"},
		{"completed", "defense", "win", "2030-11-07"},
		{"completed", "defense", "draw", "2030-11-14"},
		{"completed", "attack", "win", "2030-11-28"}, // after today: not counted
		{"declared", "attack", "", "2030-11-21"},
	}
	r := computeWarRecord(rows, "2030-11-20")
	if r.Overall.Wins != 2 || r.Overall.Losses != 1 || r.Overall.Draws != 1 || *r.Overall.Pct < 66.6 || *r.Overall.Pct > 66.7 {
		t.Errorf("overall %+v", r.Overall)
	}
	if r.Attack.Wins != 1 || r.Attack.Losses != 1 || r.Defense.Wins != 1 || r.Defense.Draws != 1 || *r.Defense.Pct != 100 {
		t.Errorf("attack %+v defense %+v", r.Attack, r.Defense)
	}

	one, two := 1, 2
	fr := computeFactionRecords([]factionBattleRow{
		{&one, 1, "win"}, {&one, 2, "loss"}, {&two, 1, "loss"}, {nil, 1, "win"},
	}, []int{1, 2})
	if len(fr) != 3 || fr[0].Season.Wins != 1 || fr[0].Season.Losses != 1 || len(fr[0].Weeks) != 2 ||
		fr[1].Season.Losses != 1 || fr[2].FactionID != nil || fr[2].Season.Wins != 1 {
		t.Errorf("faction records %+v", fr)
	}
}

// seedWarCalendar is a season with an opponent and a Copper War type set as its war type.
func seedWarCalendar(t *testing.T) int {
	t.Helper()
	seedWarSeason(t)
	res, err := db.Exec(`INSERT INTO schedule_event_types (name, short_name) VALUES ('Copper War', 'CW')`)
	if err != nil {
		t.Fatal(err)
	}
	cw, _ := res.LastInsertId()
	mustExecT(t, `INSERT INTO season_war_config (season_id, war_event_type_id) VALUES (1, ?)`, cw)
	return int(cw)
}

func warReq(t *testing.T, method string, id int, body string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	switch {
	case method == http.MethodPost:
		handleSeasonWarCreate(rr, shReq(method, body, nil, nil))
	case strings.HasSuffix(body, "#review"):
		handleSeasonWarReview(rr, shReq(method, strings.TrimSuffix(body, "#review"), map[string]string{"id": strconv.Itoa(id)}, nil))
	case method == http.MethodDelete:
		handleSeasonWarDelete(rr, shReq(method, "", map[string]string{"id": strconv.Itoa(id)}, nil))
	default:
		handleSeasonWarUpdate(rr, shReq(method, body, map[string]string{"id": strconv.Itoa(id)}, nil))
	}
	return rr
}

func TestWarLifecycleAndCalendar(t *testing.T) {
	cw := seedWarCalendar(t)
	calendar := func() (n int, date, tm, status string) {
		db.QueryRow(`SELECT COUNT(*) FROM schedule_events WHERE event_type_id = ?`, cw).Scan(&n)
		db.QueryRow(`SELECT event_date, event_time, status FROM schedule_events WHERE source_kind = 'war'`).Scan(&date, &tm, &status)
		return
	}
	rr := warReq(t, http.MethodPost, 0, `{"season_id":1,"role":"attack","opponent_season_alliance_id":1,"battle_date":"2026-10-01","battle_time":"14:00"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var made struct {
		ID int `json:"id"`
	}
	json.Unmarshal(rr.Body.Bytes(), &made)
	if n, _, _, _ := calendar(); n != 0 {
		t.Errorf("a war being considered is on the calendar")
	}
	// considering → declared skips planned: refused.
	if rr := warReq(t, http.MethodPut, made.ID, `{"status":"declared"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("considering → declared: %d", rr.Code)
	}
	warReq(t, http.MethodPut, made.ID, `{"status":"planned"}`)
	if n, d, tm, st := calendar(); n != 1 || d != "2026-10-01" || tm != "14:00" || st != "tentative" {
		t.Errorf("planned: %d %s %s %s", n, d, tm, st)
	}
	warReq(t, http.MethodPut, made.ID, `{"status":"declared","battle_time":"15:00"}`)
	if _, _, tm, st := calendar(); tm != "15:00" || st != "confirmed" {
		t.Errorf("declared and moved: %s %s", tm, st)
	}
	if rr := warReq(t, http.MethodPut, made.ID, `{"status":"completed"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("completed without an outcome: %d", rr.Code)
	}
	if rr := warReq(t, http.MethodPut, made.ID, `{"status":"completed","outcome":"win","participant_count":40}`); rr.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", rr.Code, rr.Body.String())
	}
	if _, _, _, st := calendar(); st != "completed" {
		t.Errorf("completed row: %s", st)
	}
	// The review never touches the plan.
	warReq(t, http.MethodPut, made.ID, `{"key_objective":"Hold the Capitol"}`)
	warReq(t, http.MethodPut, made.ID, `{"review_worked":"rally timing"}#review`)
	var obj, worked string
	db.QueryRow(`SELECT key_objective, review_worked FROM season_wars WHERE id = ?`, made.ID).Scan(&obj, &worked)
	if obj != "Hold the Capitol" || worked != "rally timing" {
		t.Errorf("plan %q review %q", obj, worked)
	}

	// A future war cannot be completed; a cancelled one cancels its row.
	rr = warReq(t, http.MethodPost, 0, `{"season_id":1,"role":"defense","direction":"incoming","battle_date":"2099-01-01","status":"confirmed"}`)
	json.Unmarshal(rr.Body.Bytes(), &made)
	if rr := warReq(t, http.MethodPut, made.ID, `{"status":"completed","outcome":"loss"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("completing a future war: %d", rr.Code)
	}
	warReq(t, http.MethodPut, made.ID, `{"status":"cancelled"}`)
	if n := countT(t, `SELECT COUNT(*) FROM schedule_events WHERE source_kind = 'war' AND source_id = ? AND status = 'cancelled'`, made.ID); n != 1 {
		t.Error("the cancelled war's row is not cancelled")
	}

	// No war type configured: the war saves and the answer says nothing was scheduled.
	mustExecT(t, `UPDATE season_war_config SET war_event_type_id = NULL`)
	rr = warReq(t, http.MethodPost, 0, `{"season_id":1,"role":"attack","battle_date":"2026-10-08","status":"planned"}`)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "no war event type configured") {
		t.Errorf("no type: %d %s", rr.Code, rr.Body.String())
	}
}

func TestWarAdoptsPushedCopperWar(t *testing.T) {
	cw := seedWarCalendar(t)
	// Push first: the template's Copper War in week 1.
	mustExecT(t, `INSERT INTO season_events (id, season_id, label, event_type_id, type_name, day_offset, event_time, week_start, week_end)
		VALUES (50, 1, 'Copper War', ?, 'Copper War', 3, '13:00', 1, 1)`, cw)
	s, _ := loadSeasonByID(1)
	if _, err := pushSeasonEventsToSchedule(s, 1, "admin"); err != nil {
		t.Fatal(err)
	}
	var date string
	db.QueryRow(`SELECT event_date FROM schedule_events WHERE season_event_id = 50`).Scan(&date)
	rr := warReq(t, http.MethodPost, 0, `{"season_id":1,"role":"attack","battle_date":"`+date+`","status":"planned"}`)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"adopted"`) {
		t.Fatalf("war on the pushed date: %d %s", rr.Code, rr.Body.String())
	}
	if n := countT(t, `SELECT COUNT(*) FROM schedule_events WHERE event_type_id = ? AND source_kind = 'war' AND season_event_id = 50 AND event_time = '13:00'`, cw); n != 1 ||
		countT(t, `SELECT COUNT(*) FROM schedule_events WHERE event_type_id = ?`, cw) != 1 {
		t.Error("the war did not adopt the pushed row (one row, both origins, its time)")
	}

	// War first, then push: the push stamps the war's row (week 2, a week after the first).
	d1, _ := time.Parse("2006-01-02", date)
	date2 := d1.AddDate(0, 0, 7).Format("2006-01-02")
	rr = warReq(t, http.MethodPost, 0, `{"season_id":1,"role":"defense","battle_date":"`+date2+`","battle_time":"13:00","status":"planned"}`)
	if rr.Code != http.StatusOK {
		t.Fatal(rr.Body.String())
	}
	mustExecT(t, `UPDATE season_events SET week_end = 2 WHERE id = 50`)
	if _, err := pushSeasonEventsToSchedule(s, 1, "admin"); err != nil {
		t.Fatal(err)
	}
	if n := countT(t, `SELECT COUNT(*) FROM schedule_events WHERE event_date = ? AND event_type_id = ?`, date2, cw); n != 1 ||
		countT(t, `SELECT COUNT(*) FROM schedule_events WHERE event_date = ? AND source_kind = 'war' AND season_event_id = 50`, date2) != 1 {
		t.Error("the push did not stamp the war's row")
	}
}

func TestFactionBattlesAndMobileResults(t *testing.T) {
	seedWarSeason(t)
	mustExecT(t, `INSERT INTO season_alliances (id, season_id, external_alliance_id, relationship, faction_id) VALUES (2, 1, 7, 'allied', 1)`)
	put := func(body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		handleSeasonFactionBattlesPut(rr, shReq(http.MethodPut, body, nil, nil))
		return rr
	}
	if rr := put(`{"season_id":1,"battles":[{"battle_date":"2030-11-07","alliance_season_alliance_id":2,"opponent_season_alliance_id":1,"role":"attack","outcome":"win"},
		{"battle_date":"2030-11-07","alliance_season_alliance_id":1,"opponent_label":"Somebody","role":"defense","outcome":"loss"}]}`); rr.Code != http.StatusOK {
		t.Fatalf("battles: %d %s", rr.Code, rr.Body.String())
	}
	// The same battle twice: refused.
	if rr := put(`{"season_id":1,"battles":[{"battle_date":"2030-11-07","alliance_season_alliance_id":2,"role":"attack","outcome":"loss"}]}`); rr.Code != http.StatusConflict {
		t.Errorf("duplicate battle: %d", rr.Code)
	}
	req := shReq(http.MethodGet, "", nil, nil)
	req.URL.RawQuery = "season_id=1"
	rr := httptest.NewRecorder()
	handleSeasonWarGet(rr, req)
	var out struct {
		FactionsWL []factionRecord `json:"factions_wl"`
	}
	json.Unmarshal(rr.Body.Bytes(), &out)
	// One alliance in faction 1 (the win); the other has no faction (the loss).
	if len(out.FactionsWL) != 2 || out.FactionsWL[0].Season.Wins != 1 || out.FactionsWL[1].FactionID != nil || out.FactionsWL[1].Season.Losses != 1 {
		t.Errorf("faction W/L: %+v", out.FactionsWL)
	}

	// Mobile results by (battle date, role).
	mustExecT(t, `INSERT INTO season_wars (id, season_id, role, battle_date, status) VALUES (9, 1, 'attack', '2026-10-01', 'declared')`)
	rr = httptest.NewRecorder()
	postMobileSeasonWarResults(rr, shReq(http.MethodPost, `{"wars":[{"battle_date":"2026-10-01","role":"attack","outcome":"win","participant_count":38},
		{"battle_date":"2026-10-02","role":"attack","outcome":"win"}],
		"faction_battles":[{"battle_date":"2030-11-08","alliance_tag":"XYZ","alliance_server":1701,"opponent_tag":"ABC","opponent_server":1701,"role":"attack","outcome":"win"}]}`, nil, nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"written":2`) {
		t.Fatalf("mobile results: %d %s", rr.Code, rr.Body.String())
	}
	var outcome, status string
	var n int
	db.QueryRow(`SELECT outcome, status, participant_count FROM season_wars WHERE id = 9`).Scan(&outcome, &status, &n)
	if outcome != "win" || status != "declared" || n != 38 {
		t.Errorf("war 9: %s %s %d (the status stays the officers')", outcome, status, n)
	}
}
