// models.go - Defines all data models used in the application, including database models and frontend template contexts.

package main

import (
	"database/sql"
	"encoding/json"
	"html/template"
	"reflect"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/sessions"
)

// Global Variables
var db *sql.DB
var store *sessions.CookieStore

// --- Database Models ---

type Member struct {
	ID                  int    `json:"id"`
	Name                string `json:"name"`
	Rank                string `json:"rank"`
	Level               int    `json:"level"`
	Eligible            bool   `json:"eligible"`
	Power               *int64 `json:"power"`
	PowerUpdatedAt      string `json:"power_updated_at"`
	HasUser             bool   `json:"has_user"`
	SquadType           string `json:"squad_type"`
	SquadPower          *int64 `json:"squad_power"`
	SquadPowerUpdatedAt string `json:"squad_power_updated_at"`
	HeroPower           *int64 `json:"hero_power"`
	HeroPowerUpdatedAt  string `json:"hero_power_updated_at"`
	CurrentKills        *int64 `json:"current_kills"`
	KillsUpdatedAt      string `json:"kills_updated_at"`
	TroopLevel          int    `json:"troop_level"`
	Profession          string `json:"profession"`
	ProfessionLevel     *int   `json:"profession_level"` // history-only; latest profession_level_history row
	GlobalAliases       string `json:"global_aliases"`
	PersonalAliases     string `json:"personal_aliases"`
	Notes               string `json:"notes"`
	JoinedAt            string `json:"joined_at"` // YYYY-MM-DD (date-only); "" = unknown
	Skills              string `json:"skills"`    // comma-separated skill keys, e.g. "medical_aid"
	// LastRank linkage. PublicID is auto-captured on a successful Phase-1 match;
	// SyncedAt is the wall-clock of our last sync for this member (drives the
	// oldest-first ordering of the Phase-2 extended sync).
	LastRankPublicID *int    `json:"lastrank_public_id"`
	LastRankSyncedAt *string `json:"lastrank_synced_at"`
	// Avatar URLs hotlinked from the game CDN (populated by the extended sync).
	LastRankPhotoURL      string `json:"lastrank_photo_url"`
	LastRankPhotoFailover string `json:"lastrank_photo_failover"`
}

type FormerMember struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	LastPower   *int64 `json:"last_power"`
	TrainCount  int    `json:"train_count"`
	LastVSWeek  string `json:"last_vs_week"`
	LeaveReason string `json:"leave_reason"`
}

type Prospect struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	Server         string `json:"server"`
	SourceAlliance string `json:"source_alliance"`
	Power          *int64 `json:"power"`
	RankInAlliance string `json:"rank_in_alliance"`
	RecruiterID    *int   `json:"recruiter_id"`
	RecruiterName  string `json:"recruiter_name"`
	Status         string `json:"status"`
	Notes          string `json:"notes"`
	HeroPower      *int64 `json:"hero_power"`
	SeatColor      string `json:"seat_color"`
	InterestedInR4 bool   `json:"interested_in_r4"`
	FirstContacted string `json:"first_contacted"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	ProspectType   string `json:"prospect_type"`
	// LastRankPublicID is pasted/derived once by an officer; recruiting lookups
	// fetch the per-player endpoint on demand. No synced_at — no history kept.
	LastRankPublicID *int `json:"lastrank_public_id"`
	// Avatar URLs hotlinked from the game CDN (populated on lookup).
	LastRankPhotoURL      string `json:"lastrank_photo_url"`
	LastRankPhotoFailover string `json:"lastrank_photo_failover"`
}

type Alias struct {
	ID       int    `json:"id"`
	MemberID int    `json:"member_id"`
	UserID   *int   `json:"user_id,omitempty"`
	Alias    string `json:"alias"`
	IsGlobal bool   `json:"is_global"` // Deprecated: keep for legacy compatibility if needed
	Category string `json:"category"`  // 'global', 'personal', or 'ocr'
}

type SquadPowerHistory struct {
	ID         int    `json:"id"`
	MemberID   int    `json:"member_id"`
	Power      int64  `json:"power"`
	RecordedAt string `json:"recorded_at"`
}

type HeroPowerHistory struct {
	ID         int    `json:"id"`
	MemberID   int    `json:"member_id"`
	Power      int64  `json:"power"`
	RecordedAt string `json:"recorded_at"`
}

type KillHistory struct {
	ID         int    `json:"id"`
	MemberID   int    `json:"member_id"`
	Kills      int64  `json:"kills"`
	RecordedAt string `json:"recorded_at"`
}

type KillCount struct {
	MemberID       int    `json:"member_id"`
	MemberName     string `json:"member_name"`
	MemberRank     string `json:"member_rank"`
	CurrentKills   int64  `json:"current_kills"`
	KillsDelta7d   *int64 `json:"kills_delta_7d"`
	KillsDelta30d  *int64 `json:"kills_delta_30d"`
	LastRecordedAt string `json:"last_recorded_at"`
}

// HQLevelStat is a Tracking-page row: current HQ level + change over 7/30 days.
// HQ changes slowly, so a nil delta (no baseline that far back) is common.
type HQLevelStat struct {
	MemberID       int    `json:"member_id"`
	MemberName     string `json:"member_name"`
	MemberRank     string `json:"member_rank"`
	CurrentHQLevel int    `json:"current_hq_level"`
	Delta7d        *int   `json:"delta_7d"`
	Delta30d       *int   `json:"delta_30d"`
	LastRecordedAt string `json:"last_recorded_at"`
}

// ProfessionLevelStat is a Tracking-page row for profession (career) level.
type ProfessionLevelStat struct {
	MemberID               int    `json:"member_id"`
	MemberName             string `json:"member_name"`
	MemberRank             string `json:"member_rank"`
	Profession             string `json:"profession"`
	CurrentProfessionLevel int    `json:"current_profession_level"`
	Delta7d                *int   `json:"delta_7d"`
	Delta30d               *int   `json:"delta_30d"`
	LastRecordedAt         string `json:"last_recorded_at"`
}

type MemberStats struct {
	ID                   int     `json:"id"`
	Name                 string  `json:"name"`
	Rank                 string  `json:"rank"`
	ConductorCount       int     `json:"conductor_count"`
	LastConductorDate    *string `json:"last_conductor_date"`
	BackupCount          int     `json:"backup_count"`
	BackupUsedCount      int     `json:"backup_used_count"`
	ConductorNoShowCount int     `json:"conductor_no_show_count"`
}

type User struct {
	ID       int
	Username string
	Password string
	MemberID *int
	IsAdmin  bool
}

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type TrainSchedule struct {
	ID                int     `json:"id"`
	Date              string  `json:"date"`
	ConductorID       int     `json:"conductor_id"`
	ConductorName     string  `json:"conductor_name"`
	ConductorScore    *int    `json:"conductor_score"`
	BackupID          int     `json:"backup_id"`
	BackupName        string  `json:"backup_name"`
	BackupRank        string  `json:"backup_rank"`
	ConductorShowedUp *bool   `json:"conductor_showed_up"`
	Notes             *string `json:"notes"`
	CreatedAt         string  `json:"created_at"`
}

type Award struct {
	ID         int    `json:"id"`
	WeekDate   string `json:"week_date"`
	AwardType  string `json:"award_type"`
	Rank       int    `json:"rank"`
	MemberID   int    `json:"member_id"`
	MemberName string `json:"member_name"`
	CreatedAt  string `json:"created_at"`
}

type AwardType struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Active    bool   `json:"active"`
	SortOrder int    `json:"sort_order"`
	CreatedAt string `json:"created_at"`
}

type Recommendation struct {
	ID              int    `json:"id"`
	MemberID        int    `json:"member_id"`
	MemberName      string `json:"member_name"`
	MemberRank      string `json:"member_rank"`
	RecommendedBy   string `json:"recommended_by"`
	RecommendedByID int    `json:"recommended_by_id"`
	Notes           string `json:"notes"`
	CreatedAt       string `json:"created_at"`
	Expired         bool   `json:"expired"`
}

type DynoRecommendation struct {
	ID             int    `json:"id"`
	MemberID       int    `json:"member_id"`
	MemberName     string `json:"member_name"`
	MemberRank     string `json:"member_rank"`
	Points         int    `json:"points"`
	Notes          string `json:"notes"`
	CreatedBy      string `json:"created_by"`
	CreatedByID    int    `json:"created_by_id"`
	CreatedAt      string `json:"created_at"`
	Expired        bool   `json:"expired"`
	IsAuthorPublic bool   `json:"is_author_public"`
	MinViewRank    string `json:"min_view_rank"`
}

type PowerHistory struct {
	ID         int    `json:"id"`
	MemberID   int    `json:"member_id"`
	Power      int64  `json:"power"`
	RecordedAt string `json:"recorded_at"`
}

type LoginSession struct {
	ID        int     `json:"id"`
	UserID    int     `json:"user_id"`
	Username  string  `json:"username"`
	IPAddress *string `json:"ip_address,omitempty"`
	UserAgent *string `json:"user_agent,omitempty"`
	Country   *string `json:"country,omitempty"`
	City      *string `json:"city,omitempty"`
	ISP       *string `json:"isp,omitempty"`
	LoginTime string  `json:"login_time"`
	Success   bool    `json:"success"`
}

type IPGeolocation struct {
	Status      string  `json:"status"`
	Country     string  `json:"country"`
	CountryCode string  `json:"countryCode"`
	Region      string  `json:"region"`
	RegionName  string  `json:"regionName"`
	City        string  `json:"city"`
	Zip         string  `json:"zip"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Timezone    string  `json:"timezone"`
	ISP         string  `json:"isp"`
	Org         string  `json:"org"`
	AS          string  `json:"as"`
	Query       string  `json:"query"`
}

type AdminUserRequest struct {
	Username            string `json:"username"`
	Password            string `json:"password,omitempty"`
	MemberID            *int   `json:"member_id,omitempty"`
	IsAdmin             bool   `json:"is_admin"`
	ForcePasswordChange bool   `json:"force_password_change"`
}

type AdminUserResponse struct {
	ID                  int            `json:"id"`
	Username            string         `json:"username"`
	MemberID            *int           `json:"member_id,omitempty"`
	MemberName          *string        `json:"member_name,omitempty"`
	IsAdmin             bool           `json:"is_admin"`
	CreatedAt           string         `json:"created_at,omitempty"`
	LastLogin           *string        `json:"last_login,omitempty"`
	LoginCount          int            `json:"login_count"`
	RecentLogins        []LoginSession `json:"recent_logins,omitempty"`
	ForcePasswordChange bool           `json:"force_password_change"`
}

type Settings struct {
	ID                      int    `json:"id"`
	ScheduleMessageTemplate string `json:"schedule_message_template"`
	DailyMessageTemplate    string `json:"daily_message_template"`
	PowerTrackingEnabled    bool   `json:"power_tracking_enabled"`
	StormTimezones          string `json:"storm_timezones"`
	StormRespectDST         bool   `json:"storm_respect_dst"`
	LoginMessage            string `json:"login_message"`
	MaxHQLevel              int    `json:"max_hq_level"`
	SquadTrackingEnabled    bool   `json:"squad_tracking_enabled"`
	PwdMinLength            int    `json:"pwd_min_length"`
	PwdRequireSpecial       bool   `json:"pwd_require_special"`
	PwdRequireUpper         bool   `json:"pwd_require_upper"`
	PwdRequireLower         bool   `json:"pwd_require_lower"`
	PwdRequireNumber        bool   `json:"pwd_require_number"`
	PwdHistoryCount         int    `json:"pwd_history_count"`
	PwdValidityDays         int    `json:"pwd_validity_days"`
	CVWorkerURL             string `json:"cv_worker_url"`
	// OCRBackendMode = "cloud" (default — Google Cloud Vision via OIDC) or
	// "local" (PaddleOCR sidecar via plain HTTP). Set during install/update
	// when the operator opts into the local sidecar. See
	// image_processing.go and install.sh for the deployment plumbing.
	OCRBackendMode string `json:"ocr_backend_mode"`
	// OCRArchiveMode selects where OCR requests are archived (best-effort):
	// "none" (default), "gcp", "local", or "both". Decoupled from OCRBackendMode.
	// OCRArchiveBucket is the GCS bucket for the gcp/both destinations (set in the
	// admin UI, like CVWorkerURL). The local destination's path is OCR_ARCHIVE_DIR
	// (env). See ocr_archive.go.
	OCRArchiveMode                  string `json:"ocr_archive_mode"`
	OCRArchiveBucket                string `json:"ocr_archive_bucket"`
	TrainFreeDailyLimit             int    `json:"train_free_daily_limit"`
	TrainPurchasedDailyLimit        int    `json:"train_purchased_daily_limit"`
	AllianceMaxMembers              int    `json:"alliance_max_members"`
	JoinRequirements                string `json:"join_requirements"`
	VSMinimumPoints                 int    `json:"vs_minimum_points"`
	VsFlagDaysThreshold             int    `json:"vs_flag_days_threshold"`
	StrikeNeedsImprovementThreshold int    `json:"strike_needs_improvement_threshold"`
	StrikeAtRiskThreshold           int    `json:"strike_at_risk_threshold"`
	// Schedule defaults
	MGBaseline      int    `json:"mg_baseline"`
	ZSBaseline      int    `json:"zs_baseline"`
	MGDefaultTime   string `json:"mg_default_time"`
	ZSDefaultTime   string `json:"zs_default_time"`
	CurrentSeason   *int   `json:"current_season"`
	SeasonStartDate string `json:"season_start_date"`
	// Event generation rules
	MGAnchorDate             string `json:"mg_anchor_date"`
	ZSScheduleMode           string `json:"zs_schedule_mode"`
	ZSWeekdays               string `json:"zs_weekdays"`
	ZSAnchorDate             string `json:"zs_anchor_date"`
	ZSAnchorTime             string `json:"zs_anchor_time"`
	SeasonScoreLevelsDefault string `json:"season_score_levels_default"`
	AllianceName             string `json:"alliance_name"`
	AllianceTag              string `json:"alliance_tag"`
	// LastRankAllianceID is the 32-char hex id for the alliance on lastrank.fun,
	// used by the Phase-1 sync. Empty by default (operator pastes it in Settings).
	LastRankAllianceID string `json:"lastrank_alliance_id"`
	// OurServerID is the game server we play on. 0 = not configured.
	OurServerID int `json:"our_server_id"`
	// NAPSize is how many top alliances on our server the Non-Aggression Pact covers,
	// INCLUDING us — a size of 10 means us plus nine partners.
	NAPSize int `json:"nap_size"`
	// NAPImportLimit is how many alliances to fetch and cache from the LastRank ladder,
	// starting from the top. >= NAPSize, so the NAP tab can show who sits just below the line.
	NAPImportLimit int `json:"nap_import_limit"`
}

type StormAssignment struct {
	ID         int    `json:"id"`
	TaskForce  string `json:"task_force"`
	BuildingID string `json:"building_id"`
	MemberID   int    `json:"member_id"`
	Position   int    `json:"position"`
}

type StormTFConfig struct {
	TaskForce     string `json:"task_force"`
	TimeSlot      *int   `json:"time_slot"`
	Participating int    `json:"participating"`
}

type StormRegistration struct {
	ID          int    `json:"id"`
	MemberID    int    `json:"member_id"`
	MemberName  string `json:"member_name,omitempty"`
	MemberRank  string `json:"member_rank,omitempty"`
	MemberPower *int64 `json:"member_power,omitempty"`
	Slot1       int    `json:"slot_1"`
	Slot2       int    `json:"slot_2"`
	Slot3       int    `json:"slot_3"`
	UpdatedAt   string `json:"updated_at"`
}

type StormGroupMember struct {
	ID       int  `json:"id"`
	MemberID int  `json:"member_id"`
	IsSub    bool `json:"is_sub"`
	Position int  `json:"position"`
}

type StormGroupBuilding struct {
	ID         int                `json:"id"`
	BuildingID string             `json:"building_id"`
	SortOrder  int                `json:"sort_order"`
	Members    []StormGroupMember `json:"members"`
}

type StormGroup struct {
	ID            int                  `json:"id"`
	TaskForce     string               `json:"task_force"`
	Name          string               `json:"name"`
	Instructions  string               `json:"instructions"`
	SortOrder     int                  `json:"sort_order"`
	Buildings     []StormGroupBuilding `json:"buildings"`
	DirectMembers []StormGroupMember   `json:"direct_members"`
}

type DetectedMember struct {
	Name         string   `json:"name"`
	Rank         string   `json:"rank"`
	Level        int      `json:"level,omitempty"`
	Power        int64    `json:"power,omitempty"`
	IsNew        bool     `json:"is_new"`
	RankChanged  bool     `json:"rank_changed"`
	OldRank      string   `json:"old_rank,omitempty"`
	SimilarMatch []string `json:"similar_match,omitempty"`
	SquadType    string   `json:"squad_type,omitempty"`
	SquadPower   int64    `json:"squad_power,omitempty"`
	TroopLevel   int      `json:"troop_level,omitempty"`
	Profession   string   `json:"profession,omitempty"`
	JoinedAt     string   `json:"joined_at,omitempty"` // optional join date (YYYY-MM-DD); blank → today
}

type RenameInfo struct {
	OldName string `json:"old_name"`
	NewName string `json:"new_name"`
}

type MemberToRemove struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Rank string `json:"rank"`
}

type ConfirmRequest struct {
	Members         []DetectedMember `json:"members"`
	RemoveMemberIDs []int            `json:"remove_member_ids"`
	Renames         []RenameInfo     `json:"renames"`
	// Source tags the provenance of any power/squad history written here.
	// members.js sends 'csv'; absent/unknown → neutral 'import'.
	Source string `json:"source,omitempty"`
}

type ConfirmResult struct {
	Added     int `json:"added"`
	Updated   int `json:"updated"`
	Unchanged int `json:"unchanged"`
	Removed   int `json:"removed"`
}

type VSPoints struct {
	ID        int    `json:"id"`
	MemberID  int    `json:"member_id"`
	WeekDate  string `json:"week_date"`
	Monday    int    `json:"monday"`
	Tuesday   int    `json:"tuesday"`
	Wednesday int    `json:"wednesday"`
	Thursday  int    `json:"thursday"`
	Friday    int    `json:"friday"`
	Saturday  int    `json:"saturday"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type VSPointsWithMember struct {
	VSPoints
	MemberName string `json:"member_name"`
	MemberRank string `json:"member_rank"`
}

// ── VS Duel League ──────────────────────────────────────────────────────────
// Nullable columns use pointer fields so JSON serializes missing values as null
// (not zero/""), and so a view-only caller's strategy/notes fields can be omitted.
// VSLeagueWeekStanding (the computed rollup) lives in vs_league_scoring.go.

// VSLeagueSeason is one Alliance Duel League cycle — its own numbering (e.g. S34),
// independent of the game's named seasons and the app's Season Hub.
type VSLeagueSeason struct {
	ID           int     `json:"id"`
	SeasonNumber int     `json:"season_number"`
	LeagueTier   *string `json:"league_tier,omitempty"`
	StartDate    *string `json:"start_date,omitempty"`
	EndDate      *string `json:"end_date,omitempty"`
	FinalRank    *int    `json:"final_rank,omitempty"`
	IsActive     bool    `json:"is_active"`
	ArchivedAt   *string `json:"archived_at,omitempty"`
	Notes        *string `json:"notes,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

// VSLeagueDay is one day (1-6) of our matchup. Match points/theme are derived, not stored.
type VSLeagueDay struct {
	ID            int     `json:"id"`
	WeekID        int     `json:"week_id"`
	DayNumber     int     `json:"day_number"`
	OurScore      *int64  `json:"our_score,omitempty"`
	OpponentScore *int64  `json:"opponent_score,omitempty"`
	Outcome       string  `json:"outcome"` // win|loss|tie|pending
	MVPIsOurs     bool    `json:"mvp_is_ours"`
	MVPMemberID   *int    `json:"mvp_member_id,omitempty"`
	MVPName       *string `json:"mvp_name,omitempty"`
	Points        int     `json:"points"` // vsDayMatchPoints(day_number) — derived
}

// VSLeagueMatchup is one bracket pairing (a "Match Record" row). All-of-bracket, weekly.
type VSLeagueMatchup struct {
	ID         int     `json:"id"`
	WeekID     int     `json:"week_id"`
	MatchIndex *int    `json:"match_index,omitempty"`
	ARank      *int    `json:"a_rank,omitempty"`
	AServer    *int    `json:"a_server,omitempty"`
	ATag       *string `json:"a_tag,omitempty"`
	AName      *string `json:"a_name,omitempty"`
	APoints    *int    `json:"a_points,omitempty"`
	BRank      *int    `json:"b_rank,omitempty"`
	BServer    *int    `json:"b_server,omitempty"`
	BTag       *string `json:"b_tag,omitempty"`
	BName      *string `json:"b_name,omitempty"`
	BPoints    *int    `json:"b_points,omitempty"`
	IsOurs     bool    `json:"is_ours"`
}

// VSLeagueWeek is our matchup for a week, its days, and the computed standing.
type VSLeagueWeek struct {
	ID                     int     `json:"id"`
	SeasonID               int     `json:"season_id"`
	WeekNumber             *int    `json:"week_number,omitempty"`
	WeekDate               string  `json:"week_date"`
	LeagueTier             *string `json:"league_tier,omitempty"`
	LeagueRank             *int    `json:"league_rank,omitempty"`
	OpponentTag            *string `json:"opponent_tag,omitempty"`
	OpponentName           *string `json:"opponent_name,omitempty"`
	OpponentServer         *int    `json:"opponent_server,omitempty"`
	OpponentLastRankID     *string `json:"opponent_lastrank_id,omitempty"`
	OpponentPower          *int64  `json:"opponent_power,omitempty"`
	OpponentKills          *int64  `json:"opponent_kills,omitempty"`
	OpponentMemberCount    *int    `json:"opponent_member_count,omitempty"`
	OpponentSnapshotAt     *string `json:"opponent_snapshot_at,omitempty"`
	OpponentLastRankSeenAt *string `json:"opponent_lastrank_seen_at,omitempty"`
	// Our own alliance snapshot for the week (LastRank when configured, else summed roster).
	OurPower       *int64  `json:"our_power,omitempty"`
	OurKills       *int64  `json:"our_kills,omitempty"`
	OurMemberCount *int    `json:"our_member_count,omitempty"`
	OurServer      *int    `json:"our_server,omitempty"`
	OurSnapshotAt  *string `json:"our_snapshot_at,omitempty"`
	// Leadership context — only populated in responses for manage_vs_points users (F-R09/F-013).
	StrategyLabel  *string `json:"strategy_label,omitempty"`
	StrategyResult *string `json:"strategy_result,omitempty"`
	Notes          *string `json:"notes,omitempty"`
	// Derived + children.
	Standing    VSLeagueWeekStanding `json:"standing"`      // computed from days (or summary fields when no day rows)
	SummaryOnly bool                 `json:"summary_only"`  // true = no day rows; our_points/opponent_points are stored inputs
	Days        []VSLeagueDay        `json:"days"`
	CreatedAt   string               `json:"created_at"`
	UpdatedAt   string               `json:"updated_at"`
}

// VSLeagueParticipationDay is live per-day participation health, derived from member vs_points.
type VSLeagueParticipationDay struct {
	DayNumber     int     `json:"day_number"`
	Imported      bool    `json:"imported"`
	ActiveScorers int     `json:"active_scorers"`
	ZeroScore     int     `json:"zero_score"`
	AvgPerActive  float64 `json:"avg_per_active"`
	Top10Pct      float64 `json:"top10_pct"`
}

// VSLeagueOpponentSnapshot is a point-in-time LastRank alliance lookup (opponent scouting).
type VSLeagueOpponentSnapshot struct {
	AllianceID  string `json:"alliance_id"`
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	ServerID    int    `json:"server_id"`
	Power       int64  `json:"power"`
	Kills       int64  `json:"kills"`
	MemberCount int    `json:"member_count"`
	LastSeenAt  string `json:"last_seen_at"`
}

// VSLeagueAllianceSearchResult is one hit from the LastRank alliance search (fuzzy tag/name,
// optional strict server filter). Lean by design — member_count isn't on the search row, so a
// picked result funnels through the by-id lookup to fill it. Nullable fields are pointers.
type VSLeagueAllianceSearchResult struct {
	LastRankID string  `json:"lastrank_id"`
	Tag        *string `json:"tag"`
	Name       *string `json:"name"`
	Server     *int    `json:"server"`
	Power      *int64  `json:"power"`
	Kills      *int64  `json:"kills"`
	// PowerRank/KillsRank are the alliance's true ladder position on its server, and CapturedAt is
	// when upstream snapshotted that ladder. Populated by the alliance-list endpoint; the opponent
	// picker ignores them, but the NAP sync needs all three.
	PowerRank  *int    `json:"power_rank"`
	KillsRank  *int    `json:"kills_rank"`
	CapturedAt *string `json:"captured_at"`
}

// VSLeagueOpponentMember is one member of the opponent alliance from LastRank, for the daily-MVP
// picker. We persist only the name (as mvp_name); power/rank are display hints in the dropdown.
type VSLeagueOpponentMember struct {
	Name         string `json:"name"`
	Power        *int64 `json:"power,omitempty"`
	AllianceRank *int   `json:"alliance_rank,omitempty"`
}

// VSLeagueAnalytics is the cross-season roll-up (a season is only 4 weeks, so trends are only
// meaningful across seasons). Computed on read from weeks/days; nothing is persisted.
type VSLeagueAnalytics struct {
	Seasons     []VSLASeason   `json:"seasons"`
	Totals      VSLATotals     `json:"totals"`
	ByDay       []VSLADay      `json:"by_day"`
	DayAverages []VSLADayAvg   `json:"day_averages"`
	ByStrategy  []VSLAStrategy `json:"by_strategy"`
	Opponents   []VSLAOpponent `json:"opponents"`
}

// VSLADayAvg is the average alliance VS-points total on a theme day, over ALL weeks in vs_points
// (season or not), excluding zero/not-imported days — a far larger sample than duel-season days.
type VSLADayAvg struct {
	DayNumber    int      `json:"day_number"`
	AvgPoints    *float64 `json:"avg_points"`     // avg alliance total for the day (over weeks)
	AvgPerPlayer *float64 `json:"avg_per_player"` // avg non-zero member score for the day
	WeeksN       int      `json:"weeks_n"`
}

type VSLASeason struct {
	SeasonNumber int    `json:"season_number"`
	Tier         string `json:"tier"`
	FinalRank    *int   `json:"final_rank"`
	Wins         int    `json:"wins"`
	Losses       int    `json:"losses"`
	Ties         int    `json:"ties"`
	Weeks        int    `json:"weeks"`
}

type VSLATotals struct {
	Seasons       int      `json:"seasons"`
	Wins          int      `json:"wins"`
	Losses        int      `json:"losses"`
	Ties          int      `json:"ties"`
	WinRate       *float64 `json:"win_rate"` // over decided weeks; null if none decided
	BestFinalRank *int     `json:"best_final_rank"`
}

type VSLADay struct {
	DayNumber int      `json:"day_number"`
	Wins      int      `json:"wins"`
	Losses    int      `json:"losses"`
	Ties      int      `json:"ties"`
	MarginAvg *float64 `json:"margin_avg"` // avg (our-opp) raw score over days with both scores
	MarginN   int      `json:"margin_n"`
}

type VSLAStrategy struct {
	Label  string `json:"label"`
	Worked int    `json:"worked"`
	Failed int    `json:"failed"`
	Mixed  int    `json:"mixed"`
	Total  int    `json:"total"`
}

type VSLAOpponent struct {
	Tag      string `json:"tag"`
	Name     string `json:"name"`
	Wins     int    `json:"wins"`
	Losses   int    `json:"losses"`
	Ties     int    `json:"ties"`
	Meetings int    `json:"meetings"`
}

// ExternalAlliance is a cached outside alliance seen via LastRank (populated on every lookup)
// so it can be re-entered by tag without another lookup. Not VS-specific — allies/recruiting
// can use it too. Tag is not unique (changeable).
type ExternalAlliance struct {
	ID          int     `json:"id"`
	LastRankID  *string `json:"lastrank_id,omitempty"`
	Tag         *string `json:"tag,omitempty"`
	Name        *string `json:"name,omitempty"`
	Server      *int    `json:"server,omitempty"`
	Power       *int64  `json:"power,omitempty"`
	Kills       *int64  `json:"kills,omitempty"`
	MemberCount *int    `json:"member_count,omitempty"`
	LastSeenAt  *string `json:"lastrank_seen_at,omitempty"`
	UpdatedAt   string  `json:"updated_at"`
	// Relationship flags (computed for the External Alliances registry page).
	AllyStatus    string `json:"ally_status"` // "active" | "former" | "never"
	ProspectCount int    `json:"prospect_count"`
	IsOpponent    bool `json:"is_opponent"` // we've faced them in VS League (decided or pending)
	VSWins        int  `json:"vs_wins"`
	VSLosses      int  `json:"vs_losses"`
	VSTies        int  `json:"vs_ties"`
}

type RankPermissions struct {
	Rank                 string `json:"rank"`
	ViewTrain            bool   `json:"view_train"`
	ManageTrain          bool   `json:"manage_train"`
	ViewAwards           bool   `json:"view_awards"`
	ManageAwards         bool   `json:"manage_awards"`
	ViewRecs             bool   `json:"view_recs"`
	ManageRecs           bool   `json:"manage_recs"`
	ViewDyno             bool   `json:"view_dyno"`
	ManageDyno           bool   `json:"manage_dyno"`
	ViewRankings         bool   `json:"view_rankings"`
	ViewStorm            bool   `json:"view_storm"`
	ManageStorm          bool   `json:"manage_storm"`
	ViewVSPoints         bool   `json:"view_vs_points"`
	ManageVSPoints       bool   `json:"manage_vs_points"`
	ViewUpload           bool   `json:"view_upload"`
	ManageMembers        bool   `json:"manage_members"`
	ManageSettings       bool   `json:"manage_settings"`
	ViewFiles            bool   `json:"view_files"`
	UploadFiles          bool   `json:"upload_files"`
	ManageFiles          bool   `json:"manage_files"`
	ViewAnonymousAuthors bool   `json:"view_anonymous_authors"`
	ViewSchedule         bool   `json:"view_schedule"`
	ManageSchedule       bool   `json:"manage_schedule"`
	ViewOfficerCommand   bool   `json:"view_officer_command"`
	ManageOfficerCommand bool   `json:"manage_officer_command"`
	ViewRecruiting       bool   `json:"view_recruiting"`
	ManageRecruiting     bool   `json:"manage_recruiting"`
	ViewAllies           bool   `json:"view_allies"`
	ManageAllies         bool   `json:"manage_allies"`
	ViewActivity         bool   `json:"view_activity"`
	ViewAccountability   bool   `json:"view_accountability"`
	ManageAccountability bool   `json:"manage_accountability"`
	ViewSeasonHub        bool   `json:"view_season_hub"`
	ManageSeasonHub      bool   `json:"manage_season_hub"`
	ManageSeasonRewards  bool   `json:"manage_season_rewards"`
	ViewComms            bool   `json:"view_comms"`
	ManageComms          bool   `json:"manage_comms"`
	ViewPolls            bool   `json:"view_polls"`
	ManagePolls          bool   `json:"manage_polls"`
}

// ValidSkillKeys is the canonical list of skill keys. Adding a new skill = add here only.
var ValidSkillKeys = []string{"medical_aid"}

// SkillLabels maps skill keys to human-readable labels for templates and API responses.
var SkillLabels = map[string]string{
	"medical_aid": "Medical Aid",
}

// PermissionRow is a single permission entry within a feature group (key + short label).
type PermissionRow struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// PermissionGroup groups related permissions under a single feature name for the RBAC matrix UI.
// Adding a new permission = add a PermissionRow to the appropriate group here + 1 field in RankPermissions. No migration needed.
type PermissionGroup struct {
	Feature string          `json:"feature"`
	Rows    []PermissionRow `json:"rows"`
}

// PermissionGroups is the canonical list shown in the Settings matrix, in display order.
var PermissionGroups = []PermissionGroup{
	{Feature: "Roster", Rows: []PermissionRow{
		{Key: "manage_members", Label: "Manage"},
	}},
	{Feature: "Train Tracker", Rows: []PermissionRow{
		{Key: "view_train", Label: "View"},
		{Key: "manage_train", Label: "Manage"},
	}},
	{Feature: "Shoutouts", Rows: []PermissionRow{
		{Key: "view_dyno", Label: "View"},
		{Key: "manage_dyno", Label: "Manage"},
		{Key: "view_anonymous_authors", Label: "Anon Authors"},
	}},
	{Feature: "Analytics Dashboard", Rows: []PermissionRow{
		{Key: "view_rankings", Label: "View"},
	}},
	{Feature: "Desert Storm", Rows: []PermissionRow{
		{Key: "view_storm", Label: "View"},
		{Key: "manage_storm", Label: "Manage"},
	}},
	{Feature: "VS & Duel League", Rows: []PermissionRow{
		{Key: "view_vs_points", Label: "View"},
		{Key: "manage_vs_points", Label: "Manage"},
	}},
	{Feature: "OCR Upload", Rows: []PermissionRow{
		{Key: "view_upload", Label: "Access"},
	}},
	{Feature: "Alliance Files", Rows: []PermissionRow{
		{Key: "view_files", Label: "View"},
		{Key: "upload_files", Label: "Upload"},
		{Key: "manage_files", Label: "Manage"},
	}},
	{Feature: "Schedule", Rows: []PermissionRow{
		{Key: "view_schedule", Label: "View"},
		{Key: "manage_schedule", Label: "Manage"},
	}},
	{Feature: "Officer Command", Rows: []PermissionRow{
		{Key: "view_officer_command", Label: "View"},
		{Key: "manage_officer_command", Label: "Manage"},
	}},
	{Feature: "Recruiting", Rows: []PermissionRow{
		{Key: "view_recruiting", Label: "View"},
		{Key: "manage_recruiting", Label: "Manage"},
	}},
	{Feature: "Allies", Rows: []PermissionRow{
		{Key: "view_allies", Label: "View"},
		{Key: "manage_allies", Label: "Manage"},
	}},
	{Feature: "Activity Log", Rows: []PermissionRow{
		{Key: "view_activity", Label: "View"},
	}},
	{Feature: "Accountability", Rows: []PermissionRow{
		{Key: "view_accountability", Label: "View"},
		{Key: "manage_accountability", Label: "Manage"},
	}},
	{Feature: "Season Hub", Rows: []PermissionRow{
		{Key: "view_season_hub", Label: "View"},
		{Key: "manage_season_hub", Label: "Manage"},
		{Key: "manage_season_rewards", Label: "Rewards"},
	}},
	{Feature: "Comms", Rows: []PermissionRow{
		{Key: "view_comms", Label: "View"},
		{Key: "manage_comms", Label: "Manage"},
	}},
	{Feature: "Polls", Rows: []PermissionRow{
		{Key: "view_polls", Label: "View"},
		{Key: "manage_polls", Label: "Manage"},
	}},
	{Feature: "Settings", Rows: []PermissionRow{
		{Key: "manage_settings", Label: "Access"},
	}},
}

// allPermissionsTrue returns a RankPermissions with every bool field set to true.
// Used for admin users — automatically covers any new bool fields added to the struct.
func allPermissionsTrue() RankPermissions {
	var p RankPermissions
	v := reflect.ValueOf(&p).Elem()
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Kind() == reflect.Bool {
			v.Field(i).SetBool(true)
		}
	}
	return p
}

type CommsTemplate struct {
	ID           int     `json:"id"`
	Type         string  `json:"type"`
	Title        string  `json:"title"`
	Category     string  `json:"category"`
	Content      string  `json:"content"`
	SeasonID     *int    `json:"season_id"`
	Slug         *string `json:"slug"`
	RequiredVars string  `json:"required_vars"`
	CreatedBy    string  `json:"created_by"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type CommsResource struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
}

type PollTemplate struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Question    string `json:"question"`
	Options     string `json:"options"`   // JSON array ["Yes","No","Abstain"]
	PollType    string `json:"poll_type"` // "named" | "anonymous"
	MultiSelect bool   `json:"multi_select"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
}

type PollInstance struct {
	ID             int     `json:"id"`
	TemplateID     *int    `json:"template_id"` // nullable — template may be deleted
	Label          string  `json:"label"`
	Question       string  `json:"question"`       // snapshotted at launch
	Options        string  `json:"options"`        // snapshotted at launch (JSON)
	PollType       string  `json:"poll_type"`      // snapshotted at launch
	MultiSelect    bool    `json:"multi_select"`   // snapshotted at launch
	RankFilter     *string `json:"rank_filter"`    // NULL = all active; JSON ["R4","R5"] = subset
	TotalEligible  int     `json:"total_eligible"` // snapshotted at launch
	CreatedBy      string  `json:"created_by"`
	CreatedAt      string  `json:"created_at"`
	RespondedCount int     `json:"responded_count"` // computed on list queries
}

type PollMemberStatus struct {
	MemberID    int      `json:"member_id"`
	MemberName  string   `json:"member_name"`
	Rank        string   `json:"rank"`
	Responded   bool     `json:"responded"`
	Options     []string `json:"options"`      // selected option(s), empty if pending
	RespondedAt string   `json:"responded_at"` // earliest recorded_at across this member's options; empty if pending
}

type PollAnonCount struct {
	OptionKey     string `json:"option_key"`
	ResponseCount int    `json:"response_count"`
}

// PollOptionMember is a single member within a by-option bucket, in sign-up order.
type PollOptionMember struct {
	MemberID   int    `json:"member_id"`
	MemberName string `json:"member_name"`
	Rank       string `json:"rank"`
	RecordedAt string `json:"recorded_at"`
}

// PollOptionBucket groups the members who selected one option (named polls),
// ordered by recorded_at. Powers the "By option" detail view.
type PollOptionBucket struct {
	Option  string             `json:"option"`
	Members []PollOptionMember `json:"members"`
}

type ActivityLog struct {
	ID          int    `json:"id"`
	UserID      *int   `json:"user_id"`
	Username    string `json:"username"`
	Action      string `json:"action"`
	EntityType  string `json:"entity_type"`
	EntityName  string `json:"entity_name"`
	Details     string `json:"details"`
	EntityCount int    `json:"entity_count"`
	IsSensitive bool   `json:"is_sensitive"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// --- Officer Command Models ---

type OCAssignee struct {
	MemberID int    `json:"member_id"`
	Name     string `json:"name"`
	Rank     string `json:"rank"`
}

type OCResponsibility struct {
	ID           int          `json:"id"`
	CategoryID   int          `json:"category_id"`
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	Frequency    string       `json:"frequency"`
	DisplayOrder int          `json:"display_order"`
	Assignees    []OCAssignee `json:"assignees"`
}

type OCCategory struct {
	ID               int                `json:"id"`
	Name             string             `json:"name"`
	DisplayOrder     int                `json:"display_order"`
	Responsibilities []OCResponsibility `json:"responsibilities"`
}

// --- Schedule Models ---

type ScheduleEventType struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
	Icon      string `json:"icon"`
	IsSystem  bool   `json:"is_system"`
	Active    bool   `json:"active"`
	SortOrder int    `json:"sort_order"`
	CreatedAt string `json:"created_at"`
}

type ScheduleEvent struct {
	ID          int    `json:"id"`
	EventDate   string `json:"event_date"`
	EventTypeID int    `json:"event_type_id"`
	TypeName    string `json:"type_name"`
	TypeShort   string `json:"type_short"`
	TypeIcon    string `json:"type_icon"`
	IsSystem    bool   `json:"is_system"`
	EventTime   string `json:"event_time"`
	AllDay      bool   `json:"all_day"`
	Level       *int   `json:"level"`
	Notes       string `json:"notes"`
	CreatedBy   int    `json:"created_by"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type ServerEvent struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	ShortName      string `json:"short_name"`
	Icon           string `json:"icon"`
	DurationDays   int    `json:"duration_days"`
	RepeatType     string `json:"repeat_type"`
	RepeatInterval *int   `json:"repeat_interval"`
	RepeatWeekday  *int   `json:"repeat_weekday"`
	AnchorDate     string `json:"anchor_date"`
	Active         bool   `json:"active"`
	SortOrder      int    `json:"sort_order"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type StormSlotTime struct {
	Slot   int    `json:"slot"`
	Label  string `json:"label"`
	TimeST string `json:"time_st"`
}

type TrainLog struct {
	ID            int     `json:"id"`
	Date          string  `json:"date"`
	TrainType     string  `json:"train_type"`
	ConductorID   int     `json:"conductor_id"`
	ConductorName string  `json:"conductor_name"`
	VIPID         *int    `json:"vip_id"`
	VIPName       *string `json:"vip_name"`
	VIPType       *string `json:"vip_type"` // "SPECIAL_GUEST" | "GUARDIAN_DEFENDER" | nil
	Notes         string  `json:"notes"`
	CreatedBy     int     `json:"created_by"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

type EligibilityRule struct {
	ID              int             `json:"id"`
	Name            string          `json:"name"`
	SelectionMethod json.RawMessage `json:"selection_method"`
	Conditions      json.RawMessage `json:"conditions"`
	CreatedBy       int             `json:"created_by"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
}

// EligibleMember is returned by the /run endpoint with per-member stats.
type EligibleMember struct {
	MemberID               int     `json:"member_id"`
	Name                   string  `json:"name"`
	Rank                   string  `json:"rank"`
	VSTotalWeek            int     `json:"vs_total_week"`
	VSYesterday            int     `json:"vs_yesterday"`
	VSTotalPrevWeek        int     `json:"vs_total_prev_week"`
	VSDayMonday            int     `json:"vs_day_monday"`
	VSDayTuesday           int     `json:"vs_day_tuesday"`
	VSDayWednesday         int     `json:"vs_day_wednesday"`
	VSDayThursday          int     `json:"vs_day_thursday"`
	VSDayFriday            int     `json:"vs_day_friday"`
	VSDaySaturday          int     `json:"vs_day_saturday"`
	DaysSinceFreeConducted float64 `json:"days_since_free_conducted"` // 9999 = never
	DaysSinceAnyConducted  float64 `json:"days_since_any_conducted"`  // 9999 = never
}

type AllianceFile struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	FileName    string `json:"file_name"`
	FileType    string `json:"file_type"`
	MinRank     string `json:"min_rank"`
	MinEditRank string `json:"min_edit_rank"`
	OwnerUserID int    `json:"owner_user_id"`
	OwnerName   string `json:"owner_name"`
	CreatedAt   string `json:"created_at"`
	IsOwner     bool   `json:"is_owner"`
}

type WOPIClaims struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	FileID   int    `json:"file_id"`
	CanEdit  bool   `json:"can_edit"`
	jwt.RegisteredClaims
}

type ImageRegion struct {
	Name   string
	Top    int
	Bottom int
	Left   int
	Right  int
}

type ScreenshotAttributes struct {
	Width          int
	Height         int
	TitleBarRegion *ImageRegion
	TabsRegion     *ImageRegion
	HeaderRegion   *ImageRegion
	DataRegion     *ImageRegion
	ButtonRegion   *ImageRegion
	RowHeight      int
	EstimatedRows  int
}

type RankingContext struct {
	Settings          Settings
	RecommendationMap map[int]int
	AwardScoreMap     map[int]int
	ConductorStats    map[int]ConductorStat
	AvgConductorCount float64
	ReferenceDate     time.Time
}

type ConductorStat struct {
	Count          int
	LastDate       *string
	LastBackupUsed *string
}

// --- Frontend Template Context ---

type PageData struct {
	Title             string
	ActivePage        string
	IsAuthenticated   bool
	Username          string
	IsAdmin           bool
	Rank              string
	Permissions       RankPermissions
	CSRFToken         template.HTML
	HasGCPCredentials bool
	OCRPipelineReady  bool
	VSMinimumPoints   int
	// VS daily-minimum flagging (server-authoritative evaluated week — see vsEvalContext).
	VsFlagDaysThreshold int
	VSEvalWeek          string
	VSCompletedDays     int
	VSImportedCompleted int
	VSFallbackActive    bool
	MemberID            int
	// OCRBackendMode is "cloud" (default — Cloud Vision auto-detect) or
	// "local" (PaddleOCR sidecar; user picks scene per batch). Templates
	// use this to swap help text and conditionally disable the
	// auto-detect option in the upload page.
	OCRBackendMode string
	AllianceName   string
	AllianceTag    string
	// OurServerID is the game server we play on (0 = not configured). Available on every page.
	OurServerID int
	SkillLabels    map[string]string
	// LastRank avatar for the logged-in user's linked member, shown in the
	// sidebar user tile (falls back to initials when empty or blocked).
	UserPhotoURL      string
	UserPhotoFailover string
}

// DashboardCard represents a single card in the dashboard with its visibility state.
type DashboardCard struct {
	ID      string `json:"id"`
	Visible bool   `json:"visible"`
}

// DashboardPrefsResponse is returned by GET /api/dashboard/prefs.
type DashboardPrefsResponse struct {
	Prefs     []DashboardCard `json:"prefs"`
	Available []DashboardCard `json:"available"`
}

type WeekAwards struct {
	WeekDate string             `json:"week_date"`
	Awards   map[string][]Award `json:"awards"`
}

// CredentialUpdateRequest is used by admins to submit new API keys.
type CredentialUpdateRequest struct {
	ServiceName string `json:"service_name"`
	Secret      string `json:"secret"`
}

// --- Import Payloads ---

type VSImportPreviewResponse struct {
	Matched    []VSImportRow `json:"matched"`
	Ambiguous  []VSImportRow `json:"ambiguous"` // NEW: For fuzzy matches needing review
	Unresolved []VSImportRow `json:"unresolved"`
	AllMembers []Member      `json:"all_members"`
}

type VSImportRow struct {
	OriginalName  string         `json:"original_name"`
	MatchedMember *Member        `json:"matched_member,omitempty"`
	MatchType     string         `json:"match_type"` // 'exact', 'personal_alias', 'global_alias', 'none'
	UpdatedFields map[string]int `json:"updated_fields"`
	Total         *int           `json:"total,omitempty"`
	CalculatedSat bool           `json:"calculated_sat"`
	Error         string         `json:"error,omitempty"`
	Candidates    []OCRPlayer    `json:"candidates,omitempty"` // ambiguous name/score splits; present on unresolved rows only
}

type NewAliasMapping struct {
	FailedAlias string `json:"failed_alias"`
	MemberID    int    `json:"member_id"`
	Category    string `json:"category"` // 'global', 'personal', or 'ocr'
}

type VSImportCommitRequest struct {
	WeekDate    string            `json:"week_date"`
	Records     []VSImportRow     `json:"records"`
	SaveAliases []NewAliasMapping `json:"save_aliases"`
	// OCRSummary is the compact OCR diagnostics line from the smart-screenshot
	// preview, echoed back by the client so the activity entry can be enriched.
	// Empty for CSV imports and legacy OCR responses. Informational log text only.
	OCRSummary string `json:"ocr_summary,omitempty"`
	// Source distinguishes the origin of this commit for datapoint provenance:
	// "ocr" (upload.js) or "csv" (vs.js). Threaded into the history-table inserts.
	// Falls back to "import" when absent/unrecognized (old clients).
	Source string `json:"source,omitempty"`
}

// --- Mobile API Payloads ---

type MobileTokenClaims struct {
	UserID        int    `json:"user_id"`
	Username      string `json:"username"`
	MemberID      *int   `json:"member_id,omitempty"`
	IsAdmin       bool   `json:"is_admin"`
	ManageVS      bool   `json:"manage_vs"`
	ManageMembers bool   `json:"manage_members"`
	jwt.RegisteredClaims
}

type MobileScanEntry struct {
	Name     string `json:"name"`
	Score    int64  `json:"score"`
	Category string `json:"category"`
}

// MobileAlias is one row from member_aliases, scoped to what the current
// mobile user is allowed to see (their own personals + all global + all OCR).
type MobileAlias struct {
	Alias    string `json:"alias"`
	Category string `json:"category"`
}

// MobileMember is the roster row served to the mobile scanner so that its
// RosterAliasResolver can run the same Exact → Personal → Global → OCR
// lookup that the backend does in resolveMemberAlias.
type MobileMember struct {
	ID      int           `json:"id"`
	Name    string        `json:"name"`
	Rank    string        `json:"rank"`
	Aliases []MobileAlias `json:"aliases"`
}

type MobilePreviewRequest struct {
	WeekDate string            `json:"week_date"`
	Entries  []MobileScanEntry `json:"entries"`
}

type MobilePreviewMatch struct {
	OriginalName  string  `json:"original_name"`
	MatchedMember *Member `json:"matched_member,omitempty"`
	MatchType     string  `json:"match_type"`
	Category      string  `json:"category"`
	Score         int64   `json:"score"`
}

type MobilePreviewResponse struct {
	WeekDate        string               `json:"week_date"`
	Matched         []MobilePreviewMatch `json:"matched"`
	Unresolved      []MobilePreviewMatch `json:"unresolved"`
	AllMembers      []MobileMember       `json:"all_members"`
	TotalSubmitted  int                  `json:"total_submitted"`
	TotalMatched    int                  `json:"total_matched"`
	TotalUnresolved int                  `json:"total_unresolved"`
}

type MobileCommitRecord struct {
	MemberID     int    `json:"member_id"`
	OriginalName string `json:"original_name"`
	Category     string `json:"category"`
	Score        int64  `json:"score"`
}

type MobileCommitRequest struct {
	WeekDate    string               `json:"week_date"`
	Records     []MobileCommitRecord `json:"records"`
	SaveAliases []NewAliasMapping    `json:"save_aliases"`
}

type MobileCommitResponse struct {
	Message           string   `json:"message"`
	VSRecordsSaved    int      `json:"vs_records_saved"`
	PowerRecordsSaved int      `json:"power_records_saved"`
	KillRecordsSaved  int      `json:"kill_records_saved"`
	AliasesSaved      int      `json:"aliases_saved"`
	Errors            []string `json:"errors"`
}

// --- Allies Models ---

type AllyAgreementType struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Active    bool   `json:"active"`
	SortOrder int    `json:"sort_order"`
	CreatedAt string `json:"created_at"`
}

type Ally struct {
	ID               int    `json:"id"`
	Server           string `json:"server"`
	Tag              string `json:"tag"`
	Name             string `json:"name"`
	Active           bool   `json:"active"`
	Notes            string `json:"notes"`
	Contact          string `json:"contact"`
	CreatedAt        string `json:"created_at"`
	AgreementTypeIDs []int  `json:"agreement_type_ids"`
}

// --- LastRank Payloads ---
//
// These are the app-facing shapes exchanged with the frontend. The raw
// lastrank.fun wire structs live in lastrank_client.go and never leave it.

// LastRankAllianceMeta is the alliance-level summary shown above the Phase-1 review.
type LastRankAllianceMeta struct {
	AllianceID string `json:"alliance_id"`
	Abbr       string `json:"abbr"`
	Name       string `json:"name"`
	ServerID   int    `json:"server_id"`
	CurMember  int    `json:"cur_member"`
	MaxMember  int    `json:"max_member"`
	LastSeenAt string `json:"last_seen_at"` // ISO-8601 capture date for this pull
}

// LastRankStatDiff is a proposed numeric history update (power / hero power).
// Apply is false when the value is unchanged or LastRank's data is stale.
type LastRankStatDiff struct {
	Current    *int64 `json:"current"` // our latest value, nil if none recorded
	New        int64  `json:"new"`     // LastRank's value
	Apply      bool   `json:"apply"`
	SkipReason string `json:"skip_reason,omitempty"` // "unchanged" | "stale"
}

// LastRankHQDiff is a proposed members.level update (HQ never regresses).
type LastRankHQDiff struct {
	Current    int    `json:"current"`
	New        int    `json:"new"`
	Apply      bool   `json:"apply"`
	SkipReason string `json:"skip_reason,omitempty"` // "not_higher"
}

// LastRankRankDiff is a rank mismatch surfaced for review. Never auto-applied.
type LastRankRankDiff struct {
	Current string `json:"current"` // e.g. "R4"
	New     string `json:"new"`     // e.g. "R5"
}

// LastRankNameChange flags a matched member whose current in-game name on LastRank
// differs from our roster's primary name (i.e. they were matched via an alias).
// Surfaced for review — never auto-applied.
type LastRankNameChange struct {
	Current string `json:"current"` // our roster primary name
	New     string `json:"new"`     // LastRank's current in-game name
}

// LastRankMemberDiff is one matched roster member with its proposed Phase-1 changes.
type LastRankMemberDiff struct {
	LastRankName     string              `json:"lastrank_name"`
	LastRankPublicID int                 `json:"lastrank_public_id"`
	MatchedMember    *Member             `json:"matched_member"`
	MatchType        string              `json:"match_type"`
	Power            *LastRankStatDiff   `json:"power,omitempty"`
	HeroPower        *LastRankStatDiff   `json:"hero_power,omitempty"`
	HQLevel          *LastRankHQDiff     `json:"hq_level,omitempty"`
	RankDiff         *LastRankRankDiff   `json:"rank_diff,omitempty"`
	NameChange       *LastRankNameChange `json:"name_change,omitempty"`
}

// LastRankUnmatched is a LastRank member that did not resolve to any roster member.
type LastRankUnmatched struct {
	LastRankName     string `json:"lastrank_name"`
	LastRankPublicID int    `json:"lastrank_public_id"`
	Power            *int64 `json:"power"`
	HeroPower        *int64 `json:"hero_power"`
	Rank             string `json:"rank"` // mapped R-string for display
	BaseLevel        *int   `json:"base_level"`
}

// LastRankArchiveCandidate is one of our active members who appears to have left
// the alliance per LastRank (absent from the roster, or present but unranked).
type LastRankArchiveCandidate struct {
	MemberID int    `json:"member_id"`
	Name     string `json:"name"`
	Rank     string `json:"rank"`
	Reason   string `json:"reason"`
}

// LastRankSyncPreviewResponse is the Phase-1 alliance-fetch result.
type LastRankSyncPreviewResponse struct {
	Alliance          LastRankAllianceMeta       `json:"alliance"`
	Matched           []LastRankMemberDiff       `json:"matched"`
	Unmatched         []LastRankUnmatched        `json:"unmatched"`
	ArchiveCandidates []LastRankArchiveCandidate `json:"archive_candidates"`
	AllMembers        []Member                   `json:"all_members"` // for the unmatched assign dropdown
}

// LastRankCommitMember is one confirmed set of Phase-1 changes to apply.
// A nil pointer means "don't touch that field". NewRank present = officer-approved.
type LastRankCommitMember struct {
	MemberID         int    `json:"member_id"`
	LastRankPublicID int    `json:"lastrank_public_id"`
	Power            *int64 `json:"power,omitempty"`
	HeroPower        *int64 `json:"hero_power,omitempty"`
	HQLevel          *int   `json:"hq_level,omitempty"`
	NewRank          string `json:"new_rank,omitempty"`
	// Name-change disposition: "rename" (adopt NameNew as primary, old → global
	// alias) or "alias" (add NameNew as a global alias, keep primary). NameNew is
	// the LastRank current name.
	NameAction string `json:"name_action,omitempty"`
	NameNew    string `json:"name_new,omitempty"`
}

// LastRankUnmatchedAction is the officer's chosen disposition for an unmatched name.
// When ApplyStats is set (alias/rename/add), the LastRank entry's power/hero/HQ are
// applied to the paired/new member with the same stale + capture-date gating as a
// matched member.
type LastRankUnmatchedAction struct {
	LastRankName     string `json:"lastrank_name"`
	LastRankPublicID int    `json:"lastrank_public_id"`
	Action           string `json:"action"`              // "alias" | "rename" | "add" | "ignore"
	MemberID         int    `json:"member_id,omitempty"` // target for "alias"/"rename"
	NewRank          string `json:"new_rank,omitempty"`  // rank for "add"
	JoinedAt         string `json:"joined_at,omitempty"` // join date for "add" (YYYY-MM-DD); blank → today
	ApplyStats       bool   `json:"apply_stats,omitempty"`
	Power            *int64 `json:"power,omitempty"`
	HeroPower        *int64 `json:"hero_power,omitempty"`
	BaseLevel        *int   `json:"base_level,omitempty"`
}

// LastRankCommitRequest is the Phase-1 confirm payload.
type LastRankCommitRequest struct {
	CaptureDate string                    `json:"capture_date"` // echoed alliance last_seen_at → recorded_at
	Members     []LastRankCommitMember    `json:"members"`
	Unmatched   []LastRankUnmatchedAction `json:"unmatched"`
	Archive     []int                     `json:"archive"` // member IDs to archive (rank → EX)
}

// LastRankPlayerSyncRequest triggers one Phase-2 per-player fetch+write.
type LastRankPlayerSyncRequest struct {
	MemberID int `json:"member_id"`
}

// LastRankPlayerSyncResponse is one Phase-2 per-player result for the row UI.
type LastRankPlayerSyncResponse struct {
	MemberID               int    `json:"member_id"`
	LastRankName           string `json:"lastrank_name"`
	Kills                  *int64 `json:"kills"`
	KillsApplied           bool   `json:"kills_applied"`
	PowerApplied           bool   `json:"power_applied"`
	HeroApplied            bool   `json:"hero_applied"`
	HQApplied              bool   `json:"hq_applied"`
	ProfessionLevelApplied bool   `json:"profession_level_applied"`
	ProfessionChanged      bool   `json:"profession_changed"`
	PhotoUpdated           bool   `json:"photo_updated"`
	SkipReason             string `json:"skip_reason,omitempty"` // "unchanged" | "stale" | "no_id"
	CaptureDate            string `json:"capture_date"`
	SyncedAt               string `json:"synced_at"`
}

// LastRankFinishRequest logs one summary activity row at the end of a browser-
// driven batch (Phase-2 extended sync, or a recruiting bulk refresh).
type LastRankFinishRequest struct {
	Kind              string `json:"kind"` // "extended" | "prospects"
	MembersSynced     int    `json:"members_synced"`
	KillRecords       int    `json:"kill_records"`
	PowerRecords      int    `json:"power_records"`
	HeroRecords       int    `json:"hero_records"`
	HQRecords         int    `json:"hq_records"`
	ProfessionRecords int    `json:"profession_records"` // profession-level history rows written
	ProfessionChanges int    `json:"profession_changes"` // profession (career type) relabels
	PhotoRecords      int    `json:"photo_records"`
	ProspectsSynced   int    `json:"prospects_synced"`
}

// LastRankProspectLookupRequest fetches a prospect's player data on demand.
// LastRankInput (optional) is a pasted URL or bare id to store before fetching.
// Bulk suppresses the per-call activity log (the bulk loop logs once via finish).
type LastRankProspectLookupRequest struct {
	ProspectID    int    `json:"prospect_id"`
	LastRankInput string `json:"lastrank_input,omitempty"`
	Bulk          bool   `json:"bulk,omitempty"`
}

// LastRankProspectLookupResponse is the recruiting on-demand lookup result.
type LastRankProspectLookupResponse struct {
	ProspectID       int    `json:"prospect_id"`
	LastRankPublicID int    `json:"lastrank_public_id"`
	LastRankName     string `json:"lastrank_name"`
	Power            *int64 `json:"power"`
	HeroPower        *int64 `json:"hero_power"`
	AllianceAbbr     string `json:"alliance_abbr"`
	AllianceName     string `json:"alliance_name"`
	ServerID         int    `json:"server_id"`
	BaseLevel        *int   `json:"base_level"`
	Rank             string `json:"rank"`
	CaptureDate      string `json:"capture_date"`
	Updated          bool   `json:"updated"` // persisted power/hero to the prospect record
}
