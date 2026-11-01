-- +goose Up

-- Weeks that don't count (Project 21, private-docs 283; owner, 2026-10-22). The season's last
-- week is when rewards are given and everything is finalised, so it is not a week anyone is
-- scored for. A season lists the weeks its standings leave out; the status engine skips them
-- (computeSeasonStanding), so Rankings, the dashboard, eligibility and the report agree.
ALTER TABLE seasons ADD COLUMN uncounted_weeks TEXT NOT NULL DEFAULT '[]';

-- Archived eight-week seasons whose week 8 is genuinely empty (no member was scored for it)
-- leave it out. A season that did score week 8 keeps counting it: that install's officers
-- treated it as a scored week, and its archived percentages must not move.
UPDATE seasons SET uncounted_weeks = '[8]'
WHERE COALESCE(archived_at, '') != '' AND week_count = 8
  AND NOT EXISTS (SELECT 1 FROM season_participation p WHERE p.season_id = seasons.id AND p.week_number = 8);

-- New seasons from a shipped template start with week 8 left out. 088's existing-install rule:
-- a template's defaults change only while they still read as shipped.
UPDATE season_templates SET defaults = '{"week_count":8,"key_event_name":"City Clash","key_event_required":0,"key_event_total":0,"resource_name":"","uncounted_weeks":[8]}'
WHERE season_number = 1
  AND CASE WHEN json_valid(defaults) THEN json(defaults) = json('{"week_count":8,"key_event_name":"City Clash","key_event_required":0,"key_event_total":0,"resource_name":""}') ELSE 0 END;
UPDATE season_templates SET defaults = '{"week_count":8,"key_event_name":"Rare Soil War","key_event_required":4,"key_event_total":8,"resource_name":"","uncounted_weeks":[8]}'
WHERE season_number = 2
  AND CASE WHEN json_valid(defaults) THEN json(defaults) = json('{"week_count":8,"key_event_name":"Rare Soil War","key_event_required":4,"key_event_total":8,"resource_name":""}') ELSE 0 END;
UPDATE season_templates SET defaults = '{"week_count":8,"key_event_name":"Goldust Conquest","key_event_required":4,"key_event_total":8,"resource_name":"","uncounted_weeks":[8]}'
WHERE season_number = 3
  AND CASE WHEN json_valid(defaults) THEN json(defaults) = json('{"week_count":8,"key_event_name":"Goldust Conquest","key_event_required":4,"key_event_total":8,"resource_name":""}') ELSE 0 END;
UPDATE season_templates SET defaults = '{"week_count":8,"key_event_name":"Copper War","key_event_required":4,"key_event_total":8,"resource_name":"Copper","uncounted_weeks":[8]}'
WHERE season_number = 4
  AND CASE WHEN json_valid(defaults) THEN json(defaults) = json('{"week_count":8,"key_event_name":"Copper War","key_event_required":4,"key_event_total":8,"resource_name":"Copper"}') ELSE 0 END;
UPDATE season_templates SET defaults = '{"week_count":8,"key_event_name":"Finance Tycoon / Bank Capture","key_event_required":2,"key_event_total":0,"resource_name":"","uncounted_weeks":[8]}'
WHERE season_number = 5
  AND CASE WHEN json_valid(defaults) THEN json(defaults) = json('{"week_count":8,"key_event_name":"Finance Tycoon / Bank Capture","key_event_required":2,"key_event_total":0,"resource_name":""}') ELSE 0 END;
UPDATE season_templates SET defaults = '{"week_count":8,"key_event_name":"Faction Clash","key_event_required":4,"key_event_total":12,"resource_name":"","uncounted_weeks":[8]}'
WHERE season_number = 6
  AND CASE WHEN json_valid(defaults) THEN json(defaults) = json('{"week_count":8,"key_event_name":"Faction Clash","key_event_required":4,"key_event_total":12,"resource_name":""}') ELSE 0 END;

-- +goose Down
UPDATE season_templates SET defaults = '{"week_count":8,"key_event_name":"City Clash","key_event_required":0,"key_event_total":0,"resource_name":""}'
WHERE season_number = 1
  AND CASE WHEN json_valid(defaults) THEN json(defaults) = json('{"week_count":8,"key_event_name":"City Clash","key_event_required":0,"key_event_total":0,"resource_name":"","uncounted_weeks":[8]}') ELSE 0 END;
UPDATE season_templates SET defaults = '{"week_count":8,"key_event_name":"Rare Soil War","key_event_required":4,"key_event_total":8,"resource_name":""}'
WHERE season_number = 2
  AND CASE WHEN json_valid(defaults) THEN json(defaults) = json('{"week_count":8,"key_event_name":"Rare Soil War","key_event_required":4,"key_event_total":8,"resource_name":"","uncounted_weeks":[8]}') ELSE 0 END;
UPDATE season_templates SET defaults = '{"week_count":8,"key_event_name":"Goldust Conquest","key_event_required":4,"key_event_total":8,"resource_name":""}'
WHERE season_number = 3
  AND CASE WHEN json_valid(defaults) THEN json(defaults) = json('{"week_count":8,"key_event_name":"Goldust Conquest","key_event_required":4,"key_event_total":8,"resource_name":"","uncounted_weeks":[8]}') ELSE 0 END;
UPDATE season_templates SET defaults = '{"week_count":8,"key_event_name":"Copper War","key_event_required":4,"key_event_total":8,"resource_name":"Copper"}'
WHERE season_number = 4
  AND CASE WHEN json_valid(defaults) THEN json(defaults) = json('{"week_count":8,"key_event_name":"Copper War","key_event_required":4,"key_event_total":8,"resource_name":"Copper","uncounted_weeks":[8]}') ELSE 0 END;
UPDATE season_templates SET defaults = '{"week_count":8,"key_event_name":"Finance Tycoon / Bank Capture","key_event_required":2,"key_event_total":0,"resource_name":""}'
WHERE season_number = 5
  AND CASE WHEN json_valid(defaults) THEN json(defaults) = json('{"week_count":8,"key_event_name":"Finance Tycoon / Bank Capture","key_event_required":2,"key_event_total":0,"resource_name":"","uncounted_weeks":[8]}') ELSE 0 END;
UPDATE season_templates SET defaults = '{"week_count":8,"key_event_name":"Faction Clash","key_event_required":4,"key_event_total":12,"resource_name":""}'
WHERE season_number = 6
  AND CASE WHEN json_valid(defaults) THEN json(defaults) = json('{"week_count":8,"key_event_name":"Faction Clash","key_event_required":4,"key_event_total":12,"resource_name":"","uncounted_weeks":[8]}') ELSE 0 END;
ALTER TABLE seasons DROP COLUMN uncounted_weeks;
