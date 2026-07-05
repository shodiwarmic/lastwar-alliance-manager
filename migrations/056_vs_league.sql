-- +goose Up
-- VS Duel League tracker. The Alliance Duel League runs on its OWN season numbering
-- (e.g. "S34"), independent of the game's named seasons and the app's Season Hub, so it
-- gets its own tables.
--
-- IMPORTANT: foreign keys are declared for schema-style consistency and future-proofing,
-- but this app opens SQLite with NO `PRAGMA foreign_keys=ON` (see handlers_polls.go), so
-- ON DELETE CASCADE / SET NULL are INERT. Handlers must delete children explicitly and
-- MVP display must LEFT JOIN members + fall back to mvp_name.

CREATE TABLE vs_league_seasons (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    season_number INTEGER NOT NULL,        -- League's OWN number, independent of game seasons
    league_tier   TEXT,                     -- starting/display tier, e.g. "Gold Tier 29-2"
    start_date    TEXT,
    end_date      TEXT,
    final_rank    INTEGER CHECK (final_rank BETWEEN 1 AND 16),
    is_active     INTEGER NOT NULL DEFAULT 0 CHECK (is_active IN (0,1)),
    archived_at   DATETIME,
    notes         TEXT,
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(season_number)                  -- one row per League season; blocks duplicate "S34"
);
-- at most one active season, enforced at the DB level (partial unique index)
CREATE UNIQUE INDEX idx_vs_league_seasons_one_active ON vs_league_seasons(is_active) WHERE is_active = 1;

CREATE TABLE vs_league_weeks (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    season_id             INTEGER NOT NULL,
    week_number           INTEGER,          -- sequential within season (game's "Week 1-4")
    week_date             TEXT NOT NULL,    -- game-time Monday; SAME key as vs_points
    league_tier           TEXT,             -- authoritative per-week tier (can change on promo/demo)
    league_rank           INTEGER CHECK (league_rank BETWEEN 1 AND 16),  -- our bracket rank that week
    opponent_tag          TEXT,
    opponent_name         TEXT,
    opponent_server       INTEGER,
    opponent_lastrank_id  TEXT,             -- 32-hex, if enriched
    opponent_power        INTEGER,          -- snapshot: fightpower (nullable)
    opponent_kills        INTEGER,          -- snapshot: army_kill (nullable)
    opponent_member_count INTEGER,          -- snapshot: cur_member (cap is the gamewide 100)
    opponent_snapshot_at      DATETIME,     -- when the OFFICER captured/saved the snapshot (app time)
    opponent_lastrank_seen_at DATETIME,     -- upstream LastRank last_seen_at, to flag stale pasted data
    our_points            INTEGER CHECK (our_points BETWEEN 0 AND 13),       -- STORED only for summary-only weeks; day-backed weeks compute on read
    opponent_points       INTEGER CHECK (opponent_points BETWEEN 0 AND 13),  -- summary-only fallback; else computed on read
    outcome               TEXT CHECK (outcome IN ('win','loss','tie')),  -- summary-only fallback; NULL = not-yet-entered. 'pending' is computed, never stored at week level
    strategy_label        TEXT CHECK (strategy_label IN ('push','save','normal','test','recovery')),
    strategy_result       TEXT CHECK (strategy_result IN ('worked','failed','mixed')),  -- did the plan achieve its goal?
    notes                 TEXT,
    created_at            TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at            TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (season_id) REFERENCES vs_league_seasons(id) ON DELETE CASCADE,
    UNIQUE(season_id, week_date),
    UNIQUE(season_id, week_number),        -- no duplicate "Week 3" columns (NULLs allowed)
    CHECK (our_points IS NULL OR opponent_points IS NULL OR our_points + opponent_points <= 13)
);
CREATE INDEX idx_vs_league_weeks_season ON vs_league_weeks(season_id, week_date);

CREATE TABLE vs_league_days (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    week_id        INTEGER NOT NULL,
    day_number     INTEGER NOT NULL CHECK (day_number BETWEEN 1 AND 6),  -- Mon-Sat; theme + match points derived from this
    our_score      INTEGER CHECK (our_score >= 0),        -- raw Alliance Duel Score that day (optional context)
    opponent_score INTEGER CHECK (opponent_score >= 0),   -- raw opponent Alliance Duel Score that day (optional)
    outcome        TEXT NOT NULL DEFAULT 'pending' CHECK (outcome IN ('win','loss','tie','pending')),  -- one undecided encoding = 'pending' (never NULL); normalized from raw scores when both entered
    mvp_is_ours    INTEGER NOT NULL DEFAULT 1 CHECK (mvp_is_ours IN (0,1)),  -- 1 = our member, 0 = opponent's
    mvp_member_id  INTEGER,                  -- set ONLY when mvp_is_ours = 1 (FK inert; LEFT JOIN + mvp_name fallback)
    mvp_name       TEXT,                     -- display name (either side)
    created_at     TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (week_id)       REFERENCES vs_league_weeks(id) ON DELETE CASCADE,
    FOREIGN KEY (mvp_member_id) REFERENCES members(id)        ON DELETE SET NULL,
    UNIQUE(week_id, day_number)
);
CREATE INDEX idx_vs_league_days_week ON vs_league_days(week_id, day_number);

-- Weekly bracket MATCH RECORD: every pairing in the 16-team bracket (up to 8), captured
-- ONCE at end of week (not daily). Mirrors the in-game "Match Record" screen. We only hold
-- daily/MVP detail for OUR pairing (is_ours); for every other pairing this score is all we record.
CREATE TABLE vs_league_matchups (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    week_id      INTEGER NOT NULL,
    match_index  INTEGER CHECK (match_index BETWEEN 1 AND 8),  -- row order on the screen
    a_rank INTEGER CHECK (a_rank BETWEEN 1 AND 16), a_server INTEGER, a_tag TEXT, a_name TEXT, a_points INTEGER CHECK (a_points BETWEEN 0 AND 13),  -- left/blue alliance
    b_rank INTEGER CHECK (b_rank BETWEEN 1 AND 16), b_server INTEGER, b_tag TEXT, b_name TEXT, b_points INTEGER CHECK (b_points BETWEEN 0 AND 13),  -- right/red alliance
    is_ours      INTEGER NOT NULL DEFAULT 0 CHECK (is_ours IN (0,1)),  -- our own pairing (rich detail lives on the week/day rows)
    created_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,  -- end-of-week capture time
    FOREIGN KEY (week_id) REFERENCES vs_league_weeks(id) ON DELETE CASCADE,
    UNIQUE(week_id, match_index),            -- max 8 slots/week; blocks dupes from a double-click/retry
    CHECK (a_points IS NULL OR b_points IS NULL OR a_points + b_points <= 13)
);
CREATE INDEX idx_vs_league_matchups_week ON vs_league_matchups(week_id, match_index);
-- at most one pairing per week may be flagged as ours (partial unique index)
CREATE UNIQUE INDEX idx_vs_league_matchups_one_ours ON vs_league_matchups(week_id) WHERE is_ours = 1;

-- +goose Down
DROP INDEX IF EXISTS idx_vs_league_matchups_one_ours;
DROP INDEX IF EXISTS idx_vs_league_matchups_week;
DROP TABLE IF EXISTS vs_league_matchups;
DROP INDEX IF EXISTS idx_vs_league_days_week;
DROP TABLE IF EXISTS vs_league_days;
DROP INDEX IF EXISTS idx_vs_league_weeks_season;
DROP TABLE IF EXISTS vs_league_weeks;
DROP INDEX IF EXISTS idx_vs_league_seasons_one_active;
DROP TABLE IF EXISTS vs_league_seasons;
