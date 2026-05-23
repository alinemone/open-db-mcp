package adapters

import "testing"

func TestAssertReadOnly(t *testing.T) {
	cases := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{"simple select", "SELECT 1", false},
		{"select lower", "select * from t", false},
		{"with cte", "WITH x AS (SELECT 1) SELECT * FROM x", false},
		{"explain", "EXPLAIN SELECT * FROM t", false},
		{"show", "SHOW TABLES", false},
		{"describe", "DESCRIBE t", false},
		{"desc short", "DESC t", false},
		{"leading line comment then select", "-- hi\nSELECT 1", false},
		{"leading block comment then select", "/* hi */ SELECT 1", false},

		{"empty", "", true},
		{"only whitespace", "   \t  ", true},
		{"only comment", "-- comment only", true},
		{"unterminated block comment", "/* no end", true},
		{"insert", "INSERT INTO t VALUES (1)", true},
		{"update", "UPDATE t SET x = 1", true},
		{"delete", "DELETE FROM t", true},
		{"drop", "DROP TABLE t", true},
		{"truncate", "TRUNCATE TABLE t", true},
		{"alter", "ALTER TABLE t ADD c INT", true},
		{"create", "CREATE TABLE t (id int)", true},
		{"grant", "GRANT SELECT ON t TO u", true},
		{"revoke", "REVOKE SELECT ON t FROM u", true},
		{"merge", "MERGE INTO t USING s ON ...", true},
		{"call", "CALL proc()", true},
		{"exec", "EXEC proc", true},
		{"select then drop", "SELECT 1; DROP TABLE t", true},
		// CTE that hides a destructive keyword in a string literal — the regex
		// catches it. False positives are acceptable for a read-only guard.
		{"destructive in literal", "SELECT 'DROP TABLE t' FROM t", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := AssertReadOnly(c.sql)
			if c.wantErr && err == nil {
				t.Fatalf("expected error for %q, got nil", c.sql)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", c.sql, err)
			}
		})
	}
}

func TestQuoteIdentPG(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"users", `"users"`, false},
		{"with space", `"with space"`, false},
		{`he"llo`, `"he""llo"`, false},
		{"unicode_حساب", `"unicode_حساب"`, false},
		{"", "", true},
		{"\x00null", "", true},
		{"line\nbreak", "", true},
	}
	for _, c := range cases {
		got, err := QuoteIdentPG(c.in)
		if c.wantErr {
			if err == nil {
				t.Fatalf("QuoteIdentPG(%q) expected error, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("QuoteIdentPG(%q) unexpected error: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("QuoteIdentPG(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestQuoteIdentMySQL(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"users", "`users`", false},
		{"with`tick", "`with``tick`", false},
		{"", "", true},
		{"\tcontrol", "", true},
	}
	for _, c := range cases {
		got, err := QuoteIdentMySQL(c.in)
		if c.wantErr {
			if err == nil {
				t.Fatalf("QuoteIdentMySQL(%q) expected error, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("QuoteIdentMySQL(%q) unexpected error: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("QuoteIdentMySQL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
