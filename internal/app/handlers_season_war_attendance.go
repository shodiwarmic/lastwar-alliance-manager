// handlers_season_war_attendance.go — war attendance and tri-state polls (private-docs 282).
//
// One row per member and war: attended (unknown, yes, no) and the in-game poll response the
// officers confirmed (yes, no, no response), entered with the weekly participation save, by CSV,
// never from a phone (officer judgement, decision 9). A war with no rows says nothing about
// anyone; a member with no row on a completed war is unknown, not absent.

package app

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// warAttendanceIn is one member's line for one war, as the week save and the CSV send it.
type warAttendanceIn struct {
	WarID        int    `json:"war_id"`
	Attended     *bool  `json:"attended"`
	PollResponse string `json:"poll_response"`
	Note         string `json:"note"`
}

// warAttendanceRow is a stored line.
type warAttendanceRow struct {
	WarID        int    `json:"war_id"`
	MemberID     int    `json:"member_id"`
	Attended     *bool  `json:"attended"`
	PollResponse string `json:"poll_response"`
	Note         string `json:"note"`
}

// participationWar is a war the weekly grid records against.
type participationWar struct {
	ID           int    `json:"id"`
	WeekNumber   *int   `json:"week_number"`
	Role         string `json:"role"`
	BattleDate   string `json:"battle_date"`
	Status       string `json:"status"`
	PollHeld     bool   `json:"poll_held"`
	OpponentTag  string `json:"opponent_tag"`
	OpponentName string `json:"opponent_name"`
}

var attendanceStatuses = map[string]bool{"planned": true, "declared": true, "confirmed": true, "completed": true}

// participationWars lists a season's wars that take attendance (planned onward, not cancelled).
func participationWars(q rowQueryer, seasonID int) ([]participationWar, bool, error) {
	wars, err := loadSeasonWars(q, seasonID)
	if err != nil {
		return nil, false, err
	}
	out := []participationWar{}
	for _, w := range wars {
		if attendanceStatuses[w.Status] {
			out = append(out, participationWar{ID: w.ID, WeekNumber: w.WeekNumber, Role: w.Role, BattleDate: w.BattleDate,
				Status: w.Status, PollHeld: w.PollHeld, OpponentTag: w.OpponentTag, OpponentName: w.OpponentName})
		}
	}
	return out, len(wars) > 0, nil
}

func loadWarAttendance(q rowQueryer, seasonID int) ([]warAttendanceRow, error) {
	rows, err := q.Query(`SELECT a.war_id, a.member_id, a.attended, a.poll_response, a.note FROM season_war_attendance a
		JOIN season_wars w ON w.id = a.war_id WHERE w.season_id = ?`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []warAttendanceRow{}
	for rows.Next() {
		var r warAttendanceRow
		var att sql.NullInt64
		if err := rows.Scan(&r.WarID, &r.MemberID, &att, &r.PollResponse, &r.Note); err != nil {
			return nil, err
		}
		if att.Valid {
			v := att.Int64 == 1
			r.Attended = &v
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func validPollResponse(s string) bool { return s == "yes" || s == "no" || s == "none" }

// writeWarAttendanceTx stores a member's line for a war; a line saying nothing (unknown, no
// response, no note) removes the row, so "nothing recorded" stays nothing.
func writeWarAttendanceTx(tx *sql.Tx, memberID int, in warAttendanceIn, userID int) (bool, error) {
	if in.PollResponse == "" {
		in.PollResponse = "none"
	}
	in.Note = strings.TrimSpace(in.Note)
	var att any
	if in.Attended != nil {
		att = boolToInt(*in.Attended)
	}
	var curAtt sql.NullInt64
	var curPoll, curNote string
	err := tx.QueryRow(`SELECT attended, poll_response, note FROM season_war_attendance WHERE war_id = ? AND member_id = ?`,
		in.WarID, memberID).Scan(&curAtt, &curPoll, &curNote)
	exists := err == nil
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	empty := in.Attended == nil && in.PollResponse == "none" && in.Note == ""
	if empty {
		if !exists {
			return false, nil
		}
		_, err := tx.Exec(`DELETE FROM season_war_attendance WHERE war_id = ? AND member_id = ?`, in.WarID, memberID)
		return err == nil, err
	}
	if exists {
		same := curPoll == in.PollResponse && curNote == in.Note &&
			((in.Attended == nil && !curAtt.Valid) || (in.Attended != nil && curAtt.Valid && (curAtt.Int64 == 1) == *in.Attended))
		if same {
			return false, nil
		}
	}
	var by any
	if userID > 0 {
		by = userID
	}
	_, err = tx.Exec(`INSERT INTO season_war_attendance (war_id, member_id, attended, poll_response, note, recorded_by, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(war_id, member_id) DO UPDATE SET attended = excluded.attended, poll_response = excluded.poll_response,
		note = excluded.note, recorded_by = excluded.recorded_by, updated_at = CURRENT_TIMESTAMP`,
		in.WarID, memberID, att, in.PollResponse, in.Note, by)
	return err == nil, err
}

// memberWarCounts is one member's war and poll figures over the completed wars to date.
type memberWarCounts struct {
	Attended       int // completed wars attended
	Known          int // completed wars whose attendance is known (the rest are unknown)
	PollsResponded int // completed wars that held a poll, answered yes or no
}

// warCountsByMember derives the figures from the season's wars and attendance rows, with the
// number of completed wars to date and of those that held a poll.
func warCountsByMember(wars []seasonWar, att []warAttendanceRow, today string) (map[int]*memberWarCounts, int, int) {
	completed, polls := map[int]bool{}, map[int]bool{}
	for _, w := range wars {
		if w.Status == "completed" && w.BattleDate != "" && w.BattleDate <= today {
			completed[w.ID] = true
			if w.PollHeld {
				polls[w.ID] = true
			}
		}
	}
	by := map[int]*memberWarCounts{}
	for _, a := range att {
		if !completed[a.WarID] {
			continue
		}
		c := by[a.MemberID]
		if c == nil {
			c = &memberWarCounts{}
			by[a.MemberID] = c
		}
		if a.Attended != nil {
			c.Attended += boolToInt(*a.Attended)
			c.Known++
		}
		if polls[a.WarID] && (a.PollResponse == "yes" || a.PollResponse == "no") {
			c.PollsResponded++
		}
	}
	return by, len(completed), len(polls)
}

// ── CSV kind ───────────────────────────────────────────────────────────────

var warRefPattern = regexp.MustCompile(`(?i)^\s*w(?:eek)?\s*(\d+)\s+(attack|defen[cs]e)\s*$`)

// resolveWarRef reads a War cell: a date ("2030-11-07", with " attack"/" defense" when the day
// holds two), or "W5 attack".
func resolveWarRef(wars []participationWar, ref string) (*participationWar, string) {
	ref = strings.TrimSpace(ref)
	var match []*participationWar
	if m := warRefPattern.FindStringSubmatch(ref); m != nil {
		week, _ := strconv.Atoi(m[1])
		role := "attack"
		if !strings.EqualFold(m[2], "attack") {
			role = "defense"
		}
		for i := range wars {
			if wars[i].WeekNumber != nil && *wars[i].WeekNumber == week && wars[i].Role == role {
				match = append(match, &wars[i])
			}
		}
	} else {
		parts := strings.Fields(ref)
		if len(parts) == 0 {
			return nil, "War is empty"
		}
		if _, err := time.Parse("2006-01-02", parts[0]); err != nil {
			return nil, "War must be a date (YYYY-MM-DD) or a week and role (W5 attack)"
		}
		role := ""
		if len(parts) > 1 {
			role = strings.ToLower(parts[1])
			if strings.HasPrefix(role, "def") {
				role = "defense"
			}
		}
		for i := range wars {
			if wars[i].BattleDate == parts[0] && (role == "" || wars[i].Role == role) {
				match = append(match, &wars[i])
			}
		}
	}
	switch len(match) {
	case 0:
		return nil, "no war of this season matches " + ref
	case 1:
		return match[0], ""
	}
	return nil, ref + " matches more than one war — add attack or defense"
}

type attendanceImport struct {
	memberID int
	in       warAttendanceIn
}

func parseTriState(v string) (*bool, string) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return nil, ""
	case "yes", "y", "1", "true", "attended":
		t := true
		return &t, ""
	case "no", "n", "0", "false", "absent":
		f := false
		return &f, ""
	}
	return nil, "Attended must be yes, no or blank"
}

func parsePollCell(v string) (string, string) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "none", "no response", "no_response", "-":
		return "none", ""
	case "yes", "y":
		return "yes", ""
	case "no", "n":
		return "no", ""
	}
	return "", "Poll must be yes, no or blank (no response)"
}

func init() {
	registerSeasonImportKind("war_attendance", importKind{
		Title:     "War attendance and polls",
		Perms:     []string{"manage_season_participation"},
		ParseInTx: true,
		Columns: []importColumn{
			{Key: "member", Label: "Member", Aliases: []string{"name", "player"}, Required: true},
			{Key: "war", Label: "War", Aliases: []string{"date", "battle"}, Required: true},
			{Key: "attended", Label: "Attended", Aliases: []string{"attendance"}},
			{Key: "poll", Label: "Poll", Aliases: []string{"poll_response", "vote"}},
			{Key: "note", Label: "Note", Aliases: []string{"notes"}},
		},
		Prepare: func(ctx *importCtx) error {
			wars, _, err := participationWars(db, ctx.Season.ID)
			ctx.Data["wars"] = wars
			return err
		},
		Parse: func(ctx *importCtx, rec map[string]string) (any, map[string]any, []string) {
			summary := map[string]any{"member": rec["member"], "war": rec["war"], "attended": rec["attended"], "poll": rec["poll"]}
			idx, _ := ctx.Data["index"].(*foldedNameIndex)
			if idx == nil {
				var err error
				if idx, err = buildFoldedNameIndex(ctx.Tx, ctx.User.ID); err != nil {
					return nil, summary, []string{"could not read the roster"}
				}
				ctx.Data["index"] = idx
			}
			m, _, err := resolveMemberAliasWithIndex(ctx.Tx, rec["member"], ctx.User.ID, idx)
			if err != nil || m == nil {
				return nil, summary, []string{"no member matches " + rec["member"]}
			}
			summary["member"] = m.Name
			war, msg := resolveWarRef(ctx.Data["wars"].([]participationWar), rec["war"])
			if msg != "" {
				return nil, summary, []string{msg}
			}
			summary["war"] = fmt.Sprintf("%s %s", war.BattleDate, war.Role)
			att, msg := parseTriState(rec["attended"])
			if msg != "" {
				return nil, summary, []string{msg}
			}
			poll, msg := parsePollCell(rec["poll"])
			if msg != "" {
				return nil, summary, []string{msg}
			}
			return attendanceImport{memberID: m.ID, in: warAttendanceIn{WarID: war.ID, Attended: att, PollResponse: poll, Note: rec["note"]}}, summary, nil
		},
		Write: func(tx *sql.Tx, ctx *importCtx, items []any) (int, int, error) {
			written, unchanged := 0, 0
			for _, it := range items {
				a := it.(attendanceImport)
				ok, err := writeWarAttendanceTx(tx, a.memberID, a.in, ctx.User.ID)
				if err != nil {
					return 0, 0, err
				}
				if ok {
					written++
				} else {
					unchanged++
				}
			}
			return written, unchanged, nil
		},
	})
	warInUseChecks = append(warInUseChecks, func(id int) string {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM season_war_attendance WHERE war_id = ?`, id).Scan(&n)
		if n > 0 {
			return fmt.Sprintf("this war has attendance recorded for %d members — cancel it instead", n)
		}
		return ""
	})
}
