// season_war_snapshot.go — a war keeps the figures it was judged on (private-docs 324).
//
// A war's matchup is otherwise live: each side's latest figures, read every time. Once a war is
// fought that drifts with figures taken after it, so each war keeps one snapshot — both sides'
// figures, each with its instant and source, and the matchup they gave — the way vs_league_weeks
// freezes both sides for a VS week. The owner's rules (2026-10-30):
//
//   - taken when the war is declared (ours) or confirmed (theirs): the figures the decision was
//     made on; a change of opponent while it stands takes it again;
//   - re-taken by an officer until the war is completed;
//   - a war completed with none (or for another opponent) gets one from the figures known at its
//     battle time;
//   - the card shows the live rating until the war is completed, then only the snapshot.
//
// Faction battles have no rating and no snapshot (a follow-up).

package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// warSnapshot is one war's frozen figures and matchup.
type warSnapshot struct {
	OpponentSeasonAllianceID *int          `json:"opponent_season_alliance_id"`
	Trigger                  string        `json:"trigger"`  // declared | confirmed | retake | completed
	TakenAt                  string        `json:"taken_at"` // UTC
	AsOf                     string        `json:"as_of"`    // UTC: the instant the figures are as of
	Ours                     warSide       `json:"ours"`
	Theirs                   warSide       `json:"theirs"`
	Matchup                  matchupResult `json:"matchup"`
}

// buildWarSnapshot computes a war's figures and matchup as of an instant (nil: now), on q — a
// transaction's or the pool's, nothing else.
func buildWarSnapshot(q rowQueryer, seasonID, opponentID int, asOf *time.Time) (warSnapshot, error) {
	snap := warSnapshot{OpponentSeasonAllianceID: &opponentID}
	cfg, err := loadWarConfig(q, seasonID)
	if err != nil {
		return snap, err
	}
	obs, err := loadWarObservations(q, seasonID)
	if err != nil {
		return snap, err
	}
	var rank sql.NullInt64
	if err := q.QueryRow(`SELECT group_rank FROM season_alliances WHERE id = ?`, opponentID).Scan(&rank); err != nil && err != sql.ErrNoRows {
		return snap, err
	}
	own, _, err := ownWarSideAt(q, asOf)
	if err != nil {
		return snap, err
	}
	var theirs warSide
	if rank.Valid {
		v := rank.Int64
		theirs.GroupRank = warFigure{Value: &v, Source: "season row"}
	}
	var ownObs, oppObs []warObservation
	for _, o := range obs {
		if asOf != nil && o.at.After(*asOf) {
			continue
		}
		if o.IsOwn {
			ownObs = append(ownObs, o)
		} else if o.SeasonAllianceID != nil && *o.SeasonAllianceID == opponentID {
			oppObs = append(oppObs, o)
		}
	}
	applyObservations(&own, ownObs)
	applyObservations(&theirs, oppObs)
	at := time.Now()
	if asOf != nil {
		at = *asOf
	}
	snap.AsOf = landUTC(at)
	snap.Ours, snap.Theirs = own, theirs
	snap.Matchup = computeMatchup(own.figures(), theirs.figures(), cfg)
	return snap, nil
}

// writeWarSnapshotTx replaces a war's snapshot.
func writeWarSnapshotTx(tx *sql.Tx, war *seasonWar, snap warSnapshot, userID int) error {
	ours, _ := json.Marshal(snap.Ours)
	theirs, _ := json.Marshal(snap.Theirs)
	matchup, _ := json.Marshal(snap.Matchup)
	var by any
	if userID > 0 {
		by = userID
	}
	_, err := tx.Exec(`INSERT INTO season_war_snapshots (war_id, season_id, opponent_season_alliance_id, taken_trigger, taken_at,
		as_of, taken_by, ours, theirs, matchup, score, rating) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(war_id) DO UPDATE SET opponent_season_alliance_id = excluded.opponent_season_alliance_id,
		taken_trigger = excluded.taken_trigger, taken_at = excluded.taken_at, as_of = excluded.as_of, taken_by = excluded.taken_by,
		ours = excluded.ours, theirs = excluded.theirs, matchup = excluded.matchup, score = excluded.score, rating = excluded.rating`,
		war.ID, war.SeasonID, snap.OpponentSeasonAllianceID, snap.Trigger, snap.TakenAt, snap.AsOf, by,
		string(ours), string(theirs), string(matchup), snap.Matchup.Score, snap.Matchup.Rating)
	return err
}

// loadWarSnapshots returns a season's snapshots by war id.
func loadWarSnapshots(q rowQueryer, seasonID int) (map[int]*warSnapshot, error) {
	rows, err := q.Query(`SELECT war_id, opponent_season_alliance_id, taken_trigger, taken_at, as_of, ours, theirs, matchup
		FROM season_war_snapshots WHERE season_id = ?`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]*warSnapshot{}
	for rows.Next() {
		var id int
		var s warSnapshot
		var ours, theirs, matchup string
		if err := rows.Scan(&id, &s.OpponentSeasonAllianceID, &s.Trigger, &s.TakenAt, &s.AsOf, &ours, &theirs, &matchup); err != nil {
			return nil, err
		}
		s.TakenAt, s.AsOf = storedUTC(s.TakenAt), storedUTC(s.AsOf)
		json.Unmarshal([]byte(ours), &s.Ours)
		json.Unmarshal([]byte(theirs), &s.Theirs)
		json.Unmarshal([]byte(matchup), &s.Matchup)
		out[id] = &s
	}
	return out, rows.Err()
}

// warBattleInstant is the war's battle start (ST date and time; no time is the day's end), nil
// without a date.
func warBattleInstant(w *seasonWar) *time.Time {
	if w.BattleDate == "" {
		return nil
	}
	clock := w.BattleTime
	if clock == "" {
		clock = "23:59"
	}
	t, err := parseLandInstant(w.BattleDate + " " + clock)
	if err != nil {
		return nil
	}
	return &t
}

// snapshotWarTx applies the owner's rules after a war is written (saveWar's transaction): a war
// that becomes declared or confirmed, or changes opponent while it is, is snapshotted now; one
// completed with no snapshot for its opponent gets one as of its battle. Returns the activity
// log's detail, "" when nothing was taken.
func snapshotWarTx(tx *sql.Tx, before *seasonWar, after *seasonWar, userID int) (string, error) {
	if after.OpponentSeasonAllianceID == nil {
		return "", nil
	}
	opp := *after.OpponentSeasonAllianceID
	var existing sql.NullInt64
	hasSnap := tx.QueryRow(`SELECT opponent_season_alliance_id FROM season_war_snapshots WHERE war_id = ?`, after.ID).Scan(&existing) == nil
	sameOpp := hasSnap && existing.Valid && int(existing.Int64) == opp
	trigger := ""
	var asOf *time.Time
	switch {
	case after.Status == "declared" || after.Status == "confirmed":
		statusChanged := before == nil || before.Status != after.Status
		if statusChanged || !sameOpp {
			trigger = after.Status
		}
	case after.Status == "completed" && !sameOpp:
		trigger, asOf = "completed", warBattleInstant(after)
	}
	if trigger == "" {
		return "", nil
	}
	snap, err := buildWarSnapshot(tx, after.SeasonID, opp, asOf)
	if err != nil {
		return "", err
	}
	snap.Trigger, snap.TakenAt = trigger, landUTC(time.Now())
	if err := writeWarSnapshotTx(tx, after, snap, userID); err != nil {
		return "", err
	}
	return "snapshot (" + trigger + "): " + snapshotDetail(snap), nil
}

// POST /api/season-hub/war/wars/{id}/snapshot — take the war's snapshot again, now: allowed
// until the war is completed. A completed war with no snapshot for its opponent gets one as of its
// battle; one that has it is frozen.
func handleSeasonWarSnapshot(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	war := warFromPath(w, r)
	if war == nil {
		return
	}
	if ok, msg := seasonAcceptsEdits(war.SeasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	if war.OpponentSeasonAllianceID == nil {
		http.Error(w, "Pick the war's opponent first", http.StatusBadRequest)
		return
	}
	if war.Status == "cancelled" {
		http.Error(w, "A cancelled war has no matchup to keep", http.StatusConflict)
		return
	}
	trigger, asOf := "retake", (*time.Time)(nil)
	if war.Status == "completed" {
		var existing sql.NullInt64
		if db.QueryRow(`SELECT opponent_season_alliance_id FROM season_war_snapshots WHERE war_id = ?`, war.ID).Scan(&existing) == nil &&
			existing.Valid && int(existing.Int64) == *war.OpponentSeasonAllianceID {
			http.Error(w, "This war is completed: its snapshot is frozen", http.StatusConflict)
			return
		}
		trigger, asOf = "completed", warBattleInstant(war)
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleSeasonWarSnapshot: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	snap, err := buildWarSnapshot(tx, war.SeasonID, *war.OpponentSeasonAllianceID, asOf)
	if err == nil {
		snap.Trigger, snap.TakenAt = trigger, landUTC(time.Now())
		err = writeWarSnapshotTx(tx, war, snap, user.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		slog.Error("handleSeasonWarSnapshot: write", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "updated", "season_war", warLabel(war), false, "snapshot: "+snapshotDetail(snap))
	writeJSON(w, map[string]any{"snapshot": snap})
}

// snapshotDetail is the activity log's one line for a snapshot.
func snapshotDetail(s warSnapshot) string {
	d := s.Matchup.Rating
	if s.Matchup.Score != nil {
		d += fmt.Sprintf(" %+.2f%%", *s.Matchup.Score)
	}
	return d + ", figures as of " + landST(parseLandStored(s.AsOf)) + " ST"
}

// joinDetails joins the non-empty activity details with "; ".
func joinDetails(parts ...string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += "; "
		}
		out += p
	}
	return out
}
