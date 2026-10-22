-- +goose Up
-- 085 is reserved by the separate Desert Storm attendance PR.
-- This deliberately does not resurrect migration 012's obsolete train_schedules.
-- +goose StatementBegin
CREATE TABLE train_schedule_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    date TEXT NOT NULL,
    train_type TEXT NOT NULL CHECK(train_type IN ('FREE','PURCHASED')),
    conductor_id INTEGER REFERENCES members(id) ON DELETE SET NULL,
    conductor_name TEXT NOT NULL,
    vip_id INTEGER REFERENCES members(id) ON DELETE SET NULL,
    vip_name TEXT NOT NULL DEFAULT '',
    vip_type TEXT CHECK(vip_type IN ('SPECIAL_GUEST','GUARDIAN_DEFENDER')),
    notes TEXT NOT NULL DEFAULT '',
    completed_log_id INTEGER UNIQUE REFERENCES train_logs(id) ON DELETE SET NULL,
    cancelled_at DATETIME,
    cancelled_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK(cancelled_at IS NULL OR completed_log_id IS NULL)
);
CREATE INDEX idx_train_schedule_entries_date ON train_schedule_entries(date, id);
CREATE INDEX idx_train_schedule_entries_conductor ON train_schedule_entries(conductor_id);
CREATE INDEX idx_train_schedule_entries_vip ON train_schedule_entries(vip_id);
-- New permissions default to leadership only; preserve any explicitly set values.
UPDATE rank_permissions SET permissions = json_set(permissions, '$.view_train_schedule',
    json(CASE WHEN rank IN ('R4','R5') THEN 'true' ELSE 'false' END))
WHERE json_type(permissions, '$.view_train_schedule') IS NULL;
UPDATE rank_permissions SET permissions = json_set(permissions, '$.manage_train_schedule',
    json(CASE WHEN rank IN ('R4','R5') THEN 'true' ELSE 'false' END))
WHERE json_type(permissions, '$.manage_train_schedule') IS NULL;
-- +goose StatementEnd

-- +goose Down
-- Removes plans only; completed train logs remain ordinary historic records.
-- +goose StatementBegin
DROP TABLE train_schedule_entries;
UPDATE rank_permissions SET permissions = json_remove(permissions, '$.view_train_schedule', '$.manage_train_schedule');
-- +goose StatementEnd
