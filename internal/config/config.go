package config

import (
	"strconv"
	"strings"

	"github.com/open-db-mcp/open-db-mcp/internal/auth"
)

// ServerConfig is the top-level runtime configuration parsed from env.
type ServerConfig struct {
	Port       int
	Principals []auth.Principal
	LogLevel   string
	TZ         string

	// CORSOrigins is the explicit allow-list emitted in
	// Access-Control-Allow-Origin. Empty slice → no CORS header at all.
	// A single "*" element is allowed for fully-open deployments.
	CORSOrigins []string

	CLOG CLOGConfig
}

// CLOGConfig enables the clog_* tools when CLOG_ES_SOURCE is set.
type CLOGConfig struct {
	ESSource      string
	IngressIndex  string
	LogsPrefix    string
	AllLogsIndex  string
	NamespaceFlds []string
	ContainerFlds []string
	ServiceFlds   []string
	StatusFlds    []string
	LatencyFlds   []string
	HostFlds      []string
	PathFlds      []string
}

// Load reads ServerConfig from the supplied env map.
func Load(env map[string]string) ServerConfig {
	c := ServerConfig{
		Port:        atoi(env["PORT"], 3000),
		Principals:  parsePrincipals(env),
		LogLevel:    defaultStr(env["LOG_LEVEL"], "info"),
		TZ:          defaultStr(env["TZ"], "UTC"),
		CORSOrigins: splitCSV(env["MCP_CORS_ORIGINS"]),
		CLOG: CLOGConfig{
			ESSource:      env["CLOG_ES_SOURCE"],
			IngressIndex:  defaultStr(env["CLOG_INGRESS_INDEX"], "logs-ingress-*"),
			LogsPrefix:    defaultStr(env["CLOG_LOGS_PREFIX"], "logs-"),
			AllLogsIndex:  defaultStr(env["CLOG_ALL_LOGS_INDEX"], "logs-*"),
			NamespaceFlds: splitCSV(env["CLOG_NAMESPACE_FIELDS"]),
			ContainerFlds: splitCSV(env["CLOG_CONTAINER_FIELDS"]),
			ServiceFlds:   splitCSV(env["CLOG_SERVICE_FIELDS"]),
			StatusFlds:    splitCSV(env["CLOG_INGRESS_STATUS_FIELDS"]),
			LatencyFlds:   splitCSV(env["CLOG_INGRESS_LATENCY_FIELDS"]),
			HostFlds:      splitCSV(env["CLOG_INGRESS_HOST_FIELDS"]),
			PathFlds:      splitCSV(env["CLOG_INGRESS_PATH_FIELDS"]),
		},
	}
	return c
}

// parsePrincipals collects API tokens from two styles of env vars:
//
//  1. Per-user (preferred):
//     MCP_USER_ALI=token-for-ali
//     MCP_USER_ALI_ROLE=writer        (default: reader)
//
//  2. Legacy comma-list (kept for backward compat with db-mcp):
//     MCP_API_KEYS=token1:role1,token2:role2
//
// Tokens from both sources are merged into a slice keyed by the sha256 hash of
// the raw token (see auth.Principal). On token collision, the per-user form
// wins.
func parsePrincipals(env map[string]string) []auth.Principal {
	// rawByName collects token + role for each named user before hashing, so we
	// can override legacy entries with the per-user form deterministically.
	type raw struct {
		token string
		role  auth.RoleLevel
	}
	byName := map[string]raw{}

	// Legacy form first so per-user can override.
	if rawList := env["MCP_API_KEYS"]; rawList != "" {
		for i, pair := range strings.Split(rawList, ",") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			token, role, _ := strings.Cut(pair, ":")
			token = strings.TrimSpace(token)
			role = strings.TrimSpace(role)
			if token == "" {
				continue
			}
			name := role
			if name == "" {
				name = "user"
			}
			// Disambiguate duplicate legacy role labels.
			key := name + "#" + strconv.Itoa(i)
			byName[key] = raw{token: token, role: auth.ParseRole(role)}
		}
	}

	// Per-user form: any env key starting with MCP_USER_ and *not* ending in
	// _ROLE. The role suffix is read separately so users can supply it
	// alongside the token.
	for k, v := range env {
		if !strings.HasPrefix(k, "MCP_USER_") {
			continue
		}
		if strings.HasSuffix(k, "_ROLE") {
			continue
		}
		name := strings.ToLower(strings.TrimPrefix(k, "MCP_USER_"))
		token := strings.TrimSpace(v)
		if name == "" || token == "" {
			continue
		}
		role := auth.ParseRole(env["MCP_USER_"+strings.ToUpper(name)+"_ROLE"])
		byName[name] = raw{token: token, role: role}
	}

	out := make([]auth.Principal, 0, len(byName))
	for name, r := range byName {
		// Legacy key collisions use the disambiguator suffix; drop it for the
		// stored Name so logs read cleanly.
		displayName := name
		if i := strings.IndexByte(name, '#'); i >= 0 {
			displayName = name[:i]
		}
		out = append(out, auth.NewPrincipal(displayName, r.role, r.token))
	}
	return out
}

func atoi(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
