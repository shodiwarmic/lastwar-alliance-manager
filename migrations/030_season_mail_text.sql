-- +goose Up
-- +goose StatementBegin

-- Recreate season_mail as a text-only table (title + copy-paste content).
-- The original table stored file uploads; this replaces file_name and file_type
-- with a content TEXT column.
CREATE TABLE season_mail_new (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  season_id   INTEGER NOT NULL REFERENCES seasons(id),
  title       TEXT    NOT NULL,
  content     TEXT    NOT NULL DEFAULT '',
  posted_by   INTEGER REFERENCES users(id) ON DELETE SET NULL,
  posted_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO season_mail_new (id, season_id, title, content, posted_by, posted_at)
SELECT id, season_id, title, '', uploaded_by, uploaded_at FROM season_mail;

DROP TABLE season_mail;
ALTER TABLE season_mail_new RENAME TO season_mail;

-- +goose StatementEnd
