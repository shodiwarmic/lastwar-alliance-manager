package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/golang-jwt/jwt/v5"
)

// Authentication middleware
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		if auth, ok := session.Values["authenticated"].(bool); !ok || !auth {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Refresh the session cookie expiration (Rolling Session)
		session.Save(r, w)

		next(w, r)
	}
}

// Permission matrix checker
func requirePermission(permColumn string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")

		if isAdmin, ok := session.Values["is_admin"].(bool); ok && isAdmin {
			next(w, r)
			return
		}

		if memberID, ok := session.Values["member_id"].(int); ok {
			var rank string
			if err := db.QueryRow("SELECT rank FROM members WHERE id = ?", memberID).Scan(&rank); err == nil {
				var hasPerm bool
				query := fmt.Sprintf("SELECT %s FROM rank_permissions WHERE rank = ?", permColumn)
				if err := db.QueryRow(query, rank).Scan(&hasPerm); err == nil && hasPerm {
					next(w, r)
					return
				}
			}
		}

		http.Error(w, "Forbidden: You do not have permission to access this feature.", http.StatusForbidden)
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

// WOPI document lock and verification middleware
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
