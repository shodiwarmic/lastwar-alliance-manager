package app

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// staticLike serves static/ the way the production branch of the catch-all does: an ETag,
// then http.FileServer (which answers If-None-Match with 304).
func staticLike() http.Handler {
	files := http.FileServer(http.Dir(staticDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"abc12345"`)
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}

func gzGet(t *testing.T, h http.Handler, path string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func gunzip(t *testing.T, b []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	return out
}

func TestGzipCompressesStaticCSS(t *testing.T) {
	h := gzipMiddleware(staticLike())
	w := gzGet(t, h, "/styles.css", map[string]string{"Accept-Encoding": "gzip"})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if !strings.Contains(w.Header().Get("Vary"), "Accept-Encoding") {
		t.Errorf("Vary = %q, want Accept-Encoding", w.Header().Get("Vary"))
	}
	if !bytes.Contains(gunzip(t, w.Body.Bytes()), []byte("--bg-primary")) {
		t.Error("decompressed body is not styles.css")
	}

	// A client that does not ask for gzip gets the plain file.
	w = gzGet(t, h, "/styles.css", nil)
	if w.Header().Get("Content-Encoding") != "" {
		t.Errorf("compressed for a client that did not accept it")
	}
}

// The ETag survives compression unchanged, so revalidation still yields a 304 whether or
// not the client accepts gzip.
func TestGzipKeepsTheETagRoundTrip(t *testing.T) {
	h := gzipMiddleware(staticLike())
	for _, ae := range []string{"gzip", ""} {
		w := gzGet(t, h, "/styles.css", map[string]string{"Accept-Encoding": ae})
		etag := w.Header().Get("ETag")
		if etag != `"abc12345"` {
			t.Fatalf("Accept-Encoding %q: ETag = %q", ae, etag)
		}
		w = gzGet(t, h, "/styles.css", map[string]string{"Accept-Encoding": ae, "If-None-Match": etag})
		if w.Code != http.StatusNotModified {
			t.Errorf("Accept-Encoding %q: revalidation = %d, want 304", ae, w.Code)
		}
	}
}

func TestGzipLeavesNonTextAndSmallResponsesAlone(t *testing.T) {
	big := bytes.Repeat([]byte("x"), 8192)
	for name, tc := range map[string]struct {
		ctype string
		body  []byte
	}{
		"png":   {"image/png", big},
		"zip":   {"application/zip", big},
		"docx":  {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", big},
		"octet": {"application/octet-stream", big},
		"small": {"application/json", []byte(`{"ok":true}`)},
	} {
		h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", tc.ctype)
			w.Write(tc.body)
		}))
		w := gzGet(t, h, "/x", map[string]string{"Accept-Encoding": "gzip"})
		if ce := w.Header().Get("Content-Encoding"); ce != "" {
			t.Errorf("%s: Content-Encoding = %q, want none", name, ce)
		}
		if !bytes.Equal(w.Body.Bytes(), tc.body) {
			t.Errorf("%s: body altered", name)
		}
	}
}

func TestGzipJSONRoundTrip(t *testing.T) {
	rows := make([]map[string]any, 200)
	for i := range rows {
		rows[i] = map[string]any{"id": i, "name": "member", "power": 123456789}
	}
	want, _ := json.Marshal(rows)
	h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(want)
	}))
	w := gzGet(t, h, "/api/members", map[string]string{"Accept-Encoding": "gzip"})
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("JSON not compressed")
	}
	if got := gunzip(t, w.Body.Bytes()); !bytes.Equal(got, want) {
		t.Error("decompressed JSON differs from the original")
	}
}

// A handler that already encoded its response is passed through, not double-encoded.
func TestGzipPassesThroughPreEncoded(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(bytes.Repeat([]byte("y"), 4096))
	zw.Close()
	h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Encoding", "gzip")
		w.Write(buf.Bytes())
	}))
	w := gzGet(t, h, "/x", map[string]string{"Accept-Encoding": "gzip"})
	if !bytes.Equal(w.Body.Bytes(), buf.Bytes()) {
		t.Error("pre-encoded body was re-encoded")
	}
}
