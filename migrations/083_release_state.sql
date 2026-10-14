-- +goose Up

-- What the release check (release_check.go) last learned from GitHub about the newest
-- published release, and when it last tried. A singleton table of its own rather than
-- columns on `settings`, whose ~50-entry positional SELECT/Scan/UPDATE lists make every
-- added column an edit to three lists (CLAUDE.md -> "Dead schema"). All TEXT: timestamps
-- are written by SQLite's datetime('now'), so they read back in the shape they were written.
--
--   attempted_at      every attempt, before the request: the backoff (one a day)
--   checked_at        every answer GitHub gave (200 or 304): "checked <when>"
--   next_check_after  a rate-limit reset GitHub named, when it refused
-- +goose StatementBegin
CREATE TABLE release_state (
    id                  INTEGER PRIMARY KEY CHECK (id = 1),
    checked_at          TEXT,
    etag                TEXT,
    latest_tag          TEXT,
    latest_name         TEXT,
    latest_url          TEXT,
    latest_body         TEXT,
    latest_published_at TEXT,
    last_error          TEXT,
    attempted_at        TEXT,
    next_check_after    TEXT
);
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO release_state (id) VALUES (1);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE release_state;
-- +goose StatementEnd
