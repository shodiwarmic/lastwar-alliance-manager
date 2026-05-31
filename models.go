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
	GlobalAliases       string `json:"global_aliases"`
	PersonalAliases     string `json:"personal_aliases"`
	Notes               string `json:"notes"`
	Skills              string `json:"skills"` // comma-separated skill keys, e.g. "medical_aid"
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
	ID                           int    `json:"id"`
	ScheduleMessageTemplate      string `json:"schedule_message_template"`
	DailyMessageTemplate         string `json:"daily_message_template"`
	PowerTrackingEnabled         bool   `json:"power_tracking_enabled"`
	StormTimezones               string `json:"storm_timezones"`
	StormRespectDST              bool   `json:"storm_respect_dst"`
	LoginMessage                 string `json:"login_message"`
	MaxHQLevel                   int    `json:"max_hq_level"`
	SquadTrackingEnabled         bool   `json:"squad_tracking_enabled"`
	PwdMinLength                 int    `json:"pwd_min_length"`
	PwdRequireSpecial            bool   `json:"pwd_require_special"`
	PwdRequireUpper              bool   `json:"pwd_require_upper"`
	PwdRequireLower              bool   `json:"pwd_require_lower"`
	PwdRequireNumber             bool   `json:"pwd_require_number"`
	PwdHistoryCount              int    `json:"pwd_history_count"`
	PwdValidityDays              int    `json:"pwd_validity_days"`
	CVWorkerURL                  string `json:"cv_worker_url"`
	// OCRBackendMode = "cloud" (default — Google Cloud Vision via OIDC) or
	// "local" (PaddleOCR sidecar via plain HTTP). Set during install/update
	// when the operator opts into the local sidecar. See
	// image_processing.go and install.sh for the deployment plumbing.
	OCRBackendMode               string `json:"ocr_backend_mode"`
	TrainFreeDailyLimit          int    `json:"train_free_daily_limit"`
	TrainPurchasedDailyLimit     int    `json:"train_purchased_daily_limit"`
	AllianceMaxMembers           int    `json:"alliance_max_members"`
	JoinRequirements             string `json:"join_requirements"`
	VSMinimumPoints              int    `json:"vs_minimum_points"`
	StrikeNeedsImprovementThreshold int `json:"strike_needs_improvement_threshold"`
	StrikeAtRiskThreshold           int `json:"strike_at_risk_threshold"`
	// Schedule defaults
	MGBaseline     int    `json:"mg_baseline"`
	ZSBaseline     int    `json:"zs_baseline"`
	MGDefaultTime  string `json:"mg_default_time"`
	ZSDefaultTime  string `json:"zs_default_time"`
	CurrentSeason  *int   `json:"current_season"`
	SeasonStartDate string `json:"season_start_date"`
	// Event generation rules
	MGAnchorDate   string `json:"mg_anchor_date"`
	ZSScheduleMode string `json:"zs_schedule_mode"`
	ZSWeekdays     string `json:"zs_weekdays"`
	ZSAnchorDate   string `json:"zs_anchor_date"`
	ZSAnchorTime   string `json:"zs_anchor_time"`
	SeasonScoreLevelsDefault string `json:"season_score_levels_default"`
	AllianceName             string `json:"alliance_name"`
	AllianceTag              string `json:"alliance_tag"`
}

type StormAssignment struct {
	ID         int    `json:"id"`
	TaskForce  string `json:"task_force"`
	BuildingID string `json:"building_id"`
	MemberID   int    `json:"member_id"`
	Position   int    `json:"position"`
}

type StormTFConfig struct {
	TaskForce    string `json:"task_force"`
	TimeSlot     *int   `json:"time_slot"`
	Participating int   `json:"participating"`
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
	"medical_aid": "🩹 Medical Aid",
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
	{Feature: "VS Points", Rows: []PermissionRow{
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
	Options     string `json:"options"`     // JSON array ["Yes","No","Abstain"]
	PollType    string `json:"poll_type"`   // "named" | "anonymous"
	MultiSelect bool   `json:"multi_select"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
}

type PollInstance struct {
	ID             int     `json:"id"`
	TemplateID     *int    `json:"template_id"` // nullable — template may be deleted
	Label          string  `json:"label"`
	Question       string  `json:"question"`     // snapshotted at launch
	Options        string  `json:"options"`      // snapshotted at launch (JSON)
	PollType       string  `json:"poll_type"`    // snapshotted at launch
	MultiSelect    bool    `json:"multi_select"` // snapshotted at launch
	RankFilter     *string `json:"rank_filter"`  // NULL = all active; JSON ["R4","R5"] = subset
	TotalEligible  int     `json:"total_eligible"`  // snapshotted at launch
	CreatedBy      string  `json:"created_by"`
	CreatedAt      string  `json:"created_at"`
	RespondedCount int     `json:"responded_count"` // computed on list queries
}

type PollMemberStatus struct {
	MemberID   int      `json:"member_id"`
	MemberName string   `json:"member_name"`
	Rank       string   `json:"rank"`
	Responded  bool     `json:"responded"`
	Options    []string `json:"options"` // selected option(s), empty if pending
}

type PollAnonCount struct {
	OptionKey     string `json:"option_key"`
	ResponseCount int    `json:"response_count"`
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
	MemberID                  int     `json:"member_id"`
	Name                      string  `json:"name"`
	Rank                      string  `json:"rank"`
	VSTotalWeek               int     `json:"vs_total_week"`
	VSYesterday               int     `json:"vs_yesterday"`
	VSTotalPrevWeek           int     `json:"vs_total_prev_week"`
	VSDayMonday               int     `json:"vs_day_monday"`
	VSDayTuesday              int     `json:"vs_day_tuesday"`
	VSDayWednesday            int     `json:"vs_day_wednesday"`
	VSDayThursday             int     `json:"vs_day_thursday"`
	VSDayFriday               int     `json:"vs_day_friday"`
	VSDaySaturday             int     `json:"vs_day_saturday"`
	DaysSinceFreeConducted    float64 `json:"days_since_free_conducted"`    // 9999 = never
	DaysSinceAnyConducted     float64 `json:"days_since_any_conducted"`     // 9999 = never
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
	MemberID          int
	// OCRBackendMode is "cloud" (default — Cloud Vision auto-detect) or
	// "local" (PaddleOCR sidecar; user picks scene per batch). Templates
	// use this to swap help text and conditionally disable the
	// auto-detect option in the upload page.
	OCRBackendMode    string
	AllianceName      string
	AllianceTag       string
	SkillLabels       map[string]string
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
