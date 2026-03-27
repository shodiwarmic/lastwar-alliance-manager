-- +goose Up
-- +goose StatementBegin

-- Enable permissions: all ranks view, R4/R5 manage
UPDATE rank_permissions SET view_train = 1 WHERE rank IN ('R1','R2','R3','R4','R5');
UPDATE rank_permissions SET manage_train = 1 WHERE rank IN ('R4','R5');

-- Remove the old unused train_schedules table (feature was removed; train_logs replaces it)
DROP TABLE IF EXISTS train_schedules;

CREATE TABLE IF NOT EXISTS train_logs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    date         TEXT NOT NULL,  -- YYYY-MM-DD game date (UTC-2); SQLite has no native DATE type
    train_type   TEXT NOT NULL CHECK(train_type IN ('FREE','PURCHASED')),
    conductor_id INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    vip_id       INTEGER REFERENCES members(id) ON DELETE SET NULL,
    vip_type     TEXT CHECK(vip_type IN ('SPECIAL_GUEST','GUARDIAN_DEFENDER')),
    notes        TEXT,
    created_by   INTEGER NOT NULL REFERENCES users(id),
    created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_train_logs_date      ON train_logs(date DESC);
CREATE INDEX IF NOT EXISTS idx_train_logs_conductor ON train_logs(conductor_id);

CREATE TABLE IF NOT EXISTS eligibility_rules (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             TEXT NOT NULL,
    -- JSON: {"type":"RANDOM"} | {"type":"GREATEST","field":"days_since_free_conducted"} | {"type":"LEAST","field":"vs_total_week"}
    selection_method TEXT NOT NULL DEFAULT '{"type":"RANDOM"}',
    -- JSON: {"groups":[{"conditions":[{"variable":"rank","op":">=","value":"R3"}]}]}
    conditions       TEXT NOT NULL DEFAULT '{"groups":[]}',
    created_by       INTEGER NOT NULL REFERENCES users(id),
    created_at       DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- +goose StatementEnd
