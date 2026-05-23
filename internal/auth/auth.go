// Package auth implements token-based middleware for the MCP HTTP transport.
package auth

import (
	"net/http"
	"strings"
)

type ctxKey int

const principalKey ctxKey = 0

// Options tweaks middleware behavior without changing the call signature.
type Options struct {
	// AllowQueryKey enables ?api_key= as a token source. Off by default — query
	// strings leak into reverse-proxy logs, browser history, and Referer
	// headers.
	AllowQueryKey bool
}

// Middleware returns an http.Handler that rejects requests without a valid
// token. Token sources (in order):
//
//   - Authorization: Bearer <token>
//   - X-Api-Key: <token>
//   - ?api_key=<token>     (only if Options.AllowQueryKey == true)
//
// Paths in `open` bypass the check entirely (e.g. "/health"). The matched
// Principal is stored on the request context.
func Middleware(principals []Principal, opts Options, open ...string) func(http.Handler) http.Handler {
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
			token := extract(r, opts.AllowQueryKey)
			if token == "" {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, `{"error":"unauthorized","message":"valid token required"}`, http.StatusUnauthorized)
				return
			}
			p, ok := lookup(principals, token)
			if !ok {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, `{"error":"unauthorized","message":"valid token required"}`, http.StatusUnauthorized)
				return
			}
			ctx := WithPrincipal(r.Context(), p)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extract(r *http.Request, allowQuery bool) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[len("Bearer "):])
	}
	if h := r.Header.Get("X-Api-Key"); h != "" {
		return strings.TrimSpace(h)
	}
	if allowQuery {
		return r.URL.Query().Get("api_key")
	}
	return ""
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
