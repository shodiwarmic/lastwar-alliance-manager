-- +goose Up
-- +goose StatementBegin
CREATE TABLE accountability_strikes_new (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    member_id      INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    strike_type    TEXT    NOT NULL DEFAULT 'manual',
    reason         TEXT    NOT NULL DEFAULT '',
    ref_date       TEXT,
    status         TEXT    NOT NULL DEFAULT 'active'
                   CHECK(status IN ('active','excused')),
    excused_by     INTEGER REFERENCES users(id) ON DELETE SET NULL,
    excused_reason TEXT    NOT NULL DEFAULT '',
    created_by     INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at     DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO accountability_strikes_new
    SELECT id, member_id, strike_type, reason, ref_date, status,
           excused_by, excused_reason, created_by, created_at
    FROM accountability_strikes;

DROP TABLE accountability_strikes;
ALTER TABLE accountability_strikes_new RENAME TO accountability_strikes;

CREATE INDEX idx_strikes_member ON accountability_strikes(member_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Intentionally left blank
-- +goose StatementEnd
