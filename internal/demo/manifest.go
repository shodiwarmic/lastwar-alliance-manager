package demo

import (
	"math"
	"math/rand/v2"
	"sort"
)

// Account is one seeded login.
type Account struct {
	Username string `json:"username"`
	Rank     string `json:"rank"`             // "admin" for the unlinked administrator
	Member   string `json:"member,omitempty"` // the linked member's name
	IsAdmin  bool   `json:"is_admin"`
	UserID   int    `json:"user_id"`
	MemberID int    `json:"member_id,omitempty"`
}

// File is one seeded files row.
type File struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Ext   string `json:"ext"`
	Kind  string `json:"kind"` // files.file_type: document | spreadsheet | image
}

// BoardRow is one row of a canned leaderboard.
type BoardRow struct {
	Name  string `json:"player_name"`
	Score int64  `json:"score"`
	Rank  int    `json:"rank"`
}

// ManifestData is what the fixtures service needs to answer as if it had read this
// alliance's screens and opened its files — no database required.
type ManifestData struct {
	AllianceName string                `json:"alliance_name"`
	AllianceTag  string                `json:"alliance_tag"`
	ServerID     int                   `json:"server_id"`
	Accounts     []Account             `json:"accounts"`
	Files        []File                `json:"files"`
	Boards       map[string][]BoardRow `json:"boards"`
}

// RankingCategories are the OCR contract's twenty-three ranking categories.
var RankingCategories = []string{
	"monday", "tuesday", "wednesday", "thursday", "friday", "saturday",
	"weekly", "power", "kills", "donation_daily", "donation_weekly",
	"mutual_assistance_daily", "mutual_assistance_weekly", "mutual_assistance_season",
	"siege_daily", "siege_weekly", "siege_season",
	"rare_soil_war_daily", "rare_soil_war_weekly", "rare_soil_war_season",
	"defeat_daily", "defeat_weekly", "defeat_season",
}

// MailCategories are the post-event mails (OCR service v1.0.0).
var MailCategories = []string{"alliance_exercise", "zombie_siege", "desert_storm"}

// sampleFiles is the Files page, in insertion order (ids 1..4). No .pptx: nothing in the
// tree writes one, and a presentation writer for one sample file is not worth it.
var sampleFiles = []File{
	{1, "Alliance Rules", ".docx", "document"},
	{2, "VS Schedule", ".xlsx", "spreadsheet"},
	{3, "Roster Export", ".csv", "spreadsheet"},
	{4, "Desert Storm Layout", ".png", "image"},
}

// boardSize is how many rows a canned board carries: about three screens of a ranking.
const boardSize = 30

// accounts lists the six seeded logins: an unlinked administrator, and one account per
// rank linked to that rank's first member. User ids are 1..6 in this order.
func (ds *dataset) accounts() []Account {
	out := []Account{{Username: "demo-admin", Rank: "admin", IsAdmin: true, UserID: 1}}
	for i, rank := range []string{"R5", "R4", "R3", "R2", "R1"} {
		m := ds.byRank(rank)[0]
		out = append(out, Account{
			Username: "demo-" + lower(rank), Rank: rank, Member: m.Name,
			UserID: i + 2, MemberID: m.ID,
		})
	}
	return out
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// Manifest is a pure function of the seed: the same alliance whatever the anchor date.
func Manifest() ManifestData {
	ds := build()
	md := ManifestData{
		AllianceName: AllianceName, AllianceTag: AllianceTag, ServerID: ServerID,
		Accounts: ds.accounts(), Files: append([]File(nil), sampleFiles...),
		Boards: map[string][]BoardRow{},
	}
	act := ds.activeMembers()
	board := func(score func(member) int64) []BoardRow {
		var rows []BoardRow
		for i, m := range topBy(act, boardSize, score) {
			rows = append(rows, BoardRow{Name: m.Name, Score: score(m), Rank: i + 1})
		}
		return rows
	}
	for d, cat := range RankingCategories[:6] {
		day := d
		md.Boards[cat] = board(func(m member) int64 { return vsScore(m, 0, day) })
	}
	md.Boards["weekly"] = board(func(m member) int64 {
		var t int64
		for d := 0; d < 6; d++ {
			t += vsScore(m, 1, d)
		}
		return t
	})
	md.Boards["power"] = board(func(m member) int64 { return m.Power })
	md.Boards["kills"] = board(func(m member) int64 { return m.Kills })
	// The rest are boards the app does not store (donations, the Alliance Contribution
	// tabs) or mails: scaled from engagement so the same members lead them.
	scaled := map[string]float64{
		"donation_daily": 9e3, "donation_weekly": 60e3,
		"mutual_assistance_daily": 40, "mutual_assistance_weekly": 260, "mutual_assistance_season": 1900,
		"siege_daily": 1.2e6, "siege_weekly": 8e6, "siege_season": 55e6,
		"rare_soil_war_daily": 300, "rare_soil_war_weekly": 2100, "rare_soil_war_season": 14e3,
		"defeat_daily": 9e3, "defeat_weekly": 60e3, "defeat_season": 420e3,
		"alliance_exercise": 2.4e9, "zombie_siege": 18, "desert_storm": 1.6e6,
	}
	cats := make([]string, 0, len(scaled))
	for c := range scaled {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	for _, cat := range cats {
		base := scaled[cat]
		c := cat
		md.Boards[cat] = board(func(m member) int64 { return scaledScore(m, c, base) })
	}
	return md
}

// scaledScore is a member's figure on a board the generator has no distribution for: the
// category's typical value times the member's engagement and rank, jittered per member.
func scaledScore(m member, category string, base float64) int64 {
	h := uint64(0)
	for _, c := range category {
		h = h*131 + uint64(c)
	}
	r := rand.New(rand.NewPCG(seed, h^uint64(m.ID)<<32))
	v := base * m.Engagement * rankPowerRatio[m.Rank] * (0.6 + 0.8*r.Float64())
	if base < 100 {
		return int64(math.Max(0, math.Round(v)))
	}
	return roundStat(r, v)
}
