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

func heldCities(n int) []landHolding {
	out := make([]landHolding, n)
	for i := range out {
		out[i] = landHolding{Name: "City " + strconv.Itoa(i), Kind: "city"}
	}
	return out
}

func plannedCities(t *testing.T, at ...string) []landTarget {
	var out []landTarget
	for i, s := range at {
		out = append(out, landTarget{ID: i + 1, Name: "Target " + strconv.Itoa(i), Kind: "city", Status: "planned", PlannedCaptureAt: stTime(t, s)})
	}
	return out
}

func TestLandDailyCards(t *testing.T) {
	cfg := defaultLandConfig(1) // caps 6, two captures a day
	now := *stTime(t, "2030-11-10 09:00")
	tomorrow := "2030-11-11"
	card := func(cards []landDayCard, date string) landKindCount {
		for _, c := range cards {
			if c.Date == date {
				return c.Cities
			}
		}
		t.Fatalf("no card for %s in %+v", date, cards)
		return landKindCount{}
	}

	// 5 held + 2 planned − 6 = 1 drop; 6 + 2 − 6 = 2.
	if got := card(landDailyCards(heldCities(5), plannedCities(t, tomorrow+" 15:00", tomorrow+" 18:00"), nil, nil, cfg, now), tomorrow); got.DropsRequired != 1 || got.Held != 5 {
		t.Errorf("5+2−6: %+v", got)
	}
	if got := card(landDailyCards(heldCities(6), plannedCities(t, tomorrow+" 15:00", tomorrow+" 18:00"), nil, nil, cfg, now), tomorrow); got.DropsRequired != 2 {
		t.Errorf("6+2−6: %+v", got)
	}

	// A drop on the same day as the captures frees exactly one slot, and the next day starts
	// from what the day left.
	drops := []landPlannedDrop{{Kind: "city", MustStartBy: *stTime(t, tomorrow+" 14:00")}}
	cards := landDailyCards(heldCities(6), plannedCities(t, tomorrow+" 15:00", tomorrow+" 18:00", "2030-11-12 15:00"), drops, nil, cfg, now)
	if got := card(cards, tomorrow); got.DropsRequired != 1 || got.DropsPlanned != 1 {
		t.Errorf("same-day drop: %+v", got)
	}
	if got := card(cards, "2030-11-12"); got.Held != 7 || got.DropsRequired != 2 {
		t.Errorf("the next day: %+v", got)
	}

	// Three captures planned in a day with a limit of two warns; so does one more on a day two
	// were already made.
	if got := card(landDailyCards(nil, plannedCities(t, tomorrow+" 01:00", tomorrow+" 02:00", tomorrow+" 03:00"), nil, nil, cfg, now), tomorrow); !got.OverDaily {
		t.Errorf("three planned with a limit of two: %+v", got)
	}
	today := plannedCities(t, "2030-11-10 20:00")
	if got := card(landDailyCards(nil, today, nil, map[string]int{"city": 2}, cfg, now), "2030-11-10"); !got.OverDaily {
		t.Errorf("two made and one planned today: %+v", got)
	}

	// The Capitol counts toward the city cap only when the season says so; holdings are derived.
	ev := []landEvent{
		{AreaID: "1", Name: "Capitol", Kind: "capitol", Action: "captured", At: *stTime(t, "2030-11-09 10:00")},
		{AreaID: "2", Name: "Cottage", Kind: "city", Action: "captured", At: *stTime(t, "2030-11-09 11:00")},
		{AreaID: "2", Name: "Cottage", Kind: "city", Action: "abandoned", At: *stTime(t, "2030-11-09 12:00")},
	}
	held := landHoldingsAt(ev, now)
	if len(held) != 1 || held[0].Name != "Capitol" {
		t.Fatalf("holdings = %+v", held)
	}
	if got := card(landDailyCards(held, nil, nil, nil, cfg, now), "2030-11-10"); got.Held != 0 {
		t.Errorf("Capitol counted without the setting: %+v", got)
	}
	cfg.CapitolCountsTowardCap = true
	if got := card(landDailyCards(held, nil, nil, nil, cfg, now), "2030-11-10"); got.Held != 1 {
		t.Errorf("Capitol not counted with the setting: %+v", got)
	}
}

func TestLandDropsFollowTheirCapture(t *testing.T) {
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	seedSeasonRow(t, 1, 4, "2030-11-04", true, false)
	rr := httptest.NewRecorder()
	handleSeasonLandRoutePut(rr, shReq(http.MethodPut,
		`{"season_id":1,"slot":"primary","targets":[{"name":"Hot Spring Town","kind":"city","planned_capture_at":"2030-11-11 15:00"}]}`, nil, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("route: %d %s", rr.Code, rr.Body.String())
	}
	var targetID int
	db.QueryRow(`SELECT id FROM season_land_targets`).Scan(&targetID)
	calendar := func(kind string) (date, tm, status string) {
		t.Helper()
		db.QueryRow(`SELECT event_date, event_time, status FROM schedule_events WHERE source_kind = ?`, kind).Scan(&date, &tm, &status)
		return
	}
	if d, tm, st := calendar(sourceLandCapture); d != "2030-11-11" || tm != "15:00" || st != "tentative" {
		t.Errorf("Land Capture row %s %s %s", d, tm, st)
	}

	rr = httptest.NewRecorder()
	handleSeasonLandDropCreate(rr, shReq(http.MethodPost, `{"season_id":1,"replacement_target_id":`+strconv.Itoa(targetID)+
		`,"holdings":[{"name":"Cottage","kind":"city"}]}`, nil, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("drop: %d %s", rr.Code, rr.Body.String())
	}
	var must string
	db.QueryRow(`SELECT must_start_by FROM season_land_drops`).Scan(&must)
	if must != "2030-11-11 16:00:00" {
		t.Errorf("must_start_by stored %q, want 14:00 ST = 16:00 UTC", must)
	}
	if d, tm, _ := calendar(sourceLandDrop); d != "2030-11-11" || tm != "14:00" {
		t.Errorf("Land Abandonment row %s %s, want 14:00", d, tm)
	}
	if n := countT(t, `SELECT COUNT(*) FROM schedule_events WHERE source_kind = ? AND leadership_only = 1`, sourceLandDrop); n != 1 {
		t.Error("the abandonment row is not leadership-only")
	}

	// Moving the capture moves the drop and both rows.
	body, _ := json.Marshal(map[string]any{"season_id": 1, "slot": "primary", "targets": []map[string]any{
		{"id": targetID, "name": "Hot Spring Town", "kind": "city", "planned_capture_at": "2030-11-11 16:00"}}})
	rr = httptest.NewRecorder()
	handleSeasonLandRoutePut(rr, shReq(http.MethodPut, string(body), nil, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("move: %d %s", rr.Code, rr.Body.String())
	}
	if _, tm, _ := calendar(sourceLandCapture); tm != "16:00" {
		t.Errorf("capture row at %s after the move", tm)
	}
	if _, tm, _ := calendar(sourceLandDrop); tm != "15:00" {
		t.Errorf("abandonment row at %s after the move, want 15:00", tm)
	}

	// The target can't be removed while the drop points at it.
	rr = httptest.NewRecorder()
	handleSeasonLandRoutePut(rr, shReq(http.MethodPut, `{"season_id":1,"slot":"primary","targets":[]}`, nil, nil))
	if rr.Code != http.StatusConflict {
		t.Errorf("removing a drop's replacement: %d", rr.Code)
	}

	// Completing the drop records the abandonment; cancelling another cancels its row.
	var dropID int
	db.QueryRow(`SELECT id FROM season_land_drops`).Scan(&dropID)
	rr = httptest.NewRecorder()
	handleSeasonLandDropUpdate(rr, shReq(http.MethodPut, `{"status":"complete","replacement_target_id":`+strconv.Itoa(targetID)+`}`,
		map[string]string{"id": strconv.Itoa(dropID)}, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", rr.Code, rr.Body.String())
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_land_events WHERE action = 'abandoned' AND name = 'Cottage'`); n != 1 {
		t.Errorf("%d abandoned events after completing the drop", n)
	}
	if _, _, st := calendar(sourceLandDrop); st != "completed" {
		t.Errorf("abandonment row %s after completion", st)
	}
	rr = httptest.NewRecorder()
	handleSeasonLandDropUpdate(rr, shReq(http.MethodPut, `{"status":"cancelled"}`, map[string]string{"id": strconv.Itoa(dropID)}, nil))
	if _, _, st := calendar(sourceLandDrop); st != "cancelled" {
		t.Errorf("abandonment row %s after cancelling", st)
	}

	// Marking the target captured stamps it and records the capture once, however often saved.
	body, _ = json.Marshal(map[string]any{"season_id": 1, "slot": "primary", "targets": []map[string]any{
		{"id": targetID, "name": "Hot Spring Town", "kind": "city", "planned_capture_at": "2030-11-11 16:00",
			"actual_capture_at": "2026-10-01 16:05", "status": "captured"}}})
	for i := 0; i < 2; i++ {
		rr = httptest.NewRecorder()
		handleSeasonLandRoutePut(rr, shReq(http.MethodPut, string(body), nil, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("captured: %d %s", rr.Code, rr.Body.String())
		}
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_land_events WHERE action = 'captured' AND name = 'Hot Spring Town'`); n != 1 {
		t.Errorf("%d captured events, want 1", n)
	}
	if _, _, st := calendar(sourceLandCapture); st != "completed" {
		t.Errorf("capture row %s after the capture", st)
	}

	// The tab's read derives holdings from the events.
	req := shReq(http.MethodGet, "", nil, nil)
	req.URL.RawQuery = "season_id=1"
	rr = httptest.NewRecorder()
	handleSeasonLandGet(rr, req)
	var out struct {
		Holdings []landHolding `json:"holdings"`
		Drops    []landDrop    `json:"drops"`
	}
	json.Unmarshal(rr.Body.Bytes(), &out)
	if len(out.Holdings) != 1 || out.Holdings[0].Name != "Hot Spring Town" || len(out.Drops) != 1 {
		t.Errorf("land read: %+v", out)
	}
}

func TestMobileLandEventsAppend(t *testing.T) {
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	seedSeasonRow(t, 1, 4, "2030-11-04", true, false)
	post := func(body string) map[string]any {
		t.Helper()
		rr := httptest.NewRecorder()
		postMobileSeasonLandEvents(rr, shReq(http.MethodPost, body, nil, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("post: %d %s", rr.Code, rr.Body.String())
		}
		var out map[string]any
		json.Unmarshal(rr.Body.Bytes(), &out)
		return out
	}
	at := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	body := `{"captured_at":"` + at + `","events":[{"area_id":"86","action":"captured"},{"name":"Nowhere","action":"razed"}]}`
	out := post(body)
	if out["written"] != float64(1) {
		t.Fatalf("first post: %v", out)
	}
	results := out["results"].([]any)
	if results[1].(map[string]any)["status"] != "refused" {
		t.Errorf("an invalid action was not refused: %v", results)
	}
	var name, source string
	db.QueryRow(`SELECT name, source FROM season_land_events`).Scan(&name, &source)
	if name == "" || source != "mobile" {
		t.Errorf("event named %q from %q, want the lookup's name and mobile", name, source)
	}
	out = post(body)
	if out["written"] != float64(0) || out["results"].([]any)[0].(map[string]any)["status"] != "unchanged" {
		t.Errorf("a repeat was not reported unchanged: %v", out)
	}

	rr := httptest.NewRecorder()
	getMobileSeasonLand(rr, shReq(http.MethodGet, "", nil, nil))
	var got struct {
		Holdings []landHolding `json:"holdings"`
	}
	json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got.Holdings) != 1 {
		t.Errorf("mobile holdings: %s", rr.Body.String())
	}
}

func TestSeasonImportLandEvents(t *testing.T) {
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	seedSeasonRow(t, 1, 4, "2030-11-04", true, false)
	call := func(h http.HandlerFunc, csv string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]any{"season_id": 1, "kind": "land_events", "csv": csv})
		rr := httptest.NewRecorder()
		h(rr, shReq(http.MethodPost, string(b), nil, nil))
		return rr
	}
	// No Action column: refused before any row is read.
	rr := call(handleSeasonImportPreview, "Name,At\nCottage,2026-10-01 12:00\n")
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "Action") {
		t.Errorf("missing Action: %d %s", rr.Code, rr.Body.String())
	}
	csv := "Name,Kind,Action,At\nCottage,city,captured,2026-10-01 12:00\nShrine,city,razed,2026-10-01 12:00\n"
	rr = call(handleSeasonImportPreview, csv)
	var prev struct {
		Valid   int `json:"valid"`
		Invalid int `json:"invalid"`
	}
	json.Unmarshal(rr.Body.Bytes(), &prev)
	if prev.Valid != 1 || prev.Invalid != 1 {
		t.Errorf("preview: %s", rr.Body.String())
	}
	if rr := call(handleSeasonImportConfirm, csv); rr.Code != http.StatusBadRequest {
		t.Errorf("confirm with an invalid row: %d", rr.Code)
	}
	csv = "Name,Kind,Action,At\nCottage,city,captured,2026-10-01 12:00\n"
	if rr := call(handleSeasonImportConfirm, csv); rr.Code != http.StatusOK {
		t.Fatalf("confirm: %d %s", rr.Code, rr.Body.String())
	}
	if rr := call(handleSeasonImportConfirm, csv); !strings.Contains(rr.Body.String(), `"unchanged":1`) {
		t.Errorf("a repeat import: %s", rr.Body.String())
	}

	// A kind's permission is checked: land_events needs manage_season_land.
	b, _ := json.Marshal(map[string]any{"season_id": 1, "kind": "land_events", "csv": csv})
	rr = httptest.NewRecorder()
	handleSeasonImportPreview(rr, shReq(http.MethodPost, string(b), nil, &AuthUser{ID: 1, Username: "r1", Rank: "R1"}))
	if rr.Code != http.StatusForbidden {
		t.Errorf("an R1 preview: %d", rr.Code)
	}
}
