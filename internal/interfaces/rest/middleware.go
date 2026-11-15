package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/romero-project/romero/internal/domain/identity"
)

type authContextKey struct{}

// Authenticator validates raw Bearer or X-API-Key credentials into an AuthContext.
type Authenticator interface {
	Authenticate(ctx context.Context, rawAPIKey string) (identity.AuthContext, error)
}

// WithAuthContext attaches an authenticated principal context to ctx.
func WithAuthContext(ctx context.Context, auth identity.AuthContext) context.Context {
	return context.WithValue(ctx, authContextKey{}, auth)
}

// AuthFromContext extracts the authenticated principal context from ctx.
func AuthFromContext(ctx context.Context) (identity.AuthContext, bool) {
	auth, ok := ctx.Value(authContextKey{}).(identity.AuthContext)
	return auth, ok
}

// AuthMiddleware enforces Zero-Trust API key authentication on /api/v1/* endpoints.
func AuthMiddleware(auth Authenticator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		if auth == nil {
			writeUnauthorized(w)
			return
		}
		rawKey := extractAPIKey(r)
		if rawKey == "" {
			writeUnauthorized(w)
			return
		}
		caller, err := auth.Authenticate(r.Context(), rawKey)
		if err != nil {
			writeUnauthorized(w)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		next.ServeHTTP(w, r.WithContext(WithAuthContext(r.Context(), caller)))
	})
}

func extractAPIKey(r *http.Request) string {
	if authz := strings.TrimSpace(r.Header.Get("Authorization")); authz != "" {
		if len(authz) > 7 && strings.EqualFold(authz[:7], "Bearer ") {
			return strings.TrimSpace(authz[7:])
		}
		return ""
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"title":  "Unauthorized",
		"status": http.StatusUnauthorized,
		"detail": "missing or invalid credentials",
	})
}
