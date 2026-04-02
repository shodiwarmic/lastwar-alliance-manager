-- +goose Up
-- +goose StatementBegin
CREATE TABLE activity_log (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id      INTEGER REFERENCES users(id) ON DELETE SET NULL,
    username     TEXT    NOT NULL,
    action       TEXT    NOT NULL,
    entity_type  TEXT    NOT NULL,
    entity_name  TEXT    NOT NULL,
    entity_count INTEGER NOT NULL DEFAULT 1,
    is_sensitive INTEGER NOT NULL DEFAULT 0,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_activity_log_created   ON activity_log(created_at DESC);
CREATE INDEX idx_activity_log_user      ON activity_log(user_id, created_at DESC);
CREATE INDEX idx_activity_log_sensitive ON activity_log(is_sensitive, created_at DESC);

ALTER TABLE rank_permissions ADD COLUMN view_activity INTEGER NOT NULL DEFAULT 0;
UPDATE rank_permissions SET view_activity = 1 WHERE rank IN ('R4', 'R5');
-- +goose StatementEnd
