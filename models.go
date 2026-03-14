package main

import (
	"database/sql"
	"html/template"
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
	TroopLevel          int    `json:"troop_level"`
	Profession          string `json:"profession"`
}

type SquadPowerHistory struct {
	ID         int    `json:"id"`
	MemberID   int    `json:"member_id"`
	Power      int64  `json:"power"`
	RecordedAt string `json:"recorded_at"`
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
	ID          int    `json:"id"`
	MemberID    int    `json:"member_id"`
	MemberName  string `json:"member_name"`
	MemberRank  string `json:"member_rank"`
	Points      int    `json:"points"`
	Notes       string `json:"notes"`
	CreatedBy   string `json:"created_by"`
	CreatedByID int    `json:"created_by_id"`
	CreatedAt   string `json:"created_at"`
	Expired     bool   `json:"expired"`
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
	AwardFirstPoints             int    `json:"award_first_points"`
	AwardSecondPoints            int    `json:"award_second_points"`
	AwardThirdPoints             int    `json:"award_third_points"`
	RecommendationPoints         int    `json:"recommendation_points"`
	RecentConductorPenaltyDays   int    `json:"recent_conductor_penalty_days"`
	AboveAverageConductorPenalty int    `json:"above_average_conductor_penalty"`
	R4R5RankBoost                int    `json:"r4r5_rank_boost"`
	FirstTimeConductorBoost      int    `json:"first_time_conductor_boost"`
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
}

type MemberRanking struct {
	Member                  Member        `json:"member"`
	TotalScore              int           `json:"total_score"`
	AwardPoints             int           `json:"award_points"`
	RecommendationPoints    int           `json:"recommendation_points"`
	RecentConductorPenalty  int           `json:"recent_conductor_penalty"`
	AboveAveragePenalty     int           `json:"above_average_penalty"`
	RankBoost               int           `json:"rank_boost"`
	FirstTimeConductorBoost int           `json:"first_time_conductor_boost"`
	ConductorCount          int           `json:"conductor_count"`
	LastConductorDate       *string       `json:"last_conductor_date"`
	DaysSinceLastConductor  *int          `json:"days_since_last_conductor"`
	AwardDetails            []AwardDetail `json:"award_details"`
	RecommendationCount     int           `json:"recommendation_count"`
}

type AwardDetail struct {
	AwardType string `json:"award_type"`
	Rank      int    `json:"rank"`
	Points    int    `json:"points"`
	WeekDate  string `json:"week_date"`
	Expired   bool   `json:"expired"`
}

type StormAssignment struct {
	ID         int    `json:"id"`
	TaskForce  string `json:"task_force"`
	BuildingID string `json:"building_id"`
	MemberID   int    `json:"member_id"`
	Position   int    `json:"position"`
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
	Rank           string `json:"rank"`
	ViewTrain      bool   `json:"view_train"`
	ManageTrain    bool   `json:"manage_train"`
	ViewAwards     bool   `json:"view_awards"`
	ManageAwards   bool   `json:"manage_awards"`
	ViewRecs       bool   `json:"view_recs"`
	ManageRecs     bool   `json:"manage_recs"`
	ViewDyno       bool   `json:"view_dyno"`
	ManageDyno     bool   `json:"manage_dyno"`
	ViewRankings   bool   `json:"view_rankings"`
	ViewStorm      bool   `json:"view_storm"`
	ManageStorm    bool   `json:"manage_storm"`
	ViewVSPoints   bool   `json:"view_vs_points"`
	ManageVSPoints bool   `json:"manage_vs_points"`
	ViewUpload     bool   `json:"view_upload"`
	ManageMembers  bool   `json:"manage_members"`
	ManageSettings bool   `json:"manage_settings"`
	ViewFiles      bool   `json:"view_files"`
	UploadFiles    bool   `json:"upload_files"`
	ManageFiles    bool   `json:"manage_files"`
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
	Title           string
	ActivePage      string
	IsAuthenticated bool
	Username        string
	IsAdmin         bool
	Rank            string
	Permissions     RankPermissions
	CSRFToken       template.HTML
}

type WeekAwards struct {
	WeekDate string             `json:"week_date"`
	Awards   map[string][]Award `json:"awards"`
}
