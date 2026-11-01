// handlers_season_poll_reviews.go — strikes' season context and war poll reviews
// (private-docs 285).
//
// A strike may carry its season, week and war. A member who voted Yes on a completed war's poll
// and did not attend is put to the officers as a review — derived on every read, never stored
// until decided: Apply (a war_poll_yes_no_show strike, manage_accountability) or Dismiss
// (manage_season_war), once per war and member. A changed attendance removes a pending review,
// never a decision. A strike never changes a weekly score.

package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

const warPollStrikeType = "war_poll_yes_no_show"

// validateStrikeSeason checks a strike's optional season context: the season exists, the week is
// one of its weeks, the war is one of its wars; a week or war needs a season.
func validateStrikeSeason(seasonID, week, warID *int) string {
	if seasonID == nil {
		if week != nil || warID != nil {
			return "a strike's week or war needs its season"
		}
		return ""
	}
	var weeks int
	if err := db.QueryRow(`SELECT week_count FROM seasons WHERE id = ?`, *seasonID).Scan(&weeks); err != nil {
		return "the season does not exist"
	}
	if week != nil && (*week < 1 || *week > weeks) {
		return fmt.Sprintf("the week must be between 1 and %d", weeks)
	}
	if warID != nil {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM season_wars WHERE id = ? AND season_id = ?`, *warID, *seasonID).Scan(&n)
		if n == 0 {
			return "the war is not one of that season's"
		}
	}
	return ""
}

// GET /api/accountability/season-options (manage_accountability) — the open seasons, their
// weeks and wars, for the strike form.
func handleStrikeSeasonOptions(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, name, season_number, week_count FROM seasons WHERE archived_at IS NULL OR archived_at = ''
		ORDER BY start_date DESC`)
	if err != nil {
		slog.Error("handleStrikeSeasonOptions", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	type warOpt struct {
		ID    int    `json:"id"`
		Label string `json:"label"`
		Week  *int   `json:"week"`
	}
	type opt struct {
		ID           int      `json:"id"`
		Name         string   `json:"name"`
		SeasonNumber int      `json:"season_number"`
		WeekCount    int      `json:"week_count"`
		Wars         []warOpt `json:"wars"`
	}
	out := []opt{}
	for rows.Next() {
		var o opt
		if err := rows.Scan(&o.ID, &o.Name, &o.SeasonNumber, &o.WeekCount); err != nil {
			rows.Close()
			slog.Error("handleStrikeSeasonOptions: scan", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		o.Wars = []warOpt{}
		out = append(out, o)
	}
	rows.Close()
	for i := range out {
		wars, err := loadSeasonWars(db, out[i].ID)
		if err != nil {
			slog.Error("handleStrikeSeasonOptions: wars", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		for _, sw := range wars {
			if sw.Status == "cancelled" {
				continue
			}
			out[i].Wars = append(out[i].Wars, warOpt{ID: sw.ID, Label: warReviewLabel(&sw), Week: sw.WeekNumber})
		}
	}
	writeJSON(w, out)
}

func warReviewLabel(w *seasonWar) string {
	l := "Attack"
	if w.Role == "defense" {
		l = "Defense"
	}
	if w.WeekNumber != nil {
		l += fmt.Sprintf(" W%d", *w.WeekNumber)
	}
	if w.BattleDate != "" {
		l += " (" + w.BattleDate + ")"
	}
	if w.OpponentTag != "" {
		l += " vs [" + w.OpponentTag + "]"
	}
	return l
}

// seasonStrikesFor lists a member's active strikes in a season (the reward context).
func init() {
	seasonStrikesFor = func(s *Season, memberID int) ([]map[string]any, error) {
		rows, err := db.Query(`SELECT s.id, s.strike_type, COALESCE(t.label, s.strike_type), s.reason, COALESCE(s.ref_date, ''), s.season_week
			FROM accountability_strikes s LEFT JOIN strike_types t ON t.key = s.strike_type
			WHERE s.season_id = ? AND s.member_id = ? AND s.status = 'active' ORDER BY s.created_at`, s.ID, memberID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id int
			var key, label, reason, ref string
			var week sql.NullInt64
			if err := rows.Scan(&id, &key, &label, &reason, &ref, &week); err != nil {
				return nil, err
			}
			m := map[string]any{"id": id, "type": key, "label": label, "reason": reason, "ref_date": ref}
			if week.Valid {
				m["week"] = week.Int64
			}
			out = append(out, m)
		}
		return out, rows.Err()
	}
}

// seasonStrikeCounts is each member's active strikes in a season (Rankings, the grid filter).
func seasonStrikeCounts(q rowQueryer, seasonID int) (map[int]int, error) {
	rows, err := q.Query(`SELECT member_id, COUNT(*) FROM accountability_strikes WHERE season_id = ? AND status = 'active'
		GROUP BY member_id`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]int{}
	for rows.Next() {
		var m, n int
		if err := rows.Scan(&m, &n); err != nil {
			return nil, err
		}
		out[m] = n
	}
	return out, rows.Err()
}

// pollReview is one pending review: a Yes vote and no attendance on a completed war.
type pollReview struct {
	WarID      int    `json:"war_id"`
	MemberID   int    `json:"member_id"`
	MemberName string `json:"member_name"`
	MemberRank string `json:"member_rank"`
	War        string `json:"war"`
	BattleDate string `json:"battle_date"`
	Role       string `json:"role"`
}

// pendingPollReviews derives the reviews still to decide for a season.
func pendingPollReviews(q rowQueryer, seasonID int, today string) ([]pollReview, error) {
	wars, err := loadSeasonWars(q, seasonID)
	if err != nil {
		return nil, err
	}
	byID := map[int]*seasonWar{}
	for i := range wars {
		if wars[i].Status == "completed" && wars[i].BattleDate != "" && wars[i].BattleDate <= today {
			byID[wars[i].ID] = &wars[i]
		}
	}
	rows, err := q.Query(`SELECT a.war_id, a.member_id, COALESCE(m.name, '#' || a.member_id), COALESCE(m.rank, '')
		FROM season_war_attendance a JOIN season_wars w ON w.id = a.war_id
		LEFT JOIN members m ON m.id = a.member_id
		WHERE w.season_id = ? AND a.poll_response = 'yes' AND a.attended = 0
		AND NOT EXISTS (SELECT 1 FROM season_poll_reviews r WHERE r.war_id = a.war_id AND r.member_id = a.member_id)
		AND NOT EXISTS (SELECT 1 FROM accountability_strikes s WHERE s.member_id = a.member_id AND s.strike_type = ?
			AND s.ref_date = w.battle_date AND (s.season_war_id IS NULL OR s.season_war_id = a.war_id))
		ORDER BY w.battle_date, m.name`, seasonID, warPollStrikeType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []pollReview{}
	for rows.Next() {
		var p pollReview
		if err := rows.Scan(&p.WarID, &p.MemberID, &p.MemberName, &p.MemberRank); err != nil {
			return nil, err
		}
		w := byID[p.WarID]
		if w == nil {
			continue
		}
		p.War, p.BattleDate, p.Role = warReviewLabel(w), w.BattleDate, w.Role
		out = append(out, p)
	}
	return out, rows.Err()
}

// GET /api/season-hub/poll-reviews?season_id= (view_season_war)
func handlePollReviewsGet(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	sid, _ := strconv.Atoi(r.URL.Query().Get("season_id"))
	reviews, err := pendingPollReviews(db, sid, gametimeDateOf(time.Now()))
	if err != nil {
		slog.Error("handlePollReviewsGet", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"reviews": reviews, "can_apply": userHasPermission(user, "manage_accountability"),
		"can_dismiss": userHasPermission(user, "manage_season_war")})
}

// POST /api/season-hub/poll-reviews {war_id, member_id, decision} — Apply needs
// manage_accountability (it writes a strike), Dismiss manage_season_war. The review is
// re-derived inside the transaction, so a second decision is refused.
func handlePollReviewDecide(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var b struct {
		WarID    int    `json:"war_id"`
		MemberID int    `json:"member_id"`
		Decision string `json:"decision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	switch b.Decision {
	case "applied":
		if !userHasPermission(user, "manage_accountability") {
			http.Error(w, "Forbidden: applying a strike needs the Accountability manage permission", http.StatusForbidden)
			return
		}
	case "dismissed":
		if !userHasPermission(user, "manage_season_war") {
			http.Error(w, "Forbidden: dismissing needs the War tab's manage permission", http.StatusForbidden)
			return
		}
	default:
		http.Error(w, "decision must be applied or dismissed", http.StatusBadRequest)
		return
	}
	var seasonID int
	if err := db.QueryRow(`SELECT season_id FROM season_wars WHERE id = ?`, b.WarID).Scan(&seasonID); err != nil {
		http.Error(w, "War not found", http.StatusNotFound)
		return
	}
	if ok, msg := seasonAcceptsEdits(seasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	today := gametimeDateOf(time.Now())
	tx, err := db.Begin()
	if err != nil {
		slog.Error("handlePollReviewDecide: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	pending, err := pendingPollReviews(tx, seasonID, today)
	if err != nil {
		slog.Error("handlePollReviewDecide: derive", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	var p *pollReview
	for i := range pending {
		if pending[i].WarID == b.WarID && pending[i].MemberID == b.MemberID {
			p = &pending[i]
		}
	}
	if p == nil {
		http.Error(w, "There is no pending review for that member and war — it has been decided, or the attendance changed", http.StatusConflict)
		return
	}
	var strikeID any
	if b.Decision == "applied" {
		role := "Attack"
		if p.Role == "defense" {
			role = "Defense"
		}
		var week sql.NullInt64
		tx.QueryRow(`SELECT week_number FROM season_wars WHERE id = ?`, p.WarID).Scan(&week)
		reason := fmt.Sprintf("Voted Yes on the %s poll and did not attend %s", role, p.War)
		res, err := tx.Exec(`INSERT INTO accountability_strikes (member_id, strike_type, reason, ref_date, created_by, season_id, season_week, season_war_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, p.MemberID, warPollStrikeType, reason, p.BattleDate, user.ID, seasonID, week, p.WarID)
		if err != nil {
			slog.Error("handlePollReviewDecide: strike", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		id, _ := res.LastInsertId()
		strikeID = id
	}
	if _, err := tx.Exec(`INSERT INTO season_poll_reviews (war_id, member_id, decision, strike_id, decided_by) VALUES (?, ?, ?, ?, ?)`,
		p.WarID, p.MemberID, b.Decision, strikeID, user.ID); err != nil {
		if isUniqueViolation(err) {
			http.Error(w, "That review has already been decided", http.StatusConflict)
			return
		}
		slog.Error("handlePollReviewDecide: review", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handlePollReviewDecide: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if b.Decision == "applied" {
		logActivity(user.ID, user.Username, "created", "accountability_strike", p.MemberName, false,
			"type: "+warPollStrikeType+" · from a war poll review ("+p.War+")")
	}
	logActivity(user.ID, user.Username, "updated", "season_poll_review", p.MemberName+", "+p.War, false, "decision: "+b.Decision)
	writeJSON(w, map[string]any{"decision": b.Decision, "strike_id": strikeID})
}
