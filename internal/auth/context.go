package auth

import "context"

// WithPrincipal attaches a Principal to ctx. The middleware uses this on the
// hot path; tests use it to simulate an authenticated request.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// PrincipalOf returns the authenticated principal attached to ctx, or a
// zero-value Principal (role=reader, name="") for unauthenticated contexts.
func PrincipalOf(ctx context.Context) Principal {
	p, _ := ctx.Value(principalKey).(Principal)
	return p
}

// Role returns the role string for the request context. Kept for legacy logging
// call sites that just want the label.
func Role(ctx context.Context) string {
	return PrincipalOf(ctx).Role.String()
}

// UserName returns the authenticated user name, or "" if missing.
func UserName(ctx context.Context) string {
	return PrincipalOf(ctx).Name
}
