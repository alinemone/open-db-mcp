package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSqlError(t *testing.T) {
	tests := []struct {
		name       string
		in         error
		wantPrefix string // "" means: must be returned unchanged
		wantSubstr string
	}{
		{
			name:       "statement timeout 57014",
			in:         &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"},
			wantPrefix: "sql error [57014]",
			wantSubstr: "timeout",
		},
		{
			name:       "undefined column 42703 surfaces message",
			in:         &pgconn.PgError{Code: "42703", Message: `column "x" does not exist`},
			wantPrefix: "sql error [42703]",
			wantSubstr: `column "x" does not exist`,
		},
		{
			name:       "context deadline -> explicit timeout",
			in:         context.DeadlineExceeded,
			wantPrefix: "sql error",
			wantSubstr: "5m",
		},
		{
			name: "non-pg error passes through unchanged",
			in:   errors.New("dial tcp 10.0.0.1:5432: connect: connection refused"),
		},
		{
			name: "nil stays nil",
			in:   nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sqlError(tc.in)
			if tc.in == nil {
				if got != nil {
					t.Fatalf("expected nil, got %v", got)
				}
				return
			}
			if tc.wantPrefix == "" {
				// must be returned unchanged (collapses to generic upstream)
				if got.Error() != tc.in.Error() {
					t.Fatalf("expected unchanged %q, got %q", tc.in.Error(), got.Error())
				}
				return
			}
			if !strings.HasPrefix(got.Error(), tc.wantPrefix) {
				t.Fatalf("expected prefix %q, got %q", tc.wantPrefix, got.Error())
			}
			if tc.wantSubstr != "" && !strings.Contains(got.Error(), tc.wantSubstr) {
				t.Fatalf("expected substring %q in %q", tc.wantSubstr, got.Error())
			}
		})
	}
}

// errors.As must still find the wrapped PgError so callers/audit can inspect it
// is not required, but ensure wrapped PgError messages don't leak host/infra.
func TestSqlErrorNoInfraLeak(t *testing.T) {
	pg := &pgconn.PgError{Code: "42P01", Message: `relation "foo" does not exist`}
	msg := sqlError(pg).Error()
	for _, bad := range []string{"host=", "password", "10.42.", "user="} {
		if strings.Contains(msg, bad) {
			t.Fatalf("infra detail %q leaked in %q", bad, msg)
		}
	}
}
