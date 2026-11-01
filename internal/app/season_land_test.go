package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"lastwar-alliance/internal/gametime"
)

func stTime(t *testing.T, s string) *time.Time {
	t.Helper()
	v, err := parseLandST(s)
	if err != nil {
		t.Fatal(err)
	}
	return &v
}

func TestLandUnlockDerivation(t *testing.T) {
	got, err := landUnlockAt("2030-11-04", 1, 3, 12)
	if err != nil {
		t.Fatal(err)
	}
	if s := landST(&got); s != "2030-11-06 12:00" {
		t.Errorf("W1 d3 +12h = %s ST, want 2030-11-06 12:00", s)
	}
	if got.UTC().Hour() != 14 {
		t.Errorf("stored as %v UTC, want 14:00 (server time is UTC−2)", got.UTC())
	}
}

func TestLandReadiness(t *testing.T) {
	now := *stTime(t, "2030-11-10 12:00")
	unlockFar := stTime(t, "2030-11-13 12:00")  // three days out, buffer 24 h → deadline 11-12 12:00
	unlockNear := stTime(t, "2030-11-10 20:00") // inside the buffer
	unlockPast := stTime(t, "2030-11-10 06:00")
	early := stTime(t, "2030-11-11 15:00")
	late := stTime(t, "2030-11-12 18:00")
	pre := func(status string, planned *time.Time) landTarget {
		return landTarget{ID: 1, Name: "Cottage", Status: status, PlannedCaptureAt: planned, Connected: "touching"}
	}
	next := func(unlock *time.Time, connected string) landTarget {
		p := 1
		return landTarget{ID: 2, Name: "Hot Spring Town", Status: "planned", UnlockAt: unlock, Connected: connected, PrerequisiteTargetID: &p}
	}
	// A target's connector usually comes first on the route; here it sits after the target so
	// the target is the one the rule judges and its connector's plan is what it reads.
	cases := []struct {
		name    string
		targets []landTarget
		want    string
	}{
		{"all captured", []landTarget{{ID: 1, Status: "captured"}, {ID: 2, Status: "skipped"}}, "completed"},
		{"unlock unknown", []landTarget{pre("captured", nil), next(nil, "touching")}, "unknown"},
		{"connected unknown", []landTarget{pre("captured", nil), next(unlockFar, "unknown")}, "unknown"},
		{"unlock passed", []landTarget{pre("captured", nil), next(unlockPast, "touching")}, "behind"},
		{"touching", []landTarget{pre("captured", nil), next(unlockNear, "touching")}, "on_track"},
		{"connector planned in time", []landTarget{next(unlockFar, "not_touching"), pre("planned", early)}, "on_track"},
		{"connector planned too late", []landTarget{next(unlockFar, "not_touching"), pre("planned", late)}, "at_risk"},
		{"no connector plan", []landTarget{next(unlockFar, "not_touching"), pre("planned", nil)}, "at_risk"},
		{"inside the buffer, connector not taken", []landTarget{next(unlockNear, "not_touching"), pre("planned", early)}, "at_risk"},
		{"connector captured", []landTarget{pre("captured", nil), next(unlockFar, "not_touching")}, "on_track"},
	}
	for _, c := range cases {
		got := computeLandReadiness(9, "Primary", "primary", c.targets, 24, now)
		if got.Status != c.want {
			t.Errorf("%s: status %s, want %s (%v)", c.name, got.Status, c.want, got.Reasons)
		}
		if got.Status != "completed" && len(got.Reasons) == 0 {
			t.Errorf("%s: no reasons", c.name)
		}
	}
	if got := computeLandReadiness(0, "", "", nil, 24, now); got.Status != "none" {
		t.Errorf("no active route: %s", got.Status)
	}
}

func TestLandRoutesSaveActivateAndRevise(t *testing.T) {
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	seedSeasonRow(t, 1, 4, "2030-11-04", true, false)
	put := func(body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		handleSeasonLandRoutePut(rr, shReq(http.MethodPut, body, nil, nil))
		return rr
	}
	// The four-step proposal by map ID: names, kinds and levels come from the S4 lookup.
	rr := put(`{"season_id":1,"slot":"primary","targets":[{"area_id":"86"},{"area_id":"100"},{"area_id":"87"},{"area_id":"101"}]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rr.Code, rr.Body.String())
	}
	var named int
	db.QueryRow(`SELECT COUNT(*) FROM season_land_targets WHERE name != '' AND level IS NOT NULL`).Scan(&named)
	if named != 4 {
		t.Errorf("%d targets named from the lookup, want 4", named)
	}
	put(`{"season_id":1,"slot":"backup_a","targets":[{"name":"Cottage","kind":"city"}]}`)
	if n := countT(t, `SELECT COUNT(*) FROM season_land_routes WHERE is_active = 1`); n != 1 {
		t.Fatalf("%d active routes after two saves, want the first only", n)
	}
	var backup int
	db.QueryRow(`SELECT id FROM season_land_routes WHERE slot = 'backup_a'`).Scan(&backup)
	rr = httptest.NewRecorder()
	handleSeasonLandRouteActivate(rr, shReq(http.MethodPut, "", map[string]string{"id": strconv.Itoa(backup)}, nil))
	if n := countT(t, `SELECT COUNT(*) FROM season_land_routes WHERE is_active = 1 AND slot = 'backup_a'`); n != 1 ||
		countT(t, `SELECT COUNT(*) FROM season_land_routes WHERE is_active = 1`) != 1 {
		t.Error("activate did not flip exactly one route")
	}

	// A resave keeps target ids, reorders, drops the one left out, and appends a revision.
	var ids []int
	rows, _ := db.Query(`SELECT t.id FROM season_land_targets t JOIN season_land_routes r ON r.id = t.route_id
		WHERE r.slot = 'primary' ORDER BY t.position`)
	for rows.Next() {
		var id int
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	body, _ := json.Marshal(map[string]any{"season_id": 1, "slot": "primary", "targets": []map[string]any{
		{"id": ids[1], "name": "Second", "kind": "city", "planned_capture_at": "2030-11-06 15:00", "connected": "touching"},
		{"id": ids[0], "name": "First", "kind": "city"},
	}})
	if rr := put(string(body)); rr.Code != http.StatusOK {
		t.Fatalf("resave: %d %s", rr.Code, rr.Body.String())
	}
	var first int
	var planned string
	db.QueryRow(`SELECT id, planned_capture_at FROM season_land_targets WHERE position = 0 AND name = 'Second'`).Scan(&first, &planned)
	if first != ids[1] {
		t.Errorf("target id not kept across a save")
	}
	if planned != "2030-11-06 17:00:00" {
		t.Errorf("planned capture stored as %q, want UTC 2030-11-06 17:00:00", planned)
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_land_targets t JOIN season_land_routes r ON r.id = t.route_id WHERE r.slot = 'primary'`); n != 2 {
		t.Errorf("%d targets after the resave, want 2", n)
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_land_route_revisions v JOIN season_land_routes r ON r.id = v.route_id WHERE r.slot = 'primary'`); n != 2 {
		t.Errorf("%d revisions, want one per save", n)
	}

	// The tab's read carries readiness for the active route in server time.
	req := shReq(http.MethodGet, "", nil, nil)
	req.URL.RawQuery = "season_id=1"
	rr = httptest.NewRecorder()
	handleSeasonLandGet(rr, req)
	var out struct {
		Readiness landReadiness `json:"readiness"`
		HasLookup bool          `json:"has_lookup"`
	}
	json.Unmarshal(rr.Body.Bytes(), &out)
	if out.Readiness.Slot != "backup_a" || !out.HasLookup {
		t.Errorf("land read = %+v", out)
	}

	// Nothing moves on an archived season.
	mustExecT(t, `UPDATE seasons SET archived_at = CURRENT_TIMESTAMP, is_active = 0 WHERE id = 1`)
	if rr := put(`{"season_id":1,"slot":"primary","targets":[]}`); rr.Code != http.StatusConflict {
		t.Errorf("archived save: %d", rr.Code)
	}
	_ = gametime.Loc
}
