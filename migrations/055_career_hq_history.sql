-- +goose Up
-- Career type / profession level / HQ level tracking, all history-only: neither
-- carries a point-in-time column on members. "Current" is the latest history row,
-- same as power_history / hero_power_history / kill_history. HQ level's old
-- members.level column is seeded into hq_level_history and then dropped.

-- HQ level history. Seeded from members.level below; that column is then dropped.
CREATE TABLE hq_level_history (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    member_id   INTEGER NOT NULL,
    hq_level    INTEGER NOT NULL,
    source      TEXT NOT NULL DEFAULT 'manual',
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (member_id) REFERENCES members(id) ON DELETE CASCADE
);
CREATE INDEX idx_hq_level_history_member ON hq_level_history(member_id, recorded_at DESC);

-- Profession level history (career_lv from LastRank / manual entry).
CREATE TABLE profession_level_history (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    member_id        INTEGER NOT NULL,
    profession_level INTEGER NOT NULL,
    source           TEXT NOT NULL DEFAULT 'manual',
    recorded_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (member_id) REFERENCES members(id) ON DELETE CASCADE
);
CREATE INDEX idx_profession_level_history_member ON profession_level_history(member_id, recorded_at DESC);

-- Seed HQ history from the existing point-in-time column so the roster's derived
-- "current HQ" is unchanged after the column goes away. Skip 0/NULL (no real data).
INSERT INTO hq_level_history (member_id, hq_level, source, recorded_at)
SELECT id, level, 'manual', CURRENT_TIMESTAMP
FROM members
WHERE level IS NOT NULL AND level > 0;

-- Drop the now-redundant column. HQ level is history-only from here on.
ALTER TABLE members DROP COLUMN level;

-- +goose Down
-- Restore the point-in-time column and backfill it from the latest history row.
ALTER TABLE members ADD COLUMN level INTEGER DEFAULT 0;
UPDATE members SET level = COALESCE(
    (SELECT hq_level FROM hq_level_history WHERE member_id = members.id ORDER BY recorded_at DESC LIMIT 1), 0);
DROP INDEX IF EXISTS idx_profession_level_history_member;
DROP TABLE IF EXISTS profession_level_history;
DROP INDEX IF EXISTS idx_hq_level_history_member;
DROP TABLE IF EXISTS hq_level_history;
