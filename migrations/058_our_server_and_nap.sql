-- +goose Up
-- +goose StatementBegin
-- Our own server number. Nullable: not every deployment configures one, and the NAP tab degrades
-- gracefully when it's unset.
ALTER TABLE settings ADD COLUMN our_server_id INTEGER;

-- NAP sizing. nap_size = how many top alliances the pact covers (INCLUDING us — it's a pact
-- between the top X, and we are one of them). nap_import_limit = how many we actually fetch and
-- cache from LastRank (>= nap_size, giving headroom to see who sits just below the line). A server
-- has 50+ alliances; importing them all would fill the registry with rows nobody benefits from.
ALTER TABLE settings ADD COLUMN nap_size         INTEGER NOT NULL DEFAULT 10;
ALTER TABLE settings ADD COLUMN nap_import_limit INTEGER NOT NULL DEFAULT 15;

-- True server rank from the LastRank ladder. Needed because we exclude ourselves from the registry:
-- ordering the remaining rows by power would renumber them 1,2,3... and silently skip our position.
ALTER TABLE external_alliances ADD COLUMN power_rank INTEGER;
ALTER TABLE external_alliances ADD COLUMN kills_rank INTEGER;

-- Capture date of the LADDER snapshot that last wrote this row's power/kills/ranks.
-- Deliberately NOT lastrank_seen_at: that column carries the upstream *last_seen_at* (a game-side
-- "alliance last active" clock, written by the per-alliance detail endpoints). The ladder endpoint
-- returns no last_seen_at at all, and guarding a ladder captured_at against a detail last_seen_at
-- would mix two different clocks — a per-alliance Refresh done today would then permanently block
-- ladder writes for that row, leaving its power_rank NULL forever. Separate column, one meaning.
ALTER TABLE external_alliances ADD COLUMN lastrank_captured_at DATETIME;

-- Alliance-level time series: the alliance analogue of power_history / kill_history. Every ladder
-- capture we see is appended, so growth and rank movement can be charted later.
--
-- SUBJECT KEY IS external_alliance_id — our internal registry id, NOT lastrank_id. This follows the
-- identity rule 057 already set for the registry ("our internal id (the PK) is the identity — NOT
-- the external LastRank id; lastrank_id is kept only as a reference attribute, nullable, not a
-- key"). LastRank is A SOURCE, NOT A REQUIRED SERVICE: a datapoint may equally come from OCR, CSV,
-- the mobile scanner, or an officer typing it in, none of which have a lastrank_id. Keying on
-- lastrank_id would make those rows unstorable and contradict the source column below.
--
-- Any source must therefore resolve its alliance into external_alliances first (via
-- findOrCreateExternalAllianceTx) — the registry stays the single identity source.
--
-- OUR OWN alliance is the one subject with no registry row (it must never be pickable as a VS
-- opponent), so is_own = 1 identifies its series instead. There is exactly one "us", so is_own is a
-- complete key for it. The CHECK keeps the two cases mutually exclusive.
--
-- tag/name are denormalized on purpose: alliances rename, and a snapshot should record what they
-- were called at the time. recorded_at is the OBSERVATION date (LastRank's captured_at for synced
-- rows, the screenshot/entry date otherwise) — never the sync time.
CREATE TABLE alliance_stats_history (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    external_alliance_id INTEGER,          -- subject identity; NULL iff is_own = 1
    is_own               INTEGER NOT NULL DEFAULT 0,
    lastrank_id          TEXT,             -- reference attribute only, nullable, NOT a key
    server               INTEGER,
    tag                  TEXT,
    name                 TEXT,
    power                INTEGER,
    kills                INTEGER,
    power_rank           INTEGER,
    kills_rank           INTEGER,
    member_count         INTEGER,
    recorded_at          TIMESTAMP NOT NULL,
    source               TEXT NOT NULL DEFAULT 'manual',   -- manual|lastrank|ocr|csv|mobile
    created_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CHECK ((is_own = 1 AND external_alliance_id IS NULL)
        OR (is_own = 0 AND external_alliance_id IS NOT NULL)),
    -- Two sources legitimately observing the same alliance on the same date are distinct
    -- datapoints, so source is part of the key. Re-running one source is a no-op.
    UNIQUE(external_alliance_id, recorded_at, source)
);
-- SQLite treats NULLs as distinct in UNIQUE, so the constraint above does NOT cover our own rows
-- (external_alliance_id IS NULL) — without this partial index they would duplicate on every refresh.
CREATE UNIQUE INDEX idx_alliance_stats_own_unique
    ON alliance_stats_history(recorded_at, source) WHERE is_own = 1;
CREATE INDEX idx_alliance_stats_subject ON alliance_stats_history(external_alliance_id, recorded_at DESC);
CREATE INDEX idx_alliance_stats_server  ON alliance_stats_history(server, recorded_at DESC);

-- Normalize officer-entered ally server strings ("S123", "#123", " 123 ") to digits.
-- LTRIM strips leading S/s/#/space, then CAST takes the leading digit run — the same
-- first-contiguous-run semantics as parseServerNumber in Go ("123S456" -> 123, never a fused
-- 123456). Guarded by > 0 so genuinely unparseable values ("Server 1712") are left untouched rather
-- than overwritten with a wrong number; Go parses those correctly on every read anyway.
UPDATE allies
SET server = CAST(LTRIM(server, 'Ss# ') AS INTEGER)
WHERE TRIM(COALESCE(server, '')) != ''
  AND CAST(LTRIM(server, 'Ss# ') AS INTEGER) > 0;

-- Repair registry rows that 057 seeded with server = 0 from an "S123"-style ally.
UPDATE external_alliances
SET server = (
    SELECT CAST(a.server AS INTEGER) FROM allies a
    WHERE a.external_alliance_id = external_alliances.id
      AND CAST(a.server AS INTEGER) > 0 LIMIT 1
)
WHERE (server IS NULL OR server = 0)
  AND EXISTS (
    SELECT 1 FROM allies a
    WHERE a.external_alliance_id = external_alliances.id
      AND CAST(a.server AS INTEGER) > 0
  );

-- PURGE OURSELVES FROM THE REGISTRY.
-- "Stop inserting" is not the same as "not present": createExternalAlliance and
-- lookupExternalAlliance let an officer cache ANY alliance, and findOrCreateExternalAllianceTx
-- minted one from any ally/prospect save carrying our tag. A row for us may therefore already
-- exist, and skipping future writes would just freeze it — still pickable as a VS opponent.
--
-- Match on lastrank_id OR tag: a manually-created row has a NULL lastrank_id, so an id-only purge
-- would miss it. Detach references before deleting — FKs are not enforced app-wide, so a bare
-- DELETE leaves dangling ids. NOTE this is deliberately NOT what deleteExternalAlliance does: that
-- handler REFUSES with 409 when an ally references the row and detaches prospects only. Blocking
-- would strand the invariant, so detaching the ally link here is intentional new behaviour.
--
-- Known, accepted collateral: the tag half matches on ANY server, because our_server_id is added by
-- this very migration and is still NULL while it runs — there is nothing to scope by. Tags are
-- reusable over time (057), so in principle a cached cross-server opponent holding a tag we once
-- used could be deleted. The loss is one cache row, recoverable with a re-lookup. The refresh-time
-- scrub, where the server IS known, scopes its tag match.
--
-- All three statements no-op when both lastrank_alliance_id and alliance_tag are unset.
UPDATE prospects SET source_alliance_id = NULL
WHERE source_alliance_id IN (SELECT id FROM external_alliances ea WHERE
      (ea.lastrank_id IS NOT NULL AND ea.lastrank_id = (SELECT NULLIF(TRIM(COALESCE(lastrank_alliance_id,'')),'') FROM settings WHERE id=1))
   OR (TRIM(COALESCE(ea.tag,'')) != '' AND ea.tag = (SELECT NULLIF(TRIM(COALESCE(alliance_tag,'')),'') FROM settings WHERE id=1) COLLATE NOCASE));

UPDATE allies SET external_alliance_id = NULL
WHERE external_alliance_id IN (SELECT id FROM external_alliances ea WHERE
      (ea.lastrank_id IS NOT NULL AND ea.lastrank_id = (SELECT NULLIF(TRIM(COALESCE(lastrank_alliance_id,'')),'') FROM settings WHERE id=1))
   OR (TRIM(COALESCE(ea.tag,'')) != '' AND ea.tag = (SELECT NULLIF(TRIM(COALESCE(alliance_tag,'')),'') FROM settings WHERE id=1) COLLATE NOCASE));

-- No table alias here: SQLite rejects a bare alias in DELETE ("DELETE FROM t x" is a syntax error),
-- and migrations run in initDB() at startup, so a bad statement doesn't just fail the migration —
-- it stops the app booting.
DELETE FROM external_alliances WHERE
      (lastrank_id IS NOT NULL AND lastrank_id = (SELECT NULLIF(TRIM(COALESCE(lastrank_alliance_id,'')),'') FROM settings WHERE id=1))
   OR (TRIM(COALESCE(tag,'')) != '' AND tag = (SELECT NULLIF(TRIM(COALESCE(alliance_tag,'')),'') FROM settings WHERE id=1) COLLATE NOCASE);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- The data repair and the self-purge are intentionally not reversed: they correct corrupt/invalid
-- state rather than adding anything.
DROP TABLE IF EXISTS alliance_stats_history;   -- drops its indexes with it
ALTER TABLE external_alliances DROP COLUMN lastrank_captured_at;
ALTER TABLE external_alliances DROP COLUMN kills_rank;
ALTER TABLE external_alliances DROP COLUMN power_rank;
ALTER TABLE settings DROP COLUMN nap_import_limit;
ALTER TABLE settings DROP COLUMN nap_size;
ALTER TABLE settings DROP COLUMN our_server_id;
-- +goose StatementEnd
