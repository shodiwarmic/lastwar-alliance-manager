-- +goose Up
-- Our own server number for the week snapshot (mirrors opponent_server). Captured from our LastRank
-- alliance when configured; our server is effectively constant, but stored per-week for symmetry
-- with the opponent side and so the card can show it without a separate settings lookup.
ALTER TABLE vs_league_weeks ADD COLUMN our_server INTEGER;

-- +goose Down
ALTER TABLE vs_league_weeks DROP COLUMN our_server;
