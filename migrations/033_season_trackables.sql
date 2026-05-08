-- +goose Up
-- +goose StatementBegin

-- Defines what is tracked for a season (replaces hardcoded contribution columns).
CREATE TABLE season_trackables (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    season_id  INTEGER NOT NULL REFERENCES seasons(id) ON DELETE CASCADE,
    key        TEXT    NOT NULL,
    label      TEXT    NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    UNIQUE(season_id, key)
);
CREATE INDEX idx_st_season ON season_trackables(season_id);

-- EAV value store replacing season_contributions.
-- week_number=0 is the season-end snapshot (canonical tie-breaker source).
CREATE TABLE season_member_records (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    season_id      INTEGER NOT NULL REFERENCES seasons(id) ON DELETE CASCADE,
    member_id      INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    week_number    INTEGER NOT NULL DEFAULT 0,
    trackable_id   INTEGER NOT NULL REFERENCES season_trackables(id) ON DELETE CASCADE,
    recorded_value INTEGER NOT NULL DEFAULT 0,
    logged_by      INTEGER REFERENCES users(id) ON DELETE SET NULL,
    updated_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(season_id, member_id, week_number, trackable_id)
);
CREATE INDEX idx_smr_lookup ON season_member_records(season_id, member_id, week_number);

-- Seed the 4 standard trackables for every existing season.
INSERT INTO season_trackables (season_id, key, label, sort_order)
SELECT id, 'mutual_assistance', 'Mutual Assistance', 0 FROM seasons;

INSERT INTO season_trackables (season_id, key, label, sort_order)
SELECT id, 'siege', 'Siege', 1 FROM seasons;

INSERT INTO season_trackables (season_id, key, label, sort_order)
SELECT id, 'rare_soil_war', 'Rare Soil War', 2 FROM seasons;

INSERT INTO season_trackables (season_id, key, label, sort_order)
SELECT id, 'defeat', 'Defeat', 3 FROM seasons;

-- Migrate mutual_assistance data (non-zero rows only).
INSERT INTO season_member_records (season_id, member_id, week_number, trackable_id, recorded_value, logged_by)
SELECT sc.season_id, sc.member_id, sc.week_number, st.id, sc.mutual_assistance, sc.imported_by
FROM season_contributions sc
JOIN season_trackables st ON st.season_id = sc.season_id AND st.key = 'mutual_assistance'
WHERE sc.mutual_assistance != 0;

-- Migrate siege data (non-zero rows only).
INSERT INTO season_member_records (season_id, member_id, week_number, trackable_id, recorded_value, logged_by)
SELECT sc.season_id, sc.member_id, sc.week_number, st.id, sc.siege, sc.imported_by
FROM season_contributions sc
JOIN season_trackables st ON st.season_id = sc.season_id AND st.key = 'siege'
WHERE sc.siege != 0;

-- Migrate rare_soil_war data (non-zero rows only).
INSERT INTO season_member_records (season_id, member_id, week_number, trackable_id, recorded_value, logged_by)
SELECT sc.season_id, sc.member_id, sc.week_number, st.id, sc.rare_soil_war, sc.imported_by
FROM season_contributions sc
JOIN season_trackables st ON st.season_id = sc.season_id AND st.key = 'rare_soil_war'
WHERE sc.rare_soil_war != 0;

-- Migrate defeat data (non-zero rows only).
INSERT INTO season_member_records (season_id, member_id, week_number, trackable_id, recorded_value, logged_by)
SELECT sc.season_id, sc.member_id, sc.week_number, st.id, sc.defeat, sc.imported_by
FROM season_contributions sc
JOIN season_trackables st ON st.season_id = sc.season_id AND st.key = 'defeat'
WHERE sc.defeat != 0;

DROP TABLE season_contributions;

-- +goose StatementEnd
