-- +goose Up

-- A war keeps the figures it was judged on (Project 21, private-docs 324; owner, 2026-10-30).
-- The matchup on a war was recomputed from each side's latest observations on every read, so a
-- war already fought drifted with figures taken after it. One row per war: both sides' figures,
-- each with its instant and source, and the matchup they gave, taken when the war is declared
-- (ours) or confirmed (theirs), re-taken by an officer until it is completed, and taken from the
-- figures at its battle time if it is completed without one. The model is vs_league_weeks'
-- frozen opponent and own figures (056).
CREATE TABLE IF NOT EXISTS season_war_snapshots (
    war_id                      INTEGER PRIMARY KEY REFERENCES season_wars(id),
    season_id                   INTEGER NOT NULL REFERENCES seasons(id),
    opponent_season_alliance_id INTEGER REFERENCES season_alliances(id),
    taken_trigger               TEXT    NOT NULL CHECK(taken_trigger IN ('declared','confirmed','retake','completed')),
    taken_at                    TEXT    NOT NULL,   -- UTC, SQLite layout
    as_of                       TEXT    NOT NULL,   -- UTC: the instant the figures are as of
    taken_by                    INTEGER REFERENCES users(id),
    ours                        TEXT    NOT NULL,   -- JSON warSide: each figure's value, instant and source
    theirs                      TEXT    NOT NULL,   -- JSON warSide
    matchup                     TEXT    NOT NULL,   -- JSON matchupResult: categories, weights, score, rating
    score                       REAL,
    rating                      TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_season_war_snapshots_season ON season_war_snapshots(season_id);

-- +goose Down
DROP TABLE IF EXISTS season_war_snapshots;
