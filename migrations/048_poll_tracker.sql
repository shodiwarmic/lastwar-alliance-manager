-- +goose Up
-- +goose StatementBegin
CREATE TABLE poll_templates (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    title        TEXT    NOT NULL,
    question     TEXT    NOT NULL,
    options      TEXT    NOT NULL DEFAULT '[]',   -- JSON ["Yes","No","Abstain"]
    poll_type    TEXT    NOT NULL DEFAULT 'named', -- 'named' | 'anonymous'
    multi_select INTEGER NOT NULL DEFAULT 0,
    created_by   INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at   TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE poll_instances (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    template_id    INTEGER REFERENCES poll_templates(id) ON DELETE SET NULL,
    label          TEXT    NOT NULL,
    question       TEXT    NOT NULL,   -- snapshot from template at launch
    options        TEXT    NOT NULL,   -- snapshot from template at launch (JSON)
    poll_type      TEXT    NOT NULL,   -- snapshot from template at launch
    multi_select   INTEGER NOT NULL,   -- snapshot from template at launch
    rank_filter    TEXT,               -- NULL = all active; JSON ["R4","R5"] = subset
    total_eligible INTEGER NOT NULL DEFAULT 0, -- snapshotted at launch
    created_by     INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at     TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE poll_responses (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL REFERENCES poll_instances(id) ON DELETE CASCADE,
    member_id   INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    option_key  TEXT    NOT NULL,   -- exact option label; one row per member-option pair
    recorded_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    recorded_at TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE(instance_id, member_id, option_key)
);

CREATE TABLE poll_anonymous_counts (
    instance_id    INTEGER NOT NULL REFERENCES poll_instances(id) ON DELETE CASCADE,
    option_key     TEXT    NOT NULL,
    response_count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (instance_id, option_key)
);

CREATE INDEX idx_poll_responses_instance ON poll_responses(instance_id);
CREATE INDEX idx_poll_responses_member   ON poll_responses(member_id);
CREATE INDEX idx_poll_instances_created  ON poll_instances(created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_poll_instances_created;
DROP INDEX IF EXISTS idx_poll_responses_member;
DROP INDEX IF EXISTS idx_poll_responses_instance;
DROP TABLE IF EXISTS poll_anonymous_counts;
DROP TABLE IF EXISTS poll_responses;
DROP TABLE IF EXISTS poll_instances;
DROP TABLE IF EXISTS poll_templates;
-- +goose StatementEnd
