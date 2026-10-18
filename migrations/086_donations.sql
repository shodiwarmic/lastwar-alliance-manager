-- +goose Up

-- Donations Tracking (Project 20): the members' tech donations, the minimums they are
-- judged against, a strike category and a report key.
--
-- donation_readings holds what a screen showed, never a derived figure. The game shows
-- two numbers per member, both "so far": today's donation (since the 02:00 UTC reset)
-- and the week's (since Monday, game time; the donation week is Monday-Sunday). Each
-- source sends one or both as a half: kind 'today' or 'week'. One row per member, game
-- day and kind — the latest reading wins (an older capture never overwrites a newer
-- one). The days are derived from the halves when read (deriveDonationWeek,
-- donations.go), so every source and every surface agrees.
--
-- captured_at is TEXT on purpose: a DATETIME column comes back RFC 3339, and the write
-- compares it in SQL against the incoming reading in gametime.SQLiteLayout.
-- foreign_keys is off app-wide: member_id's ON DELETE is documentation, and a member
-- delete removes the rows explicitly (deleteMemberTx).
-- +goose StatementBegin
CREATE TABLE donation_readings (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    member_id   INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    game_date   TEXT    NOT NULL,                       -- the game day (UTC-2) the screen was read on
    kind        TEXT    NOT NULL CHECK (kind IN ('today','week')),
    amount      INTEGER NOT NULL CHECK (amount >= 0),
    captured_at TEXT    NOT NULL,                       -- UTC, gametime.SQLiteLayout
    source      TEXT    NOT NULL DEFAULT 'manual',      -- ocr | mobile | manual, as the histories
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (member_id, game_date, kind)
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_donation_readings_date ON donation_readings(game_date);
-- +goose StatementEnd

-- The donation minimums that applied to each week (the Monday, game time), both values
-- in one row. The Settings save writes the current week's row when either changes; 0 is
-- a real value ("off that week"). Resolution for week W: the row with the greatest
-- week_date <= W, else today's settings (donationMinimumsForWeek). No seed row: an
-- install's first Settings save records the first one, rather than recording the column
-- defaults as if an officer had chosen them.
-- +goose StatementBegin
CREATE TABLE donation_week_minimums (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    week_date      TEXT    NOT NULL UNIQUE,
    weekly_minimum INTEGER NOT NULL CHECK (weekly_minimum >= 0),
    daily_minimum  INTEGER NOT NULL CHECK (daily_minimum >= 0),
    set_by         INTEGER REFERENCES users(id) ON DELETE SET NULL,
    set_at         DATETIME DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE settings ADD COLUMN donation_weekly_minimum INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE settings ADD COLUMN donation_daily_minimum INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE settings ADD COLUMN donation_flag_days_threshold INTEGER NOT NULL DEFAULT 2;
-- +goose StatementEnd

-- The strike category Add Donation Strikes files under, a system row in 079's managed
-- list. Inserted if absent, then promoted: an install may already hold a custom row with
-- this key (the create route derives keys from labels), which keeps its id and strikes.
-- +goose StatementBegin
INSERT INTO strike_types (key, label, is_system, active, sort_order)
SELECT 'donation_below_minimum', 'Donation Below Minimum', 1, 1, (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM strike_types)
WHERE NOT EXISTS (SELECT 1 FROM strike_types WHERE key = 'donation_below_minimum');
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE strike_types SET is_system = 1, active = 1 WHERE key = 'donation_below_minimum';
-- +goose StatementEnd

-- The Donations report's key, seeded for every rank that holds the VS misses report
-- (owner, 2026-10-17). Only where the key is absent, so a later choice is never
-- overwritten.
-- +goose StatementBegin
UPDATE rank_permissions
SET permissions = json_set(permissions, '$.view_report_donations', json('true'))
WHERE json_extract(permissions, '$.view_report_vs_misses') = 1
  AND json_type(permissions, '$.view_report_donations') IS NULL;
-- +goose StatementEnd

-- +goose Down
-- The settings columns, the strike category and the permission key stay: the previous
-- image ignores them.
-- +goose StatementBegin
DROP TABLE IF EXISTS donation_week_minimums;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS donation_readings;
-- +goose StatementEnd
