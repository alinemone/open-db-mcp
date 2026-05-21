package auth

import "context"

func withRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, userKey, role)
}

// Role returns the role string associated with the request, or "" if missing.
func Role(ctx context.Context) string {
	v, _ := ctx.Value(userKey).(string)
	return v
}
