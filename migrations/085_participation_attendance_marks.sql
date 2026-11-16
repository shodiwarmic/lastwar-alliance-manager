-- +goose Up
-- SQLite CHECK constraints require rebuilding the table. manual_missed preserves
-- the officer's underlying substitute miss across Excuse / Dismiss and their undo.
-- Legacy exceptions retain their original kind and provenance (board source).
-- +goose StatementBegin
CREATE TABLE participation_exceptions_new (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    board_id    INTEGER NOT NULL REFERENCES participation_boards(id) ON DELETE CASCADE,
    member_id   INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK(kind IN ('excused','dismissed','missed','late')),
    reason      TEXT NOT NULL DEFAULT '',
    recorded_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    manual_missed INTEGER NOT NULL DEFAULT 0 CHECK(manual_missed IN (0,1)),
    UNIQUE(board_id, member_id)
);
INSERT INTO participation_exceptions_new
    (id, board_id, member_id, kind, reason, recorded_by, created_at)
    SELECT id, board_id, member_id, kind, reason, recorded_by, created_at
    FROM participation_exceptions;
DROP TABLE participation_exceptions;
ALTER TABLE participation_exceptions_new RENAME TO participation_exceptions;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TABLE participation_exceptions_new (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    board_id    INTEGER NOT NULL REFERENCES participation_boards(id) ON DELETE CASCADE,
    member_id   INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK(kind IN ('excused','dismissed','missed')),
    reason      TEXT NOT NULL DEFAULT '',
    recorded_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(board_id, member_id)
);
-- Late is informational: removing it restores the ordinary entry-derived Present.
INSERT INTO participation_exceptions_new
    SELECT id, board_id, member_id, kind, reason, recorded_by, created_at
    FROM participation_exceptions WHERE kind != 'late';
DROP TABLE participation_exceptions;
ALTER TABLE participation_exceptions_new RENAME TO participation_exceptions;
-- +goose StatementEnd
