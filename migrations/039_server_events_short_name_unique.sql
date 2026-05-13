-- +goose Up
-- +goose StatementBegin

-- Defence in depth: enforce short_name uniqueness on server_events.
-- pushSeasonEventsToSchedule already SELECTs before INSERTing and appends a
-- numeric suffix on collision, so this index should never reject a row in
-- single-admin use. If it does, the admin will see a 500 and the underlying
-- race condition becomes visible instead of silently producing duplicates.
CREATE UNIQUE INDEX IF NOT EXISTS idx_server_events_short_name_unique
    ON server_events(short_name);

-- +goose StatementEnd
