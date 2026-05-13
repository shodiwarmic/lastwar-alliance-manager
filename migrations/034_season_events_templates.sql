-- +goose Up
-- +goose StatementBegin

-- Recurring event templates for a season, pushable to schedule_events or
-- server_events depending on the is_server_event flag (added in 038).
--
-- day_offset: 1=Mon, 2=Tue, 3=Wed, 4=Thu, 5=Fri, 6=Sat, 7=Sun, NULL=unscheduled.
-- Seasons always start on Monday, so day_offset maps directly to day-of-week.
-- Unscheduled events appear in the UI but are skipped during schedule push.
--
-- week_start may be 0 or negative for pre-season events (e.g. S6 Faction Awards
-- at week_start=0 = the Monday one week before the season starts).
-- week_end < week_start is the "open-ended" sentinel meaning "run through
-- season.week_count" (so week_start=1, week_end=0 still means weeks 1..N).
-- week_end may exceed week_count for post-season events (e.g. Age of Oil
-- runs on weeks 9-13 anchored to Season 2's start date).
CREATE TABLE season_events (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    season_id     INTEGER NOT NULL REFERENCES seasons(id) ON DELETE CASCADE,
    label         TEXT    NOT NULL,
    event_type_id INTEGER REFERENCES schedule_event_types(id) ON DELETE SET NULL,
    day_offset    INTEGER DEFAULT NULL,
    event_time    TEXT    NOT NULL DEFAULT '20:00',
    all_day       INTEGER NOT NULL DEFAULT 0,
    level         INTEGER,
    week_start    INTEGER NOT NULL DEFAULT 1,
    week_end      INTEGER NOT NULL DEFAULT 0,
    notes         TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX idx_sev_season ON season_events(season_id);

-- Pre-defined season templates for quick season creation.
-- trackables JSON: [{key, label, sort_order}]
-- defaults  JSON: {week_count, key_event_name, key_event_required}
-- events    JSON: [{label, type_name, type_short, type_icon, day_offset,
--                   event_time, week_start, week_end, level?, notes,
--                   is_server_event, duration_days}]
CREATE TABLE season_templates (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    template_name TEXT    NOT NULL,
    season_number INTEGER NOT NULL,
    trackables    TEXT    NOT NULL DEFAULT '[]',
    defaults      TEXT    NOT NULL DEFAULT '{}',
    events        TEXT    NOT NULL DEFAULT '[]',
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- S1 — The Crimson Plague (⚠️ approximate — completely different contribution architecture; no factions)
INSERT INTO season_templates (template_name, season_number, trackables, defaults, events) VALUES (
    'The Crimson Plague',
    1,
    '[{"key":"city_captures","label":"City Captures","sort_order":0},{"key":"warzone_donations","label":"Warzone Donations","sort_order":1}]',
    '{"week_count":8,"key_event_name":"City Clash","key_event_required":0}',
    '[
      {"label":"City Clash (L1)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":1,"week_end":1,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L2)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":1,"week_end":1,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Exclusive Weapon: Kimberly","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":1,"week_end":1,"is_server_event":true,"duration_days":7,"notes":""},
      {"label":"City Clash (L3)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":2,"week_end":2,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L4)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":2,"week_end":2,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L5)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":3,"week_end":3,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L6)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":3,"week_end":3,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Exclusive Weapon: DVA","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":3,"week_end":3,"is_server_event":true,"duration_days":7,"notes":""},
      {"label":"Infinite Octagon (Capitol War)","type_name":"Infinite Octagon","type_short":"IO","type_icon":"🔄","day_offset":6,"event_time":"12:00","week_start":5,"week_end":7,"is_server_event":true,"duration_days":1,"notes":""},
      {"label":"Exclusive Weapon: Tesla","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":6,"week_end":6,"is_server_event":true,"duration_days":7,"notes":""}
    ]'
);

-- S2 — Polar World (✅ confirmed; includes Age of Oil post-season weeks 9-13)
INSERT INTO season_templates (template_name, season_number, trackables, defaults, events) VALUES (
    'Polar World',
    2,
    '[{"key":"mutual_assistance","label":"Mutual Assistance","sort_order":0},{"key":"siege","label":"Siege","sort_order":1},{"key":"rare_soil_war","label":"Rare Soil War","sort_order":2},{"key":"defeat","label":"Defeat","sort_order":3}]',
    '{"week_count":8,"key_event_name":"Rare Soil War","key_event_required":4}',
    '[
      {"label":"City Clash (L1)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":1,"week_end":1,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L2)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":1,"week_end":1,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Exclusive Weapon: Murphy (Tank)","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":1,"week_end":1,"is_server_event":true,"duration_days":7,"notes":""},
      {"label":"City Clash (L3)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":2,"week_end":2,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L4)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":2,"week_end":2,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Exclusive Weapon: Carlie (Aircraft)","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":3,"week_end":3,"is_server_event":true,"duration_days":7,"notes":""},
      {"label":"Faction Awards","type_name":"Faction Awards","type_short":"FA","type_icon":"🏆","day_offset":1,"event_time":"00:00","week_start":3,"week_end":3,"is_server_event":true,"duration_days":1,"notes":""},
      {"label":"City Clash (L5 — Launch Sites)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":3,"week_end":3,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L6 — War Palaces)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":3,"week_end":3,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Rare Soil War","type_name":"Rare Soil War","type_short":"RSW","type_icon":"🌾","day_offset":3,"event_time":"12:00","week_start":4,"week_end":6,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Rare Soil War","type_name":"Rare Soil War","type_short":"RSW","type_icon":"🌾","day_offset":6,"event_time":"12:00","week_start":4,"week_end":6,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Exclusive Weapon: Swift (Missile)","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":6,"week_end":6,"is_server_event":true,"duration_days":7,"notes":""},
      {"label":"Rare Soil Showdown (30% plunder)","type_name":"Rare Soil War","type_short":"RSW","type_icon":"🌾","day_offset":3,"event_time":"12:00","week_start":7,"week_end":7,"is_server_event":false,"duration_days":1,"notes":"30% plunder"},
      {"label":"Rare Soil Showdown (30% plunder)","type_name":"Rare Soil War","type_short":"RSW","type_icon":"🌾","day_offset":6,"event_time":"12:00","week_start":7,"week_end":7,"is_server_event":false,"duration_days":1,"notes":"30% plunder"},
      {"label":"Age of Oil Begins","type_name":"Age of Oil","type_short":"AoO","type_icon":"🛢️","day_offset":1,"event_time":"00:00","week_start":9,"week_end":9,"is_server_event":true,"duration_days":1,"notes":"Post-season phase begins"},
      {"label":"Black Market / Bingo / Champion Duel","type_name":"Champion Duel","type_short":"CD","type_icon":"🥊","day_offset":1,"event_time":"00:00","week_start":10,"week_end":10,"is_server_event":true,"duration_days":1,"notes":""},
      {"label":"Transfer Surge","type_name":"Transfer Surge","type_short":"TS","type_icon":"🔀","day_offset":4,"event_time":"00:00","week_start":10,"week_end":10,"is_server_event":true,"duration_days":1,"notes":""},
      {"label":"Champion Duel (ongoing)","type_name":"Champion Duel","type_short":"CD","type_icon":"🥊","day_offset":null,"event_time":"00:00","week_start":10,"week_end":13,"is_server_event":true,"duration_days":28,"notes":"28-day window"}
    ]'
);

-- S3 — Golden Realm (⚠️ partially confirmed)
INSERT INTO season_templates (template_name, season_number, trackables, defaults, events) VALUES (
    'Golden Realm',
    3,
    '[{"key":"mutual_assistance","label":"Mutual Assistance","sort_order":0},{"key":"siege","label":"Siege","sort_order":1},{"key":"spice_war","label":"Spice War","sort_order":2},{"key":"defeat","label":"Defeat","sort_order":3}]',
    '{"week_count":8,"key_event_name":"Spice War","key_event_required":4}',
    '[
      {"label":"City Clash (L1)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":1,"week_end":1,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L2)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":1,"week_end":1,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Exclusive Weapon: Marshall (Tank)","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":1,"week_end":1,"is_server_event":true,"duration_days":7,"notes":""},
      {"label":"City Clash (L3)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":2,"week_end":2,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L4)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":2,"week_end":2,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Exclusive Weapon: Schuyler (Aircraft)","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":3,"week_end":3,"is_server_event":true,"duration_days":7,"notes":""},
      {"label":"Faction Awards","type_name":"Faction Awards","type_short":"FA","type_icon":"🏆","day_offset":1,"event_time":"00:00","week_start":3,"week_end":3,"is_server_event":true,"duration_days":1,"notes":""},
      {"label":"City Clash (L5)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":3,"week_end":3,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L6)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":3,"week_end":3,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Spice War","type_name":"Spice War","type_short":"SPW","type_icon":"🌶️","day_offset":3,"event_time":"12:00","week_start":4,"week_end":7,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Spice War","type_name":"Spice War","type_short":"SPW","type_icon":"🌶️","day_offset":6,"event_time":"12:00","week_start":4,"week_end":7,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Exclusive Weapon: McGregor (Missile)","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":6,"week_end":6,"is_server_event":true,"duration_days":7,"notes":""},
      {"label":"Faction Duel — Invasion Right Contest","type_name":"Faction Duel","type_short":"FD","type_icon":"⚔️","day_offset":1,"event_time":"00:00","week_start":7,"week_end":7,"is_server_event":true,"duration_days":1,"notes":"Ranking phase Mon–Fri"},
      {"label":"Faction Duel — Capitol War","type_name":"Faction Duel","type_short":"FD","type_icon":"⚔️","day_offset":7,"event_time":"12:00","week_start":7,"week_end":7,"is_server_event":true,"duration_days":1,"notes":""}
    ]'
);

-- S4 — Evernight Isle (⚠️ partially confirmed)
INSERT INTO season_templates (template_name, season_number, trackables, defaults, events) VALUES (
    'Evernight Isle',
    4,
    '[{"key":"mutual_assistance","label":"Mutual Assistance","sort_order":0},{"key":"siege","label":"Siege","sort_order":1},{"key":"copper_war","label":"Copper War","sort_order":2},{"key":"defeat","label":"Defeat","sort_order":3}]',
    '{"week_count":8,"key_event_name":"Copper War","key_event_required":4}',
    '[
      {"label":"City Clash (L1)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":1,"week_end":1,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L2)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":1,"week_end":1,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Exclusive Weapon: Williams (Tank)","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":1,"week_end":1,"is_server_event":true,"duration_days":7,"notes":""},
      {"label":"Blood Night Descend (3x daily)","type_name":"Blood Night","type_short":"BN","type_icon":"🩸","day_offset":null,"event_time":"02:30","week_start":1,"week_end":0,"is_server_event":true,"duration_days":1,"notes":"3× daily at 02:30 / 10:30 / 18:30 server time"},
      {"label":"City Clash (L3)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":2,"week_end":2,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L4)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":2,"week_end":2,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Exclusive Weapon: Lucius (Aircraft)","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":3,"week_end":3,"is_server_event":true,"duration_days":7,"notes":""},
      {"label":"Faction Awards","type_name":"Faction Awards","type_short":"FA","type_icon":"🏆","day_offset":1,"event_time":"00:00","week_start":3,"week_end":3,"is_server_event":true,"duration_days":1,"notes":""},
      {"label":"City Clash (L5)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":3,"week_end":3,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L6)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":3,"week_end":3,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Copper War","type_name":"Copper War","type_short":"CPW","type_icon":"🥉","day_offset":3,"event_time":"12:00","week_start":4,"week_end":6,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Copper War","type_name":"Copper War","type_short":"CPW","type_icon":"🥉","day_offset":6,"event_time":"12:00","week_start":4,"week_end":6,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Exclusive Weapon: Adam (Missile)","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":6,"week_end":6,"is_server_event":true,"duration_days":7,"notes":""},
      {"label":"Copper War Finals","type_name":"Copper War","type_short":"CPW","type_icon":"🥉","day_offset":6,"event_time":"12:00","week_start":7,"week_end":7,"is_server_event":false,"duration_days":1,"notes":"Finals"}
    ]'
);

-- S5 — Wild West (✅ confirmed; no factions)
INSERT INTO season_templates (template_name, season_number, trackables, defaults, events) VALUES (
    'Wild West',
    5,
    '[{"key":"crystal_gold","label":"CrystalGold","sort_order":0},{"key":"capture","label":"Capture","sort_order":1},{"key":"kills","label":"Kills","sort_order":2}]',
    '{"week_count":8,"key_event_name":"Finance Tycoon / Bank Capture","key_event_required":2}',
    '[
      {"label":"City Clash (L1)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":1,"week_end":1,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L2)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":1,"week_end":1,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Exclusive Weapon: Fiona (Missile)","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":1,"week_end":1,"is_server_event":true,"duration_days":7,"notes":""},
      {"label":"High Noon / Shooting Duel (daily)","type_name":"High Noon","type_short":"HN","type_icon":"🌞","day_offset":null,"event_time":"00:00","week_start":1,"week_end":0,"is_server_event":true,"duration_days":1,"notes":"Daily throughout season"},
      {"label":"City Clash (L3)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":2,"week_end":2,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L4)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":2,"week_end":2,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Wasteland Trade Train (every 4h)","type_name":"Trade Train","type_short":"TTR","type_icon":"🚂","day_offset":null,"event_time":"00:00","week_start":2,"week_end":0,"is_server_event":true,"duration_days":1,"notes":"Every 4 hours: 00:00 / 04:00 / 08:00 / 12:00 / 16:00 / 20:00"},
      {"label":"Exclusive Weapon: Stetmann (Tank)","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":3,"week_end":3,"is_server_event":true,"duration_days":7,"notes":""},
      {"label":"City Clash (L5)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":3,"week_end":3,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L6)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":3,"week_end":3,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Finance Tycoon / Bank Capture","type_name":"Bank Capture","type_short":"BC","type_icon":"🏦","day_offset":3,"event_time":"12:00","week_start":4,"week_end":7,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Finance Tycoon / Bank Capture","type_name":"Bank Capture","type_short":"BC","type_icon":"🏦","day_offset":6,"event_time":"12:00","week_start":4,"week_end":7,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Exclusive Weapon: Morrison (Aircraft)","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":6,"week_end":6,"is_server_event":true,"duration_days":7,"notes":""},
      {"label":"Golden Palace Opens","type_name":"Bank Capture","type_short":"BC","type_icon":"🏦","day_offset":6,"event_time":"13:00","week_start":7,"week_end":7,"is_server_event":false,"duration_days":1,"notes":"Golden Palace unlocks"}
    ]'
);

-- S6 — Lost Rainforest / Shadow Rainforest (✅ confirmed)
INSERT INTO season_templates (template_name, season_number, trackables, defaults, events) VALUES (
    'Lost Rainforest',
    6,
    '[{"key":"war_merit","label":"War Merit","sort_order":0},{"key":"capture","label":"Capture","sort_order":1},{"key":"defeat","label":"Defeat","sort_order":2}]',
    '{"week_count":8,"key_event_name":"Faction Clash","key_event_required":4}',
    '[
      {"label":"Faction Awards","type_name":"Faction Awards","type_short":"FA","type_icon":"🏆","day_offset":1,"event_time":"00:00","week_start":0,"week_end":0,"is_server_event":true,"duration_days":1,"notes":"Pre-season Week 2 (week before season start)"},
      {"label":"City Clash (L1)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":1,"week_end":1,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L2)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":1,"week_end":1,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Exclusive Weapon: Kimberly Awakening","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":1,"week_end":1,"is_server_event":true,"duration_days":7,"notes":""},
      {"label":"City Clash (L3)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":2,"week_end":2,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L4)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":2,"week_end":2,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Global Expedition Round 1 (14 days)","type_name":"Global Expedition","type_short":"GE","type_icon":"🌍","day_offset":1,"event_time":"00:00","week_start":2,"week_end":2,"is_server_event":true,"duration_days":14,"notes":"14-day round"},
      {"label":"Altar Conquest","type_name":"Altar Conquest","type_short":"ALT","type_icon":"🗿","day_offset":2,"event_time":"12:00","week_start":2,"week_end":7,"is_server_event":false,"duration_days":1,"notes":"1-hour window"},
      {"label":"Beneath the Ruins PvP","type_name":"Beneath the Ruins","type_short":"BTR","type_icon":"🏚️","day_offset":null,"event_time":"00:00","week_start":2,"week_end":7,"is_server_event":true,"duration_days":1,"notes":"Unscheduled"},
      {"label":"City Clash (L5)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":3,"week_end":3,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"City Clash (L6)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":3,"week_end":3,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Faction Clash","type_name":"Faction Clash","type_short":"FCL","type_icon":"🏹","day_offset":3,"event_time":"12:00","week_start":3,"week_end":7,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Faction Clash","type_name":"Faction Clash","type_short":"FCL","type_icon":"🏹","day_offset":6,"event_time":"12:00","week_start":3,"week_end":7,"is_server_event":false,"duration_days":1,"notes":""},
      {"label":"Global Expedition Round 2 (14 days)","type_name":"Global Expedition","type_short":"GE","type_icon":"🌍","day_offset":1,"event_time":"00:00","week_start":4,"week_end":4,"is_server_event":true,"duration_days":14,"notes":"14-day round"},
      {"label":"Global Expedition Round 3 (14 days)","type_name":"Global Expedition","type_short":"GE","type_icon":"🌍","day_offset":1,"event_time":"00:00","week_start":6,"week_end":6,"is_server_event":true,"duration_days":14,"notes":"14-day round"},
      {"label":"Faction Duel (Final)","type_name":"Faction Duel","type_short":"FD","type_icon":"⚔️","day_offset":7,"event_time":"12:00","week_start":7,"week_end":7,"is_server_event":true,"duration_days":1,"notes":""}
    ]'
);

-- +goose StatementEnd
