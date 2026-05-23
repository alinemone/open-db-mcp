// Package clickhouse implements the Adapter contract for ClickHouse.
//
// Discovery prefix: CH_
// Per-source env vars (NAME uppercase, e.g. CH_OLAP_HOST):
//
//	CH_<NAME>_HOST   (required)
//	CH_<NAME>_PORT   (default 9000 — native protocol)
//	CH_<NAME>_USER   (default "default")
//	CH_<NAME>_PASS   (default empty)
//	CH_<NAME>_DB     (default "default")
//	CH_<NAME>_SECURE (default empty; "true" enables TLS)
package clickhouse

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"

	"github.com/open-db-mcp/open-db-mcp/internal/adapters"
	"github.com/open-db-mcp/open-db-mcp/internal/config"
)

type Adapter struct {
	mu  sync.Mutex
	dbs map[string]*sql.DB
}

func New() *Adapter { return &Adapter{dbs: map[string]*sql.DB{}} }

func (a *Adapter) Kind() adapters.Kind { return adapters.KindClickHouse }
func (a *Adapter) EnvPrefix() string   { return "CH_" }

func (a *Adapter) Discover(env map[string]string) ([]adapters.Source, error) {
	groups := config.ParsePrefixed(env, a.EnvPrefix())
	var out []adapters.Source
	for name, cfg := range groups {
		if cfg["HOST"] == "" {
			continue
		}
		out = append(out, adapters.Source{
			Name: name,
			Kind: a.Kind(),
			Cfg: map[string]string{
				"host":   cfg["HOST"],
				"port":   orDefault(cfg["PORT"], "9000"),
				"user":   orDefault(cfg["USER"], "default"),
				"pass":   cfg["PASS"],
				"db":     orDefault(cfg["DB"], "default"),
				"secure": cfg["SECURE"],
				"write":  cfg["WRITE"], // "true" → db_execute_write allowed
			},
		})
	}
	return out, nil
}

func (a *Adapter) Connect(ctx context.Context, src adapters.Source) (adapters.Conn, error) {
	a.mu.Lock()
	if db, ok := a.dbs[src.Name]; ok {
		a.mu.Unlock()
		return &conn{db: db}, nil
	}
	a.mu.Unlock()

	port := 9000
	fmt.Sscanf(src.Cfg["port"], "%d", &port)

	opts := &clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%d", src.Cfg["host"], port)},
		Auth: clickhouse.Auth{
			Database: src.Cfg["db"],
			Username: src.Cfg["user"],
			Password: src.Cfg["pass"],
		},
		DialTimeout:     5 * time.Second,
		ConnMaxLifetime: 30 * time.Minute,
		MaxIdleConns:    2,
		MaxOpenConns:    5,
		Settings: clickhouse.Settings{
			"max_execution_time": 300,
		},
	}
	if src.Cfg["secure"] == "true" {
		opts.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	db := clickhouse.OpenDB(opts)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("clickhouse ping %s: %w", src.Name, err)
	}

	a.mu.Lock()
	a.dbs[src.Name] = db
	a.mu.Unlock()
	return &conn{db: db}, nil
}

func (a *Adapter) CloseAll() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, db := range a.dbs {
		_ = db.Close()
	}
	a.dbs = map[string]*sql.DB{}
	return nil
}

// ---- Conn ----

type conn struct{ db *sql.DB }

func (c *conn) Close() error { return nil }

func (c *conn) ListSchemas(ctx context.Context) ([]string, error) {
	rows, err := c.db.QueryContext(ctx,
		`SELECT name FROM system.databases WHERE name NOT IN ('system','INFORMATION_SCHEMA','information_schema') ORDER BY name`)
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

func (c *conn) ListTables(ctx context.Context, schema string) ([]adapters.TableInfo, error) {
	rows, err := c.db.QueryContext(ctx,
		`SELECT name, engine FROM system.tables WHERE database = ? ORDER BY name`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapters.TableInfo
	for rows.Next() {
		var name, engine string
		if err := rows.Scan(&name, &engine); err != nil {
			return nil, err
		}
		kind := "table"
		switch engine {
		case "View":
			kind = "view"
		case "MaterializedView":
			kind = "matview"
		}
		out = append(out, adapters.TableInfo{Schema: schema, Name: name, Kind: kind, Comment: engine})
	}
	return out, rows.Err()
}

func (c *conn) ListColumns(ctx context.Context, schema, table string) ([]adapters.ColumnInfo, error) {
	rows, err := c.db.QueryContext(ctx,
		`SELECT name, type, default_expression
FROM system.columns
WHERE database = ? AND table = ?
ORDER BY position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapters.ColumnInfo
	for rows.Next() {
		var ci adapters.ColumnInfo
		if err := rows.Scan(&ci.Name, &ci.Type, &ci.Default); err != nil {
			return nil, err
		}
		ci.Nullable = strings.Contains(ci.Type, "Nullable")
		out = append(out, ci)
	}
	return out, rows.Err()
}

func (c *conn) TableStats(ctx context.Context, schema, table string) (adapters.TableStats, error) {
	var ts adapters.TableStats
	err := c.db.QueryRowContext(ctx,
		`SELECT total_bytes, total_rows FROM system.tables WHERE database = ? AND table = ?`,
		schema, table).Scan(&ts.SizeBytes, &ts.RowEstimate)
	if err != nil && err != sql.ErrNoRows {
		return ts, err
	}
	return ts, nil
}

func (c *conn) SampleRows(ctx context.Context, schema, table string, limit int) ([]map[string]any, error) {
	if !isIdent(schema) || !isIdent(table) {
		return nil, fmt.Errorf("invalid identifiers")
	}
	if limit <= 0 {
		limit = 5
	}
	q := fmt.Sprintf("SELECT * FROM `%s`.`%s` LIMIT %d", schema, table, limit)
	return queryToMaps(ctx, c.db, q)
}

func (c *conn) FindRelationships(_ context.Context, _, _ string) ([]adapters.Relationship, error) {
	return nil, adapters.ErrNotSupported
}

func (c *conn) ExecuteQuery(ctx context.Context, q adapters.Query) (adapters.QueryResult, error) {
	if !q.Write {
		if err := adapters.AssertReadOnly(q.SQL); err != nil {
			return adapters.QueryResult{}, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	if q.Write && !looksLikeSelect(q.SQL) {
		res, err := c.db.ExecContext(ctx, q.SQL)
		if err != nil {
			return adapters.QueryResult{}, err
		}
		affected, _ := res.RowsAffected()
		return adapters.QueryResult{
			Columns:  []string{"rows_affected"},
			Rows:     [][]any{{affected}},
			Affected: affected,
		}, nil
	}

	// Read path: enable readonly=2 for this query only. Mode 2 still allows
	// SETTINGS changes (which the driver may issue), unlike mode 1.
	if !q.Write {
		ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
			"readonly": 2,
		}))
	}
	rows, err := c.db.QueryContext(ctx, q.SQL)
	if err != nil {
		return adapters.QueryResult{}, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return adapters.QueryResult{}, err
	}
	var data [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return adapters.QueryResult{}, err
		}
		data = append(data, vals)
	}
	return adapters.QueryResult{Columns: cols, Rows: data}, rows.Err()
}

func looksLikeSelect(sql string) bool {
	s := strings.TrimSpace(sql)
	if s == "" {
		return false
	}
	first := strings.ToUpper(strings.Fields(s)[0])
	return first == "SELECT" || first == "WITH" || first == "EXPLAIN" || first == "SHOW" || first == "DESCRIBE" || first == "DESC"
}

func queryToMaps(ctx context.Context, db *sql.DB, q string) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, name := range cols {
			row[name] = vals[i]
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
		if !ok {
			return false
		}
	}
	return true
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func init() {
	adapters.Register(New())
}
