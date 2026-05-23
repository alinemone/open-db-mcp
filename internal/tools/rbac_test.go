package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/open-db-mcp/open-db-mcp/internal/adapters"
	"github.com/open-db-mcp/open-db-mcp/internal/auth"
)

// fakeAdapter is a no-op adapter just to satisfy adapters.SourceRef.
type fakeAdapter struct{}

func (fakeAdapter) Kind() adapters.Kind                                   { return adapters.KindPostgres }
func (fakeAdapter) EnvPrefix() string                                     { return "PG_" }
func (fakeAdapter) Discover(map[string]string) ([]adapters.Source, error) { return nil, nil }
func (fakeAdapter) Connect(context.Context, adapters.Source) (adapters.Conn, error) {
	return nil, nil
}
func (fakeAdapter) CloseAll() error { return nil }

func srcRef(writable bool) adapters.SourceRef {
	cfg := map[string]string{}
	if writable {
		cfg["write"] = "true"
	}
	return adapters.SourceRef{
		Adapter: fakeAdapter{},
		Source: adapters.Source{
			Name: "TEST",
			Kind: adapters.KindPostgres,
			Cfg:  cfg,
		},
	}
}

func ctxWithRole(role auth.RoleLevel) context.Context {
	p := auth.NewPrincipal("tester", role, "tok")
	return auth.WithPrincipal(context.Background(), p)
}

func TestRequireWrite_ReaderForbidden(t *testing.T) {
	err := requireWrite(ctxWithRole(auth.RoleReader), srcRef(true))
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("expected forbidden error, got %v", err)
	}
}

func TestRequireWrite_WriterOnWritableSource(t *testing.T) {
	err := requireWrite(ctxWithRole(auth.RoleWriter), srcRef(true))
	if err != nil {
		t.Fatalf("writer on writable source should pass, got %v", err)
	}
}

func TestRequireWrite_WriterOnLockedSource(t *testing.T) {
	err := requireWrite(ctxWithRole(auth.RoleWriter), srcRef(false))
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("expected read-only error, got %v", err)
	}
}

func TestRequireWrite_AdminOnLockedSource(t *testing.T) {
	// admin must NOT bypass the per-source kill-switch.
	err := requireWrite(ctxWithRole(auth.RoleAdmin), srcRef(false))
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("admin must still be blocked by source.write flag, got %v", err)
	}
}

func TestRequireWrite_AdminOnWritableSource(t *testing.T) {
	err := requireWrite(ctxWithRole(auth.RoleAdmin), srcRef(true))
	if err != nil {
		t.Fatalf("admin on writable source should pass, got %v", err)
	}
}
