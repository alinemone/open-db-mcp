package mcp

import (
	"errors"
	"testing"
)

// A DB-side SQL error (surfaced by the postgres adapter behind the "sql error"
// prefix) must reach the client verbatim, not collapse to "internal error".
func TestClientErrMsgSurfacesSQLError(t *testing.T) {
	err := errors.New("sql error [57014]: query canceled — likely statement timeout; narrow the scan")
	got := clientErrMsg(err, false)
	if got != err.Error() {
		t.Fatalf("sql error should pass through verbatim; got %q", got)
	}
}

// A genuine internal/infra error still collapses to the generic message when
// not in verbose mode.
func TestClientErrMsgHidesInternal(t *testing.T) {
	err := errors.New("dial tcp 10.42.200.181:5432: connect: connection refused")
	if got := clientErrMsg(err, false); got != "internal error" {
		t.Fatalf("infra error should be hidden; got %q", got)
	}
	if got := clientErrMsg(err, true); got != err.Error() {
		t.Fatalf("verbose should expose; got %q", got)
	}
}
