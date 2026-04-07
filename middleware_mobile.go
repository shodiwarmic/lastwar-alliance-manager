package main

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// mobileContextKey is a typed context key to avoid collisions with WOPI and other middleware.
type mobileContextKey struct{}

func mobileBearerMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

		secretKey := os.Getenv("SESSION_KEY")
		claims := &MobileTokenClaims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
			return []byte(secretKey), nil
		})
		if err != nil || !token.Valid {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), mobileContextKey{}, claims)
		next(w, r.WithContext(ctx))
	}
}

// getMobileClaims retrieves the mobile JWT claims stored by mobileBearerMiddleware.
// Panics if called outside the mobile middleware chain (programming error).
func getMobileClaims(r *http.Request) *MobileTokenClaims {
	claims, ok := r.Context().Value(mobileContextKey{}).(*MobileTokenClaims)
	if !ok {
		panic("getMobileClaims called outside mobile bearer middleware")
	}
	return claims
}

// requireMobilePermission checks a specific permission from the JWT claims.
// Supported permission strings: "manage_vs", "manage_members", "is_admin".
func requireMobilePermission(permission string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := getMobileClaims(r)
		var allowed bool
		switch permission {
		case "manage_vs":
			allowed = claims.ManageVS || claims.IsAdmin
		case "manage_members":
			allowed = claims.ManageMembers || claims.IsAdmin
		case "is_admin":
			allowed = claims.IsAdmin
		}
		if !allowed {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}
