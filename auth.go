package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
)

// initSessionStore initializes the session store with secure settings
func initSessionStore() {
	sessionKey := os.Getenv("SESSION_KEY")
	if sessionKey == "" {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			log.Fatal("Failed to generate random session key: ", err)
		}
		sessionKey = hex.EncodeToString(key)
		log.Println("WARNING: No SESSION_KEY environment variable set. Using generated key (not persistent across restarts).")
	}

	key, err := hex.DecodeString(sessionKey)
	if err != nil || len(key) != 32 {
		key = []byte(sessionKey)
		if len(key) < 32 {
			padded := make([]byte, 32)
			copy(padded, key)
			key = padded
		}
	}

	store = sessions.NewCookieStore(key[:32])

	isProduction := os.Getenv("PRODUCTION") == "true" || os.Getenv("HTTPS") == "true"

	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   SessionMaxAge,
		HttpOnly: true,
		Secure:   isProduction,
		SameSite: http.SameSiteStrictMode,
	}
}

// Get client IP with X-Forwarded-For and X-Real-IP support
func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	ip := r.RemoteAddr
	if colonIndex := strings.LastIndex(ip, ":"); colonIndex != -1 {
		ip = ip[:colonIndex]
	}
	return ip
}

// Get IP geolocation information using ip-api.com
func getIPGeolocation(ip string) (*IPGeolocation, error) {
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

// Track login attempt in database. Geolocation is resolved asynchronously so it
// never blocks the login response.
func trackLogin(userID int, username string, r *http.Request, success bool) {
	ip := getClientIP(r)
	userAgent := r.Header.Get("User-Agent")

	go func() {
		var country, city, isp *string

		if geo, err := getIPGeolocation(ip); err == nil {
			country = &geo.Country
			city = &geo.City
			isp = &geo.ISP
		}

		if _, err := db.Exec(`INSERT INTO login_sessions (user_id, username, ip_address, user_agent, country, city, isp, success)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			userID, username, ip, userAgent, country, city, isp, success); err != nil {
			log.Printf("Failed to track login: %v", err)
		}
	}()
}

func login(w http.ResponseWriter, r *http.Request) {
	var creds Credentials
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var user User
	var memberID sql.NullInt64
	var isAdmin sql.NullBool

	var forcePasswordChange bool
	var isExpired bool
	var minLen int
	var reqSpecial, reqUpper, reqLower, reqNumber bool

	err := db.QueryRow(`
		SELECT u.id, u.username, u.password, u.member_id, u.is_admin, u.force_password_change,
		       CASE WHEN s.pwd_validity_days > 0 AND (julianday('now') - julianday(u.password_changed_at)) > s.pwd_validity_days THEN 1 ELSE 0 END as expired,
		       s.pwd_min_length, s.pwd_require_special, s.pwd_require_upper, s.pwd_require_lower, s.pwd_require_number
		FROM users u CROSS JOIN settings s WHERE s.id = 1 AND u.username = ?`, creds.Username).Scan(
		&user.ID, &user.Username, &user.Password, &memberID, &isAdmin, &forcePasswordChange, &isExpired,
		&minLen, &reqSpecial, &reqUpper, &reqLower, &reqNumber)

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

	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(creds.Password))
	if err != nil {
		trackLogin(user.ID, user.Username, r, false)
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	trackLogin(user.ID, user.Username, r, true)

	if forcePasswordChange || isExpired {
		session, _ := store.Get(r, "session")
		session.Values["force_change_user_id"] = user.ID
		session.Save(r, w)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"requires_password_change": true,
			"message":                  "You must change your password to continue.",
			"policy": map[string]interface{}{
				"min_length":      minLen,
				"require_special": reqSpecial,
				"require_upper":   reqUpper,
				"require_lower":   reqLower,
				"require_number":  reqNumber,
			},
		})
		return
	}

	session, _ := store.Get(r, "session")
	delete(session.Values, "force_change_user_id")
	session.Values["authenticated"] = true
	session.Values["username"] = user.Username
	session.Values["user_id"] = user.ID
	if user.MemberID != nil {
		session.Values["member_id"] = *user.MemberID
	}
	session.Values["is_admin"] = user.IsAdmin
	session.Save(r, w)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":  "Login successful",
		"username": user.Username,
	})
}

func forceChangePassword(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, ok := session.Values["force_change_user_id"].(int)
	if !ok {
		http.Error(w, `{"message": "Unauthorized or session expired"}`, http.StatusUnauthorized)
		return
	}

	var req struct {
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"message": "Invalid request"}`, http.StatusBadRequest)
		return
	}

	if err := validatePasswordPolicy(req.NewPassword, userID); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
		return
	}

	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)

	_, err := db.Exec("UPDATE users SET password = ?, force_password_change = 0, password_changed_at = CURRENT_TIMESTAMP WHERE id = ?", string(hashedPassword), userID)
	if err != nil {
		http.Error(w, `{"message": "Failed to update password"}`, http.StatusInternalServerError)
		return
	}
	db.Exec("INSERT INTO password_history (user_id, password_hash) VALUES (?, ?)", userID, string(hashedPassword))

	var user User
	db.QueryRow("SELECT username, member_id, is_admin FROM users WHERE id = ?", userID).Scan(&user.Username, &user.MemberID, &user.IsAdmin)

	delete(session.Values, "force_change_user_id")
	session.Values["authenticated"] = true
	session.Values["user_id"] = userID
	session.Values["username"] = user.Username
	session.Values["is_admin"] = user.IsAdmin
	if user.MemberID != nil {
		session.Values["member_id"] = *user.MemberID
	}
	session.Save(r, w)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Password changed and logged in successfully"})
}

func logout(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	session.Values["authenticated"] = false
	session.Options.MaxAge = -1
	session.Save(r, w)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Logout successful"})
}

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

	var currentHash string
	err := db.QueryRow("SELECT password FROM users WHERE id = ?", userID).Scan(&currentHash)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(input.CurrentPassword))
	if err != nil {
		http.Error(w, "Current password is incorrect", http.StatusUnauthorized)
		return
	}

	if err := validatePasswordPolicy(input.NewPassword, userID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec("UPDATE users SET password = ?, force_password_change = 0, password_changed_at = CURRENT_TIMESTAMP WHERE id = ?", string(newHash), userID)
	if err != nil {
		http.Error(w, "Failed to update password", http.StatusInternalServerError)
		return
	}

	db.Exec("INSERT INTO password_history (user_id, password_hash) VALUES (?, ?)", userID, string(newHash))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Password changed successfully"})
}

func checkAuth(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	if auth, ok := session.Values["authenticated"].(bool); ok && auth {
		username := session.Values["username"].(string)
		isAdmin := false
		if adminVal, ok := session.Values["is_admin"].(bool); ok {
			isAdmin = adminVal
		}

		var rank string
		var perms RankPermissions

		if isAdmin {
			rank = "Admin"
			perms = RankPermissions{ViewTrain: true, ManageTrain: true, ViewAwards: true, ManageAwards: true, ViewRecs: true, ManageRecs: true, ViewDyno: true, ManageDyno: true, ViewRankings: true, ViewStorm: true, ManageStorm: true, ViewVSPoints: true, ManageVSPoints: true, ViewUpload: true, ManageMembers: true, ManageSettings: true}
		} else if memberID, ok := session.Values["member_id"].(int); ok {
			err := db.QueryRow("SELECT rank FROM members WHERE id = ?", memberID).Scan(&rank)
			if err == nil {
				perms = getRankPermissions(rank)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated": true,
			"username":      username,
			"rank":          rank,
			"is_admin":      isAdmin,
			"permissions":   perms,
		})
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"authenticated": false})
	}
}

func validatePasswordPolicy(password string, userID int) error {
	var s Settings
	// Updated query to include pwd_require_lower
	err := db.QueryRow("SELECT pwd_min_length, pwd_require_special, pwd_require_upper, pwd_require_lower, pwd_require_number, pwd_history_count FROM settings WHERE id = 1").Scan(
		&s.PwdMinLength, &s.PwdRequireSpecial, &s.PwdRequireUpper, &s.PwdRequireLower, &s.PwdRequireNumber, &s.PwdHistoryCount,
	)
	if err != nil {
		return err
	}

	if len(password) < s.PwdMinLength {
		return fmt.Errorf("password must be at least %d characters", s.PwdMinLength)
	}
	if s.PwdRequireUpper && !regexp.MustCompile(`[A-Z]`).MatchString(password) {
		return fmt.Errorf("password must contain an uppercase letter")
	}
	if s.PwdRequireLower && !regexp.MustCompile(`[a-z]`).MatchString(password) { // NEW
		return fmt.Errorf("password must contain a lowercase letter")
	}
	if s.PwdRequireNumber && !regexp.MustCompile(`[0-9]`).MatchString(password) {
		return fmt.Errorf("password must contain a number")
	}
	if s.PwdRequireSpecial && !regexp.MustCompile(`[^a-zA-Z0-9]`).MatchString(password) {
		return fmt.Errorf("password must contain a special character")
	}

	if s.PwdHistoryCount > 0 && userID > 0 {
		rows, err := db.Query("SELECT password_hash FROM password_history WHERE user_id = ? ORDER BY created_at DESC LIMIT ?", userID, s.PwdHistoryCount)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var hash string
				rows.Scan(&hash)
				if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil {
					return fmt.Errorf("password has been used recently and cannot be reused")
				}
			}
		}
	}
	return nil
}
