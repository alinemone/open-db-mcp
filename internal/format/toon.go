// Package format provides compact, LLM-friendly representations of query results.
package format

import (
	"fmt"
	"sort"
	"strings"
)

// ToTOON renders rows as Tabular Object Object Notation, a CSV-like layout
// designed to use fewer tokens than JSON when fed to LLMs.
//
// Output:
//
//	Tickets[2]{id,subject}:
//	1,first
//	2,second
//
// Commas and newlines inside cells are sanitized (comma → semicolon, newline → space).
func ToTOON(name string, rows []map[string]any) string {
	if len(rows) == 0 {
		return fmt.Sprintf("%s[0]{}:", name)
	}
	headers := headerUnion(rows)

	var b strings.Builder
	b.Grow(64 + 16*len(rows)*len(headers))
	fmt.Fprintf(&b, "%s[%d]{%s}:\n", name, len(rows), strings.Join(headers, ","))

	for i, row := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		for j, h := range headers {
			if j > 0 {
				b.WriteByte(',')
			}
			b.WriteString(sanitize(row[h]))
		}
	}
	return b.String()
}

// ToTOONColumns renders a column-oriented result (Cols + Rows) without an
// intermediate []map allocation. Useful for raw SQL results.
func ToTOONColumns(name string, cols []string, rows [][]any) string {
	if len(rows) == 0 {
		return fmt.Sprintf("%s[0]{%s}:", name, strings.Join(cols, ","))
	}
	var b strings.Builder
	b.Grow(64 + 16*len(rows)*len(cols))
	fmt.Fprintf(&b, "%s[%d]{%s}:\n", name, len(rows), strings.Join(cols, ","))
	for i, row := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		for j, v := range row {
			if j > 0 {
				b.WriteByte(',')
			}
			b.WriteString(sanitize(v))
		}
	}
	return b.String()
}

func headerUnion(rows []map[string]any) []string {
	seen := map[string]struct{}{}
	for k := range rows[0] {
		seen[k] = struct{}{}
	}
	for _, r := range rows[1:] {
		for k := range r {
			seen[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	// Preserve the first row's key order at the front, append the rest after.
	first := make([]string, 0, len(rows[0]))
	for k := range rows[0] {
		first = append(first, k)
	}
	sort.Strings(first)
	extra := out[:0]
	mark := map[string]struct{}{}
	for _, k := range first {
		mark[k] = struct{}{}
	}
	for _, k := range out {
		if _, ok := mark[k]; !ok {
			extra = append(extra, k)
		}
	}
	return append(first, extra...)
}

func sanitize(v any) string {
	if v == nil {
		return ""
	}
	s := fmt.Sprintf("%v", v)
	if strings.ContainsAny(s, ",\n") {
		s = strings.ReplaceAll(s, ",", ";")
		s = strings.ReplaceAll(s, "\n", " ")
	}
	return s
}
