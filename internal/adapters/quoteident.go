package adapters

import (
	"fmt"
	"strings"
	"unicode"
)

// QuoteIdentPG returns a safely-quoted PostgreSQL identifier ("foo", embedded
// double-quotes doubled). It rejects identifiers containing control characters
// or NUL bytes — those would let a caller break out of any context.
//
// Use this everywhere a schema/table/column name is interpolated into SQL
// instead of being sent as a parameter (e.g. SELECT * FROM <ident>, which
// cannot be parameterized).
func QuoteIdentPG(s string) (string, error) {
	if err := validateIdent(s); err != nil {
		return "", err
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`, nil
}

// QuoteIdentMySQL returns a safely-quoted MySQL identifier (`foo`, embedded
// backticks doubled). Same rejection rules as QuoteIdentPG.
func QuoteIdentMySQL(s string) (string, error) {
	if err := validateIdent(s); err != nil {
		return "", err
	}
	return "`" + strings.ReplaceAll(s, "`", "``") + "`", nil
}

// validateIdent rejects identifiers that are empty, oversized, or contain
// control characters / NUL. We allow Unicode letters and digits since both
// Postgres and MySQL accept them when quoted.
func validateIdent(s string) error {
	if s == "" {
		return fmt.Errorf("empty identifier")
	}
	if len(s) > 128 {
		return fmt.Errorf("identifier too long")
	}
	for _, r := range s {
		if r == 0 || unicode.IsControl(r) {
			return fmt.Errorf("invalid character in identifier")
		}
	}
	return nil
}
