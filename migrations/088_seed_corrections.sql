-- +goose Up

-- Seed corrections (Project 21, private-docs 294–299). Every game fact a shipped season
-- template carries gains a source and a grade, and the six templates are corrected from the
-- audit's per-value table (lastwar-private-docs planning/project-21/seed-corrections.md).
--
-- Grades: official (FunFly's season calendar or publisher wiki article), cpt_hedge,
-- guide (another community guide), owner, requester (Pudgey's PDFs), conflict (sources
-- disagree; the row records the chosen value and the source names the alternative),
-- unverified (no raw source). In-game observation outranks all of them.
--
-- THE EXISTING-INSTALL RULE. A template is corrected only where it still reads as shipped,
-- column by column: each of events, defaults and trackables is replaced only while it still
-- equals its 034 text (compared through json(), which normalises whitespace). An operator's
-- edited column is left alone and the template's other columns are still corrected. A column
-- that is not valid JSON is left alone too, rather than aborting the migration and blocking
-- boot — the CASE keeps json() from ever being evaluated on it. Existing SEASONS are never
-- rewritten here: Edit Season shows what changed (GET .../template-diff) with a per-row Apply.
--
-- SERVER EVENTS keep their shape (owner, 2026-10-19). Their facts, recorded here rather than
-- as columns: Rally Challenge runs 2 days, Zombie Invasion 3, General's Trial 3; Rampage
-- Bosses and Doomsday run 1 day each, on Sundays only, alternating (each every 14 days, a
-- week apart). All five have fixed weekdays in the game, but the app does NOT enforce
-- weekdays — each install sets its own anchor dates (standing decision: "Server event
-- weekdays are not enforced").
--
-- The Desert Storm slot times need no step: migration 027 already set Early 09:00 /
-- Evening 18:00 / Late 23:00 on every install (private-docs 300, closed as already fixed).

-- Provenance on a season's own rows. template_key is the template row's stable "key", stamped
-- when a season is created from a template; the template diff matches on it, because a label
-- is not unique (S4 has two rows labelled Copper War). Rows from seasons created before this
-- migration have none and are matched by a composite fallback.
-- +goose StatementBegin
ALTER TABLE season_events ADD COLUMN template_key TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE season_events ADD COLUMN source TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE season_events ADD COLUMN grade TEXT NOT NULL DEFAULT 'unverified';
-- +goose StatementEnd

-- S4 — Evernight Isle (private-docs 296). The Exclusive Weapons un-rotated (Lucius W1, Adam W3,
-- Williams W6); Copper War at 12:30, W4–W7 Wednesday and Saturday, the W7 pair the finals —
-- eight wars, which is key_event_total; the Capitol unlock, Faction Duel, Holy Mountain (W8,
-- the one W8 event), Season Settlement and Transfer Surge (W9 day 1) added. Warehouse production
-- stopping at W8 day 1 is War configuration (season_war_config), not an event row.
-- +goose StatementBegin
UPDATE season_templates SET events = '[
  {"key":"s4_digging_stronghold_clash","label":"Digging Stronghold Clash","type_name":"Digging Stronghold Clash","type_short":"DSC","type_icon":"⛏️","day_offset":1,"event_time":"00:00","week_start":1,"week_end":1,"is_server_event":true,"duration_days":14,"notes":"","source":"Cpt Hedge; official S4 calendar W1–W2; requester PDF","grade":"cpt_hedge"},
  {"key":"s4_alliance_center","label":"Alliance Center and Tactics Cards","type_name":"Alliance Center","type_short":"AC","type_icon":"🏛️","day_offset":2,"event_time":"00:00","week_start":1,"week_end":1,"is_server_event":true,"duration_days":1,"notes":"","source":"Cpt Hedge; requester PDF","grade":"cpt_hedge"},
  {"key":"s4_blood_night","label":"Blood Night Descend (3x daily)","type_name":"Blood Night","type_short":"BN","type_icon":"🩸","day_offset":null,"event_time":"02:30","week_start":1,"week_end":0,"is_server_event":true,"duration_days":1,"notes":"3× daily at 02:30 / 10:30 / 18:30 server time, from W1 day 2","source":"LWHB times (paraphrase, unverified); start W1 day 2 (Cpt Hedge)","grade":"unverified"},
  {"key":"s4_city_clash_l1","label":"City Clash (L1)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":1,"week_end":1,"is_server_event":false,"duration_days":1,"notes":"","source":"Cpt Hedge City Clash guide; requester PDF","grade":"cpt_hedge"},
  {"key":"s4_city_clash_l2","label":"City Clash (L2)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":1,"week_end":1,"is_server_event":false,"duration_days":1,"notes":"","source":"Cpt Hedge City Clash guide; requester PDF","grade":"cpt_hedge"},
  {"key":"s4_ew_w1","label":"Exclusive Weapon: Lucius (Aircraft)","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":1,"week_end":1,"is_server_event":true,"duration_days":7,"notes":"","source":"Official S4 calendar; Cpt Hedge; lastwarhub","grade":"official"},
  {"key":"s4_city_clash_l3","label":"City Clash (L3)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":2,"week_end":2,"is_server_event":false,"duration_days":1,"notes":"","source":"Cpt Hedge City Clash guide; requester PDF","grade":"cpt_hedge"},
  {"key":"s4_city_clash_l4","label":"City Clash (L4)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":2,"week_end":2,"is_server_event":false,"duration_days":1,"notes":"","source":"Cpt Hedge City Clash guide; requester PDF","grade":"cpt_hedge"},
  {"key":"s4_faction_awards","label":"Faction Awards","type_name":"Faction Awards","type_short":"FA","type_icon":"🏆","day_offset":1,"event_time":"00:00","week_start":3,"week_end":3,"is_server_event":true,"duration_days":1,"notes":"","source":"Official S4 calendar (\"Fraction Awards\", W3); Cpt Hedge","grade":"official"},
  {"key":"s4_sarah_ur","label":"Sarah UR promotion","type_name":"UR Promotion","type_short":"UR","type_icon":"⭐","day_offset":1,"event_time":"00:00","week_start":3,"week_end":3,"is_server_event":true,"duration_days":1,"notes":"","source":"Cpt Hedge; requester PDF","grade":"cpt_hedge"},
  {"key":"s4_trade_posts","label":"Trade Posts / Oni Generals","type_name":"Trade Posts","type_short":"TP","type_icon":"🏪","day_offset":1,"event_time":"00:00","week_start":3,"week_end":3,"is_server_event":true,"duration_days":7,"notes":"","source":"Cpt Hedge; requester PDF","grade":"cpt_hedge"},
  {"key":"s4_ew_w3","label":"Exclusive Weapon: Adam (Missile)","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":3,"week_end":3,"is_server_event":true,"duration_days":7,"notes":"","source":"Official S4 calendar; Cpt Hedge","grade":"official"},
  {"key":"s4_city_clash_l5","label":"City Clash (L5)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":3,"week_end":3,"is_server_event":false,"duration_days":1,"notes":"","source":"Cpt Hedge City Clash guide; requester PDF","grade":"cpt_hedge"},
  {"key":"s4_city_clash_l6","label":"City Clash (L6)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":3,"week_end":3,"is_server_event":false,"duration_days":1,"notes":"","source":"Cpt Hedge City Clash guide; requester PDF","grade":"cpt_hedge"},
  {"key":"s4_copper_war_wed","label":"Copper War","type_name":"Copper War","type_short":"CPW","type_icon":"🥉","day_offset":3,"event_time":"12:30","week_start":4,"week_end":6,"is_server_event":false,"duration_days":1,"notes":"","source":"Official S4 calendar W4–W7; Cpt Hedge: Wed/Sat, battle 12:30; wiki 125","grade":"official"},
  {"key":"s4_copper_war_sat","label":"Copper War","type_name":"Copper War","type_short":"CPW","type_icon":"🥉","day_offset":6,"event_time":"12:30","week_start":4,"week_end":6,"is_server_event":false,"duration_days":1,"notes":"","source":"Official S4 calendar W4–W7; Cpt Hedge: Wed/Sat, battle 12:30; wiki 125","grade":"official"},
  {"key":"s4_city_clash_l7_capitol","label":"City Clash (L7 — Tenryū Castle, Capitol)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":7,"event_time":"12:00","week_start":4,"week_end":4,"is_server_event":false,"duration_days":1,"notes":"Capitol unlocks","source":"Official S4 calendar; Cpt Hedge; requester PDF p.25","grade":"official"},
  {"key":"s4_ew_w6","label":"Exclusive Weapon: Williams (Tank)","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":6,"week_end":6,"is_server_event":true,"duration_days":7,"notes":"","source":"Official S4 calendar; Cpt Hedge","grade":"official"},
  {"key":"s4_copper_war_finals_wed","label":"Copper War Finals","type_name":"Copper War","type_short":"CPW","type_icon":"🥉","day_offset":3,"event_time":"12:30","week_start":7,"week_end":7,"is_server_event":false,"duration_days":1,"notes":"Finals","source":"Cpt Hedge: the W7 Wednesday and Saturday wars are the finals","grade":"cpt_hedge"},
  {"key":"s4_copper_war_finals_sat","label":"Copper War Finals","type_name":"Copper War","type_short":"CPW","type_icon":"🥉","day_offset":6,"event_time":"12:30","week_start":7,"week_end":7,"is_server_event":false,"duration_days":1,"notes":"Finals","source":"Cpt Hedge: the W7 Wednesday and Saturday wars are the finals","grade":"cpt_hedge"},
  {"key":"s4_faction_duel_irc","label":"Faction Duel — Invasion Right Contest","type_name":"Faction Duel","type_short":"FD","type_icon":"⚔️","day_offset":1,"event_time":"00:00","week_start":7,"week_end":7,"is_server_event":true,"duration_days":6,"notes":"Ranking phase","source":"Official S4 calendar W7; Cpt Hedge; requester PDF","grade":"official"},
  {"key":"s4_faction_duel_capitol","label":"Faction Duel — Capitol clash","type_name":"Faction Duel","type_short":"FD","type_icon":"⚔️","day_offset":7,"event_time":"12:00","week_start":7,"week_end":7,"is_server_event":true,"duration_days":1,"notes":"","source":"Cpt Hedge; lastwarhub","grade":"cpt_hedge"},
  {"key":"s4_holy_mountain","label":"Holy Mountain (Blood Night stage 4)","type_name":"Holy Mountain","type_short":"HM","type_icon":"⛰️","day_offset":1,"event_time":"00:00","week_start":8,"week_end":8,"is_server_event":true,"duration_days":7,"notes":"","source":"Official S4 calendar: lit in W8 only; Cpt Hedge; requester 2026-10-19","grade":"official"},
  {"key":"s4_season_settlement","label":"Season Settlement","type_name":"Season Settlement","type_short":"SET","type_icon":"🏁","day_offset":1,"event_time":"00:00","week_start":8,"week_end":8,"is_server_event":true,"duration_days":7,"notes":"","source":"Official season calendar","grade":"official"},
  {"key":"s4_transfer_surge","label":"Transfer Surge","type_name":"Transfer Surge","type_short":"TS","type_icon":"🔀","day_offset":1,"event_time":"00:00","week_start":9,"week_end":9,"is_server_event":true,"duration_days":1,"notes":"","source":"Owner: season day 57 (W9 day 1)","grade":"owner"}
]'
WHERE season_number = 4
  AND CASE WHEN json_valid(events) THEN json(events) = json('[
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
    ]') ELSE 0 END;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE season_templates SET defaults = '{"week_count":8,"key_event_name":"Copper War","key_event_required":4,"key_event_total":8,"resource_name":"Copper"}'
WHERE season_number = 4
  AND CASE WHEN json_valid(defaults) THEN json(defaults) = json('{"week_count":8,"key_event_name":"Copper War","key_event_required":4}') ELSE 0 END;
-- +goose StatementEnd

-- S5 — Wild West (private-docs 297). City Clash from the official calendar (L1–L10 over W1–W5);
-- Golden Palace W5–W7 Saturday 13:00, graded conflict (wiki and Cpt Hedge say Saturday, the
-- calendar Sunday); Grand Nexus W3 Sunday and W8 Wednesday (the W8 exception); Bank Strongholds,
-- Trade Posts, Warzone Outpost and CrystalGold Tycoon added. The Wed/Sat Bank Capture rows and
-- the key event stay as seeded, graded unverified (owner, 2026-10-19: do not replace one guess
-- with another).
-- +goose StatementBegin
UPDATE season_templates SET events = '[
  {"key":"s5_city_clash_l1","label":"City Clash (L1)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":1,"week_end":1,"is_server_event":false,"duration_days":1,"notes":"","source":"Official S5 calendar (day 3)","grade":"official"},
  {"key":"s5_city_clash_l2","label":"City Clash (L2)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":1,"week_end":1,"is_server_event":false,"duration_days":1,"notes":"","source":"Official S5 calendar (day 6)","grade":"official"},
  {"key":"s5_ew_fiona","label":"Exclusive Weapon: Fiona (Missile)","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":1,"week_end":1,"is_server_event":true,"duration_days":7,"notes":"","source":"Cpt Hedge (Thursdays)","grade":"cpt_hedge"},
  {"key":"s5_high_noon","label":"High Noon / Shooting Duel (daily)","type_name":"High Noon","type_short":"HN","type_icon":"🌞","day_offset":null,"event_time":"00:00","week_start":1,"week_end":0,"is_server_event":true,"duration_days":1,"notes":"Daily; from W1 day 2 per one guide","source":"LWHB \"from day 2\" (paraphrase retracted)","grade":"unverified"},
  {"key":"s5_bank_strongholds","label":"Bank Strongholds","type_name":"Bank Strongholds","type_short":"BS","type_icon":"🏦","day_offset":null,"event_time":"00:00","week_start":1,"week_end":7,"is_server_event":true,"duration_days":1,"notes":"Daily window (Rare Soil War)","source":"Official S5 calendar: W1–W7, L6–L10 from W4","grade":"official"},
  {"key":"s5_city_clash_l3","label":"City Clash (L3)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":2,"week_end":2,"is_server_event":false,"duration_days":1,"notes":"","source":"Official S5 calendar (day 10)","grade":"official"},
  {"key":"s5_city_clash_l4","label":"City Clash (L4)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":2,"week_end":2,"is_server_event":false,"duration_days":1,"notes":"","source":"Official S5 calendar (day 13)","grade":"official"},
  {"key":"s5_trade_train","label":"Wasteland Trade Train (every 4h)","type_name":"Trade Train","type_short":"TTR","type_icon":"🚂","day_offset":null,"event_time":"00:00","week_start":2,"week_end":0,"is_server_event":true,"duration_days":1,"notes":"Every 4 hours: 00:00 / 04:00 / 08:00 / 12:00 / 16:00 / 20:00 (cadence unverified)","source":"Cpt Hedge (from W2); the 4-hour cadence is unverified","grade":"cpt_hedge"},
  {"key":"s5_crystalgold_tycoon","label":"CrystalGold Tycoon (weekly)","type_name":"CrystalGold Tycoon","type_short":"CGT","type_icon":"💎","day_offset":null,"event_time":"00:00","week_start":2,"week_end":7,"is_server_event":true,"duration_days":7,"notes":"","source":"Official S5 calendar","grade":"official"},
  {"key":"s5_ew_stetmann","label":"Exclusive Weapon: Stetmann (Tank)","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":3,"week_end":3,"is_server_event":true,"duration_days":7,"notes":"","source":"Cpt Hedge (Thursdays)","grade":"cpt_hedge"},
  {"key":"s5_city_clash_l5","label":"City Clash (L5)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":3,"week_end":3,"is_server_event":false,"duration_days":1,"notes":"","source":"Official S5 calendar (day 17)","grade":"official"},
  {"key":"s5_trade_posts_w3","label":"Trade Posts","type_name":"Trade Posts","type_short":"TP","type_icon":"🏪","day_offset":1,"event_time":"00:00","week_start":3,"week_end":3,"is_server_event":true,"duration_days":7,"notes":"","source":"Official S5 calendar","grade":"official"},
  {"key":"s5_grand_nexus_w3","label":"Grand Nexus","type_name":"Grand Nexus","type_short":"GN","type_icon":"🔷","day_offset":7,"event_time":"12:00","week_start":3,"week_end":3,"is_server_event":true,"duration_days":1,"notes":"","source":"Official S5 calendar: Sunday of W3","grade":"official"},
  {"key":"s5_warzone_outpost_build","label":"Warzone Outpost build","type_name":"Warzone Outpost","type_short":"WO","type_icon":"🏕️","day_offset":1,"event_time":"00:00","week_start":4,"week_end":4,"is_server_event":true,"duration_days":1,"notes":"","source":"Official S5 calendar (day 22)","grade":"official"},
  {"key":"s5_city_clash_l6_l7","label":"City Clash (L6–L7)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":4,"week_end":4,"is_server_event":false,"duration_days":1,"notes":"","source":"Official S5 calendar (day 24)","grade":"official"},
  {"key":"s5_city_clash_l8_l9","label":"City Clash (L8–L9)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":4,"week_end":4,"is_server_event":false,"duration_days":1,"notes":"","source":"Official S5 calendar (day 27)","grade":"official"},
  {"key":"s5_bank_capture_wed","label":"Finance Tycoon / Bank Capture","type_name":"Bank Capture","type_short":"BC","type_icon":"🏦","day_offset":3,"event_time":"12:00","week_start":4,"week_end":7,"is_server_event":false,"duration_days":1,"notes":"","source":"Seed (migration 034); no official basis found (Bank Strongholds run daily W1–W7)","grade":"unverified"},
  {"key":"s5_bank_capture_sat","label":"Finance Tycoon / Bank Capture","type_name":"Bank Capture","type_short":"BC","type_icon":"🏦","day_offset":6,"event_time":"12:00","week_start":4,"week_end":7,"is_server_event":false,"duration_days":1,"notes":"","source":"Seed (migration 034); no official basis found (Bank Strongholds run daily W1–W7)","grade":"unverified"},
  {"key":"s5_warzone_outpost_w5_mon","label":"Warzone Outpost battle","type_name":"Warzone Outpost","type_short":"WO","type_icon":"🏕️","day_offset":1,"event_time":"12:00","week_start":5,"week_end":5,"is_server_event":true,"duration_days":1,"notes":"","source":"Official S5 calendar (day 29)","grade":"official"},
  {"key":"s5_city_clash_l10","label":"City Clash (L10)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":5,"week_end":5,"is_server_event":false,"duration_days":1,"notes":"","source":"Official S5 calendar (day 31)","grade":"official"},
  {"key":"s5_warzone_outpost_w5_fri","label":"Warzone Outpost battle","type_name":"Warzone Outpost","type_short":"WO","type_icon":"🏕️","day_offset":5,"event_time":"12:00","week_start":5,"week_end":5,"is_server_event":true,"duration_days":1,"notes":"","source":"Official S5 calendar (day 33)","grade":"official"},
  {"key":"s5_golden_palace_w5","label":"Golden Palace Opens","type_name":"Golden Palace","type_short":"GP","type_icon":"🏰","day_offset":6,"event_time":"13:00","week_start":5,"week_end":5,"is_server_event":false,"duration_days":1,"notes":"Golden Palace unlocks","source":"Wiki 189 and Cpt Hedge: Saturday 13:00; the official calendar says Sunday (under development)","grade":"conflict"},
  {"key":"s5_ew_morrison","label":"Exclusive Weapon: Morrison (Aircraft)","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":6,"week_end":6,"is_server_event":true,"duration_days":7,"notes":"","source":"Cpt Hedge (Thursdays)","grade":"cpt_hedge"},
  {"key":"s5_warzone_outpost_w6","label":"Warzone Outpost battle","type_name":"Warzone Outpost","type_short":"WO","type_icon":"🏕️","day_offset":5,"event_time":"12:00","week_start":6,"week_end":6,"is_server_event":true,"duration_days":1,"notes":"","source":"Official S5 calendar (day 40)","grade":"official"},
  {"key":"s5_golden_palace_w6","label":"Golden Palace Opens","type_name":"Golden Palace","type_short":"GP","type_icon":"🏰","day_offset":6,"event_time":"13:00","week_start":6,"week_end":6,"is_server_event":false,"duration_days":1,"notes":"Golden Palace unlocks","source":"Wiki 189 and Cpt Hedge: Saturday 13:00; the official calendar says Sunday (under development)","grade":"conflict"},
  {"key":"s5_warzone_outpost_w7","label":"Warzone Outpost battle","type_name":"Warzone Outpost","type_short":"WO","type_icon":"🏕️","day_offset":5,"event_time":"12:00","week_start":7,"week_end":7,"is_server_event":true,"duration_days":1,"notes":"","source":"Official S5 calendar (day 47)","grade":"official"},
  {"key":"s5_golden_palace_w7","label":"Golden Palace Opens","type_name":"Golden Palace","type_short":"GP","type_icon":"🏰","day_offset":6,"event_time":"13:00","week_start":7,"week_end":7,"is_server_event":false,"duration_days":1,"notes":"Golden Palace unlocks","source":"Wiki 189 and Cpt Hedge: Saturday 13:00; the official calendar says Sunday (under development)","grade":"conflict"},
  {"key":"s5_trade_posts_w5","label":"Trade Posts","type_name":"Trade Posts","type_short":"TP","type_icon":"🏪","day_offset":1,"event_time":"00:00","week_start":5,"week_end":5,"is_server_event":true,"duration_days":7,"notes":"","source":"Official S5 calendar","grade":"official"},
  {"key":"s5_trade_posts_w7","label":"Trade Posts","type_name":"Trade Posts","type_short":"TP","type_icon":"🏪","day_offset":1,"event_time":"00:00","week_start":7,"week_end":7,"is_server_event":true,"duration_days":7,"notes":"","source":"Official S5 calendar","grade":"official"},
  {"key":"s5_season_settlement","label":"Season Settlement","type_name":"Season Settlement","type_short":"SET","type_icon":"🏁","day_offset":1,"event_time":"00:00","week_start":8,"week_end":8,"is_server_event":true,"duration_days":7,"notes":"","source":"Official season calendar","grade":"official"},
  {"key":"s5_grand_nexus_w8","label":"Grand Nexus","type_name":"Grand Nexus","type_short":"GN","type_icon":"🔷","day_offset":3,"event_time":"12:00","week_start":8,"week_end":8,"is_server_event":true,"duration_days":1,"notes":"","source":"Official S5 calendar: Wednesday of W8 (the W8 exception, raised on 294)","grade":"official"}
]'
WHERE season_number = 5
  AND CASE WHEN json_valid(events) THEN json(events) = json('[
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
    ]') ELSE 0 END;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE season_templates SET defaults = '{"week_count":8,"key_event_name":"Finance Tycoon / Bank Capture","key_event_required":2,"key_event_total":0,"resource_name":""}'
WHERE season_number = 5
  AND CASE WHEN json_valid(defaults) THEN json(defaults) = json('{"week_count":8,"key_event_name":"Finance Tycoon / Bank Capture","key_event_required":2}') ELSE 0 END;
-- +goose StatementEnd


-- S6 — Lost Rainforest (private-docs 297). City Clash L1–L3 only (L4–L6 removed); Faction Clash
-- W2–W7 Wednesday and Saturday (twelve, key_event_total); Altar Conquest W3–W7 Tuesdays 12:00;
-- Global Expedition rounds from W1 day 4, W3 and W5; Sanctuary opening and conquests; Tesla's
-- Awakening at W5, graded conflict (W6 on some servers); Season Conclusion at W8.
-- +goose StatementBegin
UPDATE season_templates SET events = '[
  {"key":"s6_faction_awards","label":"Faction Awards","type_name":"Faction Awards","type_short":"FA","type_icon":"🏆","day_offset":1,"event_time":"00:00","week_start":0,"week_end":0,"is_server_event":true,"duration_days":1,"notes":"Pre-season Week 2 (week before season start)","source":"BlueStacks \"pre-season day 4\" (paraphrase retracted)","grade":"unverified"},
  {"key":"s6_city_clash_l1","label":"City Clash (L1)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":1,"week_end":1,"is_server_event":false,"duration_days":1,"notes":"","source":"Official S6 calendar (day 3)","grade":"official"},
  {"key":"s6_beneath_the_ruins","label":"Beneath the Ruins PvP","type_name":"Beneath the Ruins","type_short":"BTR","type_icon":"🏚️","day_offset":null,"event_time":"00:00","week_start":1,"week_end":7,"is_server_event":true,"duration_days":1,"notes":"From W1 day 2","source":"Cpt Hedge: from season day 2","grade":"cpt_hedge"},
  {"key":"s6_ew_kimberly","label":"Exclusive Weapon: Kimberly Awakening","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":1,"week_end":1,"is_server_event":true,"duration_days":7,"notes":"","source":"Cpt Hedge","grade":"cpt_hedge"},
  {"key":"s6_global_expedition_r1","label":"Global Expedition Round 1 (14 days)","type_name":"Global Expedition","type_short":"GE","type_icon":"🌍","day_offset":4,"event_time":"00:00","week_start":1,"week_end":1,"is_server_event":true,"duration_days":14,"notes":"14-day round","source":"Cpt Hedge (verbatim)","grade":"cpt_hedge"},
  {"key":"s6_city_clash_l2","label":"City Clash (L2)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":6,"event_time":"12:00","week_start":1,"week_end":1,"is_server_event":false,"duration_days":1,"notes":"","source":"Official S6 calendar (day 6)","grade":"official"},
  {"key":"s6_faction_clash_wed","label":"Faction Clash","type_name":"Faction Clash","type_short":"FCL","type_icon":"🏹","day_offset":3,"event_time":"12:00","week_start":2,"week_end":7,"is_server_event":false,"duration_days":1,"notes":"","source":"Official S6 calendar: W2–W7; Cpt Hedge: Wed/Sat","grade":"official"},
  {"key":"s6_faction_clash_sat","label":"Faction Clash","type_name":"Faction Clash","type_short":"FCL","type_icon":"🏹","day_offset":6,"event_time":"12:00","week_start":2,"week_end":7,"is_server_event":false,"duration_days":1,"notes":"","source":"Official S6 calendar: W2–W7; Cpt Hedge: Wed/Sat","grade":"official"},
  {"key":"s6_city_clash_l3","label":"City Clash (L3)","type_name":"City Clash","type_short":"CC","type_icon":"🏙️","day_offset":3,"event_time":"12:00","week_start":2,"week_end":2,"is_server_event":false,"duration_days":1,"notes":"","source":"Official S6 calendar (day 10)","grade":"official"},
  {"key":"s6_sanctuary_opening","label":"Sanctuary opening","type_name":"Sanctuary","type_short":"SAN","type_icon":"🛕","day_offset":7,"event_time":"12:00","week_start":2,"week_end":2,"is_server_event":true,"duration_days":1,"notes":"","source":"Official S6 calendar (day 14, Sunday)","grade":"official"},
  {"key":"s6_ew_dva","label":"Exclusive Weapon: DVA Awakening","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":3,"week_end":3,"is_server_event":true,"duration_days":7,"notes":"","source":"Cpt Hedge; BlueStacks","grade":"cpt_hedge"},
  {"key":"s6_global_expedition_r2","label":"Global Expedition Round 2 (14 days)","type_name":"Global Expedition","type_short":"GE","type_icon":"🌍","day_offset":1,"event_time":"00:00","week_start":3,"week_end":3,"is_server_event":true,"duration_days":14,"notes":"14-day round","source":"Cpt Hedge","grade":"cpt_hedge"},
  {"key":"s6_altar_conquest","label":"Altar Conquest","type_name":"Altar Conquest","type_short":"ALT","type_icon":"🗿","day_offset":2,"event_time":"12:00","week_start":3,"week_end":7,"is_server_event":false,"duration_days":1,"notes":"1-hour window","source":"Official S6 calendar (days 16–44); wiki 292: Tuesdays 12:00, one hour","grade":"official"},
  {"key":"s6_ew_tesla","label":"Exclusive Weapon: Tesla Awakening","type_name":"Exclusive Weapon","type_short":"EW","type_icon":"⚔️","day_offset":4,"event_time":"00:00","week_start":5,"week_end":5,"is_server_event":true,"duration_days":7,"notes":"W6 on some servers","source":"Cpt Hedge: W5; BlueStacks: W5 or W6 by server","grade":"conflict"},
  {"key":"s6_global_expedition_r3","label":"Global Expedition Round 3 (14 days)","type_name":"Global Expedition","type_short":"GE","type_icon":"🌍","day_offset":1,"event_time":"00:00","week_start":5,"week_end":5,"is_server_event":true,"duration_days":14,"notes":"14-day round","source":"Cpt Hedge","grade":"cpt_hedge"},
  {"key":"s6_sanctuary_conquest_w5","label":"Sanctuary conquest","type_name":"Sanctuary","type_short":"SAN","type_icon":"🛕","day_offset":6,"event_time":"12:00","week_start":5,"week_end":5,"is_server_event":true,"duration_days":1,"notes":"","source":"Official S6 calendar (day 34)","grade":"official"},
  {"key":"s6_sanctuary_conquest_w6","label":"Sanctuary conquest","type_name":"Sanctuary","type_short":"SAN","type_icon":"🛕","day_offset":6,"event_time":"12:00","week_start":6,"week_end":6,"is_server_event":true,"duration_days":1,"notes":"","source":"Official S6 calendar (day 41)","grade":"official"},
  {"key":"s6_sanctuary_conquest_w7","label":"Sanctuary conquest","type_name":"Sanctuary","type_short":"SAN","type_icon":"🛕","day_offset":6,"event_time":"12:00","week_start":7,"week_end":7,"is_server_event":true,"duration_days":1,"notes":"","source":"Official S6 calendar (day 48)","grade":"official"},
  {"key":"s6_faction_duel_final","label":"Faction Duel (Final)","type_name":"Faction Duel","type_short":"FD","type_icon":"⚔️","day_offset":7,"event_time":"12:00","week_start":7,"week_end":7,"is_server_event":true,"duration_days":1,"notes":"","source":"Official S6 calendar: W7 only; seed","grade":"official"},
  {"key":"s6_season_conclusion","label":"Season Conclusion","type_name":"Season Settlement","type_short":"SET","type_icon":"🏁","day_offset":1,"event_time":"00:00","week_start":8,"week_end":8,"is_server_event":true,"duration_days":7,"notes":"","source":"Official season calendar","grade":"official"}
]'
WHERE season_number = 6
  AND CASE WHEN json_valid(events) THEN json(events) = json('[
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
    ]') ELSE 0 END;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE season_templates SET defaults = '{"week_count":8,"key_event_name":"Faction Clash","key_event_required":4,"key_event_total":12,"resource_name":""}'
WHERE season_number = 6
  AND CASE WHEN json_valid(defaults) THEN json(defaults) = json('{"week_count":8,"key_event_name":"Faction Clash","key_event_required":4}') ELSE 0 END;
-- +goose StatementEnd

-- +goose Down
-- The template corrections are not reverted: a rollback that rewrote templates would have to
-- tell a corrected row from an operator's edit, which is the judgement the Up refuses to make.
-- +goose StatementBegin
ALTER TABLE season_events DROP COLUMN grade;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE season_events DROP COLUMN source;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE season_events DROP COLUMN template_key;
-- +goose StatementEnd
