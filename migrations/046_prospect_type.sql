-- +goose Up
-- +goose StatementBegin
ALTER TABLE prospects ADD COLUMN prospect_type TEXT NOT NULL DEFAULT 'transfer';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- +goose StatementEnd
