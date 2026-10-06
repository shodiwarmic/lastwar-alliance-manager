package demo

import (
	"fmt"
	"math"
)

// settings is the one table the seed UPDATEs: migration 054 creates the singleton on every
// migrate. pwd_validity_days = 0 because a frozen anchor would otherwise make every
// screenshot login a forced password change once password_changed_at is 180 days old.
func (s *seeder) settings() {
	mode := "cloud"
	if s.o.WorkerURL != "" {
		mode = "local"
	}
	s.exec(`UPDATE settings SET alliance_name = ?, alliance_tag = ?, our_server_id = ?,
		pwd_validity_days = 0, cv_worker_url = ?, ocr_backend_mode = ?,
		login_message = '', join_requirements = ?, vs_minimum_points = ?
		WHERE id = 1`,
		AllianceName, AllianceTag, ServerID, s.o.WorkerURL, mode,
		"HQ 30+, 150M power, active in VS and Desert Storm.", 2_500_000)
	// Event level ceilings, as an operator sets them under Settings → Game Limits; the
	// migrations seed a ceiling of the baseline, which would put every event at one level.
	for _, c := range []struct {
		short string
		max   int
	}{{"MG", 12}, {"ZS", 30}, {"LS", 40}} {
		s.exec(`UPDATE schedule_event_types SET max_level = ? WHERE short_name = ?`, c.max, c.short)
	}
}

func (s *seeder) members() {
	for _, m := range s.ds.Members {
		id := s.exec(`INSERT INTO members (name, rank, eligible, squad_type, troop_level, profession, notes, leave_reason, joined_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.Name, m.Rank, m.Eligible, m.Squad, m.Troop, m.Profession, "", m.Notes, s.date(m.JoinedAgo))
		if s.err == nil && int(id) != m.ID {
			s.err = fmt.Errorf("member %q got id %d, want %d — the members table was not empty", m.Name, id, m.ID)
		}
	}
}

func (s *seeder) users() {
	for _, a := range s.ds.accounts() {
		var memberID any
		if a.MemberID != 0 {
			memberID = a.MemberID
		}
		id := s.exec(`INSERT INTO users (username, password, member_id, is_admin, force_password_change, password_changed_at, is_active)
			VALUES (?, ?, ?, ?, 0, ?, 1)`,
			a.Username, s.hash, memberID, a.IsAdmin, s.ts(1))
		if s.err == nil && int(id) != a.UserID {
			s.err = fmt.Errorf("user %q got id %d, want %d — the users table was not empty", a.Username, id, a.UserID)
		}
	}
}

// aliases: thirty, mixing the three categories. OCR aliases are the misreads a scan
// produces (accents dropped, a letter doubled); global ones are nicknames; the personal
// ones belong to demo-admin.
func (s *seeder) aliases() {
	r := stream("aliases")
	act := s.ds.activeMembers()
	used := map[int]bool{}
	for i := 0; i < 30; i++ {
		m := act[r.IntN(len(act))]
		if used[m.ID] {
			i--
			continue
		}
		used[m.ID] = true
		var alias, category string
		var user any
		switch {
		case i < 16:
			alias, category = ocrMisread(m.Name, i), "ocr"
		case i < 26:
			runes := []rune(m.Name)
			alias, category = string(runes[:max(3, len(runes)-2)]), "global"
		default:
			alias, category, user = "my "+lower(m.Rank)+" "+fmt.Sprint(i-25), "personal", adminUser
		}
		if alias == m.Name {
			alias += "1"
		}
		s.exec(`INSERT INTO member_aliases (member_id, user_id, alias, category) VALUES (?, ?, ?, ?)`,
			m.ID, user, alias, category)
	}
	// Medical aid, the one tracked skill, on a handful of members.
	for i, m := range act {
		if i%7 == 3 {
			s.exec(`INSERT INTO member_skills (member_id, skill_key, recorded_by, recorded_at) VALUES (?, 'medical_aid', ?, ?)`,
				m.ID, r4User, s.ts(float64(10+i)))
		}
	}
}

// ocrMisread is a plausible OCR misreading of a name.
func ocrMisread(name string, i int) string {
	f := foldKey(name)
	if f != lower(name) {
		return f // accents lost — the commonest misread
	}
	runes := []rune(name)
	switch i % 3 {
	case 0:
		return string(runes) + "."
	case 1:
		for j, c := range runes {
			if c == 'l' || c == 'I' {
				runes[j] = '1'
				return string(runes)
			}
		}
		return string(runes) + "_"
	default:
		return string(runes[:len(runes)-1]) + string(runes[len(runes)-1]) + string(runes[len(runes)-1])
	}
}

// historyWeeks is how many weekly datapoints each history series carries.
const historyWeeks = 12

// history writes 12 weekly points per active member into every stat history table,
// walking back from the member's current figure at the measured growth rate. Sources mix
// the import paths so the provenance column has something to show.
func (s *seeder) history() {
	r := stream("history")
	sources := []string{"ocr", "ocr", "csv", "manual", "mobile"}
	for _, m := range s.ds.Members {
		weeks := historyWeeks
		offset := 0
		if !m.active() {
			// A former member's series stops when they left.
			offset = 14 + r.IntN(40)
			weeks = 6
		}
		for k := 0; k < weeks; k++ {
			days := float64(offset+7*k) + r.Float64()*1.5
			if days > float64(m.JoinedAgo) {
				break
			}
			grow := math.Pow(1+weeklyPowerGrowth, float64(k))
			src := sources[r.IntN(len(sources))]
			at := s.ts(days + 0.1)
			s.exec(`INSERT INTO power_history (member_id, power, recorded_at, source) VALUES (?, ?, ?, ?)`,
				m.ID, int64(float64(m.Power)/grow*(1+0.004*r.NormFloat64())), at, src)
			s.exec(`INSERT INTO hero_power_history (member_id, power, recorded_at, source) VALUES (?, ?, ?, ?)`,
				m.ID, int64(float64(m.Hero)/grow*(1+0.004*r.NormFloat64())), at, src)
			s.exec(`INSERT INTO squad_power_history (member_id, power, recorded_at, source) VALUES (?, ?, ?, ?)`,
				m.ID, int64(float64(m.SquadPower)/grow), at, src)
			// Kills only ever rise, and faster than power.
			s.exec(`INSERT INTO kill_history (member_id, kills, recorded_at, source) VALUES (?, ?, ?, ?)`,
				m.ID, int64(float64(m.Kills)/math.Pow(1.04, float64(k))), at, src)
			if k%4 == 0 {
				hq := m.HQ - k/8
				s.exec(`INSERT INTO hq_level_history (member_id, hq_level, source, recorded_at) VALUES (?, ?, ?, ?)`,
					m.ID, hq, src, at)
				s.exec(`INSERT INTO profession_level_history (member_id, profession_level, source, recorded_at) VALUES (?, ?, ?, ?)`,
					m.ID, max(1, m.ProfLevel-k/4), src, at)
			}
		}
	}
}

// vsWeeks is how many VS weeks the VS page carries, the current one included.
const vsWeeks = 8

// vsPoints writes eight weeks for every active member. The current week has only the days
// that have finished (game time), as a real install would on that day.
func (s *seeder) vsPoints() {
	done := s.todayWeekday() // Mon = 0 days finished … Sun = 6
	for w := 0; w < vsWeeks; w++ {
		days := 6
		if w == 0 {
			days = min(done, 6)
			if days == 0 {
				continue // Monday: the week has no finished day yet
			}
		}
		for _, m := range s.ds.activeMembers() {
			if m.JoinedAgo < 7*w {
				continue
			}
			var d [6]int64
			for i := 0; i < days; i++ {
				d[i] = vsScore(m, w, i)
			}
			at := s.ts(float64(7*w) + 0.2)
			s.exec(`INSERT INTO vs_points (member_id, week_date, monday, tuesday, wednesday, thursday, friday, saturday, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				m.ID, s.monday(w), d[0], d[1], d[2], d[3], d[4], d[5], at, at)
		}
	}
}
