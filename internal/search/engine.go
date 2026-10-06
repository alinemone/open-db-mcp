// Package search provides a small fuzzy index over the union of all tables
// reachable through the registered adapters.
//
// It is intentionally not a full text-search engine — every embed adds 5–10 MB
// to the binary, and we only need substring-with-score matching for a few
// thousand symbols. Synonyms come from an optional, user-supplied map.
package search

import (
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Entry is one searchable row: a single table (or column) with the metadata
// callers may want to display.
type Entry struct {
	Source string // adapter source name, e.g. "ANALYTICS"
	Kind   string // adapter kind, e.g. "postgres"
	Schema string
	Table  string
	Column string // empty for table-level entries
	Type   string // column type, empty for table-level
}

// Index holds entries plus an optional synonyms map (canonical → variants).
type Index struct {
	mu       sync.RWMutex
	entries  []Entry
	synonyms map[string][]string
}

// New returns an empty index.
func New() *Index { return &Index{synonyms: map[string][]string{}} }

// Replace swaps the index contents atomically.
func (i *Index) Replace(entries []Entry) {
	i.mu.Lock()
	i.entries = entries
	i.mu.Unlock()
}

// SetSynonyms installs a synonym map (e.g. "order" → ["sale", "invoice"]).
func (i *Index) SetSynonyms(m map[string][]string) {
	i.mu.Lock()
	i.synonyms = m
	i.mu.Unlock()
}

// Size returns the number of indexed entries.
func (i *Index) Size() int {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.entries)
}

// Result is an entry plus its match score (higher = better).
type Result struct {
	Entry
	Score int
}

// Search returns the top matches for the query. Scope:
//
//	"table"  — only table-level entries (Column == "")
//	"column" — only column-level entries (Column != "")
//	"all"    — both (default)
//
// Spaces in the query match underscores ("order item" finds order_item).
// Scoring rules, in priority order:
//
//	+100 exact table name match
//	+50  query is a prefix of table name
//	+25  query is a substring of table name
//	+20  table name is one typo (edit) away from the query
//	+10  query is a substring of column name
//	+5   query is a substring of schema name
//	+30  per synonym hit
func (i *Index) Search(query, scope string, limit int) []Result {
	q := strings.Join(strings.Fields(strings.ToLower(query)), "_")
	if q == "" {
		return nil
	}
	i.mu.RLock()
	expanded := append([]string{q}, expandSynonyms(i.synonyms, q)...)
	entries := i.entries // safe: only replaced via Replace which swaps the slice header
	i.mu.RUnlock()

	return rank(entries, scope, limit, func(e Entry) int { return score(e, expanded) })
}

// SearchRegex returns entries whose table (or column, per scope) matches re.
// Every hit scores the same; results are ordered by source, schema, table.
func (i *Index) SearchRegex(re *regexp.Regexp, scope string, limit int) []Result {
	i.mu.RLock()
	entries := i.entries
	i.mu.RUnlock()
	return rank(entries, scope, limit, func(e Entry) int {
		target := e.Table
		if e.Column != "" {
			target = e.Column
		}
		if re.MatchString(target) {
			return 1
		}
		return 0
	})
}

func rank(entries []Entry, scope string, limit int, scoreFn func(Entry) int) []Result {
	if limit <= 0 {
		limit = 50
	}
	var out []Result
	for _, e := range entries {
		if scope == "table" && e.Column != "" {
			continue
		}
		if scope == "column" && e.Column == "" {
			continue
		}
		if s := scoreFn(e); s > 0 {
			out = append(out, Result{Entry: e, Score: s})
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Score != out[b].Score {
			return out[a].Score > out[b].Score
		}
		// Stable secondary sort for determinism.
		if out[a].Source != out[b].Source {
			return out[a].Source < out[b].Source
		}
		if out[a].Schema != out[b].Schema {
			return out[a].Schema < out[b].Schema
		}
		if out[a].Table != out[b].Table {
			return out[a].Table < out[b].Table
		}
		return out[a].Column < out[b].Column
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// expandSynonyms returns the other members of every synonym group q belongs
// to, whether q is the canonical word or one of its variants.
func expandSynonyms(groups map[string][]string, q string) []string {
	var out []string
	for canon, variants := range groups {
		members := append([]string{canon}, variants...)
		hit := false
		for _, m := range members {
			if strings.ToLower(m) == q {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		for _, m := range members {
			if m = strings.ToLower(m); m != q {
				out = append(out, m)
			}
		}
	}
	return out
}

// ParseSynonyms reads "canon:var1|var2;canon2:var3" (the SEARCH_SYNONYMS env
// format) into a synonym map. Malformed groups are skipped.
func ParseSynonyms(s string) map[string][]string {
	out := map[string][]string{}
	for _, group := range strings.Split(s, ";") {
		canon, vars, ok := strings.Cut(group, ":")
		canon = strings.ToLower(strings.TrimSpace(canon))
		if !ok || canon == "" {
			continue
		}
		for _, v := range strings.Split(vars, "|") {
			if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
				out[canon] = append(out[canon], v)
			}
		}
	}
	return out
}

func score(e Entry, queries []string) int {
	total := 0
	tbl := strings.ToLower(e.Table)
	col := strings.ToLower(e.Column)
	sch := strings.ToLower(e.Schema)

	primary := queries[0]
	switch {
	case tbl == primary:
		total += 100
	case strings.HasPrefix(tbl, primary):
		total += 50
	case strings.Contains(tbl, primary):
		total += 25
	case len(primary) >= 4 && withinOneEdit(tbl, primary):
		total += 20
	}
	if col != "" && strings.Contains(col, primary) {
		total += 10
	}
	if strings.Contains(sch, primary) {
		total += 5
	}

	for _, syn := range queries[1:] {
		if strings.Contains(tbl, syn) || strings.Contains(col, syn) {
			total += 30
		}
	}
	return total
}

// withinOneEdit reports whether a and b differ by at most one insertion,
// deletion or substitution.
func withinOneEdit(a, b string) bool {
	if len(a) < len(b) {
		a, b = b, a
	}
	if len(a)-len(b) > 1 {
		return false
	}
	i, j, edits := 0, 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			i++
			j++
			continue
		}
		if edits++; edits > 1 {
			return false
		}
		if len(a) == len(b) {
			j++
		}
		i++
	}
	return edits+(len(a)-i) <= 1
}
