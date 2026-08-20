-- +goose Up
-- +goose StatementBegin

-- Every Faction War week has both an Attack action and a Defense action (never
-- two of the same role in one week, but which role falls on which day flips week
-- to week). The single `note` column added in 029 could not say which context a
-- note belonged to, so officers get one field per role.
--
-- `note` is deliberately KEPT as the general/no-role note — it holds already
-- entered data and is not migrated into either new column, because there is no
-- way to know retroactively which role an existing note referred to.
--
-- The *_poll_voted flags are officer-confirmed observations of the IN-GAME poll.
-- They are NOT connected to the app's own Poll Tracker (comms) tables; wiring
-- the two together is a possible future enhancement, not a promise made here.
ALTER TABLE season_participation ADD COLUMN attack_note TEXT NOT NULL DEFAULT '';
ALTER TABLE season_participation ADD COLUMN defense_note TEXT NOT NULL DEFAULT '';
ALTER TABLE season_participation ADD COLUMN attack_poll_voted INTEGER NOT NULL DEFAULT 0;
ALTER TABLE season_participation ADD COLUMN defense_poll_voted INTEGER NOT NULL DEFAULT 0;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE season_participation DROP COLUMN defense_poll_voted;
ALTER TABLE season_participation DROP COLUMN attack_poll_voted;
ALTER TABLE season_participation DROP COLUMN defense_note;
ALTER TABLE season_participation DROP COLUMN attack_note;
-- +goose StatementEnd
