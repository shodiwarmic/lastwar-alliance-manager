-- +goose Up
CREATE TABLE member_aliases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    member_id INTEGER NOT NULL,
    user_id INTEGER, -- NULL = Global Alias, SET = Personal Nickname
    alias TEXT NOT NULL COLLATE NOCASE,
    FOREIGN KEY(member_id) REFERENCES members(id) ON DELETE CASCADE,
    FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Indexes for lightning-fast OCR matching
CREATE INDEX idx_aliases_member ON member_aliases(member_id);
CREATE INDEX idx_aliases_user ON member_aliases(user_id);
CREATE INDEX idx_aliases_text ON member_aliases(alias);

-- +goose Down
DROP TABLE member_aliases;