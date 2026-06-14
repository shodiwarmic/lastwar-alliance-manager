package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

// OCR request archival — best-effort, non-blocking retention of OCR inputs
// (screenshots) + outputs (parsed response) for improving OCR and diagnosing
// extraction mistakes. Destination is selected by settings.ocr_archive_mode
// ("none"|"gcp"|"local"|"both"); it is decoupled from the OCR backend, so either
// backend may archive to either destination. Never blocks or fails the user's
// OCR request. Wired into both ProcessImagesVia*Worker functions.

const (
	// archiveDateLayout is the SINGLE source of truth for the date-dir layout.
	// archiveOCRRequest formats with it (UTC) and the janitor parses with it —
	// any drift would orphan directories permanently, so both must use this const.
	archiveDateLayout = "2006-01-02"

	// maxArchiveBytes caps the cumulative size of captured images per request.
	// Enforced upfront (summing multipart.FileHeader.Size) so capture is simply
	// skipped for oversized payloads — never aborted mid-stream.
	maxArchiveBytes = 64 << 20 // 64 MB

	// defaultArchiveRetentionDays is the local-disk retention default. Local
	// archives live on the app's host disk, so a tighter default than the GCS
	// 14-day lifecycle rule.
	defaultArchiveRetentionDays = 7
)

// archiveSem bounds concurrent in-flight archive operations so the captured
// image bytes (held until the upload finishes) can't OOM the app under a spike.
// MUST be initialized here with make() — a nil channel would block forever and
// never reach the non-blocking default skip; a per-request channel would defeat
// the cap. Do not move to func init() or lazy-init.
var archiveSem = make(chan struct{}, 4)

// releaseArchiveSlot frees one archiveSem slot.
func releaseArchiveSlot() { <-archiveSem }

// archivedImage is an in-memory copy of an uploaded screenshot, captured during
// the worker's existing read loop (tee'd via io.MultiWriter — no extra read).
type archivedImage struct {
	name        string
	contentType string
	data        []byte
}

// archiveStatus records the last archive error/success for the admin UI, so a
// silently-failing best-effort task (e.g. missing Storage Object Creator role)
// is visible instead of losing data unnoticed.
var archiveStatus struct {
	mu            sync.Mutex
	lastError     string
	lastErrorAt   time.Time
	lastSuccessAt time.Time
}

func recordArchiveError(msg string) {
	archiveStatus.mu.Lock()
	defer archiveStatus.mu.Unlock()
	archiveStatus.lastError = msg
	archiveStatus.lastErrorAt = time.Now().UTC()
}

func recordArchiveSuccess() {
	archiveStatus.mu.Lock()
	defer archiveStatus.mu.Unlock()
	archiveStatus.lastSuccessAt = time.Now().UTC()
}

// getArchiveStatus returns a snapshot for getSettings. Times are zero if unset.
func getArchiveStatus() (lastError string, lastErrorAt, lastSuccessAt time.Time) {
	archiveStatus.mu.Lock()
	defer archiveStatus.mu.Unlock()
	return archiveStatus.lastError, archiveStatus.lastErrorAt, archiveStatus.lastSuccessAt
}

// loadOCRArchiveConfig reads the archive mode + bucket from settings. Defaults
// to ("none", "") on any error so archival simply stays off.
func loadOCRArchiveConfig() (mode, bucket string) {
	mode, bucket = "none", ""
	err := db.QueryRow(
		"SELECT COALESCE(ocr_archive_mode, 'none'), COALESCE(ocr_archive_bucket, '') FROM settings WHERE id = 1",
	).Scan(&mode, &bucket)
	if err != nil {
		slog.Warn("ocr archive: could not load archive config", "error", err)
		return "none", ""
	}
	return mode, bucket
}

// beginOCRArchiveCapture decides whether the worker should capture this request's
// images for archival. Called at the top of each worker, BEFORE the file loop, so
// the io.MultiWriter tee (doubled memory) is only allocated when a slot is held.
//
// It returns capture=true only when: archival is enabled, the payload is within
// maxArchiveBytes (summed upfront from FileHeader.Size — never a mid-stream
// abort), and an archiveSem slot was acquired non-blocking. When true, the caller
// owns one archiveSem slot and must release it (via releaseArchiveSlot, panic-safe
// defer) or hand ownership to the archive goroutine.
func beginOCRArchiveCapture(files []*multipart.FileHeader) (capture bool, mode, bucket string) {
	mode, bucket = loadOCRArchiveConfig()
	if mode == "" || mode == "none" {
		return false, mode, bucket
	}

	var total int64
	for _, f := range files {
		total += f.Size
	}
	if total > maxArchiveBytes {
		slog.Warn("ocr archive: payload exceeds cap, skipping capture",
			"bytes", total, "cap", int64(maxArchiveBytes))
		return false, mode, bucket
	}

	select {
	case archiveSem <- struct{}{}:
		return true, mode, bucket
	default:
		slog.Warn("ocr archive: too many in-flight, skipping capture")
		return false, mode, bucket
	}
}

// generateRequestID returns 16 hex chars used as the per-request archive folder.
func generateRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

// archiveOCRRequest writes the captured images + response + metadata to the
// configured destination(s). Always run as a goroutine; must never panic or block
// the caller. meta.json is written FIRST as a sentinel so the retention janitor
// can reclaim partially-written (interrupted) archives.
func archiveOCRRequest(images []archivedImage, responseJSON []byte, categoryKeys []string, mode, requestedCategory, archiveMode, bucket string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("ocr archive: panic recovered", "panic", r)
		}
	}()

	requestID := generateRequestID()
	prefix := time.Now().UTC().Format(archiveDateLayout) + "/" + requestID

	meta := map[string]any{
		"request_id":          requestID,
		"archived_at":         time.Now().UTC().Format(time.RFC3339),
		"image_count":         len(images),
		"ocr_backend_mode":    mode,
		"requested_category":  requestedCategory,
		"detected_categories": categoryKeys,
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		slog.Warn("ocr archive: failed to marshal meta", "error", err)
		metaJSON = []byte("{}")
	}

	var wroteAny bool
	var firstErr error

	if archiveMode == "local" || archiveMode == "both" {
		if dir := os.Getenv("OCR_ARCHIVE_DIR"); dir != "" {
			if err := archiveToLocalDisk(dir, prefix, images, responseJSON, metaJSON); err != nil {
				slog.Warn("ocr archive: local write failed", "prefix", prefix, "error", err)
				if firstErr == nil {
					firstErr = err
				}
			} else {
				wroteAny = true
			}
		}
	}

	if archiveMode == "gcp" || archiveMode == "both" {
		if bucket != "" {
			if err := archiveToGCS(bucket, prefix, images, responseJSON, metaJSON); err != nil {
				slog.Warn("ocr archive: GCS write failed", "prefix", prefix, "error", err)
				if firstErr == nil {
					firstErr = err
				}
			} else {
				wroteAny = true
			}
		}
	}

	if firstErr != nil {
		recordArchiveError(firstErr.Error())
	}
	if wroteAny {
		recordArchiveSuccess()
		slog.Info("ocr archive: request archived", "prefix", prefix, "images", len(images))
	}
}

// archiveToLocalDisk writes the archive under <dir>/<prefix>/. meta.json first.
func archiveToLocalDisk(dir, prefix string, images []archivedImage, responseJSON, metaJSON []byte) error {
	target := filepath.Join(dir, prefix)
	// 0755/0644 matches the rest of the app's stored files (e.g. uploads, the DB)
	// so archives are inspectable from the host — the whole point of local archival.
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	// meta.json FIRST — the sentinel the janitor keys on.
	if err := os.WriteFile(filepath.Join(target, "meta.json"), metaJSON, 0o644); err != nil {
		return fmt.Errorf("meta: %w", err)
	}
	if err := os.WriteFile(filepath.Join(target, "response.json"), responseJSON, 0o644); err != nil {
		return fmt.Errorf("response: %w", err)
	}
	for i, img := range images {
		// filepath.Base strips any path components from the user-supplied filename
		// (prevents traversal outside target).
		name := fmt.Sprintf("image_%02d_%s", i+1, filepath.Base(img.name))
		if err := os.WriteFile(filepath.Join(target, name), img.data, 0o644); err != nil {
			return fmt.Errorf("image %d: %w", i+1, err)
		}
	}
	return nil
}

// archiveToGCS writes the archive to gs://<bucket>/<prefix>/. meta.json first.
// Re-decrypts the gcp_vision key independently (it wipes its own copy), so the
// existing in-worker decrypt/wipe is untouched and this works for either backend.
func archiveToGCS(bucket, prefix string, images []archivedImage, responseJSON, metaJSON []byte) error {
	key, err := getDecryptedGCPKey()
	if err != nil {
		return fmt.Errorf("credentials: %w", err)
	}
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()

	// Fresh context — the request context is gone by the time this goroutine runs.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := storage.NewClient(ctx, option.WithCredentialsJSON(key))
	if err != nil {
		return fmt.Errorf("storage client: %w", err)
	}
	defer client.Close()

	bkt := client.Bucket(bucket)
	writeObj := func(name, contentType string, data []byte) error {
		w := bkt.Object(name).NewWriter(ctx)
		w.ContentType = contentType
		if _, err := w.Write(data); err != nil {
			_ = w.Close()
			return err
		}
		return w.Close()
	}

	// meta.json FIRST — sentinel parity with the local path.
	if err := writeObj(prefix+"/meta.json", "application/json", metaJSON); err != nil {
		return fmt.Errorf("meta: %w", err)
	}
	if err := writeObj(prefix+"/response.json", "application/json", responseJSON); err != nil {
		return fmt.Errorf("response: %w", err)
	}
	for i, img := range images {
		ct := img.contentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		name := fmt.Sprintf("%s/image_%02d_%s", prefix, i+1, filepath.Base(img.name))
		if err := writeObj(name, ct, img.data); err != nil {
			return fmt.Errorf("image %d: %w", i+1, err)
		}
	}
	return nil
}

// startLocalArchiveJanitor launches the periodic retention sweep for local-disk
// archives. No-op when OCR_ARCHIVE_DIR is unset. Sweeps once shortly after boot,
// then every 24h for the process lifetime (not startup-only).
func startLocalArchiveJanitor() {
	dir := os.Getenv("OCR_ARCHIVE_DIR")
	if dir == "" {
		return
	}

	retentionDays := defaultArchiveRetentionDays
	if v := os.Getenv("OCR_ARCHIVE_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if n < 0 {
				slog.Info("ocr archive janitor: retention disabled (negative OCR_ARCHIVE_RETENTION_DAYS)")
				return
			}
			if n > 0 { // 0 / unparseable falls back to the default
				retentionDays = n
			}
		}
	}

	slog.Info("ocr archive janitor: starting", "dir", dir, "retention_days", retentionDays)
	go func() {
		pruneLocalArchive(dir, retentionDays)
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			pruneLocalArchive(dir, retentionDays)
		}
	}()
}

// pruneLocalArchive removes date-named archive dirs older than retentionDays.
// Safety: operates only on direct children whose name strictly parses as the
// archive date layout AND that contain a <requestID>/meta.json sentinel (i.e.
// folders we created); refuses to run on a dangerous root.
func pruneLocalArchive(dir string, retentionDays int) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("ocr archive janitor: panic recovered", "panic", r)
		}
	}()

	clean := filepath.Clean(dir)
	if clean == "/" || clean == "." || clean == "" {
		slog.Error("ocr archive janitor: refusing to prune unsafe OCR_ARCHIVE_DIR", "dir", dir)
		return
	}

	entries, err := os.ReadDir(clean)
	if err != nil {
		// The dir only exists once the first archive is written — not an error.
		if !os.IsNotExist(err) {
			slog.Warn("ocr archive janitor: cannot read dir", "dir", clean, "error", err)
		}
		return
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		d, err := time.Parse(archiveDateLayout, e.Name())
		if err != nil {
			continue // not a date-named dir we created
		}
		if !d.Before(cutoff) {
			continue // still within retention
		}
		full := filepath.Join(clean, e.Name())
		if !strings.HasPrefix(full, clean+string(os.PathSeparator)) {
			continue // paranoia: never escape the archive dir
		}
		if !dateDirHasSentinel(full) {
			continue // no <requestID>/meta.json — not one of ours
		}
		if err := os.RemoveAll(full); err != nil {
			slog.Warn("ocr archive janitor: failed to remove", "path", full, "error", err)
			continue
		}
		slog.Info("ocr archive janitor: pruned", "path", full)
	}
}

// dateDirHasSentinel reports whether a date dir contains at least one
// <requestID>/meta.json — proof the app created it (vs. a mounted backup that
// merely happens to use date-named folders).
func dateDirHasSentinel(dateDir string) bool {
	subs, err := os.ReadDir(dateDir)
	if err != nil {
		return false
	}
	for _, s := range subs {
		if !s.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dateDir, s.Name(), "meta.json")); err == nil {
			return true
		}
	}
	return false
}
