-- +goose Up
-- File modification tracking: WHEN (updated_at) and WHO (updated_by) a file was last
-- changed — via a metadata edit or a Collabora/WOPI save. Both columns are nullable:
--
--  * updated_at — SQLite forbids a non-constant default on ALTER TABLE ADD COLUMN
--    ("Cannot add a column with non-constant default"), so it's added nullable and
--    backfilled from created_at. There is NO column default: the Go write paths set it
--    explicitly on insert, and reads use COALESCE(updated_at, created_at).
--
--  * updated_by — NULL for pre-migration rows and for un-edited uploads. Set by the two
--    edit paths (updateFile, wopiPutFile). The roster query LEFT JOINs users to resolve
--    the name. References users(id) in intent only (FKs are declarative-only app-wide).
ALTER TABLE files ADD COLUMN updated_at TIMESTAMP;
UPDATE files SET updated_at = created_at WHERE updated_at IS NULL;
ALTER TABLE files ADD COLUMN updated_by INTEGER;

-- +goose Down
ALTER TABLE files DROP COLUMN updated_by;
ALTER TABLE files DROP COLUMN updated_at;
