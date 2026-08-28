package main

import (
	"path/filepath"
	"testing"
)

// setupTranslationTestDB points the package-level db at a fresh migrated temp
// SQLite file. Same shape as setupNameMatchTestDB.
func setupTranslationTestDB(t *testing.T) {
	t.Helper()
	prev := db
	t.Setenv("DATABASE_PATH", filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("STORAGE_PATH", t.TempDir())
	t.Setenv("SESSION_KEY", "test-session-key-at-least-32-chars-long")
	if err := initDB(); err != nil {
		t.Fatalf("initDB: %v", err)
	}
	t.Cleanup(func() {
		if db != nil {
			db.Close()
		}
		db = prev
	})
}

// An unknown mode must be rejected rather than stored. Persisting one would fall
// through to "no server backend" everywhere it is read, so the feature would look
// like it had silently switched itself off.
func TestValidTranslationMode(t *testing.T) {
	for _, m := range []string{"ondevice", "cloud", "local"} {
		if !validTranslationMode(m) {
			t.Errorf("mode %q should be valid", m)
		}
	}
	for _, m := range []string{"", "off", "none", "Cloud", "gcp", "libretranslate"} {
		if validTranslationMode(m) {
			t.Errorf("mode %q should be rejected", m)
		}
	}
}

// isServerTranslationMode gates the endpoint. If `ondevice` ever counted as a
// server mode, an install holding gcp_vision credentials for OCR could have its
// Translation quota spent on a feature the operator never enabled.
func TestOnDeviceIsNotAServerMode(t *testing.T) {
	if isServerTranslationMode(TranslationOnDevice) {
		t.Fatal("ondevice must not be treated as a server backend — it would let quota be spent with the feature off")
	}
	if !isServerTranslationMode(TranslationCloud) || !isServerTranslationMode(TranslationLocal) {
		t.Fatal("cloud and local are server backends")
	}
}

// The cache key must ignore surrounding whitespace, or the same note re-rendered
// with a stray newline pays for a second translation.
func TestHashSourceTextTrimsAndIsStable(t *testing.T) {
	a := hashSourceText("Bom trabalho esta semana.")
	b := hashSourceText("  Bom trabalho esta semana.\n")
	if a != b {
		t.Error("hash must ignore surrounding whitespace")
	}
	if a == hashSourceText("Bom trabalho esta semanas.") {
		t.Error("different text must hash differently")
	}
}

func TestBaseLangTag(t *testing.T) {
	cases := map[string]string{
		"en": "en", "en-GB": "en", "pt_BR": "pt", "zh-Hant-TW": "zh", "": "",
	}
	for in, want := range cases {
		if got := baseLangTag(in); got != want {
			t.Errorf("baseLangTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidLangTag(t *testing.T) {
	for _, ok := range []string{"en", "pt", "pt-BR", "fil"} {
		if !validLangTag(ok) {
			t.Errorf("%q should be accepted", ok)
		}
	}
	for _, bad := range []string{"", "e", "english", "e n", "pt/BR", "12"} {
		if validLangTag(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

// The default must be on-device so an install that has never touched the setting
// behaves exactly as it did before this feature existed.
func TestTranslationConfigDefaultsToOnDevice(t *testing.T) {
	setupTranslationTestDB(t)

	mode, charCap, err := LoadTranslationConfig()
	if err != nil {
		t.Fatalf("LoadTranslationConfig: %v", err)
	}
	if mode != TranslationOnDevice {
		t.Errorf("default mode = %q, want ondevice", mode)
	}
	if charCap != 400000 {
		t.Errorf("default cap = %d, want 400000", charCap)
	}
}

// Two viewers can miss the cache on the same note at once. The loser of that race
// must not error; it just re-reads what the winner wrote.
func TestStoreTranslationIsIdempotent(t *testing.T) {
	setupTranslationTestDB(t)

	h := hashSourceText("Bom trabalho esta semana.")
	storeTranslation(h, "en", "pt", "Good work this week.", 25)
	storeTranslation(h, "en", "pt", "Something else entirely.", 25)

	var rows, total int
	if err := db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(char_count), 0) FROM translation_cache
		 WHERE text_hash = ? AND target_lang = ?`, h, "en",
	).Scan(&rows, &total); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Errorf("got %d rows, want 1 — the unique constraint must collapse the race", rows)
	}
	// The ledger must not double-count a race either, or the monthly cap drifts.
	if total != 25 {
		t.Errorf("char_count total = %d, want 25", total)
	}

	got, ok := lookupTranslationCache(h, "en")
	if !ok {
		t.Fatal("expected a cache hit")
	}
	if got.TranslatedText != "Good work this week." {
		t.Errorf("first write should win, got %q", got.TranslatedText)
	}
	if !got.FromCache {
		t.Error("FromCache must be set on a hit")
	}
}

// A same-language answer is cached with an EMPTY translated_text: storing the
// original would put a second copy of member prose in a table that deliberately
// keeps only a hash of it.
func TestSameLanguageResultIsCachedWithoutTheOriginal(t *testing.T) {
	setupTranslationTestDB(t)

	h := hashSourceText("Good work this week everyone.")
	storeTranslation(h, "en", "en", "", 30)

	got, ok := lookupTranslationCache(h, "en")
	if !ok {
		t.Fatal("expected a cache hit")
	}
	if !got.AlreadyTarget {
		t.Error("source_lang == target_lang must report AlreadyTarget")
	}
	if got.TranslatedText != "" {
		t.Errorf("same-language rows must not store the original, got %q", got.TranslatedText)
	}
}

// The month window is computed in SQL. A Go time.Time bound into the comparison
// would be formatted with a zone suffix that sorts wrong against the space-form
// CURRENT_TIMESTAMP values — the timestamp trap in CLAUDE.md.
func TestMonthlyCharsUsedCountsOnlyThisMonth(t *testing.T) {
	setupTranslationTestDB(t)

	storeTranslation(hashSourceText("this month"), "en", "pt", "x", 100)
	if _, err := db.Exec(
		`INSERT INTO translation_cache (text_hash, target_lang, source_lang, translated_text, char_count, created_at)
		 VALUES (?, 'en', 'pt', 'y', 999, datetime('now', '-2 months'))`,
		hashSourceText("old"),
	); err != nil {
		t.Fatalf("seed old row: %v", err)
	}

	used, err := monthlyCharsUsed()
	if err != nil {
		t.Fatalf("monthlyCharsUsed: %v", err)
	}
	if used != 100 {
		t.Errorf("used = %d, want 100 — last month's spend must not count against this month's cap", used)
	}
}

// Translate must refuse before spending anything when no server backend is set.
func TestTranslateRefusesWithoutAServerBackend(t *testing.T) {
	setupTranslationTestDB(t)

	if _, err := Translate(t.Context(), "Bom trabalho esta semana.", "en"); err == nil {
		t.Fatal("expected a refusal in ondevice mode")
	}
}

// A cache hit must be served even with no backend configured and no credentials:
// it costs nothing and calls nothing.
func TestTranslateServesCacheHitWithoutABackend(t *testing.T) {
	setupTranslationTestDB(t)

	text := "Bom trabalho esta semana."
	storeTranslation(hashSourceText(text), "en", "pt", "Good work this week.", 25)

	got, err := Translate(t.Context(), text, "en")
	if err != nil {
		t.Fatalf("cached lookup should not need a backend: %v", err)
	}
	if got.TranslatedText != "Good work this week." || !got.FromCache {
		t.Errorf("expected the cached row, got %+v", got)
	}
}
