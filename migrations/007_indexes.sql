-- +goose Up
-- Indexes for frequently queried columns identified during code review.

-- Login session lookups (login history page, failure counts)
CREATE INDEX IF NOT EXISTS idx_login_sessions_user_id ON login_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_login_sessions_user_success ON login_sessions(user_id, success);

-- Password history checks on every password change
CREATE INDEX IF NOT EXISTS idx_password_history_user_created ON password_history(user_id, created_at DESC);

-- Latest power / squad power lookups (getMembers correlated subqueries)
CREATE INDEX IF NOT EXISTS idx_power_history_member_recorded ON power_history(member_id, recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_squad_power_history_member_recorded ON squad_power_history(member_id, recorded_at DESC);

-- Alias resolution (alias engine lookups by alias string and by member)
CREATE INDEX IF NOT EXISTS idx_member_aliases_alias ON member_aliases(alias);
CREATE INDEX IF NOT EXISTS idx_member_aliases_member_id ON member_aliases(member_id);

-- VS points weekly lookups
CREATE INDEX IF NOT EXISTS idx_vs_points_member_week ON vs_points(member_id, week_date);

-- +goose Down
DROP INDEX IF EXISTS idx_login_sessions_user_id;
DROP INDEX IF EXISTS idx_login_sessions_user_success;
DROP INDEX IF EXISTS idx_password_history_user_created;
DROP INDEX IF EXISTS idx_power_history_member_recorded;
DROP INDEX IF EXISTS idx_squad_power_history_member_recorded;
DROP INDEX IF EXISTS idx_member_aliases_alias;
DROP INDEX IF EXISTS idx_member_aliases_member_id;
DROP INDEX IF EXISTS idx_vs_points_member_week;
