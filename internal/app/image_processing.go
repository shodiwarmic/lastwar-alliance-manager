package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

// ocrContractVersion is the wire-contract version this app parses: the canonical
// text is lastwar-screen-definitions' README (Consumer Contract → Wire contract
// v1). Every request names it, and a response in any other version is refused
// rather than half-read. The OCR service's side is its SCHEMA_VERSIONS.
const ocrContractVersion = 1

// ProcessImages dispatches to the right OCR backend based on the operator's
// settings. `category` is required for local mode and NOT sent in cloud mode,
// where the service auto-detects — so a stray screenshot in a VS batch is
// classified for what it is rather than forced into the selected category.
// Handlers that must have every frame read as one category (the participation
// import) call ProcessImagesForCategory instead.
func ProcessImages(ctx context.Context, files []*multipart.FileHeader, category string) (CVWorkerResponse, *OCRDiagnostics, error) {
	return processImages(ctx, files, category, false)
}

// ProcessImagesForCategory is ProcessImages with the category sent in both modes:
// the service skips classification and reads every frame as that category. The
// post-event mails need it, because the service never auto-detects a mail.
func ProcessImagesForCategory(ctx context.Context, files []*multipart.FileHeader, category string) (CVWorkerResponse, *OCRDiagnostics, error) {
	if category == "" {
		return nil, nil, fmt.Errorf("a category is required")
	}
	return processImages(ctx, files, category, true)
}

func processImages(ctx context.Context, files []*multipart.FileHeader, category string, categoryInCloud bool) (CVWorkerResponse, *OCRDiagnostics, error) {
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
	if !categoryInCloud {
		category = ""
	}
	return processImagesViaWorker(ctx, files, workerURL, category)
}

// OCRServiceError is a refusal from the OCR service (a 4xx with the v1 body
// `{error, code?}`), or an answer this app cannot read. Its Error() is the
// service's own sentence, which is written for a person.
type OCRServiceError struct {
	Status  int
	Code    string // "schema_not_supported", "category_not_supported", or ""
	Message string
}

func (e *OCRServiceError) Error() string { return e.Message }

// readServiceRefusal turns a non-200 response into an error: the body's `error`
// sentence when the service sent one, else the status and raw body.
func readServiceRefusal(resp *http.Response, what string) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var refusal struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 &&
		json.Unmarshal(body, &refusal) == nil && refusal.Error != "" {
		return &OCRServiceError{Status: resp.StatusCode, Code: refusal.Code, Message: "OCR service: " + refusal.Error}
	}
	return fmt.Errorf("%s returned status %d: %s", what, resp.StatusCode, string(body))
}

// newCloudOCRClient builds the HTTP client for a cloud-mode OCR service: an ID
// token for workerURL minted from the stored service-account key. A variable so
// tests can substitute a plain client against an httptest server.
var newCloudOCRClient = func(ctx context.Context, workerURL string) (*http.Client, func(), error) {
	plaintextJSON, err := getDecryptedGCPKey()
	if err != nil {
		return nil, func() {}, fmt.Errorf("authentication failed: %v", err)
	}
	// Securely wipe the plaintext key from memory once the client exists.
	wipe := func() {
		for i := range plaintextJSON {
			plaintextJSON[i] = 0
		}
	}
	client, err := idtoken.NewClient(ctx, workerURL, option.WithCredentialsJSON(plaintextJSON))
	if err != nil {
		wipe()
		return nil, func() {}, fmt.Errorf("failed to create authenticated GCP client: %v", err)
	}
	return client, wipe, nil
}

// ocrHTTPClient is the client for the configured OCR service: an ID token in
// cloud mode, plain HTTP for the local sidecar. /process-batch and /health go
// through the same one, so a capability check sees what an upload would.
func ocrHTTPClient(ctx context.Context, mode OCRBackendMode, workerURL string) (*http.Client, func(), error) {
	if mode == OCRBackendLocal {
		return &http.Client{Timeout: 5 * time.Minute}, func() {}, nil
	}
	return newCloudOCRClient(ctx, workerURL)
}

// OCRPlayer represents a single player's parsed score from the OCR worker.
// When a player name ends in digits that run flush against the score, Vision API
// may merge them into one token. In that case the worker enumerates every valid
// comma-grouped split in Candidates (smallest score first). Candidates is nil /
// absent when the name/score boundary is unambiguous.
//
// Rank, RankInferred and ScoreUnread are contract-v1 additions (absent from older
// services). They are kept here, not just read, so the archived response keeps
// them: the archive marshals this type.
type OCRPlayer struct {
	PlayerName   string      `json:"player_name"`
	Score        int64       `json:"score"`
	Candidates   []OCRPlayer `json:"candidates,omitempty"`
	Rank         *int        `json:"rank,omitempty"`
	RankInferred bool        `json:"rank_inferred,omitempty"`
	ScoreUnread  bool        `json:"score_unread,omitempty"`
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

// decodeWorkerResponse reads the OCR worker's {results, diagnostics} envelope,
// returning the players map plus the opaque diagnostics blob (which may be nil).
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

	// A response without schema_version is v1 by rule: every service released
	// before the field existed spoke v1. Any other version is refused whole.
	if verRaw, ok := probe["schema_version"]; ok {
		var version int
		if err := json.Unmarshal(verRaw, &version); err != nil || version != ocrContractVersion {
			return nil, nil, &OCRServiceError{Code: "schema_mismatch", Message: fmt.Sprintf(
				"The OCR service answered in contract version %s; this app reads version %d.",
				string(verRaw), ocrContractVersion)}
		}
	}

	resRaw, ok := probe["results"]
	if !ok {
		return nil, nil, fmt.Errorf("OCR response missing 'results' envelope")
	}
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

// parseOCRDiagnostics unmarshals the opaque diagnostics blob into the lean typed
// view used for the activity-log summary. Returns nil when the blob is absent
// (the OCR service sent no diagnostics) or unparseable — callers must nil-check.
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
// Returns "" when d is nil (no diagnostics in the response) so callers can
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

// processImagesViaWorker sends the screenshots to a cloud-mode OCR service (ID
// token from the stored key). category may be "" (auto-detect).
func processImagesViaWorker(ctx context.Context, files []*multipart.FileHeader, workerURL, category string) (CVWorkerResponse, *OCRDiagnostics, error) {
	return postProcessBatch(ctx, OCRBackendCloud, files, workerURL, category)
}

// ProcessImagesViaWorker is processImagesViaWorker with no category: the
// service auto-detects every frame.
func ProcessImagesViaWorker(ctx context.Context, files []*multipart.FileHeader, workerURL string) (CVWorkerResponse, *OCRDiagnostics, error) {
	return postProcessBatch(ctx, OCRBackendCloud, files, workerURL, "")
}

// ProcessImagesViaLocalWorker is the manual-mode local-OCR counterpart: it posts
// to the PaddleOCR sidecar (lastwar-ocr-service's Dockerfile.local image) over
// plain HTTP, and `category` is required — the caller picks the screen / tab
// because PaddleOCR's stylised-header OCR isn't reliable enough to auto-detect.
func ProcessImagesViaLocalWorker(ctx context.Context, files []*multipart.FileHeader, workerURL, category string) (CVWorkerResponse, *OCRDiagnostics, error) {
	if category == "" {
		return nil, nil, fmt.Errorf("local OCR mode requires a category (the user-selected screen+tab) — auto-classification is unreliable on PaddleOCR's stylised-header read")
	}
	return postProcessBatch(ctx, OCRBackendLocal, files, workerURL, category)
}

// postProcessBatch is the one /process-batch call for both backends: multipart
// `images`, `schema_version`, and `category` when one is given. Every response is
// best-effort archived with the category it was asked for.
func postProcessBatch(ctx context.Context, mode OCRBackendMode, files []*multipart.FileHeader, workerURL, category string) (CVWorkerResponse, *OCRDiagnostics, error) {
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no images provided for processing")
	}
	what := "OCR service"
	if mode == OCRBackendLocal {
		what = "local OCR sidecar"
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

	client, wipe, err := ocrHTTPClient(ctx, mode, workerURL)
	if err != nil {
		return nil, nil, err
	}
	defer wipe()

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
	if category != "" {
		// The service treats it as a classification override.
		if err := writer.WriteField("category", category); err != nil {
			return nil, nil, fmt.Errorf("failed to write category form field: %v", err)
		}
	}
	if err := writer.WriteField("schema_version", fmt.Sprint(ocrContractVersion)); err != nil {
		return nil, nil, fmt.Errorf("failed to write schema_version form field: %v", err)
	}
	if err := writer.Close(); err != nil {
		return nil, nil, fmt.Errorf("failed to close multipart writer: %v", err)
	}

	endpoint := fmt.Sprintf("%s/process-batch", workerURL)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, &requestBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create OCR request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		if mode == OCRBackendLocal {
			return nil, nil, fmt.Errorf("local OCR sidecar unreachable at %s: %v", endpoint, err)
		}
		return nil, nil, fmt.Errorf("microservice request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, readServiceRefusal(resp, what)
	}

	// Decode the {schema_version, results, diagnostics} envelope.
	result, diagJSON, err := decodeWorkerResponse(resp.Body)
	if err != nil {
		var refusal *OCRServiceError
		if errors.As(err, &refusal) {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("failed to decode %s JSON response: %v", what, err)
	}

	// Best-effort archive. Marshal synchronously so the goroutine never shares the
	// live result map; hand off the archiveSem slot to it.
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
			archiveOCRRequest(imgs, respJSON, diag, keys, string(mode), category, archMode, archBucket)
		}()
	}
	return result, parseOCRDiagnostics(diagJSON), nil
}
