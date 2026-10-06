package app

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"lastwar-alliance/internal/fixtures"
)

// The demo's fixtures service must answer in the contract this app reads: its /health
// through the app's capability check, and its canned boards through the app's own decoder.
func TestFixturesSpeakTheOCRContract(t *testing.T) {
	srv := httptest.NewServer(fixtures.Handler(fixtures.Config{}))
	defer srv.Close()
	setupOCRTestDB(t, OCRBackendLocal, srv.URL)

	info, err := fetchOCRServiceInfo(context.Background(), OCRBackendLocal, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Speaks() || info.Legacy {
		t.Errorf("fixtures do not speak contract v%d: %+v", ocrContractVersion, info)
	}
	for _, c := range []string{"desert_storm", "monday", "power"} {
		if !info.Reads(c) {
			t.Errorf("fixtures do not read %s", c)
		}
	}

	// postProcessBatch is the app's one /process-batch call: it decodes the body with
	// decodeWorkerResponse and the diagnostics with parseOCRDiagnostics.
	for _, cat := range []string{"monday", "alliance_exercise"} {
		res, d, err := postProcessBatch(context.Background(), OCRBackendLocal, testUploads(t, 1), srv.URL, cat)
		if err != nil {
			t.Fatalf("%s: %v", cat, err)
		}
		if len(res[cat]) == 0 {
			t.Errorf("%s: no rows", cat)
		}
		if d == nil || d.Engine != "demo-canned" || len(d.Sections) != 1 {
			t.Fatalf("%s: diagnostics %+v", cat, d)
		}
		if cat == "alliance_exercise" {
			if ts, why := mailTimestamp(d.Sections); ts == "" {
				t.Errorf("the mail import would find no timestamp: %s", why)
			}
		}
		if summarizeOCRDiagnostics(d) == "" {
			t.Errorf("%s: empty activity-log summary", cat)
		}
	}

	// A batch with no category is refused in the service's own words.
	_, _, err = postProcessBatch(context.Background(), OCRBackendLocal, testUploads(t, 1), srv.URL, "")
	var se *OCRServiceError
	if !errors.As(err, &se) || se.Code != "category_required" {
		t.Errorf("no category: err = %v", err)
	}
}
