package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"

	"google.golang.org/api/idtoken"
	"google.golang.org/api/option"
)

// PlayerRecord represents a single player's parsed score from the OCR worker.
type PlayerRecord struct {
	PlayerName string `json:"player_name"`
	Score      int64  `json:"score"`
}

// CVWorkerResponse maps the categorized UI state (e.g., "monday", "power")
// to the slice of extracted player records.
type CVWorkerResponse map[string][]PlayerRecord

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

	// --- NEW: Intercept and log the raw JSON payload ---
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read worker response body: %v", err)
	}

	log.Printf("Raw JSON from CV Worker:\n%s\n", string(bodyBytes))

	// Re-wrap the bytes into a reader so the JSON decoder can still consume it
	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	// ---------------------------------------------------

	// 5. Decode the structured JSON response
	var result CVWorkerResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode worker JSON response: %v", err)
	}

	return result, nil
}
