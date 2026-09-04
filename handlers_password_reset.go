// handlers_password_reset.go - Single-use password reset links.
//
// Replaces the old flow that generated a random plaintext password and displayed it to
// an admin to relay out-of-band. Here the user sets their own password via a one-time
// link, so no intermediary ever sees it.
//
// Closely mirrors handlers_invite.go — read that alongside this.

package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
)

// Shorter than the invite window (48h): this is a reset for an existing account, not
// first-time onboarding, so the holder is expected to act promptly.
const passwordResetTokenTTL = "+24 hours"
const passwordResetTTLLabel = "24 hours"

// Target-side failures from createPasswordResetToken. Sentinels rather than a bare
// error so both handlers can map them to the right HTTP status without string-matching.
var (
	errResetUserNotFound  = errors.New("user not found")
	errResetUserInactive  = errors.New("user is deactivated")
	errResetTargetIsAdmin = errors.New("target is an admin account")
)

type ResetPageData struct {
	CSRFToken         template.HTML
	Username          string
	Error             string
	PwdMinLength      int
	PwdRequireUpper   bool
	PwdRequireLower   bool
	PwdRequireNumber  bool
	PwdRequireSpecial bool
}

// createPasswordResetToken mints a single-use reset link for userID.
//
// It owns every target-side rule so neither caller can forget one. In particular the
// admin guard lives here, not in the member-keyed handler: "member-linked" is not a
// privilege boundary, since a member-linked account can be an admin account. Without
// this an R5 could mint a reset link for the alliance leader, claim it, and be
// auto-logged-in as them.
func createPasswordResetToken(userID int, actor *AuthUser) (string, string, error) {
	var username string
	var isActive, targetIsAdmin bool
	err := db.QueryRow(
		"SELECT username, is_active, is_admin FROM users WHERE id = ?", userID,
	).Scan(&username, &isActive, &targetIsAdmin)
	if err != nil {
		return "", "", errResetUserNotFound
	}
	if targetIsAdmin && !actor.IsAdmin {
		return "", "", errResetTargetIsAdmin
	}
	if !isActive {
		return "", "", errResetUserInactive
	}

	// One active link at a time, same as invites.
	if _, err := db.Exec(
		"DELETE FROM password_reset_tokens WHERE user_id = ? AND used_at IS NULL", userID,
	); err != nil {
		slog.Error("failed to clear existing reset tokens", "error", err, "userID", userID)
		return "", "", err
	}

	// Passive sweep — nothing else ever removes expired rows, since expiry is only
	// enforced at read time.
	if _, err := db.Exec(
		"DELETE FROM password_reset_tokens WHERE expires_at < CURRENT_TIMESTAMP",
	); err != nil {
		slog.Error("failed to sweep expired reset tokens", "error", err)
		// Non-fatal: housekeeping shouldn't block issuing a link.
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		slog.Error("failed to generate reset token", "error", err)
		return "", "", err
	}
	token := hex.EncodeToString(raw)

	// expires_at is computed in SQL, never bound from a Go time.Time. The driver's
	// default write format is time.Time.String() ("... +0200 CEST"), which compares
	// lexically against UTC CURRENT_TIMESTAMP and silently shifts the TTL by the host's
	// UTC offset. datetime('now', ...) yields exactly CURRENT_TIMESTAMP's shape and basis.
	if _, err := db.Exec(
		`INSERT INTO password_reset_tokens (token, user_id, created_by, expires_at)
		 VALUES (?, ?, ?, datetime('now', ?))`,
		token, userID, actor.ID, passwordResetTokenTTL,
	); err != nil {
		slog.Error("failed to insert reset token", "error", err, "userID", userID)
		return "", "", err
	}

	logActivity(actor.ID, actor.Username, "created", "password_reset_link", username, true,
		"expires in "+passwordResetTTLLabel)

	return token, username, nil
}

// writeResetTokenError maps a createPasswordResetToken sentinel to an HTTP response.
// These strings are written by us, not sourced from the DB, so they are safe to return
// verbatim — and they reach the user as the toast text, so they are the actual UI copy.
func writeResetTokenError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errResetTargetIsAdmin):
		http.Error(w, "You don't have permission to reset an administrator's password. Ask an admin to issue this link.", http.StatusForbidden)
	case errors.Is(err, errResetUserInactive):
		http.Error(w, "This account is deactivated. An admin must reactivate it before a reset link can be issued.", http.StatusConflict)
	case errors.Is(err, errResetUserNotFound):
		http.Error(w, "No user account exists for this member.", http.StatusNotFound)
	default:
		http.Error(w, "Database error", http.StatusInternalServerError)
	}
}

func writeResetLinkResponse(w http.ResponseWriter, token, username string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"reset_url":  "/reset-password/" + token,
		"expires_in": passwordResetTTLLabel,
		"username":   username,
	})
}

// POST /api/admin/users/{id}/reset-link — admin-only, keyed on user id.
func generateUserResetLink(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	token, username, err := createPasswordResetToken(userID, getAuthUser(r))
	if err != nil {
		writeResetTokenError(w, err)
		return
	}
	writeResetLinkResponse(w, token, username)
}

// POST /api/members/{id}/reset-link — manage_settings, keyed on member id.
//
// Resolving member -> user is what restricts R5s to member-linked accounts; the admin
// guard inside createPasswordResetToken is what stops that reaching an admin account.
func generateMemberResetLink(w http.ResponseWriter, r *http.Request) {
	memberID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid member ID", http.StatusBadRequest)
		return
	}

	var userID int
	if err := db.QueryRow("SELECT id FROM users WHERE member_id = ?", memberID).Scan(&userID); err != nil {
		http.Error(w, "No user account exists for this member.", http.StatusNotFound)
		return
	}

	token, username, err := createPasswordResetToken(userID, getAuthUser(r))
	if err != nil {
		writeResetTokenError(w, err)
		return
	}
	writeResetLinkResponse(w, token, username)
}

// GET /reset-password/{token} — render the reset page (unauthenticated).
func showResetPasswordPage(w http.ResponseWriter, r *http.Request) {
	// Same oracle as the invite page: this reports whether a reset token is valid, without
	// the bcrypt cost the POST pays. Throttling only the POST would leave the cheap path open.
	if !getLoginLimiter(getClientIP(r)).Allow() {
		slog.Warn("password reset page rate limit exceeded", "ip", getClientIP(r))
		http.Error(w, "Too many attempts. Please try again later.", http.StatusTooManyRequests)
		return
	}

	token := mux.Vars(r)["token"]

	data := ResetPageData{CSRFToken: csrf.TemplateField(r)}

	var username string
	// u.is_active guards the case where the account was deactivated after the link was
	// issued — the link must stop working immediately.
	err := db.QueryRow(`
		SELECT u.username
		FROM password_reset_tokens t
		JOIN users u ON u.id = t.user_id
		WHERE t.token = ? AND t.used_at IS NULL AND t.expires_at > CURRENT_TIMESTAMP
		  AND u.is_active = 1`,
		token,
	).Scan(&username)

	if err != nil {
		data.Error = "This password reset link has already been used or has expired."
		renderResetPage(w, data)
		return
	}

	data.Username = username

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

	renderResetPage(w, data)
}

// POST /reset-password/{token} — claim the link and set a new password (unauthenticated).
func claimPasswordReset(w http.ResponseWriter, r *http.Request) {
	// Unauthenticated endpoint that runs bcrypt per request — same per-IP throttle as login.
	if !getLoginLimiter(getClientIP(r)).Allow() {
		slog.Warn("password reset rate limit exceeded", "ip", getClientIP(r))
		http.Error(w, "Too many attempts. Please try again later.", http.StatusTooManyRequests)
		return
	}

	token := mux.Vars(r)["token"]

	var req struct {
		Password        string `json:"password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Password != req.ConfirmPassword {
		http.Error(w, "Passwords do not match", http.StatusBadRequest)
		return
	}

	// Everything that touches db must happen BEFORE db.Begin(): with
	// SetMaxOpenConns(1) a query issued inside the transaction would wait forever for a
	// connection the transaction itself holds. validatePasswordPolicy runs a db.Query.
	var userID int
	var username string
	var memberID sql.NullInt64
	var isAdmin bool
	err := db.QueryRow(`
		SELECT u.id, u.username, u.member_id, u.is_admin
		FROM password_reset_tokens t
		JOIN users u ON u.id = t.user_id
		WHERE t.token = ? AND t.used_at IS NULL AND t.expires_at > CURRENT_TIMESTAMP
		  AND u.is_active = 1`,
		token,
	).Scan(&userID, &username, &memberID, &isAdmin)
	if err != nil {
		http.Error(w, "This password reset link has already been used or has expired.", http.StatusGone)
		return
	}

	// Passing the real userID (unlike the invite claim, which passes 0) enables the
	// password-history reuse check.
	if err := validatePasswordPolicy(req.Password, userID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("failed to hash password during reset claim", "error", err)
		http.Error(w, "Failed to process password", http.StatusInternalServerError)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("failed to begin reset claim transaction", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Re-validate under the transaction to guard against replay / concurrent claims.
	var txUserID int
	err = tx.QueryRow(`
		SELECT t.user_id FROM password_reset_tokens t
		JOIN users u ON u.id = t.user_id
		WHERE t.token = ? AND t.used_at IS NULL AND t.expires_at > CURRENT_TIMESTAMP
		  AND u.is_active = 1`,
		token,
	).Scan(&txUserID)
	if err != nil {
		http.Error(w, "This password reset link has already been used or has expired.", http.StatusGone)
		return
	}

	// password_changed_at must be updated too: login derives password expiry from it, so
	// leaving it stale would bounce the user straight back into the force-change flow.
	if _, err := tx.Exec(
		"UPDATE users SET password = ?, force_password_change = 0, password_changed_at = CURRENT_TIMESTAMP WHERE id = ?",
		string(hashedPassword), txUserID,
	); err != nil {
		slog.Error("failed to update password during reset claim", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if _, err := tx.Exec(
		"INSERT INTO password_history (user_id, password_hash) VALUES (?, ?)",
		txUserID, string(hashedPassword),
	); err != nil {
		slog.Error("failed to insert password history during reset claim", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// WHERE used_at IS NULL guards against a concurrent claim winning the race.
	res, err := tx.Exec(
		"UPDATE password_reset_tokens SET used_at = CURRENT_TIMESTAMP WHERE token = ? AND used_at IS NULL", token,
	)
	if err != nil {
		slog.Error("failed to mark reset token used", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if rowsAffected, _ := res.RowsAffected(); rowsAffected == 0 {
		http.Error(w, "This password reset link has already been used.", http.StatusConflict)
		return
	}

	if err := tx.Commit(); err != nil {
		slog.Error("failed to commit reset claim transaction", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	logActivity(userID, username, "reset", "password_reset_link", username, true)
	trackLogin(userID, username, r, true)

	// Session mirrors forceChangePassword, not claimInvite: this logs in an arbitrary
	// EXISTING user, who may be an admin and may have no linked member. claimInvite
	// hardcodes is_admin=false and always sets member_id, which is correct for a fresh
	// invite account but wrong here.
	session, _ := store.Get(r, "session")
	delete(session.Values, "force_change_user_id")
	session.Values["authenticated"] = true
	session.Values["username"] = username
	session.Values["user_id"] = userID
	session.Values["is_admin"] = isAdmin
	if memberID.Valid {
		session.Values["member_id"] = int(memberID.Int64)
	}
	session.Save(r, w)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func renderResetPage(w http.ResponseWriter, data ResetPageData) {
	noStoreHTML(w)

	t, err := parseTemplates("templates/reset_password.html")
	if err != nil {
		slog.Error("failed to parse reset password template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if err := t.Execute(w, data); err != nil {
		slog.Error("failed to execute reset password template", "error", err)
	}
}
