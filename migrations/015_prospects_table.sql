-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS prospects (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             TEXT NOT NULL,
    server           TEXT NOT NULL DEFAULT '',
    source_alliance  TEXT NOT NULL DEFAULT '',
    power            INTEGER,
    rank_in_alliance TEXT NOT NULL DEFAULT '',
    recruiter_id     INTEGER REFERENCES members(id) ON DELETE SET NULL,
    status           TEXT NOT NULL DEFAULT 'interested',
    notes            TEXT NOT NULL DEFAULT '',
    first_contacted  TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at       TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS prospects;
-- +goose StatementEnd
