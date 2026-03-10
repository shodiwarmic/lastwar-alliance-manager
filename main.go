package main

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"log"
	"math"
	"math/big"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	gosseract "github.com/otiai10/gosseract/v2"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type Member struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	Rank           string `json:"rank"`
	Eligible       bool   `json:"eligible"`
	Power          *int64 `json:"power"`
	PowerUpdatedAt string `json:"power_updated_at"`
	HasUser        bool   `json:"has_user"`
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

type WeekAwards struct {
	WeekDate string             `json:"week_date"`
	Awards   map[string][]Award `json:"awards"`
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
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	MemberID *int   `json:"member_id,omitempty"`
	IsAdmin  bool   `json:"is_admin"`
}

type AdminUserResponse struct {
	ID           int            `json:"id"`
	Username     string         `json:"username"`
	MemberID     *int           `json:"member_id,omitempty"`
	MemberName   *string        `json:"member_name,omitempty"`
	IsAdmin      bool           `json:"is_admin"`
	CreatedAt    string         `json:"created_at,omitempty"`
	LastLogin    *string        `json:"last_login,omitempty"`
	LoginCount   int            `json:"login_count"`
	RecentLogins []LoginSession `json:"recent_logins,omitempty"`
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
	IsNew        bool     `json:"is_new"`
	RankChanged  bool     `json:"rank_changed"`
	OldRank      string   `json:"old_rank,omitempty"`
	SimilarMatch []string `json:"similar_match,omitempty"`
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

var db *sql.DB
var store *sessions.CookieStore

// Calculate Levenshtein distance between two strings
func levenshteinDistance(s1, s2 string) int {
	s1Lower := strings.ToLower(s1)
	s2Lower := strings.ToLower(s2)
	len1 := len(s1Lower)
	len2 := len(s2Lower)

	if len1 == 0 {
		return len2
	}
	if len2 == 0 {
		return len1
	}

	matrix := make([][]int, len1+1)
	for i := range matrix {
		matrix[i] = make([]int, len2+1)
		matrix[i][0] = i
	}
	for j := 0; j <= len2; j++ {
		matrix[0][j] = j
	}

	for i := 1; i <= len1; i++ {
		for j := 1; j <= len2; j++ {
			cost := 0
			if s1Lower[i-1] != s2Lower[j-1] {
				cost = 1
			}
			matrix[i][j] = min(matrix[i-1][j]+1, matrix[i][j-1]+1, matrix[i-1][j-1]+cost)
		}
	}
	return matrix[len1][len2]
}

func min(nums ...int) int {
	if len(nums) == 0 {
		return 0
	}
	minNum := nums[0]
	for _, n := range nums[1:] {
		if n < minNum {
			minNum = n
		}
	}
	return minNum
}

// Check if two names are similar (case-insensitive)
func areSimilar(name1, name2 string) bool {
	if strings.EqualFold(name1, name2) {
		return false // Exact match, not similar but same
	}

	// Calculate similarity (case-insensitive)
	lower1 := strings.ToLower(name1)
	lower2 := strings.ToLower(name2)
	dist := levenshteinDistance(lower1, lower2)
	maxLen := max(len(lower1), len(lower2))
	similarity := 1.0 - float64(dist)/float64(maxLen)

	// Consider similar if:
	// 1. Similarity >= 70%
	// 2. Distance <= 3 characters
	// 3. One name contains the other (for abbreviations like IRA vs IRAQ Army)
	if similarity >= 0.7 || dist <= 3 {
		return true
	}

	// Check if one name contains significant part of another
	name1Lower := strings.ToLower(name1)
	name2Lower := strings.ToLower(name2)
	if strings.Contains(name1Lower, name2Lower) || strings.Contains(name2Lower, name1Lower) {
		if len(name1) >= 3 && len(name2) >= 3 {
			return true
		}
	}

	return false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// initSessionStore initializes the session store with secure settings
func initSessionStore() {
	// Get session key from environment or generate a secure one
	sessionKey := os.Getenv("SESSION_KEY")
	if sessionKey == "" {
		// Generate a random 32-byte key for development
		key := make([]byte, 32)
		rand.Read(key)
		sessionKey = hex.EncodeToString(key)
		log.Println("WARNING: No SESSION_KEY environment variable set. Using generated key (not persistent across restarts).")
		log.Printf("For production, set SESSION_KEY environment variable. Example: export SESSION_KEY=%s", sessionKey)
	}

	// Decode hex key
	key, err := hex.DecodeString(sessionKey)
	if err != nil || len(key) != 32 {
		// Fallback: use the string directly if not valid hex
		key = []byte(sessionKey)
		if len(key) < 32 {
			// Pad to 32 bytes
			padded := make([]byte, 32)
			copy(padded, key)
			key = padded
		}
	}

	store = sessions.NewCookieStore(key[:32])

	// Configure secure cookie options
	// Check if we're running in production (HTTPS)
	isProduction := os.Getenv("PRODUCTION") == "true" || os.Getenv("HTTPS") == "true"

	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400,                   // 24 hours
		HttpOnly: true,                    // Prevent JavaScript access
		Secure:   isProduction,            // Only send over HTTPS in production
		SameSite: http.SameSiteStrictMode, // CSRF protection
	}

	if isProduction {
		log.Println("Session cookies configured for HTTPS (Secure flag enabled)")
	} else {
		log.Println("Session cookies configured for HTTP (development mode)")
	}
}

// RankingContext holds all data needed for ranking calculations
type RankingContext struct {
	Settings          Settings
	RecommendationMap map[int]int // memberID -> count
	AwardScoreMap     map[int]int // memberID -> total points
	ConductorStats    map[int]ConductorStat
	AvgConductorCount float64
	ReferenceDate     time.Time
}

type ConductorStat struct {
	Count          int
	LastDate       *string
	LastBackupUsed *string
}

// loadSettings loads the settings from the database
func loadSettings() (Settings, error) {
	var settings Settings
	err := db.QueryRow(`SELECT id, award_first_points, award_second_points, award_third_points, 
		recommendation_points, recent_conductor_penalty_days, above_average_conductor_penalty, r4r5_rank_boost,
		first_time_conductor_boost, schedule_message_template, daily_message_template 
		FROM settings WHERE id = 1`).Scan(
		&settings.ID,
		&settings.AwardFirstPoints,
		&settings.AwardSecondPoints,
		&settings.AwardThirdPoints,
		&settings.RecommendationPoints,
		&settings.RecentConductorPenaltyDays,
		&settings.AboveAverageConductorPenalty,
		&settings.R4R5RankBoost,
		&settings.FirstTimeConductorBoost,
		&settings.ScheduleMessageTemplate,
		&settings.DailyMessageTemplate,
	)
	return settings, err
}

// loadRecommendations loads recommendation counts for all members (active only)
// A recommendation is active if the member hasn't been assigned as conductor/backup after it was created
func loadRecommendations() (map[int]int, error) {
	rows, err := db.Query(`
		SELECT r.member_id, COUNT(*) as rec_count
		FROM recommendations r
		WHERE NOT EXISTS (
			SELECT 1 FROM train_schedules ts
			WHERE (ts.conductor_id = r.member_id OR (ts.backup_id = r.member_id AND ts.conductor_showed_up = 0))
			AND ts.date >= date(r.created_at)
		)
		GROUP BY r.member_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	recommendationMap := make(map[int]int)
	for rows.Next() {
		var memberID, count int
		if err := rows.Scan(&memberID, &count); err != nil {
			return nil, err
		}
		recommendationMap[memberID] = count
	}
	return recommendationMap, nil
}

// loadAwards loads award scores for all members (active only)
// An award is active if the member hasn't been assigned as conductor/backup after the award week
func loadAwards(settings Settings) (map[int]int, error) {
	rows, err := db.Query(`
		SELECT a.member_id, a.rank
		FROM awards a
		WHERE NOT EXISTS (
			SELECT 1 FROM train_schedules ts
			WHERE (ts.conductor_id = a.member_id OR (ts.backup_id = a.member_id AND ts.conductor_showed_up = 0))
			AND ts.date >= a.week_date
		)
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	awardScoreMap := make(map[int]int)
	for rows.Next() {
		var memberID, rank int
		if err := rows.Scan(&memberID, &rank); err != nil {
			return nil, err
		}
		switch rank {
		case 1:
			awardScoreMap[memberID] += settings.AwardFirstPoints
		case 2:
			awardScoreMap[memberID] += settings.AwardSecondPoints
		case 3:
			awardScoreMap[memberID] += settings.AwardThirdPoints
		}
	}
	return awardScoreMap, nil
}

// loadConductorStats loads conductor statistics for all members
func loadConductorStats() (map[int]ConductorStat, float64, error) {
	rows, err := db.Query(`
		SELECT conductor_id, COUNT(*) as conductor_count, MAX(date) as last_date
		FROM train_schedules
		GROUP BY conductor_id
	`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	conductorStats := make(map[int]ConductorStat)
	totalConductorCount := 0
	memberCount := 0

	for rows.Next() {
		var memberID, count int
		var lastDate sql.NullString
		if err := rows.Scan(&memberID, &count, &lastDate); err != nil {
			return nil, 0, err
		}
		var lastDatePtr *string
		if lastDate.Valid {
			lastDatePtr = &lastDate.String
		}
		conductorStats[memberID] = ConductorStat{
			Count:    count,
			LastDate: lastDatePtr,
		}
		totalConductorCount += count
		memberCount++
	}

	// Load backup usage dates (when conductor didn't show up)
	backupRows, err := db.Query(`
		SELECT backup_id, MAX(date) as last_backup_used
		FROM train_schedules
		WHERE conductor_showed_up = 0
		GROUP BY backup_id
	`)
	if err != nil {
		return nil, 0, err
	}
	defer backupRows.Close()

	for backupRows.Next() {
		var memberID int
		var lastBackupUsed sql.NullString
		if err := backupRows.Scan(&memberID, &lastBackupUsed); err != nil {
			return nil, 0, err
		}
		var lastBackupUsedPtr *string
		if lastBackupUsed.Valid {
			lastBackupUsedPtr = &lastBackupUsed.String
		}

		// Update existing stat or create new one
		if stat, exists := conductorStats[memberID]; exists {
			stat.LastBackupUsed = lastBackupUsedPtr
			conductorStats[memberID] = stat
		} else {
			conductorStats[memberID] = ConductorStat{
				Count:          0,
				LastDate:       nil,
				LastBackupUsed: lastBackupUsedPtr,
			}
		}
	}

	var avgConductorCount float64
	if memberCount > 0 {
		avgConductorCount = float64(totalConductorCount) / float64(memberCount)
	}

	return conductorStats, avgConductorCount, nil
}

// buildRankingContext creates a complete ranking context for calculations
func buildRankingContext(referenceDate time.Time) (*RankingContext, error) {
	settings, err := loadSettings()
	if err != nil {
		return nil, err
	}

	recommendationMap, err := loadRecommendations()
	if err != nil {
		return nil, err
	}

	// Get all non-expired awards (stacks up over multiple weeks)
	awardScoreMap, err := loadAwards(settings)
	if err != nil {
		return nil, err
	}

	conductorStats, avgConductorCount, err := loadConductorStats()
	if err != nil {
		return nil, err
	}

	return &RankingContext{
		Settings:          settings,
		RecommendationMap: recommendationMap,
		AwardScoreMap:     awardScoreMap,
		ConductorStats:    conductorStats,
		AvgConductorCount: avgConductorCount,
		ReferenceDate:     referenceDate,
	}, nil
}

// calculateMemberScore calculates the ranking score for a member
func calculateMemberScore(member Member, ctx *RankingContext) int {
	score := 0

	// Add recommendation points with non-linear scaling (diminishing returns)
	recCount := ctx.RecommendationMap[member.ID]
	if recCount > 0 {
		// Formula: 5 + 5 * sqrt(recCount) rounded to nearest int
		// This gives: 1 rec = 10pts, 2 recs = 12pts, 3 recs = 14pts, 4 recs = 15pts
		recPoints := 5.0 + 5.0*math.Sqrt(float64(recCount))
		score += int(math.Round(recPoints))
	}

	// Add award points
	score += ctx.AwardScoreMap[member.ID]

	// Add rank boost for R4/R5 members (exponential based on days since last conductor)
	if member.Rank == "R4" || member.Rank == "R5" {
		baseBoost := float64(ctx.Settings.R4R5RankBoost)

		// Calculate days since last conductor/backup duty
		var daysSinceLastDuty int = 0
		if stats, exists := ctx.ConductorStats[member.ID]; exists {
			var mostRecentDate *time.Time

			if stats.LastDate != nil {
				if lastDate, err := parseDate(*stats.LastDate); err == nil {
					mostRecentDate = &lastDate
				}
			}

			// Check if backup usage was more recent
			if stats.LastBackupUsed != nil {
				if backupDate, err := parseDate(*stats.LastBackupUsed); err == nil {
					if mostRecentDate == nil || backupDate.After(*mostRecentDate) {
						mostRecentDate = &backupDate
					}
				}
			}

			if mostRecentDate != nil {
				daysSinceLastDuty = int(ctx.ReferenceDate.Sub(*mostRecentDate).Hours() / 24)
			}
		}

		// Exponential formula: base_boost * 2^(days/7)
		// Doubles every week, guarantees selection within 3 weeks
		multiplier := math.Pow(2, float64(daysSinceLastDuty)/7.0)
		score += int(math.Round(baseBoost * multiplier))
	}

	// Add first time conductor boost if member has never been conductor and has some points
	if stats, exists := ctx.ConductorStats[member.ID]; !exists || stats.Count == 0 {
		// Only give boost if they have some positive score (awards, recommendations, or rank boost)
		if score > 0 {
			score += ctx.Settings.FirstTimeConductorBoost
		}
	}

	// Apply conductor-based penalties
	if stats, exists := ctx.ConductorStats[member.ID]; exists {
		// Penalize if above average conductor count
		if float64(stats.Count) > ctx.AvgConductorCount {
			score -= ctx.Settings.AboveAverageConductorPenalty
		}

		// Penalize recent conductors - check both conductor date and backup used date
		var mostRecentDate *time.Time

		if stats.LastDate != nil {
			if lastDate, err := parseDate(*stats.LastDate); err == nil {
				mostRecentDate = &lastDate
			}
		}

		// If they stepped in as backup, check if that's more recent
		if stats.LastBackupUsed != nil {
			if backupDate, err := parseDate(*stats.LastBackupUsed); err == nil {
				if mostRecentDate == nil || backupDate.After(*mostRecentDate) {
					mostRecentDate = &backupDate
				}
			}
		}

		// Apply penalty based on most recent duty (conductor or backup usage)
		if mostRecentDate != nil {
			daysSince := int(ctx.ReferenceDate.Sub(*mostRecentDate).Hours() / 24)
			penalty := ctx.Settings.RecentConductorPenaltyDays - daysSince
			if penalty > 0 {
				score -= penalty
			}
		}
	}

	return score
}

func initDB() error {
	var err error

	// Use DATABASE_PATH environment variable if set, otherwise use local path
	dbPath := os.Getenv("DATABASE_PATH")
	if dbPath == "" {
		dbPath = "./alliance.db"
	}

	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}

	// Create members table
	createMembersTableSQL := `CREATE TABLE IF NOT EXISTS members (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		rank TEXT NOT NULL,
		eligible BOOLEAN NOT NULL DEFAULT 1
	);`

	_, err = db.Exec(createMembersTableSQL)
	if err != nil {
		return err
	}

	// Migrate existing members table to add eligible column if missing
	var eligibleColumnExists bool
	err = db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('members')
		WHERE name = 'eligible'
	`).Scan(&eligibleColumnExists)
	if err != nil {
		return err
	}

	if !eligibleColumnExists {
		_, err = db.Exec(`ALTER TABLE members ADD COLUMN eligible BOOLEAN NOT NULL DEFAULT 1`)
		if err != nil {
			return err
		}
		log.Println("Database migration: Added eligible column to members table")
	}

	// Create users table
	createUsersTableSQL := `CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password TEXT NOT NULL,
		member_id INTEGER,
		is_admin BOOLEAN DEFAULT 0,
		FOREIGN KEY (member_id) REFERENCES members(id) ON DELETE SET NULL
	);`

	_, err = db.Exec(createUsersTableSQL)
	if err != nil {
		return err
	}

	// Create train_schedules table
	createTrainSchedulesSQL := `CREATE TABLE IF NOT EXISTS train_schedules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		date TEXT NOT NULL UNIQUE,
		conductor_id INTEGER NOT NULL,
		backup_id INTEGER,
		conductor_score INTEGER,
		conductor_showed_up BOOLEAN,
		notes TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (conductor_id) REFERENCES members(id) ON DELETE CASCADE,
		FOREIGN KEY (backup_id) REFERENCES members(id) ON DELETE CASCADE
	);`

	_, err = db.Exec(createTrainSchedulesSQL)
	if err != nil {
		return err
	}

	// Migrate existing train_schedules table to add conductor_score column if missing
	var columnExists bool
	err = db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('train_schedules')
		WHERE name = 'conductor_score'
	`).Scan(&columnExists)
	if err != nil {
		return err
	}

	if !columnExists {
		_, err = db.Exec(`ALTER TABLE train_schedules ADD COLUMN conductor_score INTEGER`)
		if err != nil {
			return err
		}
		log.Println("Database migration: Added conductor_score column to train_schedules table")
	}

	// Migrate train_schedules to make backup_id nullable (for existing databases)
	// Check if the table structure needs migration by checking pragma
	migrationNeeded := false
	var backupIdNotnull int
	err = db.QueryRow(`
		SELECT "notnull"
		FROM pragma_table_info('train_schedules')
		WHERE name = 'backup_id'
	`).Scan(&backupIdNotnull)

	if err == nil && backupIdNotnull == 1 {
		migrationNeeded = true
	}

	if migrationNeeded {
		log.Println("Database migration: Making backup_id nullable in train_schedules table")

		// Create new table with correct schema
		_, err = db.Exec(`CREATE TABLE train_schedules_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			date TEXT NOT NULL UNIQUE,
			conductor_id INTEGER NOT NULL,
			backup_id INTEGER,
			conductor_score INTEGER,
			conductor_showed_up BOOLEAN,
			notes TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (conductor_id) REFERENCES members(id) ON DELETE CASCADE,
			FOREIGN KEY (backup_id) REFERENCES members(id) ON DELETE CASCADE
		)`)
		if err != nil {
			return fmt.Errorf("failed to create new train_schedules table: %v", err)
		}

		// Copy data from old table
		_, err = db.Exec(`INSERT INTO train_schedules_new (id, date, conductor_id, backup_id, conductor_score, conductor_showed_up, notes, created_at)
			SELECT id, date, conductor_id, backup_id, conductor_score, conductor_showed_up, notes, created_at
			FROM train_schedules`)
		if err != nil {
			return fmt.Errorf("failed to copy train_schedules data: %v", err)
		}

		// Drop old table
		_, err = db.Exec(`DROP TABLE train_schedules`)
		if err != nil {
			return fmt.Errorf("failed to drop old train_schedules table: %v", err)
		}

		// Rename new table
		_, err = db.Exec(`ALTER TABLE train_schedules_new RENAME TO train_schedules`)
		if err != nil {
			return fmt.Errorf("failed to rename train_schedules_new table: %v", err)
		}

		log.Println("Database migration: Successfully made backup_id nullable")
	}

	// Create award_types table
	createAwardTypesSQL := `CREATE TABLE IF NOT EXISTS award_types (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		active BOOLEAN DEFAULT 1,
		sort_order INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`

	_, err = db.Exec(createAwardTypesSQL)
	if err != nil {
		return err
	}

	// Insert default award types if table is empty
	var awardTypeCount int
	err = db.QueryRow("SELECT COUNT(*) FROM award_types").Scan(&awardTypeCount)
	if err != nil {
		return err
	}

	if awardTypeCount == 0 {
		defaultAwards := []string{
			"Alliance Champion",
			"Star of Desert Storm",
			"Soldier Crusher",
			"Divine Healer",
			"Great Destroyer",
			"Grind King",
			"Alliance Exercise MVP",
			"Doom Elite Slayer",
			"Best Manager",
			"Alliance Sponsor",
			"Firefighting Leader",
			"Excavator Radar",
			"Shining Star",
			"MVP",
			"Devil Trainer",
			"Trial Assist King",
			"Good Helper",
		}

		for i, award := range defaultAwards {
			_, err = db.Exec("INSERT INTO award_types (name, active, sort_order) VALUES (?, 1, ?)", award, i)
			if err != nil {
				return err
			}
		}
	}

	// Create awards table
	createAwardsSQL := `CREATE TABLE IF NOT EXISTS awards (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		week_date TEXT NOT NULL,
		award_type TEXT NOT NULL,
		rank INTEGER NOT NULL,
		member_id INTEGER NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		expired BOOLEAN DEFAULT 0,
		FOREIGN KEY (member_id) REFERENCES members(id) ON DELETE CASCADE,
		UNIQUE(week_date, award_type, rank)
	);`

	_, err = db.Exec(createAwardsSQL)
	if err != nil {
		return err
	}

	// Create power_history table
	createPowerHistorySQL := `CREATE TABLE IF NOT EXISTS power_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		member_id INTEGER NOT NULL,
		power INTEGER NOT NULL,
		recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (member_id) REFERENCES members(id) ON DELETE CASCADE
	);`

	_, err = db.Exec(createPowerHistorySQL)
	if err != nil {
		return err
	}

	// Create index for faster power history queries
	_, err = db.Exec("CREATE INDEX IF NOT EXISTS idx_power_history_member ON power_history(member_id, recorded_at DESC)")
	if err != nil {
		return err
	}

	// Create recommendations table
	createRecommendationsSQL := `CREATE TABLE IF NOT EXISTS recommendations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		member_id INTEGER NOT NULL,
		recommended_by_id INTEGER NOT NULL,
		notes TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		expired BOOLEAN DEFAULT 0,
		FOREIGN KEY (member_id) REFERENCES members(id) ON DELETE CASCADE,
		FOREIGN KEY (recommended_by_id) REFERENCES users(id) ON DELETE CASCADE
	);`

	_, err = db.Exec(createRecommendationsSQL)
	if err != nil {
		return err
	}

	// Create dyno recommendations table (informal feedback that expires after 1 week)
	createDynoRecommendationsSQL := `CREATE TABLE IF NOT EXISTS dyno_recommendations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		member_id INTEGER NOT NULL,
		points INTEGER NOT NULL,
		notes TEXT NOT NULL,
		created_by_id INTEGER NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (member_id) REFERENCES members(id) ON DELETE CASCADE,
		FOREIGN KEY (created_by_id) REFERENCES users(id) ON DELETE CASCADE
	);`

	_, err = db.Exec(createDynoRecommendationsSQL)
	if err != nil {
		return err
	}

	// Create settings table
	createSettingsSQL := `CREATE TABLE IF NOT EXISTS settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		award_first_points INTEGER NOT NULL DEFAULT 3,
		award_second_points INTEGER NOT NULL DEFAULT 2,
		award_third_points INTEGER NOT NULL DEFAULT 1,
		recommendation_points INTEGER NOT NULL DEFAULT 10,
		recent_conductor_penalty_days INTEGER NOT NULL DEFAULT 30,
		above_average_conductor_penalty INTEGER NOT NULL DEFAULT 10,
		r4r5_rank_boost INTEGER NOT NULL DEFAULT 5,
		first_time_conductor_boost INTEGER NOT NULL DEFAULT 5,
		schedule_message_template TEXT NOT NULL DEFAULT 'Train Schedule - Week {WEEK}\n\n{SCHEDULES}\n\nNext in line:\n{NEXT_3}'
	);`

	// Add new columns if they don't exist
	_, err = db.Exec(`ALTER TABLE settings ADD COLUMN storm_timezones TEXT DEFAULT 'America/New_York,Europe/London'`)
	// Ignore error if column already exists

	_, err = db.Exec(`ALTER TABLE settings ADD COLUMN storm_respect_dst BOOLEAN DEFAULT 1`)
	// Ignore error if column already exists

	_, err = db.Exec(createSettingsSQL)
	if err != nil {
		return err
	}

	// Create storm assignments table
	createStormAssignmentsSQL := `CREATE TABLE IF NOT EXISTS storm_assignments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_force TEXT NOT NULL CHECK (task_force IN ('A', 'B')),
		building_id TEXT NOT NULL,
		member_id INTEGER NOT NULL,
		position INTEGER NOT NULL CHECK (position BETWEEN 1 AND 4),
		FOREIGN KEY (member_id) REFERENCES members(id) ON DELETE CASCADE,
		UNIQUE(task_force, building_id, position)
	);`

	_, err = db.Exec(createStormAssignmentsSQL)
	if err != nil {
		return err
	}

	// Create login_sessions table for tracking login history
	createLoginSessionsSQL := `CREATE TABLE IF NOT EXISTS login_sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		username TEXT NOT NULL,
		ip_address TEXT,
		user_agent TEXT,
		country TEXT,
		city TEXT,
		isp TEXT,
		login_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		success BOOLEAN DEFAULT 1,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);`

	_, err = db.Exec(createLoginSessionsSQL)
	if err != nil {
		return err
	}

	// Create index for faster login history queries
	_, err = db.Exec("CREATE INDEX IF NOT EXISTS idx_login_sessions_user ON login_sessions(user_id, login_time DESC)")
	if err != nil {
		return err
	}

	// Create vs_points table for tracking VS points Monday-Saturday
	createVSPointsSQL := `CREATE TABLE IF NOT EXISTS vs_points (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		member_id INTEGER NOT NULL,
		week_date TEXT NOT NULL,
		monday INTEGER NOT NULL DEFAULT 0,
		tuesday INTEGER NOT NULL DEFAULT 0,
		wednesday INTEGER NOT NULL DEFAULT 0,
		thursday INTEGER NOT NULL DEFAULT 0,
		friday INTEGER NOT NULL DEFAULT 0,
		saturday INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (member_id) REFERENCES members(id) ON DELETE CASCADE,
		UNIQUE(member_id, week_date)
	);`

	_, err = db.Exec(createVSPointsSQL)
	if err != nil {
		return err
	}

	// Create index for faster VS points queries
	_, err = db.Exec("CREATE INDEX IF NOT EXISTS idx_vs_points_week ON vs_points(week_date)")
	if err != nil {
		return err
	}

	// Initialize default settings if not exist
	var settingsCount int
	err = db.QueryRow("SELECT COUNT(*) FROM settings").Scan(&settingsCount)
	if err != nil {
		return err
	}

	if settingsCount == 0 {
		_, err = db.Exec(`INSERT INTO settings (id, award_first_points, award_second_points, award_third_points, 
			recommendation_points, recent_conductor_penalty_days, above_average_conductor_penalty, r4r5_rank_boost, 
			first_time_conductor_boost, schedule_message_template) 
			VALUES (1, 3, 2, 1, 10, 30, 10, 5, 5, 'Train Schedule - Week {WEEK}\n\n{SCHEDULES}\n\nNext in line:\n{NEXT_3}')`)
		if err != nil {
			return err
		}
		log.Println("Default settings initialized")
	}

	// Migrate settings table to add r4r5_rank_boost column if missing
	var rankBoostColumnExists bool
	err = db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('settings')
		WHERE name = 'r4r5_rank_boost'
	`).Scan(&rankBoostColumnExists)
	if err != nil {
		return err
	}

	if !rankBoostColumnExists {
		_, err = db.Exec(`ALTER TABLE settings ADD COLUMN r4r5_rank_boost INTEGER NOT NULL DEFAULT 5`)
		if err != nil {
			return err
		}
		log.Println("Database migration: Added r4r5_rank_boost column to settings table")
	}

	// Migrate settings table to add schedule_message_template column if missing
	var scheduleTemplateColumnExists bool
	err = db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('settings')
		WHERE name = 'schedule_message_template'
	`).Scan(&scheduleTemplateColumnExists)
	if err != nil {
		return err
	}

	if !scheduleTemplateColumnExists {
		_, err = db.Exec(`ALTER TABLE settings ADD COLUMN schedule_message_template TEXT NOT NULL DEFAULT 'Train Schedule - Week {WEEK}

{SCHEDULES}

Next in line:
{NEXT_3}'`)
		if err != nil {
			return err
		}
		log.Println("Database migration: Added schedule_message_template column to settings table")
	}

	// Migrate settings table to add first_time_conductor_boost column if missing
	var firstTimeBoostColumnExists bool
	err = db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('settings')
		WHERE name = 'first_time_conductor_boost'
	`).Scan(&firstTimeBoostColumnExists)
	if err != nil {
		return err
	}

	if !firstTimeBoostColumnExists {
		_, err = db.Exec(`ALTER TABLE settings ADD COLUMN first_time_conductor_boost INTEGER NOT NULL DEFAULT 5`)
		if err != nil {
			return err
		}
		log.Println("Database migration: Added first_time_conductor_boost column to settings table")
	}

	// Migrate settings table to add daily_message_template column if missing
	var dailyTemplateColumnExists bool
	err = db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('settings')
		WHERE name = 'daily_message_template'
	`).Scan(&dailyTemplateColumnExists)
	if err != nil {
		return err
	}

	if !dailyTemplateColumnExists {
		defaultDailyTemplate := `ALL ABOARD! Daily Train Assignment

Date: {DATE}

Today's Conductor: {CONDUCTOR_NAME} ({CONDUCTOR_RANK})
Backup Engineer: {BACKUP_NAME} ({BACKUP_RANK})

DEPARTURE SCHEDULE:
- 15:00 ST (17:00 UK) - Conductor {CONDUCTOR_NAME}, please request train assignment in alliance chat
- 16:30 ST (18:30 UK) - If conductor hasn't shown up, Backup {BACKUP_NAME} takes over and assigns train to themselves

Remember: Communication is key! Let the alliance know if you can't make it.

All aboard for another successful run!`
		// Add column without default first
		_, err = db.Exec(`ALTER TABLE settings ADD COLUMN daily_message_template TEXT`)
		if err != nil {
			return err
		}
		// Then update existing row with the default value
		_, err = db.Exec(`UPDATE settings SET daily_message_template = ? WHERE id = 1`, defaultDailyTemplate)
		if err != nil {
			return err
		}
		log.Println("Database migration: Added daily_message_template column to settings table")
	}

	// Migrate settings table to add power_tracking_enabled column if missing
	var powerTrackingColumnExists bool
	err = db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('settings') 
		WHERE name='power_tracking_enabled'
	`).Scan(&powerTrackingColumnExists)
	if err != nil || !powerTrackingColumnExists {
		_, err = db.Exec(`ALTER TABLE settings ADD COLUMN power_tracking_enabled BOOLEAN DEFAULT 0`)
		if err != nil {
			return err
		}
		log.Println("Database migration: Added power_tracking_enabled column to settings table")
	}

	// Create default admin user if no users exist
	var userCount int
	err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount)
	if err != nil {
		return err
	}

	if userCount == 0 {
		// Default credentials: admin/admin123
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		_, err = db.Exec("INSERT INTO users (username, password, is_admin) VALUES (?, ?, ?)", "admin", string(hashedPassword), true)
		if err != nil {
			return err
		}
		log.Println("Default admin user created - Username: admin, Password: admin123")
	}

	return nil
}

// Authentication middleware
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		if auth, ok := session.Values["authenticated"].(bool); !ok || !auth {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// Permission middleware - only R4/R5 or admin can manage ranks
func rankManagementMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")

		// Check if admin
		if isAdmin, ok := session.Values["is_admin"].(bool); ok && isAdmin {
			next(w, r)
			return
		}

		// Check if member has R4 or R5 rank
		if memberID, ok := session.Values["member_id"].(int); ok {
			var rank string
			err := db.QueryRow("SELECT rank FROM members WHERE id = ?", memberID).Scan(&rank)
			if err == nil && (rank == "R4" || rank == "R5") {
				next(w, r)
				return
			}
		}

		http.Error(w, "Forbidden: Only R4/R5 members can manage ranks", http.StatusForbidden)
	}
}

// Permission middleware - only R5 or admin
func adminR5Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")

		// Check if admin
		if isAdmin, ok := session.Values["is_admin"].(bool); ok && isAdmin {
			next(w, r)
			return
		}

		// Check if member has R5 rank
		if memberID, ok := session.Values["member_id"].(int); ok {
			var rank string
			err := db.QueryRow("SELECT rank FROM members WHERE id = ?", memberID).Scan(&rank)
			if err == nil && rank == "R5" {
				next(w, r)
				return
			}
		}

		http.Error(w, "Forbidden: Only R5 members and admins can perform this action", http.StatusForbidden)
	}
}

// Admin-only middleware
func adminMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")

		if isAdmin, ok := session.Values["is_admin"].(bool); !ok || !isAdmin {
			http.Error(w, "Forbidden: Admin access required", http.StatusForbidden)
			return
		}

		next(w, r)
	}
}

// Login handler
func login(w http.ResponseWriter, r *http.Request) {
	var creds Credentials
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var user User
	var memberID sql.NullInt64
	var isAdmin sql.NullBool
	err := db.QueryRow("SELECT id, username, password, member_id, is_admin FROM users WHERE username = ?", creds.Username).Scan(&user.ID, &user.Username, &user.Password, &memberID, &isAdmin)
	if err != nil {
		// Track failed login attempt
		trackLogin(0, creds.Username, r, false)
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	if memberID.Valid {
		mid := int(memberID.Int64)
		user.MemberID = &mid
	}
	user.IsAdmin = isAdmin.Valid && isAdmin.Bool

	// Compare password
	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(creds.Password))
	if err != nil {
		// Track failed login attempt
		trackLogin(user.ID, user.Username, r, false)
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Track successful login
	trackLogin(user.ID, user.Username, r, true)

	// Create session
	session, _ := store.Get(r, "session")
	session.Values["authenticated"] = true
	session.Values["username"] = user.Username
	session.Values["user_id"] = user.ID
	if user.MemberID != nil {
		session.Values["member_id"] = *user.MemberID
	}
	session.Values["is_admin"] = user.IsAdmin
	session.Save(r, w)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"message": "Login successful", "username": user.Username})
}

// Logout handler
func logout(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	session.Values["authenticated"] = false
	session.Options.MaxAge = -1
	session.Save(r, w)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Logout successful"})
}

// Change password handler
func changePassword(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, ok := session.Values["user_id"].(int)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(input.NewPassword) < 6 {
		http.Error(w, "New password must be at least 6 characters", http.StatusBadRequest)
		return
	}

	// Get current password hash
	var currentHash string
	err := db.QueryRow("SELECT password FROM users WHERE id = ?", userID).Scan(&currentHash)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Verify current password
	err = bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(input.CurrentPassword))
	if err != nil {
		http.Error(w, "Current password is incorrect", http.StatusUnauthorized)
		return
	}

	// Hash new password
	newHash, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	// Update password
	_, err = db.Exec("UPDATE users SET password = ? WHERE id = ?", string(newHash), userID)
	if err != nil {
		http.Error(w, "Failed to update password", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Password changed successfully"})
}

// Generate random alphanumeric password
func generateRandomPassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	password := make([]byte, length)
	for i := range password {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		password[i] = charset[num.Int64()]
	}
	return string(password), nil
}

// Get client IP with X-Forwarded-For and X-Real-IP support
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (proxy/load balancer)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP header (nginx)
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if colonIndex := strings.LastIndex(ip, ":"); colonIndex != -1 {
		ip = ip[:colonIndex]
	}
	return ip
}

// Get IP geolocation information using ip-api.com (free, no key required)
func getIPGeolocation(ip string) (*IPGeolocation, error) {
	// Skip localhost/private IPs
	if ip == "127.0.0.1" || ip == "::1" || strings.HasPrefix(ip, "192.168.") || strings.HasPrefix(ip, "10.") {
		return &IPGeolocation{
			Status:  "success",
			Country: "Local Network",
			City:    "Localhost",
			ISP:     "Private Network",
			Query:   ip,
		}, nil
	}

	url := fmt.Sprintf("http://ip-api.com/json/%s?fields=status,country,countryCode,region,regionName,city,zip,lat,lon,timezone,isp,org,as,query", ip)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var geo IPGeolocation
	if err := json.NewDecoder(resp.Body).Decode(&geo); err != nil {
		return nil, err
	}

	if geo.Status != "success" {
		return nil, fmt.Errorf("geolocation lookup failed")
	}

	return &geo, nil
}

// Track login attempt in database
func trackLogin(userID int, username string, r *http.Request, success bool) {
	ip := getClientIP(r)
	userAgent := r.Header.Get("User-Agent")

	var country, city, isp *string

	// Get geolocation data (non-blocking, log errors but don't fail)
	if geo, err := getIPGeolocation(ip); err == nil {
		country = &geo.Country
		city = &geo.City
		isp = &geo.ISP
	}

	_, err := db.Exec(`INSERT INTO login_sessions (user_id, username, ip_address, user_agent, country, city, isp, success) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, username, ip, userAgent, country, city, isp, success)

	if err != nil {
		log.Printf("Failed to track login: %v", err)
	}
}

// Create user for member
func createUserForMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	memberID, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid member ID", http.StatusBadRequest)
		return
	}

	// Check if member exists
	var memberName string
	err = db.QueryRow("SELECT name FROM members WHERE id = ?", memberID).Scan(&memberName)
	if err != nil {
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}

	// Check if user already exists for this member
	var existingUserID int
	err = db.QueryRow("SELECT id FROM users WHERE member_id = ?", memberID).Scan(&existingUserID)
	if err == nil {
		http.Error(w, "User already exists for this member", http.StatusConflict)
		return
	}

	// Generate random password
	randomPassword, err := generateRandomPassword(10)
	if err != nil {
		http.Error(w, "Failed to generate password", http.StatusInternalServerError)
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(randomPassword), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	// Create username from member name (lowercase, no spaces)
	username := strings.ToLower(strings.ReplaceAll(memberName, " ", ""))

	// Check if username already exists, if so, append member ID
	var existingUsername string
	err = db.QueryRow("SELECT username FROM users WHERE username = ?", username).Scan(&existingUsername)
	if err == nil {
		username = username + strconv.Itoa(memberID)
	}

	// Insert user
	_, err = db.Exec("INSERT INTO users (username, password, member_id, is_admin) VALUES (?, ?, ?, ?)",
		username, string(hashedPassword), memberID, false)
	if err != nil {
		http.Error(w, "Failed to create user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":  "User created successfully",
		"username": username,
		"password": randomPassword,
	})
}

// Check auth status
func checkAuth(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	if auth, ok := session.Values["authenticated"].(bool); ok && auth {
		username := session.Values["username"].(string)
		isAdmin := false
		if adminVal, ok := session.Values["is_admin"].(bool); ok {
			isAdmin = adminVal
		}

		var rank string
		var canManageRanks bool

		if isAdmin {
			rank = "Admin"
			canManageRanks = true
		} else if memberID, ok := session.Values["member_id"].(int); ok {
			// Get member's rank
			err := db.QueryRow("SELECT rank FROM members WHERE id = ?", memberID).Scan(&rank)
			if err == nil {
				canManageRanks = (rank == "R4" || rank == "R5")
			}
		}

		// Check if user is R5 or admin (for more sensitive operations)
		isR5OrAdmin := isAdmin
		if !isR5OrAdmin && rank == "R5" {
			isR5OrAdmin = true
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated":    true,
			"username":         username,
			"rank":             rank,
			"is_admin":         isAdmin,
			"can_manage_ranks": canManageRanks,
			"is_r5_or_admin":   isR5OrAdmin,
		})
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"authenticated": false})
	}
}

// Admin: Get all users with login information
func getAdminUsers(w http.ResponseWriter, r *http.Request) {
	query := `
		SELECT u.id, u.username, u.member_id, u.is_admin, 
		   m.name as member_name,
		   (SELECT login_time FROM login_sessions WHERE user_id = u.id AND success = 1 ORDER BY login_time DESC LIMIT 1) as last_login,
		   (SELECT COUNT(*) FROM login_sessions WHERE user_id = u.id AND success = 1) as login_count
		FROM users u
		LEFT JOIN members m ON u.member_id = m.id
		ORDER BY u.is_admin DESC, u.username ASC
	`

	rows, err := db.Query(query)
	if err != nil {
		http.Error(w, "Failed to fetch users", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	users := []AdminUserResponse{}
	for rows.Next() {
		var user AdminUserResponse
		var memberID sql.NullInt64
		var memberName sql.NullString
		var lastLogin sql.NullString

		err := rows.Scan(&user.ID, &user.Username, &memberID, &user.IsAdmin,
			&memberName, &lastLogin, &user.LoginCount)
		if err != nil {
			continue
		}

		if memberID.Valid {
			mid := int(memberID.Int64)
			user.MemberID = &mid
		}
		if memberName.Valid {
			user.MemberName = &memberName.String
		}
		if lastLogin.Valid {
			user.LastLogin = &lastLogin.String
		}

		// Get recent logins (last 5)
		loginRows, err := db.Query(`
			SELECT id, user_id, username, ip_address, user_agent, country, city, isp, login_time, success
			FROM login_sessions
			WHERE user_id = ? AND success = 1
			ORDER BY login_time DESC
			LIMIT 5
		`, user.ID)

		if err == nil {
			recentLogins := []LoginSession{}
			for loginRows.Next() {
				var login LoginSession
				var ipAddr, userAgent, country, city, isp sql.NullString

				loginRows.Scan(&login.ID, &login.UserID, &login.Username,
					&ipAddr, &userAgent, &country, &city, &isp,
					&login.LoginTime, &login.Success)

				if ipAddr.Valid {
					login.IPAddress = &ipAddr.String
				}
				if userAgent.Valid {
					login.UserAgent = &userAgent.String
				}
				if country.Valid {
					login.Country = &country.String
				}
				if city.Valid {
					login.City = &city.String
				}
				if isp.Valid {
					login.ISP = &isp.String
				}

				recentLogins = append(recentLogins, login)
			}
			loginRows.Close()
			user.RecentLogins = recentLogins
		}

		users = append(users, user)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}

// Admin: Create new user
func createAdminUser(w http.ResponseWriter, r *http.Request) {
	var req AdminUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Username == "" || req.Password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	if len(req.Password) < 6 {
		http.Error(w, "Password must be at least 6 characters", http.StatusBadRequest)
		return
	}

	// Check if username already exists
	var existingID int
	err := db.QueryRow("SELECT id FROM users WHERE username = ?", req.Username).Scan(&existingID)
	if err == nil {
		http.Error(w, "Username already exists", http.StatusConflict)
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	// Insert user
	result, err := db.Exec("INSERT INTO users (username, password, member_id, is_admin) VALUES (?, ?, ?, ?)",
		req.Username, string(hashedPassword), req.MemberID, req.IsAdmin)
	if err != nil {
		http.Error(w, "Failed to create user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "User created successfully",
		"id":      id,
	})
}

// Admin: Update user
func updateAdminUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	var req AdminUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Check if user exists
	var existingUsername string
	err = db.QueryRow("SELECT username FROM users WHERE id = ?", userID).Scan(&existingUsername)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Check if new username already exists (if username is being changed)
	if req.Username != "" && req.Username != existingUsername {
		var otherID int
		err = db.QueryRow("SELECT id FROM users WHERE username = ? AND id != ?", req.Username, userID).Scan(&otherID)
		if err == nil {
			http.Error(w, "Username already exists", http.StatusConflict)
			return
		}
	}

	// Build update query
	if req.Username != "" {
		_, err = db.Exec("UPDATE users SET username = ?, member_id = ?, is_admin = ? WHERE id = ?",
			req.Username, req.MemberID, req.IsAdmin, userID)
	} else {
		_, err = db.Exec("UPDATE users SET member_id = ?, is_admin = ? WHERE id = ?",
			req.MemberID, req.IsAdmin, userID)
	}

	if err != nil {
		http.Error(w, "Failed to update user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "User updated successfully"})
}

// Admin: Delete user
func deleteAdminUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// Check if user exists
	var username string
	err = db.QueryRow("SELECT username FROM users WHERE id = ?", userID).Scan(&username)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Prevent deleting the last admin
	var adminCount int
	err = db.QueryRow("SELECT COUNT(*) FROM users WHERE is_admin = 1").Scan(&adminCount)
	if err == nil && adminCount <= 1 {
		var isAdmin bool
		db.QueryRow("SELECT is_admin FROM users WHERE id = ?", userID).Scan(&isAdmin)
		if isAdmin {
			http.Error(w, "Cannot delete the last admin user", http.StatusForbidden)
			return
		}
	}

	// Delete user
	_, err = db.Exec("DELETE FROM users WHERE id = ?", userID)
	if err != nil {
		http.Error(w, "Failed to delete user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "User deleted successfully"})
}

// Admin: Reset user password
func resetUserPassword(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// Check if user exists
	var username string
	err = db.QueryRow("SELECT username FROM users WHERE id = ?", userID).Scan(&username)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Generate random password
	randomPassword, err := generateRandomPassword(10)
	if err != nil {
		http.Error(w, "Failed to generate password", http.StatusInternalServerError)
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(randomPassword), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	// Update password
	_, err = db.Exec("UPDATE users SET password = ? WHERE id = ?", string(hashedPassword), userID)
	if err != nil {
		http.Error(w, "Failed to reset password: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":  "Password reset successfully",
		"username": username,
		"password": randomPassword,
	})
}

// Admin: Get login history
func getLoginHistory(w http.ResponseWriter, r *http.Request) {
	// Get optional filters from query params
	userIDParam := r.URL.Query().Get("user_id")
	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "100"
	}

	query := `
		SELECT ls.id, ls.user_id, ls.username, ls.ip_address, ls.user_agent, 
		       ls.country, ls.city, ls.isp, ls.login_time, ls.success
		FROM login_sessions ls
	`

	if userIDParam != "" {
		query += " WHERE ls.user_id = " + userIDParam
	}

	query += " ORDER BY ls.login_time DESC LIMIT " + limit

	rows, err := db.Query(query)
	if err != nil {
		http.Error(w, "Failed to fetch login history", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	history := []LoginSession{}
	for rows.Next() {
		var login LoginSession
		var ipAddr, userAgent, country, city, isp sql.NullString

		err := rows.Scan(&login.ID, &login.UserID, &login.Username,
			&ipAddr, &userAgent, &country, &city, &isp,
			&login.LoginTime, &login.Success)
		if err != nil {
			continue
		}

		if ipAddr.Valid {
			login.IPAddress = &ipAddr.String
		}
		if userAgent.Valid {
			login.UserAgent = &userAgent.String
		}
		if country.Valid {
			login.Country = &country.String
		}
		if city.Valid {
			login.City = &city.String
		}
		if isp.Valid {
			login.ISP = &isp.String
		}

		history = append(history, login)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

// Get all members
func getMembers(w http.ResponseWriter, r *http.Request) {
	query := `
        SELECT m.id, m.name, m.rank, COALESCE(m.eligible, 1),
               COALESCE((SELECT power FROM power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), 0) as latest_power,
               COALESCE((SELECT recorded_at FROM power_history WHERE member_id = m.id ORDER BY recorded_at DESC LIMIT 1), '') as latest_power_date,
               EXISTS(SELECT 1 FROM users WHERE member_id = m.id) as has_user
        FROM members m
        ORDER BY m.name
    `
	rows, err := db.Query(query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	members := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.Name, &m.Rank, &m.Eligible, &m.Power, &m.PowerUpdatedAt, &m.HasUser); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		members = append(members, m)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(members)
}

// Get member statistics for train scheduling
func getMemberStats(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT 
			m.id, 
			m.name, 
			m.rank,
			COUNT(DISTINCT CASE WHEN ts.conductor_id = m.id THEN ts.date END) as conductor_count,
			MAX(CASE WHEN ts.conductor_id = m.id THEN ts.date END) as last_conductor_date,
			COUNT(DISTINCT CASE WHEN ts.backup_id = m.id THEN ts.date END) as backup_count,
			COUNT(DISTINCT CASE WHEN ts.backup_id = m.id AND ts.conductor_showed_up = 0 THEN ts.date END) as backup_used_count,
			COUNT(DISTINCT CASE WHEN ts.conductor_id = m.id AND ts.conductor_showed_up = 0 THEN ts.date END) as conductor_no_show_count
		FROM members m
		LEFT JOIN train_schedules ts ON ts.conductor_id = m.id OR ts.backup_id = m.id
		GROUP BY m.id, m.name, m.rank
		ORDER BY m.name
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	stats := []MemberStats{}
	for rows.Next() {
		var s MemberStats
		var lastDate sql.NullString
		if err := rows.Scan(&s.ID, &s.Name, &s.Rank, &s.ConductorCount, &lastDate, &s.BackupCount, &s.BackupUsedCount, &s.ConductorNoShowCount); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if lastDate.Valid {
			s.LastConductorDate = &lastDate.String
		}
		stats = append(stats, s)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// Create a new member
func createMember(w http.ResponseWriter, r *http.Request) {
	var m Member
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Default to eligible if not specified
	if !m.Eligible {
		m.Eligible = true
	}

	result, err := db.Exec("INSERT INTO members (name, rank, eligible) VALUES (?, ?, ?)", m.Name, m.Rank, m.Eligible)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	m.ID = int(id)

	// Simplified for New Members: If power was sent (including 0), log it.
	if m.Power != nil {
		_, insertErr := db.Exec(`INSERT INTO power_history (member_id, power, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, m.ID, *m.Power)
		if insertErr != nil {
			log.Printf("Warning: Failed to log initial power history for member %d: %v", m.ID, insertErr)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(m)
}

// Update a member
func updateMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid member ID", http.StatusBadRequest)
		return
	}

	var m Member
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		log.Printf("JSON Decode Error: %v", err) // <--- ADD THIS
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 1. Update the base member details
	_, err = db.Exec("UPDATE members SET name = ?, rank = ?, eligible = ? WHERE id = ?", m.Name, m.Rank, m.Eligible, id)
	if err != nil {
		log.Printf("DB Update Error: %v", err) // <--- ADD THIS
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. Handle the Power History insertion using pointer dereferencing
	// We only check if m.Power is NOT nil (meaning the field was in the JSON)
	if m.Power != nil {
		var currentPower int64 = -1 // Default to -1 so that 0 is seen as a change

		// Check their most recent recorded power - using _ to ignore the "no rows" error
		_ = db.QueryRow(`SELECT power FROM power_history WHERE member_id = ? ORDER BY recorded_at DESC LIMIT 1`, id).Scan(&currentPower)

		// Compare the dereferenced pointer value (*m.Power) to the database value
		if currentPower != *m.Power {
			_, insertErr := db.Exec(`INSERT INTO power_history (member_id, power, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, id, *m.Power)
			if insertErr != nil {
				log.Printf("Warning: Failed to log power history for member %d: %v", id, insertErr)
			}
		}
	}

	m.ID = id
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(m)
}

// Delete a member
func deleteMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid member ID", http.StatusBadRequest)
		return
	}

	// 1. Clean up linked user accounts so they can no longer log in
	_, err = db.Exec("DELETE FROM users WHERE member_id = ?", id)
	if err != nil {
		log.Printf("Warning: Failed to delete linked user for member %d: %v", id, err)
	}

	// 2. Clean up linked power history so it doesn't clutter the database
	_, err = db.Exec("DELETE FROM power_history WHERE member_id = ?", id)
	if err != nil {
		log.Printf("Warning: Failed to delete power history for member %d: %v", id, err)
	}

	// 3. Delete the actual member
	_, err = db.Exec("DELETE FROM members WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Get train schedules (optionally filtered by date range)
func getTrainSchedules(w http.ResponseWriter, r *http.Request) {
	startDate := r.URL.Query().Get("start")
	endDate := r.URL.Query().Get("end")

	query := `
		SELECT 
			ts.id, ts.date, ts.conductor_id, m1.name as conductor_name,
			ts.conductor_score, ts.backup_id, m2.name as backup_name, m2.rank as backup_rank,
			ts.conductor_showed_up, ts.notes, ts.created_at
		FROM train_schedules ts
		JOIN members m1 ON ts.conductor_id = m1.id
		JOIN members m2 ON ts.backup_id = m2.id
	`

	var rows *sql.Rows
	var err error

	if startDate != "" && endDate != "" {
		query += " WHERE ts.date BETWEEN ? AND ? ORDER BY ts.date, ts.conductor_score DESC"
		rows, err = db.Query(query, startDate, endDate)
	} else {
		query += " ORDER BY ts.date, ts.conductor_score DESC"
		rows, err = db.Query(query)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	schedules := []TrainSchedule{}
	for rows.Next() {
		var ts TrainSchedule
		var showedUp sql.NullBool
		var notes sql.NullString
		var score sql.NullInt64

		if err := rows.Scan(&ts.ID, &ts.Date, &ts.ConductorID, &ts.ConductorName,
			&score, &ts.BackupID, &ts.BackupName, &ts.BackupRank, &showedUp, &notes, &ts.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if showedUp.Valid {
			ts.ConductorShowedUp = &showedUp.Bool
		}
		if notes.Valid {
			ts.Notes = &notes.String
		}
		if score.Valid {
			scoreInt := int(score.Int64)
			ts.ConductorScore = &scoreInt
		}

		schedules = append(schedules, ts)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(schedules)
}

// Create a train schedule
func createTrainSchedule(w http.ResponseWriter, r *http.Request) {
	var ts TrainSchedule
	if err := json.NewDecoder(r.Body).Decode(&ts); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Validate backup is R4 or R5
	var backupRank string
	err := db.QueryRow("SELECT rank FROM members WHERE id = ?", ts.BackupID).Scan(&backupRank)
	if err != nil {
		http.Error(w, "Backup member not found", http.StatusBadRequest)
		return
	}

	if backupRank != "R4" && backupRank != "R5" {
		http.Error(w, "Backup must be an R4 or R5 member", http.StatusBadRequest)
		return
	}

	// Use INSERT OR REPLACE to allow updating schedules created by auto-schedule
	result, err := db.Exec(
		"INSERT OR REPLACE INTO train_schedules (date, conductor_id, backup_id, conductor_score, conductor_showed_up, notes) VALUES (?, ?, ?, ?, ?, ?)",
		ts.Date, ts.ConductorID, ts.BackupID, ts.ConductorScore, ts.ConductorShowedUp, ts.Notes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Awards and recommendations automatically become inactive via on-the-fly calculation

	id, _ := result.LastInsertId()
	ts.ID = int(id)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(ts)
}

// Update a train schedule
func updateTrainSchedule(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid schedule ID", http.StatusBadRequest)
		return
	}

	var ts TrainSchedule
	if err := json.NewDecoder(r.Body).Decode(&ts); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Get existing schedule to check if conductor or backup changed
	var existingConductorID, existingBackupID int
	err = db.QueryRow("SELECT conductor_id, backup_id FROM train_schedules WHERE id = ?", id).Scan(&existingConductorID, &existingBackupID)
	if err != nil {
		http.Error(w, "Schedule not found", http.StatusNotFound)
		return
	}

	// Validate backup is R4 or R5 if backup is being updated
	if ts.BackupID > 0 {
		var backupRank string
		err := db.QueryRow("SELECT rank FROM members WHERE id = ?", ts.BackupID).Scan(&backupRank)
		if err != nil {
			http.Error(w, "Backup member not found", http.StatusBadRequest)
			return
		}

		if backupRank != "R4" && backupRank != "R5" {
			http.Error(w, "Backup must be an R4 or R5 member", http.StatusBadRequest)
			return
		}
	}

	_, err = db.Exec(
		"UPDATE train_schedules SET date = ?, conductor_id = ?, backup_id = ?, conductor_score = ?, conductor_showed_up = ?, notes = ? WHERE id = ?",
		ts.Date, ts.ConductorID, ts.BackupID, ts.ConductorScore, ts.ConductorShowedUp, ts.Notes, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Awards and recommendations automatically become inactive via on-the-fly calculation

	ts.ID = id
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ts)
}

// Delete a train schedule
func deleteTrainSchedule(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid schedule ID", http.StatusBadRequest)
		return
	}

	_, err = db.Exec("DELETE FROM train_schedules WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Auto-schedule train conductors and backups for a single day
func autoSchedule(w http.ResponseWriter, r *http.Request) {
	var input struct {
		StartDate string `json:"start_date"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Parse the date and get the week start (Monday)
	scheduleDate, err := parseDate(input.StartDate)
	if err != nil {
		http.Error(w, "Invalid date format", http.StatusBadRequest)
		return
	}

	weekStart := getMondayOfWeek(scheduleDate)

	// Build ranking context
	ctx, err := buildRankingContext(weekStart)
	if err != nil {
		http.Error(w, "Failed to load ranking context: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get all eligible members
	rows, err := db.Query("SELECT id, name, rank, COALESCE(eligible, 1) FROM members WHERE COALESCE(eligible, 1) = 1 ORDER BY name")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var candidates []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.Name, &m.Rank, &m.Eligible); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		candidates = append(candidates, m)
	}

	if len(candidates) < 7 {
		http.Error(w, "Not enough members for weekly scheduling (need at least 7)", http.StatusBadRequest)
		return
	}

	// Score each candidate using the abstracted ranking system
	type ScoredMember struct {
		Member Member
		Score  int
	}

	var scoredCandidates []ScoredMember
	for _, member := range candidates {
		score := calculateMemberScore(member, ctx)
		scoredCandidates = append(scoredCandidates, ScoredMember{
			Member: member,
			Score:  score,
		})
	}

	// Sort by score (highest first)
	for i := 0; i < len(scoredCandidates); i++ {
		for j := i + 1; j < len(scoredCandidates); j++ {
			if scoredCandidates[j].Score > scoredCandidates[i].Score {
				scoredCandidates[i], scoredCandidates[j] = scoredCandidates[j], scoredCandidates[i]
			}
		}
	}

	// Pre-select top 7 performers as conductors for the week
	plannedConductors := make(map[int]bool)
	for i := 0; i < 7 && i < len(scoredCandidates); i++ {
		plannedConductors[scoredCandidates[i].Member.ID] = true
	}

	// Now schedule each day
	var weekSchedules []TrainSchedule
	usedConductors := make(map[int]bool)
	usedBackups := make(map[int]bool)

	for day := 0; day < 7; day++ {
		currentDate := weekStart.AddDate(0, 0, day)
		dateStr := formatDateString(currentDate)

		// Select conductor from top 7 who hasn't been assigned yet
		var conductorID int
		var conductorScore int

		for _, sc := range scoredCandidates {
			if plannedConductors[sc.Member.ID] && !usedConductors[sc.Member.ID] {
				conductorID = sc.Member.ID
				conductorScore = sc.Score
				usedConductors[sc.Member.ID] = true
				break
			}
		}

		if conductorID == 0 {
			http.Error(w, "Unable to assign conductor for all days", http.StatusInternalServerError)
			return
		}

		// Select backup (must be R4/R5, not a planned conductor, not already used as backup)
		var availableBackups []Member
		for _, sc := range scoredCandidates {
			if !plannedConductors[sc.Member.ID] &&
				!usedBackups[sc.Member.ID] &&
				(sc.Member.Rank == "R4" || sc.Member.Rank == "R5") {
				availableBackups = append(availableBackups, sc.Member)
			}
		}

		var backupID int
		if len(availableBackups) > 0 {
			// Randomly select from available backups
			randomIndex := time.Now().UnixNano() % int64(len(availableBackups))
			backupID = availableBackups[randomIndex].ID
			usedBackups[backupID] = true
		}

		// If backupID is 0, no backup available - continue anyway and allow manual assignment

		// Insert schedule for this day
		var result sql.Result
		if backupID > 0 {
			result, err = db.Exec(
				"INSERT OR REPLACE INTO train_schedules (date, conductor_id, backup_id, conductor_score) VALUES (?, ?, ?, ?)",
				dateStr, conductorID, backupID, conductorScore,
			)
		} else {
			result, err = db.Exec(
				"INSERT OR REPLACE INTO train_schedules (date, conductor_id, backup_id, conductor_score) VALUES (?, ?, NULL, ?)",
				dateStr, conductorID, conductorScore,
			)
		}
		if err != nil {
			http.Error(w, "Failed to create schedule: "+err.Error(), http.StatusInternalServerError)
			return
		}

		scheduleID, _ := result.LastInsertId()

		// Get the full schedule details
		var schedule TrainSchedule
		var score sql.NullInt64
		var backupName sql.NullString
		var backupRank sql.NullString

		if backupID > 0 {
			err = db.QueryRow(`
			SELECT 
				ts.id, ts.date, ts.conductor_id, 
				mc.name, ts.conductor_score, ts.backup_id, mb.name, mb.rank,
				ts.conductor_showed_up, ts.notes, ts.created_at
			FROM train_schedules ts
			JOIN members mc ON ts.conductor_id = mc.id
			LEFT JOIN members mb ON ts.backup_id = mb.id
			WHERE ts.id = ?
		`, scheduleID).Scan(
				&schedule.ID, &schedule.Date, &schedule.ConductorID,
				&schedule.ConductorName, &score, &schedule.BackupID, &backupName,
				&backupRank, &schedule.ConductorShowedUp, &schedule.Notes,
				&schedule.CreatedAt,
			)
		} else {
			err = db.QueryRow(`
			SELECT 
				ts.id, ts.date, ts.conductor_id, 
				mc.name, ts.conductor_score,
				ts.conductor_showed_up, ts.notes, ts.created_at
			FROM train_schedules ts
			JOIN members mc ON ts.conductor_id = mc.id
			WHERE ts.id = ?
		`, scheduleID).Scan(
				&schedule.ID, &schedule.Date, &schedule.ConductorID,
				&schedule.ConductorName, &score,
				&schedule.ConductorShowedUp, &schedule.Notes,
				&schedule.CreatedAt,
			)
			// Set backup fields to empty/zero
			schedule.BackupID = 0
			schedule.BackupName = ""
			schedule.BackupRank = ""
		}

		if err != nil {
			http.Error(w, "Failed to retrieve schedule: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if backupID > 0 {
			if backupName.Valid {
				schedule.BackupName = backupName.String
			}
			if backupRank.Valid {
				schedule.BackupRank = backupRank.String
			}
		}

		if score.Valid {
			scoreInt := int(score.Int64)
			schedule.ConductorScore = &scoreInt
		}

		weekSchedules = append(weekSchedules, schedule)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":   "Week scheduled successfully",
		"schedules": weekSchedules,
	})
}

// Helper function to parse date string
func parseDate(dateStr string) (time.Time, error) {
	return time.Parse("2006-01-02", dateStr)
}

// Helper function to format date to string
func formatDateString(t time.Time) string {
	return t.Format("2006-01-02")
}

// Helper function to get Monday of a week
func getMondayOfWeek(date time.Time) time.Time {
	offset := int(time.Monday - date.Weekday())
	if offset > 0 {
		offset = -6
	}
	return date.AddDate(0, 0, offset)
}

// Import members from CSV
func importCSV(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form (10MB max)
	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		http.Error(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Parse CSV
	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1 // Allow variable number of fields

	records, err := reader.ReadAll()
	if err != nil {
		http.Error(w, "Failed to parse CSV: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(records) == 0 {
		http.Error(w, "CSV file is empty", http.StatusBadRequest)
		return
	}

	// Skip header row if it looks like a header
	startIndex := 0
	if len(records) > 0 {
		firstRow := records[0]
		if len(firstRow) > 0 {
			firstCell := strings.ToLower(strings.TrimSpace(firstRow[0]))
			// Check if first row is a header
			if firstCell == "username" || firstCell == "name" || firstCell == "member" {
				startIndex = 1
			}
		}
	}

	validRanks := map[string]bool{"R1": true, "R2": true, "R3": true, "R4": true, "R5": true}
	detectedMembers := []DetectedMember{}
	errors := []string{}

	// Get existing members
	existingMembers := make(map[string]Member)
	rows, err := db.Query("SELECT id, name, rank FROM members")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var m Member
			rows.Scan(&m.ID, &m.Name, &m.Rank)
			existingMembers[m.Name] = m
		}
	}

	for i := startIndex; i < len(records); i++ {
		record := records[i]

		// New format: Username,Rank,Power,Level,Status,Last_Active
		// We only care about Username (index 0) and Rank (index 1)
		if len(record) < 2 {
			errors = append(errors, fmt.Sprintf("Line %d: Insufficient columns (need at least Username and Rank)", i+1))
			continue
		}

		name := strings.TrimSpace(record[0])
		rank := strings.TrimSpace(record[1])

		if name == "" {
			errors = append(errors, fmt.Sprintf("Line %d: Empty username", i+1))
			continue
		}

		if !validRanks[rank] {
			errors = append(errors, fmt.Sprintf("Line %d: Invalid rank '%s' (must be R1-R5)", i+1, rank))
			continue
		}

		detected := DetectedMember{
			Name: name,
			Rank: rank,
		}

		// Check if member exists
		if existing, found := existingMembers[name]; found {
			// Existing member - check for rank change
			if existing.Rank != rank {
				detected.RankChanged = true
				detected.OldRank = existing.Rank
			}
		} else {
			// New member - check for similar names in existing members
			detected.IsNew = true
			similarNames := []string{}
			for existingName := range existingMembers {
				if areSimilar(name, existingName) {
					similarNames = append(similarNames, existingName)
				}
			}
			if len(similarNames) > 0 {
				detected.SimilarMatch = similarNames
			}
		}

		detectedMembers = append(detectedMembers, detected)
	}

	// Find members that would be removed (in database but not in CSV)
	membersToRemove := []MemberToRemove{}
	csvNames := make(map[string]bool)
	for _, m := range detectedMembers {
		csvNames[m.Name] = true
	}
	for _, existing := range existingMembers {
		if !csvNames[existing.Name] {
			membersToRemove = append(membersToRemove, MemberToRemove{
				ID:   existing.ID,
				Name: existing.Name,
				Rank: existing.Rank,
			})
		}
	}

	// Return preview data
	result := map[string]interface{}{
		"detected_members":  detectedMembers,
		"members_to_remove": membersToRemove,
		"errors":            errors,
		"total_rows":        len(records) - startIndex,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// Confirm CSV import
func confirmMemberUpdates(w http.ResponseWriter, r *http.Request) {
	var request ConfirmRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result := ConfirmResult{}

	// Process renames first
	for _, rename := range request.Renames {
		_, err := db.Exec("UPDATE members SET name = ? WHERE name = ?", rename.NewName, rename.OldName)
		if err != nil {
			log.Printf("Error renaming member %s to %s: %v", rename.OldName, rename.NewName, err)
			continue
		}
		log.Printf("Renamed member %s to %s", rename.OldName, rename.NewName)
	}

	// Create a set of member names from the request
	memberNames := make(map[string]bool)
	for _, member := range request.Members {
		memberNames[member.Name] = true
	}

	for _, member := range request.Members {
		// Check if member exists
		var existingID int
		var existingRank string
		err := db.QueryRow("SELECT id, rank FROM members WHERE name = ?", member.Name).Scan(&existingID, &existingRank)

		if err == sql.ErrNoRows {
			// Add new member
			_, err = db.Exec("INSERT INTO members (name, rank) VALUES (?, ?)", member.Name, member.Rank)
			if err != nil {
				log.Printf("Error adding member %s: %v", member.Name, err)
				continue
			}
			result.Added++
		} else if err == nil {
			// Update existing member if rank changed
			if existingRank != member.Rank {
				_, err = db.Exec("UPDATE members SET rank = ? WHERE id = ?", member.Rank, existingID)
				if err != nil {
					log.Printf("Error updating member %s: %v", member.Name, err)
					continue
				}
				result.Updated++
			} else {
				result.Unchanged++
			}
		}
	}

	// Remove specific members by ID if requested
	if len(request.RemoveMemberIDs) > 0 {
		for _, id := range request.RemoveMemberIDs {
			_, err := db.Exec("DELETE FROM members WHERE id = ?", id)
			if err != nil {
				log.Printf("Error removing member with id %d: %v", id, err)
				continue
			}
			result.Removed++
		}
		log.Printf("Removed %d selected members", result.Removed)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// Get awards for a specific week or all weeks
func getAwards(w http.ResponseWriter, r *http.Request) {
	weekDate := r.URL.Query().Get("week")

	var query string
	var rows *sql.Rows
	var err error

	if weekDate != "" {
		query = `
			SELECT a.id, a.week_date, a.award_type, a.rank, a.member_id, m.name, a.created_at
			FROM awards a
			JOIN members m ON a.member_id = m.id
			WHERE a.week_date = ?
			ORDER BY a.award_type, a.rank
		`
		rows, err = db.Query(query, weekDate)
	} else {
		query = `
			SELECT a.id, a.week_date, a.award_type, a.rank, a.member_id, m.name, a.created_at
			FROM awards a
			JOIN members m ON a.member_id = m.id
			ORDER BY a.week_date DESC, a.award_type, a.rank
		`
		rows, err = db.Query(query)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	awards := []Award{}
	for rows.Next() {
		var a Award
		if err := rows.Scan(&a.ID, &a.WeekDate, &a.AwardType, &a.Rank, &a.MemberID, &a.MemberName, &a.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		awards = append(awards, a)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(awards)
}

// Save awards for a week (bulk operation)
func saveAwards(w http.ResponseWriter, r *http.Request) {
	var data struct {
		WeekDate string `json:"week_date"`
		Awards   []struct {
			AwardType string `json:"award_type"`
			Rank      int    `json:"rank"`
			MemberID  int    `json:"member_id"`
		} `json:"awards"`
	}

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Begin transaction
	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Delete existing awards for this week
	_, err = tx.Exec("DELETE FROM awards WHERE week_date = ?", data.WeekDate)
	if err != nil {
		tx.Rollback()
		http.Error(w, "Failed to clear existing awards", http.StatusInternalServerError)
		return
	}

	// Insert new awards
	for _, award := range data.Awards {
		if award.MemberID > 0 { // Only insert if a member is selected
			_, err = tx.Exec(
				"INSERT INTO awards (week_date, award_type, rank, member_id) VALUES (?, ?, ?, ?)",
				data.WeekDate, award.AwardType, award.Rank, award.MemberID)
			if err != nil {
				tx.Rollback()
				http.Error(w, "Failed to save award", http.StatusInternalServerError)
				return
			}
		}
	}

	// Commit transaction
	err = tx.Commit()
	if err != nil {
		http.Error(w, "Failed to save changes", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Awards saved successfully"})
}

// Delete awards for a specific week
func deleteWeekAwards(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	weekDate := vars["week"]

	_, err := db.Exec("DELETE FROM awards WHERE week_date = ?", weekDate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Get VS points for a week or all weeks
func getVSPoints(w http.ResponseWriter, r *http.Request) {
	weekDate := r.URL.Query().Get("week")

	var query string
	var rows *sql.Rows
	var err error

	if weekDate != "" {
		query = `
			SELECT v.id, v.member_id, v.week_date, v.monday, v.tuesday, v.wednesday, 
			       v.thursday, v.friday, v.saturday, v.created_at, v.updated_at,
			       m.name, m.rank
			FROM vs_points v
			JOIN members m ON v.member_id = m.id
			WHERE v.week_date = ?
			ORDER BY m.name
		`
		rows, err = db.Query(query, weekDate)
	} else {
		query = `
			SELECT v.id, v.member_id, v.week_date, v.monday, v.tuesday, v.wednesday, 
			       v.thursday, v.friday, v.saturday, v.created_at, v.updated_at,
			       m.name, m.rank
			FROM vs_points v
			JOIN members m ON v.member_id = m.id
			ORDER BY v.week_date DESC, m.name
		`
		rows, err = db.Query(query)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	vsPoints := []VSPointsWithMember{}
	for rows.Next() {
		var v VSPointsWithMember
		if err := rows.Scan(&v.ID, &v.MemberID, &v.WeekDate, &v.Monday, &v.Tuesday,
			&v.Wednesday, &v.Thursday, &v.Friday, &v.Saturday, &v.CreatedAt, &v.UpdatedAt,
			&v.MemberName, &v.MemberRank); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		vsPoints = append(vsPoints, v)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(vsPoints)
}

// Save VS points for a week (bulk operation)
func saveVSPoints(w http.ResponseWriter, r *http.Request) {
	var data struct {
		WeekDate string `json:"week_date"`
		Points   []struct {
			MemberID  int `json:"member_id"`
			Monday    int `json:"monday"`
			Tuesday   int `json:"tuesday"`
			Wednesday int `json:"wednesday"`
			Thursday  int `json:"thursday"`
			Friday    int `json:"friday"`
			Saturday  int `json:"saturday"`
		} `json:"points"`
	}

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Begin transaction
	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Upsert VS points for each member
	for _, point := range data.Points {
		// Check if record exists
		var existingID int
		err = tx.QueryRow("SELECT id FROM vs_points WHERE member_id = ? AND week_date = ?",
			point.MemberID, data.WeekDate).Scan(&existingID)

		if err == sql.ErrNoRows {
			// Insert new record
			_, err = tx.Exec(`
				INSERT INTO vs_points (member_id, week_date, monday, tuesday, wednesday, thursday, friday, saturday, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
				point.MemberID, data.WeekDate, point.Monday, point.Tuesday, point.Wednesday,
				point.Thursday, point.Friday, point.Saturday)
		} else if err == nil {
			// Update existing record
			_, err = tx.Exec(`
				UPDATE vs_points 
				SET monday = ?, tuesday = ?, wednesday = ?, thursday = ?, friday = ?, saturday = ?, updated_at = CURRENT_TIMESTAMP
				WHERE member_id = ? AND week_date = ?`,
				point.Monday, point.Tuesday, point.Wednesday, point.Thursday, point.Friday, point.Saturday,
				point.MemberID, data.WeekDate)
		}

		if err != nil {
			tx.Rollback()
			http.Error(w, "Failed to save VS points", http.StatusInternalServerError)
			return
		}
	}

	// Commit transaction
	err = tx.Commit()
	if err != nil {
		http.Error(w, "Failed to save changes", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "VS points saved successfully"})
}

// Delete VS points for a specific week
func deleteWeekVSPoints(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	weekDate := vars["week"]

	_, err := db.Exec("DELETE FROM vs_points WHERE week_date = ?", weekDate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Get all award types
func getAwardTypes(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT id, name, active, sort_order, created_at
		FROM award_types
		ORDER BY sort_order, name
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	awardTypes := []AwardType{}
	for rows.Next() {
		var at AwardType
		if err := rows.Scan(&at.ID, &at.Name, &at.Active, &at.SortOrder, &at.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		awardTypes = append(awardTypes, at)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(awardTypes)
}

// Create a new award type
func createAwardType(w http.ResponseWriter, r *http.Request) {
	var at AwardType
	if err := json.NewDecoder(r.Body).Decode(&at); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Validate name
	if strings.TrimSpace(at.Name) == "" {
		http.Error(w, "Award type name is required", http.StatusBadRequest)
		return
	}

	// Check if award type already exists
	var existingID int
	err := db.QueryRow("SELECT id FROM award_types WHERE name = ?", at.Name).Scan(&existingID)
	if err == nil {
		http.Error(w, "Award type already exists", http.StatusConflict)
		return
	}

	// Get max sort_order and add 1
	var maxOrder int
	err = db.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM award_types").Scan(&maxOrder)
	if err != nil {
		maxOrder = -1
	}

	result, err := db.Exec(
		"INSERT INTO award_types (name, active, sort_order) VALUES (?, ?, ?)",
		at.Name, true, maxOrder+1)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	at.ID = int(id)
	at.Active = true
	at.SortOrder = maxOrder + 1
	at.CreatedAt = time.Now().Format(time.RFC3339)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(at)
}

// Update award type (mainly for active status)
func updateAwardType(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var at AwardType
	if err := json.NewDecoder(r.Body).Decode(&at); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err = db.Exec(
		"UPDATE award_types SET active = ?, name = ? WHERE id = ?",
		at.Active, at.Name, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Award type updated"})
}

// Delete award type (supports force deletion)
func deleteAwardType(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// Check for force parameter
	force := r.URL.Query().Get("force") == "true"

	// Get award type name
	var name string
	err = db.QueryRow("SELECT name FROM award_types WHERE id = ?", id).Scan(&name)
	if err != nil {
		http.Error(w, "Award type not found", http.StatusNotFound)
		return
	}

	if force {
		// Delete all awards of this type first
		_, err = db.Exec("DELETE FROM awards WHERE award_type = ?", name)
		if err != nil {
			http.Error(w, "Failed to delete related awards", http.StatusInternalServerError)
			return
		}
	} else {
		// Check if award type is used in any awards
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM awards WHERE award_type = ?", name).Scan(&count)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if count > 0 {
			http.Error(w, "Cannot delete award type that is used in awards. Use force=true to delete anyway.", http.StatusBadRequest)
			return
		}
	}

	_, err = db.Exec("DELETE FROM award_types WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Get all recommendations
func getRecommendations(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT 
			rec.id, 
			rec.member_id, 
			m.name, 
			m.rank,
			u.username,
			rec.recommended_by_id,
			COALESCE(rec.notes, ''),
			rec.created_at,
			CASE 
				WHEN EXISTS (
					SELECT 1 FROM train_schedules ts
					WHERE (ts.conductor_id = rec.member_id OR (ts.backup_id = rec.member_id AND ts.conductor_showed_up = 0))
					AND ts.date >= date(rec.created_at)
				) THEN 1
				ELSE 0
			END as expired
		FROM recommendations rec
		JOIN members m ON rec.member_id = m.id
		JOIN users u ON rec.recommended_by_id = u.id
		ORDER BY rec.created_at DESC
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	recommendations := []Recommendation{}
	for rows.Next() {
		var rec Recommendation
		if err := rows.Scan(&rec.ID, &rec.MemberID, &rec.MemberName, &rec.MemberRank,
			&rec.RecommendedBy, &rec.RecommendedByID, &rec.Notes, &rec.CreatedAt, &rec.Expired); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		recommendations = append(recommendations, rec)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(recommendations)
}

// Create recommendation
func createRecommendation(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID := session.Values["user_id"].(int)

	var input struct {
		MemberID int    `json:"member_id"`
		Notes    string `json:"notes"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if input.MemberID == 0 {
		http.Error(w, "Member ID is required", http.StatusBadRequest)
		return
	}

	// Check if member exists
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM members WHERE id = ?)", input.MemberID).Scan(&exists)
	if err != nil || !exists {
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}

	result, err := db.Exec(
		"INSERT INTO recommendations (member_id, recommended_by_id, notes) VALUES (?, ?, ?)",
		input.MemberID, userID, input.Notes,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()

	// Get the created recommendation
	var rec Recommendation
	err = db.QueryRow(`
		SELECT 
			rec.id, 
			rec.member_id, 
			m.name, 
			m.rank,
			u.username,
			rec.recommended_by_id,
			COALESCE(rec.notes, ''),
			rec.created_at,
			CASE 
				WHEN EXISTS (
					SELECT 1 FROM train_schedules ts
					WHERE (ts.conductor_id = rec.member_id OR (ts.backup_id = rec.member_id AND ts.conductor_showed_up = 0))
					AND ts.date >= date(rec.created_at)
				) THEN 1
				ELSE 0
			END as expired
		FROM recommendations rec
		JOIN members m ON rec.member_id = m.id
		JOIN users u ON rec.recommended_by_id = u.id
		WHERE rec.id = ?
	`, id).Scan(&rec.ID, &rec.MemberID, &rec.MemberName, &rec.MemberRank,
		&rec.RecommendedBy, &rec.RecommendedByID, &rec.Notes, &rec.CreatedAt, &rec.Expired)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(rec)
}

// Delete recommendation
func deleteRecommendation(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID := session.Values["user_id"].(int)
	isAdmin := session.Values["is_admin"].(bool)

	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// Check if user is the one who created the recommendation or is admin
	var recommendedByID int
	err = db.QueryRow("SELECT recommended_by_id FROM recommendations WHERE id = ?", id).Scan(&recommendedByID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Recommendation not found", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	if recommendedByID != userID && !isAdmin {
		http.Error(w, "You can only delete your own recommendations", http.StatusForbidden)
		return
	}

	_, err = db.Exec("DELETE FROM recommendations WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Get dyno recommendations (expires after 1 week)
func getDynoRecommendations(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT 
			dr.id, 
			dr.member_id, 
			m.name, 
			m.rank,
			dr.points,
			dr.notes,
			u.username,
			dr.created_by_id,
			dr.created_at,
			CASE 
				WHEN datetime(dr.created_at, '+7 days') < datetime('now') THEN 1
				ELSE 0
			END as expired
		FROM dyno_recommendations dr
		JOIN members m ON dr.member_id = m.id
		JOIN users u ON dr.created_by_id = u.id
		ORDER BY dr.created_at DESC
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	dynoRecs := []DynoRecommendation{}
	for rows.Next() {
		var dr DynoRecommendation
		if err := rows.Scan(&dr.ID, &dr.MemberID, &dr.MemberName, &dr.MemberRank,
			&dr.Points, &dr.Notes, &dr.CreatedBy, &dr.CreatedByID, &dr.CreatedAt, &dr.Expired); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		dynoRecs = append(dynoRecs, dr)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dynoRecs)
}

// Create dyno recommendation
func createDynoRecommendation(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID := session.Values["user_id"].(int)

	var input struct {
		MemberID int    `json:"member_id"`
		Points   int    `json:"points"`
		Notes    string `json:"notes"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if input.MemberID == 0 {
		http.Error(w, "Member ID is required", http.StatusBadRequest)
		return
	}

	if input.Notes == "" {
		http.Error(w, "Notes are required", http.StatusBadRequest)
		return
	}

	// Check if member exists
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM members WHERE id = ?)", input.MemberID).Scan(&exists)
	if err != nil || !exists {
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}

	result, err := db.Exec(
		"INSERT INTO dyno_recommendations (member_id, points, notes, created_by_id) VALUES (?, ?, ?, ?)",
		input.MemberID, input.Points, input.Notes, userID,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()

	// Get the created dyno recommendation
	var dr DynoRecommendation
	err = db.QueryRow(`
		SELECT 
			dr.id, 
			dr.member_id, 
			m.name, 
			m.rank,
			dr.points,
			dr.notes,
			u.username,
			dr.created_by_id,
			dr.created_at,
			CASE 
				WHEN datetime(dr.created_at, '+7 days') < datetime('now') THEN 1
				ELSE 0
			END as expired
		FROM dyno_recommendations dr
		JOIN members m ON dr.member_id = m.id
		JOIN users u ON dr.created_by_id = u.id
		WHERE dr.id = ?
	`, id).Scan(&dr.ID, &dr.MemberID, &dr.MemberName, &dr.MemberRank,
		&dr.Points, &dr.Notes, &dr.CreatedBy, &dr.CreatedByID, &dr.CreatedAt, &dr.Expired)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(dr)
}

// Delete dyno recommendation
func deleteDynoRecommendation(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID := session.Values["user_id"].(int)
	isAdmin := session.Values["is_admin"].(bool)

	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// Check if user is the one who created the recommendation or is admin
	var createdByID int
	err = db.QueryRow("SELECT created_by_id FROM dyno_recommendations WHERE id = ?", id).Scan(&createdByID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Dyno recommendation not found", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	if createdByID != userID && !isAdmin {
		http.Error(w, "You can only delete your own dyno recommendations", http.StatusForbidden)
		return
	}

	_, err = db.Exec("DELETE FROM dyno_recommendations WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Get settings
func getSettings(w http.ResponseWriter, r *http.Request) {
	var settings Settings
	err := db.QueryRow(`SELECT id, award_first_points, award_second_points, award_third_points, 
		recommendation_points, recent_conductor_penalty_days, above_average_conductor_penalty, r4r5_rank_boost,
		first_time_conductor_boost, schedule_message_template, daily_message_template, 
		COALESCE(power_tracking_enabled, 0) as power_tracking_enabled, storm_timezones, storm_respect_dst
		FROM settings WHERE id = 1`).Scan(
		&settings.ID,
		&settings.AwardFirstPoints,
		&settings.AwardSecondPoints,
		&settings.AwardThirdPoints,
		&settings.RecommendationPoints,
		&settings.RecentConductorPenaltyDays,
		&settings.AboveAverageConductorPenalty,
		&settings.R4R5RankBoost,
		&settings.FirstTimeConductorBoost,
		&settings.ScheduleMessageTemplate,
		&settings.DailyMessageTemplate,
		&settings.PowerTrackingEnabled,
		&settings.StormTimezones,
		&settings.StormRespectDST,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

// Update settings (admin only)
func updateSettings(w http.ResponseWriter, r *http.Request) {
	var settings Settings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err := db.Exec(`UPDATE settings SET 
		award_first_points = ?, 
		award_second_points = ?, 
		award_third_points = ?, 
		recommendation_points = ?, 
		recent_conductor_penalty_days = ?, 
		above_average_conductor_penalty = ?,
		r4r5_rank_boost = ?,
		first_time_conductor_boost = ?,
		schedule_message_template = ?,
		daily_message_template = ?,
		power_tracking_enabled = ?,
		storm_timezones = ?,
		storm_respect_dst = ?
		WHERE id = 1`,
		settings.AwardFirstPoints,
		settings.AwardSecondPoints,
		settings.AwardThirdPoints,
		settings.RecommendationPoints,
		settings.RecentConductorPenaltyDays,
		settings.AboveAverageConductorPenalty,
		settings.R4R5RankBoost,
		settings.FirstTimeConductorBoost,
		settings.ScheduleMessageTemplate,
		settings.DailyMessageTemplate,
		settings.PowerTrackingEnabled,
		settings.StormTimezones,
		settings.StormRespectDST,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Settings updated successfully"})
}

// Get member rankings with detailed score breakdown
func getMemberRankings(w http.ResponseWriter, r *http.Request) {
	// Always include all awards (active and inactive) - filtering is done on client side

	// Build ranking context using current date
	now := time.Now()
	ctx, err := buildRankingContext(now)
	if err != nil {
		http.Error(w, "Failed to load ranking context: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get all members
	rows, err := db.Query("SELECT id, name, rank FROM members ORDER BY name")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var members []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.Name, &m.Rank); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		members = append(members, m)
	}

	// Load all award details with expired flag
	awardQuery := `
		SELECT 
			a.member_id, 
			a.award_type, 
			a.rank, 
			a.week_date,
			CASE 
				WHEN EXISTS (
					SELECT 1 FROM train_schedules ts
					WHERE (ts.conductor_id = a.member_id OR (ts.backup_id = a.member_id AND ts.conductor_showed_up = 0))
					AND ts.date >= a.week_date
				) THEN 1
				ELSE 0
			END as expired
		FROM awards a
		ORDER BY a.week_date DESC, a.rank ASC
	`

	awardRows, err := db.Query(awardQuery)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer awardRows.Close()

	memberAwards := make(map[int][]AwardDetail)
	for awardRows.Next() {
		var memberID, rank, expired int
		var awardType, weekDate string
		if err := awardRows.Scan(&memberID, &awardType, &rank, &weekDate, &expired); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		points := 0
		switch rank {
		case 1:
			points = ctx.Settings.AwardFirstPoints
		case 2:
			points = ctx.Settings.AwardSecondPoints
		case 3:
			points = ctx.Settings.AwardThirdPoints
		}
		memberAwards[memberID] = append(memberAwards[memberID], AwardDetail{
			AwardType: awardType,
			Rank:      rank,
			Points:    points,
			WeekDate:  weekDate,
			Expired:   expired == 1,
		})
	}

	// Calculate rankings for each member
	var rankings []MemberRanking
	for _, member := range members {
		ranking := MemberRanking{
			Member:         member,
			AwardDetails:   memberAwards[member.ID],
			ConductorCount: 0,
		}

		// Calculate award points
		ranking.AwardPoints = ctx.AwardScoreMap[member.ID]

		// Calculate recommendation points
		recCount := ctx.RecommendationMap[member.ID]
		ranking.RecommendationCount = recCount
		ranking.RecommendationPoints = recCount * ctx.Settings.RecommendationPoints

		// Apply rank boost for R4/R5 members (exponential based on days since last conductor)
		if member.Rank == "R4" || member.Rank == "R5" {
			baseBoost := float64(ctx.Settings.R4R5RankBoost)

			// Calculate days since last conductor/backup duty
			var daysSinceLastDuty int = 0
			if stats, exists := ctx.ConductorStats[member.ID]; exists {
				var mostRecentDate *time.Time

				if stats.LastDate != nil {
					if lastDate, err := parseDate(*stats.LastDate); err == nil {
						mostRecentDate = &lastDate
					}
				}

				// Check if backup usage was more recent
				if stats.LastBackupUsed != nil {
					if backupDate, err := parseDate(*stats.LastBackupUsed); err == nil {
						if mostRecentDate == nil || backupDate.After(*mostRecentDate) {
							mostRecentDate = &backupDate
						}
					}
				}

				if mostRecentDate != nil {
					daysSinceLastDuty = int(now.Sub(*mostRecentDate).Hours() / 24)
				}
			}

			// Exponential formula: base_boost * 2^(days/7)
			multiplier := math.Pow(2, float64(daysSinceLastDuty)/7.0)
			ranking.RankBoost = int(math.Round(baseBoost * multiplier))
		}

		// Apply first time conductor boost if member has never been conductor
		if stats, exists := ctx.ConductorStats[member.ID]; !exists || stats.Count == 0 {
			// Calculate base score without first time boost
			baseScore := ranking.AwardPoints + ranking.RecommendationPoints + ranking.RankBoost
			if baseScore > 0 {
				ranking.FirstTimeConductorBoost = ctx.Settings.FirstTimeConductorBoost
			}
		}

		// Get conductor stats
		if stats, exists := ctx.ConductorStats[member.ID]; exists {
			ranking.ConductorCount = stats.Count
			ranking.LastConductorDate = stats.LastDate

			// Calculate above average penalty
			if float64(stats.Count) > ctx.AvgConductorCount {
				ranking.AboveAveragePenalty = ctx.Settings.AboveAverageConductorPenalty
			}

			// Calculate recent conductor penalty - check both conductor date and backup used date
			var mostRecentDate *time.Time

			if stats.LastDate != nil {
				if lastDate, err := parseDate(*stats.LastDate); err == nil {
					mostRecentDate = &lastDate
				}
			}

			// If they stepped in as backup, check if that's more recent
			if stats.LastBackupUsed != nil {
				if backupDate, err := parseDate(*stats.LastBackupUsed); err == nil {
					if mostRecentDate == nil || backupDate.After(*mostRecentDate) {
						mostRecentDate = &backupDate
					}
				}
			}

			// Apply penalty based on most recent duty (conductor or backup usage)
			if mostRecentDate != nil {
				daysSince := int(now.Sub(*mostRecentDate).Hours() / 24)
				ranking.DaysSinceLastConductor = &daysSince
				penalty := ctx.Settings.RecentConductorPenaltyDays - daysSince
				if penalty > 0 {
					ranking.RecentConductorPenalty = penalty
				}
			}
		}

		// Calculate total score using the same abstracted function
		ranking.TotalScore = calculateMemberScore(member, ctx)

		rankings = append(rankings, ranking)
	}

	// Sort by total score (highest first)
	for i := 0; i < len(rankings); i++ {
		for j := i + 1; j < len(rankings); j++ {
			if rankings[j].TotalScore > rankings[i].TotalScore {
				rankings[i], rankings[j] = rankings[j], rankings[i]
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"rankings":                rankings,
		"settings":                ctx.Settings,
		"average_conductor_count": ctx.AvgConductorCount,
	})
}

// Get member point accumulation timelines over specified months
func getMemberTimelines(w http.ResponseWriter, r *http.Request) {
	// Parse months parameter (default to 3)
	monthsParam := r.URL.Query().Get("months")
	months := 3
	if monthsParam != "" {
		if m, err := strconv.Atoi(monthsParam); err == nil && m > 0 {
			months = m
		}
	}

	// Calculate start date (N months ago from today)
	now := time.Now()
	startDate := now.AddDate(0, -months, 0)

	// Get all members
	memberRows, err := db.Query("SELECT id, name, rank FROM members ORDER BY name")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer memberRows.Close()

	var members []Member
	for memberRows.Next() {
		var m Member
		if err := memberRows.Scan(&m.ID, &m.Name, &m.Rank); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		members = append(members, m)
	}

	// Get settings for point calculations
	var settings Settings
	err = db.QueryRow(`SELECT award_first_points, award_second_points, award_third_points, 
		recommendation_points, r4r5_rank_boost, first_time_conductor_boost,
		recent_conductor_penalty_days, above_average_conductor_penalty 
		FROM settings WHERE id = 1`).Scan(
		&settings.AwardFirstPoints, &settings.AwardSecondPoints,
		&settings.AwardThirdPoints, &settings.RecommendationPoints,
		&settings.R4R5RankBoost, &settings.FirstTimeConductorBoost,
		&settings.RecentConductorPenaltyDays, &settings.AboveAverageConductorPenalty)
	if err != nil {
		http.Error(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Calculate average conductor count for above average penalty
	var totalConductors, memberCount int
	db.QueryRow(`
		SELECT COUNT(DISTINCT conductor_id), 
		       (SELECT COUNT(*) FROM members WHERE eligible = 1)
		FROM train_schedules
	`).Scan(&totalConductors, &memberCount)
	avgConductorCount := 0.0
	if memberCount > 0 {
		avgConductorCount = float64(totalConductors) / float64(memberCount)
	}

	// Build timeline data for each member
	timelines := make(map[int]map[string]interface{})

	for _, member := range members {
		// Get train schedules where member was conductor (to track resets)
		conductorDates := []string{}
		conductorRows, err := db.Query(`
			SELECT date FROM train_schedules 
			WHERE (conductor_id = ? OR (backup_id = ? AND conductor_showed_up = 0))
			AND date >= ?
			ORDER BY date ASC
		`, member.ID, member.ID, formatDateString(startDate))
		if err != nil {
			continue
		}
		for conductorRows.Next() {
			var dateStr string
			if err := conductorRows.Scan(&dateStr); err == nil {
				conductorDates = append(conductorDates, dateStr)
			}
		}
		conductorRows.Close()

		// Get all awards and recommendations since start date (tracked separately)
		type PointEvent struct {
			Date   string
			Awards int
			Recs   int
		}
		eventMap := make(map[string]*PointEvent)

		// Get awards
		awardRows, err := db.Query(`
			SELECT week_date, rank FROM awards 
			WHERE member_id = ? AND week_date >= ?
			ORDER BY week_date ASC
		`, member.ID, formatDateString(startDate))
		if err == nil {
			for awardRows.Next() {
				var weekDate string
				var rank int
				if err := awardRows.Scan(&weekDate, &rank); err == nil {
					points := 0
					switch rank {
					case 1:
						points = settings.AwardFirstPoints
					case 2:
						points = settings.AwardSecondPoints
					case 3:
						points = settings.AwardThirdPoints
					}
					if eventMap[weekDate] == nil {
						eventMap[weekDate] = &PointEvent{Date: weekDate}
					}
					eventMap[weekDate].Awards += points
				}
			}
			awardRows.Close()
		}

		// Get recommendations (calculate non-linear points: 5*sqrt(n))
		recRows, err := db.Query(`
			SELECT DATE(created_at) as rec_date, COUNT(*) as count
			FROM recommendations 
			WHERE member_id = ? AND DATE(created_at) >= ?
			GROUP BY DATE(created_at)
			ORDER BY rec_date ASC
		`, member.ID, formatDateString(startDate))
		if err == nil {
			for recRows.Next() {
				var recDate string
				var count int
				if err := recRows.Scan(&recDate, &count); err == nil {
					// Non-linear formula: 5*sqrt(count)
					points := int(5 * math.Sqrt(float64(count)))
					if eventMap[recDate] == nil {
						eventMap[recDate] = &PointEvent{Date: recDate}
					}
					eventMap[recDate].Recs += points
				}
			}
			recRows.Close()
		}

		// Convert map to sorted slice
		var events []PointEvent
		for _, event := range eventMap {
			events = append(events, *event)
		}
		sort.Slice(events, func(i, j int) bool {
			return events[i].Date < events[j].Date
		})

		// Get power history for this member
		powerHistoryMap := make(map[string]int)
		powerRows, err := db.Query(`
			SELECT DATE(recorded_at) as power_date, 
			       MAX(power) as max_power
			FROM power_history 
			WHERE member_id = ? AND DATE(recorded_at) >= ?
			GROUP BY DATE(recorded_at)
			ORDER BY power_date ASC
		`, member.ID, formatDateString(startDate))
		if err == nil {
			for powerRows.Next() {
				var powerDate string
				var maxPower int
				if err := powerRows.Scan(&powerDate, &maxPower); err == nil {
					powerHistoryMap[powerDate] = maxPower
				}
			}
			powerRows.Close()
		}

		// Build weekly timeline arrays
		weekLabels := []string{}
		pointsWithReset := []int{}
		pointsCumulative := []int{}
		awardsWithReset := []int{}
		awardsCumulative := []int{}
		recsWithReset := []int{}
		recsCumulative := []int{}
		rankBoostWithReset := []int{}
		rankBoostCumulative := []int{}
		firstTimeBoostWithReset := []int{}
		firstTimeBoostCumulative := []int{}
		recentPenaltyWithReset := []int{}
		recentPenaltyCumulative := []int{}
		aboveAvgPenaltyWithReset := []int{}
		aboveAvgPenaltyCumulative := []int{}
		powerValues := []int{}

		currentPoints := 0
		cumulativePoints := 0
		currentAwards := 0
		cumulativeAwards := 0
		currentRecs := 0
		cumulativeRecs := 0
		currentRankBoost := 0
		cumulativeRankBoost := 0
		currentFirstTimeBoost := 0
		cumulativeFirstTimeBoost := 0
		currentRecentPenalty := 0
		cumulativeRecentPenalty := 0
		currentAboveAvgPenalty := 0
		cumulativeAboveAvgPenalty := 0
		conductorIdx := 0
		conductorCountSoFar := 0

		// Generate week range from start to now (by Monday of each week)
		currentDate := getMondayOfWeek(startDate)
		for currentDate.Before(now) || currentDate.Equal(now) {
			weekStart := currentDate
			weekEnd := currentDate.AddDate(0, 0, 6)
			weekStartStr := formatDateString(weekStart)
			weekEndStr := formatDateString(weekEnd)

			// Format: "Jan 1 - Jan 7"
			weekLabel := fmt.Sprintf("%s - %s",
				weekStart.Format("Jan 2"),
				weekEnd.Format("Jan 2"))
			weekLabels = append(weekLabels, weekLabel)

			// Check if this week has a conductor event (train reset)
			weekHasReset := false
			for conductorIdx < len(conductorDates) && conductorDates[conductorIdx] <= weekEndStr {
				if conductorDates[conductorIdx] >= weekStartStr {
					weekHasReset = true
					conductorCountSoFar++
				}
				conductorIdx++
			}

			// Add points from events in this week
			weekAwards := 0
			weekRecs := 0
			for _, event := range events {
				if event.Date >= weekStartStr && event.Date <= weekEndStr {
					weekAwards += event.Awards
					weekRecs += event.Recs
				}
			}
			weekPoints := weekAwards + weekRecs

			currentPoints += weekPoints
			cumulativePoints += weekPoints
			currentAwards += weekAwards
			cumulativeAwards += weekAwards
			currentRecs += weekRecs
			cumulativeRecs += weekRecs

			// Calculate Rank Boost (R4/R5 exponential)
			weekRankBoost := 0
			if member.Rank == "R4" || member.Rank == "R5" {
				baseBoost := float64(settings.R4R5RankBoost)
				// Find most recent conductor date before this week
				daysSinceLastDuty := 0
				for i := conductorIdx - 1; i >= 0; i-- {
					if i < len(conductorDates) {
						if lastConductorDate, err := time.Parse("2006-01-02", conductorDates[i]); err == nil {
							daysSinceLastDuty = int(weekEnd.Sub(lastConductorDate).Hours() / 24)
							break
						}
					}
				}
				multiplier := math.Pow(2, float64(daysSinceLastDuty)/7.0)
				weekRankBoost = int(math.Round(baseBoost * multiplier))
			}
			currentRankBoost += weekRankBoost
			cumulativeRankBoost += weekRankBoost

			// Calculate First Time Conductor Boost
			weekFirstTimeBoost := 0
			if conductorCountSoFar == 0 {
				// Only if they have other points (awards, recs, or rank boost)
				if currentAwards > 0 || currentRecs > 0 || currentRankBoost > 0 {
					weekFirstTimeBoost = settings.FirstTimeConductorBoost
				}
			}
			currentFirstTimeBoost += weekFirstTimeBoost
			cumulativeFirstTimeBoost += weekFirstTimeBoost

			// Calculate Recent Conductor Penalty
			weekRecentPenalty := 0
			if conductorCountSoFar > 0 {
				// Find most recent conductor date
				for i := conductorIdx - 1; i >= 0; i-- {
					if i < len(conductorDates) {
						if lastConductorDate, err := time.Parse("2006-01-02", conductorDates[i]); err == nil {
							daysSince := int(weekEnd.Sub(lastConductorDate).Hours() / 24)
							penalty := settings.RecentConductorPenaltyDays - daysSince
							if penalty > 0 {
								weekRecentPenalty = penalty
							}
							break
						}
					}
				}
			}
			currentRecentPenalty += weekRecentPenalty
			cumulativeRecentPenalty += weekRecentPenalty

			// Calculate Above Average Penalty
			weekAboveAvgPenalty := 0
			if float64(conductorCountSoFar) > avgConductorCount {
				weekAboveAvgPenalty = settings.AboveAverageConductorPenalty
			}
			currentAboveAvgPenalty += weekAboveAvgPenalty
			cumulativeAboveAvgPenalty += weekAboveAvgPenalty

			// Apply reset at end of week if conductor event occurred
			if weekHasReset {
				currentPoints = 0
				currentAwards = 0
				currentRecs = 0
				currentRankBoost = 0
				currentFirstTimeBoost = 0
				currentRecentPenalty = 0
				currentAboveAvgPenalty = 0
			}

			// Find max power value for this week
			weekMaxPower := 0
			for powerDate, power := range powerHistoryMap {
				if powerDate >= weekStartStr && powerDate <= weekEndStr {
					if power > weekMaxPower {
						weekMaxPower = power
					}
				}
			}

			pointsWithReset = append(pointsWithReset, currentPoints)
			pointsCumulative = append(pointsCumulative, cumulativePoints)
			awardsWithReset = append(awardsWithReset, currentAwards)
			awardsCumulative = append(awardsCumulative, cumulativeAwards)
			recsWithReset = append(recsWithReset, currentRecs)
			recsCumulative = append(recsCumulative, cumulativeRecs)
			rankBoostWithReset = append(rankBoostWithReset, currentRankBoost)
			rankBoostCumulative = append(rankBoostCumulative, cumulativeRankBoost)
			firstTimeBoostWithReset = append(firstTimeBoostWithReset, currentFirstTimeBoost)
			firstTimeBoostCumulative = append(firstTimeBoostCumulative, cumulativeFirstTimeBoost)
			recentPenaltyWithReset = append(recentPenaltyWithReset, currentRecentPenalty)
			recentPenaltyCumulative = append(recentPenaltyCumulative, cumulativeRecentPenalty)
			aboveAvgPenaltyWithReset = append(aboveAvgPenaltyWithReset, currentAboveAvgPenalty)
			aboveAvgPenaltyCumulative = append(aboveAvgPenaltyCumulative, cumulativeAboveAvgPenalty)
			powerValues = append(powerValues, weekMaxPower)

			currentDate = currentDate.AddDate(0, 0, 7)
		}

		// Format conductor dates for display (convert to week labels)
		conductorWeekLabels := []string{}
		for _, condDate := range conductorDates {
			if parsedDate, err := time.Parse("2006-01-02", condDate); err == nil {
				monday := getMondayOfWeek(parsedDate)
				weekEnd := monday.AddDate(0, 0, 6)
				weekLabel := fmt.Sprintf("%s - %s",
					monday.Format("Jan 2"),
					weekEnd.Format("Jan 2"))
				conductorWeekLabels = append(conductorWeekLabels, weekLabel)
			}
		}

		timelines[member.ID] = map[string]interface{}{
			"dates":                        weekLabels,
			"points_with_reset":            pointsWithReset,
			"points_cumulative":            pointsCumulative,
			"awards_with_reset":            awardsWithReset,
			"awards_cumulative":            awardsCumulative,
			"recommendations_with_reset":   recsWithReset,
			"recommendations_cumulative":   recsCumulative,
			"rank_boost_with_reset":        rankBoostWithReset,
			"rank_boost_cumulative":        rankBoostCumulative,
			"first_time_boost_with_reset":  firstTimeBoostWithReset,
			"first_time_boost_cumulative":  firstTimeBoostCumulative,
			"recent_penalty_with_reset":    recentPenaltyWithReset,
			"recent_penalty_cumulative":    recentPenaltyCumulative,
			"above_avg_penalty_with_reset": aboveAvgPenaltyWithReset,
			"above_avg_penalty_cumulative": aboveAvgPenaltyCumulative,
			"conductor_dates":              conductorWeekLabels,
			"power":                        powerValues,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(timelines)
}

// Generate weekly schedule message
func generateWeeklyMessage(w http.ResponseWriter, r *http.Request) {
	startDate := r.URL.Query().Get("start")
	if startDate == "" {
		http.Error(w, "start date is required", http.StatusBadRequest)
		return
	}

	// Parse start date
	weekStart, err := parseDate(startDate)
	if err != nil {
		http.Error(w, "Invalid date format", http.StatusBadRequest)
		return
	}

	// Get settings
	settings, err := loadSettings()
	if err != nil {
		http.Error(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get schedules for the week
	weekEnd := weekStart.AddDate(0, 0, 6)
	rows, err := db.Query(`
		SELECT 
			ts.date, m1.name as conductor_name, m2.name as backup_name
		FROM train_schedules ts
		JOIN members m1 ON ts.conductor_id = m1.id
		JOIN members m2 ON ts.backup_id = m2.id
		WHERE ts.date >= ? AND ts.date <= ?
		ORDER BY ts.date
	`, formatDateString(weekStart), formatDateString(weekEnd))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var schedulesText strings.Builder
	for rows.Next() {
		var date, conductor, backup string
		if err := rows.Scan(&date, &conductor, &backup); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Parse date to get day name
		dateObj, _ := parseDate(date)
		dayName := dateObj.Format("Monday")

		schedulesText.WriteString(dayName + ": " + conductor + " (Backup: " + backup + ")\n")
	}

	// Build ranking context to get next 3 candidates
	ctx, err := buildRankingContext(weekStart)
	if err != nil {
		http.Error(w, "Failed to load ranking context: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get all eligible members and score them
	memberRows, err := db.Query("SELECT id, name, rank FROM members WHERE eligible = 1 ORDER BY name")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer memberRows.Close()

	type ScoredMember struct {
		Name  string
		Score int
	}

	var scoredMembers []ScoredMember
	for memberRows.Next() {
		var m Member
		if err := memberRows.Scan(&m.ID, &m.Name, &m.Rank); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		score := calculateMemberScore(m, ctx)
		scoredMembers = append(scoredMembers, ScoredMember{
			Name:  m.Name,
			Score: score,
		})
	}

	// Sort by score (highest first)
	for i := 0; i < len(scoredMembers); i++ {
		for j := i + 1; j < len(scoredMembers); j++ {
			if scoredMembers[j].Score > scoredMembers[i].Score {
				scoredMembers[i], scoredMembers[j] = scoredMembers[j], scoredMembers[i]
			}
		}
	}

	// Get top 3
	var next3Text strings.Builder
	limit := 3
	if len(scoredMembers) < 3 {
		limit = len(scoredMembers)
	}
	for i := 0; i < limit; i++ {
		next3Text.WriteString(scoredMembers[i].Name + "\n")
	}

	// Format the message using template
	message := settings.ScheduleMessageTemplate
	message = strings.ReplaceAll(message, "{WEEK}", weekStart.Format("Jan 2, 2006"))
	message = strings.ReplaceAll(message, "{SCHEDULES}", schedulesText.String())
	message = strings.ReplaceAll(message, "{NEXT_3}", next3Text.String())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": message,
	})
}

// Generate daily message with conductor and backup for a specific date
func generateDailyMessage(w http.ResponseWriter, r *http.Request) {
	dateParam := r.URL.Query().Get("date")
	if dateParam == "" {
		http.Error(w, "date is required", http.StatusBadRequest)
		return
	}

	// Parse date
	date, err := parseDate(dateParam)
	if err != nil {
		http.Error(w, "Invalid date format", http.StatusBadRequest)
		return
	}

	// Get settings
	settings, err := loadSettings()
	if err != nil {
		http.Error(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get schedule for the specific date
	var conductorName, conductorRank, backupName, backupRank string
	err = db.QueryRow(`
		SELECT 
			m1.name as conductor_name, m1.rank as conductor_rank,
			m2.name as backup_name, m2.rank as backup_rank
		FROM train_schedules ts
		JOIN members m1 ON ts.conductor_id = m1.id
		JOIN members m2 ON ts.backup_id = m2.id
		WHERE ts.date = ?
	`, formatDateString(date)).Scan(&conductorName, &conductorRank, &backupName, &backupRank)

	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			http.Error(w, "No schedule found for this date", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Format the message using template
	message := settings.DailyMessageTemplate
	message = strings.ReplaceAll(message, "{DATE}", date.Format("Monday, Jan 2, 2006"))
	message = strings.ReplaceAll(message, "{CONDUCTOR_NAME}", conductorName)
	message = strings.ReplaceAll(message, "{CONDUCTOR_RANK}", conductorRank)
	message = strings.ReplaceAll(message, "{BACKUP_NAME}", backupName)
	message = strings.ReplaceAll(message, "{BACKUP_RANK}", backupRank)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": message,
	})
}

// Generate individual conductor reminder messages for the week
func generateConductorMessages(w http.ResponseWriter, r *http.Request) {
	startDate := r.URL.Query().Get("start")
	if startDate == "" {
		http.Error(w, "start date is required", http.StatusBadRequest)
		return
	}

	// Parse start date
	weekStart, err := parseDate(startDate)
	if err != nil {
		http.Error(w, "Invalid date format", http.StatusBadRequest)
		return
	}

	// Get schedules for the week
	weekEnd := weekStart.AddDate(0, 0, 6)
	rows, err := db.Query(`
		SELECT 
			ts.date, m1.name as conductor_name
		FROM train_schedules ts
		JOIN members m1 ON ts.conductor_id = m1.id
		WHERE ts.date >= ? AND ts.date <= ?
		ORDER BY ts.date
	`, formatDateString(weekStart), formatDateString(weekEnd))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Message variations for natural variety
	messageTemplates := []string{
		"Hi {NAME}! Just a reminder that you're the train conductor on {DAY}, {DATE}. Please be online around 15:00 ST / 17:00 UK / 18:00 CET and ask in alliance chat for the train to be assigned to you. If anything comes up, let us know early so we can coordinate with the backup. Please add a reminder in your phone so you don't forget. Thanks for helping keep the train golden!",
		"Hi {NAME}! You're scheduled as train conductor on {DAY}, {DATE}. Please be online at 15:00 ST / 17:00 UK / 18:00 CET and request the train in alliance chat. If your schedule changes, let us know in advance so we can coordinate with the backup. Add a reminder in your phone to make sure you're on time. Appreciate your support!",
		"Hi {NAME}! Just a heads-up that you're the train conductor on {DAY}, {DATE}. Please be online around 15:00 ST / 17:00 UK / 18:00 CET and ask for the train in alliance chat. If you need help or need to swap, reach out early. Set a phone reminder so you don't miss it. Thanks a lot!",
		"Hi {NAME}! You're assigned as train conductor on {DAY}, {DATE}. Please be online at 15:00 ST / 17:00 UK / 18:00 CET and request the train in alliance chat. If there are any timing issues, let us know so we can plan with the backup. Don't forget to add a reminder in your phone. Thanks for stepping up!",
		"Hi {NAME}! Reminder that you're the train conductor on {DAY}, {DATE}. Please be online around 15:00 ST / 17:00 UK / 18:00 CET and ask in alliance chat for the train assignment. Let us know early if anything changes. Make sure to set a phone reminder. Much appreciated!",
		"Hi {NAME}! You're scheduled as train conductor on {DAY}, {DATE}. Please be online at 15:00 ST / 17:00 UK / 18:00 CET and request the train in alliance chat. If you need assistance or a timing adjustment, just let us know. Add a phone reminder to help you remember. Thanks!",
		"Hi {NAME}! Just a reminder that you're the train conductor on {DAY}, {DATE}. Please be online around 15:00 ST / 17:00 UK / 18:00 CET and ask in alliance chat for the train to be assigned. If anything comes up, please reach out early. Set a reminder in your phone so you're prepared. Thanks for helping the alliance!",
	}

	type DayMessage struct {
		Day     string `json:"day"`
		Name    string `json:"name"`
		Message string `json:"message"`
	}

	var messages []DayMessage
	templateIndex := 0

	for rows.Next() {
		var date, conductor string
		if err := rows.Scan(&date, &conductor); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Parse date to get day name and formatted date
		dateObj, _ := parseDate(date)
		dayName := dateObj.Format("Monday")
		dateFormatted := dateObj.Format("2 Jan") // e.g., "3 Jan" - unambiguous date format

		// Get template and cycle through them
		template := messageTemplates[templateIndex]
		templateIndex = (templateIndex + 1) % len(messageTemplates)

		// Replace placeholders
		message := strings.ReplaceAll(template, "{NAME}", conductor)
		message = strings.ReplaceAll(message, "{DAY}", dayName)
		message = strings.ReplaceAll(message, "{DATE}", dateFormatted)

		messages = append(messages, DayMessage{
			Day:     dayName,
			Name:    conductor,
			Message: message,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": messages,
	})
}

// R4/R5/Admin middleware - checks if user has R4, R5 rank or is admin
func r4r5Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		memberID, ok := session.Values["member_id"].(int)
		if !ok {
			http.Error(w, "Not authenticated", http.StatusUnauthorized)
			return
		}

		// Check if user is admin
		var isAdmin bool
		err := db.QueryRow("SELECT is_admin FROM users WHERE member_id = ?", memberID).Scan(&isAdmin)
		if err == nil && isAdmin {
			next(w, r)
			return
		}

		// Get member rank
		var rank string
		err = db.QueryRow("SELECT rank FROM members WHERE id = ?", memberID).Scan(&rank)
		if err != nil {
			http.Error(w, "Member not found", http.StatusNotFound)
			return
		}

		if rank != "R4" && rank != "R5" {
			http.Error(w, "Access denied - R4, R5 rank or admin privileges required", http.StatusForbidden)
			return
		}

		next(w, r)
	}
}

// Get storm assignments
func getStormAssignments(w http.ResponseWriter, r *http.Request) {
	taskForce := r.URL.Query().Get("task_force")
	if taskForce == "" {
		taskForce = "A"
	}

	rows, err := db.Query(`
		SELECT id, task_force, building_id, member_id, position
		FROM storm_assignments
		WHERE task_force = ?
		ORDER BY building_id, position
	`, taskForce)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	assignments := []StormAssignment{}
	for rows.Next() {
		var a StormAssignment
		if err := rows.Scan(&a.ID, &a.TaskForce, &a.BuildingID, &a.MemberID, &a.Position); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		assignments = append(assignments, a)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(assignments)
}

// Save storm assignments
func saveStormAssignments(w http.ResponseWriter, r *http.Request) {
	var request struct {
		TaskForce   string `json:"task_force"`
		Assignments []struct {
			BuildingID string `json:"building_id"`
			MemberID   int    `json:"member_id"`
			Position   int    `json:"position"`
		} `json:"assignments"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Validate task force
	if request.TaskForce != "A" && request.TaskForce != "B" {
		http.Error(w, "Invalid task force - must be A or B", http.StatusBadRequest)
		return
	}

	// Start transaction
	tx, err := db.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Delete existing assignments for this task force
	_, err = tx.Exec("DELETE FROM storm_assignments WHERE task_force = ?", request.TaskForce)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Insert new assignments
	for _, assignment := range request.Assignments {
		_, err = tx.Exec(`
			INSERT INTO storm_assignments (task_force, building_id, member_id, position)
			VALUES (?, ?, ?, ?)
		`, request.TaskForce, assignment.BuildingID, assignment.MemberID, assignment.Position)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Assignments saved successfully",
	})
}

// Delete storm assignments for a task force
func deleteStormAssignments(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	taskForce := vars["taskForce"]

	if taskForce != "A" && taskForce != "B" {
		http.Error(w, "Invalid task force - must be A or B", http.StatusBadRequest)
		return
	}

	_, err := db.Exec("DELETE FROM storm_assignments WHERE task_force = ?", taskForce)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Get power history for a specific member or all members
func getPowerHistory(w http.ResponseWriter, r *http.Request) {
	memberID := r.URL.Query().Get("member_id")
	limit := r.URL.Query().Get("limit")

	if limit == "" {
		limit = "30" // Default to last 30 records
	}

	var rows *sql.Rows
	var err error

	if memberID != "" {
		rows, err = db.Query(`
			SELECT ph.id, ph.member_id, ph.power, ph.recorded_at
			FROM power_history ph
			WHERE ph.member_id = ?
			ORDER BY ph.recorded_at DESC
			LIMIT ?
		`, memberID, limit)
	} else {
		rows, err = db.Query(`
			SELECT ph.id, ph.member_id, ph.power, ph.recorded_at
			FROM power_history ph
			ORDER BY ph.recorded_at DESC
			LIMIT ?
		`, limit)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	history := []PowerHistory{}
	for rows.Next() {
		var ph PowerHistory
		if err := rows.Scan(&ph.ID, &ph.MemberID, &ph.Power, &ph.RecordedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		history = append(history, ph)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

// Add power record manually
func addPowerRecord(w http.ResponseWriter, r *http.Request) {
	var request struct {
		MemberID int   `json:"member_id"`
		Power    int64 `json:"power"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Check if member exists
	var exists int
	err := db.QueryRow("SELECT COUNT(*) FROM members WHERE id = ?", request.MemberID).Scan(&exists)
	if err != nil || exists == 0 {
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}

	result, err := db.Exec("INSERT INTO power_history (member_id, power) VALUES (?, ?)",
		request.MemberID, request.Power)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Power record added successfully",
		"id":      id,
	})
}

// ImageRegion represents a detected region in the screenshot
type ImageRegion struct {
	Name   string
	Top    int
	Bottom int
	Left   int
	Right  int
}

// ScreenshotAttributes contains analyzed attributes from the screenshot
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

// Analyze screenshot to detect distinct regions and attributes
func analyzeScreenshot(img image.Image) *ScreenshotAttributes {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	attrs := &ScreenshotAttributes{
		Width:  width,
		Height: height,
	}

	// Detect dark title bar at top (typically 5-10% of height)
	// Title bars are usually dark colored
	titleBarHeight := height / 15 // ~6-7%
	if titleBarHeight < 30 {
		titleBarHeight = 30
	}
	attrs.TitleBarRegion = &ImageRegion{
		Name:   "TitleBar",
		Top:    0,
		Bottom: titleBarHeight,
		Left:   0,
		Right:  width,
	}

	// Detect tabs region (typically right below title bar, ~5-8% of height)
	tabsHeight := height / 15
	if tabsHeight < 40 {
		tabsHeight = 40
	}
	attrs.TabsRegion = &ImageRegion{
		Name:   "Tabs",
		Top:    titleBarHeight,
		Bottom: titleBarHeight + tabsHeight,
		Left:   0,
		Right:  width,
	}

	// Detect column headers (below tabs, ~5% of height)
	headerHeight := height / 20
	if headerHeight < 30 {
		headerHeight = 30
	}
	haederTop := titleBarHeight + tabsHeight
	attrs.HeaderRegion = &ImageRegion{
		Name:   "Headers",
		Top:    haederTop,
		Bottom: haederTop + headerHeight,
		Left:   0,
		Right:  width,
	}

	// Detect bottom button region (typically last 8-10% of height)
	buttonHeight := height / 10
	if buttonHeight < 50 {
		buttonHeight = 50
	}
	attrs.ButtonRegion = &ImageRegion{
		Name:   "BottomButton",
		Top:    height - buttonHeight,
		Bottom: height,
		Left:   0,
		Right:  width,
	}

	// Data region is everything between headers and bottom button
	dataTop := haederTop + headerHeight
	dataBottom := height - buttonHeight
	attrs.DataRegion = &ImageRegion{
		Name:   "DataRows",
		Top:    dataTop,
		Bottom: dataBottom,
		Left:   0,
		Right:  width,
	}

	// Estimate row height and count
	dataHeight := dataBottom - dataTop
	attrs.RowHeight = dataHeight / 10 // Assume ~10 visible rows
	if attrs.RowHeight < 40 {
		attrs.RowHeight = 40
	}
	attrs.EstimatedRows = dataHeight / attrs.RowHeight

	log.Printf("Screenshot Analysis: %dx%d, DataRegion: (%d,%d) to (%d,%d), Est. Rows: %d",
		width, height, attrs.DataRegion.Left, attrs.DataRegion.Top,
		attrs.DataRegion.Right, attrs.DataRegion.Bottom, attrs.EstimatedRows)

	return attrs
}

// Crop image to data region only
func cropToDataRegion(img image.Image, region *ImageRegion) image.Image {
	bounds := img.Bounds()
	top := region.Top
	bottom := region.Bottom
	left := region.Left
	right := region.Right

	// Ensure bounds are valid
	if top < bounds.Min.Y {
		top = bounds.Min.Y
	}
	if bottom > bounds.Max.Y {
		bottom = bounds.Max.Y
	}
	if left < bounds.Min.X {
		left = bounds.Min.X
	}
	if right > bounds.Max.X {
		right = bounds.Max.X
	}

	croppedImg := image.NewRGBA(image.Rect(0, 0, right-left, bottom-top))
	draw.Draw(croppedImg, croppedImg.Bounds(), img, image.Point{left, top}, draw.Src)

	log.Printf("Cropped image from %v to %v", bounds, croppedImg.Bounds())
	return croppedImg
}

// Convert image to grayscale
func convertToGrayscale(img image.Image) *image.Gray {
	bounds := img.Bounds()
	gray := image.NewGray(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gray.Set(x, y, img.At(x, y))
		}
	}

	return gray
}

// Enhance contrast using histogram equalization (simplified)
func enhanceContrast(img *image.Gray) *image.Gray {
	bounds := img.Bounds()
	enhanced := image.NewGray(bounds)

	// Calculate histogram
	histogram := make([]int, 256)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			grayVal := img.GrayAt(x, y).Y
			histogram[grayVal]++
		}
	}

	// Calculate cumulative distribution
	totalPixels := bounds.Dx() * bounds.Dy()
	cdf := make([]float64, 256)
	cdf[0] = float64(histogram[0]) / float64(totalPixels)
	for i := 1; i < 256; i++ {
		cdf[i] = cdf[i-1] + float64(histogram[i])/float64(totalPixels)
	}

	// Apply equalization
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			grayVal := img.GrayAt(x, y).Y
			newVal := uint8(cdf[grayVal] * 255)
			enhanced.SetGray(x, y, color.Gray{Y: newVal})
		}
	}

	return enhanced
}

// Apply adaptive thresholding to enhance text
func applyAdaptiveThreshold(img *image.Gray, blockSize int) *image.Gray {
	bounds := img.Bounds()
	thresholded := image.NewGray(bounds)

	halfBlock := blockSize / 2

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			// Calculate local mean in block
			sum := 0
			count := 0
			for by := y - halfBlock; by <= y+halfBlock; by++ {
				for bx := x - halfBlock; bx <= x+halfBlock; bx++ {
					if bx >= bounds.Min.X && bx < bounds.Max.X && by >= bounds.Min.Y && by < bounds.Max.Y {
						sum += int(img.GrayAt(bx, by).Y)
						count++
					}
				}
			}
			mean := uint8(sum / count)

			// Threshold: if pixel is darker than local mean, make it black, else white
			pixel := img.GrayAt(x, y).Y
			if pixel < mean-10 { // -10 for bias towards text
				thresholded.SetGray(x, y, color.Gray{Y: 0}) // Black (text)
			} else {
				thresholded.SetGray(x, y, color.Gray{Y: 255}) // White (background)
			}
		}
	}

	return thresholded
}

// Invert image (make text black on white background)
func invertImage(img *image.Gray) *image.Gray {
	bounds := img.Bounds()
	inverted := image.NewGray(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			grayVal := img.GrayAt(x, y).Y
			inverted.SetGray(x, y, color.Gray{Y: 255 - grayVal})
		}
	}

	return inverted
}

// Scale image up for better OCR
func scaleImage(img image.Image, factor int) image.Image {
	bounds := img.Bounds()
	newWidth := bounds.Dx() * factor
	newHeight := bounds.Dy() * factor

	scaled := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

	// Simple nearest-neighbor scaling
	for y := 0; y < newHeight; y++ {
		for x := 0; x < newWidth; x++ {
			origX := x / factor
			origY := y / factor
			scaled.Set(x, y, img.At(origX, origY))
		}
	}

	return scaled
}

// Preprocess image for better OCR
func preprocessImageForOCR(imageData []byte) ([]byte, error) {
	// Decode image
	img, format, err := image.Decode(bytes.NewReader(imageData))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %v", err)
	}

	log.Printf("Original image: %dx%d, format: %s", img.Bounds().Dx(), img.Bounds().Dy(), format)

	// Analyze screenshot to detect regions
	attrs := analyzeScreenshot(img)

	// Crop to data region only (remove UI elements)
	// For narrow screenshots or when power values might be cut off, use less aggressive cropping
	croppedImg := img
	if attrs.DataRegion != nil && attrs.Width > 600 {
		croppedImg = cropToDataRegion(img, attrs.DataRegion)
	} else {
		log.Printf("Skipping crop for narrow image to preserve power values")
	}

	// Scale up 2x for better OCR (small text is hard to read)
	scaledImg := scaleImage(croppedImg, 2)

	// Convert to grayscale
	grayImg := convertToGrayscale(scaledImg)

	// For small images, skip contrast enhancement (can make things worse)
	processedImg := grayImg

	// Encode back to bytes
	var buf bytes.Buffer
	if err := png.Encode(&buf, processedImg); err != nil {
		return nil, fmt.Errorf("failed to encode processed image: %v", err)
	}

	log.Printf("Image preprocessed: %dx%d -> %dx%d (2x scaled grayscale)",
		img.Bounds().Dx(), img.Bounds().Dy(),
		processedImg.Bounds().Dx(), processedImg.Bounds().Dy())

	return buf.Bytes(), nil
}

// Extract power data from image using OCR with preprocessing
func extractPowerDataFromImage(imageData []byte) ([]struct {
	MemberName string `json:"member_name"`
	Power      int64  `json:"power"`
}, error) {
	// Preprocess image to filter and enhance relevant regions
	processedData, err := preprocessImageForOCR(imageData)
	if err != nil {
		log.Printf("Warning: Image preprocessing failed: %v. Using original image.", err)
		processedData = imageData // Fallback to original
	}

	client := gosseract.NewClient()
	defer client.Close()

	err = client.SetImageFromBytes(processedData)
	if err != nil {
		return nil, fmt.Errorf("failed to load image: %v", err)
	}

	// Try different PSM modes for better recognition
	var text string
	psmModes := []gosseract.PageSegMode{
		gosseract.PSM_AUTO,
		gosseract.PSM_SINGLE_BLOCK,
		gosseract.PSM_SPARSE_TEXT,
	}

	for i, mode := range psmModes {
		client.SetPageSegMode(mode)
		extractedText, err := client.Text()
		if err == nil && len(strings.TrimSpace(extractedText)) > 0 {
			text = extractedText
			log.Printf("OCR successful with PSM mode %d (attempt %d)", mode, i+1)
			break
		}
		log.Printf("OCR attempt %d with PSM mode %d failed or empty", i+1, mode)
	}

	if len(strings.TrimSpace(text)) == 0 {
		return nil, fmt.Errorf("OCR failed: no text extracted after trying multiple modes")
	}

	// Log the extracted text for debugging
	log.Printf("OCR extracted text:\n%s\n---END OCR---", text)

	// Parse the OCR text
	records := parsePowerRankingsText(text)

	if len(records) == 0 {
		return nil, fmt.Errorf("no valid records found in extracted text (see server logs for OCR output)")
	}

	return records, nil
}

// Parse power rankings text (from OCR or manual input)
func parsePowerRankingsText(text string) []struct {
	MemberName string `json:"member_name"`
	Power      int64  `json:"power"`
} {
	var records []struct {
		MemberName string `json:"member_name"`
		Power      int64  `json:"power"`
	}

	lines := strings.Split(text, "\n")

	// Pattern specifically for Last War rankings format
	// Matches: optional rank badge (R4, R3), name (can have spaces), then large power number
	// Examples: "R4 Gary6126 77421000", "Nutty Tx 61926102", "R3 DYNOSUR 63785308"
	// Updated to better handle multi-word names
	rankPattern := regexp.MustCompile(`(?:R[0-9]\s+)?([A-Za-z][A-Za-z0-9_\s]+?)\s+([0-9]{7,})`)

	// Alternative simpler pattern: captures name with spaces followed by 7+ digit number
	simplePattern := regexp.MustCompile(`([A-Za-z][A-Za-z0-9_\s]+?)\s+([0-9]{7,})`)

	// Pattern for lines with rank number prefix: "1 Gary6126 R4 77421000" or "1 ileesu R4 66715876"
	rankPrefixPattern := regexp.MustCompile(`^[0-9]{1,3}\s+([A-Za-z][A-Za-z0-9_\s]+?)\s+(?:R[0-9]\s+)?([0-9]{7,})`)

	// Flexible pattern that allows letters in power (for OCR errors): "B 25) Nutty Tx s1926102"
	// This captures name followed by 7+ chars that may contain letters misread as digits
	flexiblePattern := regexp.MustCompile(`(?:[A-Z]{1,3}\s+)?(?:\d+\)?\s+)?([A-Za-z][A-Za-z0-9_\s]+?)\s+([A-Za-z0-9]{7,})`)

	// Track seen names to avoid duplicates from multi-line OCR
	seenNames := make(map[string]bool)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Skip lines that are clearly UI elements or rank numbers
		if len(line) < 5 || regexp.MustCompile(`^[0-9]{1,2}$`).MatchString(line) {
			continue
		}

		// Skip common UI text
		lowerLine := strings.ToLower(line)
		if strings.Contains(lowerLine, "ranking") ||
			strings.Contains(lowerLine, "commander") ||
			strings.Contains(lowerLine, "power") ||
			strings.Contains(lowerLine, "kills") ||
			strings.Contains(lowerLine, "donation") {
			continue
		}

		// Try rank pattern first (for lines with R4, R3, etc.)
		matches := rankPattern.FindStringSubmatch(line)
		if len(matches) == 0 {
			// Try pattern with rank number prefix
			matches = rankPrefixPattern.FindStringSubmatch(line)
		}
		if len(matches) == 0 {
			// Try simple pattern
			matches = simplePattern.FindStringSubmatch(line)
		}
		if len(matches) == 0 {
			// Try flexible pattern (allows letters in power number for OCR errors)
			matches = flexiblePattern.FindStringSubmatch(line)
		}

		if len(matches) >= 3 {
			name := strings.TrimSpace(matches[1])
			// Clean up extra whitespace in names
			name = regexp.MustCompile(`\s+`).ReplaceAllString(name, " ")

			powerStr := strings.ReplaceAll(matches[2], ",", "")
			powerStr = strings.ReplaceAll(powerStr, " ", "")
			powerStr = strings.ReplaceAll(powerStr, ".", "") // Remove periods OCR might insert
			// Common OCR character misreads for digits
			powerStr = strings.ReplaceAll(powerStr, "O", "0")
			powerStr = strings.ReplaceAll(powerStr, "o", "0")
			powerStr = strings.ReplaceAll(powerStr, "s", "6") // s often misread as 6
			powerStr = strings.ReplaceAll(powerStr, "S", "5") // S often misread as 5
			powerStr = strings.ReplaceAll(powerStr, "l", "1") // l often misread as 1
			powerStr = strings.ReplaceAll(powerStr, "I", "1") // I often misread as 1
			powerStr = strings.ReplaceAll(powerStr, "Z", "2") // Z sometimes misread as 2
			powerStr = strings.ReplaceAll(powerStr, "B", "8") // B sometimes misread as 8
			powerStr = strings.ReplaceAll(powerStr, "e", "6") // e sometimes misread as 6
			powerStr = strings.ReplaceAll(powerStr, "g", "9") // g sometimes misread as 9
			powerStr = strings.ReplaceAll(powerStr, "G", "6") // G sometimes misread as 6
			// Remove any remaining non-digit characters
			powerStr = regexp.MustCompile(`[^0-9]`).ReplaceAllString(powerStr, "")

			power, err := strconv.ParseInt(powerStr, 10, 64)

			// Validate: power should be realistic (1M to 1B range), name should be reasonable
			if err == nil && power >= 1000000 && power <= 9999999999 &&
				len(name) >= 3 && len(name) <= 30 && !seenNames[name] {
				records = append(records, struct {
					MemberName string `json:"member_name"`
					Power      int64  `json:"power"`
				}{
					MemberName: name,
					Power:      power,
				})
				seenNames[name] = true
				log.Printf("Parsed: %s -> %d", name, power)
			} else if err != nil {
				log.Printf("Failed to parse power for %s: %s (error: %v)", name, powerStr, err)
			}
		}
	}

	return records
}

// Normalize name for matching (remove common prefixes, spaces, special chars)
func normalizeName(name string) string {
	name = strings.ToLower(name)
	// Remove common prefixes
	name = strings.TrimPrefix(name, "the ")
	name = strings.TrimPrefix(name, "a ")
	name = strings.TrimPrefix(name, "an ")
	// Remove spaces and special characters
	name = strings.ReplaceAll(name, " ", "")
	name = strings.ReplaceAll(name, "_", "")
	name = strings.ReplaceAll(name, "-", "")
	return name
}

// Calculate string similarity (0-100) using improved algorithm
func calculateSimilarity(s1, s2 string) int {
	// Normalize both strings
	n1 := normalizeName(s1)
	n2 := normalizeName(s2)

	// If normalized strings are identical, perfect match
	if n1 == n2 {
		return 100
	}

	// If one contains the other after normalization, very high score
	if strings.Contains(n1, n2) || strings.Contains(n2, n1) {
		return 90
	}

	// Calculate Levenshtein distance using existing function
	distance := levenshteinDistance(n1, n2)
	maxLen := len(n1)
	if len(n2) > maxLen {
		maxLen = len(n2)
	}

	if maxLen == 0 {
		return 0
	}

	// Convert distance to similarity percentage
	similarity := ((maxLen - distance) * 100) / maxLen

	return similarity
}

// Detect selected day tab by color (white/light background indicates selected tab)
func detectDayByColor(img image.Image) string {
	bounds := img.Bounds()
	width := bounds.Dx()

	// Days are arranged horizontally: Mon, Tues, Wed, Thur, Fri, Sat
	days := []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	tabWidth := width / 6 // Each tab takes ~1/6 of the width

	// Count white/light pixels in each tab region
	// Selected tab has white background, unselected tabs are gray
	lightCounts := make([]int, 6)

	for dayIdx := 0; dayIdx < 6; dayIdx++ {
		// Define the region for this day tab
		startX := dayIdx * tabWidth
		endX := startX + tabWidth
		if dayIdx == 5 {
			endX = width // Last tab goes to the end
		}

		// Sample the center 70% of each tab to avoid edge overlap issues
		// This prevents bleeding from adjacent tabs
		tabCenter := startX + tabWidth/2
		sampleWidth := int(float64(tabWidth) * 0.70)
		sampleStartX := tabCenter - sampleWidth/2
		sampleEndX := tabCenter + sampleWidth/2

		// Ensure we stay within bounds
		if sampleStartX < startX {
			sampleStartX = startX
		}
		if sampleEndX > endX {
			sampleEndX = endX
		}

		// Count white/light pixels in this region
		// Selected tab has white/cream background (high RGB values)
		// Unselected tabs are gray/dark (RGB < 180)
		lightCount := 0
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := sampleStartX; x < sampleEndX; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				// Convert from 16-bit to 8-bit color
				r8, g8, b8 := uint8(r>>8), uint8(g>>8), uint8(b>>8)

				// White/cream/light background detection
				if r8 > 200 && g8 > 200 && b8 > 200 {
					lightCount++
				}
			}
		}
		lightCounts[dayIdx] = lightCount
	}

	// Find the day with the most light pixels
	maxLight := 0
	selectedDay := -1
	for i, count := range lightCounts {
		if count > maxLight {
			maxLight = count
			selectedDay = i
		}
	}

	// Require a minimum threshold to avoid false positives
	minThreshold := 100 // At least 100 light pixels
	if selectedDay >= 0 && maxLight > minThreshold {
		log.Printf("Day detected by color: %s (light pixel count: %d)", days[selectedDay], maxLight)
		log.Printf("Color counts per day: Mon=%d, Tue=%d, Wed=%d, Thu=%d, Fri=%d, Sat=%d",
			lightCounts[0], lightCounts[1], lightCounts[2], lightCounts[3], lightCounts[4], lightCounts[5])
		return days[selectedDay]
	}

	log.Printf("Color detection failed: max light count %d below threshold %d", maxLight, minThreshold)
	log.Printf("Color counts per day: Mon=%d, Tue=%d, Wed=%d, Thu=%d, Fri=%d, Sat=%d",
		lightCounts[0], lightCounts[1], lightCounts[2], lightCounts[3], lightCounts[4], lightCounts[5])
	return ""
}

// Extract just the day tab region and detect selected day by color
func detectDayFromTabRegion(imageData []byte) string {
	// Decode the image
	img, format, err := image.Decode(bytes.NewReader(imageData))
	if err != nil {
		log.Printf("Failed to decode image for tab detection: %v", err)
		return ""
	}
	log.Printf("Image format for tab detection: %s, bounds: %v", format, img.Bounds())

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// The tab region is approximately at y=220-290 pixels from the top
	// Adjust based on image size
	tabTop := int(float64(height) * 0.08)     // ~8% from top
	tabBottom := int(float64(height) * 0.105) // ~10.5% from top

	// Ensure we don't go out of bounds
	if tabTop < 0 {
		tabTop = 0
	}
	if tabBottom > height {
		tabBottom = height
	}
	if tabBottom <= tabTop {
		tabBottom = tabTop + 100 // minimum 100px height
	}

	log.Printf("Extracting tab region: y=%d to y=%d (full image: %dx%d)", tabTop, tabBottom, width, height)

	// Create a new image with just the tab region
	tabRegion := image.NewRGBA(image.Rect(0, 0, width, tabBottom-tabTop))
	draw.Draw(tabRegion, tabRegion.Bounds(), img, image.Point{0, tabTop}, draw.Src)

	// First try: Detect by color (most reliable for this UI)
	dayByColor := detectDayByColor(tabRegion)
	if dayByColor != "" {
		return dayByColor
	}

	log.Printf("Color detection failed, falling back to OCR")

	// Fallback: Try OCR detection

	// Simple preprocessing for tab region: scale 2x and convert to grayscale
	// Don't use preprocessImageForOCR() as it tries to detect data regions (fails on small images)
	scaledTab := scaleImage(tabRegion, 2)
	grayTab := convertToGrayscale(scaledTab)

	// Convert to PNG bytes
	var buf bytes.Buffer
	if err := png.Encode(&buf, grayTab); err != nil {
		log.Printf("Failed to encode tab region: %v", err)
		return ""
	}

	log.Printf("Tab region preprocessed: %dx%d -> %dx%d (scaled 2x, grayscale)",
		tabRegion.Bounds().Dx(), tabRegion.Bounds().Dy(),
		grayTab.Bounds().Dx(), grayTab.Bounds().Dy())

	// Run OCR on the tab region
	client := gosseract.NewClient()
	defer client.Close()

	if err := client.SetImageFromBytes(buf.Bytes()); err != nil {
		log.Printf("Failed to load tab region for OCR: %v", err)
		return ""
	}

	client.SetPageSegMode(gosseract.PSM_SINGLE_LINE)
	text, err := client.Text()
	if err != nil || len(strings.TrimSpace(text)) == 0 {
		log.Printf("Tab region OCR failed or empty")
		return ""
	}

	log.Printf("Tab region OCR text: %s", text)

	// Look for day names in the tab text
	textLower := strings.ToLower(text)
	days := []struct {
		name     string
		patterns []string
	}{
		{"monday", []string{"monday", "mon.", "mon"}},
		{"tuesday", []string{"tuesday", "tues.", "tues", "tue"}},
		{"wednesday", []string{"wednesday", "wed.", "wed"}},
		{"thursday", []string{"thursday", "thur.", "thur", "thu"}},
		{"friday", []string{"friday", "fri.", "fri"}},
		{"saturday", []string{"saturday", "sat.", "sat"}},
	}

	// Find which day patterns appear in the text
	// The selected tab usually appears first or more prominently
	for _, day := range days {
		for _, pattern := range day.patterns {
			if strings.Contains(textLower, pattern) {
				// Check if this appears early in the text (likely the selected/highlighted tab)
				idx := strings.Index(textLower, pattern)
				if idx < 100 { // Within first 100 chars suggests it's prominent
					log.Printf("Detected day '%s' from tab region (pattern: '%s' at position %d)", day.name, pattern, idx)
					return day.name
				}
			}
		}
	}

	return ""
}

// Extract VS points data from image and detect which day
func extractVSPointsDataFromImage(imageData []byte) (day string, records []struct {
	MemberName string `json:"member_name"`
	Points     int64  `json:"points"`
}, error error) {
	// First try to detect the day from the tab region specifically
	detectedDay := detectDayFromTabRegion(imageData)

	// If day detection failed, try text-based detection
	if detectedDay == "" {
		log.Printf("Tab region day detection failed, will try OCR fallback")
	}

	// Decode image for row segmentation
	img, _, err := image.Decode(bytes.NewReader(imageData))
	if err != nil {
		return "", nil, fmt.Errorf("failed to decode image: %v", err)
	}

	// Analyze screenshot to get regions
	attrs := analyzeScreenshot(img)

	// Try segmented OCR approach: extract and process individual rows
	log.Printf("Attempting row-by-row segmented OCR...")
	records, err = extractVSPointsByRows(img, attrs)

	if err != nil || len(records) == 0 {
		log.Printf("Segmented OCR failed (%v), falling back to full image OCR", err)
		// Fallback to original full-image OCR approach
		records, err = extractVSPointsFullImage(imageData, attrs)
		if err != nil {
			return detectedDay, nil, err
		}
	}

	// If we didn't detect day from tab region, try text-based detection as fallback
	if detectedDay == "" {
		// Use current system day as last resort
		now := time.Now()
		weekday := now.Weekday()
		switch weekday {
		case time.Monday:
			detectedDay = "monday"
		case time.Tuesday:
			detectedDay = "tuesday"
		case time.Wednesday:
			detectedDay = "wednesday"
		case time.Thursday:
			detectedDay = "thursday"
		case time.Friday:
			detectedDay = "friday"
		case time.Saturday:
			detectedDay = "saturday"
		default:
			// Sunday - default to Monday
			detectedDay = "monday"
		}
		log.Printf("Warning: Could not detect day from screenshot, using current system day: %s", detectedDay)
	}

	if len(records) == 0 {
		return "", nil, fmt.Errorf("no valid VS point records found in extracted text")
	}

	return detectedDay, records, nil
}

// Extract VS points by segmenting image into rows and OCR each row independently
func extractVSPointsByRows(img image.Image, attrs *ScreenshotAttributes) ([]struct {
	MemberName string `json:"member_name"`
	Points     int64  `json:"points"`
}, error) {
	bounds := img.Bounds()
	dataRegion := attrs.DataRegion
	rowHeight := attrs.RowHeight
	estimatedRows := attrs.EstimatedRows

	if estimatedRows < 1 {
		estimatedRows = 10
	}

	records := []struct {
		MemberName string `json:"member_name"`
		Points     int64  `json:"points"`
	}{}

	log.Printf("Processing %d estimated rows with height %d", estimatedRows, rowHeight)

	// Extract and OCR each row
	for i := 0; i < estimatedRows; i++ {
		rowTop := dataRegion.Top + (i * rowHeight)
		rowBottom := rowTop + rowHeight

		// Ensure we don't go out of bounds
		if rowBottom > dataRegion.Bottom {
			rowBottom = dataRegion.Bottom
		}
		if rowTop >= rowBottom {
			break
		}

		// Extract this row as a separate image
		rowImg := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), rowBottom-rowTop))
		draw.Draw(rowImg, rowImg.Bounds(), img, image.Point{0, rowTop}, draw.Src)

		// Process this row
		// Split row into segments: rank (15%), name (50%), points (35%)
		rankWidth := bounds.Dx() * 15 / 100
		nameStart := rankWidth
		nameWidth := bounds.Dx() * 50 / 100
		pointsStart := nameStart + nameWidth

		// Extract name segment
		nameImg := image.NewRGBA(image.Rect(0, 0, nameWidth, rowBottom-rowTop))
		draw.Draw(nameImg, nameImg.Bounds(), rowImg, image.Point{nameStart, 0}, draw.Src)

		// Extract points segment
		pointsWidth := bounds.Dx() - pointsStart
		pointsImg := image.NewRGBA(image.Rect(0, 0, pointsWidth, rowBottom-rowTop))
		draw.Draw(pointsImg, pointsImg.Bounds(), rowImg, image.Point{pointsStart, 0}, draw.Src)

		// Scale 2x and convert to grayscale for better OCR
		scaledName := scaleImage(nameImg, 2)
		grayName := convertToGrayscale(scaledName)

		scaledPoints := scaleImage(pointsImg, 2)
		grayPoints := convertToGrayscale(scaledPoints)

		// OCR the name segment
		var nameBuf bytes.Buffer
		if err := png.Encode(&nameBuf, grayName); err != nil {
			log.Printf("Row %d: Failed to encode name segment: %v", i+1, err)
			continue
		}

		nameClient := gosseract.NewClient()
		defer nameClient.Close()
		nameClient.SetImageFromBytes(nameBuf.Bytes())
		nameClient.SetPageSegMode(gosseract.PSM_SINGLE_LINE)
		nameText, err := nameClient.Text()
		if err != nil || len(strings.TrimSpace(nameText)) == 0 {
			continue // Skip empty rows
		}

		// OCR the points segment
		var pointsBuf bytes.Buffer
		if err := png.Encode(&pointsBuf, grayPoints); err != nil {
			log.Printf("Row %d: Failed to encode points segment: %v", i+1, err)
			continue
		}

		pointsClient := gosseract.NewClient()
		defer pointsClient.Close()
		pointsClient.SetImageFromBytes(pointsBuf.Bytes())
		pointsClient.SetPageSegMode(gosseract.PSM_SINGLE_LINE)
		pointsText, err := pointsClient.Text()
		if err != nil || len(strings.TrimSpace(pointsText)) == 0 {
			log.Printf("Row %d: Name='%s', but no points found", i+1, strings.TrimSpace(nameText))
			continue
		}

		// Parse the extracted text
		name := strings.TrimSpace(nameText)
		// Clean up name (remove alliance tags, rank numbers, etc.)
		name = cleanPlayerName(name)

		// Parse points
		pointsStr := strings.TrimSpace(pointsText)
		pointsStr = strings.ReplaceAll(pointsStr, ",", "")
		pointsStr = strings.ReplaceAll(pointsStr, ".", "")
		pointsStr = strings.ReplaceAll(pointsStr, " ", "")

		points, err := strconv.ParseInt(pointsStr, 10, 64)
		if err != nil {
			log.Printf("Row %d: Failed to parse points '%s': %v", i+1, pointsStr, err)
			continue
		}

		if points < 100 { // Sanity check
			continue
		}

		log.Printf("Row %d: Name='%s', Points=%d", i+1, name, points)

		records = append(records, struct {
			MemberName string `json:"member_name"`
			Points     int64  `json:"points"`
		}{
			MemberName: name,
			Points:     points,
		})
	}

	return records, nil
}

// Fallback: Extract VS points from full image (original method)
func extractVSPointsFullImage(imageData []byte, attrs *ScreenshotAttributes) ([]struct {
	MemberName string `json:"member_name"`
	Points     int64  `json:"points"`
}, error) {
	// Preprocess image to filter and enhance relevant regions
	processedData, err := preprocessImageForOCR(imageData)
	if err != nil {
		log.Printf("Warning: Image preprocessing failed: %v. Using original image.", err)
		processedData = imageData // Fallback to original
	}

	client := gosseract.NewClient()
	defer client.Close()

	err = client.SetImageFromBytes(processedData)
	if err != nil {
		return nil, fmt.Errorf("failed to load image: %v", err)
	}

	// Try different PSM modes for better recognition
	var text string
	psmModes := []gosseract.PageSegMode{
		gosseract.PSM_AUTO,
		gosseract.PSM_SINGLE_BLOCK,
		gosseract.PSM_SPARSE_TEXT,
	}

	for i, mode := range psmModes {
		client.SetPageSegMode(mode)
		extractedText, err := client.Text()
		if err == nil && len(strings.TrimSpace(extractedText)) > 0 {
			text = extractedText
			log.Printf("OCR successful with PSM mode %d (attempt %d)", mode, i+1)
			break
		}
		log.Printf("OCR attempt %d with PSM mode %d failed or empty", i+1, mode)
	}

	if len(strings.TrimSpace(text)) == 0 {
		return nil, fmt.Errorf("OCR failed: no text extracted after trying multiple modes")
	}

	// Log the extracted text for debugging
	log.Printf("OCR extracted text:\n%s\n---END OCR---", text)

	// Parse the OCR text for VS points
	records := parseVSPointsText(text)

	return records, nil
}

// Clean player name by removing alliance tags, special characters, etc
func cleanPlayerName(name string) string {
	// Remove common OCR artifacts
	name = strings.ReplaceAll(name, "|", "I")
	name = strings.ReplaceAll(name, "~", "")
	name = strings.ReplaceAll(name, "`", "")

	// Remove alliance tags like [NTMs], (NTMs), etc.
	re := regexp.MustCompile(`\[.*?\]|\(.*?\)`)
	name = re.ReplaceAllString(name, "")

	// Remove rank numbers at the start (1), (2), etc.
	re = regexp.MustCompile(`^\d+\)?\s*`)
	name = re.ReplaceAllString(name, "")

	// Clean up whitespace
	name = strings.TrimSpace(name)

	return name
}

// Detect which day is selected from OCR text
// Looks for day names and context clues to determine the active tab
func detectSelectedDay(text string) string {
	textLower := strings.ToLower(text)

	// Check for presence of day names and "Daily Rank" which indicates VS points screen
	if !strings.Contains(textLower, "daily") && !strings.Contains(textLower, "rank") {
		return "" // Not a daily rank screen
	}

	// Count occurrences of each day name (the selected day often appears more prominently)
	days := map[string][]string{
		"monday":    {"monday", "mon.", "mon"},
		"tuesday":   {"tuesday", "tues.", "tues"},
		"wednesday": {"wednesday", "wed.", "wed"},
		"thursday":  {"thursday", "thur.", "thur"},
		"friday":    {"friday", "fri.", "fri"},
		"saturday":  {"saturday", "sat.", "sat"},
	}

	dayScores := make(map[string]int)

	for standardDay, variants := range days {
		for _, variant := range variants {
			// Count occurrences
			count := strings.Count(textLower, variant)
			dayScores[standardDay] += count

			// Higher weight if it appears near the beginning (likely the tab)
			if idx := strings.Index(textLower, variant); idx >= 0 && idx < 200 {
				dayScores[standardDay] += 2
			}
		}
	}

	// Find the day with the highest score
	maxScore := 0
	selectedDay := ""
	for day, score := range dayScores {
		if score > maxScore {
			maxScore = score
			selectedDay = day
		}
	}

	// Only return if we have strong confidence (score >= 3)
	if maxScore >= 3 {
		log.Printf("Detected day from full OCR text: %s (score: %d)", selectedDay, maxScore)
		return selectedDay
	}

	// Low confidence or no detection
	if maxScore > 0 {
		log.Printf("Low confidence day detection from OCR: %s (score: %d)", selectedDay, maxScore)
	}
	return ""
}

// Parse VS points text(from OCR or manual input)
func parseVSPointsText(text string) []struct {
	MemberName string `json:"member_name"`
	Points     int64  `json:"points"`
} {
	var records []struct {
		MemberName string `json:"member_name"`
		Points     int64  `json:"points"`
	}

	lines := strings.Split(text, "\n")

	// Pattern for VS points - similar to power rankings but with different number ranges
	// VS points are typically 6-9 digits (100k to 999M range)
	// Examples: "Gary6126 30598466", "Gargoland 23660312"
	rankPattern := regexp.MustCompile(`(?:R[0-9]\s+)?([A-Za-z][A-Za-z0-9_\s]*?)\s+([0-9]{6,})`)
	simplePattern := regexp.MustCompile(`([A-Za-z][A-Za-z0-9_]+)\s+([0-9]{6,})`)

	// Track seen names to avoid duplicates
	seenNames := make(map[string]bool)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Skip lines that are clearly UI elements
		if len(line) < 5 {
			continue
		}

		// Skip common UI text
		lowerLine := strings.ToLower(line)
		if strings.Contains(lowerLine, "ranking") ||
			strings.Contains(lowerLine, "commander") ||
			strings.Contains(lowerLine, "points") ||
			strings.Contains(lowerLine, "daily") ||
			strings.Contains(lowerLine, "weekly") ||
			strings.Contains(lowerLine, "mon") ||
			strings.Contains(lowerLine, "tues") ||
			strings.Contains(lowerLine, "wed") ||
			strings.Contains(lowerLine, "thur") ||
			strings.Contains(lowerLine, "fri") ||
			strings.Contains(lowerLine, "sat") ||
			strings.Contains(lowerLine, "alliance") ||
			strings.Contains(lowerLine, "your alliance") {
			continue
		}

		// Try patterns
		matches := rankPattern.FindStringSubmatch(line)
		if len(matches) == 0 {
			matches = simplePattern.FindStringSubmatch(line)
		}

		if len(matches) >= 3 {
			name := strings.TrimSpace(matches[1])
			pointsStr := strings.ReplaceAll(matches[2], ",", "")
			pointsStr = strings.ReplaceAll(pointsStr, " ", "")

			points, err := strconv.ParseInt(pointsStr, 10, 64)

			// Validate: points should be realistic (10k to 999M range), name should be reasonable
			if err == nil && points >= 10000 && points <= 999999999 &&
				len(name) >= 3 && len(name) <= 30 && !seenNames[name] {
				records = append(records, struct {
					MemberName string `json:"member_name"`
					Points     int64  `json:"points"`
				}{
					MemberName: name,
					Points:     points,
				})
				seenNames[name] = true
				log.Printf("Parsed VS points: %s -> %d", name, points)
			}
		}
	}

	return records
}

// HTTP handler to process VS points screenshot
func processVSPointsScreenshot(w http.ResponseWriter, r *http.Request) {
	var records []struct {
		MemberName string `json:"member_name"`
		Points     int64  `json:"points"`
	}
	var detectedDay string
	var weekDate string

	// Check if this is a multipart form (image upload) or JSON (manual text)
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		// Handle image upload
		err := r.ParseMultipartForm(10 << 20) // 10 MB max
		if err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		// Get the week parameter (optional, defaults to "current")
		weekParam := r.FormValue("week")
		if weekParam == "" {
			weekParam = "current"
		}

		file, _, err := r.FormFile("image")
		if err != nil {
			http.Error(w, "No image file provided", http.StatusBadRequest)
			return
		}
		defer file.Close()

		imageData, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "Failed to read image", http.StatusInternalServerError)
			return
		}

		detectedDay, records, err = extractVSPointsDataFromImage(imageData)
		if err != nil {
			http.Error(w, fmt.Sprintf("OCR processing failed: %v", err), http.StatusInternalServerError)
			return
		}

		// Determine the week date based on the week parameter
		now := time.Now()
		if weekParam == "last" {
			// Subtract 7 days to get last week
			now = now.AddDate(0, 0, -7)
		}
		weekday := now.Weekday()
		daysFromMonday := int(weekday) - 1
		if weekday == time.Sunday {
			daysFromMonday = 6
		}
		monday := now.AddDate(0, 0, -daysFromMonday)
		weekDate = monday.Format("2006-01-02")
	} else {
		// Handle JSON (manual text or pre-parsed data)
		var request struct {
			Records []struct {
				MemberName string `json:"member_name"`
				Points     int64  `json:"points"`
			} `json:"records"`
			Text string `json:"text"` // Raw text to parse
			Day  string `json:"day"`  // Optional: specify the day
			Week string `json:"week"` // Optional: "current" or "last"
		}

		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if request.Text != "" {
			// Parse raw text
			records = parseVSPointsText(request.Text)
			detectedDay = detectSelectedDay(request.Text)
		} else {
			records = request.Records
		}

		// Use provided day if available, otherwise use detected day
		if request.Day != "" {
			detectedDay = strings.ToLower(request.Day)
		}

		// Determine the week date based on the week parameter
		weekParam := request.Week
		if weekParam == "" {
			weekParam = "current"
		}
		now := time.Now()
		if weekParam == "last" {
			// Subtract 7 days to get last week
			now = now.AddDate(0, 0, -7)
		}
		weekday := now.Weekday()
		daysFromMonday := int(weekday) - 1
		if weekday == time.Sunday {
			daysFromMonday = 6
		}
		monday := now.AddDate(0, 0, -daysFromMonday)
		weekDate = monday.Format("2006-01-02")
	}

	if len(records) == 0 {
		http.Error(w, "No valid VS point records found", http.StatusBadRequest)
		return
	}

	if detectedDay == "" {
		http.Error(w, "Could not determine which day these VS points are for. Please specify the day manually.", http.StatusBadRequest)
		return
	}

	// Normalize day name
	dayColumn := detectedDay
	validDays := []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	isValidDay := false
	for _, d := range validDays {
		if dayColumn == d {
			isValidDay = true
			break
		}
	}
	if !isValidDay {
		http.Error(w, fmt.Sprintf("Invalid day: %s. Must be monday-saturday", dayColumn), http.StatusBadRequest)
		return
	}

	// Start transaction
	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	successCount := 0
	notFoundMembers := []string{}
	updatedMembers := []string{}

	for _, record := range records {
		// Try to find member by exact name match first
		var memberID int
		var memberName string
		err := tx.QueryRow("SELECT id, name FROM members WHERE LOWER(name) = LOWER(?)", record.MemberName).Scan(&memberID, &memberName)

		if err == sql.ErrNoRows {
			// Try fuzzy matching
			rows, err := tx.Query("SELECT id, name FROM members")
			if err != nil {
				continue
			}

			bestMatch := ""
			bestScore := 0
			bestID := 0

			for rows.Next() {
				var id int
				var name string
				if err := rows.Scan(&id, &name); err != nil {
					continue
				}

				score := calculateSimilarity(record.MemberName, name)
				if score > bestScore {
					bestScore = score
					bestMatch = name
					bestID = id
				}
			}
			rows.Close()

			// Use fuzzy match if similarity is high enough (70%+)
			if bestScore >= 70 {
				memberID = bestID
				memberName = bestMatch
				log.Printf("Fuzzy matched '%s' to '%s' (similarity: %d%%)", record.MemberName, bestMatch, bestScore)
			} else {
				notFoundMembers = append(notFoundMembers, record.MemberName)
				continue
			}
		}

		// Upsert VS points for this member
		var existingID int
		err = tx.QueryRow("SELECT id FROM vs_points WHERE member_id = ? AND week_date = ?",
			memberID, weekDate).Scan(&existingID)

		if err == sql.ErrNoRows {
			// Insert new record with the specific day's points
			query := fmt.Sprintf(`
				INSERT INTO vs_points (member_id, week_date, %s, updated_at)
				VALUES (?, ?, ?, CURRENT_TIMESTAMP)`, dayColumn)
			_, err = tx.Exec(query, memberID, weekDate, record.Points)
		} else if err == nil {
			// Update existing record with the specific day's points
			query := fmt.Sprintf(`
				UPDATE vs_points 
				SET %s = ?, updated_at = CURRENT_TIMESTAMP
				WHERE member_id = ? AND week_date = ?`, dayColumn)
			_, err = tx.Exec(query, record.Points, memberID, weekDate)
		}

		if err != nil {
			log.Printf("Failed to save VS points for %s: %v", memberName, err)
			continue
		}

		successCount++
		updatedMembers = append(updatedMembers, memberName)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to save changes", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"message":         fmt.Sprintf("Successfully updated VS points for %d members on %s", successCount, detectedDay),
		"day":             detectedDay,
		"week_date":       weekDate,
		"success_count":   successCount,
		"updated_members": updatedMembers,
	}

	if len(notFoundMembers) > 0 {
		response["not_found_members"] = notFoundMembers
		response["warning"] = fmt.Sprintf("%d members could not be matched to the database", len(notFoundMembers))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Process screenshot data with OCR support
func processPowerScreenshot(w http.ResponseWriter, r *http.Request) {
	// Check if power tracking is enabled
	var powerTrackingEnabled bool
	err := db.QueryRow("SELECT COALESCE(power_tracking_enabled, 0) FROM settings WHERE id = 1").Scan(&powerTrackingEnabled)
	if err != nil || !powerTrackingEnabled {
		http.Error(w, "Power tracking is not enabled", http.StatusForbidden)
		return
	}

	var records []struct {
		MemberName string `json:"member_name"`
		Power      int64  `json:"power"`
	}

	// Check if this is a multipart form (image upload) or JSON (manual text)
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		// Handle image upload
		err := r.ParseMultipartForm(10 << 20) // 10 MB max
		if err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		file, _, err := r.FormFile("image")
		if err != nil {
			http.Error(w, "No image file provided", http.StatusBadRequest)
			return
		}
		defer file.Close()

		imageData, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "Failed to read image", http.StatusInternalServerError)
			return
		}

		records, err = extractPowerDataFromImage(imageData)
		if err != nil {
			http.Error(w, fmt.Sprintf("OCR processing failed: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		// Handle JSON (manual text or pre-parsed data)
		var request struct {
			Records []struct {
				MemberName string `json:"member_name"`
				Power      int64  `json:"power"`
			} `json:"records"`
			Text string `json:"text"` // Raw text to parse
		}

		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if request.Text != "" {
			// Parse raw text
			records = parsePowerRankingsText(request.Text)
		} else {
			records = request.Records
		}
	}

	if len(records) == 0 {
		http.Error(w, "No valid records found", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	successCount := 0
	failedCount := 0
	errors := []string{}

	// Get all member names for fuzzy matching
	allMembers := []struct {
		ID   int
		Name string
	}{}
	rows, err := tx.Query("SELECT id, name FROM members")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var m struct {
				ID   int
				Name string
			}
			if rows.Scan(&m.ID, &m.Name) == nil {
				allMembers = append(allMembers, m)
			}
		}
	}

	for _, record := range records {
		// Try exact match first
		var memberID int
		err := tx.QueryRow("SELECT id FROM members WHERE name = ?", record.MemberName).Scan(&memberID)

		if err != nil {
			// Try case-insensitive match
			err = tx.QueryRow("SELECT id FROM members WHERE LOWER(name) = LOWER(?)", record.MemberName).Scan(&memberID)
		}

		if err != nil {
			// Try fuzzy matching with Levenshtein-like similarity
			bestMatch := ""
			bestMatchID := 0
			bestScore := 0

			for _, member := range allMembers {
				score := calculateSimilarity(record.MemberName, member.Name)
				log.Printf("Comparing '%s' with '%s': score=%d%%", record.MemberName, member.Name, score)
				if score > bestScore {
					bestScore = score
					bestMatch = member.Name
					bestMatchID = member.ID
				}
			}

			if bestMatchID > 0 && bestScore >= 50 { // Lowered to 50% similarity threshold
				memberID = bestMatchID
				log.Printf("✓ Fuzzy matched '%s' to '%s' (score: %d%%)", record.MemberName, bestMatch, bestScore)
			} else {
				failedCount++
				if bestMatch != "" {
					errors = append(errors, fmt.Sprintf("Member '%s' not found (closest: '%s' at %d%%, need 50%%+)", record.MemberName, bestMatch, bestScore))
				} else {
					errors = append(errors, fmt.Sprintf("Member '%s' not found (no members in database)", record.MemberName))
				}
				continue
			}
		}

		// Insert power record
		_, err = tx.Exec("INSERT INTO power_history (member_id, power) VALUES (?, ?)",
			memberID, record.Power)
		if err != nil {
			failedCount++
			errors = append(errors, fmt.Sprintf("Failed to add record for '%s': %v", record.MemberName, err))
			continue
		}

		successCount++
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to save records", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":       fmt.Sprintf("Processed %d records successfully, %d failed", successCount, failedCount),
		"success_count": successCount,
		"failed_count":  failedCount,
		"errors":        errors,
	})
}

// --- NEW TEMPLATE LOGIC ---

// PageData holds all the information we want to pass into our HTML templates
type PageData struct {
	Title           string
	ActivePage      string
	IsAuthenticated bool
	Username        string
	IsAdmin         bool
	Rank            string
	CanManageRanks  bool
	IsR5OrAdmin     bool
}

// getPageData extracts the current user's session data and permissions
func getPageData(r *http.Request, title, activePage string) PageData {
	data := PageData{
		Title:      title,
		ActivePage: activePage,
	}

	session, _ := store.Get(r, "session")
	if auth, ok := session.Values["authenticated"].(bool); ok && auth {
		data.IsAuthenticated = true
		data.Username = session.Values["username"].(string)

		// Set admin permissions FIRST (so default admin gets access)
		if adminVal, ok := session.Values["is_admin"].(bool); ok {
			data.IsAdmin = adminVal
			data.IsR5OrAdmin = adminVal
			data.CanManageRanks = adminVal
		}

		// Then layer on member-specific permissions if they are linked to a player
		if memberID, ok := session.Values["member_id"].(int); ok {
			var rank string
			// We check their rank from the database to ensure it's up to date
			err := db.QueryRow("SELECT rank FROM members WHERE id = ?", memberID).Scan(&rank)
			if err == nil {
				data.Rank = rank

				// If they are NOT an admin, we grant access based on their game rank
				if !data.IsAdmin {
					data.CanManageRanks = (rank == "R4" || rank == "R5")
					data.IsR5OrAdmin = (rank == "R5")
				}
			}
		}
	}
	return data
}

// renderTemplate parses the shared layout and the specific page together
func renderTemplate(w http.ResponseWriter, r *http.Request, tmplName string, data PageData) {
	// We parse layout.html FIRST, then the specific page content
	t, err := template.ParseFiles("templates/layout.html", "templates/"+tmplName)
	if err != nil {
		log.Printf("Template parsing error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	err = t.ExecuteTemplate(w, "layout.html", data)
	if err != nil {
		log.Printf("Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func main() {
	// Initialize session store first
	initSessionStore()

	if err := initDB(); err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer db.Close()

	router := mux.NewRouter()

	// Auth routes (public)
	router.HandleFunc("/api/login", login).Methods("POST")
	router.HandleFunc("/api/logout", logout).Methods("POST")
	router.HandleFunc("/api/check-auth", checkAuth).Methods("GET")
	router.HandleFunc("/api/change-password", authMiddleware(changePassword)).Methods("POST")
	router.HandleFunc("/api/members/{id}/create-user", authMiddleware(adminR5Middleware(createUserForMember))).Methods("POST")

	// Admin routes (admin only)
	router.HandleFunc("/api/admin/users", authMiddleware(adminMiddleware(getAdminUsers))).Methods("GET")
	router.HandleFunc("/api/admin/users", authMiddleware(adminMiddleware(createAdminUser))).Methods("POST")
	router.HandleFunc("/api/admin/users/{id}", authMiddleware(adminMiddleware(updateAdminUser))).Methods("PUT")
	router.HandleFunc("/api/admin/users/{id}", authMiddleware(adminMiddleware(deleteAdminUser))).Methods("DELETE")
	router.HandleFunc("/api/admin/users/{id}/reset-password", authMiddleware(adminMiddleware(resetUserPassword))).Methods("POST")
	router.HandleFunc("/api/admin/login-history", authMiddleware(adminMiddleware(getLoginHistory))).Methods("GET")

	// API routes (protected)
	router.HandleFunc("/api/members", authMiddleware(getMembers)).Methods("GET")
	router.HandleFunc("/api/members/stats", authMiddleware(getMemberStats)).Methods("GET")
	router.HandleFunc("/api/members", authMiddleware(rankManagementMiddleware(createMember))).Methods("POST")
	router.HandleFunc("/api/members/{id}", authMiddleware(rankManagementMiddleware(updateMember))).Methods("PUT")
	router.HandleFunc("/api/members/{id}", authMiddleware(rankManagementMiddleware(deleteMember))).Methods("DELETE")
	router.HandleFunc("/api/members/import", authMiddleware(rankManagementMiddleware(importCSV))).Methods("POST")
	router.HandleFunc("/api/members/import/confirm", authMiddleware(rankManagementMiddleware(confirmMemberUpdates))).Methods("POST")

	// Train schedule routes (protected)
	router.HandleFunc("/api/train-schedules", authMiddleware(getTrainSchedules)).Methods("GET")
	router.HandleFunc("/api/train-schedules/weekly-message", authMiddleware(generateWeeklyMessage)).Methods("GET")
	router.HandleFunc("/api/train-schedules/daily-message", authMiddleware(generateDailyMessage)).Methods("GET")
	router.HandleFunc("/api/train-schedules/conductor-messages", authMiddleware(generateConductorMessages)).Methods("GET")
	router.HandleFunc("/api/train-schedules/auto-schedule", authMiddleware(autoSchedule)).Methods("POST")
	router.HandleFunc("/api/train-schedules", authMiddleware(createTrainSchedule)).Methods("POST")
	router.HandleFunc("/api/train-schedules/{id}", authMiddleware(updateTrainSchedule)).Methods("PUT")
	router.HandleFunc("/api/train-schedules/{id}", authMiddleware(deleteTrainSchedule)).Methods("DELETE")

	// Awards routes (protected)
	router.HandleFunc("/api/awards", authMiddleware(getAwards)).Methods("GET")
	router.HandleFunc("/api/awards", authMiddleware(saveAwards)).Methods("POST")
	router.HandleFunc("/api/awards/{week}", authMiddleware(deleteWeekAwards)).Methods("DELETE")

	// Award types routes
	router.HandleFunc("/api/award-types", authMiddleware(getAwardTypes)).Methods("GET")
	router.HandleFunc("/api/award-types", authMiddleware(createAwardType)).Methods("POST")
	router.HandleFunc("/api/award-types/{id}", authMiddleware(updateAwardType)).Methods("PUT")
	router.HandleFunc("/api/award-types/{id}", authMiddleware(deleteAwardType)).Methods("DELETE")

	// VS points routes (protected)
	router.HandleFunc("/api/vs-points", authMiddleware(getVSPoints)).Methods("GET")
	router.HandleFunc("/api/vs-points", authMiddleware(saveVSPoints)).Methods("POST")
	router.HandleFunc("/api/vs-points/{week}", authMiddleware(deleteWeekVSPoints)).Methods("DELETE")
	router.HandleFunc("/api/vs-points/process-screenshot", authMiddleware(processVSPointsScreenshot)).Methods("POST")

	// Recommendations routes (protected)
	router.HandleFunc("/api/recommendations", authMiddleware(getRecommendations)).Methods("GET")
	router.HandleFunc("/api/recommendations", authMiddleware(createRecommendation)).Methods("POST")
	router.HandleFunc("/api/recommendations/{id}", authMiddleware(deleteRecommendation)).Methods("DELETE")

	// Dyno Recommendations routes (protected)
	router.HandleFunc("/api/dyno-recommendations", authMiddleware(getDynoRecommendations)).Methods("GET")
	router.HandleFunc("/api/dyno-recommendations", authMiddleware(createDynoRecommendation)).Methods("POST")
	router.HandleFunc("/api/dyno-recommendations/{id}", authMiddleware(deleteDynoRecommendation)).Methods("DELETE")

	// Settings routes (protected)
	router.HandleFunc("/api/settings", authMiddleware(getSettings)).Methods("GET")
	router.HandleFunc("/api/settings", authMiddleware(adminR5Middleware(updateSettings))).Methods("PUT")

	// Rankings routes (protected)
	router.HandleFunc("/api/rankings", authMiddleware(getMemberRankings)).Methods("GET")
	router.HandleFunc("/api/member-timelines", authMiddleware(getMemberTimelines)).Methods("GET")

	// Storm assignments routes (protected, R4/R5 only)
	router.HandleFunc("/api/storm-assignments", authMiddleware(getStormAssignments)).Methods("GET")
	router.HandleFunc("/api/storm-assignments", authMiddleware(r4r5Middleware(saveStormAssignments))).Methods("POST")
	router.HandleFunc("/api/storm-assignments/{taskForce}", authMiddleware(r4r5Middleware(deleteStormAssignments))).Methods("DELETE")

	// Power history routes (protected)
	router.HandleFunc("/api/power-history", authMiddleware(getPowerHistory)).Methods("GET")
	router.HandleFunc("/api/power-history", authMiddleware(addPowerRecord)).Methods("POST")
	router.HandleFunc("/api/power-history/process-screenshot", authMiddleware(processPowerScreenshot)).Methods("POST")

	// Home Page
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data := getPageData(r, "Members - Alliance Manager", "index")

		// Redirect to login if not authenticated
		if !data.IsAuthenticated {
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}

		renderTemplate(w, r, "index.html", data)
	}).Methods("GET")

	// Custom Login Route (No Layout)
	router.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		// 1. Check if they are already logged in
		session, _ := store.Get(r, "session")
		if auth, ok := session.Values["authenticated"].(bool); ok && auth {
			// If logged in, instantly redirect to the home page
			http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
			return
		}

		// 2. Parse ONLY the login.html file (no layout.html)
		t, err := template.ParseFiles("templates/login.html")
		if err != nil {
			log.Printf("Template parsing error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// 3. Serve the standalone page
		err = t.Execute(w, nil)
		if err != nil {
			log.Printf("Template execution error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	}).Methods("GET")

	// Map out the rest of your pages
	pages := map[string]string{
		"/train":           "train",
		"/awards":          "awards",
		"/recommendations": "recommendations",
		"/dyno":            "dyno",
		"/rankings":        "rankings",
		"/storm":           "storm",
		"/vs":              "vs",
		"/upload":          "upload",
		"/settings":        "settings",
		"/admin":           "admin",
		"/profile":         "profile",
	}

	for path, templateName := range pages {
		p := path
		tmpl := templateName
		router.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			data := getPageData(r, strings.Title(tmpl)+" - Alliance Manager", tmpl)

			// --- SERVER-SIDE SECURITY ---
			// 1. Redirect to login if not authenticated
			if !data.IsAuthenticated {
				http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
				return
			}

			// 2. Don't even render the page if they lack permissions
			if tmpl == "admin" && !data.IsAdmin {
				http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
				return
			}
			if tmpl == "settings" && !data.IsR5OrAdmin {
				http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
				return
			}

			renderTemplate(w, r, tmpl+".html", data)
		}).Methods("GET")

		// Redirect old .html links to the clean URLs
		router.HandleFunc(p+".html", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, p, http.StatusMovedPermanently)
		}).Methods("GET")
	}

	// Serve CSS, JS, and Images from static/
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("./static")))

	log.Println("Server starting on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", router))
}
