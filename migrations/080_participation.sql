-- +goose Up

-- Event participation: per-member results for the events that mail a ranked board
-- after they finish, built ONCE as a framework over schedule_events occurrences
-- rather than as a table pair per event (the mistake upstream made three times).
--
-- It re-scopes 033's season_trackables pattern from a season to an event type: a
-- declared metric set per type (participation_trackables) plus an EAV value store
-- (participation_values). Rank is a column, not a trackable — it is the one measure
-- every mail has, and it is the natural key within a board.
--
-- Recording is OPTIONAL. An occurrence with no board means nobody recorded it and
-- says nothing about any member; a view only ever speaks about boards that exist.
--
-- foreign_keys is off app-wide, so every ON DELETE clause below is documentation.
-- Each delete path clears children explicitly (deleteMemberTx, the board DELETE,
-- deleteScheduleEvent's refusal, the season purge's detach).

-- A row's presence IS the "tracks participation" flag for a type. Kept off
-- schedule_event_types so a generic table does not grow three participation-only
-- columns; still keyed on event_type_id, so a renamed or retyped event (074, 075)
-- keeps its participation.
--
-- absence_rule is the one per-type judgement the derivation needs, and getting it
-- wrong manufactures accusations, which is why it is declared rather than assumed:
--   absent — no entry is a miss (Alliance Exercise: appearing means you attacked)
--   zero   — an entry whose primary value is 0 is a miss; absence is blameless
--            (Zombie Siege: the game picks who is attacked)
--   role   — a STARTER with no entry is a miss; a sub is never penalised
--            (Desert Storm: 20 on the battlefield, subs only enter when one leaves)
-- +goose StatementBegin
CREATE TABLE participation_types (
    event_type_id INTEGER PRIMARY KEY REFERENCES schedule_event_types(id) ON DELETE CASCADE,
    absence_rule  TEXT NOT NULL CHECK(absence_rule IN ('absent','zero','role')),
    strike_type   TEXT NOT NULL              -- strike_types.key for a confirmed suggestion
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE participation_trackables (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type_id INTEGER NOT NULL REFERENCES schedule_event_types(id) ON DELETE CASCADE,
    key           TEXT NOT NULL,
    label         TEXT NOT NULL,
    sort_order    INTEGER NOT NULL DEFAULT 0,   -- lowest is the PRIMARY value (the 'zero' rule reads it)
    UNIQUE(event_type_id, key)
);
-- +goose StatementEnd

-- One board per occurrence. result_json holds the occurrence-level result (MVP
-- block, scores) opaquely: it differs per type and the future mail import (#143)
-- is what will care about its shape.
-- +goose StatementBegin
CREATE TABLE participation_boards (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    schedule_event_id INTEGER NOT NULL UNIQUE REFERENCES schedule_events(id) ON DELETE CASCADE,
    source            TEXT NOT NULL DEFAULT 'manual',   -- 'manual' | 'legacy' (081) | later 'import'
    result_json       TEXT NOT NULL DEFAULT '{}',
    notes             TEXT NOT NULL DEFAULT '',
    recorded_by       INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at        DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

-- name_snapshot is what the game called the player that day: members get renamed,
-- and an EX member who returns may come back under a new name. member_id is NULL
-- for a row nobody matched to the roster; CASCADE (documentary here) covers
-- "matched, then deleted" — participation lives until the member does.
-- +goose StatementBegin
CREATE TABLE participation_entries (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    board_id      INTEGER NOT NULL REFERENCES participation_boards(id) ON DELETE CASCADE,
    member_id     INTEGER REFERENCES members(id) ON DELETE CASCADE,
    name_snapshot TEXT NOT NULL,
    rank          INTEGER
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX idx_pe_rank ON participation_entries(board_id, rank) WHERE rank IS NOT NULL;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX idx_pe_member ON participation_entries(board_id, member_id) WHERE member_id IS NOT NULL;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_pe_board ON participation_entries(board_id);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_pe_member_id ON participation_entries(member_id);
-- +goose StatementEnd

-- Raw integers: a mail's 81.20G is stored as 81200000000.
-- +goose StatementBegin
CREATE TABLE participation_values (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    entry_id     INTEGER NOT NULL REFERENCES participation_entries(id) ON DELETE CASCADE,
    trackable_id INTEGER NOT NULL REFERENCES participation_trackables(id),
    value        INTEGER NOT NULL DEFAULT 0,
    UNIQUE(entry_id, trackable_id)
);
-- +goose StatementEnd

-- Per-occurrence role snapshot for the 'role' rule. The Desert Storm planner is
-- overwritten between battles, so the roles that decide who is suggested for a
-- strike are captured onto the board when it is first recorded.
-- +goose StatementBegin
CREATE TABLE participation_roles (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    board_id   INTEGER NOT NULL REFERENCES participation_boards(id) ON DELETE CASCADE,
    member_id  INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    role       TEXT CHECK(role IN ('starter','sub')),
    task_force TEXT CHECK(task_force IN ('A','B')),
    UNIQUE(board_id, member_id)
);
-- +goose StatementEnd

-- Stored human judgements, one per (board, member). Attendance itself is DERIVED
-- from entries + roles + the type's rule; these are the parts no number can give:
--   excused   — a reason and its author; overrides a derived miss
--   dismissed — "do not suggest a strike"; keeps the derived status, so a re-save
--               does not resurface a suggestion someone already declined
--   missed    — written by migration 081 ONLY, for legacy storm_attendance no_show
--               rows, which have no role to derive a miss from
-- +goose StatementBegin
CREATE TABLE participation_exceptions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    board_id    INTEGER NOT NULL REFERENCES participation_boards(id) ON DELETE CASCADE,
    member_id   INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK(kind IN ('excused','dismissed','missed')),
    reason      TEXT NOT NULL DEFAULT '',
    recorded_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(board_id, member_id)
);
-- +goose StatementEnd

-- The two strike categories confirmed suggestions file under, as system rows in
-- 079's managed list (Desert Storm reuses storm_no_show). Inserted if absent, then
-- promoted — an install whose free-text history already held one of these keys got
-- it as a custom row from 079, and code now writes it.
-- +goose StatementBegin
INSERT INTO strike_types (key, label, is_system, active, sort_order)
SELECT 'exercise_no_show', 'Alliance Exercise No-Show', 1, 1, (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM strike_types)
WHERE NOT EXISTS (SELECT 1 FROM strike_types WHERE key = 'exercise_no_show');
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO strike_types (key, label, is_system, active, sort_order)
SELECT 'zs_no_defense', 'Zombie Siege Not Defended', 1, 1, (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM strike_types)
WHERE NOT EXISTS (SELECT 1 FROM strike_types WHERE key = 'zs_no_defense');
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE strike_types SET is_system = 1, active = 1 WHERE key IN ('exercise_no_show', 'zs_no_defense');
-- +goose StatementEnd

-- Tracked types. Only system types are tracked (owner, 2026-09-21), but the flag
-- is independent of is_system: Sky Predator is a system type with no post-event
-- mail, so it gets no row.
-- +goose StatementBegin
INSERT INTO participation_types (event_type_id, absence_rule, strike_type)
SELECT id, 'absent', 'exercise_no_show' FROM schedule_event_types
WHERE short_name IN ('MG', 'LS') AND is_system = 1
  AND id NOT IN (SELECT event_type_id FROM participation_types);
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO participation_types (event_type_id, absence_rule, strike_type)
SELECT id, 'zero', 'zs_no_defense' FROM schedule_event_types
WHERE short_name = 'ZS' AND is_system = 1
  AND id NOT IN (SELECT event_type_id FROM participation_types);
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO participation_trackables (event_type_id, key, label, sort_order)
SELECT id, 'damage', 'Total Damage', 0 FROM schedule_event_types
WHERE short_name IN ('MG', 'LS') AND is_system = 1
  AND NOT EXISTS (SELECT 1 FROM participation_trackables pt WHERE pt.event_type_id = schedule_event_types.id AND pt.key = 'damage');
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO participation_trackables (event_type_id, key, label, sort_order)
SELECT id, 'waves', 'Waves', 0 FROM schedule_event_types
WHERE short_name = 'ZS' AND is_system = 1
  AND NOT EXISTS (SELECT 1 FROM participation_trackables pt WHERE pt.event_type_id = schedule_event_types.id AND pt.key = 'waves');
-- +goose StatementEnd

-- view_participation / manage_participation on for R4 and R5, where view_storm /
-- manage_storm sit today. An unknown key reads as false (middleware.go), so without
-- this seed the feature would be off for every rank. Only set where absent, so an
-- operator's own choice is never overwritten.
-- +goose StatementBegin
UPDATE rank_permissions
SET permissions = json_set(permissions, '$.view_participation', json('true'))
WHERE rank IN ('R4', 'R5') AND json_type(permissions, '$.view_participation') IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE rank_permissions
SET permissions = json_set(permissions, '$.manage_participation', json('true'))
WHERE rank IN ('R4', 'R5') AND json_type(permissions, '$.manage_participation') IS NULL;
-- +goose StatementEnd
