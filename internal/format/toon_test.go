package format

import (
	"strings"
	"testing"
)

func TestToTOON_Empty(t *testing.T) {
	got := ToTOON("Users", nil)
	if got != "Users[0]{}:" {
		t.Fatalf("empty case = %q", got)
	}
}

func TestToTOON_Single(t *testing.T) {
	rows := []map[string]any{{"id": 1, "name": "ali"}}
	got := ToTOON("Users", rows)
	if !strings.HasPrefix(got, "Users[1]{") {
		t.Fatalf("missing header: %q", got)
	}
	if !strings.Contains(got, "1") || !strings.Contains(got, "ali") {
		t.Fatalf("missing values: %q", got)
	}
}

func TestToTOON_HeaderUnion(t *testing.T) {
	// Second row has an extra column the first row lacks. Output should
	// include both headers and leave the missing cell empty.
	rows := []map[string]any{
		{"id": 1, "name": "ali"},
		{"id": 2, "name": "sara", "age": 30},
	}
	got := ToTOON("Users", rows)
	if !strings.Contains(got, "age") {
		t.Fatalf("missing union column: %q", got)
	}
	if !strings.Contains(got, "30") {
		t.Fatalf("missing union value: %q", got)
	}
}

func TestToTOON_Sanitizes(t *testing.T) {
	rows := []map[string]any{{"v": "a,b\nc"}}
	got := ToTOON("X", rows)
	if strings.Contains(got, ",b") || strings.Contains(got, "\nc") {
		t.Fatalf("sanitizer left raw comma or newline in cell: %q", got)
	}
	if !strings.Contains(got, "a;b c") {
		t.Fatalf("expected sanitized 'a;b c' in %q", got)
	}
}

func TestToTOON_NilCell(t *testing.T) {
	rows := []map[string]any{{"v": nil}}
	got := ToTOON("X", rows)
	// Final cell should be empty (just header + newline + empty row).
	lines := strings.Split(got, "\n")
	if len(lines) != 2 || lines[1] != "" {
		t.Fatalf("nil cell rendering wrong: %q", got)
	}
}

func TestToTOONColumns_Empty(t *testing.T) {
	got := ToTOONColumns("R", []string{"a", "b"}, nil)
	if got != "R[0]{a,b}:" {
		t.Fatalf("empty cols = %q", got)
	}
}

func TestToTOONColumns_Rows(t *testing.T) {
	got := ToTOONColumns("R", []string{"a", "b"}, [][]any{{1, "x"}, {2, nil}})
	want := "R[2]{a,b}:\n1,x\n2,"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
