// handlers_season_eligibility.go — eligibility, the reward context, exemptions and the
// transfer check (private-docs 284). The rules are eligibility.go's; this file reads the season
// once (roster, wars and attendance, week events and results, exemptions, tiers) and applies them.

package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// memberEligibility is one member's figures and their standing against every tier.
type memberEligibility struct {
	MemberID      int               `json:"member_id"`
	Name          string            `json:"name"`
	Rank          string            `json:"rank"`
	HasWars       bool              `json:"has_wars"`
	WarsAttended  int               `json:"wars_attended"`
	WarsCompleted int               `json:"wars_completed"`
	WarsScheduled int               `json:"wars_scheduled"`
	OverallPct    *float64          `json:"overall_pct"`
	CoreRequired  int               `json:"core_required"`
	CoreAttended  int               `json:"core_attended"`
	Exempt        bool              `json:"exempt"`
	ExemptNote    string            `json:"exempt_note"`
	Tiers         []tierEligibility `json:"tiers"`
}

// tierSummary is a tier's slots and how many members it has assigned and eligible.
type tierSummary struct {
	Key                string `json:"key"`
	Label              string `json:"label"`
	SlotCount          int    `json:"slot_count"`
	Assigned           int    `json:"assigned"`
	Eligible           int    `json:"eligible"`
	WarsRequired       *int   `json:"wars_required"`
	RequiresCoreEvents bool   `json:"requires_core_events"`
}

// seasonExemption is one member's exemption.
type seasonExemption struct {
	ID        int    `json:"id"`
	MemberID  int    `json:"member_id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Note      string `json:"note"`
	GrantedBy string `json:"granted_by"`
	CreatedAt string `json:"created_at"`
}

type seasonEligibilityResult struct {
	Tiers      []tierSummary       `json:"tiers"`
	Members    []memberEligibility `json:"members"`
	Exemptions []seasonExemption   `json:"exemptions"`
	SeasonOver bool                `json:"season_over"`
}

func loadExemptions(q rowQueryer, seasonID int) ([]seasonExemption, error) {
	rows, err := q.Query(`SELECT x.id, x.member_id, COALESCE(m.name, '#' || x.member_id), x.kind, x.note, COALESCE(u.username, ''),
		strftime('%Y-%m-%d', x.created_at) FROM season_member_exemptions x
		LEFT JOIN members m ON m.id = x.member_id LEFT JOIN users u ON u.id = x.granted_by
		WHERE x.season_id = ? ORDER BY m.name`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []seasonExemption{}
	for rows.Next() {
		var x seasonExemption
		if err := rows.Scan(&x.ID, &x.MemberID, &x.Name, &x.Kind, &x.Note, &x.GrantedBy, &x.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// overallPcts is each member's overall standing (points over completed, unexcused weeks).
func overallPcts(q rowQueryer, s *Season, completedWeeks int) (map[int]float64, error) {
	pts, maxPts := map[string]int{}, 0
	for _, sl := range s.ScoreLevels {
		pts[sl.Key] = sl.Points
		if sl.Points > maxPts {
			maxPts = sl.Points
		}
	}
	rows, err := q.Query(`SELECT member_id, week_number, score FROM season_participation WHERE season_id = ?`, s.ID)
	if err != nil {
		return nil, err
	}
	scores := map[int]map[int]string{}
	for rows.Next() {
		var m, w int
		var k string
		if err := rows.Scan(&m, &w, &k); err != nil {
			rows.Close()
			return nil, err
		}
		if scores[m] == nil {
			scores[m] = map[int]string{}
		}
		scores[m][w] = k
	}
	rows.Close()
	out := map[int]float64{}
	for m, byWeek := range scores {
		var weeks []weekInput
		for w := 1; w <= completedWeeks; w++ {
			weeks = append(weeks, weekInput{Week: w, Score: byWeek[w], Points: pts[byWeek[w]]})
		}
		if st := computeSeasonStanding(s, standingInputs{Weeks: weeks, MaxWeekPts: maxPts}); st.Overall.Applicable {
			out[m] = st.Overall.Pct
		}
	}
	return out, nil
}

// computeSeasonEligibility reads the season and judges every member of its roster.
func computeSeasonEligibility(q rowQueryer, s *Season, now time.Time) (seasonEligibilityResult, error) {
	var res seasonEligibilityResult
	roster, err := loadSeasonRoster(q, s)
	if err != nil {
		return res, err
	}
	wars, err := loadSeasonWars(q, s.ID)
	if err != nil {
		return res, err
	}
	att, err := loadWarAttendance(q, s.ID)
	if err != nil {
		return res, err
	}
	events, err := loadWeekEvents(q, s.ID)
	if err != nil {
		return res, err
	}
	entries, err := loadWeekEventEntries(q, s.ID)
	if err != nil {
		return res, err
	}
	exemptions, err := loadExemptions(q, s.ID)
	if err != nil {
		return res, err
	}
	completedWeeks := completedSeasonWeeks(s, now)
	pcts, err := overallPcts(q, s, completedWeeks)
	if err != nil {
		return res, err
	}
	assigned := map[string]int{}
	arows, err := q.Query(`SELECT reward_tier, COUNT(*) FROM season_rewards WHERE season_id = ? GROUP BY reward_tier`, s.ID)
	if err != nil {
		return res, err
	}
	for arows.Next() {
		var k string
		var n int
		arows.Scan(&k, &n)
		assigned[k] = n
	}
	arows.Close()

	counts, completed, _ := warCountsByMember(wars, att, gametimeDateOf(now))
	results := map[int]map[int]string{}
	for _, x := range entries {
		if results[x.MemberID] == nil {
			results[x.MemberID] = map[int]string{}
		}
		results[x.MemberID][x.EventID] = x.Result
	}
	exempt := map[int]seasonExemption{}
	for _, x := range exemptions {
		exempt[x.MemberID] = x
	}
	hasWars := len(wars) > 0
	res.SeasonOver = s.ArchivedAt != "" || (hasWars && s.KeyEventTotal > 0 && completed >= s.KeyEventTotal)
	eligibleBy := map[string]int{}
	res.Members = []memberEligibility{}
	for _, m := range roster {
		if m.Rank == "EX" && s.ArchivedAt == "" {
			continue
		}
		c := counts[m.ID]
		if c == nil {
			c = &memberWarCounts{}
		}
		req, attended, missed, unknown := coreEventInput(events, completedWeeks, results[m.ID])
		x, isExempt := exempt[m.ID]
		in := eligibilityInput{HasWars: hasWars, WarsAttended: c.Attended, WarsCompleted: completed, WarsScheduled: s.KeyEventTotal,
			SeasonOver: res.SeasonOver, CoreRequired: req, CoreAttended: attended, CoreMissed: missed, CoreUnknown: unknown,
			Exempt: isExempt, ExemptNote: x.Note}
		me := memberEligibility{MemberID: m.ID, Name: m.Name, Rank: m.Rank, HasWars: hasWars, WarsAttended: c.Attended,
			WarsCompleted: completed, WarsScheduled: s.KeyEventTotal, CoreRequired: req, CoreAttended: attended,
			Exempt: isExempt, ExemptNote: x.Note, Tiers: []tierEligibility{}}
		if p, ok := pcts[m.ID]; ok {
			p := p
			me.OverallPct = &p
		}
		for _, t := range s.RewardTiers {
			te := evaluateTier(tierRule{Key: t.Key, Label: t.Label, WarsRequired: t.WarsRequired, RequiresCore: t.RequiresCoreEvents}, in)
			if countsAsEligible(te.Status) {
				eligibleBy[t.Key]++
			}
			me.Tiers = append(me.Tiers, te)
		}
		res.Members = append(res.Members, me)
	}
	res.Tiers = []tierSummary{}
	for _, t := range s.RewardTiers {
		res.Tiers = append(res.Tiers, tierSummary{Key: t.Key, Label: t.Label, SlotCount: t.SlotCount, Assigned: assigned[t.Key],
			Eligible: eligibleBy[t.Key], WarsRequired: t.WarsRequired, RequiresCoreEvents: t.RequiresCoreEvents})
	}
	res.Exemptions = exemptions
	return res, nil
}

func eligibilitySeason(w http.ResponseWriter, r *http.Request) *Season {
	sid, _ := strconv.Atoi(r.URL.Query().Get("season_id"))
	return landSeason(w, sid)
}

// GET /api/season-hub/eligibility?season_id= (manage_season_rewards)
func handleSeasonEligibility(w http.ResponseWriter, r *http.Request) {
	s := eligibilitySeason(w, r)
	if s == nil {
		return
	}
	res, err := computeSeasonEligibility(db, s, time.Now())
	if err != nil {
		slog.Error("handleSeasonEligibility", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

// GET /api/season-hub/rewards/context?season_id=&member_id= (manage_season_rewards) — what the
// Assign Reward modal shows beside the choice; it never pre-selects or ranks.
func handleRewardContext(w http.ResponseWriter, r *http.Request) {
	s := eligibilitySeason(w, r)
	if s == nil {
		return
	}
	mid, _ := strconv.Atoi(r.URL.Query().Get("member_id"))
	res, err := computeSeasonEligibility(db, s, time.Now())
	if err != nil {
		slog.Error("handleRewardContext", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	var me *memberEligibility
	for i := range res.Members {
		if res.Members[i].MemberID == mid {
			me = &res.Members[i]
		}
	}
	if me == nil {
		http.Error(w, "That member is not on this season's roster", http.StatusNotFound)
		return
	}
	var contrib int64
	db.QueryRow(`SELECT COALESCE(SUM(recorded_value), 0) FROM season_member_records WHERE season_id = ? AND member_id = ?
		AND week_number = 0`, s.ID, mid).Scan(&contrib)
	if contrib == 0 {
		db.QueryRow(`SELECT COALESCE(SUM(recorded_value), 0) FROM season_member_records WHERE season_id = ? AND member_id = ?`,
			s.ID, mid).Scan(&contrib)
	}
	strikes, err := seasonStrikesFor(s, mid)
	if err != nil {
		slog.Error("handleRewardContext: strikes", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"member": me, "contribution_total": contrib, "strikes": strikes, "tiers": res.Tiers,
		"elite_tier": eliteRuleTier(s)})
}

// seasonStrikesFor lists a member's applied strikes for the season (private-docs 285 gives
// strikes their season); empty until then.
var seasonStrikesFor = func(s *Season, memberID int) ([]map[string]any, error) { return []map[string]any{}, nil }

// eliteRuleTier is the tier a transferred reward's recipient is checked against: the tier keyed
// "elite", else the lowest-ranked tier with a war rule; "" when the season has neither.
func eliteRuleTier(s *Season) string {
	last := ""
	for _, t := range s.RewardTiers {
		if t.Key == "elite" {
			return t.Key
		}
		if t.WarsRequired != nil {
			last = t.Key
		}
	}
	return last
}

// transferCheck refuses a transfer to a recipient below the Elite rule unless a reason is given.
// It answers "" when the save may go ahead, else the refusal naming the failed rule.
func transferCheck(s *Season, recipientID int, from *int, note string) (string, error) {
	if from == nil {
		return "", nil
	}
	if *from == recipientID {
		return "a reward is not transferred from the member receiving it", nil
	}
	tier := eliteRuleTier(s)
	if tier == "" || strings.TrimSpace(note) != "" {
		return "", nil
	}
	res, err := computeSeasonEligibility(db, s, time.Now())
	if err != nil {
		return "", err
	}
	for _, m := range res.Members {
		if m.MemberID != recipientID {
			continue
		}
		for _, t := range m.Tiers {
			if t.Tier == tier && !countsAsEligible(t.Status) && t.Status != "not_applicable" {
				return fmt.Sprintf("%s does not meet the %s rule (%s) — a transfer to them needs an override reason",
					m.Name, tier, strings.Join(t.Reasons, "; ")), nil
			}
		}
	}
	return "", nil
}

// POST /api/season-hub/exemptions {season_id, member_id, note} (manage_season_config)
func handleExemptionCreate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var b struct {
		SeasonID int    `json:"season_id"`
		MemberID int    `json:"member_id"`
		Note     string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if ok, msg := seasonAcceptsEdits(b.SeasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM members WHERE id = ?`, b.MemberID).Scan(&name); err != nil {
		http.Error(w, "Member not found", http.StatusBadRequest)
		return
	}
	_, err := db.Exec(`INSERT INTO season_member_exemptions (season_id, member_id, kind, granted_by, note) VALUES (?, ?, 'core_events', ?, ?)
		ON CONFLICT(season_id, member_id, kind) DO UPDATE SET note = excluded.note, granted_by = excluded.granted_by`,
		b.SeasonID, b.MemberID, user.ID, strings.TrimSpace(b.Note))
	if err != nil {
		slog.Error("handleExemptionCreate", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "created", "season_exemption", name+", "+seasonTagName(&b.SeasonID), false, strings.TrimSpace(b.Note))
	writeJSON(w, map[string]string{"message": "Saved"})
}

// DELETE /api/season-hub/exemptions/{id}
func handleExemptionDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}
	var seasonID int
	var name string
	if err := db.QueryRow(`SELECT x.season_id, COALESCE(m.name, '') FROM season_member_exemptions x LEFT JOIN members m ON m.id = x.member_id
		WHERE x.id = ?`, id).Scan(&seasonID, &name); err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	} else if err != nil {
		slog.Error("handleExemptionDelete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if ok, msg := seasonAcceptsEdits(seasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	if _, err := db.Exec(`DELETE FROM season_member_exemptions WHERE id = ?`, id); err != nil {
		slog.Error("handleExemptionDelete: delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "deleted", "season_exemption", name+", "+seasonTagName(&seasonID), false)
	writeJSON(w, map[string]string{"message": "Deleted"})
}
