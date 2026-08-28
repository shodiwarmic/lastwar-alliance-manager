package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/api/option"
	translate "google.golang.org/api/translate/v3"
)

// TranslationBackendMode reflects `settings.translation_backend_mode`.
//
// `ondevice` (default) means there is no server backend: the browser's built-in
// Translator is the only path, exactly as shipped. `cloud` routes through Google
// Cloud Translation. `local` is reserved for the LibreTranslate sidecar and is
// not implemented yet.
//
// Deliberately not named `off` -- the on-device path keeps working in every
// mode, so "off" would describe something that isn't true.
type TranslationBackendMode string

const (
	TranslationOnDevice TranslationBackendMode = "ondevice"
	TranslationCloud    TranslationBackendMode = "cloud"
	TranslationLocal    TranslationBackendMode = "local"
)

// translationTimeout bounds one upstream call. Cloud Translation answers a
// note-sized string in well under a second; this exists so a hung connection
// cannot occupy the single DB-free window indefinitely.
const translationTimeout = 15 * time.Second

// validTranslationMode reports whether a mode string is one we store. Used by
// updateSettings so an unknown value is rejected rather than persisted -- an
// unrecognised mode would otherwise fall through to "no server backend" and
// look like the feature had silently switched itself off.
func validTranslationMode(m string) bool {
	switch TranslationBackendMode(m) {
	case TranslationOnDevice, TranslationCloud, TranslationLocal:
		return true
	}
	return false
}

// isServerTranslationMode reports whether the mode has a server backend at all.
// The translate endpoint refuses when this is false: without it, any install
// that holds gcp_vision credentials for OCR could have its Translation quota
// spent by a signed-in user, on a feature the operator never enabled.
func isServerTranslationMode(m TranslationBackendMode) bool {
	return m == TranslationCloud || m == TranslationLocal
}

// LoadTranslationConfig reads the backend mode and the monthly character cap
// from settings. Modelled on LoadOCRBackendConfig; defaults to on-device so an
// install that has never touched the setting behaves exactly as it does today.
func LoadTranslationConfig() (mode TranslationBackendMode, charCap int, err error) {
	var rawMode string
	err = db.QueryRow(
		`SELECT COALESCE(translation_backend_mode, 'ondevice'),
		        COALESCE(translation_monthly_char_cap, 400000)
		 FROM settings WHERE id = 1`,
	).Scan(&rawMode, &charCap)
	if err != nil {
		return TranslationOnDevice, 0, err
	}
	if !validTranslationMode(rawMode) {
		rawMode = string(TranslationOnDevice)
	}
	return TranslationBackendMode(rawMode), charCap, nil
}

// hashSourceText is the cache key for a piece of prose. Hashing server-side is
// deliberate: the obvious alternative -- having the client hash and ask about a
// digest -- needs crypto.subtle, which browsers expose only in a secure context.
// This app is routinely reached over plain HTTP on a LAN (see the clipboard note
// in static/global.js), which is exactly the deployment a server backend exists
// to serve, so a secure-context-only cache lookup would fail where it is needed
// most.
func hashSourceText(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:])
}

// TranslationResult is one resolved translation, whether it came from the cache
// or from upstream.
type TranslationResult struct {
	TranslatedText string `json:"translated_text"`
	SourceLang     string `json:"source_lang"`
	// AlreadyTarget means the detected source language matched the target, so
	// there was nothing to translate. Cached like any other answer so the
	// question is asked upstream only once per note.
	AlreadyTarget bool `json:"already_target"`
	FromCache     bool `json:"from_cache"`
}

// lookupTranslationCache returns a cached result, or ok=false on a miss.
//
// Callers must treat this as a plain short read: it opens and closes its own
// row, holding nothing, because db.SetMaxOpenConns(1) means a cursor held across
// an upstream HTTP call would deadlock the whole process.
func lookupTranslationCache(textHash, target string) (TranslationResult, bool) {
	var sourceLang, translated string
	err := db.QueryRow(
		`SELECT source_lang, translated_text FROM translation_cache
		 WHERE text_hash = ? AND target_lang = ?`,
		textHash, target,
	).Scan(&sourceLang, &translated)
	if err != nil {
		return TranslationResult{}, false
	}
	return TranslationResult{
		TranslatedText: translated,
		SourceLang:     sourceLang,
		AlreadyTarget:  sourceLang == target,
		FromCache:      true,
	}, true
}

// storeTranslation records a result. ON CONFLICT DO NOTHING because two viewers
// can miss the cache on the same note simultaneously; the duplicate upstream
// call is spent either way and the cap's headroom absorbs it, but the row must
// not error out the request that lost the race.
func storeTranslation(textHash, target, sourceLang, translated string, charCount int) {
	if _, err := db.Exec(
		`INSERT INTO translation_cache (text_hash, target_lang, source_lang, translated_text, char_count)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (text_hash, target_lang) DO NOTHING`,
		textHash, target, sourceLang, translated, charCount,
	); err != nil {
		// A cache write failing costs a re-translation later, nothing more.
		slog.Error("translation cache write failed", "error", err)
	}
}

// monthlyCharsUsed sums this month's spend.
//
// The month boundary is computed in SQL rather than in Go: created_at is written
// by CURRENT_TIMESTAMP (UTC, space-separated), and a bound Go time.Time would be
// formatted by the driver with a zone suffix that compares wrong against it --
// the timestamp trap documented in CLAUDE.md.
func monthlyCharsUsed() (int, error) {
	var used int
	err := db.QueryRow(
		`SELECT COALESCE(SUM(char_count), 0) FROM translation_cache
		 WHERE created_at >= datetime('now', 'start of month')`,
	).Scan(&used)
	return used, err
}

// ErrTranslationBudget is returned when this month's cap is already spent.
var ErrTranslationBudget = fmt.Errorf("monthly translation character budget exhausted")

// Translate resolves one piece of text into `target`, consulting the cache
// first and going upstream only on a miss. It is the single dispatch point for
// every backend -- handlers must not re-implement the switch.
//
// Shaped read -> fetch -> write throughout: every DB read completes before the
// network call, and the write happens after it. Holding a cursor across the
// wire would wait forever for the one connection the app has.
func Translate(ctx context.Context, text, target string) (TranslationResult, error) {
	textHash := hashSourceText(text)

	// --- read ---
	if cached, ok := lookupTranslationCache(textHash, target); ok {
		return cached, nil
	}

	mode, charCap, err := LoadTranslationConfig()
	if err != nil {
		return TranslationResult{}, fmt.Errorf("load translation config: %w", err)
	}
	if !isServerTranslationMode(mode) {
		return TranslationResult{}, fmt.Errorf("no server translation backend configured")
	}

	charCount := utf8.RuneCountInString(text)
	if charCap > 0 {
		used, err := monthlyCharsUsed()
		if err != nil {
			return TranslationResult{}, fmt.Errorf("budget check: %w", err)
		}
		if used+charCount > charCap {
			return TranslationResult{}, ErrTranslationBudget
		}
	}

	var creds []byte
	if mode == TranslationCloud {
		// Read the credential before the network call, not during it.
		creds, err = getDecryptedGCPKey()
		if err != nil {
			return TranslationResult{}, err
		}
	}

	// --- fetch (no DB handle held) ---
	var result TranslationResult
	switch mode {
	case TranslationCloud:
		result, err = translateViaCloud(ctx, creds, text, target)
	case TranslationLocal:
		// The LibreTranslate sidecar is a separate pass; the mode is accepted by
		// the schema so the setting can exist, but nothing serves it yet.
		return TranslationResult{}, fmt.Errorf("local translation backend is not implemented yet")
	}
	if err != nil {
		return TranslationResult{}, err
	}

	// --- write ---
	// A same-language answer stores an EMPTY translated_text: writing the
	// original back would put a second copy of member prose in a cache table
	// that deliberately holds only a hash of it.
	stored := result.TranslatedText
	if result.AlreadyTarget {
		stored = ""
	}
	storeTranslation(textHash, target, result.SourceLang, stored, charCount)

	return result, nil
}

// translateViaCloud calls Google Cloud Translation v3.
//
// It reuses the SAME service-account credential already uploaded for Cloud
// Vision (credentials.service_name = 'gcp_vision'), exactly as the GCS archival
// path does -- there is no second key to manage. The operator's only setup is
// enabling the Translation API and granting roles/cloudtranslate.user on the
// project they already have.
func translateViaCloud(ctx context.Context, creds []byte, text, target string) (TranslationResult, error) {
	projectID, err := gcpProjectIDFromKey(creds)
	if err != nil {
		return TranslationResult{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, translationTimeout)
	defer cancel()

	svc, err := translate.NewService(ctx, option.WithCredentialsJSON(creds))
	if err != nil {
		return TranslationResult{}, fmt.Errorf("translation client: %w", err)
	}

	parent := fmt.Sprintf("projects/%s/locations/global", projectID)
	resp, err := svc.Projects.TranslateText(parent, &translate.TranslateTextRequest{
		Contents:           []string{text},
		TargetLanguageCode: target,
		// The v3 default is "text/html", which would escape and mangle the
		// ampersands and angle brackets that turn up in ordinary prose.
		MimeType: "text/plain",
		// SourceLanguageCode is deliberately omitted: letting the service detect
		// the source is what keeps this tier free of any client-side language
		// detection, which is the whole reason it works on mobile.
	}).Context(ctx).Do()
	if err != nil {
		return TranslationResult{}, fmt.Errorf("cloud translation request: %w", err)
	}
	if len(resp.Translations) == 0 {
		return TranslationResult{}, fmt.Errorf("cloud translation returned no result")
	}

	t := resp.Translations[0]
	source := strings.ToLower(baseLangTag(t.DetectedLanguageCode))
	return TranslationResult{
		TranslatedText: t.TranslatedText,
		SourceLang:     source,
		AlreadyTarget:  source == strings.ToLower(baseLangTag(target)),
	}, nil
}

// gcpProjectIDFromKey pulls project_id out of the service-account JSON. The v3
// API addresses a project in its `parent`, and the key already carries it --
// asking the operator to type it into a second setting would just be a way to
// get it wrong.
func gcpProjectIDFromKey(creds []byte) (string, error) {
	var key struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(creds, &key); err != nil {
		return "", fmt.Errorf("parse service account key: %w", err)
	}
	if key.ProjectID == "" {
		return "", fmt.Errorf("service account key has no project_id")
	}
	return key.ProjectID, nil
}

// baseLangTag reduces "en-GB" to "en". The engines key on the base subtag and
// regional variants would fragment the cache into near-duplicate rows.
func baseLangTag(tag string) string {
	if i := strings.IndexAny(tag, "-_"); i > 0 {
		return tag[:i]
	}
	return tag
}
