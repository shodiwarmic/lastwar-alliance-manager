-- +goose Up

-- Per-event-type ceilings for MG and ZS event levels, replacing a hardcoded
-- max="30" that lived only in three HTML attributes in templates/schedule.html.
--
-- The 30 was arbitrary rather than merely outdated: it admitted 13-30, which the
-- game never had (MG's original scale topped out at 12), while rejecting the
-- levels the game has since unlocked. It was also browser-side only -- neither
-- createScheduleEvent nor updateSettings validated a level at all, so a crafted
-- request already stored any integer.
--
-- Two columns, not one shared max_event_level: the MG and ZS ceilings diverge in
-- practice. Modelled on max_hq_level ("Update this when the game increases the
-- level cap"), which exists for exactly this reason.
--
-- Validation is a plain permissive range, 1 <= level <= max. The real scale is two
-- overlapping segments ({1..12} then tens), which no CHECK or step describes and
-- which would make historical events uneditable; a slightly permissive range that
-- accepts a level the game lacks is the deliberate trade.

-- +goose StatementBegin
ALTER TABLE settings ADD COLUMN max_mg_level INTEGER NOT NULL DEFAULT 1;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE settings ADD COLUMN max_zs_level INTEGER NOT NULL DEFAULT 1;
-- +goose StatementEnd

-- Seed each ceiling from what this install has ALREADY scheduled, not from a
-- constant: the app never has to know the game's true values, which is what stops
-- this recurring at the next level release. It also guarantees the new validation
-- invalidates no existing event and no existing baseline -- every stored level is
-- <= its seeded ceiling by construction.
--
-- The baseline term matters on a fresh install, where no events exist yet. With
-- the starting defaults of 1 (see 025) a fresh ceiling seeds to 1 and the operator
-- raises it; a database that still holds the old 11/7 defaults seeds to those
-- instead, which is correct for it.
-- +goose StatementBegin
UPDATE settings SET
    max_mg_level = max(1, COALESCE(mg_baseline, 1),
        COALESCE((SELECT max(e.level) FROM schedule_events e
                  JOIN schedule_event_types t ON t.id = e.event_type_id
                  WHERE t.short_name = 'MG'), 0)),
    max_zs_level = max(1, COALESCE(zs_baseline, 1),
        COALESCE((SELECT max(e.level) FROM schedule_events e
                  JOIN schedule_event_types t ON t.id = e.event_type_id
                  WHERE t.short_name = 'ZS'), 0))
WHERE id = 1;
-- +goose StatementEnd
