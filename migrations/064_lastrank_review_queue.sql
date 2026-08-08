-- +goose Up
-- +goose StatementBegin
-- Durable queue for LastRank Phase-1 DECISIONS.
--
-- The review modal was entirely ephemeral: close it and the state was gone, and a
-- rejected rank change came back identically on the next sync. Officers had no way
-- to work a backlog, and nothing unattended could ever propose a change safely.
--
-- The queue holds decisions, not data. Power / hero / HQ are staleness-gated,
-- provenance-stamped, append-only history — nothing there needs a human. What needs
-- a human is: a rank changed, a member renamed, a name we can't match, a member who
-- looks like they left.
--
-- subject_key, NOT (member_id, lastrank_public_id):
--   lastrankAllianceMember.PublicID is a non-pointer int, so a missing public_id
--   decodes to 0 — and the preview's pass 0 explicitly skips PublicID == 0,
--   confirming zero occurs in real data. Two unmatched entries without a public id
--   would collide on ('unmatched', 0, 0) and the second upsert would clobber the
--   first. The key is built canonically in Go (lastRankSubjectKey):
--     rank/name/archive        -> m:<member_id>
--     unmatched, id known      -> p:<public_id>
--     unmatched, no id         -> n:<foldName(lastrank_name)>
--   Folding the name fallback keeps a re-accented name from minting a duplicate.
--
-- The REFERENCES clause is documentation only — foreign_keys is off app-wide (see
-- 056/059/062), so ON DELETE SET NULL never fires. Rows for a deleted member are
-- cleared by the next reconcile, which deletes anything the pull no longer proposes.
CREATE TABLE lastrank_pending_changes (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    -- rank | name | unmatched | archive
    kind                TEXT NOT NULL,
    subject_key         TEXT NOT NULL,
    member_id           INTEGER NOT NULL DEFAULT 0 REFERENCES members(id) ON DELETE SET NULL,
    lastrank_public_id  INTEGER NOT NULL DEFAULT 0,
    lastrank_name       TEXT NOT NULL DEFAULT '',
    current_value       TEXT NOT NULL DEFAULT '',
    proposed_value      TEXT NOT NULL DEFAULT '',
    -- Identity of the PROPOSAL, not of the subject: kind|member_id|proposed_value.
    -- "Defer until it changes" means "until this fingerprint changes".
    fingerprint         TEXT NOT NULL,
    reason              TEXT NOT NULL DEFAULT '',
    -- Upstream capture date of the pull that last saw this proposal. Re-opening a
    -- defer_once row keys on this ADVANCING, not merely on another refresh — so
    -- clicking Fetch twice in a minute doesn't evaporate the deferral.
    capture_date        TEXT NOT NULL DEFAULT '',
    -- open | deferred_once | deferred_until_changed
    status              TEXT NOT NULL DEFAULT 'open',
    first_seen_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deferred_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    deferred_at         TIMESTAMP,
    UNIQUE(kind, subject_key)
);

-- The dashboard card, the Members-panel badge and the login toast all ask the same
-- question: how many open items are there?
CREATE INDEX idx_lastrank_pending_status ON lastrank_pending_changes(status, kind);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_lastrank_pending_status;
DROP TABLE IF EXISTS lastrank_pending_changes;
-- +goose StatementEnd
