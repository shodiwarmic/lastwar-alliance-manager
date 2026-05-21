-- +goose Up
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN permissions TEXT NOT NULL DEFAULT '{}';
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE rank_permissions SET permissions =
    '{"view_train":'             || CASE WHEN view_train             THEN 'true' ELSE 'false' END ||
    ',"manage_train":'           || CASE WHEN manage_train           THEN 'true' ELSE 'false' END ||
    ',"view_awards":'            || CASE WHEN view_awards            THEN 'true' ELSE 'false' END ||
    ',"manage_awards":'          || CASE WHEN manage_awards          THEN 'true' ELSE 'false' END ||
    ',"view_recs":'              || CASE WHEN view_recs              THEN 'true' ELSE 'false' END ||
    ',"manage_recs":'            || CASE WHEN manage_recs            THEN 'true' ELSE 'false' END ||
    ',"view_dyno":'              || CASE WHEN view_dyno              THEN 'true' ELSE 'false' END ||
    ',"manage_dyno":'            || CASE WHEN manage_dyno            THEN 'true' ELSE 'false' END ||
    ',"view_rankings":'          || CASE WHEN view_rankings          THEN 'true' ELSE 'false' END ||
    ',"view_storm":'             || CASE WHEN view_storm             THEN 'true' ELSE 'false' END ||
    ',"manage_storm":'           || CASE WHEN manage_storm           THEN 'true' ELSE 'false' END ||
    ',"view_vs_points":'         || CASE WHEN view_vs_points         THEN 'true' ELSE 'false' END ||
    ',"manage_vs_points":'       || CASE WHEN manage_vs_points       THEN 'true' ELSE 'false' END ||
    ',"view_upload":'            || CASE WHEN view_upload            THEN 'true' ELSE 'false' END ||
    ',"manage_members":'         || CASE WHEN manage_members         THEN 'true' ELSE 'false' END ||
    ',"manage_settings":'        || CASE WHEN manage_settings        THEN 'true' ELSE 'false' END ||
    ',"view_files":'             || CASE WHEN view_files             THEN 'true' ELSE 'false' END ||
    ',"manage_files":'           || CASE WHEN manage_files           THEN 'true' ELSE 'false' END ||
    ',"upload_files":'           || CASE WHEN upload_files           THEN 'true' ELSE 'false' END ||
    ',"view_anonymous_authors":' || CASE WHEN view_anonymous_authors THEN 'true' ELSE 'false' END ||
    ',"view_schedule":'          || CASE WHEN view_schedule          THEN 'true' ELSE 'false' END ||
    ',"manage_schedule":'        || CASE WHEN manage_schedule        THEN 'true' ELSE 'false' END ||
    ',"view_officer_command":'   || CASE WHEN view_officer_command   THEN 'true' ELSE 'false' END ||
    ',"manage_officer_command":' || CASE WHEN manage_officer_command THEN 'true' ELSE 'false' END ||
    ',"view_recruiting":'        || CASE WHEN view_recruiting        THEN 'true' ELSE 'false' END ||
    ',"manage_recruiting":'      || CASE WHEN manage_recruiting      THEN 'true' ELSE 'false' END ||
    ',"view_allies":'            || CASE WHEN view_allies            THEN 'true' ELSE 'false' END ||
    ',"manage_allies":'          || CASE WHEN manage_allies          THEN 'true' ELSE 'false' END ||
    ',"view_activity":'          || CASE WHEN view_activity          THEN 'true' ELSE 'false' END ||
    ',"view_accountability":'    || CASE WHEN view_accountability    THEN 'true' ELSE 'false' END ||
    ',"manage_accountability":'  || CASE WHEN manage_accountability  THEN 'true' ELSE 'false' END ||
    ',"view_season_hub":'        || CASE WHEN view_season_hub        THEN 'true' ELSE 'false' END ||
    ',"manage_season_hub":'      || CASE WHEN manage_season_hub      THEN 'true' ELSE 'false' END ||
    ',"manage_season_rewards":'  || CASE WHEN manage_season_rewards  THEN 'true' ELSE 'false' END ||
    ',"view_comms":'             || CASE WHEN view_comms             THEN 'true' ELSE 'false' END ||
    ',"manage_comms":'           || CASE WHEN manage_comms           THEN 'true' ELSE 'false' END ||
    '}';
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN view_train;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN manage_train;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN view_awards;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN manage_awards;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN view_recs;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN manage_recs;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN view_dyno;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN manage_dyno;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN view_rankings;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN view_storm;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN manage_storm;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN view_vs_points;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN manage_vs_points;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN view_upload;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN manage_members;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN manage_settings;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN view_files;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN manage_files;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN upload_files;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN view_anonymous_authors;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN view_schedule;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN manage_schedule;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN view_officer_command;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN manage_officer_command;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN view_recruiting;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN manage_recruiting;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN view_allies;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN manage_allies;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN view_activity;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN view_accountability;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN manage_accountability;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN view_season_hub;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN manage_season_hub;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN manage_season_rewards;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN view_comms;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN manage_comms;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_train             INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN manage_train           INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_awards            INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN manage_awards          INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_recs              INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN manage_recs            INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_dyno              INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN manage_dyno            INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_rankings          INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_storm             INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN manage_storm           INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_vs_points         INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN manage_vs_points       INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_upload            INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN manage_members         INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN manage_settings        INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_files             INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN manage_files           INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN upload_files           INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_anonymous_authors INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_schedule          INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN manage_schedule        INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_officer_command   INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN manage_officer_command INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_recruiting        INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN manage_recruiting      INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_allies            INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN manage_allies          INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_activity          INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_accountability    INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN manage_accountability  INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_season_hub        INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN manage_season_hub      INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN manage_season_rewards  INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN view_comms             INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE rank_permissions ADD COLUMN manage_comms           INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE rank_permissions SET
    view_train             = CAST(json_extract(permissions, '$.view_train')             AS INTEGER),
    manage_train           = CAST(json_extract(permissions, '$.manage_train')           AS INTEGER),
    view_awards            = CAST(json_extract(permissions, '$.view_awards')            AS INTEGER),
    manage_awards          = CAST(json_extract(permissions, '$.manage_awards')          AS INTEGER),
    view_recs              = CAST(json_extract(permissions, '$.view_recs')              AS INTEGER),
    manage_recs            = CAST(json_extract(permissions, '$.manage_recs')            AS INTEGER),
    view_dyno              = CAST(json_extract(permissions, '$.view_dyno')              AS INTEGER),
    manage_dyno            = CAST(json_extract(permissions, '$.manage_dyno')            AS INTEGER),
    view_rankings          = CAST(json_extract(permissions, '$.view_rankings')          AS INTEGER),
    view_storm             = CAST(json_extract(permissions, '$.view_storm')             AS INTEGER),
    manage_storm           = CAST(json_extract(permissions, '$.manage_storm')           AS INTEGER),
    view_vs_points         = CAST(json_extract(permissions, '$.view_vs_points')         AS INTEGER),
    manage_vs_points       = CAST(json_extract(permissions, '$.manage_vs_points')       AS INTEGER),
    view_upload            = CAST(json_extract(permissions, '$.view_upload')            AS INTEGER),
    manage_members         = CAST(json_extract(permissions, '$.manage_members')         AS INTEGER),
    manage_settings        = CAST(json_extract(permissions, '$.manage_settings')        AS INTEGER),
    view_files             = CAST(json_extract(permissions, '$.view_files')             AS INTEGER),
    manage_files           = CAST(json_extract(permissions, '$.manage_files')           AS INTEGER),
    upload_files           = CAST(json_extract(permissions, '$.upload_files')           AS INTEGER),
    view_anonymous_authors = CAST(json_extract(permissions, '$.view_anonymous_authors') AS INTEGER),
    view_schedule          = CAST(json_extract(permissions, '$.view_schedule')          AS INTEGER),
    manage_schedule        = CAST(json_extract(permissions, '$.manage_schedule')        AS INTEGER),
    view_officer_command   = CAST(json_extract(permissions, '$.view_officer_command')   AS INTEGER),
    manage_officer_command = CAST(json_extract(permissions, '$.manage_officer_command') AS INTEGER),
    view_recruiting        = CAST(json_extract(permissions, '$.view_recruiting')        AS INTEGER),
    manage_recruiting      = CAST(json_extract(permissions, '$.manage_recruiting')      AS INTEGER),
    view_allies            = CAST(json_extract(permissions, '$.view_allies')            AS INTEGER),
    manage_allies          = CAST(json_extract(permissions, '$.manage_allies')          AS INTEGER),
    view_activity          = CAST(json_extract(permissions, '$.view_activity')          AS INTEGER),
    view_accountability    = CAST(json_extract(permissions, '$.view_accountability')    AS INTEGER),
    manage_accountability  = CAST(json_extract(permissions, '$.manage_accountability')  AS INTEGER),
    view_season_hub        = CAST(json_extract(permissions, '$.view_season_hub')        AS INTEGER),
    manage_season_hub      = CAST(json_extract(permissions, '$.manage_season_hub')      AS INTEGER),
    manage_season_rewards  = CAST(json_extract(permissions, '$.manage_season_rewards')  AS INTEGER),
    view_comms             = CAST(json_extract(permissions, '$.view_comms')             AS INTEGER),
    manage_comms           = CAST(json_extract(permissions, '$.manage_comms')           AS INTEGER);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE rank_permissions DROP COLUMN permissions;
-- +goose StatementEnd
