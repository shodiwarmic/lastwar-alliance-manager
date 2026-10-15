// reports_growth.go — power growth, computed once (241, 251, 252).
//
// The weekly summary's top five and the Tracking page's growth table both read
// powerGrowth. Growth over a window is the latest reading minus the latest reading at or
// before the window's start; a member with no reading that old has NO growth (nil), never
// 0 and never their whole power — a new joiner's power is not a month's growth.
//
// Part of the reporting layer (CLAUDE.md → Reporting layer).

package app

import (
	"database/sql"
	"time"

	"lastwar-alliance/internal/gametime"
)

// The history tables powerGrowth reads. Fixed names, joined into SQL, never from input.
const (
	histPower = "power_history"
	histHero  = "hero_power_history"
)

// Growth windows are chosen per request, in days.
const (
	growthWindowMin          = 1
	growthWindowMax          = 365
	growthWindowShortDefault = 7
	growthWindowLongDefault  = 30
)

type growthParams struct {
	Table      string    // histPower or histHero
	WindowDays int       // growthWindowMin..growthWindowMax
	AsOf       time.Time // the window ends here
}

// memberGrowth is one active member's growth over a window. Current/Baseline are nil
// without a reading; Growth is set only when both are. The *At fields are display-only.
type memberGrowth struct {
	MemberID   int
	Name       string
	Rank       string
	TroopLevel int
	SquadType  string
	Current    *int64
	CurrentAt  string
	Baseline   *int64
	BaselineAt string
	Growth     *int64
}

// powerGrowth returns every active member (rank != 'EX' — the one population, 252), by
// current reading descending. Each endpoint is chosen by id, newest recorded_at then
// newest id, so two readings at one second can never multiply a member's row. The cutoff
// is in UTC: recorded_at is written in UTC (CURRENT_TIMESTAMP, lastRankCaptureToSQLite).
func powerGrowth(q rowQueryer, p growthParams) ([]memberGrowth, error) {
	if p.Table != histPower && p.Table != histHero {
		return nil, badReportParam("unknown history table")
	}
	if p.WindowDays < growthWindowMin || p.WindowDays > growthWindowMax {
		return nil, badReportParam("the growth window must be 1 to 365 days")
	}
	cutoff := p.AsOf.UTC().AddDate(0, 0, -p.WindowDays).Format(gametime.SQLiteLayout)
	t := p.Table
	rows, err := q.Query(`
		SELECT m.id, m.name, m.rank, COALESCE(m.troop_level, 0), COALESCE(m.squad_type, ''),
		       cur.power, cur.recorded_at, base.power, base.recorded_at
		FROM members m
		LEFT JOIN `+t+` cur ON cur.id = (SELECT id FROM `+t+` WHERE member_id = m.id
			ORDER BY recorded_at DESC, id DESC LIMIT 1)
		LEFT JOIN `+t+` base ON base.id = (SELECT id FROM `+t+` WHERE member_id = m.id AND recorded_at <= ?
			ORDER BY recorded_at DESC, id DESC LIMIT 1)
		WHERE m.rank != 'EX'
		ORDER BY cur.power DESC, m.name`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []memberGrowth{}
	for rows.Next() {
		var g memberGrowth
		var cur, base sql.NullInt64
		var curAt, baseAt sql.NullString
		if err := rows.Scan(&g.MemberID, &g.Name, &g.Rank, &g.TroopLevel, &g.SquadType,
			&cur, &curAt, &base, &baseAt); err != nil {
			return nil, err
		}
		if cur.Valid {
			v := cur.Int64
			g.Current, g.CurrentAt = &v, curAt.String
		}
		if base.Valid {
			v := base.Int64
			g.Baseline, g.BaselineAt = &v, baseAt.String
		}
		if g.Current != nil && g.Baseline != nil {
			d := *g.Current - *g.Baseline
			g.Growth = &d
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GrowthData is one row of the Tracking page's growth table (GET /api/rankings). Every
// figure is null when there is no reading to compute it from.
type GrowthData struct {
	MemberID         int    `json:"member_id"`
	Name             string `json:"name"`
	Rank             string `json:"rank"`
	TroopLevel       int    `json:"troop_level"`
	SquadType        string `json:"squad_type"`
	CurrentPower     *int64 `json:"current_power"`
	PowerShort       *int64 `json:"power_short"`
	PowerLong        *int64 `json:"power_long"`
	GrowthShort      *int64 `json:"growth_short"`
	GrowthLong       *int64 `json:"growth_long"`
	CurrentHeroPower *int64 `json:"current_hero_power"`
	HeroPowerShort   *int64 `json:"hero_power_short"`
	HeroPowerLong    *int64 `json:"hero_power_long"`
	HeroGrowthShort  *int64 `json:"hero_growth_short"`
	HeroGrowthLong   *int64 `json:"hero_growth_long"`
}

type growthTableParams struct {
	ShortDays int `json:"short"`
	LongDays  int `json:"long"`
}

type growthTableReport struct {
	ShortDays  int          `json:"short_days"`
	LongDays   int          `json:"long_days"`
	GrowthData []GrowthData `json:"growth_data"`
}

// runGrowthTableReport is the Tracking page's table: power and hero power over a short
// and a long window, every active member, by current power.
func runGrowthTableReport(q rowQueryer, p growthTableParams, now time.Time) (*growthTableReport, error) {
	run := func(table string, days int) (map[int]memberGrowth, []memberGrowth, error) {
		list, err := powerGrowth(q, growthParams{Table: table, WindowDays: days, AsOf: now})
		if err != nil {
			return nil, nil, err
		}
		by := make(map[int]memberGrowth, len(list))
		for _, g := range list {
			by[g.MemberID] = g
		}
		return by, list, nil
	}
	_, powerShort, err := run(histPower, p.ShortDays)
	if err != nil {
		return nil, err
	}
	powerLong, _, err := run(histPower, p.LongDays)
	if err != nil {
		return nil, err
	}
	heroShort, _, err := run(histHero, p.ShortDays)
	if err != nil {
		return nil, err
	}
	heroLong, _, err := run(histHero, p.LongDays)
	if err != nil {
		return nil, err
	}
	rep := &growthTableReport{ShortDays: p.ShortDays, LongDays: p.LongDays, GrowthData: []GrowthData{}}
	for _, g := range powerShort { // already by current power
		l, hs, hl := powerLong[g.MemberID], heroShort[g.MemberID], heroLong[g.MemberID]
		rep.GrowthData = append(rep.GrowthData, GrowthData{
			MemberID: g.MemberID, Name: g.Name, Rank: g.Rank, TroopLevel: g.TroopLevel, SquadType: g.SquadType,
			CurrentPower: g.Current, PowerShort: g.Baseline, PowerLong: l.Baseline,
			GrowthShort: g.Growth, GrowthLong: l.Growth,
			CurrentHeroPower: hs.Current, HeroPowerShort: hs.Baseline, HeroPowerLong: hl.Baseline,
			HeroGrowthShort: hs.Growth, HeroGrowthLong: hl.Growth,
		})
	}
	return rep, nil
}
