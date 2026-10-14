-- +goose Up

-- One row per update request the host update agent finished, keyed by the request's id, so its
-- outcome reaches the activity log exactly once (update_gate.go, consumeUpdateResults): the row
-- and the activity entry commit together, and a result file seen again after a crash between
-- the commit and the rename inserts nothing.
-- +goose StatementBegin
CREATE TABLE update_outcomes (
    request_id TEXT PRIMARY KEY,
    outcome    TEXT NOT NULL,
    logged_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE update_outcomes;
-- +goose StatementEnd
