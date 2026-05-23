package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newPrincipals(t *testing.T) []Principal {
	t.Helper()
	return []Principal{
		NewPrincipal("ali", RoleReader, "tok-ali"),
		NewPrincipal("dev", RoleWriter, "tok-dev"),
		NewPrincipal("root", RoleAdmin, "tok-root"),
	}
}

func TestParseRole(t *testing.T) {
	cases := map[string]RoleLevel{
		"admin":     RoleAdmin,
		"ADMIN":     RoleAdmin,
		"writer":    RoleWriter,
		"write":     RoleWriter,
		"reader":    RoleReader,
		"":          RoleReader,
		"bogus":     RoleReader,
		"  admin  ": RoleAdmin,
	}
	for in, want := range cases {
		if got := ParseRole(in); got != want {
			t.Errorf("ParseRole(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestCanWrite(t *testing.T) {
	cases := map[RoleLevel]bool{
		RoleReader: false,
		RoleWriter: true,
		RoleAdmin:  true,
	}
	for r, want := range cases {
		p := Principal{Role: r}
		if got := p.CanWrite(); got != want {
			t.Errorf("CanWrite() for role=%v = %v, want %v", r, got, want)
		}
	}
}

func TestMiddleware_MissingToken(t *testing.T) {
	mw := Middleware(newPrincipals(t), Options{})
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest("POST", "/mcp", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if called {
		t.Fatal("handler was called without a token")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMiddleware_InvalidToken(t *testing.T) {
	mw := Middleware(newPrincipals(t), Options{})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMiddleware_ValidBearer(t *testing.T) {
	mw := Middleware(newPrincipals(t), Options{})
	var seen Principal
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = PrincipalOf(r.Context())
	}))

	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer tok-dev")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if seen.Name != "dev" || seen.Role != RoleWriter {
		t.Fatalf("principal = %+v, want name=dev role=writer", seen)
	}
}

func TestMiddleware_ValidXApiKey(t *testing.T) {
	mw := Middleware(newPrincipals(t), Options{})
	var seen Principal
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = PrincipalOf(r.Context())
	}))

	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("X-Api-Key", "tok-ali")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if seen.Name != "ali" || seen.Role != RoleReader {
		t.Fatalf("principal = %+v, want name=ali role=reader", seen)
	}
}

func TestMiddleware_QueryKey_Disabled(t *testing.T) {
	mw := Middleware(newPrincipals(t), Options{AllowQueryKey: false})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest("POST", "/mcp?api_key=tok-root", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("query-key path should be denied when disabled, got %d", rec.Code)
	}
}

func TestMiddleware_QueryKey_Enabled(t *testing.T) {
	mw := Middleware(newPrincipals(t), Options{AllowQueryKey: true})
	var seen Principal
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = PrincipalOf(r.Context())
	}))

	req := httptest.NewRequest("POST", "/mcp?api_key=tok-root", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if seen.Name != "root" || seen.Role != RoleAdmin {
		t.Fatalf("principal = %+v, want admin/root", seen)
	}
}

func TestMiddleware_OpenPath(t *testing.T) {
	mw := Middleware(newPrincipals(t), Options{}, "/health")
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("open path should bypass auth")
	}
}

func TestMiddleware_Options(t *testing.T) {
	mw := Middleware(newPrincipals(t), Options{})
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest("OPTIONS", "/mcp", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("preflight OPTIONS should pass through")
	}
}

func TestRedactURL(t *testing.T) {
	cases := map[string]string{
		"/mcp?api_key=secret":         "/mcp?api_key=REDACTED",
		"/mcp?api_key=secret&other=1": "/mcp?api_key=REDACTED&other=1",
		"/mcp?other=1":                "/mcp?other=1",
		"":                            "",
	}
	for in, want := range cases {
		if got := RedactURL(in); got != want {
			t.Errorf("RedactURL(%q) = %q, want %q", in, got, want)
		}
	}
}
