-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS storm_tf_config (
    task_force  TEXT PRIMARY KEY CHECK(task_force IN ('A','B')),
    time_slot   INTEGER CHECK(time_slot IN (1,2,3))
);
INSERT OR IGNORE INTO storm_tf_config (task_force, time_slot) VALUES ('A', NULL);
INSERT OR IGNORE INTO storm_tf_config (task_force, time_slot) VALUES ('B', NULL);

CREATE TABLE IF NOT EXISTS storm_registrations (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    member_id   INTEGER NOT NULL UNIQUE REFERENCES members(id) ON DELETE CASCADE,
    slot_1      INTEGER NOT NULL DEFAULT 0 CHECK(slot_1 IN (0,1,2)),
    slot_2      INTEGER NOT NULL DEFAULT 0 CHECK(slot_2 IN (0,1,2)),
    slot_3      INTEGER NOT NULL DEFAULT 0 CHECK(slot_3 IN (0,1,2)),
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS storm_groups (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    task_force   TEXT NOT NULL CHECK(task_force IN ('A','B')),
    name         TEXT NOT NULL,
    instructions TEXT,
    sort_order   INTEGER NOT NULL DEFAULT 0,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS storm_group_buildings (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id     INTEGER NOT NULL REFERENCES storm_groups(id) ON DELETE CASCADE,
    building_id  TEXT NOT NULL,
    sort_order   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS storm_group_building_members (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    group_building_id INTEGER NOT NULL REFERENCES storm_group_buildings(id) ON DELETE CASCADE,
    member_id         INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    is_sub            INTEGER NOT NULL DEFAULT 0,
    position          INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS storm_group_members (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id   INTEGER NOT NULL REFERENCES storm_groups(id) ON DELETE CASCADE,
    member_id  INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    is_sub     INTEGER NOT NULL DEFAULT 0,
    position   INTEGER NOT NULL DEFAULT 0
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS storm_group_members;
DROP TABLE IF EXISTS storm_group_building_members;
DROP TABLE IF EXISTS storm_group_buildings;
DROP TABLE IF EXISTS storm_groups;
DROP TABLE IF EXISTS storm_registrations;
DROP TABLE IF EXISTS storm_tf_config;
-- +goose StatementEnd
