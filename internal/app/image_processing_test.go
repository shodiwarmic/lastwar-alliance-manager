package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ocrTestStrPtr(s string) *string { return &s }

// TestDecodeWorkerResponse_BareMapRejected: the pre-envelope bare category map
// is no longer supported — a response without a top-level "results" key is an
// error (the backward-compat shim was removed once the OCR service shipped the
// envelope everywhere).
func TestDecodeWorkerResponse_BareMapRejected(t *testing.T) {
	body := `{"power":[{"player_name":"Alice","score":5000}],"kills":[{"player_name":"Bob","score":12}]}`
	_, _, err := decodeWorkerResponse(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for bare category map (no 'results' envelope), got nil")
	}
	if !strings.Contains(err.Error(), "results") {
		t.Errorf("expected a 'results envelope' error, got: %v", err)
	}
}

// TestDecodeWorkerResponse_Envelope: the new {results, diagnostics} envelope
// yields players from results and an opaque diagnostics blob.
func TestDecodeWorkerResponse_Envelope(t *testing.T) {
	body := `{"results":{"power":[{"player_name":"Alice","score":5000}]},"diagnostics":{"engine":"cloud_vision","image_count":3,"schema_version":1}}`
	res, diag, err := decodeWorkerResponse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res["power"]) != 1 || res["power"][0].PlayerName != "Alice" {
		t.Errorf("results not parsed: %+v", res)
	}
	if len(diag) == 0 {
		t.Fatal("expected diagnostics blob, got empty")
	}
	d := parseOCRDiagnostics(diag)
	if d == nil || d.Engine != "cloud_vision" || d.ImageCount != 3 {
		t.Errorf("diagnostics not parsed: %+v", d)
	}
}

// TestDecodeWorkerResponse_EmptyEnvelope: empty extraction still carries
// diagnostics (the case where they are most useful) plus a warning.
func TestDecodeWorkerResponse_EmptyEnvelope(t *testing.T) {
	body := `{"results":{},"diagnostics":{"engine":"cloud_vision","image_count":0},"warning":"No player data extracted"}`
	res, diag, err := decodeWorkerResponse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("expected empty results, got %+v", res)
	}
	if len(diag) == 0 {
		t.Error("expected diagnostics even on empty result")
	}
}

func TestParseOCRDiagnostics_Absent(t *testing.T) {
	if parseOCRDiagnostics(nil) != nil {
		t.Error("expected nil for absent diagnostics")
	}
	if parseOCRDiagnostics(json.RawMessage("")) != nil {
		t.Error("expected nil for empty diagnostics")
	}
}

func TestSummarizeOCRDiagnostics_Nil(t *testing.T) {
	if got := summarizeOCRDiagnostics(nil); got != "" {
		t.Errorf("expected empty string for nil diagnostics, got %q", got)
	}
}

func TestSummarizeOCRDiagnostics_Rollup(t *testing.T) {
	d := &OCRDiagnostics{Engine: "cloud_vision", ImageCount: 14}
	for i := 0; i < 12; i++ {
		d.Sections = append(d.Sections, OCRSectionDiagnostic{
			Image: "img", Category: ocrTestStrPtr("monday"), Confidence: 0.95,
			Method: "day_color_saturation", PlayersFound: 8,
		})
	}
	for i := 0; i < 2; i++ {
		d.Sections = append(d.Sections, OCRSectionDiagnostic{
			Image: "img", Category: ocrTestStrPtr("thursday"), Confidence: 0.75,
			Method: "day_text_fallback", PlayersFound: 8,
		})
	}
	d.Sections = append(d.Sections, OCRSectionDiagnostic{
		Image: "img", Method: "unclassified", PlayersFound: 0, Note: ocrTestStrPtr("no_players"),
	})

	got := summarizeOCRDiagnostics(d)
	for _, want := range []string{
		"OCR cloud_vision, 14 imgs",
		"12×day_color_saturation@0.95",
		"2×day_text_fallback@0.75",
		"1 no_players",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q missing %q", got, want)
		}
	}
}

// TestSummarizeOCRDiagnostics_NilPointersSafe: Category/Note arrive nil when the
// OCR service strips them (model_dump(exclude_none=True)); must not panic.
func TestSummarizeOCRDiagnostics_NilPointersSafe(t *testing.T) {
	d := &OCRDiagnostics{
		Engine:     "paddleocr",
		ImageCount: 1,
		Sections: []OCRSectionDiagnostic{
			{Image: "img", Confidence: 1.0, Method: "category_override", PlayersFound: 5},
		},
	}
	got := summarizeOCRDiagnostics(d)
	if !strings.Contains(got, "OCR paddleocr, 1 imgs") {
		t.Errorf("unexpected summary: %q", got)
	}
}

// --- Contract v1 (Project 11) -------------------------------------------------

// setupOCRTestDB opens a fresh database and points the OCR settings at url.
func setupOCRTestDB(t *testing.T, mode OCRBackendMode, url string) {
	t.Helper()
	prev := db
	t.Setenv("DATABASE_PATH", filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("STORAGE_PATH", t.TempDir())
	t.Setenv("SESSION_KEY", "test-session-key-at-least-32-chars-long")
	if err := initDB(); err != nil {
		t.Fatalf("initDB: %v", err)
	}
	if _, err := db.Exec(`UPDATE settings SET ocr_backend_mode = ?, cv_worker_url = ?, ocr_archive_mode = 'none' WHERE id = 1`, string(mode), url); err != nil {
		t.Fatalf("settings: %v", err)
	}
	prevClient := newCloudOCRClient
	newCloudOCRClient = func(ctx context.Context, workerURL string) (*http.Client, func(), error) {
		return &http.Client{Timeout: 10 * time.Second}, func() {}, nil
	}
	invalidateOCRServiceInfo()
	t.Cleanup(func() {
		newCloudOCRClient = prevClient
		invalidateOCRServiceInfo()
		if db != nil {
			db.Close()
		}
		db = prev
	})
}

// testUploads builds n multipart file headers, as a handler would receive them.
func testUploads(t *testing.T, n int) []*multipart.FileHeader {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for i := 0; i < n; i++ {
		part, _ := w.CreateFormFile("images", fmt.Sprintf("f%d.png", i))
		part.Write([]byte("not really a png"))
	}
	w.Close()
	r := httptest.NewRequest("POST", "/", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	return r.MultipartForm.File["images"]
}

type seenRequest struct {
	category      string
	hasCategory   bool
	schemaVersion string
}

// fakeOCRService answers /process-batch with `reply` (status, body) and /health
// with `health`, recording what each batch request carried.
func fakeOCRService(t *testing.T, status int, reply, health string) (*httptest.Server, *[]seenRequest) {
	t.Helper()
	var seen []seenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			if health == "" {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			io.WriteString(w, health)
		case "/process-batch":
			r.ParseMultipartForm(1 << 20)
			_, has := r.MultipartForm.Value["category"]
			seen = append(seen, seenRequest{r.FormValue("category"), has, r.FormValue("schema_version")})
			w.WriteHeader(status)
			io.WriteString(w, reply)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

const okReply = `{"schema_version": 1, "results": {"zombie_siege": [{"player_name": "A", "score": 0, "rank": 98, "score_unread": true}]}}`

func TestOCRContract_SchemaVersionSentInBothModes(t *testing.T) {
	for _, mode := range []OCRBackendMode{OCRBackendCloud, OCRBackendLocal} {
		t.Run(string(mode), func(t *testing.T) {
			srv, seen := fakeOCRService(t, 200, okReply, "")
			setupOCRTestDB(t, mode, srv.URL)
			if _, _, err := ProcessImagesForCategory(context.Background(), testUploads(t, 1), "zombie_siege"); err != nil {
				t.Fatal(err)
			}
			if got := (*seen)[0]; got.schemaVersion != "1" || got.category != "zombie_siege" {
				t.Errorf("request carried %+v, want schema_version 1 and the category", got)
			}
		})
	}
}

func TestOCRContract_VSCloudPathSendsNoCategory(t *testing.T) {
	// The VS upload keeps auto-detection in cloud mode, so a stray screenshot in
	// a batch is read for what it is, not forced into the selected category.
	srv, seen := fakeOCRService(t, 200, `{"results": {}}`, "")
	setupOCRTestDB(t, OCRBackendCloud, srv.URL)
	if _, _, err := ProcessImages(context.Background(), testUploads(t, 1), "friday"); err != nil {
		t.Fatal(err)
	}
	if (*seen)[0].hasCategory {
		t.Errorf("cloud VS request carried category %q", (*seen)[0].category)
	}
}

func TestOCRContract_MissingVersionIsV1(t *testing.T) {
	res, _, err := decodeWorkerResponse(strings.NewReader(`{"results": {"weekly": [{"player_name": "A", "score": 5}]}}`))
	if err != nil || len(res["weekly"]) != 1 {
		t.Fatalf("res=%v err=%v", res, err)
	}
}

func TestOCRContract_OtherVersionRefused(t *testing.T) {
	_, _, err := decodeWorkerResponse(strings.NewReader(`{"schema_version": 2, "results": {}}`))
	var refusal *OCRServiceError
	if !errors.As(err, &refusal) || !strings.Contains(err.Error(), "version 2") || !strings.Contains(err.Error(), "version 1") {
		t.Fatalf("err = %v, want a refusal naming both versions", err)
	}
}

func TestOCRContract_CodedRefusalSurfacedAsItsSentence(t *testing.T) {
	srv, _ := fakeOCRService(t, 400, `{"error": "Unknown category 'zombie_siege'.", "code": "category_not_supported"}`, "")
	setupOCRTestDB(t, OCRBackendLocal, srv.URL)
	_, _, err := ProcessImagesForCategory(context.Background(), testUploads(t, 1), "zombie_siege")
	var refusal *OCRServiceError
	if !errors.As(err, &refusal) || refusal.Code != "category_not_supported" || strings.Contains(err.Error(), "status 400") {
		t.Fatalf("err = %v (%T), want the service's sentence and code", err, err)
	}
}

func TestOCRContract_RankSurvivesTheArchiveMarshal(t *testing.T) {
	res, _, err := decodeWorkerResponse(strings.NewReader(okReply))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(res)
	for _, want := range []string{`"rank":98`, `"score_unread":true`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("archived JSON %s lacks %s", out, want)
		}
	}
}

func TestOCRServiceInfo(t *testing.T) {
	t.Run("a service without capabilities reads as v1 ranking screens", func(t *testing.T) {
		srv, _ := fakeOCRService(t, 200, "", `{"status": "ok"}`)
		setupOCRTestDB(t, OCRBackendCloud, srv.URL)
		info, err := ocrServiceInfo(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !info.Legacy || !info.Speaks() || info.Reads("zombie_siege") || !info.Reads("weekly") || len(info.Categories) != 23 {
			t.Errorf("info = %+v", info)
		}
	})
	t.Run("a current service", func(t *testing.T) {
		srv, _ := fakeOCRService(t, 200, "", `{"status":"ok","version":"v1.0.0","commit":"abc","schema_versions":[1],"categories":["weekly","zombie_siege"]}`)
		setupOCRTestDB(t, OCRBackendLocal, srv.URL)
		info, err := ocrServiceInfo(context.Background())
		if err != nil || info.Legacy || !info.Reads("zombie_siege") || info.Version != "v1.0.0" {
			t.Fatalf("info = %+v, err = %v", info, err)
		}
	})
	t.Run("unreachable is its own error and is not cached", func(t *testing.T) {
		healthy := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !healthy {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			io.WriteString(w, `{"status":"ok","schema_versions":[1],"categories":["weekly"]}`)
		}))
		t.Cleanup(srv.Close)
		setupOCRTestDB(t, OCRBackendCloud, srv.URL)
		_, err := ocrServiceInfo(context.Background())
		var down *OCRUnreachableError
		if !errors.As(err, &down) {
			t.Fatalf("err = %v, want OCRUnreachableError", err)
		}
		healthy = true
		if _, err := ocrServiceInfo(context.Background()); err != nil {
			t.Fatalf("after recovery: %v (a failure must not be cached)", err)
		}
	})
	t.Run("cached per mode and URL", func(t *testing.T) {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			io.WriteString(w, `{"status":"ok","schema_versions":[1],"categories":["weekly"]}`)
		}))
		t.Cleanup(srv.Close)
		setupOCRTestDB(t, OCRBackendCloud, srv.URL)
		ocrServiceInfo(context.Background())
		ocrServiceInfo(context.Background())
		if calls != 1 {
			t.Fatalf("calls = %d, want 1 (cached)", calls)
		}
		db.Exec(`UPDATE settings SET ocr_backend_mode = 'local' WHERE id = 1`)
		ocrServiceInfo(context.Background())
		if calls != 2 {
			t.Fatalf("calls = %d, want 2 after the mode changed", calls)
		}
	})
}
