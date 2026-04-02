// handlers_invite.go - Invite-link user onboarding flow.

package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
)

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9._\-]{3,30}$`)

type InvitePageData struct {
	CSRFToken         template.HTML
	MemberName        string
	DefaultUsername   string
	Error             string
	PwdMinLength      int
	PwdRequireUpper   bool
	PwdRequireLower   bool
	PwdRequireNumber  bool
	PwdRequireSpecial bool
}

// POST /api/members/{id}/invite — generate a single-use invite link for a member.
func generateInvite(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	memberID, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid member ID", http.StatusBadRequest)
		return
	}

	session, _ := store.Get(r, "session")
	createdBy, ok := session.Values["user_id"].(int)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
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

	// Remove any previous unused tokens for this member (one active invite at a time).
	_, err = db.Exec("DELETE FROM invite_tokens WHERE member_id = ? AND used_at IS NULL", memberID)
	if err != nil {
		slog.Error("failed to clear existing invite tokens", "error", err, "memberID", memberID)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		slog.Error("failed to generate invite token", "error", err)
		http.Error(w, "Failed to generate invite token", http.StatusInternalServerError)
		return
	}
	token := hex.EncodeToString(raw)
	expiresAt := time.Now().Add(48 * time.Hour)

	_, err = db.Exec(
		"INSERT INTO invite_tokens (token, member_id, created_by, expires_at) VALUES (?, ?, ?, ?)",
		token, memberID, createdBy, expiresAt,
	)
	if err != nil {
		slog.Error("failed to insert invite token", "error", err, "memberID", memberID)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	actorName, _ := session.Values["username"].(string)
	logActivity(createdBy, actorName, "created", "invite", memberName, true, "expires in 48h")

	inviteURL := "/invite/" + token
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"invite_url": inviteURL,
		"expires_in": "48 hours",
	})
}

// GET /invite/{token} — render the invite acceptance page (unauthenticated).
func showInvitePage(w http.ResponseWriter, r *http.Request) {
	token := mux.Vars(r)["token"]

	data := InvitePageData{
		CSRFToken: csrf.TemplateField(r),
	}

	var memberID int
	var memberName string
	err := db.QueryRow(`
		SELECT it.member_id, m.name
		FROM invite_tokens it
		JOIN members m ON m.id = it.member_id
		WHERE it.token = ? AND it.used_at IS NULL AND it.expires_at > CURRENT_TIMESTAMP`,
		token,
	).Scan(&memberID, &memberName)

	if err != nil {
		data.Error = "This invite link has already been used or has expired."
		renderInvitePage(w, data)
		return
	}

	data.MemberName = memberName
	data.DefaultUsername = strings.ToLower(strings.ReplaceAll(memberName, " ", ""))

	var s Settings
	db.QueryRow(`SELECT pwd_min_length, pwd_require_upper, pwd_require_lower, pwd_require_number, pwd_require_special
		FROM settings WHERE id = 1`).Scan(
		&s.PwdMinLength, &s.PwdRequireUpper, &s.PwdRequireLower, &s.PwdRequireNumber, &s.PwdRequireSpecial,
	)
	data.PwdMinLength = s.PwdMinLength
	data.PwdRequireUpper = s.PwdRequireUpper
	data.PwdRequireLower = s.PwdRequireLower
	data.PwdRequireNumber = s.PwdRequireNumber
	data.PwdRequireSpecial = s.PwdRequireSpecial

	renderInvitePage(w, data)
}

// POST /invite/{token} — claim the invite and create the user account (unauthenticated).
func claimInvite(w http.ResponseWriter, r *http.Request) {
	token := mux.Vars(r)["token"]

	var req struct {
		Username        string `json:"username"`
		Password        string `json:"password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	req.Username = strings.TrimSpace(req.Username)

	if !usernameRe.MatchString(req.Username) {
		http.Error(w, "Username must be 3–30 characters: letters, numbers, . _ - only", http.StatusBadRequest)
		return
	}

	if req.Password != req.ConfirmPassword {
		http.Error(w, "Passwords do not match", http.StatusBadRequest)
		return
	}

	if err := validatePasswordPolicy(req.Password, 0); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Re-validate token under the transaction to guard against replay / concurrent claims.
	tx, err := db.Begin()
	if err != nil {
		slog.Error("failed to begin invite claim transaction", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var memberID int
	var memberName string
	err = tx.QueryRow(`
		SELECT it.member_id, m.name FROM invite_tokens it
		JOIN members m ON m.id = it.member_id
		WHERE it.token = ? AND it.used_at IS NULL AND it.expires_at > CURRENT_TIMESTAMP`,
		token,
	).Scan(&memberID, &memberName)
	if err != nil {
		http.Error(w, "This invite link has already been used or has expired.", http.StatusGone)
		return
	}

	// Check username uniqueness.
	var taken int
	tx.QueryRow("SELECT COUNT(1) FROM users WHERE username = ?", req.Username).Scan(&taken)
	if taken > 0 {
		http.Error(w, "Username is already taken", http.StatusConflict)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("failed to hash password during invite claim", "error", err)
		http.Error(w, "Failed to process password", http.StatusInternalServerError)
		return
	}

	result, err := tx.Exec(
		"INSERT INTO users (username, password, member_id, is_admin, force_password_change) VALUES (?, ?, ?, 0, 0)",
		req.Username, string(hashedPassword), memberID,
	)
	if err != nil {
		slog.Error("failed to insert user during invite claim", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	newUserID, err := result.LastInsertId()
	if err != nil {
		slog.Error("failed to get new user ID during invite claim", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec("INSERT INTO password_history (user_id, password_hash) VALUES (?, ?)", newUserID, string(hashedPassword))
	if err != nil {
		slog.Error("failed to insert password history during invite claim", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Mark token as used — WHERE used_at IS NULL guards against concurrent claims.
	res, err := tx.Exec("UPDATE invite_tokens SET used_at = CURRENT_TIMESTAMP WHERE token = ? AND used_at IS NULL", token)
	if err != nil {
		slog.Error("failed to mark invite token used", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		// Another concurrent claim won the race.
		http.Error(w, "Invite already used", http.StatusConflict)
		return
	}

	if err := tx.Commit(); err != nil {
		slog.Error("failed to commit invite claim transaction", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	logActivity(int(newUserID), req.Username, "accepted", "invite", req.Username, true, "linked to member: "+memberName)

	session, _ := store.Get(r, "session")
	session.Values["authenticated"] = true
	session.Values["username"] = req.Username
	session.Values["user_id"] = int(newUserID)
	session.Values["member_id"] = memberID
	session.Values["is_admin"] = false
	session.Save(r, w)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func renderInvitePage(w http.ResponseWriter, data InvitePageData) {
	t, err := template.ParseFiles("templates/invite.html")
	if err != nil {
		slog.Error("failed to parse invite template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if err := t.Execute(w, data); err != nil {
		slog.Error("failed to execute invite template", "error", err)
	}
}
