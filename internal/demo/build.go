package demo

import (
	"math"
	"math/rand/v2"
	"sort"
)

// Identity of the fictional alliance (decision 25).
const (
	AllianceName = "Cinder Vale"
	AllianceTag  = "CNDR"
	ServerID     = 1894
)

// rankMix is the roster, in insertion order: 100 member rows.
var rankMix = []struct {
	Rank  string
	Count int
}{{"R5", 1}, {"R4", 9}, {"R3", 55}, {"R2", 15}, {"R1", 6}, {"EX", 14}}

// member is one generated roster row and everything derived from it. ID is its row id:
// members are inserted first, in this order, into an empty table, so ids are 1..N.
type member struct {
	ID         int
	Name       string
	Rank       string
	Squad      string
	Profession string
	Troop      int
	Eligible   bool
	JoinedAgo  int // days before the anchor
	Power      int64
	Hero       int64
	Kills      int64
	SquadPower int64
	HQ         int
	ProfLevel  int
	// Engagement in [0.5, 1.5] scales this member's VS days and participation.
	Engagement float64
	Notes      string
}

func (m member) active() bool { return m.Rank != "EX" }

// dataset is everything date-independent: the same for every anchor.
type dataset struct {
	Members []member
}

// build generates the roster. A pure function of the seed.
func build() *dataset {
	r := stream("roster")
	total := 0
	for _, g := range rankMix {
		total += g.Count
	}
	names := generateNames(total)
	// Shuffle names so the accent twins are not all EX members at the end.
	r.Shuffle(len(names), func(i, j int) { names[i], names[j] = names[j], names[i] })

	ds := &dataset{}
	id := 0
	for _, g := range rankMix {
		for i := 0; i < g.Count; i++ {
			u := r.Float64()
			ratio := rankPowerRatio[g.Rank]
			power := fromDeciles(powerDeciles, u) * ratio
			// Hero and squad power track power loosely; kills much more loosely.
			hero := fromDeciles(heroDeciles, clamp01(u+0.15*(r.Float64()-0.5))) * ratio
			squad := fromDeciles(squadDeciles, clamp01(u+0.2*(r.Float64()-0.5))) * ratio
			kills := fromDeciles(killDeciles, clamp01(u+0.5*(r.Float64()-0.5))) * ratio
			hq := int(math.Round(fromDeciles(hqDeciles, clamp01(u+0.1*(r.Float64()-0.5)))))
			if g.Rank == "R1" || g.Rank == "R2" {
				hq -= 1 + r.IntN(3)
			}
			if hq > hqMax {
				hq = hqMax
			}
			prof := int(math.Round(fromDeciles(professionDeciles, r.Float64())))
			if prof > professionMax {
				prof = professionMax
			}
			troop := 10
			if hq < 32 || r.Float64() < 0.1 {
				troop = 9
			}
			joined := 30 + r.IntN(330)
			if g.Rank == "R1" {
				joined = 3 + r.IntN(25) // the newest accounts
			}
			id++
			m := member{
				ID: id, Name: names[id-1], Rank: g.Rank,
				Squad: pick(r, squadTypeShare), Profession: pick(r, professionShare),
				Troop: troop, Eligible: r.Float64() > 0.02, JoinedAgo: joined,
				Power:      roundStat(r, jitter(r, power, 0.03)),
				Hero:       roundStat(r, jitter(r, hero, 0.03)),
				Kills:      roundStat(r, jitter(r, kills, 0.05)),
				SquadPower: roundStat(r, jitter(r, squad, 0.03)),
				HQ:         hq, ProfLevel: prof,
				Engagement: 0.5 + r.Float64(),
			}
			if g.Rank == "EX" {
				m.Notes = []string{"Moved server", "Inactive for a month", "Left for a friend's alliance", "Quit the game", ""}[r.IntN(5)]
			}
			ds.Members = append(ds.Members, m)
		}
	}
	return ds
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(0.999, v)) }

// byRank returns the members of one rank, in id order.
func (ds *dataset) byRank(rank string) []member {
	var out []member
	for _, m := range ds.Members {
		if m.Rank == rank {
			out = append(out, m)
		}
	}
	return out
}

// activeMembers returns every non-EX member, in id order.
func (ds *dataset) activeMembers() []member {
	var out []member
	for _, m := range ds.Members {
		if m.active() {
			out = append(out, m)
		}
	}
	return out
}

// vsScore is a member's points on one VS weekday (0 = Monday) of the week weeksAgo back.
// Deterministic per (member, week, day), so the seed and the fixtures' canned boards
// agree on every number.
func vsScore(m member, weeksAgo, day int) int64 {
	r := rand.New(rand.NewPCG(seed, uint64(m.ID)<<16|uint64(weeksAgo)<<4|uint64(day)))
	d := vsDays[day]
	if r.Float64() < d.ZeroRate*(1.6-m.Engagement*0.6) {
		return 0
	}
	// Log-normal-ish around the median, with the 90th percentile setting the spread.
	sigma := math.Log(d.P90/d.Median) / 1.2816
	z := r.NormFloat64()
	v := d.Median * math.Exp(sigma*z) * m.Engagement * rankPowerRatio[m.Rank]
	return roundStat(r, v)
}

// topBy returns up to n active members sorted by score descending, ties by id.
func topBy(ms []member, n int, score func(member) int64) []member {
	out := append([]member(nil), ms...)
	sort.SliceStable(out, func(i, j int) bool { return score(out[i]) > score(out[j]) })
	if len(out) > n {
		out = out[:n]
	}
	return out
}
