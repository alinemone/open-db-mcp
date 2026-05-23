package config

import (
	"testing"

	"github.com/open-db-mcp/open-db-mcp/internal/auth"
)

func findByName(t *testing.T, ps []auth.Principal, name string) auth.Principal {
	t.Helper()
	for _, p := range ps {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("principal %q not found among %d", name, len(ps))
	return auth.Principal{}
}

func TestParsePrincipals_PerUser_DefaultRole(t *testing.T) {
	env := map[string]string{
		"MCP_USER_ALI": "tok-ali",
	}
	ps := parsePrincipals(env)
	if len(ps) != 1 {
		t.Fatalf("got %d principals, want 1", len(ps))
	}
	p := findByName(t, ps, "ali")
	if p.Role != auth.RoleReader {
		t.Fatalf("default role = %v, want reader", p.Role)
	}
}

func TestParsePrincipals_PerUser_WithRole(t *testing.T) {
	env := map[string]string{
		"MCP_USER_ALI":       "tok-ali",
		"MCP_USER_ALI_ROLE":  "writer",
		"MCP_USER_ROOT":      "tok-root",
		"MCP_USER_ROOT_ROLE": "admin",
	}
	ps := parsePrincipals(env)
	if len(ps) != 2 {
		t.Fatalf("got %d principals, want 2", len(ps))
	}
	if r := findByName(t, ps, "ali").Role; r != auth.RoleWriter {
		t.Errorf("ali role = %v, want writer", r)
	}
	if r := findByName(t, ps, "root").Role; r != auth.RoleAdmin {
		t.Errorf("root role = %v, want admin", r)
	}
}

func TestParsePrincipals_Legacy(t *testing.T) {
	env := map[string]string{
		"MCP_API_KEYS": "tok1:admin,tok2:writer,tok3:reader,tok4:",
	}
	ps := parsePrincipals(env)
	if len(ps) != 4 {
		t.Fatalf("got %d principals, want 4", len(ps))
	}
	roles := map[auth.RoleLevel]int{}
	for _, p := range ps {
		roles[p.Role]++
	}
	if roles[auth.RoleAdmin] != 1 || roles[auth.RoleWriter] != 1 || roles[auth.RoleReader] != 2 {
		t.Fatalf("role distribution = %+v, want admin=1 writer=1 reader=2", roles)
	}
}

func TestParsePrincipals_IgnoresRoleEnvOnly(t *testing.T) {
	// A bare MCP_USER_X_ROLE without the matching token should not produce a
	// principal.
	env := map[string]string{
		"MCP_USER_ORPHAN_ROLE": "admin",
	}
	if got := parsePrincipals(env); len(got) != 0 {
		t.Fatalf("orphan role created %d principals, want 0", len(got))
	}
}

func TestParseBool(t *testing.T) {
	truthy := []string{"1", "true", "TRUE", "yes", "on", " true "}
	falsy := []string{"", "0", "false", "no", "off", "anything"}
	for _, s := range truthy {
		if !parseBool(s) {
			t.Errorf("parseBool(%q) = false, want true", s)
		}
	}
	for _, s := range falsy {
		if parseBool(s) {
			t.Errorf("parseBool(%q) = true, want false", s)
		}
	}
}

func TestParsePrefixed_HandlesNestedKeys(t *testing.T) {
	env := map[string]string{
		"PG_MAIN_HOST":         "h",
		"PG_MAIN_PORT":         "5432",
		"PG_PROD_HOST":         "h2",
		"PG_NOPE":              "skipme", // single segment — must be skipped
		"MONGO_LOGS_AUTH_DB":   "admin",  // key with embedded underscore
		"ES_PROD_INSECURE_TLS": "true",
		"OTHER_VAR":            "ignored",
	}
	out := ParsePrefixed(env, "PG_")
	if _, ok := out["MAIN"]; !ok {
		t.Fatal("missing MAIN group")
	}
	if out["MAIN"]["HOST"] != "h" || out["MAIN"]["PORT"] != "5432" {
		t.Errorf("MAIN group bad: %+v", out["MAIN"])
	}
	if _, ok := out["NOPE"]; ok {
		t.Error("single-segment key should not become a source")
	}

	out2 := ParsePrefixed(env, "MONGO_")
	if out2["LOGS"]["AUTH_DB"] != "admin" {
		t.Errorf("MONGO_LOGS_AUTH_DB not parsed: %+v", out2)
	}

	out3 := ParsePrefixed(env, "ES_")
	if out3["PROD"]["INSECURE_TLS"] != "true" {
		t.Errorf("ES_PROD_INSECURE_TLS not parsed: %+v", out3)
	}
}
