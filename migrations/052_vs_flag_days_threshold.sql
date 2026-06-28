-- +goose Up
-- +goose StatementBegin
ALTER TABLE settings ADD COLUMN vs_flag_days_threshold INTEGER NOT NULL DEFAULT 2;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Intentionally left blank
-- +goose StatementEnd
