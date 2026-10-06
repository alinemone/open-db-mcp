package clog

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const redacted = "[REDACTED]"

// secretKeys are parameter / JSON key names whose values are masked.
const secretKeys = `api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|auth[_-]?token|token|` +
	`password|passwd|pass|pwd|secret|client[_-]?secret|authorization|x[_-]app[_-]auth|cookie|session[_-]?id`

var redactRules = []struct {
	re   *regexp.Regexp
	repl string
}{
	// Authorization: Bearer xxx / Basic xxx / ApiKey xxx (header or log text).
	{regexp.MustCompile(`(?i)\b(bearer|basic|apikey)\s+[A-Za-z0-9._~+/=-]{6,}`), "$1 " + redacted},
	// JSON "key": "value"
	{regexp.MustCompile(`(?i)("(?:` + secretKeys + `)"\s*:\s*")[^"]*(")`), "${1}" + redacted + "${2}"},
	// query string / form / log key=value
	{regexp.MustCompile(`(?i)(^|[?&;,\s"'])(` + secretKeys + `)=([^&;,\s"']+)`), "${1}${2}=" + redacted},
	// Bare JWTs anywhere.
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), redacted},
}

// Redact masks credentials (bearer/basic tokens, JWTs, secret-looking
// key=value pairs and JSON fields) in log text before it reaches the model.
func Redact(s string) string {
	for _, r := range redactRules {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}

// Truncate shortens s to at most max runes, marking the cut with "…".
func Truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}

// RouteKey normalizes an ingress path for grouping: query string and
// fragment dropped, secrets masked, length capped.
func RouteKey(path string) string {
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	return Truncate(Redact(path), 500)
}
