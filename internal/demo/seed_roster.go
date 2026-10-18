package demo

import (
	"fmt"
	"math"
	"time"

	"lastwar-alliance/internal/gametime"
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
		login_message = '', join_requirements = ?, vs_minimum_points = ?,
		donation_weekly_minimum = ?, donation_daily_minimum = ?
		WHERE id = 1`,
		AllianceName, AllianceTag, ServerID, s.o.WorkerURL, mode,
		"HQ 30+, 150M power, active in VS and Desert Storm.", 2_500_000,
		donationWeeklyMinimum, donationDailyMinimum)
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
	// The VS minimum as recorded from the earliest seeded week, so the demo's reports
	// judge every week against a recorded minimum. Migration 085 seeds a row only when
	// vs_points has rows, which it never does before the seed; the delete keeps the
	// fingerprint stable should that change (its set_at is the migration's clock).
	s.exec(`DELETE FROM vs_week_minimums WHERE kind = 'setting'`)
	s.exec(`INSERT INTO vs_week_minimums (week_date, kind, minimum_points, set_at) VALUES (?, 'setting', ?, ?)`,
		s.monday(vsWeeks-1), 2_500_000, s.ts(float64(7*(vsWeeks-1))+0.1))
}

// donations writes, for every active member, both donation halves on each day of the
// last complete Monday–Sunday week and on the finished days of the current one. Each day
// is read once, at 18:00 game time (20:00 UTC, clear of the reset): today is part of the
// day's amount (more was given after the reading) and the week is everything before it
// plus that part, so the app derives exact days from consecutive readings, and last
// week's Sunday reading makes its total final. The manifest's canned donation boards are
// what the demo's OCR service answers, not a mirror of this.
func (s *seeder) donations() {
	done := s.todayWeekday() // finished days of the current week
	for _, m := range s.ds.activeMembers() {
		for w := 1; w >= 0; w-- {
			days := 7
			if w == 0 {
				days = done
			}
			var before int64
			for d := 0; d < days; d++ {
				date := gametime.MondayOf(s.today).AddDate(0, 0, d-7*w)
				daysAgo := int(s.today.Sub(date).Hours() / 24)
				if m.JoinedAgo < daysAgo {
					before = 0
					continue
				}
				amount := donationDay(m, w, d)
				today := amount * 4 / 5
				at := time.Date(date.Year(), date.Month(), date.Day(), 20, 0, 0, 0, time.UTC).Format(gametime.SQLiteLayout)
				for _, h := range []struct {
					kind   string
					amount int64
				}{{"today", today}, {"week", before + today}} {
					s.exec(`INSERT INTO donation_readings (member_id, game_date, kind, amount, captured_at, source, updated_at)
						VALUES (?, ?, ?, ?, ?, ?, ?)`,
						m.ID, date.Format(gametime.DateLayout), h.kind, h.amount, at, "mobile", at)
				}
				before += amount
			}
		}
	}
	// The minimums as recorded from the earliest seeded week, as vsPoints records VS's.
	s.exec(`INSERT INTO donation_week_minimums (week_date, weekly_minimum, daily_minimum, set_at) VALUES (?, ?, ?, ?)`,
		s.monday(1), donationWeeklyMinimum, donationDailyMinimum, s.ts(float64(7+s.todayWeekday())+0.1))
}
