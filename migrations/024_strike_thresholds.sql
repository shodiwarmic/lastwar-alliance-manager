-- +goose Up
-- +goose StatementBegin
ALTER TABLE settings ADD COLUMN strike_needs_improvement_threshold INTEGER NOT NULL DEFAULT 1;
ALTER TABLE settings ADD COLUMN strike_at_risk_threshold INTEGER NOT NULL DEFAULT 3;
-- +goose StatementEnd
