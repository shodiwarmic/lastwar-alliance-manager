-- +goose Up
-- +goose StatementBegin
ALTER TABLE activity_log ADD COLUMN details TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd
