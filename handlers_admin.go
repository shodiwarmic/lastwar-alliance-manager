// handlers_admin.go - Admin dashboard handlers for permissions and user management

package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
)

// --- PERMISSIONS MANAGEMENT ---

func getRankPermissions(rank string) RankPermissions {
	var p RankPermissions
	p.Rank = rank
	db.QueryRow(`SELECT view_train, manage_train, view_awards, manage_awards, view_recs, manage_recs, view_dyno, manage_dyno, view_rankings, view_storm, manage_storm, view_vs_points, manage_vs_points, view_upload, manage_members, manage_settings, view_files, upload_files, manage_files, view_anonymous_authors, view_schedule, manage_schedule, view_officer_command, manage_officer_command, view_recruiting, manage_recruiting, view_allies, manage_allies, view_activity, view_accountability, manage_accountability, view_season_hub, manage_season_hub, manage_season_rewards FROM rank_permissions WHERE rank = ?`, rank).Scan(
		&p.ViewTrain, &p.ManageTrain, &p.ViewAwards, &p.ManageAwards, &p.ViewRecs, &p.ManageRecs, &p.ViewDyno, &p.ManageDyno, &p.ViewRankings, &p.ViewStorm, &p.ManageStorm, &p.ViewVSPoints, &p.ManageVSPoints, &p.ViewUpload, &p.ManageMembers, &p.ManageSettings, &p.ViewFiles, &p.UploadFiles, &p.ManageFiles, &p.ViewAnonymousAuthors, &p.ViewSchedule, &p.ManageSchedule, &p.ViewOfficerCommand, &p.ManageOfficerCommand, &p.ViewRecruiting, &p.ManageRecruiting, &p.ViewAllies, &p.ManageAllies, &p.ViewActivity, &p.ViewAccountability, &p.ManageAccountability, &p.ViewSeasonHub, &p.ManageSeasonHub, &p.ManageSeasonRewards,
	)
	return p
}

func getPermissionsMatrix(w http.ResponseWriter, r *http.Request) {
	ranks := []string{"R5", "R4", "R3", "R2", "R1"}
	var matrix []RankPermissions
	for _, rank := range ranks {
		matrix = append(matrix, getRankPermissions(rank))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(matrix)
}

func updatePermissionsMatrix(w http.ResponseWriter, r *http.Request) {
	var matrix []RankPermissions
	if err := json.NewDecoder(r.Body).Decode(&matrix); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	stmt, _ := tx.Prepare(`UPDATE rank_permissions SET view_train=?, manage_train=?, view_awards=?, manage_awards=?, view_recs=?, manage_recs=?, view_dyno=?, manage_dyno=?, view_rankings=?, view_storm=?, manage_storm=?, view_vs_points=?, manage_vs_points=?, view_upload=?, manage_members=?, manage_settings=?, view_files=?, upload_files=?, manage_files=?, view_anonymous_authors=?, view_schedule=?, manage_schedule=?, view_officer_command=?, manage_officer_command=?, view_recruiting=?, manage_recruiting=?, view_allies=?, manage_allies=?, view_activity=?, view_accountability=?, manage_accountability=?, view_season_hub=?, manage_season_hub=?, manage_season_rewards=? WHERE rank=?`)

	for _, p := range matrix {
		stmt.Exec(p.ViewTrain, p.ManageTrain, p.ViewAwards, p.ManageAwards, p.ViewRecs, p.ManageRecs, p.ViewDyno, p.ManageDyno, p.ViewRankings, p.ViewStorm, p.ManageStorm, p.ViewVSPoints, p.ManageVSPoints, p.ViewUpload, p.ManageMembers, p.ManageSettings, p.ViewFiles, p.UploadFiles, p.ManageFiles, p.ViewAnonymousAuthors, p.ViewSchedule, p.ManageSchedule, p.ViewOfficerCommand, p.ManageOfficerCommand, p.ViewRecruiting, p.ManageRecruiting, p.ViewAllies, p.ManageAllies, p.ViewActivity, p.ViewAccountability, p.ManageAccountability, p.ViewSeasonHub, p.ManageSeasonHub, p.ManageSeasonRewards, p.Rank)
	}
	stmt.Close()
	tx.Commit()

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "updated", "permissions", "rank permissions matrix", true)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Permissions updated"})
}

// --- ADMIN USER MANAGEMENT ---

// Admin: Get all users with login information (DEADLOCK FIXED)
func getAdminUsers(w http.ResponseWriter, r *http.Request) {
	query := `
		SELECT u.id, u.username, u.member_id, u.is_admin, u.force_password_change, 
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

	users := []AdminUserResponse{}

	// Phase 1: Fetch all users into memory first
	for rows.Next() {
		var user AdminUserResponse
		var memberID sql.NullInt64
		var memberName sql.NullString
		var lastLogin sql.NullString

		err := rows.Scan(&user.ID, &user.Username, &memberID, &user.IsAdmin, &user.ForcePasswordChange,
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

		users = append(users, user)
	}
	// CRITICAL: Close the rows here to release the DB connection back to the pool!
	rows.Close()

	// Phase 2: Now that the connection is free, loop through the slice and fetch recent logins
	for i := range users {
		loginRows, err := db.Query(`
			SELECT id, user_id, username, ip_address, user_agent, country, city, isp, login_time, success
			FROM login_sessions
			WHERE user_id = ? AND success = 1
			ORDER BY login_time DESC
			LIMIT 5
		`, users[i].ID)

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
			users[i].RecentLogins = recentLogins
		}
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

	if err := validatePasswordPolicy(req.Password, 0); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var existingID int
	err := db.QueryRow("SELECT id FROM users WHERE username = ?", req.Username).Scan(&existingID)
	if err == nil {
		http.Error(w, "Username already exists", http.StatusConflict)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	result, err := db.Exec("INSERT INTO users (username, password, member_id, is_admin, force_password_change) VALUES (?, ?, ?, ?, ?)",
		req.Username, string(hashedPassword), req.MemberID, req.IsAdmin, req.ForcePasswordChange)
	if err != nil {
		slog.Error("failed to create user", "error", err)
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	db.Exec("INSERT INTO password_history (user_id, password_hash) VALUES (?, ?)", id, string(hashedPassword))

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "created", "user", req.Username, true)

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

	var existingUsername string
	err = db.QueryRow("SELECT username FROM users WHERE id = ?", userID).Scan(&existingUsername)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	if req.Username != "" && req.Username != existingUsername {
		var otherID int
		err = db.QueryRow("SELECT id FROM users WHERE username = ? AND id != ?", req.Username, userID).Scan(&otherID)
		if err == nil {
			http.Error(w, "Username already exists", http.StatusConflict)
			return
		}
	}

	var oldIsAdmin bool
	db.QueryRow("SELECT is_admin FROM users WHERE id = ?", userID).Scan(&oldIsAdmin)

	if req.Username != "" {
		_, err = db.Exec("UPDATE users SET username = ?, member_id = ?, is_admin = ?, force_password_change = ? WHERE id = ?",
			req.Username, req.MemberID, req.IsAdmin, req.ForcePasswordChange, userID)
	} else {
		_, err = db.Exec("UPDATE users SET member_id = ?, is_admin = ?, force_password_change = ? WHERE id = ?",
			req.MemberID, req.IsAdmin, req.ForcePasswordChange, userID)
	}

	if err != nil {
		slog.Error("failed to update user", "error", err, "userID", userID)
		http.Error(w, "Failed to update user", http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	targetUsername := existingUsername
	if req.Username != "" {
		targetUsername = req.Username
	}
	var userChanges []string
	if req.Username != "" && req.Username != existingUsername {
		userChanges = append(userChanges, "username: "+existingUsername+" → "+req.Username)
	}
	if oldIsAdmin != req.IsAdmin {
		was, now := "standard", "standard"
		if oldIsAdmin {
			was = "admin"
		}
		if req.IsAdmin {
			now = "admin"
		}
		userChanges = append(userChanges, "role: "+was+" → "+now)
	}
	logActivity(actorID, actorName, "updated", "user", targetUsername, true, strings.Join(userChanges, "; "))

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

	var deletedUsername string
	db.QueryRow("SELECT username FROM users WHERE id = ?", userID).Scan(&deletedUsername)

	_, err = db.Exec("DELETE FROM users WHERE id = ?", userID)
	if err != nil {
		slog.Error("failed to delete user", "error", err, "userID", userID)
		http.Error(w, "Failed to delete user", http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "deleted", "user", deletedUsername, true)

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

	var username string
	err = db.QueryRow("SELECT username FROM users WHERE id = ?", userID).Scan(&username)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	randomPassword, err := generateRandomPassword(10)
	if err != nil {
		http.Error(w, "Failed to generate password", http.StatusInternalServerError)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(randomPassword), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec("UPDATE users SET password = ?, force_password_change = 1, password_changed_at = CURRENT_TIMESTAMP WHERE id = ?", string(hashedPassword), userID)
	if err != nil {
		slog.Error("failed to reset user password", "error", err, "userID", userID)
		http.Error(w, "Failed to reset password", http.StatusInternalServerError)
		return
	}

	db.Exec("INSERT INTO password_history (user_id, password_hash) VALUES (?, ?)", userID, string(hashedPassword))

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "reset_password", "user", username, true)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":  "Password reset successfully",
		"username": username,
		"password": randomPassword,
	})
}

// Admin: Get login history
func getLoginHistory(w http.ResponseWriter, r *http.Request) {
	userIDParam := r.URL.Query().Get("user_id")
	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "100"
	}

	limitInt, err := strconv.Atoi(limit)
	if err != nil || limitInt < 1 || limitInt > 10000 {
		limitInt = 100
	}

	var rows *sql.Rows
	if userIDParam != "" {
		userID, err := strconv.Atoi(userIDParam)
		if err != nil {
			http.Error(w, "Invalid user_id parameter", http.StatusBadRequest)
			return
		}
		rows, err = db.Query(`
			SELECT ls.id, ls.user_id, ls.username, ls.ip_address, ls.user_agent,
			       ls.country, ls.city, ls.isp, ls.login_time, ls.success
			FROM login_sessions ls
			WHERE ls.user_id = ?
			ORDER BY ls.login_time DESC LIMIT ?`, userID, limitInt)
	} else {
		rows, err = db.Query(`
			SELECT ls.id, ls.user_id, ls.username, ls.ip_address, ls.user_agent,
			       ls.country, ls.city, ls.isp, ls.login_time, ls.success
			FROM login_sessions ls
			ORDER BY ls.login_time DESC LIMIT ?`, limitInt)
	}
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

// --- SETTINGS MANAGEMENT ---

func getSettings(w http.ResponseWriter, r *http.Request) {
	var s Settings

	err := db.QueryRow(`SELECT
        id, schedule_message_template, daily_message_template, power_tracking_enabled,
        COALESCE(storm_timezones, ''), COALESCE(storm_respect_dst, 0), COALESCE(login_message, ''), COALESCE(max_hq_level, 35),
        COALESCE(pwd_min_length, 12), COALESCE(pwd_require_special, 0),
        COALESCE(pwd_require_upper, 0), COALESCE(pwd_require_lower, 0),
        COALESCE(pwd_require_number, 0), COALESCE(pwd_history_count, 4),
        COALESCE(pwd_validity_days, 180), COALESCE(squad_tracking_enabled, 0),
        COALESCE(cv_worker_url, ''),
        COALESCE(train_free_daily_limit, 1), COALESCE(train_purchased_daily_limit, 2),
        COALESCE(alliance_max_members, 100), COALESCE(join_requirements, ''),
        COALESCE(vs_minimum_points, 2500000),
        COALESCE(strike_needs_improvement_threshold, 1), COALESCE(strike_at_risk_threshold, 3),
        COALESCE(mg_baseline, 11), COALESCE(zs_baseline, 7),
        COALESCE(mg_default_time, '00:30'), COALESCE(zs_default_time, '23:00'),
        COALESCE(mg_anchor_date, ''), COALESCE(zs_schedule_mode, 'weekdays'),
        COALESCE(zs_weekdays, '1,4'), COALESCE(zs_anchor_date, ''), COALESCE(zs_anchor_time, '23:00')
        FROM settings WHERE id = 1`).Scan(
		&s.ID, &s.ScheduleMessageTemplate,
		&s.DailyMessageTemplate, &s.PowerTrackingEnabled,
		&s.StormTimezones, &s.StormRespectDST, &s.LoginMessage, &s.MaxHQLevel,
		&s.PwdMinLength, &s.PwdRequireSpecial, &s.PwdRequireUpper,
		&s.PwdRequireLower, &s.PwdRequireNumber, &s.PwdHistoryCount, &s.PwdValidityDays,
		&s.SquadTrackingEnabled,
		&s.CVWorkerURL,
		&s.TrainFreeDailyLimit, &s.TrainPurchasedDailyLimit,
		&s.AllianceMaxMembers, &s.JoinRequirements,
		&s.VSMinimumPoints,
		&s.StrikeNeedsImprovementThreshold, &s.StrikeAtRiskThreshold,
		&s.MGBaseline, &s.ZSBaseline,
		&s.MGDefaultTime, &s.ZSDefaultTime,
		&s.MGAnchorDate, &s.ZSScheduleMode,
		&s.ZSWeekdays, &s.ZSAnchorDate, &s.ZSAnchorTime,
	)

	if err != nil {
		http.Error(w, "Failed to load settings", http.StatusInternalServerError)
		return
	}

	// Season fields are owned by the seasons table (Season Hub).
	// Use the most recently started season whose start_date has passed — this keeps
	// the schedule day counter incrementing through the off-season without resetting.
	var seasonNum sql.NullInt64
	var seasonStart sql.NullString
	db.QueryRow(`SELECT season_number, start_date FROM seasons WHERE start_date <= date('now') ORDER BY start_date DESC LIMIT 1`).Scan(&seasonNum, &seasonStart)
	if seasonNum.Valid {
		v := int(seasonNum.Int64)
		s.CurrentSeason = &v
	}
	if seasonStart.Valid {
		s.SeasonStartDate = seasonStart.String
	}

	// Check if the GCP Vision credentials physically exist in the database
	var hasGCPKey bool
	db.QueryRow("SELECT EXISTS(SELECT 1 FROM credentials WHERE service_name = 'gcp_vision')").Scan(&hasGCPKey)

	// Wrap the standard settings struct with our status booleans
	type extendedSettings struct {
		Settings               // Embeds your existing struct fields automatically
		HasGCPCredentials bool `json:"has_gcp_credentials"`
		OCRPipelineReady  bool `json:"ocr_pipeline_ready"`
	}

	response := extendedSettings{
		Settings:          s,
		HasGCPCredentials: hasGCPKey,
		OCRPipelineReady:  hasGCPKey && s.CVWorkerURL != "", // Requires BOTH to be true
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func updateSettings(w http.ResponseWriter, r *http.Request) {
	var settings Settings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, _ := store.Get(r, "session")
	isAdmin, _ := session.Values["is_admin"].(bool)

	// Note: current_season and season_start_date are no longer editable here —
	// they are derived from the seasons table (owned by Season Hub).
	_, err := db.Exec(`UPDATE settings SET
		schedule_message_template = ?,
		daily_message_template = ?, power_tracking_enabled = ?, storm_timezones = ?,
		storm_respect_dst = ?, login_message = ?, max_hq_level = ?, squad_tracking_enabled = ?,
		train_free_daily_limit = ?, train_purchased_daily_limit = ?,
		alliance_max_members = ?, join_requirements = ?,
		vs_minimum_points = ?,
		strike_needs_improvement_threshold = ?, strike_at_risk_threshold = ?,
		mg_baseline = ?, zs_baseline = ?,
		mg_default_time = ?, zs_default_time = ?,
		mg_anchor_date = ?, zs_schedule_mode = ?,
		zs_weekdays = ?, zs_anchor_date = ?, zs_anchor_time = ?
		WHERE id = 1`,
		settings.ScheduleMessageTemplate,
		settings.DailyMessageTemplate, settings.PowerTrackingEnabled, settings.StormTimezones,
		settings.StormRespectDST, settings.LoginMessage, settings.MaxHQLevel, settings.SquadTrackingEnabled,
		settings.TrainFreeDailyLimit, settings.TrainPurchasedDailyLimit,
		settings.AllianceMaxMembers, settings.JoinRequirements,
		settings.VSMinimumPoints,
		settings.StrikeNeedsImprovementThreshold, settings.StrikeAtRiskThreshold,
		settings.MGBaseline, settings.ZSBaseline,
		settings.MGDefaultTime, settings.ZSDefaultTime,
		settings.MGAnchorDate, settings.ZSScheduleMode,
		settings.ZSWeekdays, settings.ZSAnchorDate, settings.ZSAnchorTime,
	)
	if err != nil {
		slog.Error("failed to update settings", "error", err)
		http.Error(w, "Failed to update settings", http.StatusInternalServerError)
		return
	}

	if isAdmin && settings.PwdMinLength >= 6 {
		_, err = db.Exec(`UPDATE settings SET 
			pwd_min_length = ?, pwd_require_special = ?, pwd_require_upper = ?, 
			pwd_require_lower = ?, pwd_require_number = ?, pwd_history_count = ?, pwd_validity_days = ? 
			WHERE id = 1`,
			settings.PwdMinLength, settings.PwdRequireSpecial, settings.PwdRequireUpper,
			settings.PwdRequireLower, settings.PwdRequireNumber, settings.PwdHistoryCount, settings.PwdValidityDays,
		)
		if err != nil {
			slog.Error("failed to update password settings", "error", err)
			http.Error(w, "Failed to update settings", http.StatusInternalServerError)
			return
		}
	}

	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "updated", "settings", "alliance settings", true)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Settings updated successfully"})
}

// --- USER FILE SAFEGUARDS ---

func getUserFileCount(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM files WHERE owner_user_id = ?", userID).Scan(&count)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"count": count})
}

func transferUserFiles(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	oldOwnerID, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	var req struct {
		NewOwnerID int `json:"new_owner_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NewOwnerID == 0 {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	_, err = db.Exec("UPDATE files SET owner_user_id = ? WHERE owner_user_id = ?", req.NewOwnerID, oldOwnerID)
	if err != nil {
		http.Error(w, "Failed to transfer files", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Files transferred successfully"})
}

// Admin: Partially update the settings table with only Password Policy data
func updatePasswordPolicy(w http.ResponseWriter, r *http.Request) {
	var p struct {
		MinLength      int  `json:"pwd_min_length"`
		HistoryCount   int  `json:"pwd_history_count"`
		ValidityDays   int  `json:"pwd_validity_days"`
		RequireSpecial bool `json:"pwd_require_special"`
		RequireUpper   bool `json:"pwd_require_upper"`
		RequireLower   bool `json:"pwd_require_lower"`
		RequireNumber  bool `json:"pwd_require_number"`
	}

	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if p.MinLength < 6 {
		http.Error(w, "Minimum password length must be at least 6", http.StatusBadRequest)
		return
	}

	_, err := db.Exec(`UPDATE settings SET 
		pwd_min_length = ?, pwd_history_count = ?, pwd_validity_days = ?, 
		pwd_require_special = ?, pwd_require_upper = ?, pwd_require_lower = ?, pwd_require_number = ? 
		WHERE id = 1`,
		p.MinLength, p.HistoryCount, p.ValidityDays,
		p.RequireSpecial, p.RequireUpper, p.RequireLower, p.RequireNumber)

	if err != nil {
		http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "updated", "settings", "password policy", true)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Password policy updated successfully"})
}

func updateCVWorkerURL(w http.ResponseWriter, r *http.Request) {
	var p struct {
		CVWorkerURL string `json:"cv_worker_url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Clean up the URL to prevent trailing slash routing issues
	cleanURL := strings.TrimSpace(p.CVWorkerURL)
	cleanURL = strings.TrimRight(cleanURL, "/")

	_, err := db.Exec("UPDATE settings SET cv_worker_url = ? WHERE id = 1", cleanURL)

	if err != nil {
		http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "updated", "settings", "CV worker URL", true)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Microservice routing updated successfully"})
}

// Admin: Delete an external API credential
func deleteExternalCredential(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serviceName := vars["service"]

	_, err := db.Exec("DELETE FROM credentials WHERE service_name = ?", serviceName)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "deleted", "credentials", serviceName, true)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Credential deleted successfully"})
}

// Admin: Update external credentials (Write-Only)
func updateExternalCredentials(w http.ResponseWriter, r *http.Request) {
	var req CredentialUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ServiceName == "" || req.Secret == "" {
		http.Error(w, "Service name and secret are required", http.StatusBadRequest)
		return
	}

	// Retrieve the encryption key from the environment
	hexKey := os.Getenv("CREDENTIAL_ENCRYPTION_KEY")
	if hexKey == "" {
		http.Error(w, "Server encryption key is not configured", http.StatusInternalServerError)
		return
	}

	// Convert string to byte slice so it can be zeroed out in the helper
	plaintext := []byte(req.Secret)

	// Encrypt the secret (the helper automatically zeroes 'plaintext' upon return)
	ciphertext, nonce, err := Encrypt(plaintext, hexKey)
	if err != nil {
		log.Printf("Encryption failed for service %s: %v", req.ServiceName, err)
		http.Error(w, "Internal encryption error", http.StatusInternalServerError)
		return
	}

	// Upsert the credential into the database
	query := `
		INSERT INTO credentials (service_name, encrypted_blob, nonce, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(service_name) DO UPDATE SET
			encrypted_blob = excluded.encrypted_blob,
			nonce = excluded.nonce,
			updated_at = CURRENT_TIMESTAMP;
	`

	_, err = db.Exec(query, req.ServiceName, ciphertext, nonce)
	if err != nil {
		log.Printf("Database insertion failed for credential %s: %v", req.ServiceName, err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "updated", "credentials", req.ServiceName, true)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Credential updated successfully"})
}

// --- ADVANCED SETTINGS: STORM SLOT TIMES ---

// getAdvancedStormSlots returns all 3 storm slot time definitions.
// Available to all authenticated users (schedule page needs it).
func getAdvancedStormSlots(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT slot, label, time_st FROM storm_slot_times ORDER BY slot`)
	if err != nil {
		slog.Error("getAdvancedStormSlots query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	slots := []StormSlotTime{}
	for rows.Next() {
		var s StormSlotTime
		if err := rows.Scan(&s.Slot, &s.Label, &s.TimeST); err != nil {
			slog.Error("getAdvancedStormSlots scan", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		slots = append(slots, s)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(slots)
}

// putAdvancedStormSlots updates all 3 storm slot time definitions. Admin only.
func putAdvancedStormSlots(w http.ResponseWriter, r *http.Request) {
	var slots []StormSlotTime
	if err := json.NewDecoder(r.Body).Decode(&slots); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	for _, s := range slots {
		if s.Slot < 1 || s.Slot > 3 {
			http.Error(w, "slot must be 1, 2, or 3", http.StatusBadRequest)
			return
		}
		if !reHHMM.MatchString(s.TimeST) {
			http.Error(w, "time_st must be HH:MM", http.StatusBadRequest)
			return
		}
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("putAdvancedStormSlots begin tx", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	for _, s := range slots {
		if _, err := tx.Exec(`UPDATE storm_slot_times SET label=?, time_st=? WHERE slot=?`,
			s.Label, s.TimeST, s.Slot); err != nil {
			slog.Error("putAdvancedStormSlots update", "error", err, "slot", s.Slot)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		slog.Error("putAdvancedStormSlots commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "updated", "settings", "storm slot times", true)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Storm slot times updated"})
}
