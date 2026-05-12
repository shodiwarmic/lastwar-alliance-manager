-- +goose Up
-- +goose StatementBegin

-- S1 — The Crimson Plague
UPDATE season_templates SET events = '[
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
]' WHERE season_number = 1;

-- S2 — Polar World
UPDATE season_templates SET events = '[
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
]' WHERE season_number = 2;

-- S3 — Golden Realm
UPDATE season_templates SET events = '[
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
]' WHERE season_number = 3;

-- S4 — Evernight Isle
UPDATE season_templates SET events = '[
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
]' WHERE season_number = 4;

-- S5 — Wild West
UPDATE season_templates SET events = '[
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
]' WHERE season_number = 5;

-- S6 — Lost Rainforest
UPDATE season_templates SET events = '[
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
]' WHERE season_number = 6;

-- +goose StatementEnd
