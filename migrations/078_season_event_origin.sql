-- +goose Up

-- A pushed season event had no identity beyond the date it landed on. The push
-- looked for "an event of this type on this date", and the delete looked for the
-- same — so moving a season's start_date by a day and pushing again created a
-- second copy of every event, and deleting the season then purged NEITHER, because
-- both queries recomputed a date that no longer matched anything. Six one-day
-- offset duplicates were reproduced against a copy of the live database.
--
-- The identity is (season_event_id, season_week): a template row, materialised
-- once per week it runs in. Stamped on both tables, because a season pushes into
-- both.

-- +goose StatementBegin
ALTER TABLE schedule_events ADD COLUMN season_event_id INTEGER REFERENCES season_events(id);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE schedule_events ADD COLUMN season_week INTEGER;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE server_events ADD COLUMN season_event_id INTEGER REFERENCES season_events(id);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE server_events ADD COLUMN season_week INTEGER;
-- +goose StatementEnd

-- The identity is enforced by the DATABASE, not by convention.
--
-- The push is check-then-insert with no transaction, so two officers pushing at
-- the same moment could both see no match and both insert. A partial index makes
-- the second one fail instead, and the push maps that failure to "already
-- materialised" — which is exactly what it is.
--
-- Partial (WHERE … IS NOT NULL) so the millions of unstamped rows a manual
-- schedule accumulates are not forced to be distinct from each other.
-- +goose StatementBegin
CREATE UNIQUE INDEX IF NOT EXISTS idx_schedule_events_origin
    ON schedule_events (season_event_id, season_week)
    WHERE season_event_id IS NOT NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX IF NOT EXISTS idx_server_events_origin
    ON server_events (season_event_id, season_week)
    WHERE season_event_id IS NOT NULL;
-- +goose StatementEnd
