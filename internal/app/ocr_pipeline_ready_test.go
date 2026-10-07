package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOCRPipelineReady(t *testing.T) {
	for _, tc := range []struct {
		mode string
		key  bool
		url  string
		want bool
	}{
		{"local", false, "http://ocr-local:8080", true},
		{"local", false, "", false},
		{"cloud", false, "https://ocr.example", false},
		{"cloud", true, "https://ocr.example", true},
		{"cloud", true, "", false},
		{"", true, "https://ocr.example", true}, // an unset mode is cloud
	} {
		if got := ocrPipelineReady(tc.mode, tc.key, tc.url); got != tc.want {
			t.Errorf("ocrPipelineReady(%q, %v, %q) = %v, want %v", tc.mode, tc.key, tc.url, got, tc.want)
		}
	}
}

// The settings API is what the Upload page gates on: in local mode with a sidecar URL and
// no GCP key it must say ready.
func TestSettingsSayLocalOCRIsReadyWithoutAKey(t *testing.T) {
	setupOCRTestDB(t, OCRBackendLocal, "http://ocr-local:8080")
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req = req.WithContext(context.WithValue(req.Context(), authUserKey, &AuthUser{ID: 1, Username: "a", IsAdmin: true}))
	w := httptest.NewRecorder()
	getSettings(w, req)
	var body struct {
		Ready bool `json:"ocr_pipeline_ready"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if !body.Ready {
		t.Error("local mode with a sidecar URL and no GCP key reports the pipeline not ready")
	}
}
