-- +goose Up
-- +goose StatementBegin
CREATE TABLE member_skills (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    member_id   INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    skill_key   TEXT    NOT NULL,
    recorded_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    recorded_at TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(member_id, skill_key)
);
CREATE INDEX idx_member_skills_skill_key ON member_skills(skill_key);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS member_skills;
-- +goose StatementEnd
