// handlers_season_war.go — the Season Hub's War tab (private-docs 277–280).
//
// Opponents are the season's season_alliances rows (commit 8's table, shared with the Ally
// modal). Their figures are observations, appended and never edited; the latest value of each
// figure is what the matchup compares, with its instant. Our own side is computed on read from
// the roster's latest power and hero power (or our own latest alliance-ranking row), and a typed
// own observation newer than those overrides a figure. Opponent player rosters are never stored
// (decision 12): the pairing view is the browser's, from the Scout Report.

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

func loadWarConfig(q rowQuerier, seasonID int) (warConfig, error) {
	c := defaultWarConfig(seasonID)
	var week, day sql.NullInt64
	err := q.QueryRow(`SELECT weight_power, weight_top10, weight_top5, weight_top3, band_very_favorable, band_favorable,
		band_unfavorable, band_very_unfavorable, min_categories, loot_step_pct, loot_max_pct, production_stop_week,
		production_stop_day, war_event_type_id FROM season_war_config WHERE season_id = ?`, seasonID).Scan(&c.WeightPower, &c.WeightTop10,
		&c.WeightTop5, &c.WeightTop3, &c.BandVeryFavorable, &c.BandFavorable, &c.BandUnfavorable, &c.BandVeryUnfavorable,
		&c.MinCategories, &c.LootStepPct, &c.LootMaxPct, &week, &day, &c.WarEventTypeID)
	if err != nil && err != sql.ErrNoRows {
		return c, err
	}
	saved := err == nil
	if err := q.QueryRow(`SELECT target_war_rank FROM seasons WHERE id = ?`, seasonID).Scan(&c.TargetWarRank); err != nil && err != sql.ErrNoRows {
		return c, err
	}
	if !saved {
		return c, nil
	}
	c.Saved = true
	if week.Valid {
		v := int(week.Int64)
		c.ProductionStopWeek = &v
	}
	if day.Valid {
		v := int(day.Int64)
		c.ProductionStopDay = &v
	}
	return c, nil
}

// warFigure is one figure with where it came from: its instant (UTC, SQLite layout) and source.
type warFigure struct {
	Value  *int64 `json:"value"`
	At     string `json:"at"`
	Source string `json:"source"`
}

// warSide is one side's figures as the War tab shows them.
type warSide struct {
	Power       warFigure `json:"power"`
	MemberCount warFigure `json:"member_count"`
	GroupRank   warFigure `json:"group_rank"`
	Top10       warFigure `json:"top10_thp"`
	Top5        warFigure `json:"top5_thp"`
	Top3        warFigure `json:"top3_thp"`
}

func (s warSide) figures() sideFigures {
	return sideFigures{Power: s.Power.Value, Top10: s.Top10.Value, Top5: s.Top5.Value, Top3: s.Top3.Value}
}

// warObservation is one appended row.
type warObservation struct {
	ID               int    `json:"id"`
	SeasonAllianceID *int   `json:"season_alliance_id"`
	IsOwn            bool   `json:"is_own"`
	ObservedAt       string `json:"observed_at"` // ST
	AlliancePower    *int64 `json:"alliance_power"`
	MemberCount      *int64 `json:"member_count"`
	GroupRank        *int64 `json:"group_rank"`
	Top10            *int64 `json:"top10_thp"`
	Top5             *int64 `json:"top5_thp"`
	Top3             *int64 `json:"top3_thp"`
	Source           string `json:"source"`
	Note             string `json:"note"`
	at               time.Time
}

func loadWarObservations(q rowQueryer, seasonID int) ([]warObservation, error) {
	rows, err := q.Query(`SELECT id, season_alliance_id, is_own, observed_at, alliance_power, member_count, group_rank,
		top10_thp, top5_thp, top3_thp, source, note FROM season_opponent_observations WHERE season_id = ?
		ORDER BY observed_at, id`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []warObservation{}
	for rows.Next() {
		var o warObservation
		var own int
		var at string
		if err := rows.Scan(&o.ID, &o.SeasonAllianceID, &own, &at, &o.AlliancePower, &o.MemberCount, &o.GroupRank,
			&o.Top10, &o.Top5, &o.Top3, &o.Source, &o.Note); err != nil {
			return nil, err
		}
		o.IsOwn = own == 1
		if t := parseLandStored(at); t != nil {
			o.at = *t
			o.ObservedAt = landST(t)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// applyObservations lays observations (oldest first) over a side: each figure takes the latest
// row that has it, when that row is newer than what the side already holds.
func applyObservations(side *warSide, obs []warObservation) {
	set := func(f *warFigure, v *int64, o warObservation) {
		if v == nil {
			return
		}
		if f.Value != nil && f.At != "" {
			if cur := parseLandStored(f.At); cur != nil && cur.After(o.at) {
				return
			}
		}
		val := *v
		*f = warFigure{Value: &val, At: landUTC(o.at), Source: "observation · " + o.Source}
	}
	for _, o := range obs {
		set(&side.Power, o.AlliancePower, o)
		set(&side.MemberCount, o.MemberCount, o)
		set(&side.GroupRank, o.GroupRank, o)
		set(&side.Top10, o.Top10, o)
		set(&side.Top5, o.Top5, o)
		set(&side.Top3, o.Top3, o)
	}
}

// ownTopMember is one of our strongest members by THP, for the pairing view.
type ownTopMember struct {
	MemberID   int    `json:"member_id"`
	Name       string `json:"name"`
	HeroPower  int64  `json:"hero_power"`
	RecordedAt string `json:"recorded_at"`
}

// ownWarSide computes our side from the roster: power from our latest own alliance-ranking row,
// else the sum of active members' latest power; THP from their latest hero power. Each figure's
// instant is the newest reading it used.
func ownWarSide(q rowQueryer) (warSide, []ownTopMember, error) {
	var side warSide
	var power, members sql.NullInt64
	var at string
	err := q.QueryRow(`SELECT power, member_count, recorded_at FROM alliance_stats_history WHERE is_own = 1 AND power IS NOT NULL
		ORDER BY recorded_at DESC LIMIT 1`).Scan(&power, &members, &at)
	if err != nil && err != sql.ErrNoRows {
		return side, nil, err
	}
	if power.Valid {
		v := power.Int64
		side.Power = warFigure{Value: &v, At: storedUTC(at), Source: "alliance ranking"}
	}
	rows, err := q.Query(`SELECT m.id, m.name,
		(SELECT power FROM power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1),
		(SELECT recorded_at FROM power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1),
		(SELECT power FROM hero_power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1),
		(SELECT recorded_at FROM hero_power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1)
		FROM members m WHERE m.rank != 'EX'`)
	if err != nil {
		return side, nil, err
	}
	var sumPower int64
	var powerAt string
	var active int64
	var thp []ownTopMember
	for rows.Next() {
		var id int
		var name string
		var p, h sql.NullInt64
		var pAt, hAt sql.NullString
		if err := rows.Scan(&id, &name, &p, &pAt, &h, &hAt); err != nil {
			rows.Close()
			return side, nil, err
		}
		active++
		if p.Valid {
			sumPower += p.Int64
			if s := storedUTC(pAt.String); s > powerAt {
				powerAt = s
			}
		}
		if h.Valid && h.Int64 > 0 {
			thp = append(thp, ownTopMember{MemberID: id, Name: name, HeroPower: h.Int64, RecordedAt: storedUTC(hAt.String)})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return side, nil, err
	}
	if side.Power.Value == nil && sumPower > 0 {
		side.Power = warFigure{Value: &sumPower, At: powerAt, Source: "sum of members' power"}
	}
	side.MemberCount = warFigure{Value: &active, At: landUTC(time.Now()), Source: "roster"}
	sort.Slice(thp, func(i, j int) bool { return thp[i].HeroPower > thp[j].HeroPower })
	values := make([]int64, len(thp))
	for i, m := range thp {
		values[i] = m.HeroPower
	}
	for _, n := range []struct {
		f *warFigure
		n int
	}{{&side.Top10, 10}, {&side.Top5, 5}, {&side.Top3, 3}} {
		if v := topNSum(values, n.n); v != nil {
			at := ""
			for _, m := range thp[:n.n] {
				if m.RecordedAt > at {
					at = m.RecordedAt
				}
			}
			*n.f = warFigure{Value: v, At: at, Source: "members' hero power"}
		}
	}
	if len(thp) > 10 {
		thp = thp[:10]
	}
	return side, thp, nil
}

// storedUTC normalises a stored timestamp (either driver shape) to the SQLite layout, "" if bad.
func storedUTC(s string) string {
	if t := parseLandStored(s); t != nil {
		return landUTC(*t)
	}
	return ""
}

// warOpponent is a season alliance with its figures and matchup.
type warOpponent struct {
	SeasonAlliance
	LastRankID string        `json:"lastrank_id"`
	Figures    warSide       `json:"figures"`
	Matchup    matchupResult `json:"matchup"`
}

// GET /api/season-hub/war?season_id= — everything the War tab shows.
func handleSeasonWarGet(w http.ResponseWriter, r *http.Request) {
	sid, _ := strconv.Atoi(r.URL.Query().Get("season_id"))
	s := landSeason(w, sid)
	if s == nil {
		return
	}
	fail := func(what string, err error) {
		slog.Error("handleSeasonWarGet: "+what, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
	}
	cfg, err := loadWarConfig(db, sid)
	if err != nil {
		fail("config", err)
		return
	}
	factions, err := loadSeasonFactions(db, sid)
	if err != nil {
		fail("factions", err)
		return
	}
	alliances, err := loadSeasonAlliances(db, sid)
	if err != nil {
		fail("alliances", err)
		return
	}
	obs, err := loadWarObservations(db, sid)
	if err != nil {
		fail("observations", err)
		return
	}
	own, top, err := ownWarSide(db)
	if err != nil {
		fail("own", err)
		return
	}
	lastrank := map[int]string{}
	lrows, err := db.Query(`SELECT ea.id, COALESCE(ea.lastrank_id, '') FROM season_alliances sa JOIN external_alliances ea
		ON ea.id = sa.external_alliance_id WHERE sa.season_id = ?`, sid)
	if err != nil {
		fail("lastrank ids", err)
		return
	}
	for lrows.Next() {
		var id int
		var lr string
		lrows.Scan(&id, &lr)
		lastrank[id] = lr
	}
	lrows.Close()

	byAlliance := map[int][]warObservation{}
	var ownObs []warObservation
	for _, o := range obs {
		if o.IsOwn {
			ownObs = append(ownObs, o)
		} else if o.SeasonAllianceID != nil {
			byAlliance[*o.SeasonAllianceID] = append(byAlliance[*o.SeasonAllianceID], o)
		}
	}
	applyObservations(&own, ownObs)
	opponents := []warOpponent{}
	for _, a := range alliances {
		var side warSide
		if a.GroupRank != nil {
			v := int64(*a.GroupRank)
			side.GroupRank = warFigure{Value: &v, Source: "season row"}
		}
		applyObservations(&side, byAlliance[a.ID])
		opponents = append(opponents, warOpponent{SeasonAlliance: a, LastRankID: lastrank[a.ExternalAllianceID],
			Figures: side, Matchup: computeMatchup(own.figures(), side.figures(), cfg)})
	}
	wars, err := loadSeasonWars(db, sid)
	if err != nil {
		fail("wars", err)
		return
	}
	battles, err := loadFactionBattles(db, sid)
	if err != nil {
		fail("faction battles", err)
		return
	}
	record, factionRecords := warRecordsFor(wars, battles, alliances, factions, gametimeDateOf(time.Now()))
	resources, err := loadWarResources(db, s, cfg, opponents, time.Now())
	if err != nil {
		fail("resources", err)
		return
	}
	posts, err := loadTradePosts(db, sid, alliances)
	if err != nil {
		fail("trade posts", err)
		return
	}
	postChoices := []landArea{}
	for _, a := range landLookupFor(s.SeasonNumber) {
		if a.Kind == "trade_post" {
			postChoices = append(postChoices, a)
		}
	}
	links, err := warLinkChoices(getAuthUser(r), sid)
	if err != nil {
		fail("links", err)
		return
	}
	recent := []warObservation{}
	for i := len(obs) - 1; i >= 0 && len(recent) < 200; i-- {
		recent = append(recent, obs[i])
	}
	writeJSON(w, map[string]any{
		"season":       map[string]any{"id": s.ID, "season_number": s.SeasonNumber, "archived": s.ArchivedAt != ""},
		"config":       cfg,
		"factions":     factions,
		"own":          own,
		"own_top":      top,
		"opponents":    opponents,
		"observations": recent,
		"wars":         wars,
		"battles":      battles,
		"record":       record,
		"factions_wl":  factionRecords,
		"start_date":   s.StartDate,
		"links":        links,
		"resources":    resources,
		"trade_posts":  posts,
		"post_lookup":  postChoices,
	})
}

// PUT /api/season-hub/war/config — the season's War settings.
func handleSeasonWarConfigPut(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var c warConfig
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if ok, msg := seasonAcceptsEdits(c.SeasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	if msg := validateWarConfig(c); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	if _, err := db.Exec(`INSERT INTO season_war_config (season_id, weight_power, weight_top10, weight_top5, weight_top3,
		band_very_favorable, band_favorable, band_unfavorable, band_very_unfavorable, min_categories, loot_step_pct,
		loot_max_pct, production_stop_week, production_stop_day, war_event_type_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(season_id) DO UPDATE SET weight_power = excluded.weight_power, weight_top10 = excluded.weight_top10,
		weight_top5 = excluded.weight_top5, weight_top3 = excluded.weight_top3,
		band_very_favorable = excluded.band_very_favorable, band_favorable = excluded.band_favorable,
		band_unfavorable = excluded.band_unfavorable, band_very_unfavorable = excluded.band_very_unfavorable,
		min_categories = excluded.min_categories, loot_step_pct = excluded.loot_step_pct, loot_max_pct = excluded.loot_max_pct,
		production_stop_week = excluded.production_stop_week, production_stop_day = excluded.production_stop_day,
		war_event_type_id = excluded.war_event_type_id`,
		c.SeasonID, c.WeightPower, c.WeightTop10, c.WeightTop5, c.WeightTop3, c.BandVeryFavorable, c.BandFavorable,
		c.BandUnfavorable, c.BandVeryUnfavorable, c.MinCategories, c.LootStepPct, c.LootMaxPct, c.ProductionStopWeek,
		c.ProductionStopDay, c.WarEventTypeID); err != nil {
		slog.Error("handleSeasonWarConfigPut", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := db.Exec(`UPDATE seasons SET target_war_rank = ? WHERE id = ?`, c.TargetWarRank, c.SeasonID); err != nil {
		slog.Error("handleSeasonWarConfigPut: target rank", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "updated", "season_war_config", seasonTagName(&c.SeasonID), true)
	writeJSON(w, map[string]string{"message": "Saved"})
}

func validateWarConfig(c warConfig) string {
	for _, v := range []int{c.WeightPower, c.WeightTop10, c.WeightTop5, c.WeightTop3} {
		if v < 0 || v > 100 {
			return "weights are whole numbers from 0 to 100"
		}
	}
	if c.WeightPower+c.WeightTop10+c.WeightTop5+c.WeightTop3 == 0 {
		return "at least one weight must be above 0"
	}
	if !(c.BandVeryFavorable > c.BandFavorable && c.BandFavorable > c.BandUnfavorable && c.BandUnfavorable > c.BandVeryUnfavorable) {
		return "the bands must run Very Favorable > Favorable > Unfavorable > Very Unfavorable"
	}
	if c.TargetWarRank != nil && *c.TargetWarRank < 1 {
		return "the target rank is from 1"
	}
	if c.MinCategories < 1 || c.MinCategories > 4 {
		return "the minimum number of categories is 1 to 4"
	}
	if c.LootStepPct < 0 || c.LootMaxPct < 0 || c.LootMaxPct > 100 {
		return "loot percentages are from 0 to 100"
	}
	if c.WarEventTypeID != nil {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM schedule_event_types WHERE id = ?`, *c.WarEventTypeID).Scan(&n)
		if n == 0 {
			return "the war event type is not a schedule event type"
		}
	}
	if (c.ProductionStopWeek == nil) != (c.ProductionStopDay == nil) {
		return "the production stop needs both a week and a day"
	}
	if c.ProductionStopDay != nil && (*c.ProductionStopDay < 1 || *c.ProductionStopDay > 7) {
		return "the production stop day is 1 (Monday) to 7"
	}
	return ""
}

// warObservationIn is an observation as the form, a CSV row or a phone sends it.
type warObservationIn struct {
	SeasonAllianceID *int   `json:"season_alliance_id"`
	Own              bool   `json:"own"`
	ObservedAt       string `json:"observed_at"`
	AlliancePower    *int64 `json:"alliance_power"`
	MemberCount      *int64 `json:"member_count"`
	GroupRank        *int64 `json:"group_rank"`
	Top10            *int64 `json:"top10_thp"`
	Top5             *int64 `json:"top5_thp"`
	Top3             *int64 `json:"top3_thp"`
	Note             string `json:"note"`
}

// validateWarObservation checks the figures and reads the instant ("" = now).
func validateWarObservation(in warObservationIn, now time.Time) (time.Time, string) {
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
	n := 0
	for _, v := range []*int64{in.AlliancePower, in.MemberCount, in.GroupRank, in.Top10, in.Top5, in.Top3} {
		if v != nil {
			if *v < 0 {
				return at, "figures cannot be negative"
			}
			n++
		}
	}
	if n == 0 {
		return at, "an observation needs at least one figure"
	}
	if in.MemberCount != nil && *in.MemberCount > 200 {
		return at, "member count is at most 200"
	}
	if in.GroupRank != nil && (*in.GroupRank < 1 || *in.GroupRank > 1000) {
		return at, "group rank is from 1"
	}
	return at, ""
}

// writeWarObservationTx appends an observation; the same alliance, instant and figures already
// there make it unchanged. A group rank also moves the season row's (a game fact).
func writeWarObservationTx(tx *sql.Tx, seasonID int, in warObservationIn, at time.Time, source string, userID int) (bool, error) {
	stored := landUTC(at)
	var sa any
	own := 1
	if !in.Own && in.SeasonAllianceID != nil {
		sa, own = *in.SeasonAllianceID, 0
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM season_opponent_observations WHERE season_id = ? AND is_own = ?
		AND season_alliance_id IS ? AND observed_at = ? AND alliance_power IS ? AND member_count IS ? AND group_rank IS ?
		AND top10_thp IS ? AND top5_thp IS ? AND top3_thp IS ?`, seasonID, own, sa, stored, in.AlliancePower, in.MemberCount,
		in.GroupRank, in.Top10, in.Top5, in.Top3).Scan(&n); err != nil || n > 0 {
		return false, err
	}
	var by any
	if userID > 0 {
		by = userID
	}
	if _, err := tx.Exec(`INSERT INTO season_opponent_observations (season_id, season_alliance_id, is_own, observed_at,
		alliance_power, member_count, group_rank, top10_thp, top5_thp, top3_thp, source, recorded_by, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, seasonID, sa, own, stored, in.AlliancePower, in.MemberCount,
		in.GroupRank, in.Top10, in.Top5, in.Top3, source, by, strings.TrimSpace(in.Note)); err != nil {
		return false, err
	}
	if own == 0 && in.GroupRank != nil {
		if _, err := tx.Exec(`UPDATE season_alliances SET group_rank = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			*in.GroupRank, sa); err != nil {
			return false, err
		}
	}
	return true, nil
}

// POST /api/season-hub/war/observations {season_id, season_alliance_id | own, observed_at, …}
func handleSeasonWarObservationCreate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var body struct {
		SeasonID int `json:"season_id"`
		warObservationIn
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
	at, msg := validateWarObservation(body.warObservationIn, time.Now())
	if msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleSeasonWarObservationCreate: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	written, err := writeWarObservationTx(tx, body.SeasonID, body.warObservationIn, at, "manual", user.ID)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		slog.Error("handleSeasonWarObservationCreate: write", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if written {
		logActivity(user.ID, user.Username, "created", "season_opponent_observation", label+", "+seasonTagName(&body.SeasonID), false,
			"observed "+landST(&at)+" ST")
	}
	writeJSON(w, map[string]any{"written": written})
}

// DELETE /api/season-hub/war/observations/{id} — a mistaken observation.
func handleSeasonWarObservationDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid observation ID", http.StatusBadRequest)
		return
	}
	var seasonID int
	var at string
	if err := db.QueryRow(`SELECT season_id, observed_at FROM season_opponent_observations WHERE id = ?`, id).Scan(&seasonID, &at); err == sql.ErrNoRows {
		http.Error(w, "Observation not found", http.StatusNotFound)
		return
	} else if err != nil {
		slog.Error("handleSeasonWarObservationDelete: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if ok, msg := seasonAcceptsEdits(seasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	if _, err := db.Exec(`DELETE FROM season_opponent_observations WHERE id = ?`, id); err != nil {
		slog.Error("handleSeasonWarObservationDelete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "deleted", "season_opponent_observation", seasonTagName(&seasonID), false,
		"observed "+landST(parseLandStored(at))+" ST")
	writeJSON(w, map[string]string{"message": "Deleted"})
}

// seasonAllianceByTagServer resolves an alliance of the season by tag AND server (tags repeat
// across servers); 0 when the season has no such row.
func seasonAllianceByTagServer(alliances []SeasonAlliance, tag string, server int) int {
	for _, a := range alliances {
		if strings.EqualFold(a.Tag, tag) && a.Server == server {
			return a.ID
		}
	}
	return 0
}

// ── CSV kind ───────────────────────────────────────────────────────────────

type warObservationImport struct {
	in warObservationIn
	at time.Time
}

func init() {
	registerSeasonImportKind("opponent_observations", importKind{
		Title: "Opponent observations",
		Perms: []string{"manage_season_war"},
		Columns: []importColumn{
			{Key: "tag", Label: "Tag", Aliases: []string{"alliance", "alliance_tag"}, Required: true},
			{Key: "server", Label: "Server", Aliases: []string{"srv", "server_id"}, Required: true},
			{Key: "power", Label: "Power", Aliases: []string{"alliance_power"}},
			{Key: "members", Label: "Members", Aliases: []string{"member_count"}},
			{Key: "rank", Label: "Rank", Aliases: []string{"group_rank"}},
			{Key: "top10", Label: "Top10 THP", Aliases: []string{"top10_thp", "top_10_thp", "top_10"}},
			{Key: "top5", Label: "Top5 THP", Aliases: []string{"top5_thp", "top_5_thp", "top_5"}},
			{Key: "top3", Label: "Top3 THP", Aliases: []string{"top3_thp", "top_3_thp", "top_3"}},
			{Key: "observed_at", Label: "Observed at", Aliases: []string{"at", "time", "when", "observed"}},
			{Key: "note", Label: "Note", Aliases: []string{"notes"}},
		},
		Prepare: func(ctx *importCtx) error {
			a, err := loadSeasonAlliances(db, ctx.Season.ID)
			ctx.Data["alliances"] = a
			return err
		},
		Parse: func(ctx *importCtx, rec map[string]string) (any, map[string]any, []string) {
			summary := map[string]any{"alliance": rec["tag"] + " · S" + rec["server"], "observed_at": rec["observed_at"]}
			var errs []string
			server, err := strconv.Atoi(strings.TrimSpace(rec["server"]))
			in := warObservationIn{ObservedAt: rec["observed_at"], Note: rec["note"]}
			if err != nil || server <= 0 {
				errs = append(errs, "Server is not a server number")
			} else if isOwnAlliance("", rec["tag"]) {
				in.Own = true
				summary["alliance"] = "ours"
			} else if id := seasonAllianceByTagServer(ctx.Data["alliances"].([]SeasonAlliance), rec["tag"], server); id != 0 {
				in.SeasonAllianceID = &id
			} else {
				errs = append(errs, "not one of this season's alliances — add it on the War tab first")
			}
			for _, c := range []struct {
				key string
				dst **int64
			}{{"power", &in.AlliancePower}, {"members", &in.MemberCount}, {"rank", &in.GroupRank},
				{"top10", &in.Top10}, {"top5", &in.Top5}, {"top3", &in.Top3}} {
				v, err := optIntCell(rec[c.key])
				if err != nil {
					errs = append(errs, c.key+" is not a whole number")
					continue
				}
				if v != nil {
					x := int64(*v)
					*c.dst = &x
					summary[c.key] = x
				}
			}
			if len(errs) > 0 {
				return nil, summary, errs
			}
			at, msg := validateWarObservation(in, time.Now())
			if msg != "" {
				return nil, summary, []string{msg}
			}
			summary["observed_at"] = landST(&at)
			return warObservationImport{in: in, at: at}, summary, nil
		},
		Write: func(tx *sql.Tx, ctx *importCtx, items []any) (int, int, error) {
			written, unchanged := 0, 0
			for _, it := range items {
				o := it.(warObservationImport)
				ok, err := writeWarObservationTx(tx, ctx.Season.ID, o.in, o.at, "csv", ctx.User.ID)
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

// warSeasonAllianceInUse names what still points at a season alliance row, or "".
func warSeasonAllianceInUse(id int) string {
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM season_opponent_observations WHERE season_alliance_id = ?`, id).Scan(&n)
	if n > 0 {
		return fmt.Sprintf("this alliance has %d observations this season — delete them first", n)
	}
	return ""
}

// defaultWarEventTypeTx sets a new season's war calendar type, when it has none, from its season
// events: the type of the first event labelled as the season's key event (S4: Copper War).
func defaultWarEventTypeTx(tx *sql.Tx, seasonID int64) error {
	var typeID sql.NullInt64
	err := tx.QueryRow(`SELECT e.event_type_id FROM season_events e JOIN seasons s ON s.id = e.season_id
		WHERE e.season_id = ? AND e.event_type_id IS NOT NULL AND s.key_event_name != ''
		AND LOWER(e.label) = LOWER(s.key_event_name) ORDER BY e.week_start, e.day_offset, e.id LIMIT 1`, seasonID).Scan(&typeID)
	if err == sql.ErrNoRows || (err == nil && !typeID.Valid) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO season_war_config (season_id, war_event_type_id) VALUES (?, ?)
		ON CONFLICT(season_id) DO UPDATE SET war_event_type_id = COALESCE(season_war_config.war_event_type_id, excluded.war_event_type_id)`,
		seasonID, typeID.Int64)
	return err
}

// warLinkChoices is what a war's strategy may link to, as this viewer may see it: the season's
// files (Files' own visibility), its mails (with view_comms) and the event types for the war
// type setting.
func warLinkChoices(user *AuthUser, seasonID int) (map[string]any, error) {
	type choice struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
		Kind  string `json:"kind,omitempty"`
	}
	files, mails, types := []choice{}, []choice{}, []choice{}
	if userHasPermission(user, "view_files") {
		list, err := listFilesForUser(user, &seasonID)
		if err != nil {
			return nil, err
		}
		for _, f := range list {
			files = append(files, choice{ID: f.ID, Title: f.Title, Kind: f.FileType})
		}
	}
	if userHasPermission(user, "view_comms") {
		rows, err := db.Query(`SELECT id, title FROM comms_templates WHERE season_id = ? ORDER BY title`, seasonID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c choice
			rows.Scan(&c.ID, &c.Title)
			mails = append(mails, c)
		}
		rows.Close()
	}
	rows, err := db.Query(`SELECT id, name FROM schedule_event_types WHERE active = 1 ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c choice
		rows.Scan(&c.ID, &c.Title)
		types = append(types, c)
	}
	rows.Close()
	return map[string]any{"files": files, "mails": mails, "event_types": types}, nil
}
