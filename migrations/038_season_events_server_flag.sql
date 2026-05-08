-- +goose Up
-- +goose StatementBegin
ALTER TABLE season_events ADD COLUMN is_server_event INTEGER NOT NULL DEFAULT 0;
ALTER TABLE season_events ADD COLUMN duration_days INTEGER NOT NULL DEFAULT 1;
-- +goose StatementEnd
