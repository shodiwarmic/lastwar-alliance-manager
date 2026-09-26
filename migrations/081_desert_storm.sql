-- +goose Up

-- Desert Storm gets occurrences. The whole storm_* subsystem is dateless — it
-- describes the CURRENT lineup and is overwritten between battles — so nothing
-- recorded that a battle happened on a date, and participation (080) had nothing to
-- attach to. Each task force fights its own battle and gets its own mail, so an
-- occurrence is a schedule_events row of the Desert Storm type carrying its task
-- force.
--
-- task_force is a NULLABLE attribute, not two event types: legacy storm_attendance
-- has no task_force column, so a migrated battle can only honestly say "Desert Storm
-- on this date, task force unknown". The validator requires it on every NEW row;
-- NULL occurs only on legacy rows.
-- +goose StatementBegin
ALTER TABLE schedule_events ADD COLUMN task_force TEXT CHECK(task_force IN ('A','B'));
-- +goose StatementEnd

-- One battle per task force per date, as a database fact. Makes the generator
-- idempotent and a racing duplicate create fail; the validator carries the same rule
-- so the rejection can name it. Partial, so legacy NULL rows are untouched.
-- +goose StatementBegin
CREATE UNIQUE INDEX idx_schedule_events_task_force
    ON schedule_events(event_date, event_type_id, task_force) WHERE task_force IS NOT NULL;
-- +goose StatementEnd

-- The type, promoted IN PLACE if an install already made one by hand (075's
-- pattern for Sky Predator), so its recorded rows keep their history. A row with
-- short name DS is preferred over one merely named "Desert Storm". The rename is
-- skipped if another row already holds the name, rather than failing the upgrade.
-- +goose StatementBegin
UPDATE schedule_event_types
SET short_name = 'DS',
    name       = CASE WHEN EXISTS (SELECT 1 FROM schedule_event_types o
                                   WHERE o.name = 'Desert Storm' AND o.id != schedule_event_types.id)
                      THEN name ELSE 'Desert Storm' END,
    is_system  = 1,
    active     = 1,
    has_level  = 0,
    baseline_level = NULL,
    max_level  = NULL
WHERE id = (SELECT id FROM (
              SELECT id, 0 AS pref FROM schedule_event_types WHERE short_name = 'DS'
              UNION ALL
              SELECT id, 1 AS pref FROM schedule_event_types WHERE name = 'Desert Storm'
            ) ORDER BY pref, id LIMIT 1);
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO schedule_event_types (name, short_name, icon, is_system, active, sort_order, has_level, announce)
SELECT 'Desert Storm', 'DS', '⚡', 1, 1, 3, 0, 1
WHERE NOT EXISTS (SELECT 1 FROM schedule_event_types WHERE short_name = 'DS');
-- +goose StatementEnd

-- Tracked under the 'role' rule: a starter absent from the Individual Points board
-- missed the battle; an absent sub is never counted (the battlefield holds 20, and a
-- sub only enters when a starter leaves). Confirmed suggestions reuse the existing
-- storm_no_show category, so old and new Desert Storm strikes stay one type.
-- +goose StatementBegin
INSERT INTO participation_types (event_type_id, absence_rule, strike_type)
SELECT id, 'role', 'storm_no_show' FROM schedule_event_types
WHERE short_name = 'DS' AND id NOT IN (SELECT event_type_id FROM participation_types);
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO participation_trackables (event_type_id, key, label, sort_order)
SELECT id, 'points', 'Individual Points', 0 FROM schedule_event_types
WHERE short_name = 'DS'
  AND NOT EXISTS (SELECT 1 FROM participation_trackables pt WHERE pt.event_type_id = schedule_event_types.id AND pt.key = 'points');
-- +goose StatementEnd

-- --- Legacy storm_attendance → occurrences + boards ---------------------------------
--
-- Migrated, not retired: our install holds one date, but another operator may hold
-- many (never-assume-single-install). One occurrence per distinct storm_date. Every
-- per-member statement inner-joins members: deleteMember never cleaned
-- storm_attendance, so an orphan member_id is possible elsewhere, and carrying it
-- across would only mint another orphan.
--
-- The occurrence's author (created_by is NOT NULL) is the officer who recorded that
-- date, falling back to the lowest user id — a forced choice, and the honest one.
-- +goose StatementBegin
CREATE TEMP TABLE legacy_ds AS
SELECT sa.storm_date AS d,
       COALESCE(
         (SELECT MIN(sa2.recorded_by) FROM storm_attendance sa2
           WHERE sa2.storm_date = sa.storm_date AND sa2.recorded_by IN (SELECT id FROM users)),
         (SELECT MIN(id) FROM users), 0) AS author,
       MIN(sa.created_at) AS created
FROM storm_attendance sa
JOIN members m ON m.id = sa.member_id
GROUP BY sa.storm_date;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TEMP TABLE legacy_ds_type AS SELECT id FROM schedule_event_types WHERE short_name = 'DS';
-- +goose StatementEnd

-- A DS row can already exist on a legacy date only where the type was promoted from
-- a hand-made one above. Then the legacy board attaches to that row (below) rather
-- than a second, duplicate occurrence being created.
-- +goose StatementBegin
INSERT INTO schedule_events (event_date, event_type_id, event_time, all_day, notes, created_by, created_at, updated_at)
SELECT l.d, (SELECT id FROM legacy_ds_type), '00:00', 1, '', l.author, l.created, l.created
FROM legacy_ds l
WHERE NOT EXISTS (SELECT 1 FROM schedule_events se
                  WHERE se.event_date = l.d AND se.event_type_id = (SELECT id FROM legacy_ds_type));
-- +goose StatementEnd

-- One legacy board per date, on the earliest DS row that date has with no board yet.
-- A date whose DS rows all carry a board already is the one case skipped. Written so
-- a re-run over its own output changes nothing.
-- +goose StatementBegin
INSERT INTO participation_boards (schedule_event_id, source, recorded_by, created_at, updated_at)
SELECT (SELECT MIN(se.id) FROM schedule_events se
         WHERE se.event_date = l.d AND se.event_type_id = (SELECT id FROM legacy_ds_type)
           AND se.id NOT IN (SELECT schedule_event_id FROM participation_boards)),
       'legacy', NULLIF(l.author, 0), l.created, l.created
FROM legacy_ds l
WHERE NOT EXISTS (SELECT 1 FROM participation_boards b
                  JOIN schedule_events se ON se.id = b.schedule_event_id
                  WHERE se.event_date = l.d AND se.event_type_id = (SELECT id FROM legacy_ds_type)
                    AND b.source = 'legacy')
  AND EXISTS (SELECT 1 FROM schedule_events se
              WHERE se.event_date = l.d AND se.event_type_id = (SELECT id FROM legacy_ds_type)
                AND se.id NOT IN (SELECT schedule_event_id FROM participation_boards));
-- +goose StatementEnd

-- The rows. The old statuses already encoded the role rule by hand, so they carry
-- across without guessing a role or a task force (both stay blank):
--   attended     → an entry with no rank and no score (present)
--   no_show      → a stored 'missed' — with no role to derive a miss from, the old
--                  status is kept as the human judgement it was. Only this migration
--                  ever writes kind 'missed'.
--   excused      → 'excused' with its reason
--   not_enrolled → nothing: they were never expected
-- +goose StatementBegin
INSERT INTO participation_entries (board_id, member_id, name_snapshot, rank)
SELECT b.id, sa.member_id, m.name, NULL
FROM storm_attendance sa
JOIN members m ON m.id = sa.member_id
JOIN schedule_events se ON se.event_date = sa.storm_date AND se.event_type_id = (SELECT id FROM legacy_ds_type)
JOIN participation_boards b ON b.schedule_event_id = se.id AND b.source = 'legacy'
WHERE sa.status = 'attended'
  AND NOT EXISTS (SELECT 1 FROM participation_entries e WHERE e.board_id = b.id AND e.member_id = sa.member_id);
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO participation_exceptions (board_id, member_id, kind, reason, recorded_by, created_at)
SELECT b.id, sa.member_id,
       CASE sa.status WHEN 'no_show' THEN 'missed' ELSE 'excused' END,
       CASE sa.status WHEN 'excused' THEN sa.excuse_reason ELSE '' END,
       (SELECT u.id FROM users u WHERE u.id = sa.recorded_by),
       sa.created_at
FROM storm_attendance sa
JOIN members m ON m.id = sa.member_id
JOIN schedule_events se ON se.event_date = sa.storm_date AND se.event_type_id = (SELECT id FROM legacy_ds_type)
JOIN participation_boards b ON b.schedule_event_id = se.id AND b.source = 'legacy'
WHERE sa.status IN ('no_show', 'excused')
  AND NOT EXISTS (SELECT 1 FROM participation_exceptions x WHERE x.board_id = b.id AND x.member_id = sa.member_id);
-- +goose StatementEnd

-- storm_attendance itself stays for one release: its readers are gone and its rows
-- are migrated, but it is the safety net for another operator's data, exactly like
-- a one-release script shim. Dropping it is a filed follow-up.
-- +goose StatementBegin
DROP TABLE legacy_ds;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE legacy_ds_type;
-- +goose StatementEnd
