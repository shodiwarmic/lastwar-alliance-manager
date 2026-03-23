
-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
ALTER TABLE settings ADD COLUMN cv_worker_url VARCHAR(255) DEFAULT '';

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back
ALTER TABLE settings DROP COLUMN cv_worker_url;