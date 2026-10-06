package demo

import (
	"encoding/json"
	"fmt"
	"time"

	"lastwar-alliance/internal/gametime"
)

// opponents are the fictional alliances the Duel League and the registry name. None is
// ours (Rule 2: our own alliance is never in external_alliances).
var opponents = []struct {
	Tag, Name string
	Server    int
	Power     int64
}{
	{"VXN", "Vexan Reach", 1877, 21_400_000_000},
	{"ORE", "Ore Barons", 1902, 18_900_000_000},
	{"HLX", "Helix Tide", 1861, 23_700_000_000},
	{"MRSH", "Marsh Lanterns", 1911, 17_200_000_000},
	{"QRZ", "Quartz Hollow", 1885, 20_100_000_000},
	{"SBL", "Sable Ward", 1898, 19_300_000_000},
	{"KTR", "Kestrel Row", 1870, 22_600_000_000},
}

// vsLeague: one active League season of four weeks, the current one included. Days are
// decided from both raw scores; our score is the roster's VS total that day.
func (s *seeder) vsLeague() {
	r := stream("vs-league")
	seasonID := s.exec(`INSERT INTO vs_league_seasons (season_number, league_tier, start_date, is_active, notes, created_at)
		VALUES (7, 'Gold Tier 2', ?, 1, '', ?)`, s.monday(3), s.ts(23))
	done := s.todayWeekday()
	act := s.ds.activeMembers()
	for w := 3; w >= 0; w-- {
		opp := opponents[w]
		weekID := s.exec(`INSERT INTO vs_league_weeks (season_id, week_number, week_date, league_tier, league_rank,
			opponent_tag, opponent_name, opponent_server, opponent_power, opponent_member_count, opponent_snapshot_at,
			our_server, our_power, our_member_count, our_snapshot_at, strategy_label, notes, created_at, updated_at)
			VALUES (?, ?, ?, 'Gold Tier 2', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)`,
			seasonID, 4-w, s.monday(w), 3+r.IntN(5), opp.Tag, opp.Name, opp.Server, opp.Power, 92+r.IntN(8),
			s.ts(float64(7*w)+0.5), ServerID, s.rosterPower(), len(act), s.ts(float64(7*w)+0.5),
			[]string{"push", "normal", "save", "normal"}[w], s.ts(float64(7*w)+0.5), s.ts(float64(7*w)+0.5))
		days := 6
		if w == 0 {
			days = min(done, 6)
		}
		for d := 0; d < days; d++ {
			var ours int64
			var best member
			var bestScore int64
			for _, m := range act {
				v := vsScore(m, w, d)
				ours += v
				if v > bestScore {
					best, bestScore = m, v
				}
			}
			theirs := int64(float64(ours) * (0.8 + 0.4*r.Float64()))
			outcome := "tie"
			if ours > theirs {
				outcome = "win"
			} else if theirs > ours {
				outcome = "loss"
			}
			s.exec(`INSERT INTO vs_league_days (week_id, day_number, our_score, opponent_score, outcome, mvp_is_ours, mvp_member_id, mvp_name, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?, ?)`,
				weekID, d+1, ours, theirs, outcome, best.ID, best.Name, s.ts(float64(7*w-d)), s.ts(float64(7*w-d)))
		}
		if w == 0 {
			continue // the bracket is captured at the end of the week
		}
		// The week's bracket: eight pairings, ours at our league rank.
		for i := 1; i <= 8; i++ {
			a := 7 + r.IntN(7)
			if i == 1 {
				s.exec(`INSERT INTO vs_league_matchups (week_id, match_index, a_rank, a_server, a_tag, a_name, a_points,
					b_rank, b_server, b_tag, b_name, b_points, is_ours, created_at)
					VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
					weekID, 3, ServerID, AllianceTag, AllianceName, a, 4, opp.Server, opp.Tag, opp.Name, 13-a, s.ts(float64(7*w-6)))
				continue
			}
			x, y := opponents[(i+w)%len(opponents)], opponents[(i+w+3)%len(opponents)]
			s.exec(`INSERT INTO vs_league_matchups (week_id, match_index, a_rank, a_server, a_tag, a_name, a_points,
				b_rank, b_server, b_tag, b_name, b_points, is_ours, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
				weekID, i, 2*i-1, x.Server+i, x.Tag+fmt.Sprint(i), x.Name+" "+fmt.Sprint(i), a,
				2*i, y.Server+i, y.Tag+fmt.Sprint(i+1), y.Name+" "+fmt.Sprint(i+1), 13-a, s.ts(float64(7*w-6)))
		}
	}
}

// rosterPower is the sum of active members' power — what the app snapshots when no
// LastRank alliance is configured.
func (s *seeder) rosterPower() int64 {
	var t int64
	for _, m := range s.ds.activeMembers() {
		t += m.Power
	}
	return t
}

// season3Start: the active season (Golden Realm, Season 3) began 50 days before the
// anchor, so the Alliance Exercise cutover (day 58, start + 57) lands eight days after it,
// inside the schedule window — both variants appear, with the rule in force.
const season3StartDaysAgo = 50

// seasons writes Seasons 1–3 (3 active), each with the settings' default score levels and
// reward tiers copied in and its template's trackables, as handleSeasonCreate does; then
// Season 3's weekly participation and contributions, and Season 2's rewards.
func (s *seeder) seasons() {
	r := stream("seasons")
	var scoreDefs []struct {
		Key, Label string
		Points     int
	}
	var tierDefs []struct {
		Key       string `json:"key"`
		Label     string `json:"label"`
		SlotCount int    `json:"slot_count"`
		Color     string `json:"color"`
	}
	var scoreJSON, tierJSON string
	if s.err == nil {
		s.err = s.tx.QueryRow(`SELECT season_score_levels_default, season_reward_tiers_default FROM settings WHERE id = 1`).Scan(&scoreJSON, &tierJSON)
	}
	if s.err != nil {
		return
	}
	if err := json.Unmarshal([]byte(scoreJSON), &scoreDefs); err != nil {
		s.err = err
		return
	}
	if err := json.Unmarshal([]byte(tierJSON), &tierDefs); err != nil {
		s.err = err
		return
	}

	type tmpl struct {
		Name       string
		Trackables []struct {
			Key       string `json:"key"`
			Label     string `json:"label"`
			SortOrder int    `json:"sort_order"`
		}
		KeyEvent string
		Required int
	}
	load := func(n int) tmpl {
		var t tmpl
		var tk, defs string
		if s.err == nil {
			s.err = s.tx.QueryRow(`SELECT template_name, trackables, defaults FROM season_templates WHERE season_number = ?`, n).Scan(&t.Name, &tk, &defs)
		}
		if s.err == nil {
			s.err = json.Unmarshal([]byte(tk), &t.Trackables)
		}
		var d struct {
			KeyEventName     string `json:"key_event_name"`
			KeyEventRequired int    `json:"key_event_required"`
		}
		if s.err == nil {
			s.err = json.Unmarshal([]byte(defs), &d)
		}
		t.KeyEvent, t.Required = d.KeyEventName, d.KeyEventRequired
		return t
	}

	starts := map[int]int{3: season3StartDaysAgo, 2: season3StartDaysAgo + 63, 1: season3StartDaysAgo + 126}
	seasonIDs := map[int]int64{}
	for n := 1; n <= 3; n++ {
		t := load(n)
		if s.err != nil {
			return
		}
		start := starts[n]
		var id int64
		if n == 3 {
			id = s.exec(`INSERT INTO seasons (name, season_number, start_date, week_count, key_event_name, key_event_required, is_active, created_at)
				VALUES (?, ?, ?, 8, ?, ?, 1, ?)`, t.Name, n, s.date(start), t.KeyEvent, t.Required, s.ts(float64(start)))
		} else {
			end := starts[n+1] + 1
			id = s.exec(`INSERT INTO seasons (name, season_number, start_date, end_date, week_count, key_event_name, key_event_required, is_active, archived_at, created_at)
				VALUES (?, ?, ?, ?, 8, ?, ?, 0, ?, ?)`, t.Name, n, s.date(start), s.date(end), t.KeyEvent, t.Required, s.ts(float64(end)), s.ts(float64(start)))
		}
		seasonIDs[n] = id
		for i, sl := range scoreDefs {
			s.exec(`INSERT INTO season_score_levels (season_id, key, label, points, sort_order) VALUES (?, ?, ?, ?, ?)`, id, sl.Key, sl.Label, sl.Points, i)
		}
		for i, rt := range tierDefs {
			s.exec(`INSERT INTO season_reward_tiers (season_id, key, label, slot_count, color, sort_order) VALUES (?, ?, ?, ?, ?, ?)`,
				id, rt.Key, rt.Label, rt.SlotCount, rt.Color, i)
		}
		for _, tk := range t.Trackables {
			s.exec(`INSERT INTO season_trackables (season_id, key, label, sort_order) VALUES (?, ?, ?, ?)`, id, tk.Key, tk.Label, tk.SortOrder)
		}
	}

	// Season 3: the weeks that have started.
	act := s.ds.activeMembers()
	weeksIn := season3StartDaysAgo/7 + 1
	s3 := seasonIDs[3]
	trackables := s.trackableIDs(s3)
	for w := 1; w <= weeksIn; w++ {
		daysAgo := season3StartDaysAgo - 7*(w-1)
		// Recorded at the end of the week — or, for the week still running, just now.
		at := s.ts(max(float64(daysAgo-6), 0.3))
		for _, m := range act {
			score := "full"
			switch u := r.Float64() * m.Engagement; {
			case u < 0.12:
				score = "absent"
			case u < 0.3:
				score = "partial"
			}
			key := 0
			if score != "absent" && r.Float64() < 0.8 {
				key = 1
			}
			s.exec(`INSERT INTO season_participation (season_id, member_id, week_number, score, attended_key_event, note, recorded_by, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, '', ?, ?, ?)`, s3, m.ID, w, score, key, r4User, at, at)
			for _, tid := range trackables {
				s.exec(`INSERT INTO season_member_records (season_id, member_id, week_number, trackable_id, recorded_value, logged_by, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?)`, s3, m.ID, w, tid,
					int64(float64(2000+r.IntN(9000))*m.Engagement*rankPowerRatio[m.Rank]), r4User, at)
			}
		}
	}

	// Season 2's rewards, assigned at its close, by participation.
	tiers := []string{}
	slots := []int{}
	for _, t := range tierDefs {
		tiers = append(tiers, t.Key)
		slots = append(slots, t.SlotCount)
	}
	ranked := topBy(act, len(act), func(m member) int64 { return int64(m.Engagement * 1000) })
	ti, used := 0, 0
	for _, m := range ranked {
		for ti < len(tiers) && used >= slots[ti] {
			ti, used = ti+1, 0
		}
		if ti >= len(tiers) {
			break
		}
		used++
		pct := 55 + 45*min(1, m.Engagement-0.5)
		s.exec(`INSERT INTO season_rewards (season_id, member_id, reward_tier, participation_pct, contribution_pct, note, logged_by, logged_at)
			VALUES (?, ?, ?, ?, ?, '', ?, ?)`, seasonIDs[2], m.ID, tiers[ti], pct, pct*0.9, r5User, s.ts(float64(starts[3]-1)))
	}
}

func (s *seeder) trackableIDs(seasonID int64) []int64 {
	if s.err != nil {
		return nil
	}
	rows, err := s.tx.Query(`SELECT id FROM season_trackables WHERE season_id = ? ORDER BY sort_order`, seasonID)
	if err != nil {
		s.err = err
		return nil
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	s.err = rows.Err()
	return ids
}

// scheduleWindow is how far either side of the anchor the schedule runs.
const scheduleWindow = 42

// schedule writes ±6 weeks of events, obeying the rules validateEventRules enforces even
// though it inserts directly (a test in internal/app runs the validator over every row):
//
//   - the Alliance Exercise every other day at mg_default_time — Marshal's Guard before
//     Season 3 day 58, Large Sandworm from it, one slot so one parity for both;
//   - Zombie Siege every fourth day (three clear days between sieges) at zs_default_time;
//   - Desert Storm on game-day Fridays, one battle per task force at its slot time;
//   - two custom types, weekly.
func (s *seeder) schedule() {
	typeID := func(short string) int64 {
		return s.queryInt(`SELECT id FROM schedule_event_types WHERE short_name = ?`, short)
	}
	mg, ls, zs, ds := typeID("MG"), typeID("LS"), typeID("ZS"), typeID("DS")
	meeting := s.exec(`INSERT INTO schedule_event_types (name, short_name, icon, is_system, active, sort_order, has_level, announce, created_at)
		VALUES ('Officer Meeting', 'OM', '🗣️', 0, 1, 10, 0, 1, ?)`, s.ts(90))
	contest := s.exec(`INSERT INTO schedule_event_types (name, short_name, icon, is_system, active, sort_order, has_level, announce, created_at)
		VALUES ('Truck Contest', 'TC', '🚚', 0, 1, 11, 0, 1, ?)`, s.ts(90))
	// Task force slots: A fights at slot 2, B at slot 3.
	s.exec(`UPDATE storm_tf_config SET time_slot = 2, participating = 1 WHERE task_force = 'A'`)
	s.exec(`UPDATE storm_tf_config SET time_slot = 3, participating = 1 WHERE task_force = 'B'`)
	slotTime := func(slot int) string {
		var t string
		if s.err == nil {
			s.err = s.tx.QueryRow(`SELECT time_st FROM storm_slot_times WHERE slot = ?`, slot).Scan(&t)
		}
		return t
	}
	dsTime := map[string]string{"A": slotTime(2), "B": slotTime(3)}
	var mgTime, zsTime string
	if s.err == nil {
		s.err = s.tx.QueryRow(`SELECT mg_default_time, zs_default_time FROM settings WHERE id = 1`).Scan(&mgTime, &zsTime)
	}
	cutover := -(sandwormCutoverDay - season3StartDaysAgo) // days-ago of Season 3 day 58

	add := func(key string, typ int64, daysAgo int, tm string, level any, tf any, note string) {
		id := s.exec(`INSERT INTO schedule_events (event_date, event_type_id, event_time, level, notes, created_by, created_at, updated_at, all_day, task_force)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
			s.date(daysAgo), typ, tm, level, note, r4User, s.ts(float64(max(daysAgo, 0)+3)), s.ts(float64(max(daysAgo, 0)+3)), tf)
		tfs, _ := tf.(string)
		s.scheduleIDs[key] = append(s.scheduleIDs[key], scheduledEvent{ID: id, Date: s.date(daysAgo), DaysAgo: daysAgo, TaskForce: tfs})
	}
	epoch := s.today.Unix() / 86400
	for d := scheduleWindow; d >= -scheduleWindow; d-- {
		day := epoch - int64(d)
		date, _ := time.Parse(gametime.DateLayout, s.date(d))
		if day%2 == 0 {
			if d > cutover {
				add("MG", mg, d, mgTime, 6+min(6, (scheduleWindow-d)/14), nil, "")
			} else {
				add("LS", ls, d, mgTime, 10+(cutover-d)/4, nil, "")
			}
		}
		if day%4 == 1 {
			add("ZS", zs, d, zsTime, 18+min(12, (scheduleWindow-d)/8), nil, "")
		}
		if date.Weekday() == time.Friday {
			for _, tf := range []string{"A", "B"} {
				add("DS", ds, d, dsTime[tf], nil, tf, "")
			}
		}
		switch date.Weekday() {
		case time.Sunday:
			add("OM", meeting, d, "20:00", nil, nil, "Weekly officer sync — VS plan and Desert Storm lineups.")
		case time.Wednesday:
			add("TC", contest, d, "19:00", nil, nil, "")
		}
	}
}

// sandwormCutoverDay is Season 3 day 58 counted from the season's start (start + 57),
// the same constant the app's validator uses.
const sandwormCutoverDay = 57

// participation records one board for each rule: the latest past Alliance Exercise
// (absent), the latest past Zombie Siege (zero), and the latest past Desert Storm for task
// force A (role). Status is derived by the app from these rows; the seed stores only what
// the mail said, the roles, and one excusal.
func (s *seeder) participation() {
	r := stream("participation")
	act := s.ds.activeMembers()
	latest := func(keys ...string) (scheduledEvent, string) {
		var best scheduledEvent
		var bestKey string
		for _, k := range keys {
			for _, e := range s.scheduleIDs[k] {
				if e.DaysAgo > 0 && (best.ID == 0 || e.DaysAgo < best.DaysAgo) && (k != "DS" || e.TaskForce == "A") {
					best, bestKey = e, k
				}
			}
		}
		return best, bestKey
	}
	trackable := func(short, key string) int64 {
		return s.queryInt(`SELECT pt.id FROM participation_trackables pt JOIN schedule_event_types t ON t.id = pt.event_type_id
			WHERE t.short_name = ? AND pt.key = ?`, short, key)
	}
	board := func(e scheduledEvent, notes string) int64 {
		at := s.ts(float64(e.DaysAgo) - 0.3)
		return s.exec(`INSERT INTO participation_boards (schedule_event_id, source, result_json, notes, recorded_by, created_at, updated_at)
			VALUES (?, 'import', '{}', ?, ?, ?, ?)`, e.ID, notes, r4User, at, at)
	}
	entry := func(boardID int64, m member, rank int, tid int64, value int64) {
		eid := s.exec(`INSERT INTO participation_entries (board_id, member_id, name_snapshot, rank) VALUES (?, ?, ?, ?)`,
			boardID, m.ID, m.Name, rank)
		s.exec(`INSERT INTO participation_values (entry_id, trackable_id, value) VALUES (?, ?, ?)`, eid, tid, value)
	}

	// Alliance Exercise: most members attack; the absent ones are the misses.
	if e, key := latest("MG", "LS"); e.ID != 0 {
		b := board(e, "")
		tid := trackable(key, "damage")
		attackers := []member{}
		for _, m := range act {
			if r.Float64() < 0.8*m.Engagement+0.1 {
				attackers = append(attackers, m)
			}
		}
		ranked := topBy(attackers, len(attackers), func(m member) int64 { return scaledScore(m, "alliance_exercise", 2.4e9) })
		for i, m := range ranked {
			entry(b, m, i+1, tid, scaledScore(m, "alliance_exercise", 2.4e9))
		}
		for _, m := range act {
			if !containsMember(attackers, m) {
				s.exec(`INSERT INTO participation_exceptions (board_id, member_id, kind, reason, recorded_by, created_at)
					VALUES (?, ?, 'excused', 'Told us in advance — travelling.', ?, ?)`, b, m.ID, r4User, s.ts(float64(e.DaysAgo)-0.2))
				break
			}
		}
	}
	// Zombie Siege: everyone attacked is on the board; a 0 is an undefended base.
	if e, _ := latest("ZS"); e.ID != 0 {
		b := board(e, "")
		tid := trackable("ZS", "waves")
		defended := topBy(act[:60], 60, func(m member) int64 { return scaledScore(m, "zombie_siege", 18) })
		for i, m := range defended {
			v := scaledScore(m, "zombie_siege", 18)
			if i%17 == 16 {
				v = 0
			}
			entry(b, m, i+1, tid, v)
		}
	}
	// Desert Storm, task force A: twenty starters and ten subs; an absent starter is a miss.
	if e, _ := latest("DS"); e.ID != 0 {
		b := board(e, "")
		tid := trackable("DS", "points")
		roster := topBy(act, 30, func(m member) int64 { return m.Power })
		var fought []member
		for i, m := range roster {
			role := "starter"
			if i >= 20 {
				role = "sub"
			}
			s.exec(`INSERT INTO participation_roles (board_id, member_id, role, task_force) VALUES (?, ?, ?, 'A')`, b, m.ID, role)
			if role == "starter" && i != 7 && i != 13 || role == "sub" && i%3 == 0 {
				fought = append(fought, m)
			}
		}
		for i, m := range topBy(fought, len(fought), func(m member) int64 { return scaledScore(m, "desert_storm", 1.6e6) }) {
			entry(b, m, i+1, tid, scaledScore(m, "desert_storm", 1.6e6))
		}
	}
}

func containsMember(ms []member, m member) bool {
	for _, x := range ms {
		if x.ID == m.ID {
			return true
		}
	}
	return false
}

// strikes: the Accountability page's history over the last ninety days, at
// strikesPerMemberPer30Days across the system types, a few excused.
func (s *seeder) strikes() {
	r := stream("strikes")
	act := s.ds.activeMembers()
	n := int(strikesPerMemberPer30Days * float64(len(act)) * 3)
	types := []struct{ key, reason string }{
		{"vs_below_threshold", "Below the VS minimum for the week"},
		{"train_no_show", "Missed their train slot"},
		{"storm_no_show", "Signed up for Desert Storm, did not show"},
		{"exercise_no_show", "No attack in the Alliance Exercise"},
		{"zs_no_defense", "Base not defended in Zombie Siege"},
		{"manual", "Attacked an ally's truck"},
	}
	for i := 0; i < n; i++ {
		m := act[r.IntN(len(act))]
		if m.Engagement > 1.3 {
			continue // the keenest members keep a clean sheet
		}
		t := types[r.IntN(len(types))]
		days := 2 + r.IntN(88)
		status, excusedBy, excusedReason := "active", any(nil), ""
		if r.Float64() < 0.2 {
			status, excusedBy, excusedReason = "excused", r4User, "Away on holiday, told us beforehand"
		}
		s.exec(`INSERT INTO accountability_strikes (member_id, strike_type, reason, ref_date, status, excused_by, excused_reason, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, t.key, t.reason, s.date(days), status, excusedBy, excusedReason, r4User, s.ts(float64(days)-0.4))
	}
}
