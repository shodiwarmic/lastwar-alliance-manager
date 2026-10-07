package app

import (
	"net/http"
	"strconv"
	"testing"
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
