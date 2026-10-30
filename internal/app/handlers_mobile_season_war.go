// handlers_mobile_season_war.go — the mobile API's season_war store (private-docs 271, 277,
// 278). The game shows which faction an alliance is in, its power, members and hero power,
// war results and Trade Post owners; a phone that reads them writes them here. Plans and
// judgement (strategy, terms, overrides) have no mobile path.

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
)

// GET /api/mobile/season-hub/war (view_season_war) — the active season's factions and alliance
// rows, so a phone can match what it reads to a registry identity (tag AND server).
func getMobileSeasonWar(w http.ResponseWriter, r *http.Request) {
	s, err := loadActiveSeason()
	if err != nil {
		slog.Error("getMobileSeasonWar: season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if s == nil {
		writeJSON(w, map[string]any{"season": nil, "factions": []any{}, "alliances": []any{}})
		return
	}
	factions, err := loadSeasonFactions(db, s.ID)
	if err != nil {
		slog.Error("getMobileSeasonWar: factions", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	rows, err := loadSeasonAlliances(db, s.ID)
	if err != nil {
		slog.Error("getMobileSeasonWar: alliances", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	type mobileSeasonAlliance struct {
		ID           int    `json:"id"`
		Tag          string `json:"tag"`
		Name         string `json:"name"`
		Server       int    `json:"server"`
		FactionKey   string `json:"faction_key"`
		Relationship string `json:"relationship"`
	}
	out := make([]mobileSeasonAlliance, 0, len(rows))
	for _, a := range rows {
		out = append(out, mobileSeasonAlliance{ID: a.ID, Tag: a.Tag, Name: a.Name, Server: a.Server,
			FactionKey: a.FactionKey, Relationship: a.Relationship})
	}
	wars, err := loadSeasonWars(db, s.ID)
	if err != nil {
		slog.Error("getMobileSeasonWar: wars", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	type mobileWar struct {
		ID                       int    `json:"id"`
		BattleDate               string `json:"battle_date"`
		Role                     string `json:"role"`
		Status                   string `json:"status"`
		OpponentSeasonAllianceID *int   `json:"opponent_season_alliance_id"`
	}
	mw := make([]mobileWar, 0, len(wars))
	for _, w := range wars {
		mw = append(mw, mobileWar{ID: w.ID, BattleDate: w.BattleDate, Role: w.Role, Status: w.Status, OpponentSeasonAllianceID: w.OpponentSeasonAllianceID})
	}
	writeJSON(w, map[string]any{
		"season":      map[string]any{"id": s.ID, "name": s.Name, "season_number": s.SeasonNumber, "start_date": s.StartDate, "week_count": s.WeekCount},
		"factions":    factions,
		"alliances":   out,
		"wars":        mw,
		"trade_posts": mobileTradePosts(s.ID),
	})
}

// mobileWarAlliance is one alliance a phone read: identity by tag AND server, then figures.
type mobileWarAlliance struct {
	Tag         string `json:"tag"`
	Server      int    `json:"server"`
	Name        string `json:"name"`
	FactionKey  string `json:"faction_key"`
	Power       *int64 `json:"power"`
	MemberCount *int64 `json:"member_count"`
	GroupRank   *int64 `json:"group_rank"`
	Top10       *int64 `json:"top10_thp"`
	Top5        *int64 `json:"top5_thp"`
	Top3        *int64 `json:"top3_thp"`
	// The alliance's season resource total and war rank (private-docs 279).
	ResourceTotal *int64 `json:"resource_total"`
	WarRank       *int   `json:"war_rank"`
}

// POST /api/mobile/season-hub/war/observations (manage_season_war)
// {captured_at, alliances: [{tag, server, name?, faction_key?, power?, member_count?, group_rank?,
// top10_thp?, top5_thp?, top3_thp?}]} — appended to the active season (source mobile). An alliance
// is named by tag AND server; one the registry does not know is refused (a phone never mints a
// registry row), one the registry knows but the season does not gets its season row. A faction
// key sets the alliance's faction (a game fact). Each alliance is reported written, unchanged or
// in errors[].
func postMobileSeasonWarObservations(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var body struct {
		CapturedAt string              `json:"captured_at"`
		Alliances  []mobileWarAlliance `json:"alliances"`
		// Our own total and rank, and the totals at ranks (private-docs 279).
		Own *struct {
			ResourceTotal *int64 `json:"resource_total"`
			WarRank       *int   `json:"war_rank"`
		} `json:"own"`
		Cutoffs []struct {
			Rank          int    `json:"rank"`
			ResourceTotal *int64 `json:"resource_total"`
		} `json:"cutoffs"`
		// Who holds which Trade Post (private-docs 280): an owner by season-alliance id or by tag
		// AND server; an owner the season does not list is kept as its label.
		TradePosts []struct {
			PostKey               string `json:"post_key"`
			OwnerSeasonAllianceID *int   `json:"owner_season_alliance_id"`
			OwnerTag              string `json:"owner_tag"`
			OwnerServer           int    `json:"owner_server"`
		} `json:"trade_posts"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if len(body.Alliances) > 500 || len(body.Cutoffs) > 500 || len(body.TradePosts) > 500 ||
		(len(body.Alliances) == 0 && body.Own == nil && len(body.Cutoffs) == 0 && len(body.TradePosts) == 0) {
		http.Error(w, "send 1 to 500 alliances, our own total, cutoffs or trade posts", http.StatusBadRequest)
		return
	}
	s, err := loadActiveSeason()
	if err != nil {
		slog.Error("postMobileSeasonWarObservations: season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if s == nil {
		http.Error(w, "No active season", http.StatusConflict)
		return
	}
	if ok, msg := seasonAcceptsEdits(s.ID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	factions, err := loadSeasonFactions(db, s.ID)
	if err != nil {
		slog.Error("postMobileSeasonWarObservations: factions", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	factionID := map[string]int{}
	for _, f := range factions {
		factionID[strings.ToLower(f.Key)] = f.ID
	}
	type result struct {
		Index  int    `json:"index"`
		Tag    string `json:"tag"`
		Server int    `json:"server"`
		Status string `json:"status"` // written | unchanged
	}
	type refusal struct {
		Index int    `json:"index"`
		Tag   string `json:"tag"`
		Error string `json:"error"`
	}
	type valid struct {
		i     int
		a     mobileWarAlliance
		extID int64
		in    warObservationIn
		at    time.Time
	}
	results := []result{}
	errs := []refusal{}
	var ok []valid
	now := time.Now()
	for i, a := range body.Alliances {
		a.Tag = strings.TrimSpace(a.Tag)
		if a.Tag == "" || a.Server <= 0 {
			errs = append(errs, refusal{i, a.Tag, "an alliance is named by its tag together with its server"})
			continue
		}
		if isOwnAlliance("", a.Tag) {
			errs = append(errs, refusal{i, a.Tag, "our own alliance is not an opponent"})
			continue
		}
		if a.FactionKey != "" && factionID[strings.ToLower(a.FactionKey)] == 0 {
			errs = append(errs, refusal{i, a.Tag, "faction_key " + a.FactionKey + " is not one of this season's factions"})
			continue
		}
		var extID int64
		if err := db.QueryRow(`SELECT id FROM external_alliances WHERE tag = ? COLLATE NOCASE AND server = ?
			ORDER BY updated_at DESC LIMIT 1`, a.Tag, a.Server).Scan(&extID); err == sql.ErrNoRows {
			errs = append(errs, refusal{i, a.Tag, fmt.Sprintf("[%s] on server %d is not in the alliance registry — add it on the War tab first", a.Tag, a.Server)})
			continue
		} else if err != nil {
			slog.Error("postMobileSeasonWarObservations: registry", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		in := warObservationIn{ObservedAt: body.CapturedAt, AlliancePower: a.Power, MemberCount: a.MemberCount,
			GroupRank: a.GroupRank, Top10: a.Top10, Top5: a.Top5, Top3: a.Top3}
		var at time.Time
		hasFigure := a.Power != nil || a.MemberCount != nil || a.GroupRank != nil || a.Top10 != nil || a.Top5 != nil || a.Top3 != nil
		if a.ResourceTotal != nil {
			if _, msg := validateResourceObservation(resourceObservationIn{ResourceTotal: a.ResourceTotal, WarRank: a.WarRank, ObservedAt: body.CapturedAt}, now); msg != "" {
				errs = append(errs, refusal{i, a.Tag, msg})
				continue
			}
		}
		if hasFigure {
			var msg string
			if at, msg = validateWarObservation(in, now); msg != "" {
				errs = append(errs, refusal{i, a.Tag, msg})
				continue
			}
		} else if a.ResourceTotal != nil {
			at, _ = validateResourceObservation(resourceObservationIn{ResourceTotal: a.ResourceTotal, ObservedAt: body.CapturedAt}, now)
		} else if a.FactionKey == "" {
			errs = append(errs, refusal{i, a.Tag, "nothing to record: no figures and no faction"})
			continue
		}
		ok = append(ok, valid{i, a, extID, in, at})
	}
	// Our own total and the cutoffs, checked before the transaction.
	var ownAt time.Time
	var ownIn *resourceObservationIn
	if body.Own != nil {
		in := resourceObservationIn{Own: true, ResourceTotal: body.Own.ResourceTotal, WarRank: body.Own.WarRank, ObservedAt: body.CapturedAt}
		var msg string
		if ownAt, msg = validateResourceObservation(in, now); msg != "" {
			errs = append(errs, refusal{-1, "own", msg})
		} else {
			ownIn = &in
		}
	}
	type cutoffOK struct {
		i     int
		rank  int
		total int64
		at    time.Time
	}
	var cuts []cutoffOK
	for i, c := range body.Cutoffs {
		at, msg := validateResourceObservation(resourceObservationIn{ResourceTotal: c.ResourceTotal, ObservedAt: body.CapturedAt}, now)
		if msg == "" && c.Rank < 1 {
			msg = "rank is from 1"
		}
		if msg != "" {
			errs = append(errs, refusal{i, "cutoff", msg})
			continue
		}
		cuts = append(cuts, cutoffOK{i, c.Rank, *c.ResourceTotal, at})
	}
	type postOK struct {
		i      int
		postID int
		key    string
		in     postObservationIn
		at     time.Time
	}
	var postObs []postOK
	if len(body.TradePosts) > 0 {
		alliances, err := loadSeasonAlliances(db, s.ID)
		var posts []tradePost
		if err == nil {
			posts, err = loadTradePosts(db, s.ID, nil)
		}
		if err != nil {
			slog.Error("postMobileSeasonWarObservations: posts", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		for i, tp := range body.TradePosts {
			p := postByKeyOrName(posts, tp.PostKey)
			if p == nil {
				errs = append(errs, refusal{i, tp.PostKey, "no Trade Post of this season has that key"})
				continue
			}
			in := postObservationIn{OwnerSeasonAllianceID: tp.OwnerSeasonAllianceID, ObservedAt: body.CapturedAt}
			if in.OwnerSeasonAllianceID == nil {
				if strings.TrimSpace(tp.OwnerTag) == "" || tp.OwnerServer <= 0 {
					errs = append(errs, refusal{i, tp.PostKey, "an owner is a season alliance id, or a tag together with its server"})
					continue
				}
				if id := seasonAllianceByTagServer(alliances, tp.OwnerTag, tp.OwnerServer); id != 0 {
					in.OwnerSeasonAllianceID = &id
				} else {
					in.OwnerLabel = fmt.Sprintf("%s (S%d)", strings.TrimSpace(tp.OwnerTag), tp.OwnerServer)
				}
			}
			at, msg := validatePostObservation(s.ID, in, now)
			if msg != "" {
				errs = append(errs, refusal{i, tp.PostKey, msg})
				continue
			}
			postObs = append(postObs, postOK{i, p.ID, p.PostKey, in, at})
		}
	}
	written := 0
	if len(ok) > 0 || ownIn != nil || len(cuts) > 0 || len(postObs) > 0 {
		tx, err := db.Begin()
		if err != nil {
			slog.Error("postMobileSeasonWarObservations: begin", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()
		for _, v := range ok {
			write := seasonAllianceWrite{}
			if v.a.FactionKey != "" {
				fid := factionID[strings.ToLower(v.a.FactionKey)]
				write.FactionID = optionalInt{Set: true, Value: &fid}
			}
			saID, created, err := writeSeasonAllianceTx(tx, s.ID, v.extID, write)
			if err != nil {
				slog.Error("postMobileSeasonWarObservations: season row", "error", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			changed := created || v.a.FactionKey != ""
			if v.in.AlliancePower != nil || v.in.MemberCount != nil || v.in.GroupRank != nil || v.in.Top10 != nil ||
				v.in.Top5 != nil || v.in.Top3 != nil {
				id := int(saID)
				v.in.SeasonAllianceID = &id
				wrote, err := writeWarObservationTx(tx, s.ID, v.in, v.at, "mobile", user.ID)
				if err != nil {
					slog.Error("postMobileSeasonWarObservations: write", "error", err)
					http.Error(w, "Database error", http.StatusInternalServerError)
					return
				}
				changed = changed || wrote
			}
			if v.a.ResourceTotal != nil {
				id := int(saID)
				wrote, err := writeResourceObservationTx(tx, s.ID, resourceObservationIn{SeasonAllianceID: &id,
					ResourceTotal: v.a.ResourceTotal, WarRank: v.a.WarRank}, v.at, "mobile", user.ID)
				if err != nil {
					slog.Error("postMobileSeasonWarObservations: resources", "error", err)
					http.Error(w, "Database error", http.StatusInternalServerError)
					return
				}
				changed = changed || wrote
			}
			st := "unchanged"
			if changed {
				st = "written"
				written++
			}
			results = append(results, result{v.i, v.a.Tag, v.a.Server, st})
		}
		if ownIn != nil {
			wrote, err := writeResourceObservationTx(tx, s.ID, *ownIn, ownAt, "mobile", user.ID)
			if err != nil {
				slog.Error("postMobileSeasonWarObservations: own", "error", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			st := "unchanged"
			if wrote {
				st, written = "written", written+1
			}
			results = append(results, result{-1, "own", 0, st})
		}
		for _, c := range cuts {
			wrote, err := writeRankCutoffTx(tx, s.ID, c.rank, c.total, c.at, "mobile", "", user.ID)
			if err != nil {
				slog.Error("postMobileSeasonWarObservations: cutoff", "error", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			st := "unchanged"
			if wrote {
				st, written = "written", written+1
			}
			results = append(results, result{c.i, "cutoff", c.rank, st})
		}
		for _, po := range postObs {
			wrote, err := writePostObservationTx(tx, po.postID, po.in, po.at, "mobile", user.ID)
			if err != nil {
				slog.Error("postMobileSeasonWarObservations: trade post", "error", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			st := "unchanged"
			if wrote {
				st, written = "written", written+1
			}
			results = append(results, result{po.i, "trade_post:" + po.key, 0, st})
		}
		if err := tx.Commit(); err != nil {
			slog.Error("postMobileSeasonWarObservations: commit", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if written > 0 {
		logActivity(user.ID, user.Username, "created", "season_opponent_observation", seasonTagName(&s.ID), false,
			strconv.Itoa(written)+" alliances · via mobile")
	}
	writeJSON(w, map[string]any{"season_id": s.ID, "written": written, "results": results, "errors": errs})
}

// POST /api/mobile/season-hub/war/results (manage_season_war)
// {captured_at, wars: [{war_id | (battle_date, role), outcome, participant_count?, resource_result?}],
// faction_battles: [{battle_date, alliance_tag, alliance_server, opponent_tag?, opponent_server?, role, outcome}]}
// — the result columns of the active season's wars (the status stays the officers' step) and
// faction battles (source mobile), each reported written, unchanged or in errors[].
func postMobileSeasonWarResults(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var body struct {
		CapturedAt     string        `json:"captured_at"`
		Wars           []warResultIn `json:"wars"`
		FactionBattles []struct {
			BattleDate     string `json:"battle_date"`
			AllianceTag    string `json:"alliance_tag"`
			AllianceServer int    `json:"alliance_server"`
			OpponentTag    string `json:"opponent_tag"`
			OpponentServer int    `json:"opponent_server"`
			Role           string `json:"role"`
			Outcome        string `json:"outcome"`
		} `json:"faction_battles"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if len(body.Wars)+len(body.FactionBattles) == 0 || len(body.Wars)+len(body.FactionBattles) > 500 {
		http.Error(w, "send 1 to 500 wars and faction battles", http.StatusBadRequest)
		return
	}
	s, err := loadActiveSeason()
	if err != nil {
		slog.Error("postMobileSeasonWarResults: season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if s == nil {
		http.Error(w, "No active season", http.StatusConflict)
		return
	}
	if ok, msg := seasonAcceptsEdits(s.ID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	wars, err := loadSeasonWars(db, s.ID)
	if err == nil {
		var alliances []SeasonAlliance
		alliances, err = loadSeasonAlliances(db, s.ID)
		if err == nil {
			type item struct {
				Kind   string `json:"kind"` // war | faction_battle
				Index  int    `json:"index"`
				Status string `json:"status,omitempty"`
				Error  string `json:"error,omitempty"`
			}
			results, errs := []item{}, []item{}
			var warItems []warResultIn
			var warIdx []int
			for i, wr := range body.Wars {
				wr.Role, wr.Outcome = strings.ToLower(wr.Role), strings.ToLower(wr.Outcome)
				war, msg := matchWarResult(wars, wr)
				if msg != "" {
					errs = append(errs, item{Kind: "war", Index: i, Error: msg})
					continue
				}
				wr.WarID = war.ID
				warItems = append(warItems, wr)
				warIdx = append(warIdx, i)
			}
			byID := seasonAllianceMap(alliances)
			var battles []factionBattle
			var battleIdx []int
			for i, fb := range body.FactionBattles {
				aid := seasonAllianceByTagServer(alliances, fb.AllianceTag, fb.AllianceServer)
				if aid == 0 {
					errs = append(errs, item{Kind: "faction_battle", Index: i,
						Error: fmt.Sprintf("[%s] on server %d is not one of this season's alliances", fb.AllianceTag, fb.AllianceServer)})
					continue
				}
				b := factionBattle{BattleDate: fb.BattleDate, AllianceSeasonAllianceID: aid, Role: strings.ToLower(fb.Role), Outcome: strings.ToLower(fb.Outcome)}
				if fb.OpponentTag != "" {
					if oid := seasonAllianceByTagServer(alliances, fb.OpponentTag, fb.OpponentServer); oid != 0 {
						b.OpponentSeasonAllianceID = &oid
					} else {
						b.OpponentLabel = fmt.Sprintf("%s (S%d)", fb.OpponentTag, fb.OpponentServer)
					}
				}
				if msg := validateFactionBattle(&b, s, byID); msg != "" {
					errs = append(errs, item{Kind: "faction_battle", Index: i, Error: msg})
					continue
				}
				battles = append(battles, b)
				battleIdx = append(battleIdx, i)
			}
			written := 0
			tx, err := db.Begin()
			if err != nil {
				slog.Error("postMobileSeasonWarResults: begin", "error", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			defer tx.Rollback()
			for k, wr := range warItems {
				ok, err := writeWarResultTx(tx, wr)
				if err != nil {
					slog.Error("postMobileSeasonWarResults: war", "error", err)
					http.Error(w, "Database error", http.StatusInternalServerError)
					return
				}
				st := "unchanged"
				if ok {
					st, written = "written", written+1
				}
				results = append(results, item{Kind: "war", Index: warIdx[k], Status: st})
			}
			for k, b := range battles {
				ok, msg, err := writeFactionBattleTx(tx, s.ID, b, "mobile", user.ID)
				if err != nil {
					slog.Error("postMobileSeasonWarResults: battle", "error", err)
					http.Error(w, "Database error", http.StatusInternalServerError)
					return
				}
				if msg != "" {
					errs = append(errs, item{Kind: "faction_battle", Index: battleIdx[k], Error: msg})
					continue
				}
				st := "unchanged"
				if ok {
					st, written = "written", written+1
				}
				results = append(results, item{Kind: "faction_battle", Index: battleIdx[k], Status: st})
			}
			if err := tx.Commit(); err != nil {
				slog.Error("postMobileSeasonWarResults: commit", "error", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			if written > 0 {
				logActivity(user.ID, user.Username, "updated", "season_war", seasonTagName(&s.ID), false,
					strconv.Itoa(written)+" results · via mobile")
			}
			writeJSON(w, map[string]any{"season_id": s.ID, "written": written, "results": results, "errors": errs})
			return
		}
	}
	slog.Error("postMobileSeasonWarResults: load", "error", err)
	http.Error(w, "Database error", http.StatusInternalServerError)
}

// mobileTradePosts lists the active season's posts (key and name) for the phone to name them.
func mobileTradePosts(seasonID int) []map[string]string {
	out := []map[string]string{}
	posts, err := loadTradePosts(db, seasonID, nil)
	if err != nil {
		slog.Error("mobileTradePosts", "error", err)
		return out
	}
	for _, p := range posts {
		out = append(out, map[string]string{"post_key": p.PostKey, "name": p.Name})
	}
	return out
}
