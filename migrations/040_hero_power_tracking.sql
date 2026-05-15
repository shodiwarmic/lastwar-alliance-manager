-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS hero_power_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    member_id INTEGER NOT NULL,
    power INTEGER NOT NULL,
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (member_id) REFERENCES members(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_hero_power_history_member ON hero_power_history(member_id, recorded_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_hero_power_history_member;
DROP TABLE IF EXISTS hero_power_history;
-- +goose StatementEnd
