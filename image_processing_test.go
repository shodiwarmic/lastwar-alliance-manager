package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func ocrTestStrPtr(s string) *string { return &s }

// TestDecodeWorkerResponse_Legacy: the pre-envelope bare category map still
// parses, with nil diagnostics (the temporary-shim backward-compat path).
func TestDecodeWorkerResponse_Legacy(t *testing.T) {
	body := `{"power":[{"player_name":"Alice","score":5000}],"kills":[{"player_name":"Bob","score":12}]}`
	res, diag, err := decodeWorkerResponse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diag != nil {
		t.Errorf("expected nil diagnostics on legacy response, got %s", diag)
	}
	if len(res["power"]) != 1 || res["power"][0].PlayerName != "Alice" || res["power"][0].Score != 5000 {
		t.Errorf("power not parsed correctly: %+v", res["power"])
	}
	if len(res["kills"]) != 1 || res["kills"][0].PlayerName != "Bob" {
		t.Errorf("kills not parsed correctly: %+v", res["kills"])
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
