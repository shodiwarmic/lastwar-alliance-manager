-- +goose Up
-- +goose StatementBegin
-- Soft-delete flag for user accounts. Deactivation revokes access at every auth entry
-- point while preserving login history, activity attribution and file ownership — the
-- things a hard DELETE destroys.
ALTER TABLE users ADD COLUMN is_active INTEGER NOT NULL DEFAULT 1;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN is_active;
-- +goose StatementEnd
