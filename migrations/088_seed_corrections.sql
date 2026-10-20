-- +goose Up

-- Seed corrections (Project 21, private-docs 294–299). Every game fact a shipped season
-- template carries gains a source and a grade, and the six templates are corrected from the
-- audit's per-value table (lastwar-private-docs planning/project-21/seed-corrections.md).
--
-- Grades: official (FunFly's season calendar or publisher wiki article), cpt_hedge,
-- guide (another community guide), owner, requester (Pudgey's PDFs), conflict (sources
-- disagree; the row records the chosen value and the source names the alternative),
-- unverified (no raw source). In-game observation outranks all of them.
--
-- THE EXISTING-INSTALL RULE. A template is corrected only where it still reads as shipped,
-- column by column: each of events, defaults and trackables is replaced only while it still
-- equals its 034 text (compared through json(), which normalises whitespace). An operator's
-- edited column is left alone and the template's other columns are still corrected. A column
-- that is not valid JSON is left alone too, rather than aborting the migration and blocking
-- boot — the CASE keeps json() from ever being evaluated on it. Existing SEASONS are never
-- rewritten here: Edit Season shows what changed (GET .../template-diff) with a per-row Apply.
--
-- SERVER EVENTS keep their shape (owner, 2026-10-19). Their facts, recorded here rather than
-- as columns: Rally Challenge runs 2 days, Zombie Invasion 3, General's Trial 3; Rampage
-- Bosses and Doomsday run 1 day each, on Sundays only, alternating (each every 14 days, a
-- week apart). All five have fixed weekdays in the game, but the app does NOT enforce
-- weekdays — each install sets its own anchor dates (standing decision: "Server event
-- weekdays are not enforced").
--
-- The Desert Storm slot times need no step: migration 027 already set Early 09:00 /
-- Evening 18:00 / Late 23:00 on every install (private-docs 300, closed as already fixed).

-- Provenance on a season's own rows. template_key is the template row's stable "key", stamped
-- when a season is created from a template; the template diff matches on it, because a label
-- is not unique (S4 has two rows labelled Copper War). Rows from seasons created before this
-- migration have none and are matched by a composite fallback.
-- +goose StatementBegin
ALTER TABLE season_events ADD COLUMN template_key TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE season_events ADD COLUMN source TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE season_events ADD COLUMN grade TEXT NOT NULL DEFAULT 'unverified';
-- +goose StatementEnd

-- +goose Down
-- The template corrections are not reverted: a rollback that rewrote templates would have to
-- tell a corrected row from an operator's edit, which is the judgement the Up refuses to make.
-- +goose StatementBegin
ALTER TABLE season_events DROP COLUMN grade;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE season_events DROP COLUMN source;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE season_events DROP COLUMN template_key;
-- +goose StatementEnd
