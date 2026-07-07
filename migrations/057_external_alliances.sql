-- +goose Up
-- General cache of EXTERNAL alliances, so a known alliance can be re-entered by tag or name
-- without another LastRank lookup. Not VS-League-specific: allies / recruiting / prospects also
-- reference outside alliances and can read/write this cache.
--
-- Our internal id (the PK) is the identity — NOT the external LastRank id. lastrank_id is kept
-- only as a reference attribute (nullable, not a key), so an alliance can be cached with no id and
-- have one filled in later without forcing a messy merge. A tag is globally unique in-game at any
-- moment but CHANGEABLE (and reusable), so it is NOT enforced unique here; the caching handler
-- dedups by tag/name. FKs are not enforced app-wide (see 056), so this table stands alone.
CREATE TABLE external_alliances (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,   -- our internal identity
    tag              TEXT,                                 -- alliance abbreviation; NOT unique (changeable/reusable)
    name             TEXT,
    server           INTEGER,
    power            INTEGER,
    kills            INTEGER,
    member_count     INTEGER,
    lastrank_id      TEXT,        -- external reference only (nullable, not a key)
    lastrank_seen_at DATETIME,    -- upstream last_seen_at at capture
    created_at       TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at       TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_external_alliances_tag ON external_alliances(tag);
CREATE INDEX idx_external_alliances_name ON external_alliances(name);

-- Seed (best-effort) from the existing allies list so already-known alliances are immediately
-- available for prefill. Only tag/name/server are carried over (allies has no power/kills).
INSERT INTO external_alliances (tag, name, server)
SELECT tag, name, CAST(NULLIF(TRIM(COALESCE(server, '')), '') AS INTEGER)
FROM allies
WHERE COALESCE(TRIM(tag), '') != '' OR COALESCE(TRIM(name), '') != '';

-- +goose Down
DROP INDEX IF EXISTS idx_external_alliances_name;
DROP INDEX IF EXISTS idx_external_alliances_tag;
DROP TABLE IF EXISTS external_alliances;
