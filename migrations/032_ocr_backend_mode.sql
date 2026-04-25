-- +goose Up
-- Add a settings field that selects between the cloud (Google Cloud Vision)
-- OCR backend and the local (PaddleOCR sidecar) backend. Default keeps the
-- existing Cloud Vision behaviour for any installation that doesn't opt in.
ALTER TABLE settings ADD COLUMN ocr_backend_mode TEXT NOT NULL DEFAULT 'cloud';
-- Allowed values: 'cloud' (Google Cloud Vision via OIDC) | 'local' (PaddleOCR
-- sidecar via plain HTTP). The install.sh / update.sh prompts toggle this
-- when the user enables the local OCR sidecar. Manual-mode upload UI shows
-- screen + tab dropdowns when this is 'local' (PaddleOCR's stylised-header
-- read isn't reliable enough for auto-classification, so the user picks
-- the scene per batch).

-- +goose Down
ALTER TABLE settings DROP COLUMN ocr_backend_mode;
