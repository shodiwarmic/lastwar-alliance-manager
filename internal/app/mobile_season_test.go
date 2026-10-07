package app

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
)

// The mobile Season Hub contributions store (Project 15, C9) shares the web import's
// target, resolution and upsert.
func TestMobileSeasonContributions(t *testing.T) {
	srv, _ := fakeOCRService(t, 200, `{"schema_version": 1, "results": {"mutual_assistance_weekly": [
		{"player_name": "Zed Player", "score": 70}, {"player_name": "Unknown Guy", "score": 50}]}}`, "")
	setupOCRTestDB(t, OCRBackendCloud, srv.URL)
	f := seedMobileFixture(t)
	setRankPerm(t, "R4", "manage_season_hub", true)
	setRankPerm(t, "R4", "view_season_hub", true)
	res, _ := db.Exec(`INSERT INTO seasons (name, season_number, start_date, is_active) VALUES ('S3', 3, '2026-09-01', 1)`)
	sid, _ := res.LastInsertId()
	db.Exec(`INSERT INTO season_trackables (season_id, key, label) VALUES (?, 'mutual_assistance', 'MA')`, sid)
	sidS := strconv.FormatInt(sid, 10)
	tok := mobileToken(t, 2)

	// GET lists the season and its categories.
	out := decodeObject(t, serveMobile(t, "GET", "/api/mobile/season-hub", "", tok))
	if cats := out["categories"].([]any); len(cats) != 3 || cats[1] != "mutual_assistance_weekly" {
		t.Errorf("categories = %v", out["categories"])
	}

	// The web preview (frames, through the OCR stub) and the mobile preview (rows) agree.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("images", "f.png")
	part.Write([]byte("png"))
	mw.WriteField("season_id", sidS)
	mw.WriteField("week_number", "2")
	mw.WriteField("category", "mutual_assistance_weekly")
	mw.Close()
	req := httptest.NewRequest("POST", "/api/season-hub/contributions/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(sessionCookie(t, 2, ""))
	web := httptest.NewRecorder()
	buildRouter().ServeHTTP(web, req)
	if web.Code != http.StatusOK {
		t.Fatalf("web preview: %d %s", web.Code, web.Body.String())
	}
	mob := serveMobile(t, "POST", "/api/mobile/season-hub/contributions/preview", `{"season_id":`+sidS+`,"week_number":2,
		"category":"mutual_assistance_weekly","entries":[{"name":"Zed Player","score":70},{"name":"Unknown Guy","score":50}]}`, tok)
	if mob.Code != http.StatusOK {
		t.Fatalf("mobile preview: %d %s", mob.Code, mob.Body.String())
	}
	var w1, m1 map[string]any
	json.Unmarshal(web.Body.Bytes(), &w1)
	json.Unmarshal(mob.Body.Bytes(), &m1)
	for _, k := range []string{"matched", "unresolved", "week_number", "category"} {
		if !reflect.DeepEqual(w1[k], m1[k]) {
			t.Errorf("%s: web %v, mobile %v", k, w1[k], m1[k])
		}
	}

	// Commit writes the rows and an alias, logged via mobile.
	w := serveMobile(t, "POST", "/api/mobile/season-hub/contributions/commit", `{"season_id":`+sidS+`,"week_number":2,
		"category":"mutual_assistance_weekly","records":[{"member_id":`+strconv.Itoa(f.other)+`,"original_name":"Zed Player","score":70},
		{"member_id":`+strconv.Itoa(f.r3Member)+`,"original_name":"Unknown Guy","score":50},
		{"member_id":`+strconv.Itoa(f.former)+`,"original_name":"Gone","score":5}],
		"save_aliases":[{"failed_alias":"Unknown Guy","member_id":`+strconv.Itoa(f.r3Member)+`,"category":"ocr"}]}`, tok)
	out = decodeObject(t, w)
	if out["committed"] != float64(2) || out["aliases_saved"] != float64(1) || len(out["errors"].([]any)) != 1 {
		t.Errorf("commit = %v", out)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM season_member_records WHERE season_id = ? AND week_number = 2`, sid); n != 2 {
		t.Errorf("records = %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM activity_log WHERE entity_type = 'season_contributions' AND details LIKE '%via mobile'`); n != 1 {
		t.Errorf("activity rows = %d", n)
	}

	// A season-total category lands in week 0.
	serveMobile(t, "POST", "/api/mobile/season-hub/contributions/commit", `{"season_id":`+sidS+`,"week_number":5,
		"category":"mutual_assistance_season","records":[{"member_id":`+strconv.Itoa(f.other)+`,"original_name":"Zed","score":900}]}`, tok)
	if n := countRows(t, `SELECT COUNT(*) FROM season_member_records WHERE week_number = 0 AND recorded_value = 900`); n != 1 {
		t.Errorf("season total rows in week 0 = %d", n)
	}

	// An unknown category is 400; an archived season is 409.
	if w := serveMobile(t, "POST", "/api/mobile/season-hub/contributions/preview", `{"season_id":`+sidS+`,"week_number":1,"category":"nope_weekly","entries":[]}`, tok); w.Code != http.StatusBadRequest {
		t.Errorf("unknown category: %d", w.Code)
	}
	db.Exec(`UPDATE seasons SET archived_at = CURRENT_TIMESTAMP WHERE id = ?`, sid)
	if w := serveMobile(t, "POST", "/api/mobile/season-hub/contributions/commit", `{"season_id":`+sidS+`,"week_number":1,"category":"mutual_assistance_weekly","records":[]}`, tok); w.Code != http.StatusConflict {
		t.Errorf("archived: %d", w.Code)
	}
}
