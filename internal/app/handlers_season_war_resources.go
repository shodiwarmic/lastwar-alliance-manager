// handlers_season_war_resources.go — resource totals, rank groups, cutoffs and loot scenarios
// on the War tab (private-docs 279). Totals and cutoffs are observations, appended; the
// projection, gaps, scenarios and Reward Tier Status are derived on read.

package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// resourceObservation is one appended total.
type resourceObservation struct {
	ID               int    `json:"id"`
	SeasonAllianceID *int   `json:"season_alliance_id"`
	IsOwn            bool   `json:"is_own"`
	ResourceTotal    int64  `json:"resource_total"`
	WarRank          *int   `json:"war_rank"`
	ObservedAt       string `json:"observed_at"` // ST
	Source           string `json:"source"`
	Note             string `json:"note"`
	at               time.Time
}

func loadResourceObservations(q rowQueryer, seasonID int) ([]resourceObservation, error) {
	rows, err := q.Query(`SELECT id, season_alliance_id, is_own, resource_total, war_rank, observed_at, source, note
		FROM season_resource_observations WHERE season_id = ? ORDER BY observed_at, id`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []resourceObservation{}
	for rows.Next() {
		var o resourceObservation
		var own int
		var at string
		if err := rows.Scan(&o.ID, &o.SeasonAllianceID, &own, &o.ResourceTotal, &o.WarRank, &at, &o.Source, &o.Note); err != nil {
			return nil, err
		}
		o.IsOwn = own == 1
		if t := parseLandStored(at); t != nil {
			o.at, o.ObservedAt = *t, landST(t)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// rankCutoff is one observed cutoff: the total currently at a rank.
type rankCutoff struct {
	ID            int    `json:"id"`
	Rank          int    `json:"rank"`
	ResourceTotal int64  `json:"resource_total"`
	ObservedAt    string `json:"observed_at"` // ST
	Source        string `json:"source"`
	Note          string `json:"note"`
}

func loadRankCutoffs(q rowQueryer, seasonID int) ([]rankCutoff, error) {
	rows, err := q.Query(`SELECT id, rank, resource_total, observed_at, source, note FROM season_rank_cutoffs
		WHERE season_id = ? ORDER BY observed_at, id`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []rankCutoff{}
	for rows.Next() {
		var c rankCutoff
		var at string
		if err := rows.Scan(&c.ID, &c.Rank, &c.ResourceTotal, &at, &c.Source, &c.Note); err != nil {
			return nil, err
		}
		c.ObservedAt = landST(parseLandStored(at))
		out = append(out, c)
	}
	return out, rows.Err()
}

func loadRankGroups(q rowQueryer, seasonID int) ([]rankGroup, error) {
	rows, err := q.Query(`SELECT id, label, rank_from, rank_to, position FROM season_rank_groups WHERE season_id = ?
		ORDER BY position, rank_from, id`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []rankGroup{}
	for rows.Next() {
		var g rankGroup
		if err := rows.Scan(&g.ID, &g.Label, &g.RankFrom, &g.RankTo, &g.Position); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// latestCutoffs is the latest cutoff per rank, by rank.
func latestCutoffs(all []rankCutoff) []rankCutoff {
	by := map[int]rankCutoff{}
	for _, c := range all { // oldest first: later rows win
		by[c.Rank] = c
	}
	out := make([]rankCutoff, 0, len(by))
	for _, c := range by {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rank < out[j].Rank })
	return out
}

// warResources is the War tab's resource panel.
type warResources struct {
	Own              *resourceObservation  `json:"own"`
	PerHour          int64                 `json:"per_hour"`
	Sources          []productionSource    `json:"sources"`
	UnknownOutput    []string              `json:"unknown_output"`
	ProjectedNow     *int64                `json:"projected_now"`
	ProjectedAtReset *int64                `json:"projected_at_reset"`
	NextReset        string                `json:"next_reset"` // ST
	ProductionStop   string                `json:"production_stop"`
	Stopped          bool                  `json:"stopped"`
	Rank             *int                  `json:"rank"`
	Group            *rankGroup            `json:"group"`
	TargetRank       *int                  `json:"target_rank"`
	TargetGroup      *rankGroup            `json:"target_group"`
	TargetCutoff     *rankCutoff           `json:"target_cutoff"`
	Gap              *int64                `json:"gap"`        // target cutoff − our total; ≤ 0 is past it
	NextAbove        *rankCutoff           `json:"next_above"` // the cutoff one rank above ours
	GapNextAbove     *int64                `json:"gap_next_above"`
	Status           string                `json:"status"` // on_track | at_risk | behind | unknown
	Groups           []rankGroup           `json:"groups"`
	Cutoffs          []rankCutoff          `json:"cutoffs"` // latest per rank
	Loot             []opponentLoot        `json:"loot"`
	Recent           []resourceObservation `json:"recent"`
	RecentCutoffs    []rankCutoff          `json:"recent_cutoffs"`
}

// opponentLoot is one opponent's latest total and its scenarios.
type opponentLoot struct {
	SeasonAllianceID int            `json:"season_alliance_id"`
	Tag              string         `json:"tag"`
	Server           int            `json:"server"`
	Rating           string         `json:"rating"`
	Override         *string        `json:"override"`
	Total            int64          `json:"total"`
	WarRank          *int           `json:"war_rank"`
	ObservedAt       string         `json:"observed_at"`
	Scenarios        []lootScenario `json:"scenarios"`
}

// buildWarResources derives the panel from what the War GET has read.
func buildWarResources(s *Season, cfg warConfig, obs []resourceObservation, allCutoffs []rankCutoff, groups []rankGroup,
	opponents []warOpponent, held []landHolding, levels []landLevel, now time.Time) warResources {
	out := warResources{Groups: groups, Status: "unknown", TargetRank: s.TargetWarRank}
	out.PerHour, out.Sources, out.UnknownOutput = productionFromHoldings(held, levels)
	cut := latestCutoffs(allCutoffs)
	out.Cutoffs = cut
	cutAt := func(rank int) *rankCutoff {
		for i := range cut {
			if cut[i].Rank == rank {
				return &cut[i]
			}
		}
		return nil
	}
	latestBy := map[int]resourceObservation{}
	for _, o := range obs {
		if o.IsOwn {
			o := o
			out.Own = &o
		} else if o.SeasonAllianceID != nil {
			latestBy[*o.SeasonAllianceID] = o
		}
	}
	stop := productionStopAt(s.StartDate, cfg.ProductionStopWeek, cfg.ProductionStopDay)
	if stop != nil {
		out.ProductionStop = landST(stop)
		out.Stopped = !now.Before(*stop)
	}
	reset := nextGameReset(now)
	out.NextReset = landST(&reset)
	if out.Own != nil {
		pn := projectTotal(out.Own.ResourceTotal, out.PerHour, out.Own.at, now, stop)
		pr := projectTotal(out.Own.ResourceTotal, out.PerHour, out.Own.at, reset, stop)
		out.ProjectedNow, out.ProjectedAtReset = &pn, &pr
		out.Rank = out.Own.WarRank
	}
	out.Group = groupForRank(groups, out.Rank)
	out.TargetGroup = groupForRank(groups, s.TargetWarRank)
	if s.TargetWarRank != nil {
		out.TargetCutoff = cutAt(*s.TargetWarRank)
	}
	if out.Own != nil && out.TargetCutoff != nil {
		g := out.TargetCutoff.ResourceTotal - out.Own.ResourceTotal
		out.Gap = &g
	}
	if out.Own != nil && out.Rank != nil && *out.Rank > 1 {
		if c := cutAt(*out.Rank - 1); c != nil {
			out.NextAbove = c
			g := c.ResourceTotal - out.Own.ResourceTotal
			out.GapNextAbove = &g
		}
	}
	var largest int64
	out.Loot = []opponentLoot{}
	for _, o := range opponents {
		r, ok := latestBy[o.ID]
		if !ok {
			continue
		}
		sc := lootScenarios(r.ResourceTotal, cfg.LootStepPct, cfg.LootMaxPct, out.Gap)
		if o.Relationship != "allied" && len(sc) > 0 && sc[len(sc)-1].Amount > largest {
			largest = sc[len(sc)-1].Amount
		}
		out.Loot = append(out.Loot, opponentLoot{SeasonAllianceID: o.ID, Tag: o.Tag, Server: o.Server, Rating: o.Matchup.Rating,
			Override: o.AssessmentOverride, Total: r.ResourceTotal, WarRank: r.WarRank, ObservedAt: r.ObservedAt, Scenarios: sc})
	}
	var latest *int64
	if out.Own != nil {
		v := out.Own.ResourceTotal
		latest = &v
	}
	var cutoffTotal *int64
	if out.TargetCutoff != nil {
		v := out.TargetCutoff.ResourceTotal
		cutoffTotal = &v
	}
	out.Status = rewardTierStatus(latest, out.ProjectedAtReset, cutoffTotal, largest)
	out.Recent = []resourceObservation{}
	for i := len(obs) - 1; i >= 0 && len(out.Recent) < 100; i-- {
		out.Recent = append(out.Recent, obs[i])
	}
	out.RecentCutoffs = []rankCutoff{}
	for i := len(allCutoffs) - 1; i >= 0 && len(out.RecentCutoffs) < 100; i-- {
		out.RecentCutoffs = append(out.RecentCutoffs, allCutoffs[i])
	}
	return out
}

// loadWarResources reads what the panel needs and builds it.
func loadWarResources(q rowQueryer, s *Season, cfg warConfig, opponents []warOpponent, now time.Time) (warResources, error) {
	obs, err := loadResourceObservations(q, s.ID)
	if err != nil {
		return warResources{}, err
	}
	cutoffs, err := loadRankCutoffs(q, s.ID)
	if err != nil {
		return warResources{}, err
	}
	groups, err := loadRankGroups(q, s.ID)
	if err != nil {
		return warResources{}, err
	}
	events, err := loadLandEvents(q, s.ID)
	if err != nil {
		return warResources{}, err
	}
	levels, err := loadLandLevels(q, s.ID)
	if err != nil {
		return warResources{}, err
	}
	return buildWarResources(s, cfg, obs, cutoffs, groups, opponents, landHoldingsAt(events, now), levels, now), nil
}

// ── Writes ─────────────────────────────────────────────────────────────────

// resourceObservationIn is a total as the form, a CSV row or a phone sends it.
type resourceObservationIn struct {
	SeasonAllianceID *int   `json:"season_alliance_id"`
	Own              bool   `json:"own"`
	ResourceTotal    *int64 `json:"resource_total"`
	WarRank          *int   `json:"war_rank"`
	ObservedAt       string `json:"observed_at"`
	Note             string `json:"note"`
}

func validateResourceObservation(in resourceObservationIn, now time.Time) (time.Time, string) {
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
	if in.ResourceTotal == nil || *in.ResourceTotal < 0 {
		return at, "a total (0 or more) is required"
	}
	if in.WarRank != nil && *in.WarRank < 1 {
		return at, "rank is from 1"
	}
	return at, ""
}

func writeResourceObservationTx(tx *sql.Tx, seasonID int, in resourceObservationIn, at time.Time, source string, userID int) (bool, error) {
	stored := landUTC(at)
	var sa any
	own := 1
	if !in.Own && in.SeasonAllianceID != nil {
		sa, own = *in.SeasonAllianceID, 0
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM season_resource_observations WHERE season_id = ? AND is_own = ?
		AND season_alliance_id IS ? AND observed_at = ? AND resource_total = ? AND war_rank IS ?`,
		seasonID, own, sa, stored, *in.ResourceTotal, in.WarRank).Scan(&n); err != nil || n > 0 {
		return false, err
	}
	var by any
	if userID > 0 {
		by = userID
	}
	_, err := tx.Exec(`INSERT INTO season_resource_observations (season_id, season_alliance_id, is_own, resource_total, war_rank,
		observed_at, source, recorded_by, note) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, seasonID, sa, own, *in.ResourceTotal,
		in.WarRank, stored, source, by, strings.TrimSpace(in.Note))
	return err == nil, err
}

func writeRankCutoffTx(tx *sql.Tx, seasonID, rank int, total int64, at time.Time, source, note string, userID int) (bool, error) {
	stored := landUTC(at)
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM season_rank_cutoffs WHERE season_id = ? AND rank = ? AND observed_at = ?
		AND resource_total = ?`, seasonID, rank, stored, total).Scan(&n); err != nil || n > 0 {
		return false, err
	}
	var by any
	if userID > 0 {
		by = userID
	}
	_, err := tx.Exec(`INSERT INTO season_rank_cutoffs (season_id, rank, resource_total, observed_at, source, recorded_by, note)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, seasonID, rank, total, stored, source, by, strings.TrimSpace(note))
	return err == nil, err
}

// writeOneTx runs a single write in a transaction and answers written.
func writeOneTx(w http.ResponseWriter, what string, write func(tx *sql.Tx) (bool, error)) (bool, bool) {
	tx, err := db.Begin()
	if err != nil {
		slog.Error(what+": begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return false, false
	}
	defer tx.Rollback()
	written, err := write(tx)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		slog.Error(what, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return false, false
	}
	return written, true
}

// POST /api/season-hub/war/resources {season_id, own | season_alliance_id, resource_total, war_rank, observed_at}
func handleSeasonWarResourceCreate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var body struct {
		SeasonID int `json:"season_id"`
		resourceObservationIn
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if ok, msg := seasonAcceptsEdits(body.SeasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	label := "our alliance"
	if !body.Own {
		if body.SeasonAllianceID == nil {
			http.Error(w, "name the alliance (season_alliance_id) or set own", http.StatusBadRequest)
			return
		}
		var ext int64
		if err := db.QueryRow(`SELECT external_alliance_id FROM season_alliances WHERE id = ? AND season_id = ?`,
			*body.SeasonAllianceID, body.SeasonID).Scan(&ext); err != nil {
			http.Error(w, "that alliance is not one of this season's", http.StatusBadRequest)
			return
		}
		label = seasonAllianceLabel(ext)
	}
	at, msg := validateResourceObservation(body.resourceObservationIn, time.Now())
	if msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	written, ok := writeOneTx(w, "handleSeasonWarResourceCreate", func(tx *sql.Tx) (bool, error) {
		return writeResourceObservationTx(tx, body.SeasonID, body.resourceObservationIn, at, "manual", user.ID)
	})
	if !ok {
		return
	}
	if written {
		logActivity(user.ID, user.Username, "created", "season_resource_observation", label+", "+seasonTagName(&body.SeasonID), false,
			fmt.Sprintf("total %d", *body.ResourceTotal))
	}
	writeJSON(w, map[string]any{"written": written})
}

// POST /api/season-hub/war/cutoffs {season_id, rank, resource_total, observed_at, note}
func handleSeasonWarCutoffCreate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var body struct {
		SeasonID      int    `json:"season_id"`
		Rank          int    `json:"rank"`
		ResourceTotal *int64 `json:"resource_total"`
		ObservedAt    string `json:"observed_at"`
		Note          string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if ok, msg := seasonAcceptsEdits(body.SeasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	if body.Rank < 1 {
		http.Error(w, "rank is from 1", http.StatusBadRequest)
		return
	}
	at, msg := validateResourceObservation(resourceObservationIn{ResourceTotal: body.ResourceTotal, ObservedAt: body.ObservedAt}, time.Now())
	if msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	written, ok := writeOneTx(w, "handleSeasonWarCutoffCreate", func(tx *sql.Tx) (bool, error) {
		return writeRankCutoffTx(tx, body.SeasonID, body.Rank, *body.ResourceTotal, at, "manual", body.Note, user.ID)
	})
	if !ok {
		return
	}
	if written {
		logActivity(user.ID, user.Username, "created", "season_rank_cutoff", fmt.Sprintf("rank %d, %s", body.Rank, seasonTagName(&body.SeasonID)), false,
			fmt.Sprintf("total %d", *body.ResourceTotal))
	}
	writeJSON(w, map[string]any{"written": written})
}

// deleteSeasonRow deletes one appended row by id from table, refusing an archived season.
func deleteSeasonRow(w http.ResponseWriter, r *http.Request, table, entity string) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}
	var seasonID int
	if err := db.QueryRow(`SELECT season_id FROM `+table+` WHERE id = ?`, id).Scan(&seasonID); err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	} else if err != nil {
		slog.Error("deleteSeasonRow: load", "table", table, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if ok, msg := seasonAcceptsEdits(seasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	if _, err := db.Exec(`DELETE FROM `+table+` WHERE id = ?`, id); err != nil {
		slog.Error("deleteSeasonRow", "table", table, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "deleted", entity, seasonTagName(&seasonID), false)
	writeJSON(w, map[string]string{"message": "Deleted"})
}

func handleSeasonWarResourceDelete(w http.ResponseWriter, r *http.Request) {
	deleteSeasonRow(w, r, "season_resource_observations", "season_resource_observation")
}

func handleSeasonWarCutoffDelete(w http.ResponseWriter, r *http.Request) {
	deleteSeasonRow(w, r, "season_rank_cutoffs", "season_rank_cutoff")
}

// PUT /api/season-hub/war/rank-groups {season_id, groups: [{label, rank_from, rank_to}]} — the
// whole list, in order; ranges may not overlap. A faction battle's group link is cleared when
// its group goes.
func handleSeasonWarRankGroupsPut(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var body struct {
		SeasonID int         `json:"season_id"`
		Groups   []rankGroup `json:"groups"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if ok, msg := seasonAcceptsEdits(body.SeasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	for i, g := range body.Groups {
		body.Groups[i].Label = strings.TrimSpace(g.Label)
		if body.Groups[i].Label == "" || g.RankFrom < 1 || g.RankTo < g.RankFrom {
			http.Error(w, fmt.Sprintf("group %d needs a name and ranks from 1, the first no higher than the last", i+1), http.StatusBadRequest)
			return
		}
		for j := 0; j < i; j++ {
			if g.RankFrom <= body.Groups[j].RankTo && body.Groups[j].RankFrom <= g.RankTo {
				return400(w, fmt.Sprintf("%s and %s share ranks", body.Groups[j].Label, body.Groups[i].Label))
				return
			}
		}
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleSeasonWarRankGroupsPut: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	keep := map[int]bool{}
	for pos, g := range body.Groups {
		if g.ID != 0 {
			res, err := tx.Exec(`UPDATE season_rank_groups SET label = ?, rank_from = ?, rank_to = ?, position = ? WHERE id = ? AND season_id = ?`,
				g.Label, g.RankFrom, g.RankTo, pos, g.ID, body.SeasonID)
			if err != nil {
				slog.Error("handleSeasonWarRankGroupsPut: update", "error", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			if n, _ := res.RowsAffected(); n == 1 {
				keep[g.ID] = true
				continue
			}
		}
		res, err := tx.Exec(`INSERT INTO season_rank_groups (season_id, label, rank_from, rank_to, position) VALUES (?, ?, ?, ?, ?)`,
			body.SeasonID, g.Label, g.RankFrom, g.RankTo, pos)
		if err != nil {
			slog.Error("handleSeasonWarRankGroupsPut: insert", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		id, _ := res.LastInsertId()
		keep[int(id)] = true
	}
	rows, err := tx.Query(`SELECT id FROM season_rank_groups WHERE season_id = ?`, body.SeasonID)
	if err != nil {
		slog.Error("handleSeasonWarRankGroupsPut: list", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	var gone []int
	for rows.Next() {
		var id int
		rows.Scan(&id)
		if !keep[id] {
			gone = append(gone, id)
		}
	}
	rows.Close()
	for _, id := range gone {
		if _, err := tx.Exec(`UPDATE season_faction_battles SET rank_group_id = NULL WHERE rank_group_id = ?`, id); err == nil {
			_, err = tx.Exec(`DELETE FROM season_rank_groups WHERE id = ?`, id)
		} else {
			slog.Error("handleSeasonWarRankGroupsPut: delete", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handleSeasonWarRankGroupsPut: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "updated", "season_war_config", seasonTagName(&body.SeasonID), true,
		fmt.Sprintf("rank groups: %d", len(body.Groups)))
	writeJSON(w, map[string]string{"message": "Saved"})
}

// warResourceAllianceInUse names a season alliance's resource observations.
func warResourceAllianceInUse(id int) string {
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM season_resource_observations WHERE season_alliance_id = ?`, id).Scan(&n)
	if n > 0 {
		return fmt.Sprintf("this alliance has %d resource observations this season — delete them first", n)
	}
	return ""
}

// ── CSV kinds ──────────────────────────────────────────────────────────────

type resourceImport struct {
	in resourceObservationIn
	at time.Time
}

type cutoffImport struct {
	rank  int
	total int64
	at    time.Time
	note  string
}

func init() {
	registerSeasonImportKind("resource_observations", importKind{
		Title: "Resource totals",
		Perms: []string{"manage_season_war"},
		Columns: []importColumn{
			{Key: "alliance", Label: "Alliance", Aliases: []string{"tag", "alliance_tag"}},
			{Key: "server", Label: "Server", Aliases: []string{"alliance_server"}},
			{Key: "total", Label: "Total", Aliases: []string{"resource_total", "resources", "copper"}, Required: true},
			{Key: "rank", Label: "Rank", Aliases: []string{"war_rank"}},
			{Key: "observed_at", Label: "Observed at", Aliases: []string{"at", "time", "when", "observed"}},
			{Key: "note", Label: "Note", Aliases: []string{"notes"}},
		},
		Prepare: func(ctx *importCtx) error {
			a, err := loadSeasonAlliances(db, ctx.Season.ID)
			ctx.Data["alliances"] = a
			return err
		},
		Parse: func(ctx *importCtx, rec map[string]string) (any, map[string]any, []string) {
			summary := map[string]any{"alliance": rec["alliance"], "total": rec["total"], "rank": rec["rank"], "observed_at": rec["observed_at"]}
			in := resourceObservationIn{ObservedAt: rec["observed_at"], Note: rec["note"]}
			if strings.TrimSpace(rec["alliance"]) == "" || isOwnAlliance("", rec["alliance"]) {
				in.Own = true
				summary["alliance"] = "ours"
			} else {
				id, msg := importAllianceRef(ctx, rec["alliance"], rec["server"])
				if msg != "" {
					return nil, summary, []string{msg}
				}
				in.SeasonAllianceID = &id
			}
			total, err := optIntCell(rec["total"])
			if err != nil || total == nil {
				return nil, summary, []string{"Total is not a whole number"}
			}
			t := int64(*total)
			in.ResourceTotal = &t
			rank, err := optIntCell(rec["rank"])
			if err != nil {
				return nil, summary, []string{"Rank is not a whole number"}
			}
			in.WarRank = rank
			at, msg := validateResourceObservation(in, time.Now())
			if msg != "" {
				return nil, summary, []string{msg}
			}
			summary["observed_at"] = landST(&at)
			return resourceImport{in: in, at: at}, summary, nil
		},
		Write: func(tx *sql.Tx, ctx *importCtx, items []any) (int, int, error) {
			written, unchanged := 0, 0
			for _, it := range items {
				o := it.(resourceImport)
				ok, err := writeResourceObservationTx(tx, ctx.Season.ID, o.in, o.at, "csv", ctx.User.ID)
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

	registerSeasonImportKind("rank_cutoffs", importKind{
		Title: "Rank cutoffs",
		Perms: []string{"manage_season_war"},
		Columns: []importColumn{
			{Key: "rank", Label: "Rank", Aliases: []string{"war_rank"}, Required: true},
			{Key: "total", Label: "Total", Aliases: []string{"resource_total", "resources", "copper", "cutoff"}, Required: true},
			{Key: "observed_at", Label: "Observed at", Aliases: []string{"at", "time", "when", "observed"}},
			{Key: "note", Label: "Note", Aliases: []string{"notes"}},
		},
		Parse: func(ctx *importCtx, rec map[string]string) (any, map[string]any, []string) {
			summary := map[string]any{"rank": rec["rank"], "total": rec["total"], "observed_at": rec["observed_at"]}
			rank, err := strconv.Atoi(strings.TrimSpace(rec["rank"]))
			if err != nil || rank < 1 {
				return nil, summary, []string{"Rank is not a rank from 1"}
			}
			total, err := optIntCell(rec["total"])
			if err != nil || total == nil {
				return nil, summary, []string{"Total is not a whole number"}
			}
			t := int64(*total)
			at, msg := validateResourceObservation(resourceObservationIn{ResourceTotal: &t, ObservedAt: rec["observed_at"]}, time.Now())
			if msg != "" {
				return nil, summary, []string{msg}
			}
			summary["observed_at"] = landST(&at)
			return cutoffImport{rank: rank, total: t, at: at, note: rec["note"]}, summary, nil
		},
		Write: func(tx *sql.Tx, ctx *importCtx, items []any) (int, int, error) {
			written, unchanged := 0, 0
			for _, it := range items {
				c := it.(cutoffImport)
				ok, err := writeRankCutoffTx(tx, ctx.Season.ID, c.rank, c.total, c.at, "csv", c.note, ctx.User.ID)
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
