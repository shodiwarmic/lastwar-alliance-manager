-- +goose Up
-- +goose StatementBegin

-- Remove scoring/ranking settings that have no consuming logic since
-- the old awards/rankings system was removed.
ALTER TABLE settings DROP COLUMN award_first_points;
ALTER TABLE settings DROP COLUMN award_second_points;
ALTER TABLE settings DROP COLUMN award_third_points;
ALTER TABLE settings DROP COLUMN recommendation_points;
ALTER TABLE settings DROP COLUMN recent_conductor_penalty_days;
ALTER TABLE settings DROP COLUMN above_average_conductor_penalty;
ALTER TABLE settings DROP COLUMN r4r5_rank_boost;
ALTER TABLE settings DROP COLUMN first_time_conductor_boost;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Intentionally left blank
-- +goose StatementEnd
