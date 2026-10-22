// handlers_season_trade_posts.go — the War tab's Trade Post board (private-docs 280).
//
// A post is a definition (copied with the season) plus the officers' plan for it; a planned
// take / contest / defend with a time keeps a Trade Post Action calendar row (kind trade_post),
// and clearing the plan or choosing hold / ignore cancels it. Ownership is observed and appended
// — the latest observation is the owner — from the form, a CSV file or a phone. An owner the
// season does not list is kept as a label; its faction is the observation's own, else the
// season row's.

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

var validPostActions = map[string]bool{"take": true, "contest": true, "defend": true, "hold": true, "ignore": true}
var schedulingPostActions = map[string]bool{"take": true, "contest": true, "defend": true}

// tradePost is a post as the board shows it.
type tradePost struct {
	ID                       int        `json:"id"`
	PostKey                  string     `json:"post_key"`
	Name                     string     `json:"name"`
	StrategicPriority        *string    `json:"strategic_priority"`
	PlannedAction            *string    `json:"planned_action"`
	PlannedAt                string     `json:"planned_at"` // ST
	AssignedSeasonAllianceID *int       `json:"assigned_season_alliance_id"`
	RelatedWarID             *int       `json:"related_war_id"`
	StrategyCommsID          *int       `json:"strategy_comms_id"`
	MapFileID                *int       `json:"map_file_id"`
	Notes                    string     `json:"notes"`
	ScheduleEventID          *int       `json:"schedule_event_id"`
	Position                 int        `json:"position"`
	Owner                    *postOwner `json:"owner"`
}

// postOwner is a post's latest observation.
type postOwner struct {
	ObservationID    int    `json:"observation_id"`
	SeasonAllianceID *int   `json:"season_alliance_id"`
	Label            string `json:"label"`
	FactionID        *int   `json:"faction_id"`
	ObservedAt       string `json:"observed_at"` // ST
	Source           string `json:"source"`
}

func loadTradePosts(q rowQueryer, seasonID int, alliances []SeasonAlliance) ([]tradePost, error) {
	rows, err := q.Query(`SELECT id, post_key, name, strategic_priority, planned_action, COALESCE(planned_at, ''),
		assigned_season_alliance_id, related_war_id, strategy_comms_id, map_file_id, notes, schedule_event_id, position
		FROM season_trade_posts WHERE season_id = ? ORDER BY position, id`, seasonID)
	if err != nil {
		return nil, err
	}
	out := []tradePost{}
	for rows.Next() {
		var p tradePost
		var at string
		if err := rows.Scan(&p.ID, &p.PostKey, &p.Name, &p.StrategicPriority, &p.PlannedAction, &at, &p.AssignedSeasonAllianceID,
			&p.RelatedWarID, &p.StrategyCommsID, &p.MapFileID, &p.Notes, &p.ScheduleEventID, &p.Position); err != nil {
			rows.Close()
			return nil, err
		}
		p.PlannedAt = landST(parseLandStored(at))
		out = append(out, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	orows, err := q.Query(`SELECT o.id, o.post_id, o.owner_season_alliance_id, o.owner_label, o.owner_faction_id, o.observed_at, o.source
		FROM season_trade_post_observations o JOIN season_trade_posts p ON p.id = o.post_id
		WHERE p.season_id = ? ORDER BY o.observed_at, o.id`, seasonID)
	if err != nil {
		return nil, err
	}
	defer orows.Close()
	byID := map[int]*tradePost{}
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	sa := seasonAllianceMap(alliances)
	for orows.Next() {
		var o postOwner
		var postID int
		var at string
		if err := orows.Scan(&o.ObservationID, &postID, &o.SeasonAllianceID, &o.Label, &o.FactionID, &at, &o.Source); err != nil {
			return nil, err
		}
		o.ObservedAt = landST(parseLandStored(at))
		if o.SeasonAllianceID != nil {
			if a, ok := sa[*o.SeasonAllianceID]; ok {
				o.Label = "[" + a.Tag + "] " + a.Name
				if o.FactionID == nil {
					o.FactionID = a.FactionID
				}
			}
		}
		if p := byID[postID]; p != nil {
			o := o
			p.Owner = &o // oldest first: the latest wins
		}
	}
	return out, orows.Err()
}

// postWrite is the create/update body; absent fields keep their value on update.
type postWrite struct {
	SeasonID                 int         `json:"season_id"`
	PostKey                  *string     `json:"post_key"`
	Name                     *string     `json:"name"`
	StrategicPriority        *string     `json:"strategic_priority"`
	PlannedAction            *string     `json:"planned_action"`
	PlannedAt                *string     `json:"planned_at"`
	AssignedSeasonAllianceID optionalInt `json:"assigned_season_alliance_id"`
	RelatedWarID             optionalInt `json:"related_war_id"`
	StrategyCommsID          optionalInt `json:"strategy_comms_id"`
	MapFileID                optionalInt `json:"map_file_id"`
	Notes                    *string     `json:"notes"`
}

func syncTradePostCalendarTx(tx *sql.Tx, s *Season, p *tradePost, actorID int) (syncResult, error) {
	status := "cancelled"
	var date, tm string
	if p.PlannedAction != nil && schedulingPostActions[*p.PlannedAction] && p.PlannedAt != "" {
		t, err := parseLandST(p.PlannedAt)
		if err == nil {
			status = "confirmed"
			date, tm = stDateTime(&t)
		}
	}
	typeID, err := sourcedTypeID(tx, "Trade Post Action")
	if err != nil && err != sql.ErrNoRows {
		return syncResult{}, err
	}
	notes := p.Name
	if p.PlannedAction != nil {
		notes = strings.Title(*p.PlannedAction) + " " + p.Name
	}
	res, err := syncSourcedScheduleEvent(tx, sourcedEvent{Kind: sourceTradePost, ID: int64(p.ID), SeasonID: s.ID, Date: date, Time: tm,
		TypeID: typeID, Notes: notes, Status: status}, actorID)
	if err != nil {
		return res, err
	}
	if res.EventID != 0 && (p.ScheduleEventID == nil || int64(*p.ScheduleEventID) != res.EventID) {
		_, err = tx.Exec(`UPDATE season_trade_posts SET schedule_event_id = ? WHERE id = ?`, res.EventID, p.ID)
	}
	return res, err
}

// savePost creates (existing nil) or updates a post and keeps its calendar row in step.
func savePost(w http.ResponseWriter, r *http.Request, existing *tradePost, b postWrite) {
	user := getAuthUser(r)
	s := landSeason(w, b.SeasonID)
	if s == nil {
		return
	}
	if ok, msg := seasonAcceptsEdits(s.ID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	p := tradePost{}
	if existing != nil {
		p = *existing
	}
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	setOpt := func(dst **string, v *string) {
		if v != nil {
			if t := strings.TrimSpace(*v); t == "" {
				*dst = nil
			} else {
				*dst = &t
			}
		}
	}
	set(&p.PostKey, b.PostKey)
	set(&p.Name, b.Name)
	setOpt(&p.StrategicPriority, b.StrategicPriority)
	setOpt(&p.PlannedAction, b.PlannedAction)
	set(&p.PlannedAt, b.PlannedAt)
	for _, f := range []struct {
		dst **int
		v   optionalInt
	}{{&p.AssignedSeasonAllianceID, b.AssignedSeasonAllianceID}, {&p.RelatedWarID, b.RelatedWarID},
		{&p.StrategyCommsID, b.StrategyCommsID}, {&p.MapFileID, b.MapFileID}} {
		if f.v.Set {
			*f.dst = f.v.Value
		}
	}
	set(&p.Notes, b.Notes)
	// A map ID names a post from the season's lookup.
	if p.Name == "" && p.PostKey != "" {
		if a, ok := landAreaByID(s.SeasonNumber, p.PostKey); ok && a.Kind == "trade_post" {
			p.Name = fmt.Sprintf("%s %s", a.Name, a.ID)
		}
	}
	switch {
	case p.PostKey == "" && p.Name == "":
		return400(w, "a post needs a key (its map ID) or a name")
		return
	case p.StrategicPriority != nil && *p.StrategicPriority != "high" && *p.StrategicPriority != "medium" && *p.StrategicPriority != "low":
		return400(w, "priority must be high, medium or low")
		return
	case p.PlannedAction != nil && !validPostActions[*p.PlannedAction]:
		return400(w, "action must be take, contest, defend, hold or ignore")
		return
	}
	if p.PostKey == "" {
		p.PostKey = strings.ToLower(strings.ReplaceAll(p.Name, " ", "-"))
	}
	if p.Name == "" {
		p.Name = "Trade Post " + p.PostKey
	}
	var plannedAt any
	if p.PlannedAt != "" {
		t, err := parseLandST(p.PlannedAt)
		if err != nil {
			return400(w, "the planned time must be YYYY-MM-DD HH:MM (server time)")
			return
		}
		plannedAt = landUTC(t)
	}
	if p.AssignedSeasonAllianceID != nil {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM season_alliances WHERE id = ? AND season_id = ?`, *p.AssignedSeasonAllianceID, s.ID).Scan(&n)
		if n == 0 {
			return400(w, "the assigned alliance is not one of this season's")
			return
		}
	}
	if p.RelatedWarID != nil {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM season_wars WHERE id = ? AND season_id = ?`, *p.RelatedWarID, s.ID).Scan(&n)
		if n == 0 {
			return400(w, "the related war is not one of this season's")
			return
		}
	}
	if msg := checkWarReferences(s.ID, &seasonWar{StrategyCommsID: p.StrategyCommsID, MapFileID: p.MapFileID}); msg != "" {
		return400(w, msg)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("savePost: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	if p.ID == 0 {
		var pos int
		tx.QueryRow(`SELECT COALESCE(MAX(position) + 1, 0) FROM season_trade_posts WHERE season_id = ?`, s.ID).Scan(&pos)
		res, err := tx.Exec(`INSERT INTO season_trade_posts (season_id, post_key, name, strategic_priority, planned_action, planned_at,
			assigned_season_alliance_id, related_war_id, strategy_comms_id, map_file_id, notes, position)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.ID, p.PostKey, p.Name, p.StrategicPriority, p.PlannedAction, plannedAt,
			p.AssignedSeasonAllianceID, p.RelatedWarID, p.StrategyCommsID, p.MapFileID, p.Notes, pos)
		if isUniqueViolation(err) {
			http.Error(w, "this season already has a post "+p.PostKey, http.StatusConflict)
			return
		}
		if err != nil {
			slog.Error("savePost: insert", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		id, _ := res.LastInsertId()
		p.ID = int(id)
	} else if _, err := tx.Exec(`UPDATE season_trade_posts SET post_key = ?, name = ?, strategic_priority = ?, planned_action = ?,
		planned_at = ?, assigned_season_alliance_id = ?, related_war_id = ?, strategy_comms_id = ?, map_file_id = ?, notes = ?
		WHERE id = ?`, p.PostKey, p.Name, p.StrategicPriority, p.PlannedAction, plannedAt, p.AssignedSeasonAllianceID,
		p.RelatedWarID, p.StrategyCommsID, p.MapFileID, p.Notes, p.ID); err != nil {
		if isUniqueViolation(err) {
			http.Error(w, "this season already has a post "+p.PostKey, http.StatusConflict)
			return
		}
		slog.Error("savePost: update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	cal, err := syncTradePostCalendarTx(tx, s, &p, user.ID)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		slog.Error("savePost: calendar", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	action := "updated"
	if existing == nil {
		action = "created"
	}
	logActivity(user.ID, user.Username, action, "season_trade_post", p.Name+", "+seasonTagName(&s.ID), false)
	writeJSON(w, map[string]any{"id": p.ID, "calendar": cal})
}

func postFromPath(w http.ResponseWriter, r *http.Request) (*tradePost, int) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid post ID", http.StatusBadRequest)
		return nil, 0
	}
	var seasonID int
	if err := db.QueryRow(`SELECT season_id FROM season_trade_posts WHERE id = ?`, id).Scan(&seasonID); err == sql.ErrNoRows {
		http.Error(w, "Post not found", http.StatusNotFound)
		return nil, 0
	} else if err != nil {
		slog.Error("postFromPath", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return nil, 0
	}
	posts, err := loadTradePosts(db, seasonID, nil)
	if err != nil {
		slog.Error("postFromPath: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return nil, 0
	}
	for i := range posts {
		if posts[i].ID == id {
			return &posts[i], seasonID
		}
	}
	http.Error(w, "Post not found", http.StatusNotFound)
	return nil, 0
}

// POST /api/season-hub/war/trade-posts
func handleTradePostCreate(w http.ResponseWriter, r *http.Request) {
	var b postWrite
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	savePost(w, r, nil, b)
}

// PUT /api/season-hub/war/trade-posts/{id}
func handleTradePostUpdate(w http.ResponseWriter, r *http.Request) {
	p, seasonID := postFromPath(w, r)
	if p == nil {
		return
	}
	var b postWrite
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	b.SeasonID = seasonID
	savePost(w, r, p, b)
}

// DELETE /api/season-hub/war/trade-posts/{id} — a post with recorded owners is kept (409);
// its calendar row is cancelled, never deleted.
func handleTradePostDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	p, seasonID := postFromPath(w, r)
	if p == nil {
		return
	}
	if ok, msg := seasonAcceptsEdits(seasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	if p.Owner != nil {
		http.Error(w, "this post has recorded owners — delete them first", http.StatusConflict)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleTradePostDelete: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	if _, err = syncSourcedScheduleEvent(tx, sourcedEvent{Kind: sourceTradePost, ID: int64(p.ID), Status: "cancelled"}, user.ID); err == nil {
		if _, err = tx.Exec(`DELETE FROM season_trade_posts WHERE id = ?`, p.ID); err == nil {
			err = tx.Commit()
		}
	}
	if err != nil {
		slog.Error("handleTradePostDelete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "deleted", "season_trade_post", p.Name+", "+seasonTagName(&seasonID), false)
	writeJSON(w, map[string]string{"message": "Deleted"})
}

// postObservationIn is an owner observation: a season alliance, or a label (with a faction).
type postObservationIn struct {
	OwnerSeasonAllianceID *int   `json:"owner_season_alliance_id"`
	OwnerLabel            string `json:"owner_label"`
	OwnerFactionID        *int   `json:"owner_faction_id"`
	ObservedAt            string `json:"observed_at"`
}

func writePostObservationTx(tx *sql.Tx, postID int, in postObservationIn, at time.Time, source string, userID int) (bool, error) {
	stored := landUTC(at)
	label := strings.TrimSpace(in.OwnerLabel)
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM season_trade_post_observations WHERE post_id = ? AND observed_at = ?
		AND owner_season_alliance_id IS ? AND owner_label = ?`, postID, stored, in.OwnerSeasonAllianceID, label).Scan(&n); err != nil || n > 0 {
		return false, err
	}
	var by any
	if userID > 0 {
		by = userID
	}
	_, err := tx.Exec(`INSERT INTO season_trade_post_observations (post_id, owner_season_alliance_id, owner_label, owner_faction_id,
		observed_at, source, recorded_by) VALUES (?, ?, ?, ?, ?, ?, ?)`, postID, in.OwnerSeasonAllianceID, label, in.OwnerFactionID,
		stored, source, by)
	return err == nil, err
}

// validatePostObservation reads the instant ("" = now) and checks the owner names something.
func validatePostObservation(seasonID int, in postObservationIn, now time.Time) (time.Time, string) {
	at := now
	if strings.TrimSpace(in.ObservedAt) != "" {
		t, err := parseLandInstant(in.ObservedAt)
		if err != nil {
			return at, "the time must be YYYY-MM-DD HH:MM (server time)"
		}
		at = t
	}
	if at.After(now.Add(10 * time.Minute)) {
		return at, "the time is in the future — an observation records what was seen"
	}
	if in.OwnerSeasonAllianceID == nil && strings.TrimSpace(in.OwnerLabel) == "" {
		return at, "name the owner: one of the season's alliances or a label (\"none\" for an empty post)"
	}
	if in.OwnerSeasonAllianceID != nil {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM season_alliances WHERE id = ? AND season_id = ?`, *in.OwnerSeasonAllianceID, seasonID).Scan(&n)
		if n == 0 {
			return at, "the owner is not one of this season's alliances"
		}
	}
	if in.OwnerFactionID != nil {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM season_factions WHERE id = ? AND season_id = ?`, *in.OwnerFactionID, seasonID).Scan(&n)
		if n == 0 {
			return at, "the faction is not one of this season's"
		}
	}
	return at, ""
}

// POST /api/season-hub/war/trade-posts/{id}/observations
func handleTradePostObservationCreate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	p, seasonID := postFromPath(w, r)
	if p == nil {
		return
	}
	if ok, msg := seasonAcceptsEdits(seasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	var in postObservationIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	at, msg := validatePostObservation(seasonID, in, time.Now())
	if msg != "" {
		return400(w, msg)
		return
	}
	written, ok := writeOneTx(w, "handleTradePostObservationCreate", func(tx *sql.Tx) (bool, error) {
		return writePostObservationTx(tx, p.ID, in, at, "manual", user.ID)
	})
	if !ok {
		return
	}
	if written {
		logActivity(user.ID, user.Username, "created", "season_trade_post", p.Name+", "+seasonTagName(&seasonID), false, "owner observed")
	}
	writeJSON(w, map[string]any{"written": written})
}

// DELETE /api/season-hub/war/trade-posts/observations/{id}
func handleTradePostObservationDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}
	var seasonID int
	if err := db.QueryRow(`SELECT p.season_id FROM season_trade_post_observations o JOIN season_trade_posts p ON p.id = o.post_id
		WHERE o.id = ?`, id).Scan(&seasonID); err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	} else if err != nil {
		slog.Error("handleTradePostObservationDelete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if ok, msg := seasonAcceptsEdits(seasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	if _, err := db.Exec(`DELETE FROM season_trade_post_observations WHERE id = ?`, id); err != nil {
		slog.Error("handleTradePostObservationDelete: delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "deleted", "season_trade_post", seasonTagName(&seasonID), false, "owner observation")
	writeJSON(w, map[string]string{"message": "Deleted"})
}

// tradePostAllianceInUse names a season alliance's posts.
func tradePostAllianceInUse(id int) string {
	var n int
	db.QueryRow(`SELECT (SELECT COUNT(*) FROM season_trade_posts WHERE assigned_season_alliance_id = ?) +
		(SELECT COUNT(*) FROM season_trade_post_observations WHERE owner_season_alliance_id = ?)`, id, id).Scan(&n)
	if n > 0 {
		return fmt.Sprintf("this alliance is on the Trade Post board %d times this season", n)
	}
	return ""
}

// postByKeyOrName finds a post of the season by its key or name (case-insensitive).
func postByKeyOrName(posts []tradePost, ref string) *tradePost {
	ref = strings.TrimSpace(ref)
	for i := range posts {
		if strings.EqualFold(posts[i].PostKey, ref) || strings.EqualFold(posts[i].Name, ref) {
			return &posts[i]
		}
	}
	return nil
}

type postObservationImport struct {
	postID int
	in     postObservationIn
	at     time.Time
}

func init() {
	registerSeasonImportKind("trade_post_observations", importKind{
		Title: "Trade Post owners",
		Perms: []string{"manage_season_war"},
		Columns: []importColumn{
			{Key: "post", Label: "Post", Aliases: []string{"trade_post", "post_key"}, Required: true},
			{Key: "owner_tag", Label: "Owner tag", Aliases: []string{"owner", "tag"}, Required: true},
			{Key: "owner_server", Label: "Owner server", Aliases: []string{"server"}},
			{Key: "observed_at", Label: "Observed at", Aliases: []string{"at", "time", "when", "observed"}},
		},
		Prepare: func(ctx *importCtx) error {
			a, err := loadSeasonAlliances(db, ctx.Season.ID)
			if err != nil {
				return err
			}
			ctx.Data["alliances"] = a
			posts, err := loadTradePosts(db, ctx.Season.ID, nil)
			ctx.Data["posts"] = posts
			return err
		},
		Parse: func(ctx *importCtx, rec map[string]string) (any, map[string]any, []string) {
			summary := map[string]any{"post": rec["post"], "owner": rec["owner_tag"], "observed_at": rec["observed_at"]}
			p := postByKeyOrName(ctx.Data["posts"].([]tradePost), rec["post"])
			if p == nil {
				return nil, summary, []string{"no post of this season is called " + rec["post"]}
			}
			in := postObservationIn{ObservedAt: rec["observed_at"]}
			if id, msg := importAllianceRef(ctx, rec["owner_tag"], rec["owner_server"]); msg == "" && id != 0 {
				in.OwnerSeasonAllianceID = &id
			} else {
				// An owner the season does not list is kept as its label.
				in.OwnerLabel = strings.TrimSpace(rec["owner_tag"])
				if rec["owner_server"] != "" {
					in.OwnerLabel += " (S" + strings.TrimSpace(rec["owner_server"]) + ")"
				}
			}
			at, msg := validatePostObservation(ctx.Season.ID, in, time.Now())
			if msg != "" {
				return nil, summary, []string{msg}
			}
			summary["observed_at"] = landST(&at)
			return postObservationImport{postID: p.ID, in: in, at: at}, summary, nil
		},
		Write: func(tx *sql.Tx, ctx *importCtx, items []any) (int, int, error) {
			written, unchanged := 0, 0
			for _, it := range items {
				o := it.(postObservationImport)
				ok, err := writePostObservationTx(tx, o.postID, o.in, o.at, "csv", ctx.User.ID)
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
}
