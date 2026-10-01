-- +goose Up

-- Which OCR category reads each tracked type's post-event mail, so a board can be
-- imported from screenshots. A column, not a constant: the mapping is not one to
-- one — Marshal's Guard and Large Sandworm both receive the same
-- "[Alliance Exercise] Alliance Reward" mail, read as one category — and NULL
-- means the type has no mail the OCR service reads.
-- +goose StatementBegin
ALTER TABLE participation_types ADD COLUMN ocr_category TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE participation_types SET ocr_category = 'alliance_exercise'
WHERE event_type_id IN (SELECT id FROM schedule_event_types WHERE short_name IN ('MG', 'LS') AND is_system = 1)
  AND ocr_category IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE participation_types SET ocr_category = 'zombie_siege'
WHERE event_type_id IN (SELECT id FROM schedule_event_types WHERE short_name = 'ZS' AND is_system = 1)
  AND ocr_category IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE participation_types SET ocr_category = 'desert_storm'
WHERE event_type_id IN (SELECT id FROM schedule_event_types WHERE short_name = 'DS')
  AND ocr_category IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE participation_types DROP COLUMN ocr_category;
-- +goose StatementEnd
