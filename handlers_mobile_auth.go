package main

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const mobileTokenExpiry = 7 * 24 * time.Hour

func mobileLogin(w http.ResponseWriter, r *http.Request) {
	var creds Credentials
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var user User
	var memberID sql.NullInt64
	var isAdmin sql.NullBool
	var forcePasswordChange bool

	err := db.QueryRow(`
		SELECT u.id, u.username, u.password, u.member_id, u.is_admin, u.force_password_change
		FROM users u WHERE u.username = ?`, creds.Username).Scan(
		&user.ID, &user.Username, &user.Password, &memberID, &isAdmin, &forcePasswordChange)

	if err != nil {
		trackLogin(0, creds.Username, r, false)
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	if memberID.Valid {
		mid := int(memberID.Int64)
		user.MemberID = &mid
	}
	user.IsAdmin = isAdmin.Valid && isAdmin.Bool

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(creds.Password)); err != nil {
		trackLogin(user.ID, user.Username, r, false)
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	trackLogin(user.ID, user.Username, r, true)

	if forcePasswordChange {
		http.Error(w, "Password change required. Please log in via the web interface to update your password before using the mobile app.", http.StatusForbidden)
		return
	}

	// Resolve permissions
	var manageVS, manageMembers bool
	if user.IsAdmin {
		manageVS = true
		manageMembers = true
	} else if user.MemberID != nil {
		var rank string
		if err := db.QueryRow("SELECT rank FROM members WHERE id = ?", *user.MemberID).Scan(&rank); err == nil {
			perms := getRankPermissions(rank)
			manageVS = perms.ManageVSPoints
			manageMembers = perms.ManageMembers
		}
	}

	secretKey := os.Getenv("SESSION_KEY")
	if secretKey == "" {
		slog.Error("mobileLogin: SESSION_KEY not set; cannot issue mobile token")
		http.Error(w, "Server configuration error", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	expiresAt := now.Add(mobileTokenExpiry)
	claims := MobileTokenClaims{
		UserID:        user.ID,
		Username:      user.Username,
		MemberID:      user.MemberID,
		IsAdmin:       user.IsAdmin,
		ManageVS:      manageVS,
		ManageMembers: manageMembers,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			Subject:   user.Username,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(secretKey))
	if err != nil {
		slog.Error("mobileLogin: failed to sign token", "error", err)
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token":      tokenStr,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
		"user_id":    user.ID,
		"username":   user.Username,
		"member_id":  user.MemberID,
		"is_admin":   user.IsAdmin,
		"permissions": map[string]bool{
			"manage_vs_points": manageVS,
			"manage_members":   manageMembers,
		},
	})
}
