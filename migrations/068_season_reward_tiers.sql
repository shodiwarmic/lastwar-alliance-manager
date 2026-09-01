-- +goose Up
-- +goose StatementBegin

-- Per-season configurable reward tier list, replacing the four fixed
-- tier_count_* columns on seasons. Mirrors season_trackables (033): a related
-- table keyed on (season_id, key), where `key` is the stable identifier stored
-- in season_rewards.reward_tier and `label` is the display name.
--
-- slot_count, not "count" -- `count` collides with the SQL aggregate name and
-- would need quoting at every use site.
--
-- color is a palette slot validated in Go against validTierColors
-- (handlers_season_hub.go); it maps to a --color-*-bg / --color-* token pair in
-- static/season-hub.css (.tier-badge.tone-*).
CREATE TABLE season_reward_tiers (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    season_id  INTEGER NOT NULL REFERENCES seasons(id) ON DELETE CASCADE,
    key        TEXT    NOT NULL,
    label      TEXT    NOT NULL,
    slot_count INTEGER NOT NULL DEFAULT 0,
    color      TEXT    NOT NULL DEFAULT 'neutral',
    sort_order INTEGER NOT NULL DEFAULT 0,
    UNIQUE(season_id, key)
);
CREATE INDEX idx_srt_season ON season_reward_tiers(season_id);

-- Seed the four standard tiers for every existing season, carrying each
-- season's own slot counts across. Keys are exactly the four strings the old
-- CHECK constraint allowed, so every existing season_rewards.reward_tier value
-- keeps resolving. Colours reproduce the pre-migration badge appearance.
INSERT INTO season_reward_tiers (season_id, key, label, slot_count, color, sort_order)
SELECT id, 'alliance_leader', 'Alliance Leader', tier_count_leader, 'purple', 0 FROM seasons;

INSERT INTO season_reward_tiers (season_id, key, label, slot_count, color, sort_order)
SELECT id, 'core', 'Core', tier_count_core, 'info', 1 FROM seasons;

INSERT INTO season_reward_tiers (season_id, key, label, slot_count, color, sort_order)
SELECT id, 'elite', 'Elite', tier_count_elite, 'success', 2 FROM seasons;

INSERT INTO season_reward_tiers (season_id, key, label, slot_count, color, sort_order)
SELECT id, 'valued', 'Valued', tier_count_valued, 'neutral', 3 FROM seasons;

-- Rebuild season_rewards without the CHECK constraint. SQLite cannot ALTER a
-- CHECK away, so this is the create-copy-drop-rename pattern already used by
-- 030_season_mail_text.sql. Everything else is preserved verbatim, including
-- UNIQUE(season_id, member_id) -- handleRewardSave's upsert depends on it.
--
-- The CHECK was the ONLY enforcement that reward_tier held a valid value.
-- It is replaced by rewardTierExists() in handlers_season_hub.go, called from
-- both handleRewardSave and handleRewardUpdate. Removing one without the other
-- is a net loss of integrity.
CREATE TABLE season_rewards_new (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  season_id         INTEGER NOT NULL REFERENCES seasons(id),
  member_id         INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  reward_tier       TEXT    NOT NULL,
  participation_pct REAL    NOT NULL,
  contribution_pct  REAL,
  note              TEXT    NOT NULL DEFAULT '',
  logged_by         INTEGER NOT NULL REFERENCES users(id),
  logged_at         DATETIME DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(season_id, member_id)
);

-- id is copied explicitly so reward ids survive and the AUTOINCREMENT sequence
-- resumes above the highest existing row.
INSERT INTO season_rewards_new
  (id, season_id, member_id, reward_tier, participation_pct, contribution_pct, note, logged_by, logged_at)
SELECT
   id, season_id, member_id, reward_tier, participation_pct, contribution_pct, note, logged_by, logged_at
FROM season_rewards;

DROP TABLE season_rewards;
ALTER TABLE season_rewards_new RENAME TO season_rewards;

-- +goose StatementEnd

-- Global default tier list, applied when a new season is created. Mirrors
-- settings.season_score_levels_default (035_season_hub_settings.sql).
--
-- Deliberately NOT stored in season_templates.defaults: buildDefaultsEditor's
-- getJSON() (static/settings.js) re-emits a fixed three-key object, so any tier
-- data placed there would be silently destroyed on the next template save.
-- +goose StatementBegin
ALTER TABLE settings ADD COLUMN season_reward_tiers_default TEXT NOT NULL DEFAULT '[{"key":"alliance_leader","label":"Alliance Leader","slot_count":1,"color":"purple"},{"key":"core","label":"Core","slot_count":10,"color":"info"},{"key":"elite","label":"Elite","slot_count":20,"color":"success"},{"key":"valued","label":"Valued","slot_count":69,"color":"neutral"}]';
-- +goose StatementEnd

-- Drop the dead fixed columns. They were stored, editable and round-tripped
-- through create/update, but read by nothing. Leaving them would create a
-- second source of truth that silently drifts from season_reward_tiers.
-- Must come after the seed above, which reads them.
-- +goose StatementBegin
ALTER TABLE seasons DROP COLUMN tier_count_leader;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE seasons DROP COLUMN tier_count_core;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE seasons DROP COLUMN tier_count_elite;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE seasons DROP COLUMN tier_count_valued;
-- +goose StatementEnd
