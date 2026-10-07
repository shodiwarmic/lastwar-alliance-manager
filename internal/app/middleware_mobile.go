package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
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

		claims := &MobileTokenClaims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return tokenSecret(), nil
		})
		if err != nil || !token.Valid {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if claims.Issuer != "lastwar-alliance-manager" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// The JWT is stateless and lives 7 days, so signature validity says nothing about
		// whether the account is still entitled to access. Passing IssuedAt also makes a
		// password change revoke outstanding tokens.
		if !jwtSubjectStillValid(claims.UserID, claims.IssuedAt) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Identity and permissions are resolved live, as on the session path: the user
		// goes into the context under the same key, so getAuthUser, requirePermission and
		// userHasPermission work here unchanged. The token's permission claims are never
		// read for authorization — a rank change or a matrix edit takes effect on the
		// next request, not when the seven-day token expires.
		user := loadUserFromDB(claims.UserID)
		if user == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), mobileContextKey{}, claims)
		ctx = context.WithValue(ctx, authUserKey, user)
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

// mobileGate wraps a mobile route's handler in its permission gate: none (any signed-in
// user), requirePermission, or requireAnyPermission. The keys are RankPermissions JSON
// tags, the same as the web's (mobile_stores_test.go checks).
func mobileGate(perms []string, h http.HandlerFunc) http.HandlerFunc {
	switch len(perms) {
	case 0:
		return h
	case 1:
		return requirePermission(perms[0], h)
	default:
		return requireAnyPermission(perms, h)
	}
}

// registerMobileRoutes registers every route the store registry declares, behind the
// demo block and the bearer middleware (login has no token yet).
func registerMobileRoutes(router *mux.Router) {
	routes := mobileBaseRoutes()
	for _, st := range mobileStores() {
		routes = append(routes, st.Routes...)
	}
	for _, rt := range routes {
		h := mobileGate(rt.Perms, rt.Handler)
		if rt.Path != "/api/mobile/login" {
			h = mobileBearerMiddleware(h)
		}
		router.HandleFunc(rt.Path, demoBlock(h)).Methods(rt.Method)
	}
}
