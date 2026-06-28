-- +goose Up
-- +goose StatementBegin
ALTER TABLE members ADD COLUMN joined_at TEXT;
-- +goose StatementEnd

-- +goose StatementBegin
-- Best-effort backfill: earliest record we have for each member (a lower bound on
-- tenure), falling back to ~1 week ago for members with no history at all. New rows
-- after this migration get joined_at from the genuine-join creation paths (or NULL
-- for bulk/sync imports). joined_at is always a 10-char YYYY-MM-DD (date-only).
UPDATE members SET joined_at = COALESCE(
  (SELECT MIN(d) FROM (
     SELECT MIN(week_date)            AS d FROM vs_points          WHERE member_id = members.id
     UNION ALL SELECT MIN(date(recorded_at)) FROM power_history       WHERE member_id = members.id
     UNION ALL SELECT MIN(date(recorded_at)) FROM hero_power_history  WHERE member_id = members.id
     UNION ALL SELECT MIN(date(recorded_at)) FROM kill_history        WHERE member_id = members.id
     UNION ALL SELECT MIN(date(recorded_at)) FROM squad_power_history WHERE member_id = members.id
  )),
  date('now','-7 days')
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Intentionally left blank (can't un-backfill)
-- +goose StatementEnd
