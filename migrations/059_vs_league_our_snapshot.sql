-- +goose Up
-- Snapshot OUR alliance's power/kills/members per week (mirrors the opponent snapshot), so the
-- historical "us vs them" picture for a week is frozen and doesn't drift as live power changes.
-- Sourced from our LastRank alliance when configured (settings.lastrank_alliance_id), else from the
-- summed member roster (hand-editable). Nullable — weeks captured before this stay blank.
ALTER TABLE vs_league_weeks ADD COLUMN our_power INTEGER;
ALTER TABLE vs_league_weeks ADD COLUMN our_kills INTEGER;
ALTER TABLE vs_league_weeks ADD COLUMN our_member_count INTEGER;
ALTER TABLE vs_league_weeks ADD COLUMN our_snapshot_at DATETIME;

-- +goose Down
ALTER TABLE vs_league_weeks DROP COLUMN our_snapshot_at;
ALTER TABLE vs_league_weeks DROP COLUMN our_member_count;
ALTER TABLE vs_league_weeks DROP COLUMN our_kills;
ALTER TABLE vs_league_weeks DROP COLUMN our_power;
