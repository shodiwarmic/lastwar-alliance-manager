package app

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lastwar-alliance/internal/gametime"
)

// The VS upload's OCR preview and the shared commit: categories this page cannot
// store are reported instead of guessed into a vs_points column, the Weekly Rank
// screen becomes a Saturday only when Monday–Friday are known, and a commit key
// is validated before it can become a column name (private-docs 103).

func vsAs(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), authUserKey,
		&AuthUser{ID: 1, Username: "officer", Rank: "R4", IsAdmin: true}))
}

func uploadVS(t *testing.T, srvURL string) map[string]interface{} {
	t.Helper()
	return uploadVSWith(t, nil)
}

// uploadVSWith sends the upload form with extra fields (donation_date).
func uploadVSWith(t *testing.T, fields map[string]string) map[string]interface{} {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, _ := w.CreateFormFile("images", "f.png")
	part.Write([]byte("png"))
	w.WriteField("week", "current")
	w.WriteField("force_category", "auto")
	for k, v := range fields {
		w.WriteField(k, v)
	}
	w.Close()
	r := httptest.NewRequest("POST", "/api/upload", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	processSmartScreenshot(rec, vsAs(r))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

func vsRows(out map[string]interface{}) map[string]map[string]interface{} {
	rows := map[string]map[string]interface{}{}
	for _, bucket := range []string{"matched", "unresolved"} {
		list, _ := out[bucket].([]interface{})
		for _, r := range list {
			m := r.(map[string]interface{})
			rows[m["original_name"].(string)] = m
		}
	}
	return rows
}

func setupVSImport(t *testing.T, reply string) string {
	t.Helper()
	srv, _ := fakeOCRService(t, 200, reply, "")
	setupOCRTestDB(t, OCRBackendCloud, srv.URL)
	if _, err := db.Exec(`INSERT INTO members (id, name, rank) VALUES (1, 'Alpha', 'R3'), (2, 'Beta', 'R3'), (3, 'Gamma', 'R3')`); err != nil {
		t.Fatal(err)
	}
	return gameWeekStart(time.Now(), 0)
}

func TestVSUpload_WeeklyBecomesSaturday(t *testing.T) {
	week := setupVSImport(t, `{"results": {"weekly": [
		{"player_name": "Alpha", "score": 1500},
		{"player_name": "Beta", "score": 1500},
		{"player_name": "Gamma", "score": 100}]}}`)
	// Alpha's Mon–Fri are all stored; Beta has only Monday (a mid-week upload);
	// Gamma's stored days exceed the total.
	db.Exec(`INSERT INTO vs_points (member_id, week_date, monday, tuesday, wednesday, thursday, friday) VALUES
		(1, ?, 100, 200, 300, 400, 300), (2, ?, 100, 0, 0, 0, 0), (3, ?, 50, 50, 50, 50, 50)`, week, week, week)

	rows := vsRows(uploadVS(t, ""))
	alpha := rows["Alpha"]
	if f := alpha["updated_fields"].(map[string]interface{}); f["saturday"] != float64(200) || f["weekly"] != nil || alpha["calculated_sat"] != true {
		t.Errorf("Alpha = %v, want Saturday 200 derived and no weekly field", alpha)
	}
	if beta := rows["Beta"]; beta["error"] != "Saturday not derived: Mon–Fri incomplete" || len(beta["updated_fields"].(map[string]interface{})) != 0 || beta["total"] != float64(1500) {
		t.Errorf("Beta = %v, want the total kept and the row flagged", beta)
	}
	if gamma := rows["Gamma"]; gamma["error"] != "Total is less than the sum of Monday–Friday" {
		t.Errorf("Gamma = %v", gamma)
	}
}

func TestVSUpload_UnstorableCategoriesAreReported(t *testing.T) {
	setupVSImport(t, `{"results": {
		"zombie_siege": [{"player_name": "Beta", "score": 20}],
		"friday": [{"player_name": "Gamma", "score": 5000}]}}`)
	out := uploadVS(t, "")
	skipped := strings.Join(toStrings(out["skipped_groups"]), " | ")
	if !strings.Contains(skipped, "zombie_siege (1 rows) — not a VS or Strength ranking — use the Participation page") {
		t.Errorf("skipped_groups = %q", skipped)
	}
	rows := vsRows(out)
	if _, ok := rows["Beta"]; ok {
		t.Error("a mail row reached the preview")
	}
	if rows["Gamma"]["updated_fields"].(map[string]interface{})["friday"] != float64(5000) {
		t.Errorf("Gamma = %v", rows["Gamma"])
	}
}

func toStrings(v interface{}) []string {
	list, _ := v.([]interface{})
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.(string)
	}
	return out
}

func TestVSCommit_RejectsUnknownKeysBeforeAnySQL(t *testing.T) {
	week := setupVSImport(t, `{"results": {}}`)
	for _, key := range []string{"weekly", "hq_level", "monday = 0, saturday"} {
		body, _ := json.Marshal(VSImportCommitRequest{WeekDate: week, Records: []VSImportRow{{
			OriginalName: "Alpha", MatchedMember: &Member{ID: 1},
			UpdatedFields: map[string]int{key: 5},
		}}})
		rec := httptest.NewRecorder()
		commitCSVImport(rec, vsAs(httptest.NewRequest("POST", "/api/vs/import/commit", bytes.NewReader(body))))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("key %q: status %d, want 400", key, rec.Code)
		}
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM vs_points`).Scan(&n)
	if n != 0 {
		t.Errorf("%d vs_points rows written", n)
	}
}

func TestVSCommit_SavesTheDerivedSaturday(t *testing.T) {
	week := setupVSImport(t, `{"results": {}}`)
	body, _ := json.Marshal(VSImportCommitRequest{WeekDate: week, Records: []VSImportRow{{
		OriginalName: "Alpha", MatchedMember: &Member{ID: 1},
		UpdatedFields: map[string]int{"saturday": 200, "monday": 100},
	}}})
	rec := httptest.NewRecorder()
	commitCSVImport(rec, vsAs(httptest.NewRequest("POST", "/", bytes.NewReader(body))))
	var sat int
	if err := db.QueryRow(`SELECT saturday FROM vs_points WHERE member_id = 1`).Scan(&sat); err != nil || sat != 200 {
		t.Fatalf("saturday = %d, err %v (status %d %s)", sat, err, rec.Code, rec.Body.String())
	}
}

// Donations (private-docs 259): both tabs preview and store, filed on the day the page
// names, at that day's earliest accepted instant, source ocr; a timed reading of the day
// beats a later web upload; a repeat is unchanged; today above week is refused; a row
// carrying a VS day and a donation half commits both.
func TestVSUpload_DonationsStore(t *testing.T) {
	week := setupVSImport(t, `{"results": {
		"donation_daily": [{"player_name": "Alpha", "score": 900}, {"player_name": "Gamma", "score": 700}],
		"donation_weekly": [{"player_name": "Alpha", "score": 4100}, {"player_name": "Gamma", "score": 500}]}}`)
	yesterday := gametime.DateOf(time.Now().AddDate(0, 0, -1))
	out := uploadVSWith(t, map[string]string{"donation_date": yesterday})
	if out["donation_date"] != yesterday {
		t.Errorf("donation_date echoed as %v", out["donation_date"])
	}
	f := vsRows(out)["Alpha"]["updated_fields"].(map[string]interface{})
	if f["donation_daily"] != float64(900) || f["donation_weekly"] != float64(4100) {
		t.Fatalf("Alpha's preview = %v", f)
	}

	commit := func(rows []VSImportRow) map[string]interface{} {
		t.Helper()
		body, _ := json.Marshal(VSImportCommitRequest{WeekDate: week, Records: rows, Source: "ocr", DonationDate: yesterday})
		rec := httptest.NewRecorder()
		commitCSVImport(rec, vsAs(httptest.NewRequest("POST", "/", bytes.NewReader(body))))
		if rec.Code != 200 {
			t.Fatalf("commit: %d %s", rec.Code, rec.Body.String())
		}
		var out map[string]interface{}
		json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	alpha := VSImportRow{OriginalName: "Alpha", MatchedMember: &Member{ID: 1},
		UpdatedFields: map[string]int{"donation_daily": 900, "donation_weekly": 4100, "monday": 1234}}
	gamma := VSImportRow{OriginalName: "Gamma", MatchedMember: &Member{ID: 3},
		UpdatedFields: map[string]int{"donation_daily": 700, "donation_weekly": 500}}
	res := commit([]VSImportRow{alpha, gamma})
	if res["donations_saved"] != float64(2) {
		t.Errorf("donations_saved = %v (%v)", res["donations_saved"], res)
	}
	if errs := toStrings(res["errors"]); len(errs) != 1 || !strings.Contains(errs[0], "Gamma") {
		t.Errorf("errors = %v, want Gamma's impossible pair", errs)
	}
	at := yesterday + " 02:10:00"
	if n := countRows(t, `SELECT COUNT(*) FROM donation_readings WHERE member_id = 1 AND game_date = ? AND source = 'ocr' AND captured_at = ?`, yesterday, at); n != 2 {
		t.Errorf("Alpha's halves at %s: %d rows", at, n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM vs_points WHERE member_id = 1 AND monday = 1234`); n != 1 {
		t.Error("the VS day on the same row was not saved")
	}
	if n := countRows(t, `SELECT COUNT(*) FROM donation_readings WHERE member_id = 3`); n != 0 {
		t.Errorf("Gamma's refused pair stored %d rows", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM activity_log WHERE entity_type = 'donations' AND entity_name = ?`, yesterday); n != 1 {
		t.Errorf("donations activity rows = %d", n)
	}

	// The same upload again: unchanged.
	if res := commit([]VSImportRow{alpha}); res["donations_unchanged"] != float64(2) || res["donations_saved"] != float64(0) {
		t.Errorf("repeat: %v", res)
	}

	// The scanner read the day at 20:00; a later web upload of the same day does not
	// replace it.
	mustExec(t, `UPDATE donation_readings SET amount = 1500, captured_at = ? || ' 20:00:00', source = 'mobile'
		WHERE member_id = 1 AND kind = 'today'`, yesterday)
	alpha.UpdatedFields = map[string]int{"donation_daily": 950}
	commit([]VSImportRow{alpha})
	var amount int
	var src string
	db.QueryRow(`SELECT amount, source FROM donation_readings WHERE member_id = 1 AND kind = 'today'`).Scan(&amount, &src)
	if amount != 1500 || src != "mobile" {
		t.Errorf("the timed reading was replaced: %d %s", amount, src)
	}
}

func TestDonationUploadDates(t *testing.T) {
	now := time.Date(2026, 10, 14, 2, 5, 0, 0, time.UTC) // in the reset window; game day the 14th
	if got := donationUploadInstant("2026-10-14", now); !got.Equal(time.Date(2026, 10, 14, 2, 10, 0, 0, time.UTC)) {
		t.Errorf("today at 02:05 = %v, want 02:10", got)
	}
	if got := donationUploadInstant("2026-10-12", now); !got.Equal(time.Date(2026, 10, 12, 2, 10, 0, 0, time.UTC)) {
		t.Errorf("a past day = %v", got)
	}
	for raw, ok := range map[string]bool{"": true, "2026-10-14": true, "2026-09-14": true,
		"2026-09-13": false, "2026-10-15": false, "14/10/2026": false} {
		if _, err := parseDonationDate(raw, now); (err == nil) != ok {
			t.Errorf("%q: err %v", raw, err)
		}
	}
}
