package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
)

// --- PERMISSIONS MANAGEMENT ---

func getRankPermissions(rank string) RankPermissions {
	var p RankPermissions
	p.Rank = rank
	db.QueryRow(`SELECT view_train, manage_train, view_awards, manage_awards, view_recs, manage_recs, view_dyno, manage_dyno, view_rankings, view_storm, manage_storm, view_vs_points, manage_vs_points, view_upload, manage_members, manage_settings, view_files, upload_files, manage_files FROM rank_permissions WHERE rank = ?`, rank).Scan(
		&p.ViewTrain, &p.ManageTrain, &p.ViewAwards, &p.ManageAwards, &p.ViewRecs, &p.ManageRecs, &p.ViewDyno, &p.ManageDyno, &p.ViewRankings, &p.ViewStorm, &p.ManageStorm, &p.ViewVSPoints, &p.ManageVSPoints, &p.ViewUpload, &p.ManageMembers, &p.ManageSettings, &p.ViewFiles, &p.UploadFiles, &p.ManageFiles,
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`UPDATE rank_permissions SET view_train=?, manage_train=?, view_awards=?, manage_awards=?, view_recs=?, manage_recs=?, view_dyno=?, manage_dyno=?, view_rankings=?, view_storm=?, manage_storm=?, view_vs_points=?, manage_vs_points=?, view_upload=?, manage_members=?, manage_settings=?, view_files=?, upload_files=?, manage_files=? WHERE rank=?`)
	for _, p := range matrix {
		stmt.Exec(p.ViewTrain, p.ManageTrain, p.ViewAwards, p.ManageAwards, p.ViewRecs, p.ManageRecs, p.ViewDyno, p.ManageDyno, p.ViewRankings, p.ViewStorm, p.ManageStorm, p.ViewVSPoints, p.ManageVSPoints, p.ViewUpload, p.ManageMembers, p.ManageSettings, p.ViewFiles, p.UploadFiles, p.ManageFiles, p.Rank)
	}
	stmt.Close()
	tx.Commit()

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
		http.Error(w, "Failed to create user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	db.Exec("INSERT INTO password_history (user_id, password_hash) VALUES (?, ?)", id, string(hashedPassword))

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

	if req.Username != "" {
		_, err = db.Exec("UPDATE users SET username = ?, member_id = ?, is_admin = ?, force_password_change = ? WHERE id = ?",
			req.Username, req.MemberID, req.IsAdmin, req.ForcePasswordChange, userID)
	} else {
		_, err = db.Exec("UPDATE users SET member_id = ?, is_admin = ?, force_password_change = ? WHERE id = ?",
			req.MemberID, req.IsAdmin, req.ForcePasswordChange, userID)
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
		http.Error(w, "Failed to reset password: "+err.Error(), http.StatusInternalServerError)
		return
	}

	db.Exec("INSERT INTO password_history (user_id, password_hash) VALUES (?, ?)", userID, string(hashedPassword))

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

// Create user for member
func createUserForMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	memberID, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid member ID", http.StatusBadRequest)
		return
	}

	var memberName string
	err = db.QueryRow("SELECT name FROM members WHERE id = ?", memberID).Scan(&memberName)
	if err != nil {
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}

	var existingUserID int
	err = db.QueryRow("SELECT id FROM users WHERE member_id = ?", memberID).Scan(&existingUserID)
	if err == nil {
		http.Error(w, "User already exists for this member", http.StatusConflict)
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

	username := strings.ToLower(strings.ReplaceAll(memberName, " ", ""))

	var existingUsername string
	err = db.QueryRow("SELECT username FROM users WHERE username = ?", username).Scan(&existingUsername)
	if err == nil {
		username = username + strconv.Itoa(memberID)
	}

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

// --- SETTINGS MANAGEMENT ---

func getSettings(w http.ResponseWriter, r *http.Request) {
	var s Settings

	err := db.QueryRow(`SELECT 
		id, award_first_points, award_second_points, award_third_points, 
		recommendation_points, recent_conductor_penalty_days, 
		above_average_conductor_penalty, r4r5_rank_boost, 
		first_time_conductor_boost, schedule_message_template, 
		daily_message_template, power_tracking_enabled,
		COALESCE(storm_timezones, ''), COALESCE(storm_respect_dst, 0), COALESCE(login_message, ''), COALESCE(max_hq_level, 35) as max_hq_level,
		COALESCE(pwd_min_length, 12), COALESCE(pwd_require_special, 0), 
		COALESCE(pwd_require_upper, 0), COALESCE(pwd_require_lower, 0), 
		COALESCE(pwd_require_number, 0), COALESCE(pwd_history_count, 4), 
		COALESCE(pwd_validity_days, 180), COALESCE(squad_tracking_enabled, 0)
		FROM settings WHERE id = 1`).Scan(
		&s.ID, &s.AwardFirstPoints, &s.AwardSecondPoints, &s.AwardThirdPoints,
		&s.RecommendationPoints, &s.RecentConductorPenaltyDays,
		&s.AboveAverageConductorPenalty, &s.R4R5RankBoost,
		&s.FirstTimeConductorBoost, &s.ScheduleMessageTemplate,
		&s.DailyMessageTemplate, &s.PowerTrackingEnabled,
		&s.StormTimezones, &s.StormRespectDST, &s.LoginMessage, &s.MaxHQLevel,
		&s.PwdMinLength, &s.PwdRequireSpecial, &s.PwdRequireUpper,
		&s.PwdRequireLower, &s.PwdRequireNumber, &s.PwdHistoryCount, &s.PwdValidityDays,
		&s.SquadTrackingEnabled,
	)

	if err != nil {
		http.Error(w, "Failed to load settings", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}

func updateSettings(w http.ResponseWriter, r *http.Request) {
	var settings Settings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	session, _ := store.Get(r, "session")
	isAdmin, _ := session.Values["is_admin"].(bool)

	_, err := db.Exec(`UPDATE settings SET 
		award_first_points = ?, award_second_points = ?, award_third_points = ?, 
		recommendation_points = ?, recent_conductor_penalty_days = ?, above_average_conductor_penalty = ?,
		r4r5_rank_boost = ?, first_time_conductor_boost = ?, schedule_message_template = ?,
		daily_message_template = ?, power_tracking_enabled = ?, storm_timezones = ?,
		storm_respect_dst = ?, login_message = ?, max_hq_level = ?, squad_tracking_enabled = ?
		WHERE id = 1`,
		settings.AwardFirstPoints, settings.AwardSecondPoints, settings.AwardThirdPoints,
		settings.RecommendationPoints, settings.RecentConductorPenaltyDays, settings.AboveAverageConductorPenalty,
		settings.R4R5RankBoost, settings.FirstTimeConductorBoost, settings.ScheduleMessageTemplate,
		settings.DailyMessageTemplate, settings.PowerTrackingEnabled, settings.StormTimezones,
		settings.StormRespectDST, settings.LoginMessage, settings.MaxHQLevel, settings.SquadTrackingEnabled,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

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
