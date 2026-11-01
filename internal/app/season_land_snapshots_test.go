package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func readHedgeFixture(t *testing.T, n int) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("internal", "app", "testdata", "cpt-hedge", "export-"+strconv.Itoa(n)+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestHedgeExportsValidate(t *testing.T) {
	for n := 0; n <= 5; n++ {
		sum := readHedgeExport(readHedgeFixture(t, n), 4)
		if !sum.Valid {
			t.Errorf("export-%d: %v", n, sum.Problems)
		}
		if n != 5 && len(sum.Paths) != 0 {
			t.Errorf("export-%d has %d paths, want none", n, len(sum.Paths))
		}
	}
	sum := readHedgeExport(readHedgeFixture(t, 5), 4)
	if len(sum.Paths) != 1 || len(sum.Paths[0].Steps) != 4 {
		t.Fatalf("export-5 paths: %+v", sum.Paths)
	}
	var ids []string
	for _, st := range sum.Paths[0].Steps {
		ids = append(ids, st.AreaID)
		if !st.Known || st.Name == "" || st.Level == nil {
			t.Errorf("step %d not resolved: %+v", st.Step, st)
		}
	}
	if got := ids[0] + ">" + ids[1] + ">" + ids[2] + ">" + ids[3]; got != "86>100>87>101" {
		t.Errorf("path order %s", got)
	}

	// Another season stores without resolving; a bad file is reported, not refused.
	if other := readHedgeExport(readHedgeFixture(t, 5), 5); !other.Valid || other.Paths[0].Steps[0].Known {
		t.Errorf("season 5: %+v", other)
	}
	bad := readHedgeExport([]byte(`{"version":"1.0","exportDate":"x","grids":[{"id":"g","name":"G","alliances":[{"id":"a","name":"A","areaIds":["9999"]}],
		"pathSteps":[{"areaId":"86","allianceId":"a","step":1},{"areaId":"87","allianceId":"a","step":3}]}]}`), 4)
	if bad.Valid || len(bad.Paths[0].Problems) == 0 {
		t.Errorf("an unknown area and a gap: %+v", bad)
	}
	if junk := readHedgeExport([]byte("not json"), 4); junk.Valid || len(junk.Problems) == 0 {
		t.Error("junk validated")
	}
}

func TestLandSnapshotImportPath(t *testing.T) {
	setupSettingsTestDB(t)
	dir := t.TempDir()
	t.Setenv("STORAGE_PATH", dir)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	seedSeasonRow(t, 1, 4, "2030-11-04", true, false)
	if err := os.WriteFile(filepath.Join(dir, "plan.json"), readHedgeFixture(t, 5), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{"version":"2.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mustExecT(t, `INSERT INTO files (id, title, file_name, file_type, min_rank, min_edit_rank, owner_user_id, season_id, updated_at)
		VALUES (10, 'Plan', 'plan.json', 'data', 'R1', 'R4', 1, 1, CURRENT_TIMESTAMP),
		       (11, 'Bad', 'bad.json', 'data', 'R1', 'R4', 1, 1, CURRENT_TIMESTAMP),
		       (12, 'Untagged', 'plan.json', 'data', 'R1', 'R4', 1, NULL, CURRENT_TIMESTAMP)`)
	create := func(body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		handleSeasonLandSnapshotCreate(rr, shReq(http.MethodPost, body, nil, nil))
		return rr
	}
	if rr := create(`{"season_id":1,"file_id":12}`); rr.Code != http.StatusBadRequest {
		t.Errorf("an untagged file: %d", rr.Code)
	}
	if rr := create(`{"season_id":1,"file_id":11}`); rr.Code != http.StatusOK {
		t.Errorf("a malformed export is still stored: %d %s", rr.Code, rr.Body.String())
	}
	rr := create(`{"season_id":1,"file_id":10,"slot":"backup_a"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("snapshot: %d %s", rr.Code, rr.Body.String())
	}
	var made struct {
		ID int `json:"id"`
	}
	json.Unmarshal(rr.Body.Bytes(), &made)
	var grid, exported string
	db.QueryRow(`SELECT grid_name, export_date FROM season_land_snapshots WHERE id = ?`, made.ID).Scan(&grid, &exported)
	if grid != "Season 4 - Plan" || exported == "" {
		t.Errorf("snapshot read %q %q", grid, exported)
	}

	imp := func() map[string]any {
		t.Helper()
		rr := httptest.NewRecorder()
		handleSeasonLandSnapshotImport(rr, shReq(http.MethodPost, `{"grid_id":"s4-1792294315994","alliance_id":"1792294322600","slot":"backup_a"}`,
			map[string]string{"id": strconv.Itoa(made.ID)}, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("import: %d %s", rr.Code, rr.Body.String())
		}
		var out map[string]any
		json.Unmarshal(rr.Body.Bytes(), &out)
		return out
	}
	imp()
	rows, _ := db.Query(`SELECT t.area_id, t.name, t.level, t.status, t.connected FROM season_land_targets t
		JOIN season_land_routes r ON r.id = t.route_id WHERE r.slot = 'backup_a' ORDER BY t.position`)
	var got []string
	for rows.Next() {
		var area, name, status, connected string
		var level *int
		rows.Scan(&area, &name, &level, &status, &connected)
		if name == "" || level == nil || status != "planned" || connected != "unknown" {
			t.Errorf("target %s: %q %v %s %s", area, name, level, status, connected)
		}
		got = append(got, area)
	}
	rows.Close()
	if len(got) != 4 || got[0] != "86" || got[3] != "101" {
		t.Errorf("imported targets %v", got)
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_land_route_revisions`); n != 1 {
		t.Errorf("%d revisions after the import", n)
	}
	// A repeat appends again and says so.
	again := imp()
	if len(again["already_on_route"].([]any)) != 4 || countT(t, `SELECT COUNT(*) FROM season_land_targets`) != 8 {
		t.Errorf("repeat import: %v", again)
	}

	// The tab's read lists both snapshots, the malformed one with its problems.
	req := shReq(http.MethodGet, "", nil, nil)
	req.URL.RawQuery = "season_id=1"
	rr = httptest.NewRecorder()
	handleSeasonLandGet(rr, req)
	var out struct {
		Snapshots []landSnapshot `json:"snapshots"`
	}
	json.Unmarshal(rr.Body.Bytes(), &out)
	if len(out.Snapshots) != 2 || out.Snapshots[1].Summary.Valid || !out.Snapshots[0].Summary.Valid {
		t.Errorf("snapshots read: %+v", out.Snapshots)
	}
}
