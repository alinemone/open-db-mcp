package config

import (
	"os"
	"strconv"
	"strings"
)

// LoadEnv copies os.Environ into a map for deterministic, testable parsing.
func LoadEnv() map[string]string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			continue
		}
		env[kv[:i]] = kv[i+1:]
	}
	return env
}

// ParsePrefixed groups env vars of the form `<PREFIX><NAME>_<KEY>=value` by NAME.
//
// Example: ParsePrefixed(env, "PG_") returns
//
//	{
//	  "ANALYTICS": {"HOST": "...", "PORT": "5432", ...},
//	  "CORE":      {"HOST": "...", ...},
//	}
//
// Single-segment names like "PG_HOST" are skipped — every source must have at
// least one underscore between its name and a key.
//
// Keys are canonicalized through keyAliases, so the self-explanatory spellings
// (PG_CORE_PASSWORD, PG_CORE_DATABASE, PG_CORE_ALLOW_WRITE) and the short ones
// (PG_CORE_PASS, PG_CORE_DB, PG_CORE_WRITE) are interchangeable. Values are
// trimmed, and WRITE is normalized to "true"/"false".
func ParsePrefixed(env map[string]string, prefix string) map[string]map[string]string {
	out := map[string]map[string]string{}
	for k, v := range env {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := k[len(prefix):]
		// rest must look like "<NAME>_<KEY>"; reject "NAME" alone.
		idx := strings.IndexByte(rest, '_')
		if idx <= 0 || idx == len(rest)-1 {
			continue
		}
		name, key := rest[:idx], rest[idx+1:]
		if canon, ok := keyAliases[key]; ok {
			key = canon
		}
		v = strings.TrimSpace(v)
		if key == "WRITE" {
			v = strconv.FormatBool(IsTruthy(v))
		}
		if _, ok := out[name]; !ok {
			out[name] = map[string]string{}
		}
		out[name][key] = v
	}
	return out
}

// keyAliases maps readable env-key spellings to the canonical short key the
// adapters read.
var keyAliases = map[string]string{
	"PASSWORD":    "PASS",
	"USERNAME":    "USER",
	"DATABASE":    "DB",
	"ALLOW_WRITE": "WRITE",
}

// IsTruthy reports whether an env flag value means "on".
func IsTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}
