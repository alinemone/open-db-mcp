package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"strings"
)

// RoleLevel orders the three roles so handlers can compare them numerically.
type RoleLevel int

const (
	RoleReader RoleLevel = iota
	RoleWriter
	RoleAdmin
)

// String returns the lowercase, canonical role name.
func (r RoleLevel) String() string {
	switch r {
	case RoleAdmin:
		return "admin"
	case RoleWriter:
		return "writer"
	default:
		return "reader"
	}
}

// ParseRole converts a role string (case-insensitive) to RoleLevel, defaulting
// to RoleReader for unknown values.
func ParseRole(s string) RoleLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "admin":
		return RoleAdmin
	case "writer", "write":
		return RoleWriter
	default:
		return RoleReader
	}
}

// Principal is the resolved identity behind an authenticated request.
type Principal struct {
	Name  string    // lowercased, e.g. "ali"
	Role  RoleLevel // capability level
	token [32]byte  // sha256 of the raw bearer token; kept private
}

// NewPrincipal hashes the supplied raw token and returns a Principal. The raw
// token is not stored.
func NewPrincipal(name string, role RoleLevel, rawToken string) Principal {
	return Principal{
		Name:  name,
		Role:  role,
		token: sha256.Sum256([]byte(rawToken)),
	}
}

// CanWrite reports whether this principal's role permits write operations.
// The per-source write flag is still required on top of this — it's a separate
// gate enforced at the adapter layer.
func (p Principal) CanWrite() bool { return p.Role >= RoleWriter }

// matchToken returns true when the supplied raw token hashes to this
// principal's stored hash. Comparison is constant-time.
func (p Principal) matchToken(raw string) bool {
	h := sha256.Sum256([]byte(raw))
	return subtle.ConstantTimeCompare(h[:], p.token[:]) == 1
}

// lookup walks the principal slice with constant-time compare and returns the
// first match. The whole slice is scanned even on a hit, so request time does
// not leak which token matched.
func lookup(principals []Principal, raw string) (Principal, bool) {
	var match Principal
	found := 0
	for _, p := range principals {
		if p.matchToken(raw) {
			match = p
			found = 1
		}
	}
	return match, found == 1
}
