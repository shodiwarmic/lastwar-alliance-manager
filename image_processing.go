package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"google.golang.org/api/idtoken"
	"google.golang.org/api/option"
)

// OCRBackendMode reflects the value stored in `settings.ocr_backend_mode`.
// `cloud` uses Google Cloud Vision via OIDC; `local` uses the PaddleOCR
// sidecar via plain HTTP. See migration 032 and image_processing.go's two
// ProcessImagesVia*Worker functions.
type OCRBackendMode string

const (
	OCRBackendCloud OCRBackendMode = "cloud"
	OCRBackendLocal OCRBackendMode = "local"
)

// LoadOCRBackendConfig reads the OCR backend mode + worker URL from the
// settings table. Returns ("cloud", url, nil) by default. Handlers should
// call this once and dispatch via ProcessImages.
func LoadOCRBackendConfig() (mode OCRBackendMode, workerURL string, err error) {
	var rawMode string
	err = db.QueryRow(
		"SELECT COALESCE(ocr_backend_mode, 'cloud'), COALESCE(cv_worker_url, '') FROM settings WHERE id = 1",
	).Scan(&rawMode, &workerURL)
	if err != nil {
		return OCRBackendCloud, "", err
	}
	if rawMode != string(OCRBackendLocal) {
		rawMode = string(OCRBackendCloud)
	}
	return OCRBackendMode(rawMode), workerURL, nil
}

// reconcileOCRBackendFromEnv syncs the operator's OCR_BACKEND_MODE choice
// (written to .env by install.sh / update.sh, injected into the container via
// env_file) into the settings table after migrations run. This replaces the old
// racy `sqlite3 UPDATE` those scripts used to run against the DB file from the
// host — doing it in-process and post-migration avoids the WAL-lock race against
// goose and the root-owned -wal/-shm files. Upsert because correctness must not
// depend on the 054 seed migration having run first.
func reconcileOCRBackendFromEnv() {
	mode := os.Getenv("OCR_BACKEND_MODE")
	if mode != string(OCRBackendLocal) && mode != string(OCRBackendCloud) {
		return // unset/invalid → keep the migration default ('cloud')
	}
	if _, err := db.Exec(`
		INSERT INTO settings (id, ocr_backend_mode) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET ocr_backend_mode = excluded.ocr_backend_mode`,
		mode); err != nil {
		slog.Error("reconcileOCRBackendFromEnv: set mode failed", "error", err)
		return
	}
	if mode == string(OCRBackendLocal) {
		// Default the sidecar URL once, without clobbering an admin override.
		if _, err := db.Exec(`UPDATE settings SET cv_worker_url = 'http://ocr-local:8080'
			WHERE id = 1 AND COALESCE(cv_worker_url, '') = ''`); err != nil {
			slog.Error("reconcileOCRBackendFromEnv: default cv_worker_url failed", "error", err)
		}
	}
	slog.Info("OCR backend reconciled from env", "mode", mode)
}

// ProcessImages dispatches to the right OCR backend based on the operator's
// settings. `category` is required for local mode and ignored for cloud
// mode. Wraps both ProcessImagesViaWorker and ProcessImagesViaLocalWorker
// with a single call site so handlers don't re-implement the switch.
func ProcessImages(ctx context.Context, files []*multipart.FileHeader, category string) (CVWorkerResponse, *OCRDiagnostics, error) {
	mode, workerURL, err := LoadOCRBackendConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load OCR backend config: %v", err)
	}
	if workerURL == "" {
		return nil, nil, fmt.Errorf("OCR worker URL is not configured in admin settings")
	}
	if mode == OCRBackendLocal {
		return ProcessImagesViaLocalWorker(ctx, files, workerURL, category)
	}
	return ProcessImagesViaWorker(ctx, files, workerURL)
}

// OCRPlayer represents a single player's parsed score from the OCR worker.
// When a player name ends in digits that run flush against the score, Vision API
// may merge them into one token. In that case the worker enumerates every valid
// comma-grouped split in Candidates (smallest score first). Candidates is nil /
// absent when the name/score boundary is unambiguous.
type OCRPlayer struct {
	PlayerName string      `json:"player_name"`
	Score      int64       `json:"score"`
	Candidates []OCRPlayer `json:"candidates,omitempty"`
}

// CVWorkerResponse maps the categorized UI state (e.g., "monday", "power")
// to the slice of extracted player records.
type CVWorkerResponse map[string][]OCRPlayer

// OCRDiagnostics is the lean typed view of the OCR service's `diagnostics` block
// (the new {results, diagnostics} envelope). It is used ONLY to build the
// activity-log summary — the full blob is archived opaquely as diagnostics.json,
// so schema evolution on the OCR side needs no change here. Pointer fields arrive
// nil when the OCR service omits them (its `model_dump(exclude_none=True)` strips
// nulls); summarizeOCRDiagnostics must nil-check before dereferencing.
type OCRDiagnostics struct {
	SchemaVersion    int                    `json:"schema_version"`
	Engine           string                 `json:"engine"`
	ImageCount       int                    `json:"image_count"`
	BatchCount       int                    `json:"batch_count"`
	CategoryOverride *string                `json:"category_override"`
	Sections         []OCRSectionDiagnostic `json:"sections"`
}

// OCRSectionDiagnostic is one image-region's classification outcome.
type OCRSectionDiagnostic struct {
	Image        string  `json:"image"`
	Category     *string `json:"category"`
	Confidence   float64 `json:"confidence"`
	Method       string  `json:"method"`
	PlayersFound int     `json:"players_found"`
	Note         *string `json:"note"`
}

// decodeWorkerResponse reads either the legacy bare category map OR the new
// {results, diagnostics} envelope, returning the players map plus the opaque
// diagnostics blob (nil on the legacy path).
//
// TEMPORARY: the legacy branch exists only so this repo and lastwar-ocr-service
// don't need a lock-step deploy. Once the OCR service ships the envelope
// everywhere, collapse this to a single envelope decode (see the diagnostics
// plan's "Follow-up").
func decodeWorkerResponse(body io.Reader) (CVWorkerResponse, json.RawMessage, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, nil, err
	}
	// Shallow first pass: probe captures each top-level value as raw bytes
	// (RawMessage does NOT deep-parse the heavy player arrays here).
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, nil, err
	}

	// Envelope iff a top-level "results" key is present ("results" is never a
	// valid category, so this is an unambiguous discriminator).
	if resRaw, isEnvelope := probe["results"]; isEnvelope {
		var results CVWorkerResponse
		if err := json.Unmarshal(resRaw, &results); err != nil {
			return nil, nil, err
		}
		if warnRaw, ok := probe["warning"]; ok {
			var warning string
			json.Unmarshal(warnRaw, &warning)
			if warning != "" {
				slog.Info("ocr worker warning", "warning", warning)
			}
		}
		return results, probe["diagnostics"], nil // diagnostics may be nil; passed through opaque
	}

	// Legacy: the whole body IS the category map. Decode each category's bytes
	// once from probe — no second full-tree parse.
	legacy := make(CVWorkerResponse, len(probe))
	for cat, arr := range probe {
		var players []OCRPlayer
		if err := json.Unmarshal(arr, &players); err != nil {
			return nil, nil, err
		}
		legacy[cat] = players
	}
	return legacy, nil, nil
}

// parseOCRDiagnostics unmarshals the opaque diagnostics blob into the lean typed
// view used for the activity-log summary. Returns nil when the blob is absent
// (legacy OCR response) or unparseable — callers must nil-check.
func parseOCRDiagnostics(raw json.RawMessage) *OCRDiagnostics {
	if len(raw) == 0 {
		return nil
	}
	var d OCRDiagnostics
	if err := json.Unmarshal(raw, &d); err != nil {
		slog.Warn("ocr: could not parse diagnostics blob", "error", err)
		return nil
	}
	return &d
}

// summarizeOCRDiagnostics renders a single compact activity-log line from the
// diagnostics, e.g.:
//
//	OCR cloud_vision, 14 imgs: 12×day_color_saturation@0.95, 2×day_text_fallback@0.75; 1 no_players
//
// Returns "" when d is nil (legacy OCR / temporary-shim path) so callers can
// append unconditionally. Nil-safe on the pointer section fields.
func summarizeOCRDiagnostics(d *OCRDiagnostics) string {
	if d == nil {
		return ""
	}

	engine := d.Engine
	if engine == "" {
		engine = "unknown"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "OCR %s, %d imgs", engine, d.ImageCount)

	// Roll up sections by (method, confidence); count notes separately.
	type mc struct {
		method     string
		confidence float64
	}
	counts := map[mc]int{}
	var order []mc
	notes := map[string]int{}
	var noteOrder []string
	for _, s := range d.Sections {
		method := s.Method
		if method == "" {
			method = "unclassified"
		}
		key := mc{method, s.Confidence}
		if _, ok := counts[key]; !ok {
			order = append(order, key)
		}
		counts[key]++
		if s.Note != nil && *s.Note != "" {
			if _, ok := notes[*s.Note]; !ok {
				noteOrder = append(noteOrder, *s.Note)
			}
			notes[*s.Note]++
		}
	}

	if len(order) > 0 {
		sort.SliceStable(order, func(i, j int) bool {
			if counts[order[i]] != counts[order[j]] {
				return counts[order[i]] > counts[order[j]]
			}
			return order[i].method < order[j].method
		})
		parts := make([]string, 0, len(order))
		for _, k := range order {
			parts = append(parts, fmt.Sprintf("%d×%s@%g", counts[k], k.method, k.confidence))
		}
		b.WriteString(": ")
		b.WriteString(strings.Join(parts, ", "))
	}

	if len(noteOrder) > 0 {
		sort.SliceStable(noteOrder, func(i, j int) bool {
			return notes[noteOrder[i]] > notes[noteOrder[j]]
		})
		parts := make([]string, 0, len(noteOrder))
		for _, n := range noteOrder {
			parts = append(parts, fmt.Sprintf("%d %s", notes[n], n))
		}
		b.WriteString("; ")
		b.WriteString(strings.Join(parts, ", "))
	}

	return b.String()
}

// getDecryptedGCPKey retrieves and decrypts the service account JSON from the database.
func getDecryptedGCPKey() ([]byte, error) {
	var encryptedBlob, nonce []byte

	err := db.QueryRow("SELECT encrypted_blob, nonce FROM credentials WHERE service_name = 'gcp_vision'").Scan(&encryptedBlob, &nonce)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("GCP Vision credentials not configured by admin")
		}
		return nil, fmt.Errorf("database error retrieving credentials: %v", err)
	}

	hexKey := os.Getenv("CREDENTIAL_ENCRYPTION_KEY")
	if hexKey == "" {
		return nil, fmt.Errorf("server encryption key missing (CREDENTIAL_ENCRYPTION_KEY)")
	}

	plaintextJSON, err := Decrypt(encryptedBlob, nonce, hexKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt GCP credentials: %v", err)
	}

	return plaintextJSON, nil
}

// ProcessImagesViaWorker securely packages uploaded screenshots and sends them
// to the Cloud Run Python microservice using Google OIDC authentication.
func ProcessImagesViaWorker(ctx context.Context, files []*multipart.FileHeader, workerURL string) (CVWorkerResponse, *OCRDiagnostics, error) {
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no images provided for processing")
	}

	// Decide whether to capture this request for archival (best-effort). Acquires
	// an archiveSem slot when capturing; the deferred release is panic-safe and is
	// cleared (acquired=false) only when ownership is handed to the archive
	// goroutine below.
	capture, archMode, archBucket := beginOCRArchiveCapture(files)
	acquired := capture
	defer func() {
		if acquired {
			releaseArchiveSlot()
		}
	}()
	var archImgs []archivedImage

	// 1. Retrieve and decrypt the GCP credentials from your database
	plaintextJSON, err := getDecryptedGCPKey()
	if err != nil {
		return nil, nil, fmt.Errorf("authentication failed: %v", err)
	}

	// Securely wipe the plaintext key from memory when this function exits
	defer func() {
		for i := range plaintextJSON {
			plaintextJSON[i] = 0
		}
	}()

	// 2. Create a secure, authenticated HTTP client
	// We explicitly pass the decrypted JSON to the OIDC token generator
	client, err := idtoken.NewClient(ctx, workerURL, option.WithCredentialsJSON(plaintextJSON))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create authenticated GCP client: %v", err)
	}

	// 3. Prepare the multipart form payload
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	for _, fileHeader := range files {
		file, err := fileHeader.Open()
		if err != nil {
			continue // Skip unreadable files
		}

		part, err := writer.CreateFormFile("images", fileHeader.Filename)
		if err != nil {
			file.Close()
			return nil, nil, fmt.Errorf("failed to create form file buffer: %v", err)
		}

		// When archiving, tee the same single read into a per-file buffer — no
		// extra read, no extra latency.
		dst := io.Writer(part)
		var capBuf bytes.Buffer
		if capture {
			dst = io.MultiWriter(part, &capBuf)
		}
		_, err = io.Copy(dst, file)
		file.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to copy image bytes to buffer: %v", err)
		}
		if capture {
			archImgs = append(archImgs, archivedImage{
				name:        fileHeader.Filename,
				contentType: fileHeader.Header.Get("Content-Type"),
				data:        capBuf.Bytes(),
			})
		}
	}

	err = writer.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to close multipart writer: %v", err)
	}

	// 4. Dispatch the request to the Python microservice
	endpoint := fmt.Sprintf("%s/process-batch", workerURL)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, &requestBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create worker request: %v", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("microservice request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("worker returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// 5. Decode the structured JSON response (tolerant of the legacy bare map or
	// the new {results, diagnostics} envelope — see decodeWorkerResponse).
	result, diagJSON, err := decodeWorkerResponse(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode worker JSON response: %v", err)
	}

	// Best-effort archive (cloud backend). Marshal synchronously so the goroutine
	// never shares the live result map; hand off the archiveSem slot to it.
	if capture {
		respJSON, _ := json.Marshal(result)
		keys := make([]string, 0, len(result))
		for k := range result {
			keys = append(keys, k)
		}
		imgs := archImgs
		diag := diagJSON
		acquired = false
		go func() {
			defer releaseArchiveSlot()
			archiveOCRRequest(imgs, respJSON, diag, keys, "cloud", "", archMode, archBucket)
		}()
	}

	return result, parseOCRDiagnostics(diagJSON), nil
}

// ProcessImagesViaLocalWorker is the manual-mode local-OCR counterpart to
// ProcessImagesViaWorker. It posts to a sidecar service running the
// PaddleOCR backend (lastwar-ocr-service Dockerfile.local image) and uses
// the existing `category` form param to skip auto-classification — the
// caller picks the screen / tab via the manual upload UI because
// PaddleOCR's stylised-header OCR isn't reliable enough for
// auto-detection.
//
// Differences from ProcessImagesViaWorker:
//   - No GCP credentials decryption (local sidecar isn't behind OIDC).
//   - Plain http.Client; the local URL is typically http://localhost:8080
//     or the docker-compose service hostname.
//   - `category` is required and must be one of the keys in
//     screen-definitions/catalog.yaml that maps to a real wire-format tab
//     (e.g. "friday", "siege_daily", "power").
func ProcessImagesViaLocalWorker(ctx context.Context, files []*multipart.FileHeader, workerURL, category string) (CVWorkerResponse, *OCRDiagnostics, error) {
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no images provided for processing")
	}
	if category == "" {
		return nil, nil, fmt.Errorf("local OCR mode requires a category (the user-selected screen+tab) — auto-classification is unreliable on PaddleOCR's stylised-header read")
	}

	// Best-effort archival capture (see ProcessImagesViaWorker for the pattern).
	capture, archMode, archBucket := beginOCRArchiveCapture(files)
	acquired := capture
	defer func() {
		if acquired {
			releaseArchiveSlot()
		}
	}()
	var archImgs []archivedImage

	// 1. Multipart payload — same shape as the cloud worker, plus a
	//    'category' form field that the OCR service treats as a
	//    classification override.
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	for _, fileHeader := range files {
		file, err := fileHeader.Open()
		if err != nil {
			continue
		}
		part, err := writer.CreateFormFile("images", fileHeader.Filename)
		if err != nil {
			file.Close()
			return nil, nil, fmt.Errorf("failed to create form file buffer: %v", err)
		}
		dst := io.Writer(part)
		var capBuf bytes.Buffer
		if capture {
			dst = io.MultiWriter(part, &capBuf)
		}
		_, err = io.Copy(dst, file)
		file.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to copy image bytes to buffer: %v", err)
		}
		if capture {
			archImgs = append(archImgs, archivedImage{
				name:        fileHeader.Filename,
				contentType: fileHeader.Header.Get("Content-Type"),
				data:        capBuf.Bytes(),
			})
		}
	}

	if err := writer.WriteField("category", category); err != nil {
		return nil, nil, fmt.Errorf("failed to write category form field: %v", err)
	}
	if err := writer.Close(); err != nil {
		return nil, nil, fmt.Errorf("failed to close multipart writer: %v", err)
	}

	// 2. Plain HTTP request — no OIDC auth on the local sidecar.
	endpoint := fmt.Sprintf("%s/process-batch", workerURL)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, &requestBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create local worker request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("local OCR sidecar unreachable at %s: %v", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("local OCR sidecar returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Tolerant decode (legacy map or {results, diagnostics} envelope).
	result, diagJSON, err := decodeWorkerResponse(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode local OCR JSON response: %v", err)
	}

	// Best-effort archive (local backend). Records the user-selected category.
	if capture {
		respJSON, _ := json.Marshal(result)
		keys := make([]string, 0, len(result))
		for k := range result {
			keys = append(keys, k)
		}
		imgs := archImgs
		diag := diagJSON
		acquired = false
		go func() {
			defer releaseArchiveSlot()
			archiveOCRRequest(imgs, respJSON, diag, keys, "local", category, archMode, archBucket)
		}()
	}
	return result, parseOCRDiagnostics(diagJSON), nil
}
