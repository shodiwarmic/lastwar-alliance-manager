-- +goose Up
-- OCR request archival: retains the uploaded screenshots + the worker's parsed
-- response so they can be used to improve OCR accuracy and diagnose extraction
-- mistakes. Best-effort, non-blocking, and off by default.
--
-- ocr_archive_mode selects the destination(s):
--   'none'  (default) | 'gcp' (GCS bucket) | 'local' (disk via OCR_ARCHIVE_DIR) | 'both'
-- This is decoupled from ocr_backend_mode — either OCR backend (cloud/local) may
-- archive to either destination. Configured by an admin in Admin → Security.
ALTER TABLE settings ADD COLUMN ocr_archive_mode TEXT NOT NULL DEFAULT 'none';
-- GCS bucket name for 'gcp'/'both' archival. Set in the admin UI (not env), like
-- cv_worker_url. Empty disables the GCS destination.
ALTER TABLE settings ADD COLUMN ocr_archive_bucket TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE settings DROP COLUMN ocr_archive_mode;
ALTER TABLE settings DROP COLUMN ocr_archive_bucket;
