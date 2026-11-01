// handlers_season_dashboard.go — the Season Hub's dashboard (private-docs 287).
//
// One endpoint answers every card, computed server-side from the engines the tabs use (status,
// war record, land readiness, resources) and filtered by permission: Leadership Flags only for
// participation managers, Land Path Status for view_season_land, War Record and Reward Tier
// Status for view_season_war, the Season Schedule for view_schedule; strategy fields never. The
// season's layout decides which cards show and in what order.

package app

import (
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// dashboardAlly is one allied alliance on the Faction Allies card.
type dashboardAlly struct {
	Tag        string   `json:"tag"`
	Name       string   `json:"name"`
	Server     int      `json:"server"`
	Terms      string   `json:"terms"`
	Agreements []string `json:"agreements"`
}

type dashboardFaction struct {
	FactionID *int            `json:"faction_id"`
	Label     string          `json:"label"`
	IsOurs    bool            `json:"is_ours"`
	Allies    []dashboardAlly `json:"allies"`
}

// dashboardFlagMember is one member on the Leadership Flags card.
type dashboardFlagMember struct {
	MemberID int     `json:"member_id"`
	Name     string  `json:"name"`
	Rank     string  `json:"rank"`
	Pct      float64 `json:"pct"`
	Status   string  `json:"status"`
	Reason   string  `json:"reason"`
}

type dashboardEvent struct {
	Date     string `json:"date"`
	Time     string `json:"time"`
	AllDay   bool   `json:"all_day"`
	TypeName string `json:"type_name"`
	Icon     string `json:"icon"`
	Status   string `json:"status"`
}

// bottomFive is the lowest overall percentages among current members with a rating: five, plus
// every member tied with the fifth; everyone when there are fewer than five.
func bottomFive(members []SeasonMember) []dashboardFlagMember {
	var rated []SeasonMember
	for _, m := range members {
		if m.Rank != "EX" && m.Standing.Overall.Applicable {
			rated = append(rated, m)
		}
	}
	sort.SliceStable(rated, func(i, j int) bool { return rated[i].Standing.Overall.Pct < rated[j].Standing.Overall.Pct })
	out := []dashboardFlagMember{}
	for i, m := range rated {
		if i >= 5 && m.Standing.Overall.Pct != rated[4].Standing.Overall.Pct {
			break
		}
		out = append(out, dashboardFlagMember{MemberID: m.MemberID, Name: m.Name, Rank: m.Rank, Pct: m.Standing.Overall.Pct,
			Status: m.Standing.Overall.Status, Reason: m.Standing.Overall.Reason})
	}
	return out
}

// GET /api/season-hub/dashboard?season_id= (view_season_hub)
func handleSeasonDashboard(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	sid, _ := strconv.Atoi(r.URL.Query().Get("season_id"))
	s := landSeason(w, sid)
	if s == nil {
		return
	}
	fail := func(what string, err error) {
		slog.Error("handleSeasonDashboard: "+what, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
	}
	now := time.Now()
	layout, err := loadSeasonLayout(db, s.ID)
	if err != nil {
		fail("layout", err)
		return
	}
	data := map[string]any{}

	// (1) Faction allies and agreements: the season's allied alliances by faction.
	factions, err := loadSeasonFactions(db, s.ID)
	if err != nil {
		fail("factions", err)
		return
	}
	alliances, err := loadSeasonAlliances(db, s.ID)
	if err != nil {
		fail("alliances", err)
		return
	}
	agreements := map[int][]string{}
	arows, err := db.Query(`SELECT aa.ally_id, t.name FROM ally_agreements aa JOIN ally_agreement_types t ON t.id = aa.agreement_type_id
		WHERE t.active = 1 ORDER BY t.sort_order, t.id`)
	if err != nil {
		fail("agreements", err)
		return
	}
	for arows.Next() {
		var ally int
		var name string
		arows.Scan(&ally, &name)
		agreements[ally] = append(agreements[ally], name)
	}
	arows.Close()
	groups := []dashboardFaction{}
	byFaction := map[int]int{}
	for _, f := range factions {
		id := f.ID
		byFaction[f.ID] = len(groups)
		groups = append(groups, dashboardFaction{FactionID: &id, Label: f.Label, IsOurs: f.IsOurs, Allies: []dashboardAlly{}})
	}
	noFaction := -1
	for _, a := range alliances {
		if a.Relationship != "allied" {
			continue
		}
		da := dashboardAlly{Tag: a.Tag, Name: a.Name, Server: a.Server, Terms: a.Terms, Agreements: []string{}}
		if a.AllyID != nil {
			da.Agreements = append(da.Agreements, agreements[*a.AllyID]...)
		}
		gi, ok := -1, false
		if a.FactionID != nil {
			gi, ok = byFaction[*a.FactionID]
		}
		if !ok {
			if noFaction < 0 {
				noFaction = len(groups)
				groups = append(groups, dashboardFaction{Label: "No faction", Allies: []dashboardAlly{}})
			}
			gi = noFaction
		}
		groups[gi].Allies = append(groups[gi].Allies, da)
	}
	data["faction_allies"] = groups

	// (2) Leadership flags: participation managers only.
	if userHasPermission(user, "manage_season_participation") {
		members, hasWars, err := buildSeasonMembers(s)
		if err != nil {
			fail("members", err)
			return
		}
		flagged := []dashboardFlagMember{}
		for _, m := range members {
			if m.Rank == "EX" {
				continue
			}
			if m.Standing.Final == "at_risk" || m.Standing.Final == "below" {
				from := m.Standing.Overall
				switch m.Standing.FinalFrom {
				case "polls":
					from = m.Standing.Polls
				case "wars":
					from = m.Standing.Wars
				}
				flagged = append(flagged, dashboardFlagMember{MemberID: m.MemberID, Name: m.Name, Rank: m.Rank, Pct: from.Pct,
					Status: m.Standing.Final, Reason: m.Standing.FinalFrom + ": " + from.Reason})
			}
		}
		sort.SliceStable(flagged, func(i, j int) bool {
			if flagged[i].Status != flagged[j].Status {
				return flagged[i].Status == "below"
			}
			return flagged[i].Pct < flagged[j].Pct
		})
		reviews := []pollReview{}
		if hasWars {
			if reviews, err = pendingPollReviews(db, s.ID, gametimeDateOf(now)); err != nil {
				fail("poll reviews", err)
				return
			}
		}
		data["leadership_flags"] = map[string]any{"bottom": bottomFive(members), "flagged": flagged, "poll_reviews": reviews,
			"completed_weeks": completedSeasonWeeks(s, now)}
	}

	// (3) Season schedule: today, the next event and the next seven days, and the conflicts.
	if userHasPermission(user, "view_schedule") {
		today := gametimeDateOf(now)
		week := now.In(gametimeLoc()).AddDate(0, 0, 7).Format("2006-01-02")
		cancelled := " AND se.status != 'cancelled'"
		rows, err := db.Query(`SELECT se.event_date, COALESCE(se.event_time, ''), se.all_day, t.name, t.icon, se.status
			FROM schedule_events se JOIN schedule_event_types t ON t.id = se.event_type_id
			WHERE se.season_id = ? AND se.event_date >= ?`+cancelled+scheduleVisibility(user, "se")+`
			ORDER BY se.event_date, se.all_day DESC, se.event_time LIMIT 200`, s.ID, today)
		if err != nil {
			fail("schedule", err)
			return
		}
		todays, next7 := []dashboardEvent{}, []dashboardEvent{}
		var next *dashboardEvent
		nowTime := now.In(gametimeLoc()).Format("15:04")
		for rows.Next() {
			var e dashboardEvent
			var allDay int
			if err := rows.Scan(&e.Date, &e.Time, &allDay, &e.TypeName, &e.Icon, &e.Status); err != nil {
				rows.Close()
				fail("schedule scan", err)
				return
			}
			e.AllDay = allDay == 1
			if e.Date == today {
				todays = append(todays, e)
			} else if e.Date <= week {
				next7 = append(next7, e)
			}
			if next == nil && (e.Date > today || e.AllDay || e.Time >= nowTime) {
				ev := e
				next = &ev
			}
		}
		rows.Close()
		_, conflicts, err := seasonConflicts(db, user, s.ID)
		if err != nil {
			fail("conflicts", err)
			return
		}
		data["season_schedule"] = map[string]any{"today": todays, "next": next, "next_7_days": next7, "conflicts": len(conflicts)}
	}

	// (4) Land path status.
	if userHasPermission(user, "view_season_land") {
		routes, err := loadLandRoutes(db, s.ID)
		if err != nil {
			fail("land", err)
			return
		}
		data["land_status"] = activeRouteReadiness(routes, s.LandReadinessBufferHours, now)
	}

	// (5) War record and (6) reward tier status.
	if userHasPermission(user, "view_season_war") {
		wars, err := loadSeasonWars(db, s.ID)
		if err != nil {
			fail("wars", err)
			return
		}
		battles, err := loadFactionBattles(db, s.ID)
		if err != nil {
			fail("faction battles", err)
			return
		}
		record, factionWL := warRecordsFor(wars, battles, alliances, factions, gametimeDateOf(now))
		labels := map[int]SeasonFaction{}
		for _, f := range factions {
			labels[f.ID] = f
		}
		type factionLine struct {
			Label  string   `json:"label"`
			IsOurs bool     `json:"is_ours"`
			Record wlRecord `json:"record"`
		}
		lines := []factionLine{}
		for _, f := range factionWL {
			if f.FactionID == nil {
				continue
			}
			lines = append(lines, factionLine{Label: labels[*f.FactionID].Label, IsOurs: labels[*f.FactionID].IsOurs, Record: f.Season})
		}
		cfg, err := loadWarConfig(db, s.ID)
		if err != nil {
			fail("war config", err)
			return
		}
		// The resource panel needs the opponents' matchup only for its loot list; the dashboard's
		// status reads the same engine with the opponents as the War tab sees them.
		res, err := loadWarResources(db, s, cfg, dashboardOpponents(alliances), now)
		if err != nil {
			fail("resources", err)
			return
		}
		var rank *int
		if res.Own != nil {
			rank = res.Own.WarRank
		}
		data["war_record"] = map[string]any{"record": record, "factions": lines, "war_rank": rank}
		data["reward_tier_status"] = map[string]any{"status": res.Status, "rank": res.Rank, "group": res.Group,
			"target_rank": res.TargetRank, "target_group": res.TargetGroup, "gap": res.Gap,
			"projected_at_reset": res.ProjectedAtReset, "target_cutoff": res.TargetCutoff, "own": res.Own}
	}

	writeJSON(w, map[string]any{
		"season": map[string]any{"id": s.ID, "name": s.Name, "season_number": s.SeasonNumber, "archived": s.ArchivedAt != ""},
		"cards":  layout.Cards,
		"data":   data,
	})
}

// dashboardOpponents gives the resource engine the season's alliances (relationship decides
// which count toward the largest loot; the matchup rating is not needed for the status).
func dashboardOpponents(alliances []SeasonAlliance) []warOpponent {
	out := make([]warOpponent, 0, len(alliances))
	for _, a := range alliances {
		out = append(out, warOpponent{SeasonAlliance: a})
	}
	return out
}
