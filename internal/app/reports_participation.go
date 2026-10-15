// reports_participation.go — the participation grid (48): members against every
// recorded participation board in a date range, plus a VS column.
//
// Boards only. A cell is what deriveBoard says that board says about the member, and a
// blank means the board says nothing about them; an occurrence nobody recorded says
// nothing about anyone, so there is no "missing boards" count anywhere (CLAUDE.md →
// Participation). VS is the one event with daily data, so it rides along as a column
// from the VS-misses report over the same range, labelled as VS, not as a board.
//
// Part of the reporting layer (CLAUDE.md → Reporting layer).

package app

import (
	"sort"
	"time"
)

type participationGridParams struct {
	Range reportRangeParams `json:"range"`
}

// gridBoard is one column: a recorded board in the range.
type gridBoard struct {
	EventID     int    `json:"event_id"`
	EventDate   string `json:"event_date"`
	TypeShort   string `json:"type_short"`
	TypeName    string `json:"type_name"`
	TypeIcon    string `json:"type_icon"`
	AbsenceRule string `json:"absence_rule"`
	TaskForce   string `json:"task_force"` // Desert Storm A/B, which battle on one date
	Entries     int    `json:"entries"`
	Missed      int    `json:"missed"` // missed, plus 'zero' (that rule's miss)
	Excused     int    `json:"excused"`
}

// gridCell is one member's standing on one board.
type gridCell struct {
	Status string `json:"status"` // present | zero | missed | excused
	Rank   *int   `json:"rank,omitempty"`
	Score  *int64 `json:"score,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type gridVS struct {
	DaysEvaluated int `json:"days_evaluated"`
	Misses        int `json:"misses"`
	NoScoreDays   int `json:"no_score_days"`
}

type gridMember struct {
	MemberID int              `json:"member_id"`
	Name     string           `json:"name"`
	Rank     string           `json:"rank"`
	Cells    map[int]gridCell `json:"cells"` // by event id; absent = the board says nothing about them
	Boards   int              `json:"boards"`
	Missed   int              `json:"missed"`
	Excused  int              `json:"excused"`
	VS       *gridVS          `json:"vs"` // nil for a former member, whom the VS report leaves out
}

type participationGridCoverage struct {
	Boards              int              `json:"boards"`
	MembersWithAnyBoard int              `json:"members_with_any_board"`
	VS                  vsMissesCoverage `json:"vs"`
}

type participationGridReport struct {
	From        string                    `json:"from"`
	To          string                    `json:"to"`
	GeneratedAt string                    `json:"generated_at"`
	Boards      []gridBoard               `json:"boards"`
	Members     []gridMember              `json:"members"`
	Coverage    participationGridCoverage `json:"coverage"`
}

// runParticipationGrid lists the active roster, plus anyone a board in range speaks
// about (a former member keeps their row, flagged by rank), against every recorded
// board in [from, to], newest first. Sorted by missed, then VS misses, then name.
func runParticipationGrid(q rowQueryer, p participationGridParams, now time.Time) (*participationGridReport, error) {
	rng, err := resolveReportRange(q, p.Range, now)
	if err != nil {
		return nil, err
	}
	store, err := loadPtStore(q)
	if err != nil {
		return nil, err
	}
	boards, err := loadBoards(q, store.Types, boardFilter{Recorded: true, From: rng.From, To: rng.To})
	if err != nil {
		return nil, err
	}
	vs, err := runVSMissesReport(q, vsMissesParams{Range: reportRangeParams{From: rng.From, To: rng.To}}, now)
	if err != nil {
		return nil, err
	}

	rep := &participationGridReport{
		From: rng.From, To: rng.To, GeneratedAt: now.UTC().Format(time.RFC3339),
		Boards: []gridBoard{}, Members: []gridMember{},
		Coverage: participationGridCoverage{VS: vs.Coverage},
	}
	members := map[int]*gridMember{}
	member := func(id int, name, rank string) *gridMember {
		if m := members[id]; m != nil {
			return m
		}
		m := &gridMember{MemberID: id, Name: name, Rank: rank, Cells: map[int]gridCell{}}
		members[id] = m
		return m
	}
	ranks := map[int]string{}
	for _, r := range store.Roster {
		ranks[r.ID] = r.Rank
		if r.Rank != "EX" {
			member(r.ID, r.Name, r.Rank)
		}
	}

	for _, bd := range boards {
		if bd.Type == nil || bd.Board == nil {
			continue
		}
		col := gridBoard{EventID: bd.Event.ID, EventDate: bd.Event.EventDate, TypeShort: bd.Event.TypeShort,
			TypeName: bd.Event.TypeName, TypeIcon: bd.Event.TypeIcon, AbsenceRule: bd.Type.AbsenceRule,
			Entries: len(bd.Entries)}
		if bd.Event.TaskForce != nil {
			col.TaskForce = *bd.Event.TaskForce
		}
		primary := bd.Type.primaryKey()
		for _, st := range bd.derive(store.Roster) {
			if st.MemberID <= 0 {
				continue // a name the board carries that matches no member
			}
			cell := gridCell{Status: st.Status, Rank: st.Rank, Reason: st.Reason}
			if v, ok := st.Values[primary]; ok {
				cell.Score = v
			}
			m := member(st.MemberID, st.Name, ranks[st.MemberID])
			m.Cells[col.EventID] = cell
			m.Boards++
			switch st.Status {
			case "missed", "zero":
				m.Missed++
				col.Missed++
			case "excused":
				m.Excused++
				col.Excused++
			}
		}
		rep.Boards = append(rep.Boards, col)
	}

	vsBy := map[int]vsMissesMember{}
	for _, m := range vs.Members {
		vsBy[m.MemberID] = m
	}
	for _, m := range members {
		if v, ok := vsBy[m.MemberID]; ok {
			m.VS = &gridVS{DaysEvaluated: v.DaysEvaluated, Misses: v.Misses, NoScoreDays: v.NoScoreDays}
		} else if m.Rank != "EX" {
			m.VS = &gridVS{} // active, but no VS day in range applied to them
		}
		if m.Boards > 0 {
			rep.Coverage.MembersWithAnyBoard++
		}
		rep.Members = append(rep.Members, *m)
	}
	vsMisses := func(m gridMember) int {
		if m.VS == nil {
			return 0
		}
		return m.VS.Misses
	}
	sort.Slice(rep.Members, func(i, j int) bool {
		a, b := rep.Members[i], rep.Members[j]
		if a.Missed != b.Missed {
			return a.Missed > b.Missed
		}
		if vsMisses(a) != vsMisses(b) {
			return vsMisses(a) > vsMisses(b)
		}
		return a.Name < b.Name
	})
	rep.Coverage.Boards = len(rep.Boards)
	return rep, nil
}
