-- +goose Up

-- Donations entered by hand (Project 20, private-docs 290): an officer's daily amount for a
-- member, typed on Tracking → Donations or imported from a CSV. The game itself only shows
-- "today so far" and "this week so far" (donation_readings); this is a different fact — what
-- a member gave on a day — so it has its own table rather than posing as a reading.
--
-- One row per member and game day. An entered day is exact and replaces what the readings
-- derive for that day only (deriveDonationWeek); clearing it deletes the row, and the day
-- goes back to the readings. foreign_keys is off app-wide: member delete removes the rows
-- (deleteMemberTx) and a user delete nulls entered_by (clearUserReferences).
-- +goose StatementBegin
CREATE TABLE donation_day_entries (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    member_id  INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    game_date  TEXT    NOT NULL,
    amount     INTEGER NOT NULL CHECK (amount >= 0),
    source     TEXT    NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','csv')),
    entered_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (member_id, game_date)
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_donation_day_entries_date ON donation_day_entries(game_date);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS donation_day_entries;
-- +goose StatementEnd
