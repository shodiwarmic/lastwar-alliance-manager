-- +goose Up

-- Strike categories get a home. accountability_strikes.strike_type has been free
-- text since 044, and the list of categories lived in four hard-coded places (two
-- <select>s, two label switches) that already disagreed: the Accountability form's
-- "Manual" option took free text, so a typo became a permanent category, while the
-- member page offered no custom option at all.
--
-- strike_type stays TEXT and holds a row's KEY; the strikes table is not rebuilt.
-- A key is immutable once created, because it is the value stored on every strike;
-- the label is what an officer edits.
--
-- is_system rows are referenced by code and can be neither deleted nor deactivated:
-- vs_below_threshold (de-duplicated per reference date by handleStrikeCreate),
-- train_no_show (inserted and removed by handleTrainNoShow), storm_no_show (confirmed
-- Desert Storm participation suggestions) and manual (the old "Manual" option).
-- Deactivation is refused as well as deletion because handleStrikeCreate validates
-- against active rows, so a deactivated train_no_show would make the train page's
-- automatic strike fail.
-- +goose StatementBegin
CREATE TABLE strike_types (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    key        TEXT    NOT NULL UNIQUE,
    label      TEXT    NOT NULL,
    is_system  INTEGER NOT NULL DEFAULT 0,
    active     INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

-- The system rows first, so that a stored 'manual' below is recognised as one and
-- not duplicated as a custom row.
-- +goose StatementBegin
INSERT INTO strike_types (key, label, is_system, active, sort_order) VALUES
    ('vs_below_threshold', 'VS Below Threshold', 1, 1, 0),
    ('train_no_show',      'Train No-Show',      1, 1, 1),
    ('storm_no_show',      'Storm No-Show',      1, 1, 2),
    ('manual',             'Manual',             1, 1, 3);
-- +goose StatementEnd

-- Then every other value already in use on this install, verbatim, as a custom row.
-- Look-alike spellings are NOT merged: two spellings of one category are for the
-- operator to reconcile, not for a migration to guess at. Our own install holds
-- only system keys, but other installs may hold any number of typed categories.
-- strike_type is NOT NULL (044), so only an empty string needs guarding against.
-- +goose StatementBegin
INSERT INTO strike_types (key, label, is_system, active, sort_order)
SELECT DISTINCT strike_type, strike_type, 0, 1, 100
FROM accountability_strikes
WHERE trim(strike_type) != ''
  AND strike_type NOT IN (SELECT key FROM strike_types);
-- +goose StatementEnd
