-- +goose Up
-- +goose StatementBegin
ALTER TABLE season_events ADD COLUMN type_name TEXT NOT NULL DEFAULT '';

-- Backfill type_name from the joined schedule_event_types for rows that already have an event_type_id.
UPDATE season_events
SET type_name = (
    SELECT name FROM schedule_event_types WHERE id = season_events.event_type_id
)
WHERE event_type_id IS NOT NULL AND type_name = '';
-- +goose StatementEnd
