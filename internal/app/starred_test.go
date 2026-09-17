package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// starredFixture is the 64-server regression baseline from issue #120, captured
// live on 2026-09-13 and stored verbatim.
//
// The letters in it are the game's, from the monthly image. They are NOT what the
// app stores — the letters rotate month to month, so the app labels groups 1/2/3
// by residue. The fixture is therefore used as an EQUIVALENCE RELATION: servers
// sharing a letter must share a group, and servers with different letters must
// not. That is the claim the derivation actually makes, and it survives the
// letters rotating; asserting a letter → number mapping would pin the test to one
// month and fail for a reason that means nothing.
type starredFixture struct {
	SectorStart int `json:"sector_start"`
	SectorEnd   int `json:"sector_end"`
	Servers     map[string]struct {
		OpenTime     string `json:"open_time"`
		GameOpenDate string `json:"game_open_date"`
		Group        string `json:"group"`
	} `json:"servers"`
}

func loadStarredFixture(t *testing.T) starredFixture {
	t.Helper()
	raw, err := os.ReadFile("internal/app/testdata/starred_sector_1701_1764.json")
	if err != nil {
		raw, err = os.ReadFile("testdata/starred_sector_1701_1764.json")
	}
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var f starredFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(f.Servers) != 64 {
		t.Fatalf("fixture has %d servers, want 64", len(f.Servers))
	}
	return f
}

// The whole feature in one assertion: from the recorded open_time values alone,
// the derivation must reproduce the grouping the game published.
func TestStarredFixtureReproducesThePublishedGroups(t *testing.T) {
	f := loadStarredFixture(t)

	letterToGroup := map[string]int{}
	agreements, disagreements := 0, 0
	for id, row := range f.Servers {
		date, err := gameDateOf(row.OpenTime)
		if err != nil {
			t.Fatalf("server %s: gameDateOf(%q): %v", id, row.OpenTime, err)
		}
		if date != row.GameOpenDate {
			t.Errorf("server %s: game date %s, fixture says %s", id, date, row.GameOpenDate)
		}
		group, err := starredGroup(date)
		if err != nil {
			t.Fatalf("server %s: starredGroup(%q): %v", id, date, err)
		}
		if seen, ok := letterToGroup[row.Group]; ok {
			if seen == group {
				agreements++
			} else {
				disagreements++
				t.Errorf("server %s is group %s in the game but derives group %d, where %s derived %d elsewhere",
					id, row.Group, group, row.Group, seen)
			}
			continue
		}
		// First sighting of this letter. Two different letters mapping onto the
		// same group would mean the three-day cycle had collapsed.
		for letter, g := range letterToGroup {
			if g == group {
				t.Errorf("server %s (%s) derives group %d, already taken by %s", id, row.Group, group, letter)
			}
		}
		letterToGroup[row.Group] = group
	}
	if len(letterToGroup) != 3 {
		t.Errorf("the fixture covers %d groups, want 3", len(letterToGroup))
	}
	if disagreements != 0 {
		t.Errorf("%d servers disagree with the published grouping (%d agree)", disagreements, agreements)
	}
}

// The UTC−2 conversion is the one step that can silently move a server into the
// wrong group, so it is pinned on the servers the issue measured it on: those
// opening in the small hours land on the previous game day.
func TestGameDateConversionMovesSmallHoursServers(t *testing.T) {
	f := loadStarredFixture(t)
	moved := 0
	for id, row := range f.Servers {
		date, err := gameDateOf(row.OpenTime)
		if err != nil {
			t.Fatalf("server %s: %v", id, err)
		}
		// The raw timestamp's own calendar date, before the conversion.
		if row.OpenTime[:10] != date {
			moved++
		}
	}
	// The issue measured roughly one in nine; asserting "some move" is the real
	// claim. A zero here means the conversion is not happening at all, which is
	// exactly the bug that would otherwise pass every other test in this file.
	if moved == 0 {
		t.Error("no server changed date under the UTC−2 conversion — the conversion is not being applied")
	}
}

// The day → group cycle is a residue of three, and it never skips or repeats.
func TestDayGroupIsTheResidue(t *testing.T) {
	seen := map[int]bool{}
	for _, date := range []string{"2026-09-14", "2026-09-15", "2026-09-16"} {
		g, err := starredGroup(date)
		if err != nil {
			t.Fatalf("%s: %v", date, err)
		}
		if g < 1 || g > 3 {
			t.Fatalf("%s: group %d out of range", date, g)
		}
		if seen[g] {
			t.Errorf("%s repeats group %d inside one cycle of three", date, g)
		}
		seen[g] = true
	}
	// Four days on, the cycle has come back round.
	a, _ := starredGroup("2026-09-14")
	b, _ := starredGroup("2026-09-17")
	if a != b {
		t.Errorf("three days apart should be the same group, got %d and %d", a, b)
	}

	// A date before the epoch must not produce 0 or a negative group: Go's %
	// takes the sign of the dividend, which is what the Euclidean form guards.
	for _, date := range []string{"2023-12-31", "2020-01-01", "1999-06-15"} {
		g, err := starredGroup(date)
		if err != nil {
			t.Fatalf("%s: %v", date, err)
		}
		if g < 1 || g > 3 {
			t.Errorf("%s (before the epoch) derives group %d, want 1–3", date, g)
		}
	}
}

// Out-of-sector servers fail CLOSED. A stored open date for a server outside the
// configured sector must never reach a group list — deriving a group for a server
// we may not plunder is a confident answer to the wrong question.
func TestOutOfSectorServersAreNeverReported(t *testing.T) {
	setupSettingsTestDB(t)
	setSector(t, 1701, 1710)

	for _, id := range []int{1699, 1700, 1705, 1711, 1764} {
		if _, err := db.Exec(`INSERT INTO server_open_dates (server_id, open_date, source)
			VALUES (?, '2025-07-13', 'lastrank')`, id); err != nil {
			t.Fatalf("seed %d: %v", id, err)
		}
	}

	body := getStarred(t, "")
	groups, _ := body["groups"].(map[string]any)
	found := map[int]bool{}
	for _, list := range groups {
		for _, v := range list.([]any) {
			found[int(v.(float64))] = true
		}
	}
	if !found[1705] {
		t.Error("1705 is inside the sector and was not reported")
	}
	for _, id := range []int{1699, 1700, 1711, 1764} {
		if found[id] {
			t.Errorf("%d is outside the sector 1701–1710 and was reported anyway", id)
		}
	}
}

// The sweep only ever plans servers with NO row. That is what makes a manual
// correction permanent, and what makes a cancelled run resume by being re-run.
func TestSweepPlansOnlyMissingRows(t *testing.T) {
	setupSettingsTestDB(t)
	setSector(t, 1701, 1706)

	seed := map[int]string{1701: "lastrank", 1703: "manual"}
	for id, src := range seed {
		if _, err := db.Exec(`INSERT INTO server_open_dates (server_id, open_date, source)
			VALUES (?, '2025-07-13', ?)`, id, src); err != nil {
			t.Fatalf("seed %d: %v", id, err)
		}
	}

	job := &starredOpenDatesJob{actor: jobActor{UserID: 1, Username: "t"}}
	items, err := job.Plan(t.Context())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	planned := map[int]bool{}
	for _, it := range items {
		planned[it.RefID] = true
	}
	for id := range seed {
		if planned[id] {
			t.Errorf("server %d already has a row (%s) and was planned for a re-fetch", id, seed[id])
		}
	}
	for _, id := range []int{1702, 1704, 1705, 1706} {
		if !planned[id] {
			t.Errorf("server %d has no row and was not planned", id)
		}
	}
	if len(items) != 4 {
		t.Errorf("planned %d items, want 4", len(items))
	}

	// And the resume property, stated directly: once the missing ones are filled
	// in, a re-run plans nothing at all.
	for _, id := range []int{1702, 1704, 1705, 1706} {
		db.Exec(`INSERT INTO server_open_dates (server_id, open_date, source) VALUES (?, '2025-07-14', 'lastrank')`, id)
	}
	again, err := job.Plan(t.Context())
	if err != nil {
		t.Fatalf("Plan (second): %v", err)
	}
	if len(again) != 0 {
		t.Errorf("a complete sector still planned %d items; a re-run must cost nothing", len(again))
	}
}

// A manual edit wins over the sweep — the issue's decision 1. The job's write is
// guarded so that even if a manual server were somehow planned, the row survives.
func TestManualEditWinsOverTheSweep(t *testing.T) {
	setupSettingsTestDB(t)
	setSector(t, 1701, 1710)

	if _, err := db.Exec(`INSERT INTO server_open_dates (server_id, open_date, source)
		VALUES (1705, '2025-01-01', 'manual')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// The sweep's own UPSERT, run directly against the manual row.
	if _, err := db.Exec(`
		INSERT INTO server_open_dates (server_id, open_date, source, fetched_at)
		VALUES (1705, '2025-07-13', 'lastrank', datetime('now'))
		ON CONFLICT(server_id) DO UPDATE SET
			open_date = excluded.open_date, source = 'lastrank', fetched_at = excluded.fetched_at
		WHERE server_open_dates.source <> 'manual'`); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	var date, source string
	db.QueryRow(`SELECT open_date, source FROM server_open_dates WHERE server_id = 1705`).Scan(&date, &source)
	if date != "2025-01-01" || source != "manual" {
		t.Errorf("the sweep overwrote a manual correction: %s / %s", date, source)
	}
}

// Half a sector is not a weaker configuration — it is one the group list cannot be
// built from at all.
func TestSettingsRejectHalfASector(t *testing.T) {
	setupSettingsTestDB(t)
	for _, tc := range []struct{ start, end int }{{1701, 0}, {0, 1764}} {
		if code := putSector(t, tc.start, tc.end); code != http.StatusBadRequest {
			t.Errorf("sector %d–%d accepted with %d", tc.start, tc.end, code)
		}
	}
	if code := putSector(t, 1764, 1701); code != http.StatusBadRequest {
		t.Error("a sector ending before it starts was accepted")
	}
	if code := putSector(t, 1701, 1764); code != http.StatusOK {
		t.Errorf("the real sector was rejected with %d", code)
	}
	if code := putSector(t, 0, 0); code != http.StatusOK {
		t.Errorf("clearing the sector was rejected with %d", code)
	}
}

// The width cap is operational: the sweep costs one paced request per server and
// holds the process's single job slot while it runs.
func TestSettingsRejectASectorWiderThan128(t *testing.T) {
	setupSettingsTestDB(t)
	if code := putSector(t, 1701, 1701+maxSectorWidth-1); code != http.StatusOK {
		t.Errorf("a sector of exactly %d was rejected with %d", maxSectorWidth, code)
	}
	if code := putSector(t, 1701, 1701+maxSectorWidth); code != http.StatusBadRequest {
		t.Errorf("a sector of %d was accepted", maxSectorWidth+1)
	}
}

// The day map is what the calendar renders from, so the residue has exactly one
// implementation rather than a second one in JavaScript.
func TestStarredScheduleHandsTheClientTheDayMap(t *testing.T) {
	setupSettingsTestDB(t)
	setSector(t, 1701, 1710)

	body := getStarred(t, "?from=2026-09-14&to=2026-09-16")
	days, ok := body["days"].(map[string]any)
	if !ok || len(days) != 3 {
		t.Fatalf("days = %v, want three entries", body["days"])
	}
	for date, v := range days {
		want, _ := starredGroup(date)
		if int(v.(float64)) != want {
			t.Errorf("%s: client told group %v, Go says %d", date, v, want)
		}
	}
}

// --- helpers ---

func setSector(t *testing.T, start, end int) {
	t.Helper()
	if _, err := db.Exec(`UPDATE settings SET sector_start = NULLIF(?,0), sector_end = NULLIF(?,0) WHERE id = 1`,
		start, end); err != nil {
		t.Fatalf("set sector: %v", err)
	}
}

// putSector drives the real settings PUT, so the validation under test is the one
// the browser hits rather than a copy of it.
func putSector(t *testing.T, start, end int) int {
	t.Helper()
	body := baseSettings()
	body["sector_start"] = start
	body["sector_end"] = end
	return putSettings(t, body).Code
}

func getStarred(t *testing.T, query string) map[string]any {
	t.Helper()
	req := scheduleTestActor(httptest.NewRequest(http.MethodGet, "/api/schedule/starred"+query, nil))
	rr := httptest.NewRecorder()
	getStarredSchedule(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("getStarredSchedule returned %d: %s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}
