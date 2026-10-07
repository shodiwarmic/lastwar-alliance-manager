package app

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

// Member stats are change-only (private-docs 203): a reading equal to the member's latest
// row is reported as unchanged, never written twice — on the mobile commit, the web VS
// import commit and the Members CSV import alike.

func historyRows(t *testing.T, table string, memberID int) int {
	t.Helper()
	return countRows(t, `SELECT COUNT(*) FROM `+table+` WHERE member_id = ?`, memberID)
}

func TestMobileCommitSkipsUnchangedStats(t *testing.T) {
	f := setupMobileTestDB(t)
	tok := mobileToken(t, 2)
	body := func(power, kills int) string {
		id := strconv.Itoa(f.other)
		return `{"week_date":"2026-10-05","records":[
			{"member_id":` + id + `,"original_name":"Zed","category":"power","score":` + strconv.Itoa(power) + `},
			{"member_id":` + id + `,"original_name":"Zed","category":"kills","score":` + strconv.Itoa(kills) + `}],"save_aliases":[]}`
	}

	out := decodeObject(t, serveMobile(t, "POST", "/api/mobile/commit", body(5000, 10), tok))
	if out["power_records_saved"].(float64) != 1 || out["kill_records_saved"].(float64) != 1 {
		t.Errorf("first upload = %v", out)
	}
	out = decodeObject(t, serveMobile(t, "POST", "/api/mobile/commit", body(5000, 10), tok))
	assertKeys(t, "commit", out, "records_saved", "records_unchanged")
	unchanged := out["records_unchanged"].(map[string]any)
	if out["power_records_saved"].(float64) != 0 || out["kill_records_saved"].(float64) != 0 ||
		unchanged["power"] != float64(1) || unchanged["kills"] != float64(1) {
		t.Errorf("repeat upload = %v", out)
	}
	if historyRows(t, "power_history", f.other) != 1 || historyRows(t, "kill_history", f.other) != 1 {
		t.Error("repeat upload wrote rows")
	}
	out = decodeObject(t, serveMobile(t, "POST", "/api/mobile/commit", body(6000, 10), tok))
	if out["records_saved"].(map[string]any)["power"] != float64(1) || historyRows(t, "power_history", f.other) != 2 {
		t.Errorf("changed power = %v", out)
	}

	// A misread below the category's minimum lands in errors[].
	out = decodeObject(t, serveMobile(t, "POST", "/api/mobile/commit", body(0, 10), tok))
	if len(out["errors"].([]any)) != 1 || historyRows(t, "power_history", f.other) != 2 {
		t.Errorf("zero power = %v", out)
	}
}

func TestVSImportCommitSkipsUnchangedStats(t *testing.T) {
	f := setupMobileTestDB(t)
	admin := sessionCookie(t, 1, "")
	body := `{"week_date":"2026-10-05","source":"ocr","records":[{"original_name":"Zed",
		"matched_member":{"id":` + strconv.Itoa(f.other) + `},"updated_fields":{"power":5000,"kills":10,"monday":3}}]}`
	// First: power and kills written (the VS day rides the same row). Repeat: both
	// unchanged, and the row still counts once for its VS day.
	for i, want := range []struct{ imported, unchanged float64 }{{2, 0}, {1, 2}} {
		w := serveRouter(t, "POST", "/api/vs-points/import/commit", body, admin)
		if w.Code != http.StatusOK {
			t.Fatalf("commit %d: %d %s", i, w.Code, w.Body.String())
		}
		out := decodeObject(t, w)
		if out["imported"] != want.imported || out["unchanged"] != want.unchanged {
			t.Errorf("commit %d = %v, want imported %v unchanged %v", i, out, want.imported, want.unchanged)
		}
	}
	if historyRows(t, "power_history", f.other) != 1 || historyRows(t, "kill_history", f.other) != 1 {
		t.Error("the web import wrote a repeat reading twice")
	}
}

func TestMembersImportUsesTheChangeOnlyWriter(t *testing.T) {
	f := setupMobileTestDB(t)
	admin := sessionCookie(t, 1, "")
	body := `{"source":"csv","members":[{"name":"Zed Player","rank":"R2","level":20,"power":7000,"squad_power":900}]}`
	for i := 0; i < 2; i++ {
		w := serveRouter(t, "POST", "/api/members/import/confirm", body, admin)
		if w.Code != http.StatusOK {
			t.Fatalf("confirm %d: %d %s", i, w.Code, w.Body.String())
		}
		if out := decodeObject(t, w); out["unchanged"] != float64(1) {
			t.Errorf("confirm %d result = %v", i, out)
		}
	}
	for _, tb := range []string{"hq_level_history", "power_history", "squad_power_history"} {
		if n := historyRows(t, tb, f.other); n != 1 {
			t.Errorf("%s rows = %d, want 1", tb, n)
		}
	}
	if n := countRows(t, `SELECT COUNT(*) FROM power_history WHERE member_id = ? AND source = 'csv'`, f.other); n != 1 {
		t.Errorf("csv-stamped power rows = %d", n)
	}
}

// captured_at dates a reading (private-docs 202).
func TestMobileCommitCapturedAt(t *testing.T) {
	f := setupMobileTestDB(t)
	tok := mobileToken(t, 2)
	id := strconv.Itoa(f.other)
	commit := func(power int, at string) map[string]any {
		rec := `{"member_id":` + id + `,"original_name":"Zed","category":"power","score":` + strconv.Itoa(power)
		if at != "" {
			rec += `,"captured_at":"` + at + `"`
		}
		return decodeObject(t, serveMobile(t, "POST", "/api/mobile/commit",
			`{"week_date":"2026-10-05","records":[`+rec+`}],"save_aliases":[]}`, tok))
	}
	now := time.Now().UTC()
	today := now.Add(-time.Hour)
	yesterday := now.Add(-24 * time.Hour)

	commit(9000, today.Format(time.RFC3339))
	// A back-dated, different reading lands before the newer row without touching it.
	if out := commit(8000, yesterday.Format(time.RFC3339)); out["power_records_saved"] != float64(1) {
		t.Errorf("back-dated = %v", out)
	}
	var latest int64
	db.QueryRow(`SELECT power FROM power_history WHERE member_id = ? ORDER BY recorded_at DESC LIMIT 1`, f.other).Scan(&latest)
	if latest != 9000 {
		t.Errorf("current power = %d, want today's 9000", latest)
	}
	var stored string
	db.QueryRow(`SELECT strftime('%Y-%m-%d %H:%M:%S', recorded_at) FROM power_history WHERE member_id = ? AND power = 8000`, f.other).Scan(&stored)
	if stored != yesterday.Format(sqliteTimeLayout) {
		t.Errorf("recorded_at = %q, want %q", stored, yesterday.Format(sqliteTimeLayout))
	}
	// The unchanged skip honours the time: 8000 again, an hour after yesterday's, equals
	// the row before it (yesterday's 8000), though it differs from the latest (9000).
	if out := commit(8000, yesterday.Add(time.Hour).Format(time.RFC3339)); out["records_unchanged"].(map[string]any)["power"] != float64(1) {
		t.Errorf("unchanged at its time = %v", out)
	}
	// Refused per record: the future, and more than 30 days back.
	for _, at := range []string{now.Add(20 * time.Minute).Format(time.RFC3339), now.Add(-31 * 24 * time.Hour).Format(time.RFC3339), "yesterday"} {
		if out := commit(1234, at); len(out["errors"].([]any)) != 1 || out["power_records_saved"] != float64(0) {
			t.Errorf("captured_at %s = %v", at, out)
		}
	}
	// No time means now: 9500 becomes the latest.
	commit(9500, "")
	db.QueryRow(`SELECT power FROM power_history WHERE member_id = ? ORDER BY recorded_at DESC LIMIT 1`, f.other).Scan(&latest)
	if latest != 9500 {
		t.Errorf("undated reading: latest = %d", latest)
	}
	if n := historyRows(t, "power_history", f.other); n != 3 {
		t.Errorf("rows = %d, want 3", n)
	}

	// The preview echoes captured_at from the request.
	out := decodeObject(t, serveMobile(t, "POST", "/api/mobile/preview",
		`{"week_date":"2026-10-05","entries":[{"name":"Zed Player","score":1,"category":"power","captured_at":"2026-10-05T10:00:00-04:00"}]}`, tok))
	if m := out["matched"].([]any)[0].(map[string]any); m["captured_at"] != "2026-10-05T10:00:00-04:00" {
		t.Errorf("preview echo = %v", m)
	}
}

func TestParseCapturedAt(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	if got, err := parseCapturedAt("2026-10-07T07:30:00-04:00", now); err != nil || got != "2026-10-07 11:30:00" {
		t.Errorf("offset converted = %q %v", got, err)
	}
	if got, err := parseCapturedAt("", now); got != "" || err != nil {
		t.Errorf("empty = %q %v", got, err)
	}
	if _, err := parseCapturedAt("2026-10-07T12:09:00Z", now); err != nil {
		t.Errorf("9 minutes ahead refused: %v", err)
	}
}
