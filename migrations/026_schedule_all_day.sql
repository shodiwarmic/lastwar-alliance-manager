-- +goose Up
-- +goose StatementBegin
ALTER TABLE schedule_events ADD COLUMN all_day INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
-- (purposely left blank)
-- +goose StatementEnd
