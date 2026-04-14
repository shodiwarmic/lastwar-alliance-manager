-- +goose Up
-- +goose StatementBegin
CREATE UNIQUE INDEX idx_seasons_season_number ON seasons(season_number);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_seasons_season_number;
-- +goose StatementEnd
