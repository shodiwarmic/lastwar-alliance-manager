-- +goose Up

-- The Reports page (Project 19): the VS daily minimum per week, and one permission
-- key per report.
--
-- vs_week_minimums records the VS daily minimum that applied to each week, so a
-- historical report judges a week against its own minimum rather than today's.
-- Two kinds of row, at most one of each per week:
--   setting  — the value of settings.vs_minimum_points as of that week. The app
--              writes it when a Settings save changes the minimum (the row for the
--              CURRENT game week, replaced on a second change that week), and this
--              migration seeds the week it runs in.
--   override — an officer's value for one week, set in the VS League week modal.
--              It follows its week: moved on a date correction, deleted with it.
-- Resolution for week W (vsMinimumForWeek, reports_vs.go): the override for W; else
-- the setting row with the greatest week_date <= W; else today's setting, which the
-- reports label "no record for this week: judged against today's setting".
--
-- Every week has a row-or-fallback, league week or not (most VS weeks are in no
-- league season), which is why this is not a column on vs_league_weeks.
-- foreign_keys is off app-wide: set_by's ON DELETE is documentation, and a user
-- delete nulls it explicitly (clearUserReferences).
-- +goose StatementBegin
CREATE TABLE vs_week_minimums (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    week_date      TEXT NOT NULL,                      -- the VS week's Monday (game time)
    kind           TEXT NOT NULL CHECK(kind IN ('setting','override')),
    minimum_points INTEGER NOT NULL CHECK(minimum_points > 0),
    set_by         INTEGER REFERENCES users(id) ON DELETE SET NULL,
    set_at         DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(week_date, kind)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_vs_week_minimums_week ON vs_week_minimums(week_date);
-- +goose StatementEnd

-- Seed the setting row for the game week this runs in (game time is UTC-2; minus
-- six days then forward to the next Monday maps any day to its week's Monday, a
-- Sunday to the Monday six days earlier). Only on an install with VS history: a
-- fresh install gets its first row from its first Settings save, rather than
-- recording the column default as if an officer had chosen it.
-- +goose StatementBegin
INSERT INTO vs_week_minimums (week_date, kind, minimum_points)
SELECT date(date('now', '-2 hours'), '-6 days', 'weekday 1'), 'setting', s.vs_minimum_points
FROM settings s
WHERE s.id = 1
  AND s.vs_minimum_points > 0
  AND EXISTS (SELECT 1 FROM vs_points)
  AND NOT EXISTS (SELECT 1 FROM vs_week_minimums
                  WHERE kind = 'setting'
                    AND week_date = date(date('now', '-2 hours'), '-6 days', 'weekday 1'));
-- +goose StatementEnd

-- One permission key per report, seeded true for every rank that can see the
-- Accountability page today (the weekly summary lived there). json_set only where
-- the key is absent, as 080 does, so an operator's later choice is never overwritten.
-- +goose StatementBegin
UPDATE rank_permissions
SET permissions = json_set(permissions, '$.view_report_weekly', json('true'))
WHERE json_extract(permissions, '$.view_accountability') = 1
  AND json_type(permissions, '$.view_report_weekly') IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE rank_permissions
SET permissions = json_set(permissions, '$.view_report_vs_misses', json('true'))
WHERE json_extract(permissions, '$.view_accountability') = 1
  AND json_type(permissions, '$.view_report_vs_misses') IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE rank_permissions
SET permissions = json_set(permissions, '$.view_report_participation', json('true'))
WHERE json_extract(permissions, '$.view_accountability') = 1
  AND json_type(permissions, '$.view_report_participation') IS NULL;
-- +goose StatementEnd

-- +goose Down
-- The three permission keys stay: the previous image ignores them.
-- +goose StatementBegin
DROP TABLE IF EXISTS vs_week_minimums;
-- +goose StatementEnd
