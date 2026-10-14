package app

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestGetUpdateStateShape(t *testing.T) {
	setupPreviewTestDB(t)
	t.Setenv("UPDATE_CHECK", "")
	t.Setenv("HOST_FILES_VERSION", "v2.4.0")
	t.Setenv("APP_VERSION", "v2.4.0")
	prev := appVersion
	appVersion = "v2.4.0"
	t.Cleanup(func() { appVersion = prev })
	db.Exec(`UPDATE release_state SET latest_tag = 'v3.0.0', latest_name = 'v3.0.0', latest_url = 'https://x/r',
		latest_body = 'notes', checked_at = datetime('now')`)

	w := serveRouter(t, "GET", "/api/admin/update", "", sessionCookie(t, 1, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		Running struct {
			Version, Commit, HostFiles, AppVersion string
		}
		Check     string `json:"check"`
		CheckedAt string `json:"checked_at"`
		Latest    *struct {
			Tag, URL, Notes, Level string
			NotesTruncated         bool `json:"notes_truncated"`
		} `json:"latest"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	var raw map[string]map[string]any
	json.Unmarshal(w.Body.Bytes(), &raw)
	if raw["running"]["host_files"] != "v2.4.0" || raw["running"]["app_version"] != "v2.4.0" || raw["running"]["version"] != "v2.4.0" {
		t.Errorf("running = %v", raw["running"])
	}
	if out.Check != "on" || out.CheckedAt == "" || out.Latest == nil || out.Latest.Tag != "v3.0.0" ||
		out.Latest.Level != "major" || out.Latest.Notes != "notes" || out.Latest.NotesTruncated {
		t.Errorf("payload = %s", w.Body.String())
	}

	// An officer is refused: the route is an admin's.
	if w := serveRouter(t, "GET", "/api/admin/update", "", sessionCookie(t, 3, "")); w.Code != http.StatusForbidden {
		t.Errorf("officer: %d", w.Code)
	}
}

func TestUpdateStateTruncatesLongNotes(t *testing.T) {
	setupPreviewTestDB(t)
	long := make([]byte, releaseNotesCap+50)
	for i := range long {
		long[i] = 'a'
	}
	db.Exec(`UPDATE release_state SET latest_tag = 'v3.0.0', latest_body = ?`, string(long))
	out, err := updateStatePayload()
	if err != nil {
		t.Fatal(err)
	}
	latest := out["latest"].(map[string]any)
	if n := len(latest["notes"].(string)); n != releaseNotesCap || latest["notes_truncated"] != true {
		t.Errorf("notes %d chars, truncated %v", n, latest["notes_truncated"])
	}
}

func TestCheckNowRoute(t *testing.T) {
	setupPreviewTestDB(t)
	admin := sessionCookie(t, 1, "")
	hits := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, fakeReleaseJSON) })

	t.Setenv("UPDATE_CHECK", "off")
	if w := serveRouter(t, "POST", "/api/admin/update/check", "", admin); w.Code != http.StatusConflict {
		t.Errorf("checks off: %d %s, want 409", w.Code, w.Body.String())
	}
	if hits.Load() != 0 {
		t.Fatal("a request reached GitHub with checks off")
	}

	t.Setenv("UPDATE_CHECK", "")
	w := serveRouter(t, "POST", "/api/admin/update/check", "", admin)
	if w.Code != http.StatusOK || hits.Load() != 1 {
		t.Fatalf("check now: %d %s, hits %d", w.Code, w.Body.String(), hits.Load())
	}
	var out struct {
		Latest struct{ Tag string } `json:"latest"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Latest.Tag != "v9.1.0" {
		t.Errorf("the answer should carry what was learned: %s", w.Body.String())
	}
	if w := serveRouter(t, "POST", "/api/admin/update/check", "", admin); w.Code != http.StatusTooManyRequests {
		t.Errorf("a second check inside a minute: %d, want 429", w.Code)
	}
	if hits.Load() != 1 {
		t.Errorf("hits = %d, want 1", hits.Load())
	}
}

func TestUpdateAvailableNudgesOnlyANewerRelease(t *testing.T) {
	setupNameMatchTestDB(t)
	t.Setenv("UPDATE_CHECK", "")
	prev := appVersion
	t.Cleanup(func() { appVersion = prev })
	db.Exec(`UPDATE release_state SET latest_tag = 'v2.5.0'`)
	for _, c := range []struct {
		running string
		admin   bool
		want    string
	}{
		{"v2.4.0", true, "v2.5.0"},
		{"v2.4.0", false, ""},
		{"v2.5.0", true, ""},
		{"dev", true, ""},
		{"edge", true, ""},
	} {
		appVersion = c.running
		if got := updateAvailableFor(c.admin); got != c.want {
			t.Errorf("running %s, admin %v: %q, want %q", c.running, c.admin, got, c.want)
		}
	}
}
