// Package auth implements token-based middleware for the MCP HTTP transport.
package auth

import (
	"net/http"
	"strings"
)

type ctxKey int

const userKey ctxKey = 0

// Middleware returns an http.Handler that rejects requests without a valid
// token. The token may arrive as:
//
//   - Authorization: Bearer <token>
//   - X-Api-Key: <token>
//   - ?api_key=<token>
//
// Paths in `open` bypass the check entirely (e.g. "/health"). The matched
// role is stored on the request context.
func Middleware(keys map[string]string, open ...string) func(http.Handler) http.Handler {
	openSet := map[string]struct{}{}
	for _, p := range open {
		openSet[p] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, skip := openSet[r.URL.Path]; skip || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			token := extract(r)
			role, ok := keys[token]
			if token == "" || !ok {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, `{"error":"unauthorized","message":"valid token required"}`, http.StatusUnauthorized)
				return
			}
			ctx := r.Context()
			ctx = withRole(ctx, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extract(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[len("Bearer "):])
	}
	if h := r.Header.Get("X-Api-Key"); h != "" {
		return strings.TrimSpace(h)
	}
	return r.URL.Query().Get("api_key")
}

// RedactURL replaces the api_key query parameter with REDACTED so log lines
// never carry credentials.
func RedactURL(u string) string {
	i := strings.Index(u, "api_key=")
	if i < 0 {
		return u
	}
	end := strings.IndexAny(u[i+len("api_key="):], "&#")
	if end < 0 {
		return u[:i] + "api_key=REDACTED"
	}
	return u[:i] + "api_key=REDACTED" + u[i+len("api_key=")+end:]
}
