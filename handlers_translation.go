package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// maxTranslationChars bounds one request. Every surface this serves is a note,
// a reason or a mail body; anything larger is not prose a member typed, and an
// unbounded body is a way to spend the month's character budget in one call.
const maxTranslationChars = 5000

// Per-user fixed-window rate limit. This is the only route in the app that
// spends a metered external quota, so an unthrottled one is a way to burn the
// monthly allowance from a single tab.
const (
	translateRateWindow = time.Minute
	translateRateMax    = 20
)

var translateRate = struct {
	sync.Mutex
	seen map[int]*translateWindow
}{seen: map[int]*translateWindow{}}

type translateWindow struct {
	start time.Time
	count int
}

// allowTranslate reports whether this user may make another request now, and
// prunes windows that have rolled over so the map cannot grow without bound.
func allowTranslate(userID int) bool {
	translateRate.Lock()
	defer translateRate.Unlock()

	now := time.Now()
	w, ok := translateRate.seen[userID]
	if !ok || now.Sub(w.start) >= translateRateWindow {
		translateRate.seen[userID] = &translateWindow{start: now, count: 1}
		// Opportunistic prune of stale entries from users who have stopped.
		for id, other := range translateRate.seen {
			if now.Sub(other.start) >= 10*translateRateWindow {
				delete(translateRate.seen, id)
			}
		}
		return true
	}
	if w.count >= translateRateMax {
		return false
	}
	w.count++
	return true
}

type translateRequest struct {
	Text   string `json:"text"`
	Target string `json:"target"`
	// CacheOnly asks for tier 1 only: answer from the cache or report a miss,
	// never call upstream. The client sends this when it still has a viable
	// on-device path and only wants to know whether the answer is already here.
	CacheOnly bool `json:"cache_only"`
}

// validLangTag accepts a base language subtag, optionally with a region
// ("en", "pt", "pt-BR"). It exists to keep junk out of the cache key rather
// than to be a complete BCP-47 parser.
func validLangTag(tag string) bool {
	if tag == "" {
		return false
	}
	base := baseLangTag(tag)
	if len(base) < 2 || len(base) > 3 {
		return false
	}
	for _, r := range tag {
		if r != '-' && r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

// translateText resolves one block of member-written prose into the reader's
// language.
//
// Available to any signed-in user: translating is a read affordance over content
// they can already see, and the cache is keyed on the text itself, so a result
// is only retrievable by someone who already holds the plaintext.
func translateText(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req translateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	req.Text = strings.TrimSpace(req.Text)
	req.Target = strings.ToLower(baseLangTag(strings.TrimSpace(req.Target)))

	if req.Text == "" {
		http.Error(w, "Nothing to translate", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(req.Text) > maxTranslationChars {
		http.Error(w, "Text is too long to translate", http.StatusBadRequest)
		return
	}
	if !validLangTag(req.Target) {
		http.Error(w, "Invalid target language", http.StatusBadRequest)
		return
	}

	// Tier 1 -- the cache, which costs nothing and is not rate limited.
	if cached, ok := lookupTranslationCache(hashSourceText(req.Text), req.Target); ok {
		writeJSON(w, cached)
		return
	}
	if req.CacheOnly {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Past this point the request can spend quota.
	mode, _, err := LoadTranslationConfig()
	if err != nil {
		slog.Error("translate: load config failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if !isServerTranslationMode(mode) {
		// Not an error the user caused: the operator has not enabled a server
		// backend. Refusing here is what stops an install that holds GCP
		// credentials for OCR from having its Translation quota spent on a
		// feature nobody turned on.
		http.Error(w, "Server-side translation is not enabled", http.StatusConflict)
		return
	}
	if !allowTranslate(user.ID) {
		http.Error(w, "Too many translation requests — please wait a moment", http.StatusTooManyRequests)
		return
	}

	result, err := Translate(r.Context(), req.Text, req.Target)
	if err != nil {
		if errors.Is(err, ErrTranslationBudget) {
			// Our own message, safe to return verbatim.
			http.Error(w, "This month's translation budget has been reached", http.StatusTooManyRequests)
			return
		}
		// Everything else may carry upstream or credential detail.
		slog.Error("translate failed", "target", req.Target, "mode", mode, "error", err)
		http.Error(w, "Translation unavailable", http.StatusBadGateway)
		return
	}

	writeJSON(w, result)
}

// updateTranslationSettings persists the translation backend mode and the
// monthly character cap. Written ONLY here — deliberately kept out of the mass
// updateSettings UPDATE, exactly like the OCR archival columns, so a general
// Settings save cannot blank them.
func updateTranslationSettings(w http.ResponseWriter, r *http.Request) {
	var p struct {
		TranslationBackendMode    string `json:"translation_backend_mode"`
		TranslationMonthlyCharCap int    `json:"translation_monthly_char_cap"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	mode := strings.TrimSpace(p.TranslationBackendMode)
	if !validTranslationMode(mode) {
		http.Error(w, "Invalid translation mode (must be ondevice, cloud, or local)", http.StatusBadRequest)
		return
	}
	if p.TranslationMonthlyCharCap < 0 {
		http.Error(w, "Monthly character cap cannot be negative", http.StatusBadRequest)
		return
	}

	// Re-validate prerequisites server-side (the UI gates these too, but never
	// trust the client).
	if TranslationBackendMode(mode) == TranslationCloud {
		var hasGCPKey bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM credentials WHERE service_name = 'gcp_vision')").Scan(&hasGCPKey)
		if !hasGCPKey {
			http.Error(w, "Cloud translation requires Google Cloud credentials to be configured first", http.StatusBadRequest)
			return
		}
	}
	if TranslationBackendMode(mode) == TranslationLocal {
		http.Error(w, "The self-hosted translation backend is not available yet", http.StatusBadRequest)
		return
	}

	if _, err := db.Exec(
		"UPDATE settings SET translation_backend_mode = ?, translation_monthly_char_cap = ? WHERE id = 1",
		mode, p.TranslationMonthlyCharCap,
	); err != nil {
		slog.Error("failed to update translation settings", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	actor := getAuthUser(r)
	logActivity(actor.ID, actor.Username, "updated", "settings", "Translation backend", true, "mode: "+mode)

	writeJSON(w, map[string]string{"message": "Translation settings updated successfully"})
}

// translationUsage reports this month's spend so the admin UI can show it.
func translationUsage(w http.ResponseWriter, r *http.Request) {
	mode, charCap, err := LoadTranslationConfig()
	if err != nil {
		slog.Error("translation usage: load config failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	used, err := monthlyCharsUsed()
	if err != nil {
		slog.Error("translation usage: sum failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"mode":       string(mode),
		"chars_used": used,
		"char_cap":   charCap,
	})
}
