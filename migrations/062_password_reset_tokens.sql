-- +goose Up
-- +goose StatementBegin
-- Single-use password reset links. Separate from invite_tokens on purpose: different
-- lifecycle (repeatable vs one-time first setup), different expiry (24h vs 48h) and
-- different linkage (user_id vs member_id).
--
-- The REFERENCES clauses are documentation and future-proofing only — PRAGMA
-- foreign_keys is not enabled app-wide (see 056), so ON DELETE CASCADE never fires.
-- Cleanup is explicit in Go: deactivateUser and deleteAdminUser both purge tokens.
--
-- Indexed on user_id only. expires_at is deliberately NOT indexed: the passive sweep in
-- createPasswordResetToken keeps this table to a handful of rows, so the scan it would
-- optimise is already trivial, while every index adds write cost that
-- SetMaxOpenConns(1) serializes across the whole app.
CREATE TABLE password_reset_tokens (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    token      TEXT NOT NULL UNIQUE,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    used_at    TIMESTAMP
);
CREATE INDEX idx_password_reset_tokens_user ON password_reset_tokens(user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS password_reset_tokens;   -- drops its index with it
-- +goose StatementEnd
