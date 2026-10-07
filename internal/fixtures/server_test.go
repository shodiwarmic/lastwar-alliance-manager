package fixtures

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lastwar-alliance/internal/demo"
)

func batch(t *testing.T, fields map[string]string, images int) *http.Request {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for i := 0; i < images; i++ {
		fw, _ := mw.CreateFormFile("images", "shot.png")
		fw.Write([]byte("\x89PNG not really"))
	}
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/process-batch", &b)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestHealth(t *testing.T) {
	w := httptest.NewRecorder()
	Handler(Config{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	var h struct {
		SchemaVersions []int    `json:"schema_versions"`
		Categories     []string `json:"categories"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &h); err != nil {
		t.Fatal(err)
	}
	if len(h.SchemaVersions) != 1 || h.SchemaVersions[0] != 1 {
		t.Errorf("schema_versions = %v", h.SchemaVersions)
	}
	if len(h.Categories) != 26 {
		t.Errorf("%d categories, want 26", len(h.Categories))
	}
}

func TestEveryCategoryNamesOnlyDemoMembers(t *testing.T) {
	names := map[string]bool{}
	for _, b := range demo.Manifest().Boards {
		for _, r := range b {
			names[r.Name] = true
		}
	}
	h := Handler(Config{})
	for _, cat := range Categories() {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, batch(t, map[string]string{"category": cat, "schema_version": "1"}, 2))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", cat, w.Code, w.Body.String())
		}
		var resp struct {
			SchemaVersion int `json:"schema_version"`
			Results       map[string][]struct {
				Name string `json:"player_name"`
			} `json:"results"`
			Diagnostics struct {
				Engine   string `json:"engine"`
				Sections []struct {
					Note          string  `json:"note"`
					MailTimestamp *string `json:"mail_timestamp"`
				} `json:"sections"`
			} `json:"diagnostics"`
		}
		json.Unmarshal(w.Body.Bytes(), &resp)
		rows := resp.Results[cat]
		if resp.SchemaVersion != 1 || len(rows) == 0 || resp.Diagnostics.Engine != "demo-canned" || len(resp.Diagnostics.Sections) != 2 {
			t.Errorf("%s: unexpected envelope %s", cat, w.Body.String()[:min(200, w.Body.Len())])
			continue
		}
		for _, r := range rows {
			if !names[r.Name] {
				t.Errorf("%s: %q is not a demo member", cat, r.Name)
			}
		}
		isMail := cat == "alliance_exercise" || cat == "zombie_siege" || cat == "desert_storm"
		if got := resp.Diagnostics.Sections[0].MailTimestamp != nil; got != isMail {
			t.Errorf("%s: mail_timestamp present = %v", cat, got)
		}
	}
}

func TestRefusals(t *testing.T) {
	h := Handler(Config{})
	for name, tc := range map[string]struct {
		fields map[string]string
		code   string
	}{
		"no category":      {map[string]string{}, "category_required"},
		"unknown category": {map[string]string{"category": "sunday"}, "category_not_supported"},
		"schema 2":         {map[string]string{"category": "monday", "schema_version": "2"}, "schema_not_supported"},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, batch(t, tc.fields, 1))
		var body struct{ Error, Code string }
		json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != http.StatusBadRequest || body.Code != tc.code || body.Error == "" {
			t.Errorf("%s: %d %+v", name, w.Code, body)
		}
	}
}

func TestCollaboraPicksTheFilesPicture(t *testing.T) {
	h := Handler(Config{FrameAncestors: "https://demo.example"})
	for _, tc := range []struct {
		src, theme, want string
	}{
		{"http://alliance-manager:8080/wopi/files/2", "light", "/img/spreadsheet-light.png"},
		{"http://alliance-manager:8080/wopi/files/1", "dark", "/img/document-dark.png"},
		{"http://alliance-manager:8080/wopi/files/99", "dark", "/img/generic-dark.png"},
		{"garbage", "", "/img/generic-light.png"},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/browser/dist/cool.html?WOPISrc="+tc.src+"&theme="+tc.theme, strings.NewReader("access_token=x"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `src="`+tc.want+`"`) {
			t.Errorf("%s/%s: %d, want %s", tc.src, tc.theme, w.Code, tc.want)
		}
		if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors https://demo.example") {
			t.Errorf("CSP = %q", csp)
		}
		if w.Header().Get("X-Frame-Options") != "" {
			t.Error("X-Frame-Options set: the page must be embeddable by the app")
		}
	}
	// Every picture the page can name is served.
	for _, img := range []string{"document-light", "document-dark", "spreadsheet-light", "spreadsheet-dark", "generic-light", "generic-dark"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/img/"+img+".png", nil))
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" {
			t.Errorf("%s: %d", img, w.Code)
		}
	}
}
