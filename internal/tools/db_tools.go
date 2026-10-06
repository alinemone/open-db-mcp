// Package tools wires generic MCP tools (db_*, search_tables) to the adapter
// registry. Per-database tools (mongo_*, redis_*, es_*, clog_*) live in their
// own files alongside this one.
package tools

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/open-db-mcp/open-db-mcp/internal/adapters"
	"github.com/open-db-mcp/open-db-mcp/internal/auth"
	"github.com/open-db-mcp/open-db-mcp/internal/clog"
	"github.com/open-db-mcp/open-db-mcp/internal/format"
	"github.com/open-db-mcp/open-db-mcp/internal/mcp"
	"github.com/open-db-mcp/open-db-mcp/internal/search"
)

// Deps bundles the runtime dependencies every tool may need. It is shared
// across tool families (db_*, es_*, clog_*, mongo_*, redis_*) so handlers in
// any file can reach the same resolved sources and indices.
type Deps struct {
	Sources []adapters.SourceRef // resolved once at startup
	Search  *search.Index

	// clogProfile is non-empty only when CLOG is enabled (CLOG_ES_SOURCE set).
	// Populated by RegisterCLOG; read by clog_* handlers.
	clogProfile clog.Profile
	clogCaps    capsCache // _field_caps cache for clog_* field resolution
}

// RegisterDB attaches the generic db_* tools to the MCP server.
func RegisterDB(s *mcp.Server, d *Deps) {
	s.RegisterTool(mcp.Tool{
		Name:        "db_list_sources",
		Description: "REQUIRED: Call FIRST. List every configured data source (postgres, mysql, clickhouse, mongodb, redis, sqlite, elasticsearch).",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler:     d.listSources,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "db_list_schemas",
		Description: "List schemas (or databases) on a source.",
		InputSchema: schemaObj(map[string]any{
			"source": map[string]any{"type": "string"},
		}, "source"),
		Handler: d.listSchemas,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "db_list_tables",
		Description: "List tables, views and materialized views in a schema.",
		InputSchema: schemaObj(map[string]any{
			"source": map[string]any{"type": "string"},
			"schema": map[string]any{"type": "string"},
			"search": map[string]any{"type": "string", "description": "Optional case-insensitive substring filter on table names"},
		}, "source", "schema"),
		Handler: d.listTables,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "db_list_columns",
		Description: "Compact column list for a table.",
		InputSchema: schemaObj(map[string]any{
			"source": map[string]any{"type": "string"},
			"schema": map[string]any{"type": "string"},
			"table":  map[string]any{"type": "string"},
		}, "source", "schema", "table"),
		Handler: d.listColumns,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "db_table_card",
		Description: "Identity card for a table: columns, stats, a few sample rows and warnings (huge table, timestamps without time zone).",
		InputSchema: schemaObj(map[string]any{
			"source":         map[string]any{"type": "string"},
			"schema":         map[string]any{"type": "string"},
			"table":          map[string]any{"type": "string"},
			"include_sample": map[string]any{"type": "boolean", "default": true},
		}, "source", "schema", "table"),
		Handler: d.tableCard,
	})
	cardFullSchema := schemaObj(map[string]any{
		"source":           map[string]any{"type": "string"},
		"schema":           map[string]any{"type": "string"},
		"table":            map[string]any{"type": "string"},
		"include_sample":   map[string]any{"type": "boolean", "default": true},
		"max_candidates":   map[string]any{"type": "number", "default": 50, "description": "Max heuristic relationship candidates (1-200)"},
		"include_create":   map[string]any{"type": "boolean", "default": true, "description": "Include the CREATE TABLE statement (ClickHouse)"},
		"max_create_chars": map[string]any{"type": "number", "default": 20000, "description": "Truncate CREATE TABLE after this many chars (1000-100000)"},
	}, "source", "schema", "table")
	s.RegisterTool(mcp.Tool{
		Name: "db_table_card_full",
		Description: "Full table card: everything in db_table_card plus primary key, foreign keys, tables referencing this one, " +
			"index definitions, engine/sorting/partition keys and CREATE TABLE (ClickHouse), heuristic relationships inferred " +
			"from column names, and query guidance (time columns, soft delete).",
		InputSchema: cardFullSchema,
		Handler:     d.tableCardFull,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "db_table_cardfull",
		Description: "Legacy alias of db_table_card_full.",
		InputSchema: cardFullSchema,
		Handler:     d.tableCardFull,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "db_find_relationships",
		Description: "Discover primary-key / foreign-key edges for a table (relational sources only).",
		InputSchema: schemaObj(map[string]any{
			"source": map[string]any{"type": "string"},
			"schema": map[string]any{"type": "string"},
			"table":  map[string]any{"type": "string"},
		}, "source", "schema", "table"),
		Handler: d.findRelationships,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "db_execute_query",
		Description: "Execute a read-only SQL query on a source. Returns TOON output. Destructive statements are rejected.",
		InputSchema: schemaObj(map[string]any{
			"source": map[string]any{"type": "string"},
			"query":  map[string]any{"type": "string"},
		}, "source", "query"),
		Handler: d.executeQuery,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "pg_execute_query",
		Description: "Legacy alias of db_execute_query (works on any SQL source).",
		InputSchema: schemaObj(map[string]any{
			"source": map[string]any{"type": "string"},
			"query":  map[string]any{"type": "string"},
		}, "source", "query"),
		Handler: d.executeQuery,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "db_execute_write",
		Description: "Execute a mutating SQL statement (INSERT/UPDATE/DELETE/DDL). REQUIRES the source to be explicitly marked writable via PG_<NAME>_WRITE=true (or MYSQL_<NAME>_WRITE=true, etc.). Defaults to off — most sources will refuse this call.",
		InputSchema: schemaObj(map[string]any{
			"source": map[string]any{"type": "string"},
			"query":  map[string]any{"type": "string"},
		}, "source", "query"),
		Handler: d.executeWrite,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "search_tables",
		Description: "Fuzzy search across tables and columns from every configured source. Tolerates one typo, matches spaces to underscores and expands synonyms (SEARCH_SYNONYMS); set use_regex for a regular expression.",
		InputSchema: schemaObj(map[string]any{
			"pattern":   map[string]any{"type": "string"},
			"scope":     map[string]any{"type": "string", "enum": []string{"all", "table", "column"}, "default": "all"},
			"use_regex": map[string]any{"type": "boolean", "default": false, "description": "Treat pattern as a case-insensitive regular expression"},
			"limit":     map[string]any{"type": "number", "default": 50},
		}, "pattern"),
		Handler: d.searchTables,
	})
}

func (d *Deps) listSources(ctx context.Context, _ map[string]any) (string, error) {
	rows := make([]map[string]any, 0, len(d.Sources))
	for _, sr := range d.Sources {
		rows = append(rows, map[string]any{
			"name": sr.Source.Name,
			"kind": string(sr.Source.Kind),
			"host": sr.Source.Cfg["host"],
		})
	}
	return format.ToTOON("Sources", rows), nil
}

// sqlKinds are the kinds the generic db_* tools prefer when two sources of
// different kinds share a name (e.g. PG_SALAMGATE_* and REDIS_SALAMGATE_*).
var sqlKinds = []adapters.Kind{
	adapters.KindPostgres, adapters.KindMySQL, adapters.KindClickHouse,
	adapters.KindSQLite, adapters.KindMongoDB,
}

// findSource resolves a source by name (case-insensitive). Names only have to
// be unique per kind, so when several kinds share a name the first kind in
// prefer wins; with no match among prefer, the first source found is used.
func (d *Deps) findSource(name string, prefer ...adapters.Kind) (adapters.SourceRef, error) {
	want := strings.ToUpper(name)
	var matches []adapters.SourceRef
	for _, sr := range d.Sources {
		if strings.ToUpper(sr.Source.Name) == want {
			matches = append(matches, sr)
		}
	}
	if len(matches) == 0 {
		return adapters.SourceRef{}, fmt.Errorf("source not found: %s", name)
	}
	for _, k := range prefer {
		for _, sr := range matches {
			if sr.Source.Kind == k {
				return sr, nil
			}
		}
	}
	return matches[0], nil
}

func (d *Deps) connect(ctx context.Context, name string) (adapters.Conn, adapters.SourceRef, error) {
	sr, err := d.findSource(name, sqlKinds...)
	if err != nil {
		return nil, sr, err
	}
	c, err := sr.Adapter.Connect(ctx, sr.Source)
	return c, sr, err
}

func (d *Deps) listSchemas(ctx context.Context, args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	conn, _, err := d.connect(ctx, src)
	if err != nil {
		return "", err
	}
	out, err := conn.ListSchemas(ctx)
	if err != nil {
		return "", err
	}
	rows := make([]map[string]any, len(out))
	for i, s := range out {
		rows[i] = map[string]any{"schema": s}
	}
	return format.ToTOON("Schemas", rows), nil
}

func (d *Deps) listTables(ctx context.Context, args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	schema, _ := args["schema"].(string)
	conn, _, err := d.connect(ctx, src)
	if err != nil {
		return "", err
	}
	tables, err := conn.ListTables(ctx, schema)
	if err != nil {
		return "", err
	}
	search, _ := args["search"].(string)
	search = strings.ToLower(strings.TrimSpace(search))
	rows := make([]map[string]any, 0, len(tables))
	for _, t := range tables {
		if search != "" && !strings.Contains(strings.ToLower(t.Name), search) {
			continue
		}
		rows = append(rows, map[string]any{"table": t.Name, "kind": t.Kind})
	}
	return format.ToTOON("Tables", rows), nil
}

func (d *Deps) listColumns(ctx context.Context, args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	schema, _ := args["schema"].(string)
	table, _ := args["table"].(string)
	conn, _, err := d.connect(ctx, src)
	if err != nil {
		return "", err
	}
	cols, err := conn.ListColumns(ctx, schema, table)
	if err != nil {
		return "", err
	}
	rows := make([]map[string]any, len(cols))
	for i, c := range cols {
		rows[i] = map[string]any{
			"name": c.Name, "type": c.Type, "nullable": c.Nullable, "default": c.Default,
		}
	}
	return format.ToTOON("Columns", rows), nil
}

func (d *Deps) findRelationships(ctx context.Context, args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	schema, _ := args["schema"].(string)
	table, _ := args["table"].(string)
	conn, _, err := d.connect(ctx, src)
	if err != nil {
		return "", err
	}
	rels, err := conn.FindRelationships(ctx, schema, table)
	if err != nil {
		if errors.Is(err, adapters.ErrNotSupported) {
			return "Relationships[0]{}:", nil
		}
		return "", err
	}
	rows := make([]map[string]any, len(rels))
	for i, r := range rels {
		rows[i] = map[string]any{
			"from_column": r.FromColumn,
			"to_table":    r.ToSchema + "." + r.ToTable,
			"to_column":   r.ToColumn,
			"constraint":  r.Name,
		}
	}
	return format.ToTOON("Relationships", rows), nil
}

func (d *Deps) executeQuery(ctx context.Context, args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	q, _ := args["query"].(string)
	if strings.TrimSpace(q) == "" {
		return "", fmt.Errorf("query is required")
	}
	conn, _, err := d.connect(ctx, src)
	if err != nil {
		return "", err
	}
	catalog, _ := args["catalog"].(string)
	schema, _ := args["schema"].(string)
	res, err := conn.ExecuteQuery(ctx, adapters.Query{SQL: q, Catalog: catalog, Schema: schema})
	if err != nil {
		return "", err
	}
	return format.ToTOONColumns("Result", res.Columns, res.Rows), nil
}

// executeWrite is the opt-in mutating counterpart of executeQuery.
//
// Two gates must both agree before a write is allowed:
//
//  1. The caller's role must be writer or admin (RBAC, ctx-bound).
//  2. The source must be explicitly marked writable in env, e.g.
//     PG_DEV_WRITE=true (per-source kill-switch).
//
// admin does NOT override (2) — the per-source flag is a deployment-level
// safety switch and keeps its meaning even for admins.
func (d *Deps) executeWrite(ctx context.Context, args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	q, _ := args["query"].(string)
	if strings.TrimSpace(q) == "" {
		return "", fmt.Errorf("query is required")
	}
	sr, err := d.findSource(src, sqlKinds...)
	if err != nil {
		return "", err
	}
	if err := requireWrite(ctx, sr); err != nil {
		return "", err
	}
	conn, err := sr.Adapter.Connect(ctx, sr.Source)
	if err != nil {
		return "", err
	}
	res, err := conn.ExecuteQuery(ctx, adapters.Query{SQL: q, Write: true})
	if err != nil {
		return "", err
	}
	return format.ToTOONColumns("WriteResult", res.Columns, res.Rows), nil
}

// requireWrite is the single point of truth for "may this caller write to this
// source?". Both the RBAC gate (role) and the deployment gate (source.write)
// must pass. Future write-capable tools (e.g. a mongo write tool) should call
// this helper instead of re-implementing the check.
func requireWrite(ctx context.Context, sr adapters.SourceRef) error {
	p := auth.PrincipalOf(ctx)
	if !p.CanWrite() {
		return fmt.Errorf("forbidden: user %s (role=%s) cannot write", p.Name, p.Role.String())
	}
	if sr.Source.Cfg["write"] != "true" {
		return fmt.Errorf(
			"source %s is read-only; set %s%s_ALLOW_WRITE=true in env to enable db_execute_write",
			sr.Source.Name, sr.Adapter.EnvPrefix(), sr.Source.Name,
		)
	}
	return nil
}

func (d *Deps) searchTables(_ context.Context, args map[string]any) (string, error) {
	pattern, _ := args["pattern"].(string)
	scope, _ := args["scope"].(string)
	if scope == "" {
		scope = "all"
	}
	limit := clampInt(numArg(args, "limit", 50), 1, 500)
	var results []search.Result
	if boolArg(args, "use_regex", false) {
		re, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			return "", fmt.Errorf("invalid params: bad regular expression: %v", err)
		}
		results = d.Search.SearchRegex(re, scope, limit)
	} else {
		results = d.Search.Search(pattern, scope, limit)
	}
	rows := make([]map[string]any, len(results))
	for i, r := range results {
		rows[i] = map[string]any{
			"source": r.Source,
			"kind":   r.Kind,
			"schema": r.Schema,
			"table":  r.Table,
			"column": r.Column,
			"score":  r.Score,
		}
	}
	return format.ToTOON("SearchResults", rows), nil
}

// schemaObj is a small helper that produces a JSON-schema "object" with the
// given properties and required field list.
func schemaObj(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}
