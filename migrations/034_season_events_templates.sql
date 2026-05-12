-- +goose Up
-- +goose StatementBegin

-- Recurring event templates for a season, pushable to schedule_events.
--
-- day_offset: 1=Mon, 2=Tue, 3=Wed, 4=Thu, 5=Fri, 6=Sat, 7=Sun, NULL=unscheduled.
-- Seasons always start on Monday, so day_offset maps directly to day-of-week.
-- Unscheduled events appear in the UI but are skipped during schedule push.
--
-- week_end=0 means season.week_count. week_end may exceed week_count for post-season
-- events (e.g. Age of Oil runs on weeks 9-13 anchored to Season 2's start date).
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
-- events    JSON: [{label, day_offset, event_time, week_start, week_end, level, notes}]
CREATE TABLE season_templates (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    template_name TEXT    NOT NULL,
    season_number INTEGER NOT NULL,
    trackables    TEXT    NOT NULL DEFAULT '[]',
    defaults      TEXT    NOT NULL DEFAULT '{}',
    events        TEXT    NOT NULL DEFAULT '[]',
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- S1 — The Crimson Plague (⚠️ approximate — completely different contribution architecture)
INSERT INTO season_templates (template_name, season_number, trackables, defaults, events) VALUES (
    'The Crimson Plague',
    1,
    '[{"key":"city_captures","label":"City Captures","sort_order":0},{"key":"warzone_donations","label":"Warzone Donations","sort_order":1}]',
    '{"week_count":8,"key_event_name":"City Clash","key_event_required":0}',
    '[
      {"label":"City Clash (L1)","day_offset":3,"event_time":"20:00","week_start":1,"week_end":1,"notes":""},
      {"label":"City Clash (L2)","day_offset":6,"event_time":"20:00","week_start":1,"week_end":1,"notes":""},
      {"label":"Exclusive Weapon: Kimberly","day_offset":4,"event_time":"20:00","week_start":1,"week_end":1,"notes":""},
      {"label":"City Clash (L3)","day_offset":3,"event_time":"20:00","week_start":2,"week_end":2,"notes":""},
      {"label":"City Clash (L4)","day_offset":6,"event_time":"20:00","week_start":2,"week_end":2,"notes":""},
      {"label":"City Clash (L5)","day_offset":3,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"City Clash (L6)","day_offset":6,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"Exclusive Weapon: DVA","day_offset":4,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"Faction Awards","day_offset":7,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"Infinite Octagon (Capitol War)","day_offset":6,"event_time":"20:00","week_start":5,"week_end":7,"notes":""},
      {"label":"Exclusive Weapon: Tesla","day_offset":4,"event_time":"20:00","week_start":6,"week_end":6,"notes":""}
    ]'
);

-- S2 — Polar World (✅ confirmed; includes Age of Oil post-season weeks 9-13)
INSERT INTO season_templates (template_name, season_number, trackables, defaults, events) VALUES (
    'Polar World',
    2,
    '[{"key":"mutual_assistance","label":"Mutual Assistance","sort_order":0},{"key":"siege","label":"Siege","sort_order":1},{"key":"rare_soil_war","label":"Rare Soil War","sort_order":2},{"key":"defeat","label":"Defeat","sort_order":3}]',
    '{"week_count":8,"key_event_name":"Rare Soil War","key_event_required":4}',
    '[
      {"label":"City Clash (L1)","day_offset":3,"event_time":"20:00","week_start":1,"week_end":1,"notes":""},
      {"label":"City Clash (L2)","day_offset":6,"event_time":"20:00","week_start":1,"week_end":1,"notes":""},
      {"label":"Exclusive Weapon: Murphy (Tank)","day_offset":4,"event_time":"20:00","week_start":1,"week_end":1,"notes":""},
      {"label":"City Clash (L3)","day_offset":3,"event_time":"20:00","week_start":2,"week_end":2,"notes":""},
      {"label":"City Clash (L4)","day_offset":6,"event_time":"20:00","week_start":2,"week_end":2,"notes":""},
      {"label":"Exclusive Weapon: Carlie (Aircraft)","day_offset":4,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"Faction Awards","day_offset":7,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"City Clash (L5 — Launch Sites)","day_offset":3,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"City Clash (L6 — War Palaces)","day_offset":6,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"Rare Soil War","day_offset":3,"event_time":"20:00","week_start":4,"week_end":6,"notes":""},
      {"label":"Rare Soil War","day_offset":6,"event_time":"20:00","week_start":4,"week_end":6,"notes":""},
      {"label":"Exclusive Weapon: Swift (Missile)","day_offset":4,"event_time":"20:00","week_start":6,"week_end":6,"notes":""},
      {"label":"Rare Soil Showdown (30% plunder)","day_offset":3,"event_time":"20:00","week_start":7,"week_end":7,"notes":"30% plunder"},
      {"label":"Rare Soil Showdown (30% plunder)","day_offset":6,"event_time":"20:00","week_start":7,"week_end":7,"notes":"30% plunder"},
      {"label":"Age of Oil Begins","day_offset":1,"event_time":"20:00","week_start":9,"week_end":9,"notes":"Post-season"},
      {"label":"Black Market / Bingo / Champion Duel","day_offset":1,"event_time":"20:00","week_start":10,"week_end":10,"notes":"Post-season"},
      {"label":"Transfer Surge","day_offset":4,"event_time":"20:00","week_start":10,"week_end":10,"notes":"Post-season"},
      {"label":"Champion Duel (ongoing)","day_offset":null,"event_time":"20:00","week_start":10,"week_end":13,"notes":"28-day window, unscheduled"}
    ]'
);

-- S3 — Golden Realm (⚠️ partially confirmed)
INSERT INTO season_templates (template_name, season_number, trackables, defaults, events) VALUES (
    'Golden Realm',
    3,
    '[{"key":"mutual_assistance","label":"Mutual Assistance","sort_order":0},{"key":"siege","label":"Siege","sort_order":1},{"key":"spice_war","label":"Spice War","sort_order":2},{"key":"defeat","label":"Defeat","sort_order":3}]',
    '{"week_count":8,"key_event_name":"Spice War","key_event_required":4}',
    '[
      {"label":"City Clash (L1)","day_offset":3,"event_time":"20:00","week_start":1,"week_end":1,"notes":""},
      {"label":"City Clash (L2)","day_offset":6,"event_time":"20:00","week_start":1,"week_end":1,"notes":""},
      {"label":"Exclusive Weapon: Marshall (Aircraft)","day_offset":4,"event_time":"20:00","week_start":1,"week_end":1,"notes":""},
      {"label":"City Clash (L3)","day_offset":3,"event_time":"20:00","week_start":2,"week_end":2,"notes":""},
      {"label":"City Clash (L4)","day_offset":6,"event_time":"20:00","week_start":2,"week_end":2,"notes":""},
      {"label":"Exclusive Weapon: Schuyler (Tank)","day_offset":4,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"Faction Awards","day_offset":7,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"City Clash (L5)","day_offset":3,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"City Clash (L6)","day_offset":6,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"Spice War","day_offset":3,"event_time":"20:00","week_start":4,"week_end":7,"notes":""},
      {"label":"Spice War","day_offset":6,"event_time":"20:00","week_start":4,"week_end":7,"notes":""},
      {"label":"Exclusive Weapon: McGregor (Missile)","day_offset":4,"event_time":"20:00","week_start":6,"week_end":6,"notes":""},
      {"label":"Faction Duel — Invasion Right Contest","day_offset":1,"event_time":"20:00","week_start":7,"week_end":7,"notes":"Spans Days 1-6"},
      {"label":"Faction Duel — Capitol War","day_offset":7,"event_time":"20:00","week_start":7,"week_end":7,"notes":""}
    ]'
);

-- S4 — Evernight Isle (⚠️ partially confirmed)
INSERT INTO season_templates (template_name, season_number, trackables, defaults, events) VALUES (
    'Evernight Isle',
    4,
    '[{"key":"mutual_assistance","label":"Mutual Assistance","sort_order":0},{"key":"siege","label":"Siege","sort_order":1},{"key":"copper_war","label":"Copper War","sort_order":2},{"key":"defeat","label":"Defeat","sort_order":3}]',
    '{"week_count":8,"key_event_name":"Copper War","key_event_required":4}',
    '[
      {"label":"City Clash (L1)","day_offset":3,"event_time":"20:00","week_start":1,"week_end":1,"notes":""},
      {"label":"City Clash (L2)","day_offset":6,"event_time":"20:00","week_start":1,"week_end":1,"notes":""},
      {"label":"Exclusive Weapon: Williams (Tank)","day_offset":4,"event_time":"20:00","week_start":1,"week_end":1,"notes":""},
      {"label":"Blood Night Descend (3x daily)","day_offset":null,"event_time":"20:00","week_start":1,"week_end":8,"notes":"3x daily, unscheduled"},
      {"label":"City Clash (L3)","day_offset":3,"event_time":"20:00","week_start":2,"week_end":2,"notes":""},
      {"label":"City Clash (L4)","day_offset":6,"event_time":"20:00","week_start":2,"week_end":2,"notes":""},
      {"label":"Exclusive Weapon: Lucius (Aircraft)","day_offset":4,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"Faction Awards","day_offset":7,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"City Clash (L5)","day_offset":3,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"City Clash (L6)","day_offset":6,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"Copper War","day_offset":3,"event_time":"20:00","week_start":4,"week_end":7,"notes":""},
      {"label":"Copper War","day_offset":6,"event_time":"20:00","week_start":4,"week_end":7,"notes":""},
      {"label":"Exclusive Weapon: Adam (Missile)","day_offset":4,"event_time":"20:00","week_start":6,"week_end":6,"notes":""},
      {"label":"Copper War Finals","day_offset":6,"event_time":"20:00","week_start":7,"week_end":7,"notes":""}
    ]'
);

-- S5 — Wild West (✅ confirmed)
INSERT INTO season_templates (template_name, season_number, trackables, defaults, events) VALUES (
    'Wild West',
    5,
    '[{"key":"crystal_gold","label":"CrystalGold","sort_order":0},{"key":"capture","label":"Capture","sort_order":1},{"key":"kills","label":"Kills","sort_order":2}]',
    '{"week_count":8,"key_event_name":"Finance Tycoon / Bank Capture","key_event_required":2}',
    '[
      {"label":"City Clash (L1)","day_offset":3,"event_time":"20:00","week_start":1,"week_end":1,"notes":""},
      {"label":"City Clash (L2)","day_offset":6,"event_time":"20:00","week_start":1,"week_end":1,"notes":""},
      {"label":"Exclusive Weapon: Fiona (Missile)","day_offset":4,"event_time":"20:00","week_start":1,"week_end":1,"notes":""},
      {"label":"High Noon / Shooting Duel (daily)","day_offset":null,"event_time":"20:00","week_start":1,"week_end":8,"notes":"Daily, unscheduled"},
      {"label":"City Clash (L3)","day_offset":3,"event_time":"20:00","week_start":2,"week_end":2,"notes":""},
      {"label":"City Clash (L4)","day_offset":6,"event_time":"20:00","week_start":2,"week_end":2,"notes":""},
      {"label":"Wasteland Trade Train (every 4h)","day_offset":null,"event_time":"20:00","week_start":2,"week_end":8,"notes":"Every 4h, unscheduled"},
      {"label":"Exclusive Weapon: Stetmann (Tank)","day_offset":4,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"City Clash (L5)","day_offset":3,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"City Clash (L6)","day_offset":6,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"Finance Tycoon / Bank Capture","day_offset":3,"event_time":"20:00","week_start":4,"week_end":7,"notes":""},
      {"label":"Finance Tycoon / Bank Capture","day_offset":6,"event_time":"20:00","week_start":4,"week_end":7,"notes":""},
      {"label":"Exclusive Weapon: Morrison (Aircraft)","day_offset":4,"event_time":"20:00","week_start":6,"week_end":6,"notes":""},
      {"label":"Golden Palace Opens","day_offset":3,"event_time":"20:00","week_start":7,"week_end":7,"notes":""}
    ]'
);

-- S6 — Lost Rainforest / Shadow Rainforest (✅ confirmed)
INSERT INTO season_templates (template_name, season_number, trackables, defaults, events) VALUES (
    'Lost Rainforest',
    6,
    '[{"key":"war_merit","label":"War Merit","sort_order":0},{"key":"capture","label":"Capture","sort_order":1},{"key":"defeat","label":"Defeat","sort_order":2}]',
    '{"week_count":8,"key_event_name":"Faction Clash","key_event_required":4}',
    '[
      {"label":"City Clash (L1)","day_offset":3,"event_time":"20:00","week_start":1,"week_end":1,"notes":""},
      {"label":"City Clash (L2)","day_offset":6,"event_time":"20:00","week_start":1,"week_end":1,"notes":""},
      {"label":"Exclusive Weapon: Kimberly Awakening","day_offset":4,"event_time":"20:00","week_start":1,"week_end":1,"notes":""},
      {"label":"City Clash (L3)","day_offset":3,"event_time":"20:00","week_start":2,"week_end":2,"notes":""},
      {"label":"City Clash (L4)","day_offset":6,"event_time":"20:00","week_start":2,"week_end":2,"notes":""},
      {"label":"Global Expedition Round 1 (14 days)","day_offset":1,"event_time":"20:00","week_start":2,"week_end":2,"notes":"14-day round"},
      {"label":"Altar Conquest","day_offset":2,"event_time":"20:00","week_start":2,"week_end":7,"notes":""},
      {"label":"Beneath the Ruins PvP","day_offset":null,"event_time":"20:00","week_start":2,"week_end":7,"notes":"Unscheduled"},
      {"label":"City Clash (L5)","day_offset":3,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"City Clash (L6)","day_offset":6,"event_time":"20:00","week_start":3,"week_end":3,"notes":""},
      {"label":"Faction Clash","day_offset":3,"event_time":"20:00","week_start":3,"week_end":7,"notes":""},
      {"label":"Faction Clash","day_offset":6,"event_time":"20:00","week_start":3,"week_end":7,"notes":""},
      {"label":"Global Expedition Round 2 (14 days)","day_offset":1,"event_time":"20:00","week_start":4,"week_end":4,"notes":"14-day round"},
      {"label":"Global Expedition Round 3 (14 days)","day_offset":1,"event_time":"20:00","week_start":6,"week_end":6,"notes":"14-day round"},
      {"label":"Faction Duel (Final)","day_offset":7,"event_time":"20:00","week_start":7,"week_end":7,"notes":""}
    ]'
);

-- +goose StatementEnd
