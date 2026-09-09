-- +goose Up
-- Repairs season_events rows whose type was blanked by the Edit Season modal.
--
-- The modal's type dropdown was built from a cache that read `d.event_types`
-- while GET /api/schedule/event-types returns a bare array, so the dropdown only
-- ever held "— none —". Every Save from that modal therefore wrote
-- event_type_id NULL and type_name '', after which Push to Schedule skipped the
-- row as `skipped_no_type` — and Sync Event Types could not repair it either,
-- because its backfill matches on `type_name != ''`.
--
-- The repair is deliberately narrow and idempotent:
--   * only rows that have lost BOTH fields are touched;
--   * the name is recovered from the season's own template entry, matched by
--     label, so a row renamed inside the modal is left alone (the officer
--     re-types it in the now-working dropdown);
--   * json_each() is fed through a CASE that substitutes '[]' for an
--     unparseable blob. The template save handlers store `events` without
--     validating it, and json_each raises on invalid JSON — which during a
--     migration would block boot.

-- +goose StatementBegin
UPDATE season_events
SET type_name = COALESCE((
        SELECT json_extract(je.value, '$.type_name')
        FROM seasons s
        JOIN season_templates st ON st.season_number = s.season_number
        JOIN json_each(CASE WHEN json_valid(st.events) THEN st.events ELSE '[]' END) je
        WHERE s.id = season_events.season_id
          AND json_extract(je.value, '$.label') = season_events.label
          AND COALESCE(json_extract(je.value, '$.type_name'), '') != ''
        LIMIT 1
    ), '')
WHERE event_type_id IS NULL
  AND COALESCE(type_name, '') = '';
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE season_events
SET event_type_id = (
        SELECT id FROM schedule_event_types WHERE name = season_events.type_name
    )
WHERE event_type_id IS NULL
  AND COALESCE(type_name, '') != ''
  AND EXISTS (SELECT 1 FROM schedule_event_types WHERE name = season_events.type_name);
-- +goose StatementEnd
