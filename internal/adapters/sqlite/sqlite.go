// Package sqlite implements the Adapter contract for SQLite.
//
// Discovery prefix: SQLITE_
// Per-source env vars (NAME uppercase, e.g. SQLITE_LOCAL_PATH):
//
//	SQLITE_<NAME>_PATH (required — absolute path or relative to cwd)
//
// The single logical "schema" in SQLite is "main"; that's what we expose.
//
// Uses modernc.org/sqlite (pure Go, no CGO) so the resulting binary is fully
// static and compatible with distroless images.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver

	"github.com/open-db-mcp/open-db-mcp/internal/adapters"
	"github.com/open-db-mcp/open-db-mcp/internal/config"
)

type Adapter struct {
	mu  sync.Mutex
	dbs map[string]*sql.DB
}

func New() *Adapter { return &Adapter{dbs: map[string]*sql.DB{}} }

func (a *Adapter) Kind() adapters.Kind { return adapters.KindSQLite }
func (a *Adapter) EnvPrefix() string   { return "SQLITE_" }

func (a *Adapter) Discover(env map[string]string) ([]adapters.Source, error) {
	groups := config.ParsePrefixed(env, a.EnvPrefix())
	var out []adapters.Source
	for name, cfg := range groups {
		if cfg["PATH"] == "" {
			continue
		}
		out = append(out, adapters.Source{
			Name: name,
			Kind: a.Kind(),
			Cfg: map[string]string{
				"path":  cfg["PATH"],
				"write": cfg["WRITE"], // "true" → db_execute_write allowed
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

	// query_only(true) is a hard ceiling: even a malicious caller cannot
	// mutate. We only drop it when the source has been explicitly opted into
	// write mode via SQLITE_<NAME>_WRITE=true.
	dsn := src.Cfg["path"] + "?_pragma=busy_timeout(5000)"
	if src.Cfg["write"] != "true" {
		dsn += "&_pragma=query_only(true)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite open %s: %w", src.Name, err)
	}
	db.SetMaxOpenConns(1) // SQLite is single-writer; small pool is fine
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite ping %s: %w", src.Name, err)
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

func (c *conn) ListSchemas(_ context.Context) ([]string, error) { return []string{"main"}, nil }

func (c *conn) ListTables(ctx context.Context, schema string) ([]adapters.TableInfo, error) {
	if schema == "" {
		schema = "main"
	}
	rows, err := c.db.QueryContext(ctx,
		`SELECT name, type FROM sqlite_master WHERE type IN ('table','view') ORDER BY name`)
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
		out = append(out, adapters.TableInfo{Schema: schema, Name: name, Kind: kind})
	}
	return out, rows.Err()
}

func (c *conn) ListColumns(ctx context.Context, schema, table string) ([]adapters.ColumnInfo, error) {
	if !isIdent(table) {
		return nil, fmt.Errorf("invalid table name")
	}
	rows, err := c.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%q)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapters.ColumnInfo
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out = append(out, adapters.ColumnInfo{
			Name: name, Type: typ, Nullable: notnull == 0, Default: dflt.String,
		})
	}
	return out, rows.Err()
}

func (c *conn) TableStats(ctx context.Context, schema, table string) (adapters.TableStats, error) {
	var ts adapters.TableStats
	if !isIdent(table) {
		return ts, fmt.Errorf("invalid table name")
	}
	_ = c.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %q", table)).Scan(&ts.RowEstimate)
	rows, err := c.db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='index' AND tbl_name = ?`, table)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var n string
			if rows.Scan(&n) == nil {
				ts.Indexes = append(ts.Indexes, n)
			}
		}
	}
	return ts, nil
}

func (c *conn) SampleRows(ctx context.Context, _, table string, limit int) ([]map[string]any, error) {
	if !isIdent(table) {
		return nil, fmt.Errorf("invalid table name")
	}
	if limit <= 0 {
		limit = 5
	}
	q := fmt.Sprintf("SELECT * FROM %q LIMIT %d", table, limit)
	return queryToMaps(ctx, c.db, q)
}

func (c *conn) FindRelationships(ctx context.Context, _, table string) ([]adapters.Relationship, error) {
	if !isIdent(table) {
		return nil, fmt.Errorf("invalid table name")
	}
	rows, err := c.db.QueryContext(ctx, fmt.Sprintf("PRAGMA foreign_key_list(%q)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapters.Relationship
	for rows.Next() {
		var id, seq int
		var refTable, fromCol, toCol, onUpd, onDel, match string
		if err := rows.Scan(&id, &seq, &refTable, &fromCol, &toCol, &onUpd, &onDel, &match); err != nil {
			return nil, err
		}
		out = append(out, adapters.Relationship{
			FromSchema: "main", FromTable: table, FromColumn: fromCol,
			ToSchema: "main", ToTable: refTable, ToColumn: toCol,
			Name: fmt.Sprintf("fk_%s_%d", table, id),
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
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
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
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
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
			v := vals[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			row[name] = v
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

func init() {
	adapters.Register(New())
}
