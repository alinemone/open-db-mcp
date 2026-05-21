package config

import (
	"strconv"
	"strings"
)

// ServerConfig is the top-level runtime configuration parsed from env.
type ServerConfig struct {
	Port     int
	APIKeys  map[string]string // token -> role
	LogLevel string
	TZ       string

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
		Port:     atoi(env["PORT"], 3000),
		APIKeys:  parseAPIKeys(env),
		LogLevel: defaultStr(env["LOG_LEVEL"], "info"),
		TZ:       defaultStr(env["TZ"], "UTC"),
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

// parseAPIKeys collects API tokens from two styles of env vars:
//
//  1. Per-user (preferred, readable):
//     MCP_USER_ALI=ali2412jsfdjag
//     MCP_USER_AMIR=amir122rfds
//     The role becomes the lowercased name after the prefix.
//
//  2. Legacy comma-list (kept for backward compat with db-mcp):
//     MCP_API_KEYS=token1:role1,token2:role2
//
// Tokens from both sources are merged; on conflict, the per-user form wins.
func parseAPIKeys(env map[string]string) map[string]string {
	out := map[string]string{}

	// Legacy form first so per-user can override.
	if raw := env["MCP_API_KEYS"]; raw != "" {
		for _, pair := range strings.Split(raw, ",") {
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
			if role == "" {
				role = "user"
			}
			out[token] = role
		}
	}

	// Per-user form: any env key starting with MCP_USER_.
	for k, v := range env {
		if !strings.HasPrefix(k, "MCP_USER_") {
			continue
		}
		role := strings.ToLower(strings.TrimPrefix(k, "MCP_USER_"))
		token := strings.TrimSpace(v)
		if role == "" || token == "" {
			continue
		}
		out[token] = role
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
