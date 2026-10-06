package demo

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"

	"lastwar-alliance/internal/ooxml"
)

// train: eight weeks of train logs at the measured cadence (at most one of each type a
// day, inside the default daily limits), plus two eligibility rules.
func (s *seeder) train() {
	r := stream("train")
	act := s.ds.activeMembers()
	vipTypes := []string{"SPECIAL_GUEST", "GUARDIAN_DEFENDER"}
	for d := 55; d >= 0; d-- {
		for _, typ := range []string{"FREE", "PURCHASED"} {
			if r.Float64() >= trainsPerWeek[typ]/7 {
				continue
			}
			conductor := act[r.IntN(len(act))]
			vip := act[r.IntN(len(act))]
			showed := 1
			if r.Float64() < 0.05 {
				showed = 0
			}
			s.exec(`INSERT INTO train_logs (date, train_type, conductor_id, vip_id, vip_type, notes, created_by, created_at, updated_at, showed_up)
				VALUES (?, ?, ?, ?, ?, '', ?, ?, ?, ?)`,
				s.date(d), typ, conductor.ID, vip.ID, vipTypes[r.IntN(2)], r4User, s.ts(float64(d)-0.5), s.ts(float64(d)-0.5), showed)
		}
	}
	s.exec(`INSERT INTO eligibility_rules (name, selection_method, conditions, created_by, created_at, updated_at)
		VALUES ('Longest wait, R3 and up', '{"type":"GREATEST","field":"days_since_free_conducted"}',
		        '{"groups":[{"conditions":[{"variable":"rank","op":">=","value":"R3"}]}]}', ?, ?, ?)`, r4User, s.ts(40), s.ts(40))
	s.exec(`INSERT INTO eligibility_rules (name, selection_method, conditions, created_by, created_at, updated_at)
		VALUES ('Random pick', '{"type":"RANDOM"}', '{"groups":[]}', ?, ?, ?)`, r4User, s.ts(40), s.ts(40))
}

// storm: registrations for most of the roster, and a group plan for each task force.
func (s *seeder) storm() {
	r := stream("storm")
	act := s.ds.activeMembers()
	for _, m := range act {
		if r.Float64() < 0.25 {
			continue
		}
		s.exec(`INSERT INTO storm_registrations (member_id, slot_1, slot_2, slot_3, updated_at) VALUES (?, ?, ?, ?, ?)`,
			m.ID, r.IntN(3), r.IntN(3), r.IntN(3), s.ts(float64(1+r.IntN(6))))
	}
	strongest := topBy(act, 60, func(m member) int64 { return m.Power })
	plan := []struct {
		tf, name, instructions string
		buildings              []string
	}{
		{"A", "Hospital Squad", "Take the four field hospitals in the first minute, then hold.", []string{"field_hospital_1", "field_hospital_2", "field_hospital_3", "field_hospital_4"}},
		{"A", "Refinery Push", "Oil refineries, then rotate to the Science Hub.", []string{"oil_refinery_1", "oil_refinery_2", "science_hub"}},
		{"A", "Silo Strike", "Wait for the Nuclear Silo to open. Everyone moves together.", []string{"nuclear_silo", "arsenal"}},
		{"B", "Hospital Squad", "Hospitals first.", []string{"field_hospital_1", "field_hospital_2"}},
		{"B", "Silo Strike", "Silo when it opens; Info Center otherwise.", []string{"nuclear_silo", "info_center"}},
	}
	next := map[string]int{"A": 0, "B": 30}
	for i, g := range plan {
		gid := s.exec(`INSERT INTO storm_groups (task_force, name, instructions, sort_order, created_at) VALUES (?, ?, ?, ?, ?)`,
			g.tf, g.name, g.instructions, i, s.ts(9))
		for j, b := range g.buildings {
			bid := s.exec(`INSERT INTO storm_group_buildings (group_id, building_id, sort_order) VALUES (?, ?, ?)`, gid, b, j)
			for p := 0; p < 2; p++ {
				m := strongest[next[g.tf]%len(strongest)]
				next[g.tf]++
				s.exec(`INSERT INTO storm_group_building_members (group_building_id, member_id, is_sub, position) VALUES (?, ?, 0, ?)`, bid, m.ID, p)
			}
		}
		m := strongest[next[g.tf]%len(strongest)]
		next[g.tf]++
		s.exec(`INSERT INTO storm_group_members (group_id, member_id, is_sub, position) VALUES (?, ?, 1, 0)`, gid, m.ID)
	}
}

// prospects: transfers and server prospects at every status.
func (s *seeder) prospects() {
	r := stream("prospects")
	statuses := []string{"interested", "pending", "qualified_transfer", "unqualified_transfer", "declined", "interested"}
	recruiters := s.ds.byRank("R4")
	for i, st := range statuses {
		name := composeName(r)
		typ := "transfer"
		server := strconv.Itoa(opponents[i].Server)
		if i%3 == 2 {
			typ, server = "prospect", strconv.Itoa(ServerID)
		}
		power := roundStat(r, fromDeciles(powerDeciles, r.Float64()))
		s.exec(`INSERT INTO prospects (name, server, source_alliance, power, hero_power, rank_in_alliance, recruiter_id, status, notes,
			first_contacted, seat_color, interested_in_r4, prospect_type, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			name, server, opponents[i].Tag, power, power/2, []string{"R3", "R4", "R2"}[i%3], recruiters[i%len(recruiters)].ID, st,
			[]string{"Wants a VS-focused alliance.", "Asked about Desert Storm times.", "", "Strong hero lineup.", "Joined another alliance.", ""}[i],
			s.date(3+i*4), []string{"", "red", "gold", "", "", "blue"}[i], i%4 == 1, typ, s.ts(float64(3+i*4)), s.ts(float64(1+i)))
	}
}

// allies: three allies, each linked to a registry row, plus three registry-only
// alliances (the opponent picker's list). None is ours.
func (s *seeder) allies() {
	for i, o := range opponents[4:] {
		ext := s.exec(`INSERT INTO external_alliances (tag, name, server, power, kills, member_count, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, o.Tag, o.Name, o.Server, o.Power, o.Power/2400, 95+i, s.ts(30), s.ts(2))
		ally := s.exec(`INSERT INTO allies (server, tag, name, active, notes, contact, created_at, external_alliance_id)
			VALUES (?, ?, ?, 1, ?, ?, ?, ?)`, strconv.Itoa(o.Server), o.Tag, o.Name,
			[]string{"Non-aggression since Season 1.", "Shares the eastern trade route.", "Truck pact, renewed each season."}[i],
			composeName(stream("ally-contact-"+o.Tag)), s.ts(60), ext)
		for t := int64(1); t <= int64(1+i); t++ {
			s.exec(`INSERT INTO ally_agreements (ally_id, agreement_type_id) VALUES (?, ?)`, ally, t)
		}
	}
	for i, o := range opponents[:4] {
		s.exec(`INSERT INTO external_alliances (tag, name, server, power, kills, member_count, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, o.Tag, o.Name, o.Server, o.Power, o.Power/2600, 90+i, s.ts(28), s.ts(7*float64(i)))
	}
}

// polls: a named and an anonymous template, one launched poll each.
func (s *seeder) polls() {
	r := stream("polls")
	act := s.ds.activeMembers()
	type poll struct {
		title, question, kind string
		options               []string
	}
	for i, p := range []poll{
		{"Desert Storm slot", "Which slot can you make this Friday?", "named", []string{"Evening", "Late", "Either", "Neither"}},
		{"Alliance rules vote", "Should the VS minimum go up to 3M?", "anonymous", []string{"Yes", "No", "Abstain"}},
	} {
		opts, _ := json.Marshal(p.options)
		tid := s.exec(`INSERT INTO poll_templates (title, question, options, poll_type, multi_select, created_by, created_at)
			VALUES (?, ?, ?, ?, 0, ?, ?)`, p.title, p.question, string(opts), p.kind, r4User, s.ts(20))
		iid := s.exec(`INSERT INTO poll_instances (template_id, label, question, options, poll_type, multi_select, rank_filter, total_eligible, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, 0, NULL, ?, ?, ?)`, tid, p.title+" — this week", p.question, string(opts), p.kind, len(act), r4User, s.ts(float64(2+i)))
		counts := map[string]int{}
		for _, m := range act {
			if r.Float64() > 0.7 {
				continue
			}
			o := p.options[r.IntN(len(p.options))]
			if p.kind == "anonymous" {
				counts[o]++
				continue
			}
			s.exec(`INSERT INTO poll_responses (instance_id, member_id, option_key, recorded_by, recorded_at) VALUES (?, ?, ?, ?, ?)`,
				iid, m.ID, o, r4User, s.ts(float64(1+i)))
		}
		for _, o := range p.options {
			if p.kind == "anonymous" {
				s.exec(`INSERT INTO poll_anonymous_counts (instance_id, option_key, response_count) VALUES (?, ?, ?)`, iid, o, counts[o])
			}
		}
	}
}

// dyno: shout-outs and feedback.
func (s *seeder) dyno() {
	r := stream("dyno")
	act := s.ds.activeMembers()
	notes := []string{
		"Carried the Silo push on Friday — held it for six minutes alone.",
		"Always first to help with trucks.",
		"Great shield timing during Zombie Siege.",
		"Organised the new-member welcome thread.",
		"Huge VS day on Thursday, top of the server.",
		"Covered the late Desert Storm slot when we were short.",
		"Patient with questions in alliance chat.",
		"Swapped task force at short notice.",
		"Missed the shield call twice this week.",
		"Keeps the train schedule up to date.",
	}
	for i, n := range notes {
		m := act[r.IntN(len(act))]
		points := []int{100, 250, 500}[r.IntN(3)]
		if i == 8 {
			points = -100
		}
		s.exec(`INSERT INTO dyno_recommendations (member_id, points, notes, created_by_id, created_at, is_author_public, min_view_rank)
			VALUES (?, ?, ?, ?, ?, ?, '')`, m.ID, points, n, 2+r.IntN(3), s.ts(float64(1+3*i)), i%2 == 0)
	}
}

// officerCommand: who does what among the leadership.
func (s *seeder) officerCommand() {
	leaders := append(s.ds.byRank("R5"), s.ds.byRank("R4")...)
	cats := []struct {
		name  string
		items [][3]string
	}{
		{"Events", [][3]string{{"Desert Storm lineups", "Sets both task forces' groups by Thursday night.", "Weekly"}, {"VS strategy", "Calls push or save for each Duel League week.", "Weekly"}}},
		{"Diplomacy", [][3]string{{"Ally relations", "Keeps the NAP list and agreements current.", "Weekly"}, {"Recruiting", "Works the prospect list and greets transfers.", "Daily"}}},
		{"Roster", [][3]string{{"Imports", "Uploads VS and power screenshots after reset.", "Daily"}, {"Accountability", "Reviews strike suggestions from event boards.", "Weekly"}}},
	}
	k := 0
	for i, c := range cats {
		cid := s.exec(`INSERT INTO oc_categories (name, display_order) VALUES (?, ?)`, c.name, i)
		for j, it := range c.items {
			rid := s.exec(`INSERT INTO oc_responsibilities (category_id, name, description, frequency, display_order) VALUES (?, ?, ?, ?, ?)`,
				cid, it[0], it[1], it[2], j)
			for a := 0; a < 2; a++ {
				s.exec(`INSERT INTO oc_assignees (responsibility_id, member_id) VALUES (?, ?)`, rid, leaders[k%len(leaders)].ID)
				k++
			}
		}
	}
}

// storedName is a sample file's name on disk.
func storedName(f File) string { return fmt.Sprintf("demo_%d%s", f.ID, f.Ext) }

// files writes the four sample files to UploadsDir and their rows, plus two tags.
func (s *seeder) files() {
	if s.err != nil {
		return
	}
	for _, f := range sampleFiles {
		data, err := s.fileContent(f)
		if err != nil {
			s.err = fmt.Errorf("sample file %s: %w", f.Title, err)
			return
		}
		if err := os.WriteFile(filepath.Join(s.o.UploadsDir, storedName(f)), data, 0644); err != nil {
			s.err = err
			return
		}
		id := s.exec(`INSERT INTO files (title, file_name, file_type, min_rank, min_edit_rank, owner_user_id, created_at, updated_at, updated_by)
			VALUES (?, ?, ?, 'R1', 'R4', ?, ?, ?, ?)`, f.Title, storedName(f), f.Kind, r5User, s.ts(float64(30-5*f.ID)), s.ts(float64(10-2*f.ID)), r4User)
		if s.err == nil && int(id) != f.ID {
			s.err = fmt.Errorf("file %q got id %d, want %d", f.Title, id, f.ID)
		}
	}
	rules := s.exec(`INSERT INTO file_tags (name, min_rank, color, sort_order, created_at) VALUES ('Rules', 'R1', 'purple', 0, ?)`, s.ts(30))
	events := s.exec(`INSERT INTO file_tags (name, min_rank, color, sort_order, created_at) VALUES ('Events', 'R1', 'info', 1, ?)`, s.ts(30))
	s.exec(`INSERT INTO file_tag_map (file_id, tag_id) VALUES (1, ?), (2, ?), (4, ?)`, rules, events, events)
}

func (s *seeder) fileContent(f File) ([]byte, error) {
	switch f.Ext {
	case ".docx":
		return ooxml.Docx([]string{
			"# " + AllianceName + " — Alliance Rules",
			"1. Reach the VS minimum every week. Below it twice in a month is a strike.",
			"2. Defend your base in every Zombie Siege. A zero on the board is a strike unless you told an officer beforehand.",
			"3. Sign up for Desert Storm by Thursday and show up for your slot.",
			"4. Never attack an ally's truck. The ally list is on the Allies page.",
			"5. Questions go to any R4. Be kind in alliance chat.",
		})
	case ".xlsx":
		return ooxml.Xlsx("VS Schedule", [][]string{
			{"Day", "Theme", "Focus", "Save for"},
			{"Monday", "Radar Training", "Radar tasks, stamina", "Drone parts"},
			{"Tuesday", "Base Expansion", "Construction speed-ups", "Building"},
			{"Wednesday", "Age of Science", "Research speed-ups", "Tech"},
			{"Thursday", "Train Heroes", "Hero EXP, recruitment", "Recruitment tickets"},
			{"Friday", "Total Mobilization", "Every speed-up", "Everything"},
			{"Saturday", "Enemy Buster", "Kills, healing", "Shields up"},
		})
	case ".csv":
		var b bytes.Buffer
		w := csv.NewWriter(&b)
		w.Write([]string{"Name", "Rank", "Power"})
		for _, m := range topBy(s.ds.activeMembers(), 20, func(m member) int64 { return m.Power }) {
			w.Write([]string{m.Name, m.Rank, strconv.FormatInt(m.Power, 10)})
		}
		w.Flush()
		return b.Bytes(), w.Error()
	case ".png":
		return layoutPNG()
	}
	return nil, fmt.Errorf("no generator for %s", f.Ext)
}

// layoutPNG draws a schematic Desert Storm map: buildings as blocks on a sand field.
func layoutPNG() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, 480, 320))
	fill := func(x0, y0, x1, y1 int, c color.RGBA) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				img.Set(x, y, c)
			}
		}
	}
	fill(0, 0, 480, 320, color.RGBA{0xe8, 0xd5, 0xa9, 0xff})
	hospital := color.RGBA{0xd9, 0x48, 0x48, 0xff}
	refinery := color.RGBA{0x3b, 0x3b, 0x3b, 0xff}
	silo := color.RGBA{0x66, 0x7e, 0xea, 0xff}
	for i, p := range [][2]int{{40, 40}, {400, 40}, {40, 240}, {400, 240}} {
		_ = i
		fill(p[0], p[1], p[0]+40, p[1]+40, hospital)
	}
	fill(120, 140, 170, 180, refinery)
	fill(310, 140, 360, 180, refinery)
	fill(210, 120, 270, 200, silo)
	var b bytes.Buffer
	err := png.Encode(&b, img)
	return b.Bytes(), err
}

// lastRankQueue writes a few open review decisions, so the Dashboard card and the
// Members panel have a queue to show (the demo blocks every route that reaches LastRank).
func (s *seeder) lastRankQueue() {
	r3 := s.ds.byRank("R3")
	r4 := s.ds.byRank("R4")
	add := func(kind string, m member, current, proposed, reason string) {
		s.exec(`INSERT INTO lastrank_pending_changes (kind, subject_key, member_id, lastrank_public_id, lastrank_name,
			current_value, proposed_value, fingerprint, reason, capture_date, status, first_seen_at, last_seen_at)
			VALUES (?, ?, ?, 0, ?, ?, ?, ?, ?, ?, 'open', ?, ?)`,
			kind, "m:"+strconv.Itoa(m.ID), m.ID, m.Name, current, proposed,
			kind+"|"+strconv.Itoa(m.ID)+"|"+proposed, reason, s.ts(0.4), s.ts(2), s.ts(0.4))
	}
	add("rank", r3[3], "R3", "R4", "")
	add("rank", r4[8], "R4", "R3", "")
	add("name", r3[11], r3[11].Name, r3[11].Name+"X", "")
	add("archive", r3[20], "R3", "EX", "Not on LastRank's roster for this alliance")
}

// activity: about eighty rows over the last month, so the Activity page has a history.
func (s *seeder) activity() {
	r := stream("activity")
	act := s.ds.activeMembers()
	users := s.ds.accounts()[:4] // demo-admin, demo-r5, demo-r4, demo-r3
	kinds := []struct {
		action, entity string
		sensitive      bool
		name           func() string
		details        string
	}{
		{"updated", "member", false, func() string { return act[r.IntN(len(act))].Name }, "rank: R3 → R4"},
		{"imported", "vs_points", false, func() string { return "Week of " + s.monday(r.IntN(4)) }, "86 members"},
		{"created", "schedule", false, func() string { return "Officer Meeting" }, ""},
		{"created", "accountability_strike", false, func() string { return act[r.IntN(len(act))].Name }, ""},
		{"created", "participation_board", false, func() string { return "Zombie Siege" }, ""},
		{"created", "train_log", false, func() string { return act[r.IntN(len(act))].Name }, ""},
		{"updated", "storm_group", false, func() string { return "Silo Strike" }, ""},
		{"created", "prospect", false, func() string { return composeName(r) }, ""},
		{"updated", "settings", true, func() string { return "Alliance settings" }, "vs_minimum_points: 2000000 → 2500000"},
		{"updated", "permissions", true, func() string { return "R3" }, "view_activity: off → on"},
		{"created", "file", false, func() string { return "VS Schedule" }, ""},
		{"imported", "power_records", false, func() string { return "Power ranking" }, "OCR, 86 members"},
	}
	for i := 0; i < 80; i++ {
		k := kinds[r.IntN(len(kinds))]
		u := users[r.IntN(len(users))]
		at := s.ts(30 * float64(80-i) / 80)
		s.exec(`INSERT INTO activity_log (user_id, username, action, entity_type, entity_name, entity_count, is_sensitive, created_at, updated_at, details)
			VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?, ?)`, u.UserID, u.Username, k.action, k.entity, k.name(), k.sensitive, at, at, k.details)
	}
}
