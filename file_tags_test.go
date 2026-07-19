package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// setupFileTagsTestDB points the package-level db at a fresh temp SQLite file, runs
// every migration (including 059/060), and seeds a couple of users. It restores the
// previous db handle on cleanup so tests stay isolated.
func setupFileTagsTestDB(t *testing.T) {
	t.Helper()
	prev := db
	t.Setenv("DATABASE_PATH", filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("STORAGE_PATH", t.TempDir())
	t.Setenv("SESSION_KEY", "test-session-key-at-least-32-chars-long")
	if err := initDB(); err != nil {
		t.Fatalf("initDB: %v", err)
	}
	t.Cleanup(func() {
		if db != nil {
			db.Close()
		}
		db = prev
	})

	// Two owners so we can exercise the owner-bypass and non-owner paths.
	if _, err := db.Exec(`INSERT INTO users (id, username, password, is_admin) VALUES
		(10, 'r5user', 'x', 0), (11, 'r1user', 'x', 0)`); err != nil {
		t.Fatalf("seed users: %v", err)
	}
}

func reqAs(user *AuthUser) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	return r.WithContext(context.WithValue(r.Context(), authUserKey, user))
}

// TestGetFilesListEffectiveRank is the load-bearing test: a tag's min_rank must hide a
// file from a lower-rank viewer even when the file's own min_rank would permit it, while
// the owner still sees it (without the restricted badge). It also implicitly proves the
// query path doesn't deadlock — a cursor leak on the single connection would hang here.
func TestGetFilesListEffectiveRank(t *testing.T) {
	setupFileTagsTestDB(t)

	// A file owned by r1user (id 11), min_rank R1 (open to everyone).
	res, err := db.Exec(`INSERT INTO files (title, file_name, file_type, min_rank, min_edit_rank, owner_user_id, updated_at)
		VALUES ('Violation Screenshot', 'v.png', 'image', 'R1', 'R4', 11, CURRENT_TIMESTAMP)`)
	if err != nil {
		t.Fatalf("insert file: %v", err)
	}
	fileID, _ := res.LastInsertId()

	// A Violations tag restricted to R4+, attached to the file.
	res, err = db.Exec(`INSERT INTO file_tags (name, min_rank, color) VALUES ('Violations', 'R4', 'danger')`)
	if err != nil {
		t.Fatalf("insert tag: %v", err)
	}
	tagID, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO file_tag_map (file_id, tag_id) VALUES (?, ?)`, fileID, tagID); err != nil {
		t.Fatalf("insert map: %v", err)
	}

	call := func(user *AuthUser) []AllianceFile {
		w := httptest.NewRecorder()
		getFilesList(w, reqAs(user))
		if w.Code != http.StatusOK {
			t.Fatalf("getFilesList status = %d", w.Code)
		}
		var out []AllianceFile
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}

	// R3 viewer (not owner, not admin): the R4 tag raises the file's effective view
	// rank to R4, so the R1 file must be hidden entirely.
	r3 := &AuthUser{ID: 99, Username: "r3", Rank: "R3"}
	if got := call(r3); len(got) != 0 {
		t.Fatalf("R3 viewer: expected 0 files (tag hides it), got %d", len(got))
	}

	// R4 viewer: meets the tag's rank, sees the file WITH the badge.
	r4 := &AuthUser{ID: 98, Username: "r4", Rank: "R4"}
	got := call(r4)
	if len(got) != 1 {
		t.Fatalf("R4 viewer: expected 1 file, got %d", len(got))
	}
	if len(got[0].Tags) != 1 || got[0].Tags[0].Name != "Violations" {
		t.Fatalf("R4 viewer: expected Violations badge, got %+v", got[0].Tags)
	}

	// Owner (r1user, id 11) is R1 but owns the file: sees it via owner bypass, but the
	// restricted badge is filtered out (their rank is below the tag's min_rank).
	owner := &AuthUser{ID: 11, Username: "r1user", Rank: "R1"}
	got = call(owner)
	if len(got) != 1 {
		t.Fatalf("owner: expected 1 file via owner bypass, got %d", len(got))
	}
	if len(got[0].Tags) != 0 {
		t.Fatalf("owner (R1): restricted badge should be hidden, got %+v", got[0].Tags)
	}
	if got[0].UpdatedAt == "" {
		t.Fatalf("owner: updated_at should be populated (COALESCE), got empty")
	}
}

// TestResolveSubmittedTagsRejectsInvisible proves the upload/update guard: a tag the
// caller can't see (or that doesn't exist) is rejected, not silently dropped.
func TestResolveSubmittedTagsRejectsInvisible(t *testing.T) {
	setupFileTagsTestDB(t)

	res, _ := db.Exec(`INSERT INTO file_tags (name, min_rank, color) VALUES ('Secret', 'R5', 'danger')`)
	secretID, _ := res.LastInsertId()
	res, _ = db.Exec(`INSERT INTO file_tags (name, min_rank, color) VALUES ('Public', 'R1', 'info')`)
	publicID, _ := res.LastInsertId()

	// R3 submitting the R5 tag → not visible → ok=false.
	if _, _, _, ok, err := resolveSubmittedTags([]int{int(secretID)}, "R3"); err != nil || ok {
		t.Fatalf("R3 submitting R5 tag: expected ok=false, got ok=%v err=%v", ok, err)
	}
	// R3 submitting the R1 tag → visible → ok=true.
	if out, _, _, ok, err := resolveSubmittedTags([]int{int(publicID)}, "R3"); err != nil || !ok || len(out) != 1 {
		t.Fatalf("R3 submitting R1 tag: expected ok with 1 id, got ok=%v out=%v err=%v", ok, out, err)
	}
	// A nonexistent id is also rejected (not visible ⊇ not existing).
	if _, _, _, ok, _ := resolveSubmittedTags([]int{999999}, "Admin"); ok {
		t.Fatalf("nonexistent id: expected ok=false")
	}
}

// seedRestrictedFile creates an R1 file tagged with an R4 "Violations" tag and returns
// its id. The file's own min_rank is deliberately open — only the tag restricts it.
func seedRestrictedFile(t *testing.T) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO files (title, file_name, file_type, min_rank, min_edit_rank, owner_user_id, updated_at)
		VALUES ('Violation', 'v.png', 'image', 'R1', 'R4', 11, CURRENT_TIMESTAMP)`)
	if err != nil {
		t.Fatalf("insert file: %v", err)
	}
	fileID, _ := res.LastInsertId()
	res, _ = db.Exec(`INSERT INTO file_tags (name, min_rank, color) VALUES ('Violations', 'R4', 'danger')`)
	tagID, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO file_tag_map (file_id, tag_id) VALUES (?, ?)`, fileID, tagID); err != nil {
		t.Fatalf("insert map: %v", err)
	}
	return fileID
}

func withVars(r *http.Request, id int64, user *AuthUser) *http.Request {
	r = mux.SetURLVars(r, map[string]string{"id": strconv.FormatInt(id, 10)})
	return r.WithContext(context.WithValue(r.Context(), authUserKey, user))
}

// TestDownloadFileEffectiveRank is the PM's load-bearing check at the download endpoint:
// the tag must 403 a low-rank viewer directly, not merely hide the file from the list.
func TestDownloadFileEffectiveRank(t *testing.T) {
	setupFileTagsTestDB(t)
	fileID := seedRestrictedFile(t)
	// A real file on disk so the *allowed* path can 200 rather than 404.
	if err := os.WriteFile(filepath.Join(getStoragePath(), "v.png"), []byte("PNGDATA"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// R3 (not owner): the R4 tag forbids the download.
	w := httptest.NewRecorder()
	downloadFile(w, withVars(httptest.NewRequest(http.MethodGet, "/dl", nil), fileID, &AuthUser{ID: 99, Rank: "R3"}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("R3 download: expected 403, got %d", w.Code)
	}

	// R4: allowed, serves the bytes.
	w = httptest.NewRecorder()
	downloadFile(w, withVars(httptest.NewRequest(http.MethodGet, "/dl", nil), fileID, &AuthUser{ID: 98, Rank: "R4"}))
	if w.Code != http.StatusOK || w.Body.String() != "PNGDATA" {
		t.Fatalf("R4 download: expected 200 with bytes, got %d %q", w.Code, w.Body.String())
	}
}

// TestGenerateWOPITokenEffectiveRank mirrors the download check at the WOPI-token endpoint.
func TestGenerateWOPITokenEffectiveRank(t *testing.T) {
	setupFileTagsTestDB(t)
	fileID := seedRestrictedFile(t)

	w := httptest.NewRecorder()
	generateWOPIToken(w, withVars(httptest.NewRequest(http.MethodGet, "/wt", nil), fileID, &AuthUser{ID: 99, Rank: "R3"}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("R3 wopi-token: expected 403, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	generateWOPIToken(w, withVars(httptest.NewRequest(http.MethodGet, "/wt", nil), fileID, &AuthUser{ID: 98, Rank: "R4"}))
	if w.Code != http.StatusOK {
		t.Fatalf("R4 wopi-token: expected 200, got %d", w.Code)
	}
}

// TestCreateFileTagSelfInvisibleGuard proves an officer can't mint a tag above their own
// rank — one they couldn't see, manage, or undo.
func TestCreateFileTagSelfInvisibleGuard(t *testing.T) {
	setupFileTagsTestDB(t)

	body := `{"name":"TopSecret","min_rank":"R5","color":"danger"}`
	// R4 tries to create an R5 tag → 400.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/file-tags", strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), authUserKey, &AuthUser{ID: 98, Username: "r4", Rank: "R4"}))
	createFileTag(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("R4 creating R5 tag: expected 400, got %d (%s)", w.Code, strings.TrimSpace(w.Body.String()))
	}

	// Admin can create it.
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/file-tags", strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), authUserKey, &AuthUser{ID: 1, Username: "admin", IsAdmin: true}))
	createFileTag(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("admin creating R5 tag: expected 201, got %d (%s)", w.Code, strings.TrimSpace(w.Body.String()))
	}
}

// TestWOPICollaboraPortPerHost proves COLLABORA_PORT derives the Collabora domain from
// the request host, so localhost and the LAN IP each get a matching Collabora URL from
// one config.
func TestWOPICollaboraPortPerHost(t *testing.T) {
	setupFileTagsTestDB(t)
	t.Setenv("COLLABORA_DOMAIN", "should-be-ignored:1234")
	t.Setenv("COLLABORA_PORT", "9980")

	res, _ := db.Exec(`INSERT INTO files (title, file_name, file_type, min_rank, min_edit_rank, owner_user_id, updated_at)
		VALUES ('Doc', 'd.docx', 'document', 'R1', 'R4', 10, CURRENT_TIMESTAMP)`)
	fileID, _ := res.LastInsertId()

	check := func(hostURL, want string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, hostURL, nil)
		r = mux.SetURLVars(r, map[string]string{"id": strconv.FormatInt(fileID, 10)})
		r = r.WithContext(context.WithValue(r.Context(), authUserKey, &AuthUser{ID: 1, Username: "admin", IsAdmin: true}))
		generateWOPIToken(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("wopi-token %s: status %d", hostURL, w.Code)
		}
		var out struct {
			CollaboraDomain string `json:"collabora_domain"`
		}
		json.Unmarshal(w.Body.Bytes(), &out)
		if out.CollaboraDomain != want {
			t.Fatalf("host %s: collabora_domain=%q, want %q", hostURL, out.CollaboraDomain, want)
		}
	}
	check("http://localhost:8080/wt", "localhost:9980")
	check("http://10.7.1.16:8080/wt", "10.7.1.16:9980")
}

// TestFileUpdatedByRoundTrip proves an edit stamps updated_by and getFilesList resolves
// it to the editor's username.
func TestFileUpdatedByRoundTrip(t *testing.T) {
	setupFileTagsTestDB(t)

	res, _ := db.Exec(`INSERT INTO files (title, file_name, file_type, min_rank, min_edit_rank, owner_user_id, updated_at)
		VALUES ('Doc', 'd.docx', 'document', 'R1', 'R4', 10, CURRENT_TIMESTAMP)`)
	fileID, _ := res.LastInsertId()

	// User 11 (seeded as "r1user") edits it.
	body := `{"title":"Doc v2","min_rank":"R1","min_edit_rank":"R4"}`
	w := httptest.NewRecorder()
	r := withVars(httptest.NewRequest(http.MethodPut, "/f", strings.NewReader(body)), fileID,
		&AuthUser{ID: 11, Username: "r1user", IsAdmin: true})
	updateFile(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d (%s)", w.Code, strings.TrimSpace(w.Body.String()))
	}

	// The roster now reports the editor's name.
	w2 := httptest.NewRecorder()
	getFilesList(w2, reqAs(&AuthUser{ID: 99, Username: "admin", IsAdmin: true}))
	var out []AllianceFile
	if err := json.Unmarshal(w2.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var f *AllianceFile
	for i := range out {
		if int64(out[i].ID) == fileID {
			f = &out[i]
		}
	}
	if f == nil {
		t.Fatal("file not in list")
	}
	if f.UpdatedByName != "r1user" {
		t.Fatalf("updated_by_name = %q, want r1user", f.UpdatedByName)
	}
}

// TestCreateBlankFile exercises the "Create New" flow end-to-end: a valid OOXML file
// lands on disk, the DB row carries the right type, and the tag is attached.
func TestCreateBlankFile(t *testing.T) {
	setupFileTagsTestDB(t)

	res, _ := db.Exec(`INSERT INTO file_tags (name, min_rank, color) VALUES ('Guides','R1','info')`)
	tagID, _ := res.LastInsertId()

	body := fmt.Sprintf(`{"title":"My Sheet","kind":"spreadsheet","min_rank":"R1","min_edit_rank":"R4","tag_ids":[%d]}`, tagID)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/files/create", strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), authUserKey, &AuthUser{ID: 10, Username: "r5user", Rank: "R5"}))
	createBlankFile(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("create: expected 200, got %d (%s)", w.Code, strings.TrimSpace(w.Body.String()))
	}

	var out struct {
		ID       int64  `json:"id"`
		Ext      string `json:"ext"`
		FileType string `json:"file_type"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Ext != ".xlsx" || out.FileType != "spreadsheet" {
		t.Fatalf("unexpected response: %+v", out)
	}

	// DB row with the right type.
	var fileName, ftype string
	if err := db.QueryRow(`SELECT file_name, file_type FROM files WHERE id=?`, out.ID).Scan(&fileName, &ftype); err != nil {
		t.Fatalf("file row: %v", err)
	}
	if ftype != "spreadsheet" {
		t.Fatalf("file_type = %q, want spreadsheet", ftype)
	}

	// The bytes on disk are a valid zip (OOXML container).
	data, err := os.ReadFile(filepath.Join(getStoragePath(), fileName))
	if err != nil {
		t.Fatalf("read created file: %v", err)
	}
	if _, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("created file is not a valid zip: %v", err)
	}

	// The submitted tag is attached.
	var cnt int
	db.QueryRow(`SELECT COUNT(*) FROM file_tag_map WHERE file_id=? AND tag_id=?`, out.ID, tagID).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("tag not attached, count=%d", cnt)
	}
}
