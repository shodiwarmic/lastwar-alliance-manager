-- +goose Up
-- Add case-insensitive index on members.name to match the existing COLLATE NOCASE
-- index on member_aliases.alias and avoid full table scans in LOWER() queries.
CREATE INDEX IF NOT EXISTS idx_members_name_nocase ON members (name COLLATE NOCASE);

-- +goose Down
DROP INDEX IF EXISTS idx_members_name_nocase;
