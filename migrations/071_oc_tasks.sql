-- +goose Up

-- An optional, ordered list of steps on a responsibility. Descriptive text, not
-- entities: no completion state, no due date, no assignee of its own. The whole
-- list is replaced on save, so display_order is just line order and a task has
-- no identity a client ever needs to name.
--
-- The REFERENCES clause below is documentation, not behaviour. foreign_keys is
-- off app-wide (see handlers_admin.go and handlers_season_hub.go), so no
-- ON DELETE CASCADE in this schema has ever fired -- which is why the delete
-- handlers in handlers_officer_command.go now remove children explicitly, and
-- why the sweep below is needed at all.

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS oc_tasks (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    responsibility_id INTEGER NOT NULL REFERENCES oc_responsibilities(id) ON DELETE CASCADE,
    text              TEXT    NOT NULL,
    display_order     INTEGER NOT NULL DEFAULT 0
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_oc_tasks_resp ON oc_tasks(responsibility_id, display_order);
-- +goose StatementEnd

-- Earlier category and responsibility deletes left their children behind, since
-- the cascade never ran. The rows are unreachable from both the API and the UI
-- -- getOfficerCommandData skips any child whose parent is missing -- so clearing
-- them changes nothing anyone can see. Do it once, so the explicit-delete
-- handlers start from a consistent table. Not reversible, and Down does not try.
-- Responsibilities first: clearing them orphans their assignees in turn, so the
-- assignee sweep that follows catches both those and the ones already stranded.
-- +goose StatementBegin
DELETE FROM oc_responsibilities WHERE category_id NOT IN (SELECT id FROM oc_categories);
-- +goose StatementEnd
-- +goose StatementBegin
DELETE FROM oc_assignees WHERE responsibility_id NOT IN (SELECT id FROM oc_responsibilities);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_oc_tasks_resp;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS oc_tasks;
-- +goose StatementEnd
