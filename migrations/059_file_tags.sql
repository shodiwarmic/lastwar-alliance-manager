-- +goose Up
-- File tags: many-to-many labels on alliance files, surfaced as filter chips on the Files page.
-- A tag carries a min_rank that RAISES the effective view rank of any file it is attached to
-- (effective = max(file.min_rank, max(tag.min_rank ...))) — enforced in Go, not here.
--
-- color stores a semantic TOKEN KEY (info/success/warning/danger/purple/neutral), never a raw
-- hex value, so chips/badges adapt to dark mode. name is unique case-insensitively.
--
-- FKs are declarative-only app-wide (foreign_keys pragma is off — see 056/057), so ON DELETE
-- CASCADE will NOT fire: deleteFile and the tag-delete handler remove file_tag_map rows
-- explicitly, in a transaction.
CREATE TABLE file_tags (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL UNIQUE COLLATE NOCASE,
    min_rank   TEXT NOT NULL DEFAULT 'R1',
    color      TEXT NOT NULL DEFAULT 'neutral',   -- semantic token key, not hex
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE file_tag_map (
    file_id INTEGER NOT NULL REFERENCES files(id)     ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES file_tags(id) ON DELETE CASCADE,
    PRIMARY KEY (file_id, tag_id)
);
CREATE INDEX idx_file_tag_map_tag ON file_tag_map(tag_id);

-- +goose Down
DROP INDEX IF EXISTS idx_file_tag_map_tag;
DROP TABLE IF EXISTS file_tag_map;
DROP TABLE IF EXISTS file_tags;
