-- +goose Up
-- Migration: 004_credentials_table
-- Description: Creates a dedicated table for AES-GCM encrypted external API credentials.

CREATE TABLE credentials (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    service_name TEXT UNIQUE NOT NULL,
    encrypted_blob BLOB NOT NULL,
    nonce BLOB NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_credentials_service ON credentials(service_name);

-- +goose Down
DROP TABLE credentials;