-- +goose Up
-- Unify outside-alliance identity under external_alliances: allies and prospect source
-- alliances now REFERENCE a registry row. The registry is the single identity source; allies
-- keep their own tag/name/server as a synced denormalization (handlers keep them in step), so
-- the existing Allies UI is untouched while the registry stays canonical for scouting/prefill.
ALTER TABLE allies ADD COLUMN external_alliance_id INTEGER;
ALTER TABLE prospects ADD COLUMN source_alliance_id INTEGER;

-- Backfill allies → their (seeded) registry row, matched on tag+name+server.
UPDATE allies SET external_alliance_id = (
    SELECT ea.id FROM external_alliances ea
    WHERE COALESCE(ea.tag,'')  = COALESCE(allies.tag,'')
      AND COALESCE(ea.name,'') = COALESCE(allies.name,'')
      AND COALESCE(ea.server,-1) = COALESCE(CAST(NULLIF(TRIM(COALESCE(allies.server,'')),'') AS INTEGER), -1)
    ORDER BY ea.id LIMIT 1
);

-- Backfill prospect source alliances (free text) → registry by exact tag or name (best-effort).
UPDATE prospects SET source_alliance_id = (
    SELECT ea.id FROM external_alliances ea
    WHERE ea.tag = TRIM(prospects.source_alliance) COLLATE NOCASE
       OR ea.name = TRIM(prospects.source_alliance) COLLATE NOCASE
    ORDER BY ea.id LIMIT 1
)
WHERE TRIM(COALESCE(source_alliance,'')) != '';

-- +goose Down
ALTER TABLE prospects DROP COLUMN source_alliance_id;
ALTER TABLE allies DROP COLUMN external_alliance_id;
