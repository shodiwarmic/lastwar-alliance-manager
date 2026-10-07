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

// The mobile participation store (Project 15, C10).

// seedPtAdmin adds an admin user to setupParticipationTestDB's database and returns a
// mobile token and a session cookie for it.
func seedPtAdmin(t *testing.T) (string, *http.Cookie) {
	t.Helper()
	if store == nil {
		initSessionStore()
	}
	if _, err := db.Exec(`INSERT INTO users (id, username, password, is_admin, is_active, force_password_change, password_changed_at)
		VALUES (60, 'ptadmin', 'x', 1, 1, 0, datetime('now', '-1 day'))`); err != nil {
		t.Fatal(err)
	}
	return mobileToken(t, 60), sessionCookie(t, 60, "")
}

func TestMobileParticipationRowsMatchFrames(t *testing.T) {
	f := setupParticipationTestDB(t)
	tok, _ := seedPtAdmin(t)
	seedEvent(t, f.zs, "2026-09-23")
	srv, _ := mailService(t, `"zombie_siege"`, zsReply)
	pointOCRAt(t, srv.URL)

	// Frames: multipart through the mobile route.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("images", "01.png")
	part.Write([]byte("png"))
	mw.WriteField("event_type_id", strconv.Itoa(f.zs))
	mw.WriteField("tz", "America/New_York")
	mw.Close()
	req := httptest.NewRequest("POST", "/api/mobile/participation/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+tok)
	frames := httptest.NewRecorder()
	buildRouter().ServeHTTP(frames, req)
	if frames.Code != http.StatusOK {
		t.Fatalf("frames import: %d %s", frames.Code, frames.Body.String())
	}

	// Rows: the same players, as the phone would send them.
	rows := serveMobile(t, "POST", "/api/mobile/participation/preview", `{"event_type_id":`+strconv.Itoa(f.zs)+`,
		"frames":[{"players":[{"player_name":"Alpha","score":20,"rank":1},{"player_name":"Bravo","score":0,"rank":2},
		{"player_name":"Charlie","score":0,"rank":3,"score_unread":true}],"mail_timestamp":"2026-09-23 23:03:00"}],
		"tz":"America/New_York"}`, tok)
	if rows.Code != http.StatusOK {
		t.Fatalf("rows preview: %d %s", rows.Code, rows.Body.String())
	}
	var a, b map[string]any
	json.Unmarshal(frames.Body.Bytes(), &a)
	json.Unmarshal(rows.Body.Bytes(), &b)
	for _, k := range []string{"rows", "problems", "category", "mail_timestamp", "suggested", "candidates", "frames"} {
		if !reflect.DeepEqual(a[k], b[k]) {
			t.Errorf("%s: frames %v, rows %v", k, a[k], b[k])
		}
	}
	if b["suggested"].(map[string]any)["event_id"] == nil {
		t.Errorf("no suggestion: %v", b["suggested"])
	}

	// Types list it with its mail category.
	types := serveMobile(t, "GET", "/api/mobile/participation/types", "", tok)
	var list []map[string]any
	json.Unmarshal(types.Body.Bytes(), &list)
	found := false
	for _, ty := range list {
		assertKeys(t, "type", ty, "event_type_id", "name", "absence_rule", "ocr_category", "trackables")
		if ty["event_type_id"] == float64(f.zs) && ty["ocr_category"] == "zombie_siege" {
			found = true
		}
	}
	if !found {
		t.Errorf("types = %v", list)
	}
}

func TestMobileBoardSaveKeepsJudgement(t *testing.T) {
	setupParticipationTestDB(t)
	tok, admin := seedPtAdmin(t)
	ds := dsTypeID(t)
	var key string
	db.QueryRow(`SELECT key FROM participation_trackables WHERE event_type_id = ? ORDER BY sort_order LIMIT 1`, ds).Scan(&key)
	db.Exec(`INSERT INTO storm_groups (id, task_force, name, instructions, sort_order) VALUES (1,'A','A1','',0)`)
	db.Exec(`INSERT INTO storm_group_members (group_id, member_id, is_sub, position) VALUES (1,1,0,0),(1,2,1,1)`)
	res, _ := db.Exec(`INSERT INTO schedule_events (event_date, event_type_id, event_time, notes, created_by, task_force) VALUES ('2026-09-18', ?, '23:00', '', 1, 'A')`, ds)
	ev64, _ := res.LastInsertId()
	ev := strconv.FormatInt(ev64, 10)
	entries := `[{"rank":1,"name":"Alpha","member_id":1,"values":{"` + key + `":100}},{"rank":2,"name":"Bravo","member_id":2,"values":{"` + key + `":50}}]`

	// A board first saved from the phone has no roles, so the web offers the planner's lineup.
	w := serveMobile(t, "PUT", "/api/mobile/participation/boards/"+ev, `{"entries":`+entries+`}`, tok)
	if w.Code != http.StatusOK {
		t.Fatalf("mobile save: %d %s", w.Code, w.Body.String())
	}
	out := decodeObject(t, w)
	if out["matched"] != float64(2) || out["entries"] != float64(2) {
		t.Errorf("mobile save = %v", out)
	}
	var source string
	db.QueryRow(`SELECT source FROM participation_boards WHERE schedule_event_id = ?`, ev64).Scan(&source)
	if source != "import" {
		t.Errorf("source = %q", source)
	}
	d, err := buildBoardDetail(db, int(ev64))
	if err != nil || d == nil || len(d.RolesPrefill) != 2 {
		t.Fatalf("roleless board's prefill = %+v %v", d, err)
	}

	// The leader records the lineup, result and notes on the web; an exception exists.
	w = serveRouter(t, "PUT", "/api/participation/boards/"+ev, `{"entries":`+entries+`,"roles":[{"member_id":1,"role":"starter","task_force":"A"}],
		"result":{"won":true},"notes":"good fight","source":"import"}`, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("web save: %d %s", w.Code, w.Body.String())
	}
	var boardID int
	db.QueryRow(`SELECT id FROM participation_boards WHERE schedule_event_id = ?`, ev64).Scan(&boardID)
	db.Exec(`INSERT INTO participation_exceptions (board_id, member_id, kind, reason) VALUES (?, 3, 'excused', 'away')`, boardID)

	// A phone re-save keeps roles, result, notes and the exception.
	if w := serveMobile(t, "PUT", "/api/mobile/participation/boards/"+ev, `{"entries":`+entries+`}`, tok); w.Code != http.StatusOK {
		t.Fatalf("mobile re-save: %d %s", w.Code, w.Body.String())
	}
	var roles, excs int
	var result, notes string
	count := func() {
		db.QueryRow(`SELECT COUNT(*) FROM participation_roles WHERE board_id = ?`, boardID).Scan(&roles)
		db.QueryRow(`SELECT COUNT(*) FROM participation_exceptions WHERE board_id = ?`, boardID).Scan(&excs)
		db.QueryRow(`SELECT result_json, notes FROM participation_boards WHERE id = ?`, boardID).Scan(&result, &notes)
	}
	count()
	if roles != 1 || excs != 1 || result != `{"won":true}` || notes != "good fight" {
		t.Errorf("after mobile save: roles %d, exceptions %d, result %s, notes %q", roles, excs, result, notes)
	}
	// The web PUT still replaces them, and still keeps the exception.
	if w := serveRouter(t, "PUT", "/api/participation/boards/"+ev, `{"entries":`+entries+`,"source":"import"}`, admin); w.Code != http.StatusOK {
		t.Fatalf("web re-save: %d %s", w.Code, w.Body.String())
	}
	count()
	if roles != 0 || excs != 1 || result != "{}" || notes != "" {
		t.Errorf("after web save: roles %d, exceptions %d, result %s, notes %q", roles, excs, result, notes)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM activity_log WHERE entity_type = 'participation_board' AND details LIKE '%via mobile'`); n != 2 {
		t.Errorf("mobile board activity rows = %d", n)
	}
}

func TestMobileBoardRefusesFutureEvent(t *testing.T) {
	f := setupParticipationTestDB(t)
	tok, _ := seedPtAdmin(t)
	ev := seedEvent(t, f.zs, "2099-01-01")
	w := serveMobile(t, "PUT", "/api/mobile/participation/boards/"+strconv.Itoa(ev), `{"entries":[]}`, tok)
	if w.Code != http.StatusBadRequest {
		t.Errorf("future board: %d %s", w.Code, w.Body.String())
	}
	// The occurrence route refuses a future date, and creates a past one.
	if w := serveMobile(t, "POST", "/api/mobile/participation/occurrences", `{"event_date":"2099-01-01","event_type_id":`+strconv.Itoa(f.zs)+`,"event_time":"20:00"}`, tok); w.Code != http.StatusBadRequest {
		t.Errorf("future occurrence: %d", w.Code)
	}
	w = serveMobile(t, "POST", "/api/mobile/participation/occurrences", `{"event_date":"2026-09-01","event_type_id":`+strconv.Itoa(f.zs)+`,"event_time":"20:00"}`, tok)
	if w.Code != http.StatusOK || decodeObject(t, w)["id"] == nil {
		t.Errorf("occurrence: %d %s", w.Code, w.Body.String())
	}
}
