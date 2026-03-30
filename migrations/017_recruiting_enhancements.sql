-- +goose Up
-- +goose StatementBegin

-- Prospect new fields
ALTER TABLE prospects ADD COLUMN hero_power INTEGER;
ALTER TABLE prospects ADD COLUMN seat_color TEXT NOT NULL DEFAULT '';
ALTER TABLE prospects ADD COLUMN interested_in_r4 INTEGER NOT NULL DEFAULT 0;

-- New valid status values: 'qualified_transfer' | 'unqualified_transfer'
-- Validation is enforced in Go; SQLite does not enforce CHECK on existing rows.

-- Former member reason for leaving
ALTER TABLE members ADD COLUMN leave_reason TEXT NOT NULL DEFAULT '';

-- Recruiting settings
ALTER TABLE settings ADD COLUMN alliance_max_members INTEGER NOT NULL DEFAULT 100;
ALTER TABLE settings ADD COLUMN join_requirements TEXT NOT NULL DEFAULT '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Intentionally left blank
-- +goose StatementEnd
