-- +goose Up
-- +goose StatementBegin
-- Guarantee the singleton settings row exists. No earlier migration ever seeded
-- it, yet every settings consumer does `... WHERE id = 1` and silently no-ops if
-- it's absent (e.g. updateMyProfile's max_hq_level read, the admin settings
-- handlers, the OCR-backend reconcile). All other columns carry NOT NULL DEFAULTs
-- (see 001_baseline.sql), so an id-only insert is valid. OR IGNORE makes this a
-- no-op on existing databases where the row already exists.
INSERT OR IGNORE INTO settings (id) VALUES (1);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- No-op: removing the singleton settings row would break the app.
-- +goose StatementEnd
