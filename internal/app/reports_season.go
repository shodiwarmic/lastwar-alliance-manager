// reports_season.go — the Season Participation report (private-docs 286).
//
// One row per member of the season's roster (an archived season's frozen snapshot, decision 33):
// each week's result (or "not_scored" for a finished week nobody scored), the score counts, war
// attendance by role, poll response rates, the status engine's standing, season strikes and
// dismissed reviews, contribution, eligibility per tier and the reward with any transfer.
// Every figure comes from the same engines the Hub uses (computeSeasonStanding through
// buildSeasonMembersQ, eligibility.go); the page adds nothing.

package app

import (
	"database/sql"
	"fmt"
	"time"
)

type seasonReportParams struct {
	SeasonID int `json:"season_id"`
}

type seasonReportSeason struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	SeasonNumber int    `json:"season_number"`
	Archived     bool   `json:"archived"`
	IsActive     bool   `json:"is_active"`
}

type seasonReportMember struct {
	MemberID             int               `json:"member_id"`
	Name                 string            `json:"name"`
	Rank                 string            `json:"rank"`
	Weekly               []string          `json:"weekly"` // score key, "not_scored", or "" for a week still to come
	Full                 int               `json:"full"`
	Partial              int               `json:"partial"`
	Absent               int               `json:"absent"`
	Excused              int               `json:"excused"`
	NotScored            int               `json:"not_scored"`
	HasWars              bool              `json:"has_wars"`
	WarsAttack           int               `json:"wars_attack"`
	WarsDefense          int               `json:"wars_defense"`
	WarsAttended         int               `json:"wars_attended"`
	WarsCompleted        *int              `json:"wars_completed"` // nil on a season without war records (legacy count)
	WarsScheduled        int               `json:"wars_scheduled"`
	PollAttackPct        *float64          `json:"poll_attack_pct"`
	PollDefensePct       *float64          `json:"poll_defense_pct"`
	PollPct              *float64          `json:"poll_pct"`
	Standing             standing          `json:"standing"`
	SeasonStrikesApplied int               `json:"season_strikes_applied"`
	ReviewsDismissed     int               `json:"reviews_dismissed"`
	ContributionTotal    int64             `json:"contribution_total"`
	Eligibility          []tierEligibility `json:"eligibility"`
	RewardTier           string            `json:"reward_tier"`
	TransferredFrom      string            `json:"transferred_from"`
	TransferOverrideNote string            `json:"transfer_override_note"`
}

type seasonReport struct {
	Season         seasonReportSeason   `json:"season"`
	Seasons        []seasonReportSeason `json:"seasons"`
	Weeks          []int                `json:"weeks"`
	CompletedWeeks int                  `json:"completed_weeks"`
	ScoreLevels    []ScoreLevel         `json:"score_levels"`
	Tiers          []tierSummary        `json:"tiers"`
	Members        []seasonReportMember `json:"members"`
	InProgress     bool                 `json:"in_progress"`
	GeneratedAt    time.Time            `json:"generated_at"`
}

// runSeasonParticipationReport builds the report for a season (0: the active season, else the
// newest).
func runSeasonParticipationReport(q rowQueryer, p seasonReportParams, now time.Time) (*seasonReport, error) {
	rep := &seasonReport{GeneratedAt: now.UTC(), Seasons: []seasonReportSeason{}}
	rows, err := q.Query(`SELECT id, name, season_number, COALESCE(archived_at, ''), is_active FROM seasons ORDER BY start_date DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s seasonReportSeason
		var archived string
		if err := rows.Scan(&s.ID, &s.Name, &s.SeasonNumber, &archived, &s.IsActive); err != nil {
			rows.Close()
			return nil, err
		}
		s.Archived = archived != ""
		rep.Seasons = append(rep.Seasons, s)
	}
	rows.Close()
	sid := p.SeasonID
	if sid == 0 {
		for _, s := range rep.Seasons {
			if s.IsActive {
				sid = s.ID
				break
			}
		}
		if sid == 0 && len(rep.Seasons) > 0 {
			sid = rep.Seasons[0].ID
		}
	}
	if sid == 0 {
		rep.Members = []seasonReportMember{}
		rep.Weeks = []int{}
		return rep, nil
	}
	s, err := loadSeasonByIDQ(q, sid)
	if err == sql.ErrNoRows {
		return nil, badReportParam("that season does not exist")
	}
	if err != nil {
		return nil, err
	}
	for _, x := range rep.Seasons {
		if x.ID == sid {
			rep.Season = x
		}
	}
	rep.ScoreLevels = s.ScoreLevels
	rep.CompletedWeeks = completedSeasonWeeks(s, now)
	rep.InProgress = s.ArchivedAt == "" && rep.CompletedWeeks < s.WeekCount
	for w := 1; w <= s.WeekCount; w++ {
		rep.Weeks = append(rep.Weeks, w)
	}

	members, hasWars, err := buildSeasonMembersQ(q, s, now)
	if err != nil {
		return nil, err
	}
	elig, err := computeSeasonEligibility(q, s, now)
	if err != nil {
		return nil, err
	}
	rep.Tiers = elig.Tiers
	eligBy := map[int][]tierEligibility{}
	for _, m := range elig.Members {
		eligBy[m.MemberID] = m.Tiers
	}

	// War attendance and poll response by role, over the completed wars to date.
	wars, err := loadSeasonWars(q, s.ID)
	if err != nil {
		return nil, err
	}
	att, err := loadWarAttendance(q, s.ID)
	if err != nil {
		return nil, err
	}
	today := gametimeDateOf(now)
	done := map[int]*seasonWar{}
	heldBy := map[string]int{}
	for i := range wars {
		w := &wars[i]
		if w.Status == "completed" && w.BattleDate != "" && w.BattleDate <= today {
			done[w.ID] = w
			if w.PollHeld {
				heldBy[w.Role]++
			}
		}
	}
	type roleCounts struct{ attended, responded map[string]int }
	byMember := map[int]*roleCounts{}
	for _, a := range att {
		w := done[a.WarID]
		if w == nil {
			continue
		}
		c := byMember[a.MemberID]
		if c == nil {
			c = &roleCounts{attended: map[string]int{}, responded: map[string]int{}}
			byMember[a.MemberID] = c
		}
		if a.Attended != nil && *a.Attended {
			c.attended[w.Role]++
		}
		if w.PollHeld && (a.PollResponse == "yes" || a.PollResponse == "no") {
			c.responded[w.Role]++
		}
	}

	// Reviews dismissed, and the reward rows.
	dismissed := map[int]int{}
	drows, err := q.Query(`SELECT r.member_id, COUNT(*) FROM season_poll_reviews r JOIN season_wars w ON w.id = r.war_id
		WHERE w.season_id = ? AND r.decision = 'dismissed' GROUP BY r.member_id`, s.ID)
	if err != nil {
		return nil, err
	}
	for drows.Next() {
		var m, n int
		drows.Scan(&m, &n)
		dismissed[m] = n
	}
	drows.Close()
	type rewardRow struct{ from, note string }
	rewards := map[int]rewardRow{}
	rrows, err := q.Query(`SELECT sr.member_id, COALESCE(fm.name, ''), sr.transfer_override_note FROM season_rewards sr
		LEFT JOIN members fm ON fm.id = sr.transferred_from_member_id WHERE sr.season_id = ?`, s.ID)
	if err != nil {
		return nil, err
	}
	for rrows.Next() {
		var m int
		var r rewardRow
		rrows.Scan(&m, &r.from, &r.note)
		rewards[m] = r
	}
	rrows.Close()

	pct := func(n, d int) *float64 {
		if d == 0 {
			return nil
		}
		v := roundPct(n, d)
		return &v
	}
	rep.Members = []seasonReportMember{}
	for _, m := range members {
		row := seasonReportMember{MemberID: m.MemberID, Name: m.Name, Rank: m.Rank, Weekly: make([]string, s.WeekCount),
			HasWars: hasWars, WarsScheduled: s.KeyEventTotal, Standing: m.Standing, SeasonStrikesApplied: m.SeasonStrikes,
			ReviewsDismissed: dismissed[m.MemberID], ContributionTotal: m.ContributionTotal, RewardTier: m.RewardTier,
			TransferredFrom: rewards[m.MemberID].from, TransferOverrideNote: rewards[m.MemberID].note,
			Eligibility: eligBy[m.MemberID]}
		if row.Eligibility == nil {
			row.Eligibility = []tierEligibility{}
		}
		for i, k := range m.WeeklyScores {
			w := i + 1
			switch {
			case w > rep.CompletedWeeks && k == "":
				row.Weekly[i] = ""
			case k == "":
				row.Weekly[i] = "not_scored"
				row.NotScored++
			default:
				row.Weekly[i] = k
				switch k {
				case "full":
					row.Full++
				case "partial":
					row.Partial++
				case "absent":
					row.Absent++
				case excusedScoreKey:
					row.Excused++
				}
			}
		}
		if hasWars {
			c := byMember[m.MemberID]
			if c == nil {
				c = &roleCounts{attended: map[string]int{}, responded: map[string]int{}}
			}
			row.WarsAttack, row.WarsDefense = c.attended["attack"], c.attended["defense"]
			if m.WarsAttended != nil {
				row.WarsAttended = *m.WarsAttended
			}
			row.WarsCompleted = m.WarsCompleted
			row.PollAttackPct = pct(c.responded["attack"], heldBy["attack"])
			row.PollDefensePct = pct(c.responded["defense"], heldBy["defense"])
			row.PollPct = pct(c.responded["attack"]+c.responded["defense"], heldBy["attack"]+heldBy["defense"])
		} else {
			// A season without war records reads its legacy weekly key-event count (decision 8).
			row.WarsAttended = m.KeyEventAttendance
		}
		rep.Members = append(rep.Members, row)
	}
	return rep, nil
}

// seasonReportParamsFromQuery reads season_id (optional).
func seasonReportParamsFromQuery(v string) (seasonReportParams, error) {
	var p seasonReportParams
	if v == "" {
		return p, nil
	}
	if _, err := fmt.Sscanf(v, "%d", &p.SeasonID); err != nil || p.SeasonID < 1 {
		return p, badReportParam("season_id must be a season's id")
	}
	return p, nil
}
