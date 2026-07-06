-- +goose Up
-- General cache of EXTERNAL alliances seen via LastRank, so a known alliance can be re-entered
-- by tag without another lookup. Not VS-League-specific: allies / recruiting / prospects also
-- reference outside alliances and can read/write this cache. A tag is globally unique in-game at
-- any moment but CHANGEABLE (and reusable), so it is NOT enforced unique here; the stable key is
-- the LastRank alliance id. FKs are not enforced app-wide (see 056), so this table stands alone.
CREATE TABLE external_alliances (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    lastrank_id      TEXT,        -- 32-hex stable id (nullable for manual entries)
    tag              TEXT,        -- alliance abbreviation; NOT unique (changeable/reusable)
    name             TEXT,
    server           INTEGER,
    power            INTEGER,
    kills            INTEGER,
    member_count     INTEGER,
    lastrank_seen_at DATETIME,    -- upstream last_seen_at at capture
    created_at       TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at       TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_external_alliances_tag ON external_alliances(tag);
-- One row per LastRank alliance (when we have its id); lets a re-lookup refresh in place.
CREATE UNIQUE INDEX idx_external_alliances_lrid ON external_alliances(lastrank_id)
    WHERE lastrank_id IS NOT NULL AND lastrank_id != '';

-- +goose Down
DROP INDEX IF EXISTS idx_external_alliances_lrid;
DROP INDEX IF EXISTS idx_external_alliances_tag;
DROP TABLE IF EXISTS external_alliances;
