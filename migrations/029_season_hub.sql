-- +goose Up
-- +goose StatementBegin

CREATE TABLE seasons (
  id                   INTEGER PRIMARY KEY AUTOINCREMENT,
  name                 TEXT    NOT NULL,
  season_number        INTEGER NOT NULL,
  start_date           TEXT    NOT NULL,
  end_date             TEXT,
  week_count           INTEGER NOT NULL DEFAULT 8,
  key_event_name       TEXT    NOT NULL DEFAULT 'Rare Soil War',
  key_event_required   INTEGER NOT NULL DEFAULT 4,
  -- Participation tier thresholds (percentage, inclusive lower bound)
  tier_active_min_pct  INTEGER NOT NULL DEFAULT 70,
  tier_at_risk_min_pct INTEGER NOT NULL DEFAULT 60,
  -- Reward tier slot counts
  tier_count_leader    INTEGER NOT NULL DEFAULT 1,
  tier_count_core      INTEGER NOT NULL DEFAULT 10,
  tier_count_elite     INTEGER NOT NULL DEFAULT 20,
  tier_count_valued    INTEGER NOT NULL DEFAULT 69,
  is_active            INTEGER NOT NULL DEFAULT 0,
  archived_at          DATETIME,
  created_at           DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Configurable score levels per season.
-- 'key' is a stable internal identifier stored in season_participation.score.
-- 'label' is the display name shown in the UI (e.g. 'FULL', 'Active', 'Present').
-- 'points' is the point value awarded for this level.
-- Preloaded with Season II defaults by handleSeasonCreate.
CREATE TABLE season_score_levels (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  season_id  INTEGER NOT NULL REFERENCES seasons(id) ON DELETE CASCADE,
  key        TEXT    NOT NULL,
  label      TEXT    NOT NULL,
  points     INTEGER NOT NULL DEFAULT 0,
  sort_order INTEGER NOT NULL DEFAULT 0,
  UNIQUE(season_id, key)
);
CREATE INDEX idx_ssl_season ON season_score_levels(season_id);

-- Weekly participation entries (one row per member per week per season).
-- score must match a key in season_score_levels for this season; enforced at app layer.
CREATE TABLE season_participation (
  id                 INTEGER PRIMARY KEY AUTOINCREMENT,
  season_id          INTEGER NOT NULL REFERENCES seasons(id),
  member_id          INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  week_number        INTEGER NOT NULL,
  score              TEXT    NOT NULL DEFAULT 'absent',
  attended_key_event INTEGER NOT NULL DEFAULT 0,
  note               TEXT    NOT NULL DEFAULT '',
  recorded_by        INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at         DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at         DATETIME DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(season_id, member_id, week_number)
);
CREATE INDEX idx_sp_season ON season_participation(season_id);
CREATE INDEX idx_sp_member ON season_participation(member_id);

-- Contribution scores per member per season.
-- week_number=0 is the season-end snapshot — the canonical source for the tie-breaker ranking.
-- week_number=1..N are optional weekly imports for tracking purposes only.
CREATE TABLE season_contributions (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  season_id         INTEGER NOT NULL REFERENCES seasons(id),
  member_id         INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  week_number       INTEGER NOT NULL DEFAULT 0,
  mutual_assistance INTEGER NOT NULL DEFAULT 0,
  siege             INTEGER NOT NULL DEFAULT 0,
  rare_soil_war     INTEGER NOT NULL DEFAULT 0,
  defeat            INTEGER NOT NULL DEFAULT 0,
  imported_by       INTEGER REFERENCES users(id) ON DELETE SET NULL,
  imported_at       DATETIME DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(season_id, member_id, week_number)
);
CREATE INDEX idx_sc_season ON season_contributions(season_id);

-- Reward assignments (one row per member per season, upsertable).
CREATE TABLE season_rewards (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  season_id         INTEGER NOT NULL REFERENCES seasons(id),
  member_id         INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  reward_tier       TEXT    NOT NULL CHECK(reward_tier IN ('alliance_leader','core','elite','valued')),
  participation_pct REAL    NOT NULL,
  contribution_pct  REAL,
  note              TEXT    NOT NULL DEFAULT '',
  logged_by         INTEGER NOT NULL REFERENCES users(id),
  logged_at         DATETIME DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(season_id, member_id)
);

-- Season-specific mail/document uploads (isolated from global /files).
CREATE TABLE season_mail (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  season_id   INTEGER NOT NULL REFERENCES seasons(id),
  title       TEXT    NOT NULL,
  file_name   TEXT    NOT NULL,
  file_type   TEXT    NOT NULL DEFAULT 'document',
  uploaded_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
  uploaded_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Permission columns
ALTER TABLE rank_permissions ADD COLUMN view_season_hub      INTEGER NOT NULL DEFAULT 0;
ALTER TABLE rank_permissions ADD COLUMN manage_season_hub    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE rank_permissions ADD COLUMN manage_season_rewards INTEGER NOT NULL DEFAULT 0;

UPDATE rank_permissions SET view_season_hub = 1 WHERE rank IN ('R1','R2','R3','R4','R5');
UPDATE rank_permissions SET manage_season_hub = 1 WHERE rank IN ('R4','R5');
UPDATE rank_permissions SET manage_season_rewards = 1 WHERE rank IN ('R5');

-- +goose StatementEnd
