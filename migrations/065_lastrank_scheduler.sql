-- +goose Up
-- +goose StatementBegin
-- Opt-in scheduled LastRank retrieval. Everything defaults OFF: an operator who
-- upgrades gets exactly the manual behaviour they had.
--
-- WHY 6h / 21h. The tick interval decides how often we look; the enrich max age
-- decides, per member, whether that tick actually re-pulls. Ages of 6h, 12h and 18h
-- all fall under 21h, so only the 24h slot clears it — one enrich per member per
-- 24h (a hard "refreshed within a day" guarantee), while the 1-request alliance
-- pull runs 4×/day so departures and rank changes reach the review queue within 6h
-- instead of 12.
--
-- The max age's legal band is a FUNCTION of the interval: 24 - interval < max_age
-- <= 23. Below that an extra tick per day clears the threshold; above it the run's
-- own duration can push a member past the next day's slot and revive an
-- alternate-day silent skip. updateSettings computes the band from the submitted
-- interval rather than hard-coding it.
ALTER TABLE settings ADD COLUMN lastrank_auto_sync_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE settings ADD COLUMN lastrank_auto_sync_hour INTEGER NOT NULL DEFAULT 4;
ALTER TABLE settings ADD COLUMN lastrank_auto_sync_interval_hours INTEGER NOT NULL DEFAULT 6;
ALTER TABLE settings ADD COLUMN lastrank_enrich_max_age_hours INTEGER NOT NULL DEFAULT 21;
ALTER TABLE settings ADD COLUMN nap_auto_refresh_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE settings ADD COLUMN prospect_auto_refresh_enabled INTEGER NOT NULL DEFAULT 0;

-- THREE timestamps, three different questions. Conflating any two reintroduces a
-- starvation bug, so they are deliberately separate columns:
--
--   synced_at    when we last TRIED. Always advances, so it is safe to ORDER BY —
--                that is what stops a sweep retrying the same row forever.
--   enriched_at  when the data last actually refreshed upstream (enrich_status
--                "fetched" only). Safe to FILTER by, never to order by: a member
--                whose enrich comes back "gated" never advances it.
--   attempted_at when we last made a per-player call, whatever the outcome. The
--                scheduled backoff: without it a permanently-gated member is due
--                on every tick forever and the "free" ticks stop being free.
--
-- members already has lastrank_synced_at (migration 050) — but it is ALSO stamped
-- by the Phase-1 commit paths, so a commit shortly before a heavy tick would make
-- the whole roster look freshly attempted and starve the sweep for a day. Hence a
-- dedicated attempted_at, written only by the per-player sync.
ALTER TABLE members ADD COLUMN lastrank_enriched_at DATETIME;
ALTER TABLE members ADD COLUMN lastrank_attempted_at DATETIME;

-- prospects had NO timestamps at all ("No synced_at — no history kept"), which is
-- fine for a click and not fine for a schedule: no resume ordering, no pre-filter,
-- and no staleness reference. Nothing else writes prospects.lastrank_synced_at, so
-- there it doubles as the attempt stamp — only members need the separate column.
--
-- captured_at is the prospect-side equivalent of recorded_at on the member history
-- tables. Prospects store point-in-time values rather than history, so without it a
-- cached/gated response would overwrite a fresher hand-entered figure — which is
-- how most prospects get their numbers before anyone finds their LastRank id.
ALTER TABLE prospects ADD COLUMN lastrank_synced_at DATETIME;
ALTER TABLE prospects ADD COLUMN lastrank_enriched_at DATETIME;
ALTER TABLE prospects ADD COLUMN lastrank_captured_at DATETIME;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE prospects DROP COLUMN lastrank_captured_at;
ALTER TABLE prospects DROP COLUMN lastrank_enriched_at;
ALTER TABLE prospects DROP COLUMN lastrank_synced_at;
ALTER TABLE members DROP COLUMN lastrank_attempted_at;
ALTER TABLE members DROP COLUMN lastrank_enriched_at;
ALTER TABLE settings DROP COLUMN prospect_auto_refresh_enabled;
ALTER TABLE settings DROP COLUMN nap_auto_refresh_enabled;
ALTER TABLE settings DROP COLUMN lastrank_enrich_max_age_hours;
ALTER TABLE settings DROP COLUMN lastrank_auto_sync_interval_hours;
ALTER TABLE settings DROP COLUMN lastrank_auto_sync_hour;
ALTER TABLE settings DROP COLUMN lastrank_auto_sync_enabled;
-- +goose StatementEnd
