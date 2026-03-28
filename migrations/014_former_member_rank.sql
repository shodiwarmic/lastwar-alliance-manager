-- +goose Up
-- +goose StatementBegin
INSERT OR IGNORE INTO rank_permissions (rank) VALUES ('EX');
INSERT OR IGNORE INTO rank_permissions (rank) VALUES ('PROSPECT');

ALTER TABLE rank_permissions ADD COLUMN view_recruiting INTEGER NOT NULL DEFAULT 0;
ALTER TABLE rank_permissions ADD COLUMN manage_recruiting INTEGER NOT NULL DEFAULT 0;

-- R4+ can view and manage recruiting
UPDATE rank_permissions SET view_recruiting = 1, manage_recruiting = 1 WHERE rank IN ('R4', 'R5');

ALTER TABLE members ADD COLUMN notes TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM rank_permissions WHERE rank IN ('EX', 'PROSPECT');
-- +goose StatementEnd
