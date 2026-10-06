package clog

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	cases := map[string]string{
		"Authorization: Bearer abcdef123456":    "Authorization: Bearer [REDACTED]",
		`{"password":"hunter2","user":"ali"}`:   `{"password":"[REDACTED]","user":"ali"}`,
		"GET /login?user=ali&token=s3cr3t&x=1":  "GET /login?user=ali&token=[REDACTED]&x=1",
		"api_key=AKIA123 done":                  "api_key=[REDACTED] done",
		"jwt " + jwt + " end":                   "jwt [REDACTED] end",
		"nothing secret here: status=200 ok":    "nothing secret here: status=200 ok",
		`"Authorization": "Basic dXNlcjpwYXNz"`: `"Authorization": "[REDACTED]"`,
	}
	for in, want := range cases {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestRouteKeyAndTruncate(t *testing.T) {
	if got := RouteKey("/api/v1/orders/42?token=abc#frag"); got != "/api/v1/orders/42" {
		t.Errorf("RouteKey = %q", got)
	}
	long := strings.Repeat("ق", 10)
	if got := Truncate(long, 4); got != "قققق…" {
		t.Errorf("Truncate must cut on runes: %q", got)
	}
}
