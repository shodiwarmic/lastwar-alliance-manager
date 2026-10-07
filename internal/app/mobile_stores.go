// mobile_stores.go - the mobile API's store registry and table coverage map.
//
// The standing rule (CLAUDE.md checklist, step 9): any new place that stores game data
// ships with a matching /api/mobile/* endpoint in the same change, so scanner features
// never wait on a backend release (private-docs 201). This file makes the rule
// checkable: every mobile route is declared here with the permissions that gate it, and
// every table of a migrated database is either a mobile store's or excluded with a
// reason. mobile_stores_test.go fails the build when a table or route is unaccounted for.

package app

import "net/http"

// mobileAPIVersion is reported by login and capabilities. It rises whenever the
// capabilities grow; it is informational — clients test the capability maps' keys.
// A server answering 404 on /api/mobile/capabilities speaks contract 1.
const mobileAPIVersion = 2

// mobileRoute is one /api/mobile/ route. Perms is an any-of gate resolved live on every
// request; empty means any signed-in user.
type mobileRoute struct {
	Method  string
	Path    string
	Perms   []string
	Handler http.HandlerFunc
}

// mobileStore is one kind of game data the mobile API reads or writes.
type mobileStore struct {
	Key        string
	Tables     []string
	ReadPerms  []string // any-of; what capabilities reports as "read"
	WritePerms []string // any-of; what capabilities reports as "write"
	// WriteReady, when set, must also hold for "write" — a store that can't accept a
	// write on this install yet (alliance stats with no alliance tag set).
	WriteReady func() bool
	Routes     []mobileRoute
}

// mobileBaseRoutes are the routes that belong to no store: signing in, the roster every
// store resolves names against, and the capabilities report itself.
func mobileBaseRoutes() []mobileRoute {
	return []mobileRoute{
		{Method: "POST", Path: "/api/mobile/login", Handler: mobileLogin},
		{Method: "GET", Path: "/api/mobile/members", Handler: getMobileMembers},
		{Method: "GET", Path: "/api/mobile/capabilities", Handler: getMobileCapabilities},
	}
}

// mobileStores is the registry. A function, not a var, so handlers can be referenced
// without an initialisation cycle.
func mobileStores() []mobileStore {
	return []mobileStore{
		{
			Key: "scan_commit",
			Tables: []string{"vs_points", "power_history", "kill_history", "hero_power_history",
				"squad_power_history", "hq_level_history", "profession_level_history"},
			// Days follow the VS page's gate, member stats the roster's; each record is
			// checked against its own category's permission (mobileCategories).
			ReadPerms:  []string{"manage_vs_points", "manage_members"},
			WritePerms: []string{"manage_vs_points", "manage_members"},
			Routes: []mobileRoute{
				{Method: "POST", Path: "/api/mobile/preview", Perms: []string{"manage_vs_points", "manage_members"}, Handler: mobilePreview},
				{Method: "POST", Path: "/api/mobile/commit", Perms: []string{"manage_vs_points", "manage_members"}, Handler: mobileCommit},
			},
		},
		{
			// The roster itself is read through GET /api/mobile/members (any user).
			Key:        "roster",
			Tables:     []string{"members"},
			WritePerms: []string{"manage_members"},
			Routes: []mobileRoute{
				{Method: "POST", Path: "/api/mobile/members/attributes", Perms: []string{"manage_members"}, Handler: mobileMemberAttributes},
				{Method: "POST", Path: "/api/mobile/roster/changes", Perms: []string{"manage_members"}, Handler: mobileRosterChanges},
			},
		},
		{
			Key:        "prospects",
			Tables:     []string{"prospects"},
			ReadPerms:  []string{"view_recruiting"},
			WritePerms: []string{"manage_recruiting"},
			Routes: []mobileRoute{
				{Method: "GET", Path: "/api/mobile/prospects", Perms: []string{"view_recruiting"}, Handler: getMobileProspects},
				{Method: "POST", Path: "/api/mobile/prospects", Perms: []string{"manage_recruiting"}, Handler: postMobileProspect},
			},
		},
		{
			Key:        "season_contributions",
			Tables:     []string{"season_member_records"},
			ReadPerms:  []string{"view_season_hub"},
			WritePerms: []string{"manage_season_hub"},
			Routes: []mobileRoute{
				{Method: "GET", Path: "/api/mobile/season-hub", Perms: []string{"view_season_hub"}, Handler: getMobileSeasonHub},
				{Method: "POST", Path: "/api/mobile/season-hub/contributions/preview", Perms: []string{"manage_season_hub"}, Handler: mobileContributionsPreview},
				{Method: "POST", Path: "/api/mobile/season-hub/contributions/commit", Perms: []string{"manage_season_hub"}, Handler: mobileContributionsCommit},
			},
		},
		{
			Key:        "participation",
			Tables:     []string{"participation_boards", "participation_entries", "participation_values"},
			ReadPerms:  []string{"view_participation", "manage_participation"},
			WritePerms: []string{"manage_participation"},
			Routes: []mobileRoute{
				{Method: "GET", Path: "/api/mobile/participation/types", Perms: []string{"view_participation", "manage_participation"}, Handler: getMobileParticipationTypes},
				{Method: "POST", Path: "/api/mobile/participation/preview", Perms: []string{"manage_participation"}, Handler: mobileParticipationPreview},
				{Method: "POST", Path: "/api/mobile/participation/import", Perms: []string{"manage_participation"}, Handler: mobileParticipationImport},
				{Method: "POST", Path: "/api/mobile/participation/occurrences", Perms: []string{"manage_participation"}, Handler: mobileParticipationOccurrence},
				{Method: "PUT", Path: "/api/mobile/participation/boards/{eventID:[0-9]+}", Perms: []string{"manage_participation"}, Handler: mobileParticipationBoardPut},
			},
		},
		{
			Key:        "vs_league",
			Tables:     []string{"vs_league_weeks", "vs_league_days", "vs_league_matchups"},
			ReadPerms:  []string{"view_vs_points"},
			WritePerms: []string{"manage_vs_points"},
			Routes: []mobileRoute{
				{Method: "GET", Path: "/api/mobile/vs-league/current", Perms: []string{"view_vs_points"}, Handler: getMobileVSLeagueCurrent},
				{Method: "POST", Path: "/api/mobile/vs-league/week", Perms: []string{"manage_vs_points"}, Handler: postMobileVSLeagueWeek},
			},
		},
		{
			Key:        "train_logs",
			Tables:     []string{"train_logs"},
			ReadPerms:  []string{"view_train"},
			WritePerms: []string{"manage_train"},
			Routes: []mobileRoute{
				{Method: "POST", Path: "/api/mobile/train-logs", Perms: []string{"manage_train"}, Handler: postMobileTrainLog},
			},
		},
		{
			Key:        "alliance_stats",
			Tables:     []string{"alliance_stats_history", "external_alliances"},
			ReadPerms:  []string{"view_allies"},
			WritePerms: []string{"manage_allies"},
			WriteReady: allianceStatsWritable,
			Routes: []mobileRoute{
				{Method: "POST", Path: "/api/mobile/alliance-stats", Perms: []string{"manage_allies"}, Handler: postMobileAllianceStats},
			},
		},
	}
}

// mobileExcludedTables classifies every table that is not a mobile store's, with the
// reason. A table in neither this map nor a store fails mobile_stores_test.go.
var mobileExcludedTables = map[string]string{

	// Officer judgement or app data: nothing the game shows.
	"member_aliases":               "app data: officers' name-resolution mappings, written alongside scans by save_aliases",
	"season_participation":         "officer judgement: participation levels, notes and poll votes",
	"participation_roles":          "officer judgement: a board's lineup",
	"participation_exceptions":     "officer judgement: excusals and dismissals",
	"participation_types":          "configuration: event types and their rules",
	"participation_trackables":     "configuration: what a participation type records",
	"accountability_strikes":       "officer judgement",
	"strike_types":                 "configuration",
	"storm_registrations":          "members' own slot availability, not a game reading",
	"storm_assignments":            "officer judgement: the Desert Storm planner",
	"storm_groups":                 "officer judgement: the Desert Storm planner",
	"storm_group_members":          "officer judgement: the Desert Storm planner",
	"storm_group_buildings":        "officer judgement: the Desert Storm planner",
	"storm_group_building_members": "officer judgement: the Desert Storm planner",
	"storm_slot_times":             "configuration: the Desert Storm planner",
	"storm_tf_config":              "configuration: the Desert Storm planner",
	"member_skills":                "app-defined tags (ValidSkillKeys), not a game reading",
	"awards":                       "officer judgement",
	"award_types":                  "configuration",
	"dyno_recommendations":         "officer judgement",
	"recommendations":              "officer judgement",
	"vs_league_seasons":            "setup: a week needs an active season, started on the web",
	"seasons":                      "configuration: season setup",
	"season_events":                "configuration: season setup",
	"season_rewards":               "officer judgement: reward allocation",
	"season_reward_tiers":          "configuration: season setup",
	"season_score_levels":          "configuration: season setup",
	"season_templates":             "configuration: season setup",
	"season_trackables":            "configuration: season setup",
	"schedule_events":              "the plan, not an observation (a participation board's occurrence is created through its own route)",
	"schedule_event_types":         "configuration: the schedule",
	"server_events":                "the plan, not an observation",
	"eligibility_rules":            "configuration",
	"allies":                       "officer judgement: diplomacy",
	"ally_agreements":              "officer judgement: diplomacy",
	"ally_agreement_types":         "configuration",
	"oc_categories":                "officer judgement: the Officers directory",
	"oc_responsibilities":          "officer judgement: the Officers directory",
	"oc_assignees":                 "officer judgement: the Officers directory",
	"oc_tasks":                     "officer judgement: the Officers directory",
	"comms_templates":              "app data: communications",
	"comms_resources":              "app data: communications",
	"poll_templates":               "app data: polls",
	"poll_instances":               "app data: polls",
	"poll_responses":               "app data: polls",
	"poll_anonymous_counts":        "app data: polls",

	// Reference or legacy.
	"server_open_dates":        "reference: the starred-missions sweep",
	"storm_attendance":         "legacy: no writers",
	"lastrank_pending_changes": "LastRank's review queue",

	// System.
	"users":                 "system: accounts",
	"login_sessions":        "system",
	"password_history":      "system",
	"password_reset_tokens": "system",
	"invite_tokens":         "system",
	"rank_permissions":      "system: the permission matrix",
	"settings":              "system",
	"credentials":           "system",
	"user_dashboard_prefs":  "system",
	"background_jobs":       "system",
	"background_job_items":  "system",
	"files":                 "system: the Files feature",
	"file_tags":             "system: the Files feature",
	"file_tag_map":          "system: the Files feature",
	"translation_cache":     "system",
	"activity_log":          "system",
	"goose_db_version":      "system: migrations",
	"sqlite_sequence":       "system: SQLite",
}

// anyPermission reports whether user holds any of perms; an empty list admits anyone
// signed in.
func anyPermission(user *AuthUser, perms []string) bool {
	if len(perms) == 0 {
		return user != nil
	}
	for _, p := range perms {
		if userHasPermission(user, p) {
			return true
		}
	}
	return false
}

// GET /api/mobile/capabilities — what this server can do for the caller, computed live.
func getMobileCapabilities(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	stores := map[string]map[string]bool{}
	for _, st := range mobileStores() {
		write := anyPermission(user, st.WritePerms)
		if write && st.WriteReady != nil {
			write = st.WriteReady()
		}
		stores[st.Key] = map[string]bool{"read": anyPermission(user, st.ReadPerms), "write": write}
	}
	writeJSON(w, map[string]any{
		"api_version":       mobileAPIVersion,
		"app_version":       appVersion,
		"stores":            stores,
		"commit_categories": mobileCommitCategories(user),
	})
}

// mobileCommitCategories maps every category /api/mobile/commit accepts to whether this
// caller may write it.
func mobileCommitCategories(user *AuthUser) map[string]bool {
	held := map[string]bool{}
	out := map[string]bool{}
	for c, mc := range mobileCategories {
		ok, seen := held[mc.Perm]
		if !seen {
			ok = userHasPermission(user, mc.Perm)
			held[mc.Perm] = ok
		}
		out[c] = ok
	}
	return out
}
