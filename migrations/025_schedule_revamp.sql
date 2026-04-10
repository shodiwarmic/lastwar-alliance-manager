-- +goose Up
-- +goose StatementBegin

-- Drop old template-based schedule (no calendar dates; nothing to migrate)
DROP TABLE IF EXISTS schedules;

-- Event type registry
CREATE TABLE schedule_event_types (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL UNIQUE,       -- "Marshal's Guard"
    short_name TEXT NOT NULL UNIQUE,       -- "MG"
    icon       TEXT NOT NULL DEFAULT '📅',
    is_system  INTEGER NOT NULL DEFAULT 0, -- 1 = cannot be deleted or renamed
    active     INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO schedule_event_types (name, short_name, icon, is_system, sort_order)
    VALUES ('Marshal''s Guard', 'MG', '🛡️', 1, 1);
INSERT INTO schedule_event_types (name, short_name, icon, is_system, sort_order)
    VALUES ('Zombie Siege', 'ZS', '🧟', 1, 2);

-- Calendar events
CREATE TABLE schedule_events (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    event_date     TEXT NOT NULL,  -- "YYYY-MM-DD"
    event_type_id  INTEGER NOT NULL REFERENCES schedule_event_types(id) ON DELETE RESTRICT,
    event_time     TEXT NOT NULL,  -- "HH:MM"
    level          INTEGER,        -- always set for system types (MG/ZS) at creation; null for custom event types
    notes          TEXT,
    created_by     INTEGER NOT NULL REFERENCES users(id),
    created_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_schedule_events_date ON schedule_events(event_date);

-- Repeating server-wide events
CREATE TABLE server_events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL,
    short_name      TEXT NOT NULL DEFAULT '',
    icon            TEXT NOT NULL DEFAULT '🌐',
    duration_days   INTEGER NOT NULL DEFAULT 1,
    repeat_type     TEXT NOT NULL DEFAULT 'none',  -- 'none'|'weekly'|'biweekly'|'every_n_days'
    repeat_interval INTEGER,   -- N for every_n_days
    repeat_weekday  INTEGER,   -- 0=Mon…6=Sun for weekly/biweekly
    anchor_date     TEXT,      -- "YYYY-MM-DD"
    active          INTEGER NOT NULL DEFAULT 1,
    sort_order      INTEGER NOT NULL DEFAULT 0,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO server_events (name, short_name, icon, duration_days, repeat_type, repeat_interval, sort_order)
    VALUES ('Ironclad Vehicle', 'IC', '🚙', 2, 'every_n_days', 14, 1);
INSERT INTO server_events (name, short_name, icon, duration_days, repeat_type, repeat_interval, sort_order)
    VALUES ('Zombie Invasion', 'ZI', '☣️', 3, 'every_n_days', 14, 2);
INSERT INTO server_events (name, short_name, icon, duration_days, repeat_type, repeat_interval, sort_order)
    VALUES ('Rampage Bosses', 'RB', '👹', 1, 'every_n_days', 14, 3);
INSERT INTO server_events (name, short_name, icon, duration_days, repeat_type, repeat_interval, sort_order)
    VALUES ('General''s Trial', 'GT', '⚔️', 3, 'every_n_days', 14, 4);
INSERT INTO server_events (name, short_name, icon, duration_days, repeat_type, repeat_interval, sort_order)
    VALUES ('Doomsday', 'DD', '🌋', 1, 'every_n_days', 14, 5);

-- Storm availability slot times (game-global, admin-editable via Advanced Settings)
CREATE TABLE storm_slot_times (
    slot    INTEGER PRIMARY KEY CHECK(slot IN (1,2,3)),
    label   TEXT NOT NULL DEFAULT '',
    time_st TEXT NOT NULL DEFAULT '00:00'
);
INSERT INTO storm_slot_times (slot, label, time_st) VALUES (1, 'Slot 1', '00:00');
INSERT INTO storm_slot_times (slot, label, time_st) VALUES (2, 'Slot 2', '00:00');
INSERT INTO storm_slot_times (slot, label, time_st) VALUES (3, 'Slot 3', '00:00');

-- Schedule defaults and season tracking
ALTER TABLE settings ADD COLUMN mg_baseline INTEGER NOT NULL DEFAULT 11;
ALTER TABLE settings ADD COLUMN zs_baseline INTEGER NOT NULL DEFAULT 7;
ALTER TABLE settings ADD COLUMN mg_default_time TEXT NOT NULL DEFAULT '00:30';
ALTER TABLE settings ADD COLUMN zs_default_time TEXT NOT NULL DEFAULT '23:00';
ALTER TABLE settings ADD COLUMN current_season INTEGER;
ALTER TABLE settings ADD COLUMN season_start_date TEXT;

-- Event generation rules
ALTER TABLE settings ADD COLUMN mg_anchor_date TEXT;
ALTER TABLE settings ADD COLUMN zs_schedule_mode TEXT NOT NULL DEFAULT 'weekdays';
ALTER TABLE settings ADD COLUMN zs_weekdays TEXT NOT NULL DEFAULT '1,4';
ALTER TABLE settings ADD COLUMN zs_anchor_date TEXT;
ALTER TABLE settings ADD COLUMN zs_anchor_time TEXT NOT NULL DEFAULT '23:00';

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
-- (purposely left blank)
-- +goose StatementEnd
