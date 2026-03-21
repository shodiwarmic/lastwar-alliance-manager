-- +goose Up
-- +goose StatementBegin
ALTER TABLE member_aliases ADD COLUMN category TEXT DEFAULT 'global';

-- Backfill existing data based on user_id
UPDATE member_aliases SET category = 'personal' WHERE user_id IS NOT NULL;
UPDATE member_aliases SET category = 'global' WHERE user_id IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE member_aliases DROP COLUMN category;
-- +goose StatementEnd