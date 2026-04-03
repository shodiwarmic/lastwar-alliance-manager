-- +goose Up
-- +goose StatementBegin
ALTER TABLE settings ADD COLUMN vs_minimum_points INTEGER NOT NULL DEFAULT 2500000;

CREATE TABLE user_dashboard_prefs (
    user_id    INTEGER NOT NULL PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    prefs      TEXT    NOT NULL DEFAULT '[]',
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd
