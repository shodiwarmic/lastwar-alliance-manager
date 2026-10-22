package app

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// uploadTestFile posts a multipart upload as actor and returns the response.
func uploadTestFile(t *testing.T, actor *AuthUser, name, content string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write([]byte(content))
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = req.WithContext(context.WithValue(req.Context(), authUserKey, actor))
	rr := httptest.NewRecorder()
	uploadFile(rr, req)
	return rr
}

func TestSeasonTaggedFilesAndTheirArchiveCopies(t *testing.T) {
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	r1 := seedMember(t, "Rookie", "R1")
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active, member_id) VALUES (2, 'rookie', 'x', 0, 1, ?)`, r1)
	admin := &AuthUser{ID: 1, Username: "admin", IsAdmin: true}
	rookie := &AuthUser{ID: 2, Username: "rookie", Rank: "R1", MemberID: &r1}
	seedSeasonRow(t, 1, 4, "2030-11-02", true, false)
	seedSeasonRow(t, 2, 3, "2030-01-01", false, true)

	// A .json map export uploads as a data file, tagged to the season.
	rr := uploadTestFile(t, admin, "map.json", `{"version":"1.0","grids":[]}`,
		map[string]string{"title": "Route map", "min_rank": "R1", "min_edit_rank": "R4", "season_id": "1"})
	if rr.Code != http.StatusOK {
		t.Fatalf("json upload: %d %s", rr.Code, rr.Body.String())
	}
	var fileType, fileName string
	db.QueryRow(`SELECT file_type, file_name FROM files WHERE title = 'Route map'`).Scan(&fileType, &fileName)
	if fileType != "data" {
		t.Errorf("file_type = %q, want data", fileType)
	}
	// An untagged file and one tagged to an archived season (refused).
	uploadTestFile(t, admin, "notes.txt", "hello", map[string]string{"title": "Notes", "min_rank": "R1", "min_edit_rank": "R4"})
	if rr := uploadTestFile(t, admin, "late.txt", "late", map[string]string{"title": "Late", "min_rank": "R1",
		"min_edit_rank": "R4", "season_id": "2"}); rr.Code != http.StatusConflict {
		t.Errorf("tagging to an archived season: %d", rr.Code)
	}

	// The season filter lists only the season's files.
	sid := 1
	files, err := listFilesForUser(admin, &sid)
	if err != nil || len(files) != 1 || files[0].Title != "Route map" {
		t.Fatalf("season filter = %+v (%v)", files, err)
	}

	// An R4 tag hides the R1 file from a rookie; the archive freezes that rank.
	mustExecT(t, `INSERT INTO file_tags (id, name, min_rank, color, sort_order) VALUES (9, 'Officers', 'R4', 'info', 0)`)
	var fid int
	db.QueryRow(`SELECT id FROM files WHERE title = 'Route map'`).Scan(&fid)
	mustExecT(t, `INSERT INTO file_tag_map (file_id, tag_id) VALUES (?, 9)`, fid)
	if err := archiveSeason(1, "2030-12-28"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	var snapID int
	var rank string
	db.QueryRow(`SELECT id, effective_min_rank FROM season_file_snapshots WHERE season_id = 1`).Scan(&snapID, &rank)
	if rank != "R4" {
		t.Errorf("frozen rank = %q, want R4 (the tag's)", rank)
	}
	copyPath := filepath.Join(seasonArchiveDir(1), fileName)
	if b, err := os.ReadFile(copyPath); err != nil || !strings.Contains(string(b), `"grids"`) {
		t.Fatalf("copy at %s: %v", copyPath, err)
	}

	// The tag is relaxed after the archive; the copy keeps the rank it had.
	mustExecT(t, `UPDATE file_tags SET min_rank = 'R1' WHERE id = 9`)
	download := func(actor *AuthUser) int {
		rr := httptest.NewRecorder()
		handleSeasonFileSnapshotDownload(rr, shReq(http.MethodGet, "", map[string]string{"id": strconv.Itoa(snapID)}, actor))
		return rr.Code
	}
	if code := download(rookie); code != http.StatusForbidden {
		t.Errorf("rookie download of an R4 copy: %d", code)
	}
	if code := download(admin); code != http.StatusOK {
		t.Errorf("admin download: %d", code)
	}
	req := shReq(http.MethodGet, "", nil, rookie)
	req.URL.RawQuery = "season_id=1"
	rr = httptest.NewRecorder()
	handleSeasonHubFiles(rr, req)
	if strings.Contains(rr.Body.String(), "Route map") {
		t.Error("the archived list shows a rookie a copy frozen at R4")
	}

	// Nothing moves into or out of the archived season.
	body := `{"title":"Route map","min_rank":"R1","min_edit_rank":"R4","season_id":null}`
	rr = httptest.NewRecorder()
	updateFile(rr, shReq(http.MethodPut, body, map[string]string{"id": strconv.Itoa(fid)}, admin))
	if rr.Code != http.StatusConflict {
		t.Errorf("moving a file out of an archived season: %d", rr.Code)
	}
}

func TestCommsAndPollSeasonTags(t *testing.T) {
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	seedSeasonRow(t, 1, 4, "2030-11-02", true, false)
	seedSeasonRow(t, 2, 3, "2030-01-01", false, true)

	// A mail template cannot move into an archived season.
	mustExecT(t, `INSERT INTO comms_templates (id, type, title, category, content, season_id, required_vars, created_by)
		VALUES (5, 'mail', 'Rally', 'War', 'x', 1, '[]', 1)`)
	rr := httptest.NewRecorder()
	handleCommsTemplateUpdate(rr, shReq(http.MethodPut, `{"title":"Rally","category":"War","content":"x","season_id":2}`,
		map[string]string{"id": "5"}, nil))
	if rr.Code != http.StatusConflict {
		t.Errorf("template into an archived season: %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	handleCommsTemplateUpdate(rr, shReq(http.MethodPut, `{"title":"Rally","category":"War","content":"x","season_id":null}`,
		map[string]string{"id": "5"}, nil))
	if rr.Code != http.StatusOK || countT(t, `SELECT COUNT(*) FROM comms_templates WHERE id = 5 AND season_id IS NULL`) != 1 {
		t.Errorf("clearing the season: %d", rr.Code)
	}

	// A poll launched from a season's template inherits the season unless the launch says.
	rr = httptest.NewRecorder()
	handlePollTemplateCreate(rr, shReq(http.MethodPost,
		`{"title":"Attack poll","question":"In?","options":["Yes","No"],"poll_type":"named","season_id":1}`, nil, nil))
	var tmpl struct{ ID int }
	json.Unmarshal(rr.Body.Bytes(), &tmpl)
	rr = httptest.NewRecorder()
	handlePollInstanceCreate(rr, shReq(http.MethodPost, `{"template_id":`+strconv.Itoa(tmpl.ID)+`,"label":"W5 attack"}`, nil, nil))
	rr2 := httptest.NewRecorder()
	handlePollInstanceCreate(rr2, shReq(http.MethodPost, `{"template_id":`+strconv.Itoa(tmpl.ID)+`,"label":"Unrelated","season_id":null}`, nil, nil))
	if n := countT(t, `SELECT COUNT(*) FROM poll_instances WHERE label = 'W5 attack' AND season_id = 1`); n != 1 {
		t.Error("the launched poll did not inherit its template's season")
	}
	if n := countT(t, `SELECT COUNT(*) FROM poll_instances WHERE label = 'Unrelated' AND season_id IS NULL`); n != 1 {
		t.Error("a launch with season_id null still took the season")
	}
}
