-- +goose Up

-- "MG" was never one event. The Alliance Exercise slot runs Marshal's Guard up to
-- Season 3 day 57 and LARGE SANDWORM from day 58, and the two have different level
-- scales: Marshal's Guard runs 1..12, Large Sandworm runs in tens from 10. Both
-- were being stored as the one MG type against the one 1..12-then-tens ceiling,
-- which is why this install has MG rows at level 70 sitting beside MG rows at
-- level 12 — the same column holding two scales.
--
-- The cutover is COMPUTED from the seasons table, never hardcoded: Season 3's
-- start_date plus 57 days. An install with no Season 3 row converts nothing and
-- keeps producing Marshal's Guard, which is the right answer for a server that has
-- not reached that day. Every statement below reads the cutover through a scalar
-- subquery, so when there is no such row the comparison is against NULL, is not
-- true, and matches no rows — the no-Season-3 case falls out of the SQL rather
-- than needing a branch.

-- 1. The new type. Fresh installs get a baseline of 10 — the bottom of the
--    Sandworm scale, not of Marshal's Guard's.
-- +goose StatementBegin
INSERT INTO schedule_event_types (name, short_name, icon, is_system, active, sort_order, has_level, baseline_level, max_level)
SELECT 'Large Sandworm', 'LS', '🪱', 1, 1, 1, 1, 10, 10
WHERE NOT EXISTS (SELECT 1 FROM schedule_event_types WHERE short_name = 'LS');
-- +goose StatementEnd

-- 2. Retype the events on the wrong side of the cutover, CONVERTING the ones stored
--    on Marshal's Guard's scale. Integer (level + 1) / 2 is ceil(level / 2) for
--    positive integers, so 12 -> 70 and 1 -> 20: the officer who typed "12" meant
--    the twelfth rung of the scale they were looking at.
-- +goose StatementBegin
UPDATE schedule_events
SET event_type_id = (SELECT id FROM schedule_event_types WHERE short_name = 'LS'),
    level         = 10 + 10 * ((level + 1) / 2)
WHERE event_type_id = (SELECT id FROM schedule_event_types WHERE short_name = 'MG')
  AND event_date >= (SELECT date(start_date, '+57 days') FROM seasons WHERE season_number = 3)
  AND level BETWEEN 1 AND 12;
-- +goose StatementEnd

-- 3. Retype the rest WITHOUT re-levelling: a row already at 70 is on the Sandworm
--    scale and means 70, not 360.
--
--    The level condition is PARENTHESISED. Without the brackets AND binds tighter
--    than OR and the clause reads "…or any row with a NULL level" — which would
--    retype every unlevelled custom event in the database as a Large Sandworm.
-- +goose StatementBegin
UPDATE schedule_events
SET event_type_id = (SELECT id FROM schedule_event_types WHERE short_name = 'LS')
WHERE event_type_id = (SELECT id FROM schedule_event_types WHERE short_name = 'MG')
  AND event_date >= (SELECT date(start_date, '+57 days') FROM seasons WHERE season_number = 3)
  AND (level NOT BETWEEN 1 AND 12 OR level IS NULL);
-- +goose StatementEnd

-- 4. Split the level configuration. Large Sandworm's baseline is Marshal's Guard's
--    baseline converted onto the Sandworm scale — the operator configured "12"
--    meaning the twelfth rung, and the twelfth rung is 70.
--
--    Runs only where there is something to split: an install that converted no
--    rows keeps the step-1 seed of 10/10.
-- +goose StatementBegin
UPDATE schedule_event_types
SET baseline_level = CASE
        WHEN (SELECT baseline_level FROM schedule_event_types WHERE short_name = 'MG') <= 12
        THEN 10 + 10 * (((SELECT baseline_level FROM schedule_event_types WHERE short_name = 'MG') + 1) / 2)
        ELSE (SELECT baseline_level FROM schedule_event_types WHERE short_name = 'MG')
    END
WHERE short_name = 'LS'
  AND EXISTS (SELECT 1 FROM schedule_events e
              WHERE e.event_type_id = (SELECT id FROM schedule_event_types WHERE short_name = 'LS'));
-- +goose StatementEnd

-- 5. Re-seed both ceilings from what each type now actually holds — 069's own rule,
--    re-run over the split data.
--
--    The COALESCE is load-bearing: SQLite's two-argument max() returns NULL when
--    EITHER argument is NULL, so max(12, (SELECT max(level) ... )) over a type with
--    no rows yields NULL — and a system type with a NULL ceiling is a 500 under the
--    rules migration 073 introduced.
-- +goose StatementBegin
UPDATE schedule_event_types
SET max_level = max(
        COALESCE(baseline_level, 1),
        COALESCE((SELECT max(e.level) FROM schedule_events e WHERE e.event_type_id = schedule_event_types.id), 1))
WHERE short_name IN ('MG', 'LS');
-- +goose StatementEnd
