package tools

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/open-db-mcp/open-db-mcp/internal/adapters"
	"github.com/open-db-mcp/open-db-mcp/internal/format"
)

const (
	hugeTableBytes = 10 << 30 // 10 GiB
	hugeTableRows  = 10_000_000
)

var (
	timeColumnRe = regexp.MustCompile(`(?i)(_at|_time|_date|_ts)$|^(dt|date|time|timestamp|ts)$`)
	softDeleteRe = regexp.MustCompile(`(?i)^(deleted_at|is_deleted|deleted|removed_at)$`)
	statusColRe  = regexp.MustCompile(`(?i)^(status|state)$`)
)

func (d *Deps) tableCard(ctx context.Context, args map[string]any) (string, error) {
	return d.tableCardImpl(ctx, args, false)
}

func (d *Deps) tableCardFull(ctx context.Context, args map[string]any) (string, error) {
	return d.tableCardImpl(ctx, args, true)
}

// tableCardImpl renders the table card as TOON sections. The basic card has
// columns, stats, a sample and warnings; the full card adds keys, inbound
// references, index definitions, engine DDL, heuristic relationships and
// query guidance.
func (d *Deps) tableCardImpl(ctx context.Context, args map[string]any, full bool) (string, error) {
	src, _ := args["source"].(string)
	schema, _ := args["schema"].(string)
	table, _ := args["table"].(string)
	sample := boolArg(args, "include_sample", true)

	conn, sr, err := d.connect(ctx, src)
	if err != nil {
		return "", err
	}

	var sections []string
	add := func(s string) { sections = append(sections, s) }

	cols, err := conn.ListColumns(ctx, schema, table)
	if err != nil {
		return "", err
	}
	if len(cols) == 0 {
		return "", fmt.Errorf("invalid params: table %s.%s not found on source %s (see db_list_tables)", schema, table, sr.Source.Name)
	}
	colRows := make([]map[string]any, len(cols))
	colNames := make([]string, len(cols))
	for i, c := range cols {
		colRows[i] = map[string]any{
			"name": c.Name, "type": c.Type, "nullable": c.Nullable, "default": c.Default,
		}
		colNames[i] = c.Name
	}
	add(format.ToTOON("Columns", colRows))

	stats, statsErr := conn.TableStats(ctx, schema, table)
	if statsErr == nil {
		add(format.ToTOON("Stats", []map[string]any{{
			"size_bytes":   stats.SizeBytes,
			"size_human":   humanBytes(stats.SizeBytes),
			"row_estimate": stats.RowEstimate,
			"indexes":      strings.Join(stats.Indexes, "|"),
		}}))
	}

	if sample {
		if rows, err := conn.SampleRows(ctx, schema, table, 5); err == nil && len(rows) > 0 {
			add(format.ToTOON("Sample", rows))
		}
	}

	if w := tableWarnings(stats, statsErr == nil, cols); len(w) > 0 {
		add(format.ToTOON("Warnings", w))
	}

	if full {
		d.addFullCard(ctx, args, conn, sr, schema, table, colNames, add)
	}
	return strings.Join(sections, "\n\n"), nil
}

func (d *Deps) addFullCard(ctx context.Context, args map[string]any, conn adapters.Conn, sr adapters.SourceRef,
	schema, table string, colNames []string, add func(string)) {

	if rels, err := conn.FindRelationships(ctx, schema, table); err == nil && len(rels) > 0 {
		rows := make([]map[string]any, len(rels))
		for i, r := range rels {
			rows[i] = map[string]any{
				"from_column": r.FromColumn,
				"to_table":    r.ToSchema + "." + r.ToTable,
				"to_column":   r.ToColumn,
				"constraint":  r.Name,
			}
		}
		add(format.ToTOON("ForeignKeys", rows))
	}

	var details adapters.TableDetails
	if td, ok := conn.(adapters.TableDetailer); ok {
		var err error
		if details, err = td.TableDetails(ctx, schema, table); err != nil && !errors.Is(err, adapters.ErrNotSupported) {
			add("Note: table details unavailable: " + err.Error())
		}
	}
	if len(details.PrimaryKey) > 0 {
		add("PrimaryKey: " + strings.Join(details.PrimaryKey, ", "))
	}
	if len(details.ReferencedBy) > 0 {
		rows := make([]map[string]any, len(details.ReferencedBy))
		for i, r := range details.ReferencedBy {
			rows[i] = map[string]any{
				"from_table":  r.FromSchema + "." + r.FromTable,
				"from_column": r.FromColumn,
				"to_column":   r.ToColumn,
				"constraint":  r.Name,
			}
		}
		add(format.ToTOON("ReferencedBy", rows))
	}
	if len(details.Indexes) > 0 {
		rows := make([]map[string]any, len(details.Indexes))
		for i, ix := range details.Indexes {
			rows[i] = map[string]any{"name": ix.Name, "definition": ix.Definition}
		}
		add(format.ToTOON("Indexes", rows))
	}
	if details.Engine != "" {
		add(format.ToTOON("Engine", []map[string]any{{
			"engine":        details.Engine,
			"sorting_key":   details.SortingKey,
			"partition_key": details.PartitionKey,
			"primary_key":   strings.Join(details.PrimaryKey, ", "),
		}}))
	}
	if details.CreateSQL != "" && boolArg(args, "include_create", true) {
		maxChars := clampInt(numArg(args, "max_create_chars", 20000), 1000, 100000)
		ddl := details.CreateSQL
		if len(ddl) > maxChars {
			ddl = ddl[:maxChars] + "\n-- TRUNCATED (raise max_create_chars to see more)"
		}
		add("CreateTable:\n" + ddl)
	}

	maxCand := clampInt(numArg(args, "max_candidates", 50), 1, 200)
	if d.Search != nil && d.Search.Size() > 0 {
		inferred := d.Search.InferRelationships(sr.Source.Name, schema, table, colNames, details.PrimaryKey, maxCand)
		if len(inferred) > 0 {
			rows := make([]map[string]any, len(inferred))
			for i, r := range inferred {
				rows[i] = map[string]any{
					"direction":    r.Direction,
					"own_column":   r.OwnColumn,
					"other_table":  r.Schema + "." + r.Table,
					"other_column": r.Column,
					"confidence":   r.Confidence,
					"reason":       r.Reason,
				}
			}
			add("InferredRelationships are HEURISTIC (naming conventions, not declared FKs) — verify before joining.\n" +
				format.ToTOON("InferredRelationships", rows))
		}
	}

	if g := queryGuidance(colNames); len(g) > 0 {
		add(format.ToTOON("QueryGuidance", g))
	}
}

// tableWarnings flags tables that need careful querying.
func tableWarnings(stats adapters.TableStats, haveStats bool, cols []adapters.ColumnInfo) []map[string]any {
	var w []map[string]any
	if haveStats && stats.SizeBytes > hugeTableBytes {
		w = append(w, map[string]any{"warning": "huge table (" + humanBytes(stats.SizeBytes) +
			") — always filter on an indexed or time column and add LIMIT"})
	}
	if haveStats && stats.RowEstimate > hugeTableRows {
		w = append(w, map[string]any{"warning": fmt.Sprintf(
			"high row count (~%d) — avoid full scans; aggregate or filter first", stats.RowEstimate)})
	}
	var naive []string
	for _, c := range cols {
		if isNaiveTimestamp(c.Type) {
			naive = append(naive, c.Name)
		}
	}
	if len(naive) > 0 {
		w = append(w, map[string]any{"warning": "timestamps without time zone: " + strings.Join(naive, " ") +
			" — do not assume UTC; check the app's time zone"})
	}
	return w
}

func isNaiveTimestamp(typ string) bool {
	t := strings.ToLower(strings.TrimSpace(typ))
	t = strings.TrimSuffix(strings.TrimPrefix(t, "nullable("), ")")
	switch {
	case t == "timestamp", t == "timestamp without time zone", t == "datetime":
		return true
	case strings.HasPrefix(t, "datetime64(") && !strings.Contains(t, "'"):
		return true // ClickHouse DateTime64(p) without an explicit zone
	}
	return false
}

func queryGuidance(cols []string) []map[string]any {
	var g []map[string]any
	var timeCols []string
	soft, status := "", ""
	for _, c := range cols {
		if timeColumnRe.MatchString(c) {
			timeCols = append(timeCols, c)
		}
		if softDeleteRe.MatchString(c) && soft == "" {
			soft = c
		}
		if statusColRe.MatchString(c) && status == "" {
			status = c
		}
	}
	if len(timeCols) > 0 {
		g = append(g, map[string]any{"hint": "time columns for range filters: " + strings.Join(timeCols, " ")})
	}
	if soft != "" {
		g = append(g, map[string]any{"hint": "soft delete via " + soft + " — exclude deleted rows unless asked"})
	}
	if status != "" {
		g = append(g, map[string]any{"hint": "has a " + status + " column — check its distinct values before filtering"})
	}
	return g
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// boolArg reads a boolean tool argument, falling back to def when absent.
func boolArg(args map[string]any, key string, def bool) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return def
}

// numArg reads a numeric tool argument (JSON numbers arrive as float64).
func numArg(args map[string]any, key string, def int) int {
	if v, ok := args[key].(float64); ok {
		return int(v)
	}
	return def
}

func clampInt(v, lo, hi int) int {
	return max(lo, min(v, hi))
}
