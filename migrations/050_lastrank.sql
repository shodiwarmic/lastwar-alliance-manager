-- +goose Up
-- LastRank.fun enrichment integration.
-- Member/prospect links to the per-player endpoint, the alliance id setting,
-- and a data-provenance column on the history tables.

-- Per-player endpoint linkage. public_id for members is auto-captured from the
-- Phase-1 alliance fetch; prospects are looked up individually (no synced_at).
ALTER TABLE members ADD COLUMN lastrank_public_id INTEGER;
ALTER TABLE members ADD COLUMN lastrank_synced_at DATETIME;
ALTER TABLE prospects ADD COLUMN lastrank_public_id INTEGER;

-- Alliance id lives alongside the other alliance-identity settings. Default is
-- empty so other operators don't inherit this deployment's id; the known id is
-- surfaced as an input placeholder in the Settings UI instead.
ALTER TABLE settings ADD COLUMN lastrank_alliance_id TEXT NOT NULL DEFAULT '';

-- Datapoint provenance. Values: 'lastrank' | 'ocr' | 'csv' | 'mobile' | 'manual'.
-- Default 'manual' is accurate for the genuinely-manual write paths; the import
-- and mobile paths stamp their true source going forward. Rows that predate this
-- migration are all labelled 'manual' and cannot be reclassified retroactively.
ALTER TABLE power_history      ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE hero_power_history ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE kill_history       ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE squad_power_history ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';
