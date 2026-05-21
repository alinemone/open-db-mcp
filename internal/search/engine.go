// Package search provides a small fuzzy index over the union of all tables
// reachable through the registered adapters.
//
// It is intentionally not a full text-search engine — every embed adds 5–10 MB
// to the binary, and we only need substring-with-score matching for a few
// thousand symbols. Synonyms come from an optional, user-supplied map.
package search

import (
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
// Scoring rules, in priority order:
//
//	+100 exact table name match
//	+50  query is a prefix of table name
//	+25  query is a substring of table name
//	+10  query is a substring of column name
//	+5   query is a substring of schema name
//	+30  per synonym hit
func (i *Index) Search(query, scope string, limit int) []Result {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	expanded := []string{q}
	i.mu.RLock()
	for _, v := range i.synonyms[q] {
		expanded = append(expanded, strings.ToLower(v))
	}
	entries := i.entries // safe: only replaced via Replace which swaps the slice header
	i.mu.RUnlock()

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
		s := score(e, expanded)
		if s > 0 {
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
		return out[a].Table < out[b].Table
	})
	if len(out) > limit {
		out = out[:limit]
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
