-- +goose Up
-- +goose StatementBegin

-- A comms template carrying a `slug` is fetched BY NAME by some other part of the
-- app (`ds_battle_mail` by the Desert Storm planner's Battle Mail button). Until
-- now nothing stopped an officer deleting one: the row went, the fetch started
-- 404ing, and there was no way back — `slug` is seed-only, so a re-created
-- template cannot be given one through the UI.
--
-- handleCommsTemplateDelete now refuses with 409, which stops the NEXT deletion.
-- An install that already deleted the row still has nothing to fetch, so re-seed
-- it here. This is the "only a migration can restore it" path.
--
-- Deliberately `INSERT … WHERE NOT EXISTS`: idempotent, and it never touches an
-- install whose row is present but edited. The content is migration 041's
-- skeleton verbatim — the placeholder text, not any alliance's strategy. An
-- install restored by this statement gets the blank form back and fills it in,
-- which is what a template is for.
INSERT INTO comms_templates (type, title, category, slug, required_vars, content)
SELECT 'mail', 'DS Battle Strategy Mail', 'Desert Storm', 'ds_battle_mail',
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
WHERE NOT EXISTS (SELECT 1 FROM comms_templates WHERE slug = 'ds_battle_mail');

-- +goose StatementEnd
