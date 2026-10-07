package app

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const mobileTokenExpiry = 7 * 24 * time.Hour

func mobileLogin(w http.ResponseWriter, r *http.Request) {
	// The same per-IP limiter as the web login: this endpoint takes a password too, and
	// is CSRF-exempt. The scanner and collector log in once per seven-day token, so a
	// burst of five is far above anything a real client does.
	if !getLoginLimiter(getClientIP(r)).Allow() {
		slog.Warn("mobile login rate limit exceeded", "ip", getClientIP(r))
		http.Error(w, "Too many login attempts. Please try again later.", http.StatusTooManyRequests)
		return
	}

	var creds Credentials
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var user User
	var memberID sql.NullInt64
	var isAdmin sql.NullBool
	var forcePasswordChange bool
	var isActive bool

	err := db.QueryRow(`
		SELECT u.id, u.username, u.password, u.member_id, u.is_admin, u.force_password_change, u.is_active
		FROM users u WHERE u.username = ?`, creds.Username).Scan(
		&user.ID, &user.Username, &user.Password, &memberID, &isAdmin, &forcePasswordChange, &isActive)

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

	// After the password compare, same as the web login — don't leak account state.
	if !isActive {
		trackLogin(user.ID, user.Username, r, false)
		http.Error(w, "This account has been deactivated. Contact an alliance officer.", http.StatusForbidden)
		return
	}

	trackLogin(user.ID, user.Username, r, true)

	if forcePasswordChange {
		http.Error(w, "Password change required. Please log in via the web interface to update your password before using the mobile app.", http.StatusForbidden)
		return
	}

	// The permission flags are informational for the client (and kept in the token for
	// older clients); no mobile route reads them for authorization — every request
	// resolves the user's permissions live.
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
			Issuer:    "lastwar-alliance-manager",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(tokenSecret())
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
		"api_version": mobileAPIVersion,
	})
}
