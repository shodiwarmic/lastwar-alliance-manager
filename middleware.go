package main

import (
	"context"
	"database/sql"
	"fmt"
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
// Returns nil if the user does not exist.
func loadUserFromDB(userID int) *AuthUser {
	user := &AuthUser{ID: userID}
	var memberID sql.NullInt64
	err := db.QueryRow(
		"SELECT username, is_admin, member_id FROM users WHERE id = ?", userID,
	).Scan(&user.Username, &user.IsAdmin, &memberID)
	if err != nil {
		return nil
	}
	if memberID.Valid {
		mid := int(memberID.Int64)
		user.MemberID = &mid
		db.QueryRow("SELECT rank FROM members WHERE id = ?", mid).Scan(&user.Rank)
	}
	return user
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

// requirePermission gates a handler behind a rank_permissions column check.
// Admin users bypass the rank check. All data comes from the context set by
// authMiddleware — no session reads.
func requirePermission(permColumn string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := getAuthUser(r)
		if user == nil {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		if user.IsAdmin {
			next(w, r)
			return
		}

		if user.MemberID != nil && user.Rank != "" {
			var hasPerm bool
			query := fmt.Sprintf("SELECT %s FROM rank_permissions WHERE rank = ?", permColumn)
			if err := db.QueryRow(query, user.Rank).Scan(&hasPerm); err == nil && hasPerm {
				next(w, r)
				return
			}
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

		ctx := context.WithValue(r.Context(), "wopi_claims", claims)
		next(w, r.WithContext(ctx))
	}
}
