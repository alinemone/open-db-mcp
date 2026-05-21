package adapters

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	allowedFirstWord = map[string]struct{}{
		"SELECT": {}, "WITH": {}, "EXPLAIN": {}, "DESCRIBE": {}, "DESC": {}, "SHOW": {},
	}
	destructive = regexp.MustCompile(`(?i)\b(DROP|TRUNCATE|ALTER|INSERT|UPDATE|DELETE|CREATE|GRANT|REVOKE|MERGE|CALL|EXEC|EXECUTE)\b`)
)

// AssertReadOnly returns an error if the SQL string looks like it would mutate
// data. Adapter authors should call this from ExecuteQuery before sending the
// query downstream.
func AssertReadOnly(sql string) error {
	q := strings.TrimSpace(sql)
	if q == "" {
		return fmt.Errorf("empty query")
	}
	// Strip leading SQL comments.
	for strings.HasPrefix(q, "--") || strings.HasPrefix(q, "/*") {
		if strings.HasPrefix(q, "--") {
			if i := strings.IndexByte(q, '\n'); i >= 0 {
				q = strings.TrimSpace(q[i+1:])
				continue
			}
			return fmt.Errorf("query is only a comment")
		}
		end := strings.Index(q, "*/")
		if end < 0 {
			return fmt.Errorf("unterminated comment")
		}
		q = strings.TrimSpace(q[end+2:])
	}
	first := strings.ToUpper(strings.Fields(q)[0])
	if _, ok := allowedFirstWord[first]; !ok {
		return fmt.Errorf("query must start with one of SELECT/WITH/EXPLAIN/DESCRIBE/SHOW (got %s)", first)
	}
	if destructive.MatchString(q) {
		return fmt.Errorf("destructive keyword detected in query")
	}
	return nil
}
