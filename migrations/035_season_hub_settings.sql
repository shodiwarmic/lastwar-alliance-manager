-- +goose Up
-- +goose StatementBegin
ALTER TABLE settings ADD COLUMN season_score_levels_default TEXT NOT NULL DEFAULT '[{"key":"full","label":"FULL","points":10},{"key":"partial","label":"PARTIAL","points":5},{"key":"absent","label":"ABSENT","points":0}]';
-- +goose StatementEnd
