-- +goose Up
-- +goose StatementBegin

-- Event levels move OFF `settings` and ONTO the event type row.
--
-- Until now a level was a pair of settings columns (mg_baseline / max_mg_level,
-- zs_baseline / max_zs_level) reached through a string switch that defaulted to
-- MG's columns for anything that was not 'ZS'. Two consequences, both live:
--
--   * A custom event type could not carry a level at all through the UI, yet the
--     write path never checked, so a level could leak onto one by switching the
--     type in the event modal (reproduced 2026-09-15: POST with a custom type and
--     level 12 returned 201 and stored the 12).
--   * The next system type to arrive would silently inherit Marshal's Guard's
--     baseline and ceiling through that default branch (reproduced the same day:
--     a fresh system type took MG's numbers with nothing configured for it).
--
-- Per-type columns end both. `has_level` says whether the type carries a level at
-- all; `baseline_level` and `max_level` are that type's own numbers. An unknown
-- system type is now a data error rather than a fallback, because there is no
-- other type's numbers left to fall back TO.
--
-- The four `settings` columns are left in place as dead schema — see CLAUDE.md
-- "Dead schema on `settings`". One future cleanup drops them together with
-- zs_anchor_time, current_season and season_start_date.

ALTER TABLE schedule_event_types ADD COLUMN has_level      INTEGER NOT NULL DEFAULT 0;
ALTER TABLE schedule_event_types ADD COLUMN baseline_level INTEGER;
ALTER TABLE schedule_event_types ADD COLUMN max_level      INTEGER;

-- Carry the configured values across verbatim. COALESCE because 069 declared the
-- ceilings NOT NULL DEFAULT 1 but the baselines are nullable, and an install that
-- never opened the schedule settings page has NULLs there.
UPDATE schedule_event_types
SET has_level      = 1,
    baseline_level = (SELECT COALESCE(mg_baseline, 1) FROM settings WHERE id = 1),
    max_level      = (SELECT COALESCE(max_mg_level, 1) FROM settings WHERE id = 1)
WHERE short_name = 'MG' AND is_system = 1;

UPDATE schedule_event_types
SET has_level      = 1,
    baseline_level = (SELECT COALESCE(zs_baseline, 1) FROM settings WHERE id = 1),
    max_level      = (SELECT COALESCE(max_zs_level, 1) FROM settings WHERE id = 1)
WHERE short_name = 'ZS' AND is_system = 1;

-- A system type whose ceiling is below a level already stored against it would be
-- uneditable: the update path grandfathers an unchanged level, but the create path
-- and the generator both validate against the ceiling. 069 seeded the settings
-- ceilings this way; re-assert it per type now that the ceiling lives here, so the
-- split can never strand a row.
UPDATE schedule_event_types
SET max_level = max(
        COALESCE(max_level, 1),
        COALESCE(baseline_level, 1),
        COALESCE((SELECT max(e.level) FROM schedule_events e WHERE e.event_type_id = schedule_event_types.id), 1))
WHERE has_level = 1;

-- +goose StatementEnd
