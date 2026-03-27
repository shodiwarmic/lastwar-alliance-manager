-- +goose Up
-- +goose StatementBegin

ALTER TABLE settings ADD COLUMN train_free_daily_limit INTEGER NOT NULL DEFAULT 1;
ALTER TABLE settings ADD COLUMN train_purchased_daily_limit INTEGER NOT NULL DEFAULT 2;

-- +goose StatementEnd
