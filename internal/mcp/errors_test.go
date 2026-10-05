package mcp

import (
	"errors"
	"strings"
	"testing"
)

// A DB-side SQL error (surfaced by the postgres adapter behind the "sql error"
// prefix) must reach the client verbatim, not collapse to "internal error".
func TestClientErrMsgSurfacesSQLError(t *testing.T) {
	err := errors.New("sql error [57014]: query canceled — likely statement timeout; narrow the scan")
	got := clientErrMsg(err, "CORE", false)
	if got != err.Error() {
		t.Fatalf("sql error should pass through verbatim; got %q", got)
	}
}

// A connection failure is reported as an actionable hint, but the raw driver
// message (with internal IPs) stays hidden unless verbose is set.
func TestClientErrMsgConnectionHint(t *testing.T) {
	err := errors.New("dial tcp 10.42.200.181:5432: connect: connection refused")
	got := clientErrMsg(err, "CORE", false)
	if !strings.HasPrefix(got, "connection error on source CORE: connection refused") {
		t.Fatalf("expected connection hint; got %q", got)
	}
	if strings.Contains(got, "10.42.200.181") {
		t.Fatalf("hint leaked the internal IP: %q", got)
	}
	if got := clientErrMsg(err, "CORE", true); got != err.Error() {
		t.Fatalf("verbose should expose; got %q", got)
	}
}

// Anything unclassified still collapses to the generic message.
func TestClientErrMsgHidesInternal(t *testing.T) {
	err := errors.New("pgx: something unexpected at 10.42.200.181")
	got := clientErrMsg(err, "CORE", false)
	if !strings.HasPrefix(got, "internal error") || strings.Contains(got, "10.42") {
		t.Fatalf("unclassified error should be hidden; got %q", got)
	}
}
