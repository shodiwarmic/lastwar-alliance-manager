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
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, _ := w.CreateFormFile("images", "f.png")
	part.Write([]byte("png"))
	w.WriteField("week", "current")
	w.WriteField("force_category", "auto")
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
		"donation_daily": [{"player_name": "Alpha", "score": 900}],
		"zombie_siege": [{"player_name": "Beta", "score": 20}],
		"friday": [{"player_name": "Gamma", "score": 5000}]}}`)
	out := uploadVS(t, "")
	skipped := strings.Join(toStrings(out["skipped_groups"]), " | ")
	if !strings.Contains(skipped, "Donation (Daily) (1 rows) — not stored yet") ||
		!strings.Contains(skipped, "zombie_siege (1 rows) — not a VS or Strength ranking — use the Participation page") {
		t.Errorf("skipped_groups = %q", skipped)
	}
	rows := vsRows(out)
	if _, ok := rows["Alpha"]; ok {
		t.Error("a donation row reached the preview")
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
	for _, key := range []string{"weekly", "donation_daily", "monday = 0, saturday"} {
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
