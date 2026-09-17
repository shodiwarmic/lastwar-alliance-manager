-- +goose Up

-- The nightly announcement was assembled by hand from the schedule page: read the
-- day's events off the grid, retype them into a message, paste it into the game.
-- One button now prints what the model already knows.

-- Whether a type appears in the announcement at all.
--
-- Defaults to 1 for EVERY existing type and every new one. A forgotten tick means
-- an event silently missing from an alliance-wide post — a failure nobody sees
-- until it has already happened — whereas opting out is a visible choice made in
-- the modal. The dropped-event line under the button covers the other direction.
-- +goose StatementBegin
ALTER TABLE schedule_event_types ADD COLUMN announce INTEGER NOT NULL DEFAULT 1;
-- +goose StatementEnd

-- The window an announcement covers, in server time. The default spans a whole
-- game day, so an install that never touches it gets "everything dated today".
--
-- end <= start is LEGAL and means the window wraps past midnight: the alliance
-- that prompted this posts at 18:00 for the night ahead, which runs into the next
-- game day's early hours.
-- +goose StatementBegin
ALTER TABLE settings ADD COLUMN announce_window_start TEXT NOT NULL DEFAULT '00:00';
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE settings ADD COLUMN announce_window_end TEXT NOT NULL DEFAULT '23:59';
-- +goose StatementEnd

-- The template the button fills in. Slugged, so the app can fetch it by name —
-- and so migration 072's guard keeps it from being deleted.
--
-- required_vars lists all three, because that is the display metadata the variable
-- chips read and the officer should see what is available. The seeded CONTENT
-- deliberately uses only {events}: the starred-mission lines are an option, not an
-- obligation, and a template that shipped using them would put a server list in
-- every alliance's nightly post whether or not they track that.
-- +goose StatementBegin
INSERT INTO comms_templates (type, title, category, slug, required_vars, content)
SELECT 'announcement', 'Daily events', 'Schedule', 'nightly_events',
       '["events","starred_today","starred_tomorrow"]',
       'Today''s events' || char(10) || char(10) || '{events}'
WHERE NOT EXISTS (SELECT 1 FROM comms_templates WHERE slug = 'nightly_events');
-- +goose StatementEnd
