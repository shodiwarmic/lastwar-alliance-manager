-- +goose Up
ALTER TABLE settings ADD COLUMN alliance_name TEXT NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN alliance_tag  TEXT NOT NULL DEFAULT '';
