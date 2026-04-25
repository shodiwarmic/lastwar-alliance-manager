package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"

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

// ProcessImages dispatches to the right OCR backend based on the operator's
// settings. `category` is required for local mode and ignored for cloud
// mode. Wraps both ProcessImagesViaWorker and ProcessImagesViaLocalWorker
// with a single call site so handlers don't re-implement the switch.
func ProcessImages(ctx context.Context, files []*multipart.FileHeader, category string) (CVWorkerResponse, error) {
	mode, workerURL, err := LoadOCRBackendConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load OCR backend config: %v", err)
	}
	if workerURL == "" {
		return nil, fmt.Errorf("OCR worker URL is not configured in admin settings")
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
func ProcessImagesViaWorker(ctx context.Context, files []*multipart.FileHeader, workerURL string) (CVWorkerResponse, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("no images provided for processing")
	}

	// 1. Retrieve and decrypt the GCP credentials from your database
	plaintextJSON, err := getDecryptedGCPKey()
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %v", err)
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
		return nil, fmt.Errorf("failed to create authenticated GCP client: %v", err)
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
			return nil, fmt.Errorf("failed to create form file buffer: %v", err)
		}

		_, err = io.Copy(part, file)
		file.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to copy image bytes to buffer: %v", err)
		}
	}

	err = writer.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %v", err)
	}

	// 4. Dispatch the request to the Python microservice
	endpoint := fmt.Sprintf("%s/process-batch", workerURL)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, &requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create worker request: %v", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("microservice request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("worker returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// 5. Decode the structured JSON response
	var result CVWorkerResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode worker JSON response: %v", err)
	}

	return result, nil
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
func ProcessImagesViaLocalWorker(ctx context.Context, files []*multipart.FileHeader, workerURL, category string) (CVWorkerResponse, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("no images provided for processing")
	}
	if category == "" {
		return nil, fmt.Errorf("local OCR mode requires a category (the user-selected screen+tab) — auto-classification is unreliable on PaddleOCR's stylised-header read")
	}

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
			return nil, fmt.Errorf("failed to create form file buffer: %v", err)
		}
		_, err = io.Copy(part, file)
		file.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to copy image bytes to buffer: %v", err)
		}
	}

	if err := writer.WriteField("category", category); err != nil {
		return nil, fmt.Errorf("failed to write category form field: %v", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %v", err)
	}

	// 2. Plain HTTP request — no OIDC auth on the local sidecar.
	endpoint := fmt.Sprintf("%s/process-batch", workerURL)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, &requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create local worker request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("local OCR sidecar unreachable at %s: %v", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("local OCR sidecar returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result CVWorkerResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode local OCR JSON response: %v", err)
	}
	return result, nil
}
