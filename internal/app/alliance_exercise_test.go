package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The Alliance Exercise slot is ONE slot running TWO events: Marshal's Guard up to
// Season 3 day 57, Large Sandworm from day 58. They were stored as one type against
// one 1..12-then-tens ceiling, which is why this alliance's live data has MG rows at
// level 70 beside MG rows at level 12 — the same column holding two scales.
//
// The cutover is computed from the seasons table. A server with no Season 3 row has
// no cutover, and that is a state, not an error.

func seedSeasonThree(t *testing.T, startDate string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO seasons (season_number, name, start_date, is_active)
		VALUES (3, 'Season 3', ?, 1)`, startDate); err != nil {
		t.Fatalf("seed season 3: %v", err)
	}
}

// ensureLSType applies migration 074's step 1 to a test DB whose schedule_events
// table is empty (so steps 2–5 are no-ops by construction).
func lsTypeID(t *testing.T) int { return typeIDByShort(t, "LS") }

func TestSandwormCutoverIsComputedFromTheSeasonsTable(t *testing.T) {
	setupSettingsTestDB(t)

	// No Season 3 row: no cutover, and ok is false rather than "" — every caller
	// guards on ok, because a Go compare against "" is true for every date.
	if date, ok, err := sandwormCutover(db); err != nil || ok || date != "" {
		t.Fatalf("with no Season 3 row: date=%q ok=%v err=%v, want \"\" false nil", date, ok, err)
	}

	seedSeasonThree(t, "2026-07-13")
	date, ok, err := sandwormCutover(db)
	if err != nil || !ok {
		t.Fatalf("with a Season 3 row: ok=%v err=%v", ok, err)
	}
	// Day 1 is the start date, so day 58 is start + 57.
	if date != "2026-09-08" {
		t.Errorf("cutover = %q, want 2026-09-08 (2026-07-13 + 57 days)", date)
	}
}

// The cadence rule spans BOTH variants — they are one slot in the game, so an MG
// on Monday blocks a Large Sandworm on Tuesday exactly as it blocks another MG.
func TestAllianceExerciseGapSpansBothVariants(t *testing.T) {
	setupSettingsTestDB(t)
	seedSeasonThree(t, "2026-07-13")
	mg, ls := mgTypeID(t), lsTypeID(t)

	// An MG the day before the cutover, then an LS on the cutover day itself.
	if rr := postEvent(t, map[string]any{
		"event_date": "2026-09-07", "event_type_id": mg, "event_time": "00:30",
	}); rr.Code != http.StatusCreated {
		t.Fatalf("seed MG: %d %s", rr.Code, rr.Body.String())
	}
	rr := postEvent(t, map[string]any{
		"event_date": "2026-09-08", "event_type_id": ls, "event_time": "00:30",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("an LS the day after an MG was accepted with %d — the gap does not span the family", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Marshal") || !strings.Contains(rr.Body.String(), "2026-09-07") {
		t.Errorf("message should name the conflicting event and its date; got %q", rr.Body.String())
	}

	// And the other direction: an MG the day BEFORE an existing LS.
	if rr := postEvent(t, map[string]any{
		"event_date": "2026-09-10", "event_type_id": ls, "event_time": "00:30",
	}); rr.Code != http.StatusCreated {
		t.Fatalf("seed LS: %d %s", rr.Code, rr.Body.String())
	}
	rr = postEvent(t, map[string]any{
		"event_date": "2026-09-09", "event_type_id": ls, "event_time": "00:30",
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("an LS the day before an existing LS was accepted with %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Large Sandworm") {
		t.Errorf("message should name the Large Sandworm it conflicts with; got %q", rr.Body.String())
	}
}

// The cutover is a rule in BOTH directions. Without it the migration is a one-off
// tidy-up that the next generate, push or manual create undoes.
func TestCutoverRejectsTheWrongVariant(t *testing.T) {
	setupSettingsTestDB(t)
	seedSeasonThree(t, "2026-07-13")
	mg, ls := mgTypeID(t), lsTypeID(t)

	rr := postEvent(t, map[string]any{
		"event_date": "2026-09-08", "event_type_id": mg, "event_time": "00:30",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("an MG on the cutover day was accepted with %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "2026-09-08") || !strings.Contains(rr.Body.String(), "Large Sandworm") {
		t.Errorf("message should name the date and what to schedule instead; got %q", rr.Body.String())
	}

	rr = postEvent(t, map[string]any{
		"event_date": "2026-09-07", "event_type_id": ls, "event_time": "00:30",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("an LS the day before the cutover was accepted with %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Marshal") {
		t.Errorf("message should say to schedule a Marshal's Guard; got %q", rr.Body.String())
	}

	// The right variant on each side is fine.
	if rr := postEvent(t, map[string]any{
		"event_date": "2026-09-08", "event_type_id": ls, "event_time": "00:30",
	}); rr.Code != http.StatusCreated {
		t.Errorf("an LS on the cutover day was rejected with %d: %s", rr.Code, rr.Body.String())
	}
	if rr := postEvent(t, map[string]any{
		"event_date": "2026-09-05", "event_type_id": mg, "event_time": "00:30",
	}); rr.Code != http.StatusCreated {
		t.Errorf("an MG well before the cutover was rejected with %d: %s", rr.Code, rr.Body.String())
	}
}

type allianceExerciseGenerateResponse struct {
	MGCreated int `json:"mg_created"`
	LSCreated int `json:"ls_created"`
	ZSCreated int `json:"zs_created"`
	Switched  int `json:"switched"`
	Detail    []struct {
		Date, From, To, Reason string
	} `json:"switched_detail"`
	SkippedInvalid int `json:"skipped_invalid"`
}

func runAllianceExerciseGenerate(t *testing.T, from, to string) allianceExerciseGenerateResponse {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"from": from, "to": to, "types": []string{"mg"}})
	req := scheduleTestActor(httptest.NewRequest(http.MethodPost, "/api/schedule/events/generate", strings.NewReader(string(body))))
	rr := httptest.NewRecorder()
	generateScheduleEvents(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("generate returned %d: %s", rr.Code, rr.Body.String())
	}
	var out allianceExerciseGenerateResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// Before this, generating past the cutover produced Marshal's Guards — the exact
// rows the migration has just retyped. The generator now follows the cutover, and
// says so: an officer who ticked "Alliance Exercise" and got Large Sandworms needs
// to see the rule that decided it.
func TestGeneratorSwitchesAtCutoverAndSaysSo(t *testing.T) {
	setupSettingsTestDB(t)
	seedSeasonThree(t, "2026-07-13") // cutover 2026-09-08
	if _, err := db.Exec(`UPDATE settings SET mg_anchor_date = '2026-09-04' WHERE id = 1`); err != nil {
		t.Fatalf("set anchor: %v", err)
	}

	// A range straddling the cutover: 09-04, 09-06 are MG; 09-08 onward are LS.
	out := runAllianceExerciseGenerate(t, "2026-09-04", "2026-09-14")
	if out.MGCreated == 0 || out.LSCreated == 0 {
		t.Fatalf("straddling range produced mg=%d ls=%d, want both non-zero", out.MGCreated, out.LSCreated)
	}
	if out.Switched != out.LSCreated {
		t.Errorf("switched = %d but ls_created = %d — every Sandworm produced under an MG request is a switch",
			out.Switched, out.LSCreated)
	}
	if len(out.Detail) == 0 || !strings.Contains(out.Detail[0].Reason, "2026-09-08") {
		t.Errorf("the switch reason should name the cutover date; got %+v", out.Detail)
	}

	// Every row lands on the right side of the line, with its own type's baseline.
	var wrongSide int
	db.QueryRow(`SELECT COUNT(*) FROM schedule_events e JOIN schedule_event_types t ON t.id = e.event_type_id
		WHERE (t.short_name = 'MG' AND e.event_date >= '2026-09-08')
		   OR (t.short_name = 'LS' AND e.event_date <  '2026-09-08')`).Scan(&wrongSide)
	if wrongSide != 0 {
		t.Errorf("%d generated rows sit on the wrong side of the cutover", wrongSide)
	}
}

// A server that has not reached Season 3 has no cutover, and the generator must
// keep producing Marshal's Guard rather than comparing every date against "".
func TestGeneratorWithoutSeasonThreeKeepsMG(t *testing.T) {
	setupSettingsTestDB(t)
	if _, err := db.Exec(`UPDATE settings SET mg_anchor_date = '2026-09-04' WHERE id = 1`); err != nil {
		t.Fatalf("set anchor: %v", err)
	}

	out := runAllianceExerciseGenerate(t, "2026-09-04", "2026-09-14")
	if out.LSCreated != 0 || out.Switched != 0 {
		t.Errorf("with no Season 3 row: ls_created=%d switched=%d, want 0/0 — a compare against \"\" is true for every date",
			out.LSCreated, out.Switched)
	}
	if out.MGCreated == 0 {
		t.Error("no Marshal's Guards were generated at all")
	}
}

// The migration converts levels stored on Marshal's Guard's scale and leaves the
// ones already on the Sandworm scale alone. 70 must stay 70, not become 360.
func TestMigrationConvertsOnlyMGScaleLevels(t *testing.T) {
	setupSettingsTestDB(t)
	seedSeasonThree(t, "2026-07-13")
	mg := mgTypeID(t)

	seed := func(date string, level any) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO schedule_events
			(event_date, event_type_id, event_time, all_day, level, notes, created_by, created_at, updated_at)
			VALUES (?, ?, '00:30', 0, ?, '', 1, datetime('now'), datetime('now'))`, date, mg, level); err != nil {
			t.Fatalf("seed %s: %v", date, err)
		}
	}
	seed("2026-09-09", 12) // on the MG scale, past the cutover -> LS 70
	seed("2026-09-11", 70) // already on the Sandworm scale     -> LS 70, unchanged
	seed("2026-09-01", 12) // before the cutover                -> stays MG 12

	// A custom type's unlevelled row, to catch the unparenthesised-OR case that
	// would retype every NULL-level event in the database.
	custom := newCustomType(t, "Scratch Encounter", "SCR", false)
	if _, err := db.Exec(`INSERT INTO schedule_events
		(event_date, event_type_id, event_time, all_day, level, notes, created_by, created_at, updated_at)
		VALUES ('2026-09-10', ?, '12:00', 0, NULL, '', 1, datetime('now'), datetime('now'))`, custom); err != nil {
		t.Fatalf("seed custom: %v", err)
	}

	runMigrationBody(t, "migrations/074_alliance_exercise.sql")

	type row struct {
		short string
		level *int
	}
	got := map[string]row{}
	rows, err := db.Query(`SELECT e.event_date, t.short_name, e.level FROM schedule_events e
		JOIN schedule_event_types t ON t.id = e.event_type_id`)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	for rows.Next() {
		var date string
		var r row
		rows.Scan(&date, &r.short, &r.level)
		got[date] = r
	}
	rows.Close()

	check := func(date, wantShort string, wantLevel *int) {
		t.Helper()
		r, ok := got[date]
		if !ok {
			t.Fatalf("%s missing", date)
		}
		if r.short != wantShort {
			t.Errorf("%s is typed %s, want %s", date, r.short, wantShort)
		}
		if (r.level == nil) != (wantLevel == nil) || (r.level != nil && *r.level != *wantLevel) {
			t.Errorf("%s level = %v, want %v", date, r.level, wantLevel)
		}
	}
	l70, l12 := 70, 12
	check("2026-09-09", "LS", &l70) // 12 on the MG scale is the twelfth rung: 70
	check("2026-09-11", "LS", &l70) // already 70; the 360 trap
	check("2026-09-01", "MG", &l12) // untouched before the cutover
	check("2026-09-10", "SCR", nil) // NOT retyped — the parenthesised OR
}

// MG 12/70 was one type holding two scales. After the split each carries its own.
func TestMigrationSplitsTheCeilings(t *testing.T) {
	setupSettingsTestDB(t)
	seedSeasonThree(t, "2026-07-13")
	mg := mgTypeID(t)
	if _, err := db.Exec(`UPDATE schedule_event_types SET baseline_level=12, max_level=70 WHERE id=?`, mg); err != nil {
		t.Fatalf("seed levels: %v", err)
	}
	for _, d := range []struct {
		date  string
		level int
	}{{"2026-09-09", 70}, {"2026-09-11", 12}, {"2026-09-01", 12}} {
		if _, err := db.Exec(`INSERT INTO schedule_events
			(event_date, event_type_id, event_time, all_day, level, notes, created_by, created_at, updated_at)
			VALUES (?, ?, '00:30', 0, ?, '', 1, datetime('now'), datetime('now'))`, d.date, mg, d.level); err != nil {
			t.Fatalf("seed event: %v", err)
		}
	}

	runMigrationBody(t, "migrations/074_alliance_exercise.sql")

	_, mgBase, mgMax := typeLevelsOf(t, mg)
	_, lsBase, lsMax := typeLevelsOf(t, lsTypeID(t))
	if mgBase == nil || mgMax == nil || *mgBase != 12 || *mgMax != 12 {
		t.Errorf("MG came out as %v/%v, want 12/12", mgBase, mgMax)
	}
	if lsBase == nil || lsMax == nil || *lsBase != 70 || *lsMax != 70 {
		t.Errorf("LS came out as %v/%v, want 70/70 (12 converted onto the Sandworm scale)", lsBase, lsMax)
	}
	// A NULL ceiling would be a 500 under the level rules: max(x, NULL) is NULL in
	// SQLite, so the COALESCE in the seed is load-bearing.
	if lsMax == nil {
		t.Error("LS ceiling is NULL — SQLite's max() swallowed it")
	}
}

// With no Season 3 row the migration converts nothing and the seeded Sandworm
// numbers stand.
func TestMigrationWithoutSeasonThreeConvertsNothing(t *testing.T) {
	setupSettingsTestDB(t)
	mg := mgTypeID(t)
	if _, err := db.Exec(`INSERT INTO schedule_events
		(event_date, event_type_id, event_time, all_day, level, notes, created_by, created_at, updated_at)
		VALUES ('2026-09-09', ?, '00:30', 0, 12, '', 1, datetime('now'), datetime('now'))`, mg); err != nil {
		t.Fatalf("seed: %v", err)
	}

	runMigrationBody(t, "migrations/074_alliance_exercise.sql")

	var short string
	db.QueryRow(`SELECT t.short_name FROM schedule_events e JOIN schedule_event_types t ON t.id = e.event_type_id
		WHERE e.event_date = '2026-09-09'`).Scan(&short)
	if short != "MG" {
		t.Errorf("the row was retyped to %s with no Season 3 row — the cutover subquery is not empty-safe", short)
	}
	_, base, max := typeLevelsOf(t, lsTypeID(t))
	if base == nil || max == nil || *base != 10 || *max != 10 {
		t.Errorf("LS came out as %v/%v, want the fresh-install seed of 10/10", base, max)
	}
}
