-- +goose Up

-- Season Hub revamp (Project 21, private-docs 269). One file for the feature's schema; each
-- section is one issue's. Tables use CREATE TABLE IF NOT EXISTS and seeds INSERT … WHERE NOT
-- EXISTS; the column additions cannot be guarded in SQLite's SQL, so a database that applied an
-- earlier draft of this file is re-created rather than re-migrated.
--
-- foreign_keys is off app-wide, so every REFERENCES clause below is documentation: the season
-- delete, the member delete and the user delete (handleSeasonDelete, deleteMemberTx,
-- clearUserReferences) list each new table or column explicitly.

-- ── 270: configuration, layout, archive snapshots ───────────────────────────
-- key_event_total is the number of key events (wars) the season schedules — S4: 8 Copper Wars —
-- beside key_event_required. resource_name is the season resource (S4: Copper). The poll and war
-- floors are NULL until set, meaning "the overall floors". schedule_buffer_minutes is the window
-- conflict detection looks at; land_readiness_buffer_hours how long before an unlock a target
-- should be connected (S4: 24 h).
-- +goose StatementBegin
ALTER TABLE seasons ADD COLUMN key_event_total INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE seasons ADD COLUMN resource_name TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE seasons ADD COLUMN poll_active_min_pct INTEGER;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE seasons ADD COLUMN poll_at_risk_min_pct INTEGER;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE seasons ADD COLUMN war_active_min_pct INTEGER;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE seasons ADD COLUMN war_at_risk_min_pct INTEGER;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE seasons ADD COLUMN schedule_buffer_minutes INTEGER NOT NULL DEFAULT 60;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE seasons ADD COLUMN land_readiness_buffer_hours INTEGER NOT NULL DEFAULT 24;
-- +goose StatementEnd

-- The Hub's tabs and the season dashboard's cards, per season: shown or hidden, renamed,
-- reordered. Keys are stable (seasonTabKeys / seasonCardKeys in Go); a reader merges in any key a
-- season lacks, so a later release's new tab needs no write on read.
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS season_tabs (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    season_id INTEGER NOT NULL REFERENCES seasons(id),
    tab_key   TEXT    NOT NULL,
    visible   INTEGER NOT NULL DEFAULT 1,
    label     TEXT    NOT NULL DEFAULT '',
    position  INTEGER NOT NULL DEFAULT 0,
    UNIQUE(season_id, tab_key)
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS season_dashboard_cards (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    season_id INTEGER NOT NULL REFERENCES seasons(id),
    card_key  TEXT    NOT NULL,
    visible   INTEGER NOT NULL DEFAULT 1,
    label     TEXT    NOT NULL DEFAULT '',
    position  INTEGER NOT NULL DEFAULT 0,
    UNIQUE(season_id, card_key)
);
-- +goose StatementEnd

-- The names and ranks an archived season froze (Pudgey: frozen, with a link to the current
-- profile). Written by archiveSeason; for seasons archived before this release, once here and
-- marked as_of_migration — those names are as of the upgrade, not the season's end.
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS season_roster_snapshot (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    season_id       INTEGER NOT NULL REFERENCES seasons(id),
    member_id       INTEGER NOT NULL,
    name            TEXT    NOT NULL,
    rank            TEXT    NOT NULL,
    snapshot_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
    as_of_migration INTEGER NOT NULL DEFAULT 0,
    UNIQUE(season_id, member_id)
);
-- +goose StatementEnd

-- A copy of each file tagged to a season, made when it archives (owner, 2026-10-19). The bytes
-- live at <storage>/season-<id>/<file_name>; effective_min_rank is the file's view rank with
-- every tag's folded in, frozen because tags can change after the copy cannot.
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS season_file_snapshots (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    season_id          INTEGER NOT NULL REFERENCES seasons(id),
    file_id            INTEGER NOT NULL,
    title              TEXT    NOT NULL,
    file_name          TEXT    NOT NULL,
    file_type          TEXT    NOT NULL,
    effective_min_rank TEXT    NOT NULL DEFAULT 'R1',
    owner_user_id      INTEGER,
    snapshot_at        DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(season_id, file_id)
);
-- +goose StatementEnd

-- Default layout rows for every existing season (no write on read; the reader still merges).
-- +goose StatementBegin
INSERT INTO season_tabs (season_id, tab_key, visible, position)
SELECT s.id, k.tab_key, 1, k.pos FROM seasons s
CROSS JOIN (SELECT 'dashboard' AS tab_key, 0 AS pos UNION ALL SELECT 'rankings', 1
    UNION ALL SELECT 'participation', 2 UNION ALL SELECT 'contributions', 3
    UNION ALL SELECT 'rewards', 4 UNION ALL SELECT 'land', 5 UNION ALL SELECT 'war', 6
    UNION ALL SELECT 'schedule', 7 UNION ALL SELECT 'comms', 8 UNION ALL SELECT 'files', 9) k
WHERE NOT EXISTS (SELECT 1 FROM season_tabs t WHERE t.season_id = s.id AND t.tab_key = k.tab_key);
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO season_dashboard_cards (season_id, card_key, visible, position)
SELECT s.id, k.card_key, 1, k.pos FROM seasons s
CROSS JOIN (SELECT 'faction_allies' AS card_key, 0 AS pos UNION ALL SELECT 'leadership_flags', 1
    UNION ALL SELECT 'season_schedule', 2 UNION ALL SELECT 'land_status', 3
    UNION ALL SELECT 'war_record', 4 UNION ALL SELECT 'reward_tier_status', 5) k
WHERE NOT EXISTS (SELECT 1 FROM season_dashboard_cards c WHERE c.season_id = s.id AND c.card_key = k.card_key);
-- +goose StatementEnd

-- Already-archived seasons: their roster is everyone with a row for the season, named as of now.
-- +goose StatementBegin
INSERT INTO season_roster_snapshot (season_id, member_id, name, rank, as_of_migration)
SELECT s.id, m.id, m.name, m.rank, 1 FROM seasons s JOIN members m
WHERE s.archived_at IS NOT NULL
  AND (m.id IN (SELECT member_id FROM season_participation WHERE season_id = s.id)
    OR m.id IN (SELECT member_id FROM season_member_records WHERE season_id = s.id)
    OR m.id IN (SELECT member_id FROM season_rewards WHERE season_id = s.id))
  AND NOT EXISTS (SELECT 1 FROM season_roster_snapshot r WHERE r.season_id = s.id AND r.member_id = m.id);
-- +goose StatementEnd


-- ── 304: the Season Hub's permission split ──────────────────────────────────
-- Three keys become nine (owner, 2026-10-19): manage_season_hub retires into
-- manage_season_participation and manage_season_contributions; manage_season_config takes the
-- season's setup from manage_season_rewards, which keeps only the rewards themselves; Land and
-- War get a view and a manage key each. Each new key is seeded from the key its scope lived
-- under, so no rank loses access on upgrade. 085's shape: json('true'), never a json_extract
-- copy (that writes the number 1, which getRankPermissions reads as false), and only where the
-- key is absent, so an operator's later choice is never overwritten. manage_season_hub is left
-- in the blob as dead data; nothing reads it.
-- +goose StatementBegin
UPDATE rank_permissions
SET permissions = json_set(permissions, '$.manage_season_config', json('true'))
WHERE json_extract(permissions, '$.manage_season_rewards') = 1
  AND json_type(permissions, '$.manage_season_config') IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE rank_permissions
SET permissions = json_set(permissions, '$.manage_season_participation', json('true'))
WHERE json_extract(permissions, '$.manage_season_hub') = 1
  AND json_type(permissions, '$.manage_season_participation') IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE rank_permissions
SET permissions = json_set(permissions, '$.manage_season_contributions', json('true'))
WHERE json_extract(permissions, '$.manage_season_hub') = 1
  AND json_type(permissions, '$.manage_season_contributions') IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE rank_permissions
SET permissions = json_set(permissions, '$.view_season_land', json('true'))
WHERE rank IN ('R4', 'R5') AND json_type(permissions, '$.view_season_land') IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE rank_permissions
SET permissions = json_set(permissions, '$.manage_season_land', json('true'))
WHERE rank IN ('R4', 'R5') AND json_type(permissions, '$.manage_season_land') IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE rank_permissions
SET permissions = json_set(permissions, '$.view_season_war', json('true'))
WHERE rank IN ('R4', 'R5') AND json_type(permissions, '$.view_season_war') IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE rank_permissions
SET permissions = json_set(permissions, '$.manage_season_war', json('true'))
WHERE rank IN ('R4', 'R5') AND json_type(permissions, '$.manage_season_war') IS NULL;
-- +goose StatementEnd


-- ── 271: factions and season alliances ──────────────────────────────────────
-- A season's factions (S4: Kage no Sato and Koubutai), one flagged ours; entered by an officer,
-- since they are the install's side and no template knows them.
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS season_factions (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    season_id INTEGER NOT NULL REFERENCES seasons(id),
    key       TEXT    NOT NULL,
    label     TEXT    NOT NULL,
    is_ours   INTEGER NOT NULL DEFAULT 0,
    position  INTEGER NOT NULL DEFAULT 0,
    UNIQUE(season_id, key)
);
-- +goose StatementEnd
-- One row per (season, registry alliance): the alliance's faction that season, the relationship
-- (unknown is not enemy), the season's terms and the matchup fields. The Ally modal and the War
-- tab write the same row, so nothing is entered twice, and an alliance's S5 row never touches its
-- S4 row. faction_id is a game fact (the mobile store may set it); the rest is judgement.
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS season_alliances (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    season_id            INTEGER NOT NULL REFERENCES seasons(id),
    external_alliance_id INTEGER NOT NULL REFERENCES external_alliances(id),
    faction_id           INTEGER REFERENCES season_factions(id),
    relationship         TEXT    NOT NULL DEFAULT 'unknown'
                         CHECK(relationship IN ('allied','enemy','neutral','unknown')),
    terms                TEXT    NOT NULL DEFAULT '',
    notes                TEXT    NOT NULL DEFAULT '',
    group_rank           INTEGER,
    assessment_override  TEXT,
    assessment_note      TEXT    NOT NULL DEFAULT '',
    updated_at           DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(season_id, external_alliance_id)
);
-- +goose StatementEnd


-- ── 272: season tags on Comms and Files ─────────────────────────────────────
-- comms_templates already carries season_id (Season Mail). Resources, polls (templates and
-- launched instances — an instance inherits its template's season at launch) and files gain one.
-- A season delete clears these tags rather than deleting the records: they belong to their own
-- features. Tagging anything to an archived season is refused in Go.
-- +goose StatementBegin
ALTER TABLE comms_resources ADD COLUMN season_id INTEGER REFERENCES seasons(id);
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE poll_templates ADD COLUMN season_id INTEGER REFERENCES seasons(id);
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE poll_instances ADD COLUMN season_id INTEGER REFERENCES seasons(id);
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE files ADD COLUMN season_id INTEGER REFERENCES seasons(id);
-- +goose StatementEnd


-- ── 273: season and planning fields on schedule events; sourced origins ─────
-- An ordinary calendar row gains a season, a status (tentative / confirmed / completed /
-- cancelled), leadership_only (hidden from anyone without manage_schedule — every reader,
-- the announcement included), show_on_main (off: the row shows on the season timeline only)
-- and a reference link. A cancelled row is history: it leaves the rule, duplicate and
-- generator checks (and the Desert Storm index below), so it never blocks its replacement.
-- +goose StatementBegin
ALTER TABLE schedule_events ADD COLUMN season_id INTEGER REFERENCES seasons(id);
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE schedule_events ADD COLUMN status TEXT NOT NULL DEFAULT 'confirmed'
    CHECK(status IN ('tentative','confirmed','completed','cancelled'));
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE schedule_events ADD COLUMN leadership_only INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE schedule_events ADD COLUMN show_on_main INTEGER NOT NULL DEFAULT 1;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE schedule_events ADD COLUMN reference_url TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd
-- A row a Land or War record owns (land_capture, land_drop, war, trade_post): one row per
-- source, created, moved and cancelled only by syncSourcedScheduleEvent. The 078 pattern for a
-- second kind of origin; a war that adopts a pushed occurrence carries both.
-- +goose StatementBegin
ALTER TABLE schedule_events ADD COLUMN source_kind TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE schedule_events ADD COLUMN source_id INTEGER;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX IF NOT EXISTS idx_schedule_events_source ON schedule_events(source_kind, source_id)
    WHERE source_kind IS NOT NULL;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_schedule_events_task_force;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX idx_schedule_events_task_force
    ON schedule_events(event_date, event_type_id, task_force) WHERE task_force IS NOT NULL AND status != 'cancelled';
-- +goose StatementEnd

-- The three types Land and Trade Posts write through. System rows, so no officer can delete or
-- rename them from under the sync (the system-only RULES key on MG/LS/ZS/DS short names, so none
-- applies); announce off, so planning rows stay out of the nightly announcement unless an
-- officer opts the type in. An install that already uses one of the short names gets a longer
-- one; a type already called by the name is adopted.
-- +goose StatementBegin
INSERT INTO schedule_event_types (name, short_name, icon, is_system, sort_order, announce)
SELECT 'Land Capture', CASE WHEN EXISTS (SELECT 1 FROM schedule_event_types WHERE short_name = 'LC') THEN 'LCAP' ELSE 'LC' END,
       '🏙️', 1, 90, 0
WHERE NOT EXISTS (SELECT 1 FROM schedule_event_types WHERE name = 'Land Capture');
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO schedule_event_types (name, short_name, icon, is_system, sort_order, announce)
SELECT 'Land Abandonment', CASE WHEN EXISTS (SELECT 1 FROM schedule_event_types WHERE short_name = 'LA') THEN 'LABN' ELSE 'LA' END,
       '🏳️', 1, 91, 0
WHERE NOT EXISTS (SELECT 1 FROM schedule_event_types WHERE name = 'Land Abandonment');
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO schedule_event_types (name, short_name, icon, is_system, sort_order, announce)
SELECT 'Trade Post Action', CASE WHEN EXISTS (SELECT 1 FROM schedule_event_types WHERE short_name = 'TPA') THEN 'TPAC' ELSE 'TPA' END,
       '🏪', 1, 92, 0
WHERE NOT EXISTS (SELECT 1 FROM schedule_event_types WHERE name = 'Trade Post Action');
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE schedule_event_types SET is_system = 1
WHERE name IN ('Land Capture', 'Land Abandonment', 'Trade Post Action');
-- +goose StatementEnd

-- Tag pushed rows with their season, so the season timeline needs no join: by origin (078)
-- where the row has one, else by the push's own legacy match — a row of a template row's type
-- on the date that row computes. Seasons pushed before 078 and never re-pushed (S2 and S3 on
-- most installs) have only the second. This tags; it never claims an origin.
-- +goose StatementBegin
UPDATE schedule_events SET season_id = (SELECT se.season_id FROM season_events se WHERE se.id = schedule_events.season_event_id)
WHERE season_id IS NULL AND season_event_id IS NOT NULL;
-- +goose StatementEnd
-- +goose StatementBegin
WITH RECURSIVE wk(n) AS (SELECT -2 UNION ALL SELECT n + 1 FROM wk WHERE n < 60),
cand AS (
    SELECT s.id AS season_id, se.event_type_id AS type_id,
           date(s.start_date, printf('%+d days', (wk.n - 1) * 7 + se.day_offset - 1)) AS d
    FROM season_events se JOIN seasons s ON s.id = se.season_id JOIN wk
    WHERE se.is_server_event = 0 AND se.day_offset IS NOT NULL AND se.event_type_id IS NOT NULL
      AND wk.n >= se.week_start
      AND wk.n <= CASE WHEN se.week_end < se.week_start THEN s.week_count ELSE se.week_end END
)
UPDATE schedule_events SET season_id = (SELECT c.season_id FROM cand c
    WHERE c.d = schedule_events.event_date AND c.type_id = schedule_events.event_type_id LIMIT 1)
WHERE season_id IS NULL AND season_event_id IS NULL AND EXISTS (SELECT 1 FROM cand c
    WHERE c.d = schedule_events.event_date AND c.type_id = schedule_events.event_type_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_schedule_events_task_force;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_schedule_events_source;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE schedule_events DROP COLUMN source_id;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE schedule_events DROP COLUMN source_kind;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE schedule_events DROP COLUMN reference_url;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE schedule_events DROP COLUMN show_on_main;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE schedule_events DROP COLUMN leadership_only;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE schedule_events DROP COLUMN status;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE schedule_events DROP COLUMN season_id;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX idx_schedule_events_task_force
    ON schedule_events(event_date, event_type_id, task_force) WHERE task_force IS NOT NULL;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE files DROP COLUMN season_id;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE poll_instances DROP COLUMN season_id;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE poll_templates DROP COLUMN season_id;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE comms_resources DROP COLUMN season_id;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS season_alliances;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS season_factions;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS season_file_snapshots;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS season_roster_snapshot;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS season_dashboard_cards;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS season_tabs;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE seasons DROP COLUMN land_readiness_buffer_hours;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE seasons DROP COLUMN schedule_buffer_minutes;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE seasons DROP COLUMN war_at_risk_min_pct;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE seasons DROP COLUMN war_active_min_pct;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE seasons DROP COLUMN poll_at_risk_min_pct;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE seasons DROP COLUMN poll_active_min_pct;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE seasons DROP COLUMN resource_name;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE seasons DROP COLUMN key_event_total;
-- +goose StatementEnd
