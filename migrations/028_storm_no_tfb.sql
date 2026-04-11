-- +goose Up
-- +goose StatementBegin
ALTER TABLE storm_tf_config ADD COLUMN participating INTEGER NOT NULL DEFAULT 1;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1; -- SQLite does not support DROP COLUMN; column left in place on rollback
-- +goose StatementEnd
