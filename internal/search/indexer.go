package search

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/open-db-mcp/open-db-mcp/internal/adapters"
)

// RefreshEvery walks every registered adapter and rebuilds the index. The
// caller usually runs it in a goroutine with a long ticker — once an hour is
// fine for a fleet of a few thousand tables.
func RefreshEvery(ctx context.Context, idx *Index, sources []adapters.SourceRef, interval time.Duration) {
	refresh := func() {
		entries, err := Build(ctx, sources)
		if err != nil {
			slog.WarnContext(ctx, "search index refresh failed", "err", err)
			return
		}
		idx.Replace(entries)
		slog.InfoContext(ctx, "search index refreshed", "entries", len(entries))
	}
	refresh()

	if interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			refresh()
		}
	}
}

// Build walks every (adapter, source) pair, opens a Conn, and harvests its
// tables + columns. Adapters that return ErrNotSupported for a given step are
// skipped silently — non-relational stores (Redis) just won't appear in the
// SQL-side index.
func Build(ctx context.Context, sources []adapters.SourceRef) ([]Entry, error) {
	var out []Entry
	for _, sr := range sources {
		conn, err := sr.Adapter.Connect(ctx, sr.Source)
		if err != nil {
			slog.WarnContext(ctx, "connect failed during indexing", "source", sr.Source.Name, "err", err)
			continue
		}
		schemas, err := conn.ListSchemas(ctx)
		if err != nil {
			if !errors.Is(err, adapters.ErrNotSupported) {
				slog.WarnContext(ctx, "ListSchemas failed", "source", sr.Source.Name, "err", err)
			}
			continue
		}
		for _, schema := range schemas {
			tables, err := conn.ListTables(ctx, schema)
			if err != nil {
				if !errors.Is(err, adapters.ErrNotSupported) {
					slog.DebugContext(ctx, "ListTables failed", "source", sr.Source.Name, "schema", schema, "err", err)
				}
				continue
			}
			for _, t := range tables {
				out = append(out, Entry{
					Source: sr.Source.Name,
					Kind:   string(sr.Source.Kind),
					Schema: schema,
					Table:  t.Name,
				})
				cols, err := conn.ListColumns(ctx, schema, t.Name)
				if err != nil {
					continue
				}
				for _, c := range cols {
					out = append(out, Entry{
						Source: sr.Source.Name,
						Kind:   string(sr.Source.Kind),
						Schema: schema,
						Table:  t.Name,
						Column: c.Name,
						Type:   c.Type,
					})
				}
			}
		}
	}
	return out, nil
}
