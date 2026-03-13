-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    rank TEXT NOT NULL,
    eligible BOOLEAN NOT NULL DEFAULT 1,
    level INTEGER DEFAULT 0,
    squad_type TEXT DEFAULT '',
    troop_level INTEGER DEFAULT 0,
    profession TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS squad_power_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    member_id INTEGER NOT NULL,
    power INTEGER NOT NULL,
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (member_id) REFERENCES members(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_squad_power_history_member ON squad_power_history(member_id, recorded_at DESC);

CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    password TEXT NOT NULL,
    member_id INTEGER,
    is_admin BOOLEAN DEFAULT 0,
    force_password_change BOOLEAN NOT NULL DEFAULT 1,
    password_changed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (member_id) REFERENCES members(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS train_schedules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    date TEXT NOT NULL UNIQUE,
    conductor_id INTEGER NOT NULL,
    backup_id INTEGER,
    conductor_score INTEGER,
    conductor_showed_up BOOLEAN,
    notes TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (conductor_id) REFERENCES members(id) ON DELETE CASCADE,
    FOREIGN KEY (backup_id) REFERENCES members(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS award_types (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    active BOOLEAN DEFAULT 1,
    sort_order INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS awards (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    week_date TEXT NOT NULL,
    award_type TEXT NOT NULL,
    rank INTEGER NOT NULL,
    member_id INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expired BOOLEAN DEFAULT 0,
    FOREIGN KEY (member_id) REFERENCES members(id) ON DELETE CASCADE,
    UNIQUE(week_date, award_type, rank)
);

CREATE TABLE IF NOT EXISTS power_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    member_id INTEGER NOT NULL,
    power INTEGER NOT NULL,
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (member_id) REFERENCES members(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_power_history_member ON power_history(member_id, recorded_at DESC);

CREATE TABLE IF NOT EXISTS recommendations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    member_id INTEGER NOT NULL,
    recommended_by_id INTEGER NOT NULL,
    notes TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expired BOOLEAN DEFAULT 0,
    FOREIGN KEY (member_id) REFERENCES members(id) ON DELETE CASCADE,
    FOREIGN KEY (recommended_by_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS dyno_recommendations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    member_id INTEGER NOT NULL,
    points INTEGER NOT NULL,
    notes TEXT NOT NULL,
    created_by_id INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (member_id) REFERENCES members(id) ON DELETE CASCADE,
    FOREIGN KEY (created_by_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    award_first_points INTEGER NOT NULL DEFAULT 3,
    award_second_points INTEGER NOT NULL DEFAULT 2,
    award_third_points INTEGER NOT NULL DEFAULT 1,
    recommendation_points INTEGER NOT NULL DEFAULT 10,
    recent_conductor_penalty_days INTEGER NOT NULL DEFAULT 30,
    above_average_conductor_penalty INTEGER NOT NULL DEFAULT 10,
    r4r5_rank_boost INTEGER NOT NULL DEFAULT 5,
    first_time_conductor_boost INTEGER NOT NULL DEFAULT 5,
    schedule_message_template TEXT NOT NULL DEFAULT 'Train Schedule - Week {WEEK}\n\n{SCHEDULES}\n\nNext in line:\n{NEXT_3}',
    daily_message_template TEXT,
    power_tracking_enabled BOOLEAN DEFAULT 0,
    storm_timezones TEXT DEFAULT 'America/New_York,Europe/London',
    storm_respect_dst BOOLEAN DEFAULT 1,
    login_message TEXT,
    max_hq_level INTEGER NOT NULL DEFAULT 35,
    squad_tracking_enabled BOOLEAN DEFAULT 0,
    pwd_min_length INTEGER NOT NULL DEFAULT 12,
    pwd_require_special BOOLEAN NOT NULL DEFAULT 0,
    pwd_require_upper BOOLEAN NOT NULL DEFAULT 0,
    pwd_require_lower BOOLEAN NOT NULL DEFAULT 0,
    pwd_require_number BOOLEAN NOT NULL DEFAULT 0,
    pwd_history_count INTEGER NOT NULL DEFAULT 4,
    pwd_validity_days INTEGER NOT NULL DEFAULT 180
);

CREATE TABLE IF NOT EXISTS storm_assignments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_force TEXT NOT NULL CHECK (task_force IN ('A', 'B')),
    building_id TEXT NOT NULL,
    member_id INTEGER NOT NULL,
    position INTEGER NOT NULL CHECK (position BETWEEN 1 AND 4),
    FOREIGN KEY (member_id) REFERENCES members(id) ON DELETE CASCADE,
    UNIQUE(task_force, building_id, position)
);

CREATE TABLE IF NOT EXISTS files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    file_name TEXT NOT NULL,
    file_type TEXT NOT NULL,
    min_rank TEXT NOT NULL DEFAULT 'R1',
    min_edit_rank TEXT NOT NULL DEFAULT 'R4',
    owner_user_id INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS login_sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    username TEXT NOT NULL,
    ip_address TEXT,
    user_agent TEXT,
    country TEXT,
    city TEXT,
    isp TEXT,
    login_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    success BOOLEAN DEFAULT 1,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_login_sessions_user ON login_sessions(user_id, login_time DESC);

CREATE TABLE IF NOT EXISTS vs_points (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    member_id INTEGER NOT NULL,
    week_date TEXT NOT NULL,
    monday INTEGER NOT NULL DEFAULT 0,
    tuesday INTEGER NOT NULL DEFAULT 0,
    wednesday INTEGER NOT NULL DEFAULT 0,
    thursday INTEGER NOT NULL DEFAULT 0,
    friday INTEGER NOT NULL DEFAULT 0,
    saturday INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (member_id) REFERENCES members(id) ON DELETE CASCADE,
    UNIQUE(member_id, week_date)
);
CREATE INDEX IF NOT EXISTS idx_vs_points_week ON vs_points(week_date);

CREATE TABLE IF NOT EXISTS password_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS rank_permissions (
    rank TEXT PRIMARY KEY,
    view_train BOOLEAN DEFAULT 0, manage_train BOOLEAN DEFAULT 0,
    view_awards BOOLEAN DEFAULT 0, manage_awards BOOLEAN DEFAULT 0,
    view_recs BOOLEAN DEFAULT 0, manage_recs BOOLEAN DEFAULT 0,
    view_dyno BOOLEAN DEFAULT 0, manage_dyno BOOLEAN DEFAULT 0,
    view_rankings BOOLEAN DEFAULT 0,
    view_storm BOOLEAN DEFAULT 0, manage_storm BOOLEAN DEFAULT 0,
    view_vs_points BOOLEAN DEFAULT 0, manage_vs_points BOOLEAN DEFAULT 0,
    view_upload BOOLEAN DEFAULT 0,
    manage_members BOOLEAN DEFAULT 0,
    manage_settings BOOLEAN DEFAULT 0,
    view_files BOOLEAN DEFAULT 0,
    upload_files BOOLEAN DEFAULT 0,
    manage_files BOOLEAN DEFAULT 0
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- (Down migrations purposely left blank to prevent accidental drops in production)
-- +goose StatementEnd