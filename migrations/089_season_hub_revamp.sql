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

-- +goose Down
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
