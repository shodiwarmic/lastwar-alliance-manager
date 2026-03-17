-- migrations/003_dyno_rbac_feedback.sql
-- +goose Up
-- +goose StatementBegin
ALTER TABLE dyno_recommendations ADD COLUMN is_author_public BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE dyno_recommendations ADD COLUMN min_view_rank TEXT NOT NULL DEFAULT '';
ALTER TABLE rank_permissions ADD COLUMN view_anonymous_authors BOOLEAN NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- +goose StatementEnd