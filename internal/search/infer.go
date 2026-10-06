package search

import (
	"sort"
	"strings"
)

// Inferred is a relationship guessed from naming conventions, not from a
// declared foreign key. Always present it to the caller as a heuristic.
type Inferred struct {
	Direction  string // "inbound" (other table → this one) or "outbound"
	OwnColumn  string // column on the table being described
	Schema     string // the other side
	Table      string
	Column     string
	Confidence float64
	Reason     string
}

// InferRelationships guesses join paths for source.schema.table from the
// indexed column names of the same source:
//
//	inbound  other.<table>_id     → table.id   0.9 table_id_match
//	inbound  other.<singular>_id  → table.id   0.8 singular_id_match
//	inbound  other.<pk>           → table.<pk> 0.5 name_match (pk ≠ "id")
//	outbound table.<foo>_id → foo(s).id        0.7 column_suffix_id (same schema)
//	                                           0.5 (other schema)
//
// ownCols are the columns of the described table, pk its primary key. At
// most max results per direction are returned, highest confidence first.
func (i *Index) InferRelationships(source, schema, table string, ownCols, pk []string, max int) []Inferred {
	i.mu.RLock()
	entries := i.entries
	i.mu.RUnlock()

	tbl := strings.ToLower(table)
	single := singular(tbl)
	self := func(e Entry) bool { return e.Schema == schema && e.Table == table }

	seen := map[string]bool{}
	var out []Inferred
	add := func(r Inferred) {
		key := r.Direction + "|" + r.OwnColumn + "|" + r.Schema + "." + r.Table + "." + r.Column
		if !seen[key] {
			seen[key] = true
			out = append(out, r)
		}
	}

	// Tables of this source that have an "id" column, for outbound targets.
	hasID := map[string]bool{} // "schema.table"
	for _, e := range entries {
		if e.Source == source && strings.EqualFold(e.Column, "id") {
			hasID[e.Schema+"."+e.Table] = true
		}
	}

	for _, e := range entries {
		if e.Source != source || e.Column == "" || self(e) {
			continue
		}
		col := strings.ToLower(e.Column)
		switch {
		case col == tbl+"_id":
			add(Inferred{"inbound", "id", e.Schema, e.Table, e.Column, 0.9, "table_id_match"})
		case single != tbl && col == single+"_id":
			add(Inferred{"inbound", "id", e.Schema, e.Table, e.Column, 0.8, "singular_id_match"})
		default:
			for _, p := range pk {
				if !strings.EqualFold(p, "id") && strings.EqualFold(col, p) {
					add(Inferred{"inbound", p, e.Schema, e.Table, e.Column, 0.5, "name_match"})
				}
			}
		}
	}

	for _, own := range ownCols {
		base, ok := strings.CutSuffix(strings.ToLower(own), "_id")
		if !ok || base == "" {
			continue
		}
		for _, e := range entries {
			if e.Source != source || e.Column != "" || self(e) {
				continue
			}
			if !matchesPlural(strings.ToLower(e.Table), base) || !hasID[e.Schema+"."+e.Table] {
				continue
			}
			conf := 0.7
			if e.Schema != schema {
				conf = 0.5
			}
			add(Inferred{"outbound", own, e.Schema, e.Table, "id", conf, "column_suffix_id"})
		}
	}

	// Cap each direction separately so a popular table's many inbound
	// references don't crowd out its own outbound joins.
	sort.SliceStable(out, func(a, b int) bool { return out[a].Confidence > out[b].Confidence })
	kept := out[:0]
	count := map[string]int{}
	for _, r := range out {
		if max <= 0 || count[r.Direction] < max {
			count[r.Direction]++
			kept = append(kept, r)
		}
	}
	return kept
}

// singular is a deliberately naive English singularizer for table names.
func singular(s string) string {
	switch {
	case strings.HasSuffix(s, "ies") && len(s) > 3:
		return s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "ses"), strings.HasSuffix(s, "xes"):
		return s[:len(s)-2]
	case strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss"):
		return s[:len(s)-1]
	}
	return s
}

// matchesPlural reports whether table is base or a plural form of it.
func matchesPlural(table, base string) bool {
	return table == base || singular(table) == base
}
