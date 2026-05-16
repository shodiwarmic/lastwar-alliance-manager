-- +goose Up
-- +goose StatementBegin

CREATE TABLE comms_templates (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    type          TEXT NOT NULL CHECK(type IN ('mail','announcement')),
    title         TEXT NOT NULL,
    category      TEXT NOT NULL DEFAULT 'General',
    content       TEXT NOT NULL DEFAULT '',
    season_id     INTEGER REFERENCES seasons(id),
    slug          TEXT UNIQUE,
    required_vars TEXT NOT NULL DEFAULT '[]',
    created_by    INTEGER REFERENCES users(id),
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO comms_templates (type, title, category, content, season_id, created_by, created_at, updated_at)
SELECT 'mail', sm.title,
    COALESCE('Season ' || s.season_number, 'Season Mail'),
    sm.content, sm.season_id, sm.posted_by, sm.posted_at, sm.posted_at
FROM season_mail sm
LEFT JOIN seasons s ON s.id = sm.season_id;

INSERT INTO comms_templates (type, title, category, slug, required_vars, content) VALUES (
    'mail', 'DS Battle Strategy Mail', 'Desert Storm', 'ds_battle_mail',
    '["task_force","battle_time","group_assignments"]',
    '🏜️ DESERT STORM — {task_force}
Battle Time: {battle_time}

STAGE 1 (0–10 min):
[Edit stage 1 strategy here]

STAGE 2 (10–30 min):
[Edit stage 2 strategy here]

TACTICAL TIPS:
[Edit tactical tips here]

GROUP ASSIGNMENTS:
{group_assignments}

LET''S WIN THIS 🔥'
);

DROP TABLE season_mail;

CREATE TABLE comms_resources (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    title       TEXT NOT NULL,
    url         TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_by  INTEGER REFERENCES users(id),
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE rank_permissions ADD COLUMN view_comms   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE rank_permissions ADD COLUMN manage_comms INTEGER NOT NULL DEFAULT 0;

UPDATE rank_permissions SET view_comms = 1, manage_comms = 1 WHERE rank IN ('R4','R5');
UPDATE rank_permissions SET view_comms = 1 WHERE rank IN ('R3');

-- +goose StatementEnd
