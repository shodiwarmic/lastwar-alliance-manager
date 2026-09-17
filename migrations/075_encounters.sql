-- +goose Up

-- A server event is a WINDOW; the things that happen inside it are encounters.
--
-- `server_events` already models the windows correctly — General's Trial every 14
-- days for 3 days, Zombie Invasion likewise. But the encounters inside them (Sky
-- Predator, Glacieradon) were hand-entered custom event types with no link to the
-- window, so an officer re-derived a cadence the app already knew, and nothing
-- stopped an encounter being placed on a date its parent window does not cover.
--
-- The link is one nullable column on the type. Any type may carry it, system or
-- custom: Glacieradon stays a custom type and still gets its parent, because the
-- window rule is a property of the link, not of is_system.

-- +goose StatementBegin
ALTER TABLE schedule_event_types ADD COLUMN server_event_id INTEGER REFERENCES server_events(id);
-- +goose StatementEnd

-- The game renamed Ironclad Vehicle to Rally Challenge. Conditional on the target
-- short name being free so a re-run, or an install that already renamed it by
-- hand, changes nothing. (The 025 seed is edited in the same commit for fresh
-- installs — safe per that migration's own precedent note: goose tracks applied
-- migrations by version number and never re-reads the body.)
-- +goose StatementBegin
UPDATE server_events SET name = 'Rally Challenge', short_name = 'RC'
WHERE name = 'Ironclad Vehicle'
  AND NOT EXISTS (SELECT 1 FROM server_events WHERE short_name = 'RC');
-- +goose StatementEnd

-- Sky Predator is promoted to a system type with its own level scale and its
-- parent window. It is promoted IN PLACE where it already exists, so the five
-- recorded events keep their rows — retyping them onto a fresh row would lose the
-- history the schedule is for.
--
-- The seeded baseline and ceiling come from what this install has already
-- scheduled (migration 069's rule), floored at 1. The seed CANNOT know the
-- alliance's real Sky Predator level: the five existing rows carry no level at all,
-- because the type could not hold one until now. Setting it is the operator's first
-- action after this upgrade, in Schedule → Settings and Settings → Game Limits.
-- +goose StatementBegin
UPDATE schedule_event_types
SET name            = 'Sky Predator',
    short_name      = 'SP',
    is_system       = 1,
    has_level       = 1,
    baseline_level  = max(1, COALESCE((SELECT max(e.level) FROM schedule_events e
                                       WHERE e.event_type_id = schedule_event_types.id), 1)),
    max_level       = max(1, COALESCE((SELECT max(e.level) FROM schedule_events e
                                       WHERE e.event_type_id = schedule_event_types.id), 1)),
    server_event_id = (SELECT id FROM server_events WHERE name = 'General''s Trial')
WHERE name = 'Sky Predator (GT)'
  AND NOT EXISTS (SELECT 1 FROM schedule_event_types WHERE short_name = 'SP');
-- +goose StatementEnd

-- Fresh installs, and installs that never created the type by hand, get it seeded.
-- 1/1 exactly as 025 seeds MG: the app does not know the game's real numbers and
-- does not pretend to.
-- +goose StatementBegin
INSERT INTO schedule_event_types (name, short_name, icon, is_system, active, sort_order, has_level, baseline_level, max_level, server_event_id)
SELECT 'Sky Predator', 'SP', '🦅', 1, 1, 20, 1, 1, 1,
       (SELECT id FROM server_events WHERE name = 'General''s Trial')
WHERE NOT EXISTS (SELECT 1 FROM schedule_event_types WHERE short_name = 'SP');
-- +goose StatementEnd

-- Glacieradon stays a CUSTOM type — the game does not level it — but gets its
-- parent link. The window rule applies to any type carrying one.
-- +goose StatementBegin
UPDATE schedule_event_types
SET server_event_id = (SELECT id FROM server_events WHERE name = 'Zombie Invasion')
WHERE name = 'Glacieradon (ZI)' AND server_event_id IS NULL;
-- +goose StatementEnd
