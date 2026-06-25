-- +goose Up
-- LastRank avatar URLs (hotlinked from the game CDN). Populated from the
-- per-player endpoint during the extended sync (members) and prospect lookup.
-- Note: rendering these in production requires the game CDN hosts in the
-- reverse-proxy CSP img-src (lastwar-cdn.akamaized.net / lastwar-cdn.lastwarapp.net).
ALTER TABLE members  ADD COLUMN lastrank_photo_url      TEXT NOT NULL DEFAULT '';
ALTER TABLE members  ADD COLUMN lastrank_photo_failover TEXT NOT NULL DEFAULT '';
ALTER TABLE prospects ADD COLUMN lastrank_photo_url      TEXT NOT NULL DEFAULT '';
ALTER TABLE prospects ADD COLUMN lastrank_photo_failover TEXT NOT NULL DEFAULT '';
