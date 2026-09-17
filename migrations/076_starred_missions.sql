-- +goose Up

-- Starred missions (the Secret Mobile Squad) rotate across a block of servers —
-- the game calls it a sector — in three groups, on a three-day cycle. The game
-- labels the groups A/B/C in a monthly image and the letters ROTATE, so they are
-- not stored; the group is derived from the day, and a server's group from the
-- date it opened.
--
-- The sector is two editable numbers rather than a hidden size constant. The
-- 64-wide grid rests on one tested boundary pair, and community sources claim 128
-- after Season 4 — an app that hardcoded 64 would be asserting a rule it does not
-- actually know.

-- +goose StatementBegin
ALTER TABLE settings ADD COLUMN sector_start INTEGER;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE settings ADD COLUMN sector_end INTEGER;
-- +goose StatementEnd

-- A server's opening date never changes, so a row here is written once and never
-- refreshed. That is what keeps the sweep a one-off cost against the volunteer
-- LastRank service rather than a recurring one.
--
-- `source` records who put it there: a 'manual' row is an officer's correction and
-- the sweep must never overwrite it.
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS server_open_dates (
    server_id  INTEGER PRIMARY KEY,
    open_date  TEXT NOT NULL,
    source     TEXT NOT NULL CHECK (source IN ('lastrank','manual')),
    fetched_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd
