-- +goose Up
-- +goose StatementBegin

-- Server-side translation for member-written prose.
--
-- The on-device path (static/translate.js) stays the default: it is free and
-- private, and needs no operator setup. But it exists only in recent desktop
-- Chrome/Edge, so it never reaches a phone -- and on some engines the language
-- pack never downloads at all. A server backend is what makes translated notes
-- readable on mobile and in browsers without the built-in API.
--
-- 'ondevice' is deliberately NOT called 'off': the browser path keeps working
-- regardless of this setting, so "off" would misdescribe what is happening.
ALTER TABLE settings ADD COLUMN translation_backend_mode TEXT NOT NULL DEFAULT 'ondevice';

-- 80% of Google Cloud Translation's permanent 500K chars/month free tier. The
-- headroom absorbs races (two viewers translating the same note at once) and
-- leaves room before anything is actually billed.
ALTER TABLE settings ADD COLUMN translation_monthly_char_cap INTEGER NOT NULL DEFAULT 400000;

-- Shared translation cache. Keyed on the HASH of the source text plus the target
-- language, which is what lets a cache lookup happen with no language detection
-- at all -- the first and cheapest tier of the resolution order.
--
-- The source text itself is deliberately NOT stored: this is a cache, not a
-- second copy of member-written prose sitting outside the table that owns it.
-- A same-language result (source == target, nothing to translate) is recorded
-- with an EMPTY translated_text rather than a copy of the original, for the
-- same reason.
--
-- char_count doubles as the spend ledger for the monthly budget guard, so rows
-- must NEVER be deleted -- a delete would undercount the month and let the cap
-- leak. Anything that needs to retire an entry (e.g. a future "this translation
-- is wrong" flag) must do it with a column, not a DELETE.
CREATE TABLE IF NOT EXISTS translation_cache (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    text_hash       TEXT NOT NULL,
    target_lang     TEXT NOT NULL,
    source_lang     TEXT NOT NULL DEFAULT '',
    translated_text TEXT NOT NULL DEFAULT '',
    char_count      INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (text_hash, target_lang)
);

-- The budget guard sums char_count over the current month on every miss.
CREATE INDEX IF NOT EXISTS idx_translation_cache_created ON translation_cache (created_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_translation_cache_created;
DROP TABLE IF EXISTS translation_cache;
ALTER TABLE settings DROP COLUMN translation_monthly_char_cap;
ALTER TABLE settings DROP COLUMN translation_backend_mode;
-- +goose StatementEnd
