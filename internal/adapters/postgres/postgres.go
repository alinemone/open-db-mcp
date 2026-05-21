// Package postgres implements the Adapter contract for PostgreSQL.
//
// Discovery prefix: PG_
// Per-source env vars (NAME is uppercase, e.g. PG_ANALYTICS_HOST):
//
//	PG_<NAME>_HOST   (required)
//	PG_<NAME>_PORT   (default 5432)
//	PG_<NAME>_USER   (required)
//	PG_<NAME>_PASS   (default empty)
//	PG_<NAME>_DB     (required)
//	PG_<NAME>_SSLMODE (default "prefer")
//
// To add a new database family, copy this file and replace the SQL queries —
// see CONTRIBUTING.md.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/open-db-mcp/open-db-mcp/internal/adapters"
	"github.com/open-db-mcp/open-db-mcp/internal/config"
)

type Adapter struct {
	mu    sync.Mutex
	pools map[string]*pgxpool.Pool // keyed by source name (upper)
	cfgs  map[string]adapters.Source
}

func New() *Adapter {
	return &Adapter{
		pools: map[string]*pgxpool.Pool{},
		cfgs:  map[string]adapters.Source{},
	}
}

func (a *Adapter) Kind() adapters.Kind { return adapters.KindPostgres }
func (a *Adapter) EnvPrefix() string   { return "PG_" }

func (a *Adapter) Discover(env map[string]string) ([]adapters.Source, error) {
	groups := config.ParsePrefixed(env, a.EnvPrefix())
	var out []adapters.Source
	for name, cfg := range groups {
		host := cfg["HOST"]
		db := cfg["DB"]
		user := cfg["USER"]
		if host == "" || db == "" || user == "" {
			continue // not a complete source, skip silently
		}
		src := adapters.Source{
			Name: name,
			Kind: a.Kind(),
			Cfg: map[string]string{
				"host":    host,
				"port":    defaultStr(cfg["PORT"], "5432"),
				"user":    user,
				"pass":    cfg["PASS"],
				"db":      db,
				"sslmode": defaultStr(cfg["SSLMODE"], "prefer"),
				"write":   cfg["WRITE"], // "true" → db_execute_write allowed
			},
		}
		a.mu.Lock()
		a.cfgs[name] = src
		a.mu.Unlock()
		out = append(out, src)
	}
	return out, nil
}

func (a *Adapter) Connect(ctx context.Context, src adapters.Source) (adapters.Conn, error) {
	a.mu.Lock()
	if p, ok := a.pools[src.Name]; ok {
		a.mu.Unlock()
		return &conn{pool: p, src: src.Name}, nil
	}
	a.mu.Unlock()

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s connect_timeout=5",
		src.Cfg["host"], src.Cfg["port"], src.Cfg["user"], src.Cfg["pass"], src.Cfg["db"], src.Cfg["sslmode"],
	)
	pcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("pg parse config %s: %w", src.Name, err)
	}
	pcfg.MaxConns = 5
	pcfg.MaxConnIdleTime = 30 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("pg connect %s: %w", src.Name, err)
	}

	a.mu.Lock()
	a.pools[src.Name] = pool
	a.mu.Unlock()
	return &conn{pool: pool, src: src.Name}, nil
}

func (a *Adapter) CloseAll() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range a.pools {
		p.Close()
	}
	a.pools = map[string]*pgxpool.Pool{}
	return nil
}

// ---- Conn ----

type conn struct {
	pool *pgxpool.Pool
	src  string
}

func (c *conn) Close() error { return nil } // pool lifetime owned by Adapter

const sqlListSchemas = `
SELECT schema_name
FROM information_schema.schemata
WHERE schema_name NOT IN ('information_schema', 'pg_catalog')
  AND schema_name NOT LIKE 'pg_toast%'
  AND schema_name NOT LIKE 'pg_temp%'
ORDER BY schema_name`

func (c *conn) ListSchemas(ctx context.Context) ([]string, error) {
	rows, err := c.pool.Query(ctx, sqlListSchemas)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

const sqlListTables = `
SELECT table_name, table_type
FROM information_schema.tables
WHERE table_schema = $1
  AND (table_type = 'BASE TABLE' OR table_type = 'VIEW')
UNION ALL
SELECT matviewname, 'MATERIALIZED VIEW'
FROM pg_matviews
WHERE schemaname = $1
ORDER BY 1`

func (c *conn) ListTables(ctx context.Context, schema string) ([]adapters.TableInfo, error) {
	rows, err := c.pool.Query(ctx, sqlListTables, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapters.TableInfo
	for rows.Next() {
		var name, kind string
		if err := rows.Scan(&name, &kind); err != nil {
			return nil, err
		}
		out = append(out, adapters.TableInfo{
			Schema: schema,
			Name:   name,
			Kind:   normalizeKind(kind),
		})
	}
	return out, rows.Err()
}

func normalizeKind(s string) string {
	switch strings.ToUpper(s) {
	case "BASE TABLE":
		return "table"
	case "VIEW":
		return "view"
	case "MATERIALIZED VIEW":
		return "matview"
	}
	return strings.ToLower(s)
}

const sqlListColumns = `
SELECT column_name, data_type, is_nullable, COALESCE(column_default, '')
FROM information_schema.columns
WHERE table_schema = $1 AND table_name = $2
ORDER BY ordinal_position`

func (c *conn) ListColumns(ctx context.Context, schema, table string) ([]adapters.ColumnInfo, error) {
	rows, err := c.pool.Query(ctx, sqlListColumns, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapters.ColumnInfo
	for rows.Next() {
		var ci adapters.ColumnInfo
		var nullable string
		if err := rows.Scan(&ci.Name, &ci.Type, &nullable, &ci.Default); err != nil {
			return nil, err
		}
		ci.Nullable = nullable == "YES"
		out = append(out, ci)
	}
	return out, rows.Err()
}

const sqlTableStats = `
SELECT
  COALESCE(pg_total_relation_size($1), 0) AS size_bytes,
  COALESCE((SELECT reltuples::bigint FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = $2 AND c.relname = $3), 0) AS row_estimate`

const sqlIndexes = `
SELECT indexname
FROM pg_indexes
WHERE schemaname = $1 AND tablename = $2`

func (c *conn) TableStats(ctx context.Context, schema, table string) (adapters.TableStats, error) {
	fqn := fmt.Sprintf("%q.%q", schema, table)
	var ts adapters.TableStats
	if err := c.pool.QueryRow(ctx, sqlTableStats, fqn, schema, table).Scan(&ts.SizeBytes, &ts.RowEstimate); err != nil {
		return ts, err
	}
	rows, err := c.pool.Query(ctx, sqlIndexes, schema, table)
	if err != nil {
		return ts, nil // stats already populated; indexes are optional
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err == nil {
			ts.Indexes = append(ts.Indexes, n)
		}
	}
	return ts, nil
}

func (c *conn) SampleRows(ctx context.Context, schema, table string, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 5
	}
	sql := fmt.Sprintf(`SELECT * FROM %q.%q LIMIT %d`, schema, table, limit)
	rows, err := c.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fieldDescs := rows.FieldDescriptions()
	var out []map[string]any
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make(map[string]any, len(fieldDescs))
		for i, fd := range fieldDescs {
			row[string(fd.Name)] = values[i]
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

const sqlFindRelationships = `
SELECT
  tc.constraint_name,
  tc.constraint_type,
  kcu.column_name AS from_column,
  COALESCE(ccu.table_schema, '') AS to_schema,
  COALESCE(ccu.table_name,   '') AS to_table,
  COALESCE(ccu.column_name,  '') AS to_column
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name
  AND tc.table_schema   = kcu.table_schema
LEFT JOIN information_schema.constraint_column_usage ccu
  ON ccu.constraint_name = tc.constraint_name
  AND ccu.table_schema   = tc.table_schema
WHERE tc.table_schema = $1 AND tc.table_name = $2
  AND tc.constraint_type IN ('PRIMARY KEY','FOREIGN KEY')`

func (c *conn) FindRelationships(ctx context.Context, schema, table string) ([]adapters.Relationship, error) {
	rows, err := c.pool.Query(ctx, sqlFindRelationships, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapters.Relationship
	for rows.Next() {
		var name, ctype, fromCol, toSchema, toTable, toCol string
		if err := rows.Scan(&name, &ctype, &fromCol, &toSchema, &toTable, &toCol); err != nil {
			return nil, err
		}
		if ctype != "FOREIGN KEY" {
			continue
		}
		out = append(out, adapters.Relationship{
			Name:       name,
			FromSchema: schema, FromTable: table, FromColumn: fromCol,
			ToSchema: toSchema, ToTable: toTable, ToColumn: toCol,
		})
	}
	return out, rows.Err()
}

func (c *conn) ExecuteQuery(ctx context.Context, q adapters.Query) (adapters.QueryResult, error) {
	if !q.Write {
		if err := adapters.AssertReadOnly(q.SQL); err != nil {
			return adapters.QueryResult{}, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	mode := pgx.ReadOnly
	if q.Write {
		mode = pgx.ReadWrite
	}
	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: mode})
	if err != nil {
		return adapters.QueryResult{}, err
	}
	// On error or read path we rollback; on a successful write we commit.
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	// Writes (INSERT/UPDATE/DELETE/DDL) generally don't return result rows.
	// Use Exec so we get rows-affected counts back instead of erroring out.
	if q.Write && !looksLikeSelect(q.SQL) {
		tag, err := tx.Exec(ctx, q.SQL)
		if err != nil {
			return adapters.QueryResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return adapters.QueryResult{}, err
		}
		committed = true
		return adapters.QueryResult{
			Columns:  []string{"rows_affected"},
			Rows:     [][]any{{tag.RowsAffected()}},
			Affected: tag.RowsAffected(),
		}, nil
	}

	rows, err := tx.Query(ctx, q.SQL)
	if err != nil {
		return adapters.QueryResult{}, err
	}
	defer rows.Close()

	descs := rows.FieldDescriptions()
	cols := make([]string, len(descs))
	for i, d := range descs {
		cols[i] = string(d.Name)
	}
	var data [][]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return adapters.QueryResult{}, err
		}
		data = append(data, vals)
	}
	if err := rows.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return adapters.QueryResult{}, err
	}
	if q.Write {
		if err := tx.Commit(ctx); err != nil {
			return adapters.QueryResult{}, err
		}
		committed = true
	}
	return adapters.QueryResult{Columns: cols, Rows: data}, nil
}

// looksLikeSelect peeks at the first keyword to decide between Exec and Query.
// We can't rely on AssertReadOnly here because write mode disables it.
func looksLikeSelect(sql string) bool {
	s := strings.TrimSpace(sql)
	if s == "" {
		return false
	}
	first := strings.ToUpper(strings.Fields(s)[0])
	return first == "SELECT" || first == "WITH" || first == "EXPLAIN" || first == "SHOW" || first == "DESCRIBE" || first == "DESC"
}

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func init() {
	adapters.Register(New())
}
