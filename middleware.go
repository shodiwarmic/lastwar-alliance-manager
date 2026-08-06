package main

import (
	"context"
	"database/sql"
	"net/http"
	"os"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const authUserKey contextKey = "auth_user"

// AuthUser holds live DB-sourced identity for the current request.
// It is populated by authMiddleware and accessed via getAuthUser.
type AuthUser struct {
	ID       int
	Username string
	IsAdmin  bool
	MemberID *int
	Rank     string
}

// getAuthUser returns the AuthUser injected by authMiddleware, or nil.
func getAuthUser(r *http.Request) *AuthUser {
	u, _ := r.Context().Value(authUserKey).(*AuthUser)
	return u
}

// loadUserFromDB fetches a fresh AuthUser from the database.
// Returns nil if the user does not exist OR has been deactivated.
//
// This is the single chokepoint for is_active on the session-cookie paths: it is called
// by both authMiddleware (API routes -> 401) and getPageData (page routes -> /login
// redirect via !IsAuthenticated), so one check here covers both with the correct
// response shape on each side. Do not duplicate it in authMiddleware.
func loadUserFromDB(userID int) *AuthUser {
	user := &AuthUser{ID: userID}
	var memberID sql.NullInt64
	var isActive bool
	err := db.QueryRow(
		"SELECT username, is_admin, member_id, is_active FROM users WHERE id = ?", userID,
	).Scan(&user.Username, &user.IsAdmin, &memberID, &isActive)
	if err != nil || !isActive {
		return nil
	}
	if memberID.Valid {
		mid := int(memberID.Int64)
		user.MemberID = &mid
		db.QueryRow("SELECT rank FROM members WHERE id = ?", mid).Scan(&user.Rank)
	}
	return user
}

// jwtSubjectStillValid re-checks a stateless JWT's subject against the database.
//
// Signature validity alone is not enough: our tokens are long-lived (mobile 7 days, WOPI
// 10 hours) and carry no server-side state, so without this a deactivated user keeps
// access until natural expiry. One indexed QueryRow per request — strictly cheaper than
// what the session path already pays in loadUserFromDB.
//
// issuedAt opts into password-change revocation: when non-nil, a token minted before the
// user's last password change is rejected, so "change your password" actually revokes a
// stolen token. Pass nil to check liveness only — WOPI does, because invalidating an
// in-flight Collabora session on a routine password change risks losing unsaved edits.
func jwtSubjectStillValid(userID int, issuedAt *jwt.NumericDate) bool {
	var isActive bool
	var pwdChanged sql.NullString
	err := db.QueryRow(
		"SELECT is_active, password_changed_at FROM users WHERE id = ?", userID,
	).Scan(&isActive, &pwdChanged)
	// sql.ErrNoRows (hard-deleted user) lands here too — fail closed.
	if err != nil || !isActive {
		return false
	}

	if issuedAt != nil && pwdChanged.Valid {
		// Parse with lastRankParseTime, NOT a single layout. password_changed_at is
		// written by CURRENT_TIMESTAMP as "2006-01-02 15:04:05", but it never reaches us
		// in that shape: the column is DECLARED TIMESTAMP, and the driver parses
		// declared DATE/DATETIME/TIMESTAMP columns into a time.Time (rows.go ~195),
		// which database/sql then renders into our string as RFC3339Nano. Matching only
		// the space form would silently never match, degrading this check to a no-op.
		if t, ok := lastRankParseTime(pwdChanged.String); ok {
			// Strict After: a login in the same second as a password change is fine.
			if t.After(issuedAt.Time) {
				return false
			}
		}
		// An unparseable timestamp falls through as "no constraint" — the staleness
		// check degrades open, but is_active above has already failed closed.
	}
	return true
}

// authMiddleware verifies the session has a valid user_id, loads that user
// from the database, and injects the result into the request context.
// No authorization data is trusted from the session cookie.
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		userID, ok := session.Values["user_id"].(int)
		if !ok || userID == 0 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		user := loadUserFromDB(userID)
		if user == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Refresh the session cookie expiration (rolling session).
		session.Save(r, w)

		ctx := context.WithValue(r.Context(), authUserKey, user)
		next(w, r.WithContext(ctx))
	}
}

// requirePermission gates a handler behind a JSON permissions blob check.
// Admin users bypass the rank check. All data comes from the context set by
// authMiddleware — no session reads.
// Uses json_extract with a bound parameter — safer than the former fmt.Sprintf column approach.
// COALESCE handles missing keys (new permissions on old rows) as 0 → false.
// userHasPermission reports whether the user holds a given permission key. Admins always
// pass; a nil/rank-less user never does; otherwise it reads the rank_permissions JSON blob.
// Extracted from requirePermission so handlers can do per-field gating (e.g. omitting
// leadership-only fields from a response) without duplicating the query or the admin/nil rules.
func userHasPermission(user *AuthUser, permKey string) bool {
	if user == nil {
		return false
	}
	if user.IsAdmin {
		return true
	}
	if user.MemberID != nil && user.Rank != "" {
		var val int64
		query := `SELECT COALESCE(json_extract(permissions, '$.' || ?), 0) FROM rank_permissions WHERE rank = ?`
		if err := db.QueryRow(query, permKey, user.Rank).Scan(&val); err == nil && val != 0 {
			return true
		}
	}
	return false
}

func requirePermission(permKey string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := getAuthUser(r)
		if user == nil {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if userHasPermission(user, permKey) {
			next(w, r)
			return
		}
		http.Error(w, "Forbidden: You do not have permission to access this feature.", http.StatusForbidden)
	}
}

// adminMiddleware restricts a handler to admin users only.
// Reads IsAdmin from the context set by authMiddleware — no session reads.
func adminMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := getAuthUser(r)
		if user == nil || !user.IsAdmin {
			http.Error(w, "Forbidden: Admin access required", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// wopiAuthMiddleware verifies a WOPI JWT access token.
func wopiAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenStr := r.URL.Query().Get("access_token")
		if tokenStr == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		claims := &WOPIClaims{}
		secretKey := os.Getenv("SESSION_KEY")

		token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
			return []byte(secretKey), nil
		})

		if err != nil || !token.Valid {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		// Liveness only (nil issuedAt): a deactivated user loses their document session,
		// but a routine password change must not kill an in-flight edit.
		if !jwtSubjectStillValid(claims.UserID, nil) {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), "wopi_claims", claims)
		next(w, r.WithContext(ctx))
	}
}
