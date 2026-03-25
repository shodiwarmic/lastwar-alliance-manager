-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS oc_categories (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT    NOT NULL,
    display_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS oc_responsibilities (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    category_id   INTEGER NOT NULL REFERENCES oc_categories(id) ON DELETE CASCADE,
    name          TEXT    NOT NULL,
    description   TEXT    NOT NULL DEFAULT '',
    frequency     TEXT    NOT NULL DEFAULT 'Weekly',
    display_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS oc_assignees (
    responsibility_id INTEGER NOT NULL REFERENCES oc_responsibilities(id) ON DELETE CASCADE,
    member_id         INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    PRIMARY KEY (responsibility_id, member_id)
);

ALTER TABLE rank_permissions ADD COLUMN view_officer_command   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE rank_permissions ADD COLUMN manage_officer_command INTEGER NOT NULL DEFAULT 0;

UPDATE rank_permissions SET view_officer_command   = 1 WHERE rank IN ('R1', 'R2', 'R3', 'R4', 'R5');
UPDATE rank_permissions SET manage_officer_command = 1 WHERE rank IN ('R4', 'R5');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS oc_assignees;
DROP TABLE IF EXISTS oc_responsibilities;
DROP TABLE IF EXISTS oc_categories;
-- +goose StatementEnd
