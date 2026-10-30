package demo

import "fmt"

// seasonHub fills the active season's Hub (private-docs 269–287): layout rows, two factions,
// three season alliances (two allied, one enemy) with observations, a primary Land route of
// four targets and its level table, six land events and two drops, four wars (two completed
// with attendance and polls, one member voting Yes and not showing), eight faction battles,
// resource totals and cutoffs, three Trade Posts with owners, two week events with results
// and one exemption. Wars are not put on the calendar (no war event type is configured), so
// the schedule the validator checks is unchanged; its rows in the season's weeks are filed
// under the season.
func (s *seeder) seasonHub() {
	r := stream("season-hub")
	sid := s.queryInt(`SELECT id FROM seasons WHERE season_number = 3`)
	if s.err != nil {
		return
	}
	// The season's week n began (start + 7(n-1)) — days before the anchor.
	weekStart := func(n int) int { return season3StartDaysAgo - 7*(n-1) }

	// Layout: the default tab and card order, as a new season gets it.
	for i, k := range []string{"dashboard", "rankings", "participation", "contributions", "rewards", "land", "war", "schedule", "comms", "files"} {
		s.exec(`INSERT INTO season_tabs (season_id, tab_key, visible, position) VALUES (?, ?, 1, ?)`, sid, k, i)
	}
	for i, k := range []string{"faction_allies", "leadership_flags", "season_schedule", "land_status", "war_record", "reward_tier_status"} {
		s.exec(`INSERT INTO season_dashboard_cards (season_id, card_key, visible, position) VALUES (?, ?, 1, ?)`, sid, k, i)
	}

	// Factions and the season's alliances: the first two allies allied with us, one rival enemy.
	ours := s.exec(`INSERT INTO season_factions (season_id, key, label, is_ours, position) VALUES (?, 'ember', 'Ember Accord', 1, 0)`, sid)
	theirs := s.exec(`INSERT INTO season_factions (season_id, key, label, is_ours, position) VALUES (?, 'tide', 'Tidebound', 0, 1)`, sid)
	ext := func(tag string) int64 { return s.queryInt(`SELECT id FROM external_alliances WHERE tag = ?`, tag) }
	type sa struct {
		tag, rel string
		faction  int64
		terms    string
	}
	var saIDs []int64
	for _, a := range []sa{{opponents[4].Tag, "allied", ours, "No hits on each other's land; share Trade Post timings."},
		{opponents[5].Tag, "allied", ours, "Defend each other's strongholds on request."},
		{opponents[0].Tag, "enemy", theirs, ""}} {
		saIDs = append(saIDs, s.exec(`INSERT INTO season_alliances (season_id, external_alliance_id, faction_id, relationship, terms, group_rank, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, sid, ext(a.tag), a.faction, a.rel, a.terms, 2+len(saIDs), s.ts(45)))
	}
	enemy := saIDs[2]
	// Observations: the enemy twice (a week apart), ours once.
	for i, days := range []int{12, 5} {
		s.exec(`INSERT INTO season_opponent_observations (season_id, season_alliance_id, is_own, observed_at, alliance_power, member_count,
			group_rank, top10_thp, top5_thp, top3_thp, source, recorded_by, created_at) VALUES (?, ?, 0, ?, ?, ?, 4, ?, ?, ?, 'manual', ?, ?)`,
			sid, enemy, s.ts(float64(days)), opponents[0].Power+int64(i)*400_000_000, 96+i,
			int64(1_450_000_000+i*30_000_000), int64(780_000_000+i*20_000_000), int64(495_000_000+i*10_000_000), r4User, s.ts(float64(days)))
	}
	s.exec(`INSERT INTO season_opponent_observations (season_id, is_own, observed_at, top10_thp, top5_thp, top3_thp, source, recorded_by, created_at)
		VALUES (?, 1, ?, 1520000000, 805000000, 488000000, 'manual', ?, ?)`, sid, s.ts(4), r4User, s.ts(4))
	s.exec(`INSERT INTO season_war_config (season_id, min_categories, production_stop_week, production_stop_day) VALUES (?, 2, 8, 1)`, sid)
	s.exec(`UPDATE seasons SET target_war_rank = 3 WHERE id = ?`, sid)
	for i, g := range []struct {
		label    string
		from, to int
	}{{"King", 1, 1}, {"Queen", 2, 3}, {"Rook", 4, 10}} {
		s.exec(`INSERT INTO season_rank_groups (season_id, label, rank_from, rank_to, position) VALUES (?, ?, ?, ?, ?)`, sid, g.label, g.from, g.to, i)
	}

	// Land: the season's rules and levels, a primary route of four targets, events and drops.
	s.exec(`INSERT INTO season_land_config (season_id, source, grade) VALUES (?, 'Demo', 'unverified')`, sid)
	cities := []string{"Cottage", "Food Street", "Baseball Field", "Hot Spring Town", "Sky Tower", "Electropolis"}
	for l, n := range cities {
		s.exec(`INSERT INTO season_land_levels (season_id, kind, level, name, resource_per_hour, unlock_week, unlock_day) VALUES (?, 'city', ?, ?, ?, ?, ?)`,
			sid, l+1, n, 4000*(l+1), 1+l, 3)
	}
	route := s.exec(`INSERT INTO season_land_routes (season_id, slot, name, is_active, updated_at) VALUES (?, 'primary', 'North push', 1, ?)`, sid, s.ts(6))
	var targets []int64
	for i, t := range []struct {
		name   string
		level  int
		status string
		days   float64
	}{{"Hot Spring Town", 4, "captured", 9}, {"Sky Tower", 5, "captured", 3}, {"Electropolis", 6, "planned", -1.5}, {"Sky Tower", 5, "planned", -3}} {
		var planned, actual any
		planned = s.ts(t.days)
		unlock := s.ts(t.days + 0.5)
		if t.status == "captured" {
			actual = s.ts(t.days)
		}
		targets = append(targets, s.exec(`INSERT INTO season_land_targets (route_id, position, name, kind, level, unlock_at, planned_capture_at,
			actual_capture_at, status, connected, resource_per_hour) VALUES (?, ?, ?, 'city', ?, ?, ?, ?, ?, 'touching', ?)`,
			route, i, t.name, t.level, unlock, planned, actual, t.status, 4000*t.level))
	}
	s.exec(`INSERT INTO season_land_route_revisions (route_id, saved_at, saved_by, snapshot) VALUES (?, ?, ?, '[]')`, route, s.ts(6), r4User)
	for i, e := range []struct {
		name, action string
		level        int
		days         float64
	}{{"Cottage", "captured", 1, 40}, {"Food Street", "captured", 2, 33}, {"Baseball Field", "captured", 3, 26},
		{"Cottage", "abandoned", 1, 12}, {"Hot Spring Town", "captured", 4, 9}, {"Sky Tower", "captured", 5, 3}} {
		s.exec(`INSERT INTO season_land_events (season_id, name, kind, level, action, at, source, recorded_by, created_at)
			VALUES (?, ?, 'city', ?, ?, ?, ?, ?, ?)`, sid, e.name+fmt.Sprintf(" %d", i/3+1), e.level, e.action, s.ts(e.days),
			[]string{"manual", "mobile"}[i%2], r4User, s.ts(e.days))
	}
	for i, d := range []struct{ name, status string }{{"Food Street 1", "assigned"}, {"Baseball Field 1", "drop_required"}} {
		s.exec(`INSERT INTO season_land_drops (season_id, holding_name, kind, level, replacement_target_id, replacement_at, must_start_by, status,
			assigned_member_id, updated_at) VALUES (?, ?, 'city', ?, ?, ?, ?, ?, ?, ?)`, sid, d.name, 2+i, targets[2+i], s.ts(-1.5-1.5*float64(i)),
			s.ts(-1.5-1.5*float64(i)+1.0/24), d.status, s.ds.activeMembers()[i].ID, s.ts(2))
	}

	// The calendar's rows in the season's weeks belong to it, as the event modal files them, so
	// the Hub's Schedule tab and the dashboard's Season Schedule card have something to show.
	s.exec(`UPDATE schedule_events SET season_id = ? WHERE event_date >= ? AND event_date < ?`,
		sid, s.date(season3StartDaysAgo), s.date(season3StartDaysAgo-7*8))

	// Wars: weeks 5 and 6 completed, week 7 declared, week 8 being considered.
	act := s.ds.activeMembers()
	var wars []int64
	for i, w := range []struct {
		week                 int
		role, status, result string
	}{{5, "attack", "completed", "win"}, {6, "defense", "completed", "loss"}, {7, "attack", "declared", ""}, {8, "attack", "considering", ""}} {
		date := s.date(weekStart(w.week) - 3)
		dir := "outgoing"
		if w.role == "defense" {
			dir = "incoming"
		}
		var outcome any
		if w.result != "" {
			outcome = w.result
		}
		wars = append(wars, s.exec(`INSERT INTO season_wars (season_id, week_number, role, direction, opponent_season_alliance_id, battle_date,
			battle_time, status, outcome, participant_count, poll_held, strategy_status, key_objective, created_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, '13:00', ?, ?, ?, 1, ?, ?, ?, ?, ?)`, sid, w.week, w.role, dir, enemy, date, w.status, outcome,
			map[bool]any{true: 30 + i*4, false: nil}[w.result != ""], []string{"final", "final", "ready", "draft"}[i],
			[]string{"Take the eastern copper mine", "Hold the stronghold line", "Split their rally", ""}[i], r4User, s.ts(30), s.ts(2)))
	}
	for wi := 0; wi < 2; wi++ {
		for mi, m := range act {
			attended, poll := 1, "yes"
			switch u := r.Float64() * m.Engagement; {
			case u < 0.05:
				attended, poll = 0, "no"
			case u < 0.08:
				attended, poll = 0, "none"
			}
			if wi == 0 && mi == 7 {
				attended, poll = 0, "yes" // voted Yes, did not show: a poll review waits
			}
			s.exec(`INSERT INTO season_war_attendance (war_id, member_id, attended, poll_response, recorded_by, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
				wars[wi], m.ID, attended, poll, r4User, s.ts(2))
		}
	}
	for i := 0; i < 8; i++ {
		ally := saIDs[i%2]
		date := s.date(weekStart(5+i/4) - (i % 4))
		s.exec(`INSERT INTO season_faction_battles (season_id, week_number, battle_date, alliance_season_alliance_id, opponent_label, role, outcome,
			source, recorded_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'manual', ?, ?)`, sid, 5+i/4, date, ally, fmt.Sprintf("Rival %d (S19%02d)", i%3+1, 40+i),
			[]string{"attack", "defense"}[i%2], []string{"win", "loss", "win", "draw"}[i%4], r4User, s.ts(2))
	}

	// Resources: ours and the enemy's totals, and three cutoffs.
	s.exec(`INSERT INTO season_resource_observations (season_id, is_own, resource_total, war_rank, observed_at, source, recorded_by, created_at)
		VALUES (?, 1, 7800000, 5, ?, 'manual', ?, ?)`, sid, s.ts(1), r4User, s.ts(1))
	s.exec(`INSERT INTO season_resource_observations (season_id, season_alliance_id, resource_total, war_rank, observed_at, source, recorded_by, created_at)
		VALUES (?, ?, 20000000, 2, ?, 'manual', ?, ?)`, sid, enemy, s.ts(1), r4User, s.ts(1))
	for _, c := range []struct {
		rank  int
		total int64
	}{{3, 10_000_000}, {4, 8_500_000}, {5, 7_800_000}} {
		s.exec(`INSERT INTO season_rank_cutoffs (season_id, rank, resource_total, observed_at, source, recorded_by, created_at) VALUES (?, ?, ?, ?, 'manual', ?, ?)`,
			sid, c.rank, c.total, s.ts(1), r4User, s.ts(1))
	}

	// Trade Posts: three, two with owners, one planned.
	for i, p := range []struct{ key, action string }{{"tp-north", "take"}, {"tp-river", "hold"}, {"tp-pass", ""}} {
		var action, at any
		if p.action != "" {
			action = p.action
		}
		if p.action == "take" {
			at = s.ts(-2)
		}
		post := s.exec(`INSERT INTO season_trade_posts (season_id, post_key, name, strategic_priority, planned_action, planned_at, position)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, sid, p.key, []string{"North Post", "River Post", "Pass Post"}[i], []string{"high", "medium", "low"}[i], action, at, i)
		if i < 2 {
			s.exec(`INSERT INTO season_trade_post_observations (post_id, owner_season_alliance_id, observed_at, source, recorded_by, created_at) VALUES (?, ?, ?, 'manual', ?, ?)`,
				post, []int64{enemy, saIDs[0]}[i], s.ts(2), r4User, s.ts(2))
		}
	}

	// Week 1's events: a Core VS day and the siege, with results for the first twenty members.
	var events []int64
	for i, e := range []struct{ label, kind string }{{"Monday VS", "vs"}, {"Zombie Siege", "siege"}} {
		events = append(events, s.exec(`INSERT INTO season_participation_events (season_id, week_number, label, kind, required_for_core, position)
			VALUES (?, 1, ?, ?, 1, ?)`, sid, e.label, e.kind, i))
	}
	for _, m := range act[:20] {
		for _, e := range events {
			s.exec(`INSERT INTO season_participation_event_entries (event_id, member_id, result) VALUES (?, ?, ?)`, e, m.ID,
				[]string{"full", "full", "partial", "completed"}[r.IntN(4)])
		}
	}
	s.exec(`INSERT INTO season_member_exemptions (season_id, member_id, kind, granted_by, note, created_at) VALUES (?, ?, 'core_events', ?, 'R4 duties on VS days', ?)`,
		sid, act[1].ID, r5User, s.ts(40))
}
