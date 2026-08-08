-- +goose Up
-- +goose StatementBegin
-- Server-side bulk jobs (LastRank extended sync, NAP member counts, external-alliance
-- gather, prospect refresh). These used to be browser-driven loops: ~1 request/second
-- for as long as the run takes, with the tab held open. Closing it stopped the run.
--
-- Progress is persisted rather than kept in memory so an officer can navigate away and
-- come back — and so ANY browser can watch a run, including one started by the
-- scheduler with no browser involved at all.
--
-- The REFERENCES clause is documentation only: PRAGMA foreign_keys is not enabled
-- app-wide (see 056/059/062), so ON DELETE CASCADE never fires. Item cleanup is
-- explicit in Go — see pruneOldJobs in jobs.go, which deletes items before jobs.
CREATE TABLE background_jobs (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    kind                TEXT NOT NULL,
    -- running | done | failed | interrupted | cancelled
    status              TEXT NOT NULL,
    -- manual | scheduled. Named trigger_source, not `trigger`: TRIGGER is a SQLite
    -- keyword and, while it parses as a bare column name today, it reads as a
    -- landmine to anyone writing raw SQL against this table later.
    trigger_source      TEXT NOT NULL DEFAULT 'manual',
    -- NULL for scheduled runs. activity_log.user_id uses NULL the same way; a
    -- sentinel 0 would be a dangling reference into users(id).
    started_by_user_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    started_by_username TEXT NOT NULL DEFAULT '',
    total               INTEGER NOT NULL DEFAULT 0,
    processed           INTEGER NOT NULL DEFAULT 0,
    -- JSON blob, opaque to the runner: each job kind defines its own counter keys,
    -- so adding a metric to one flow needs no schema change.
    counters            TEXT NOT NULL DEFAULT '{}',
    error               TEXT NOT NULL DEFAULT '',
    started_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at         TIMESTAMP
);

CREATE TABLE background_job_items (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id  INTEGER NOT NULL REFERENCES background_jobs(id) ON DELETE CASCADE,
    seq     INTEGER NOT NULL,
    label   TEXT NOT NULL,
    -- member_id / external_alliance_id / prospect_id, per kind. 0 when not applicable.
    ref_id  INTEGER NOT NULL DEFAULT 0,
    -- queued | active | done | skip | err
    state   TEXT NOT NULL DEFAULT 'queued',
    detail  TEXT NOT NULL DEFAULT ''
);

-- Ordered replay of one job's items (the progress list).
CREATE INDEX idx_bg_job_items_job ON background_job_items(job_id, seq);
-- "latest run of this kind", for the progress poll and the scheduler's due check.
CREATE INDEX idx_bg_jobs_kind_started ON background_jobs(kind, started_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_bg_jobs_kind_started;
DROP INDEX IF EXISTS idx_bg_job_items_job;
DROP TABLE IF EXISTS background_job_items;
DROP TABLE IF EXISTS background_jobs;
-- +goose StatementEnd
