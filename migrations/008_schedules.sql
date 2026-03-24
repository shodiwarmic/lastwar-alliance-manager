-- +goose Up
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_schedule INTEGER NOT NULL DEFAULT 0;
ALTER TABLE rank_permissions ADD COLUMN manage_schedule INTEGER NOT NULL DEFAULT 0;

-- All ranks can view; R4+ can manage
UPDATE rank_permissions SET view_schedule = 1 WHERE rank IN ('R1', 'R2', 'R3', 'R4', 'R5');
UPDATE rank_permissions SET manage_schedule = 1 WHERE rank IN ('R4', 'R5');

CREATE TABLE IF NOT EXISTS schedules (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL,
    duration_days INTEGER NOT NULL DEFAULT 7,
    is_active     INTEGER NOT NULL DEFAULT 0,
    schedule_data TEXT NOT NULL DEFAULT '[]',
    created_by    INTEGER NOT NULL REFERENCES users(id),
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS schedules;
-- +goose StatementEnd
