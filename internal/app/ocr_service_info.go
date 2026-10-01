package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"
)

// ocrV1Categories is what an OCR service that predates /health's `categories`
// reads: the twenty-three ranking categories of contract v1. Such a service
// cannot read a post-event mail.
var ocrV1Categories = []string{
	"monday", "tuesday", "wednesday", "thursday", "friday", "saturday",
	"weekly", "power", "kills", "donation_daily", "donation_weekly",
	"mutual_assistance_daily", "mutual_assistance_weekly", "mutual_assistance_season",
	"siege_daily", "siege_weekly", "siege_season",
	"rare_soil_war_daily", "rare_soil_war_weekly", "rare_soil_war_season",
	"defeat_daily", "defeat_weekly", "defeat_season",
}

// OCRServiceInfo is what the configured OCR service says about itself on
// /health: its release, and the contract versions and categories it reads.
type OCRServiceInfo struct {
	Mode           OCRBackendMode `json:"mode"`
	Version        string         `json:"version"`
	Commit         string         `json:"commit"`
	SchemaVersions []int          `json:"schema_versions"`
	Categories     []string       `json:"categories"`
	// Legacy is true when the service sent no schema_versions: it predates
	// /health's capability fields and the defaults above stand in for them.
	Legacy bool `json:"legacy"`
}

// Reads reports whether the service reads `category`.
func (i *OCRServiceInfo) Reads(category string) bool {
	return slices.Contains(i.Categories, category)
}

// Speaks reports whether the service answers in this app's contract version.
func (i *OCRServiceInfo) Speaks() bool {
	return slices.Contains(i.SchemaVersions, ocrContractVersion)
}

// OCRUnreachableError means /health could not be read at all — down, cold and
// timed out, misconfigured. A caller says "try again", not "upgrade".
type OCRUnreachableError struct{ Err error }

func (e *OCRUnreachableError) Error() string { return "OCR service unreachable: " + e.Err.Error() }
func (e *OCRUnreachableError) Unwrap() error { return e.Err }

const ocrInfoTTL = 5 * time.Minute

type ocrInfoKey struct {
	mode OCRBackendMode
	url  string
}

type ocrInfoEntry struct {
	info    *OCRServiceInfo
	fetched time.Time
}

// The /health answer is cached for five minutes per (mode, URL) — both can change
// at runtime through Admin, and a changed one is a different service. Only a
// successful answer is cached, so a service that was down is asked again next
// time rather than reported down for five minutes.
var (
	ocrInfoMu    sync.Mutex
	ocrInfoCache = map[ocrInfoKey]ocrInfoEntry{}
)

func invalidateOCRServiceInfo() {
	ocrInfoMu.Lock()
	ocrInfoCache = map[ocrInfoKey]ocrInfoEntry{}
	ocrInfoMu.Unlock()
}

// ocrServiceInfo reads the configured OCR service's /health through the same
// client as /process-batch (an ID token in cloud mode), so a capability check
// sees exactly what an upload would.
func ocrServiceInfo(ctx context.Context) (*OCRServiceInfo, error) {
	mode, workerURL, err := LoadOCRBackendConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load OCR backend config: %v", err)
	}
	if workerURL == "" {
		return nil, &OCRUnreachableError{Err: fmt.Errorf("OCR worker URL is not configured in admin settings")}
	}
	key := ocrInfoKey{mode, workerURL}

	ocrInfoMu.Lock()
	if e, ok := ocrInfoCache[key]; ok && time.Since(e.fetched) < ocrInfoTTL {
		ocrInfoMu.Unlock()
		return e.info, nil
	}
	ocrInfoMu.Unlock()

	info, err := fetchOCRServiceInfo(ctx, mode, workerURL)
	if err != nil {
		return nil, err
	}
	ocrInfoMu.Lock()
	ocrInfoCache[key] = ocrInfoEntry{info: info, fetched: time.Now()}
	ocrInfoMu.Unlock()
	return info, nil
}

func fetchOCRServiceInfo(ctx context.Context, mode OCRBackendMode, workerURL string) (*OCRServiceInfo, error) {
	client, wipe, err := ocrHTTPClient(ctx, mode, workerURL)
	if err != nil {
		return nil, &OCRUnreachableError{Err: err}
	}
	defer wipe()

	// A cold Cloud Run instance can take a while to answer; an upload waits for
	// it anyway, but a page should not hang on it.
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", workerURL+"/health", nil)
	if err != nil {
		return nil, &OCRUnreachableError{Err: err}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, &OCRUnreachableError{Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &OCRUnreachableError{Err: fmt.Errorf("/health returned status %d", resp.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, &OCRUnreachableError{Err: err}
	}
	var raw struct {
		Version        string   `json:"version"`
		Commit         string   `json:"commit"`
		SchemaVersions []int    `json:"schema_versions"`
		Categories     []string `json:"categories"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, &OCRUnreachableError{Err: fmt.Errorf("/health is not JSON: %v", err)}
	}
	info := &OCRServiceInfo{Mode: mode, Version: raw.Version, Commit: raw.Commit,
		SchemaVersions: raw.SchemaVersions, Categories: raw.Categories}
	if len(info.SchemaVersions) == 0 {
		// Before /health reported capabilities: contract v1, the ranking screens.
		info.Legacy = true
		info.SchemaVersions = []int{1}
		if len(info.Categories) == 0 {
			info.Categories = ocrV1Categories
		}
	}
	if info.Version == "" {
		info.Version = "unversioned"
	}
	return info, nil
}

// getOCRServiceInfo serves Admin → About this install's OCR line. Admin-only, and
// fetched by the page after it renders, so a cold OCR service never holds the
// page up.
func getOCRServiceInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	mode, _, _ := LoadOCRBackendConfig()
	info, err := ocrServiceInfo(r.Context())
	if err != nil {
		slog.Warn("admin: OCR service info unavailable", "error", err)
		json.NewEncoder(w).Encode(map[string]interface{}{"mode": mode, "reachable": false})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"mode":            info.Mode,
		"reachable":       true,
		"version":         info.Version,
		"commit":          info.Commit,
		"schema_versions": info.SchemaVersions,
		"legacy":          info.Legacy,
		"speaks":          info.Speaks(),
		"app_contract":    ocrContractVersion,
	})
}
