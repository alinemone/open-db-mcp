// Package mysql implements the Adapter contract for MySQL/MariaDB.
//
// Discovery prefix: MYSQL_
// Per-source env vars (NAME is uppercase, e.g. MYSQL_APP_HOST):
//
//	MYSQL_<NAME>_HOST   (required)
//	MYSQL_<NAME>_PORT   (default 3306)
//	MYSQL_<NAME>_USER   (required)
//	MYSQL_<NAME>_PASS   (default empty)
//	MYSQL_<NAME>_DB     (required — used as the default schema)
//	MYSQL_<NAME>_TLS    (default empty; e.g. "true" / "skip-verify")
//
// MySQL has no schemas in the Postgres sense; it uses databases instead. To
// keep the Adapter interface uniform, we treat each database the source can
// reach as a schema.
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql" // driver

	"github.com/open-db-mcp/open-db-mcp/internal/adapters"
	"github.com/open-db-mcp/open-db-mcp/internal/config"
)

type Adapter struct {
	mu  sync.Mutex
	dbs map[string]*sql.DB
}

func New() *Adapter { return &Adapter{dbs: map[string]*sql.DB{}} }

func (a *Adapter) Kind() adapters.Kind { return adapters.KindMySQL }
func (a *Adapter) EnvPrefix() string   { return "MYSQL_" }

func (a *Adapter) Discover(env map[string]string) ([]adapters.Source, error) {
	groups := config.ParsePrefixed(env, a.EnvPrefix())
	var out []adapters.Source
	for name, cfg := range groups {
		if cfg["HOST"] == "" || cfg["USER"] == "" || cfg["DB"] == "" {
			continue
		}
		out = append(out, adapters.Source{
			Name: name,
			Kind: a.Kind(),
			Cfg: map[string]string{
				"host":  cfg["HOST"],
				"port":  orDefault(cfg["PORT"], "3306"),
				"user":  cfg["USER"],
				"pass":  cfg["PASS"],
				"db":    cfg["DB"],
				"tls":   cfg["TLS"],
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
		return &conn{db: db, defaultDB: src.Cfg["db"]}, nil
	}
	a.mu.Unlock()

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&readTimeout=300s&writeTimeout=300s",
		src.Cfg["user"], src.Cfg["pass"], src.Cfg["host"], src.Cfg["port"], src.Cfg["db"])
	if tls := src.Cfg["tls"]; tls != "" {
		dsn += "&tls=" + tls
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("mysql open %s: %w", src.Name, err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(30 * time.Second)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mysql ping %s: %w", src.Name, err)
	}

	a.mu.Lock()
	a.dbs[src.Name] = db
	a.mu.Unlock()
	return &conn{db: db, defaultDB: src.Cfg["db"]}, nil
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

type conn struct {
	db        *sql.DB
	defaultDB string
}

func (c *conn) Close() error { return nil }

func (c *conn) ListSchemas(ctx context.Context) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT schema_name FROM information_schema.schemata
WHERE schema_name NOT IN ('mysql','information_schema','performance_schema','sys')
ORDER BY schema_name`)
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
	rows, err := c.db.QueryContext(ctx, `SELECT table_name, table_type FROM information_schema.tables
WHERE table_schema = ? ORDER BY table_name`, schema)
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
		out = append(out, adapters.TableInfo{Schema: schema, Name: name, Kind: normalizeKind(kind)})
	}
	return out, rows.Err()
}

func normalizeKind(s string) string {
	switch strings.ToUpper(s) {
	case "BASE TABLE":
		return "table"
	case "VIEW":
		return "view"
	}
	return strings.ToLower(s)
}

func (c *conn) ListColumns(ctx context.Context, schema, table string) ([]adapters.ColumnInfo, error) {
	rows, err := c.db.QueryContext(ctx,
		`SELECT column_name, column_type, is_nullable, COALESCE(column_default,'')
FROM information_schema.columns
WHERE table_schema = ? AND table_name = ?
ORDER BY ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapters.ColumnInfo
	for rows.Next() {
		var ci adapters.ColumnInfo
		var null string
		if err := rows.Scan(&ci.Name, &ci.Type, &null, &ci.Default); err != nil {
			return nil, err
		}
		ci.Nullable = strings.EqualFold(null, "YES")
		out = append(out, ci)
	}
	return out, rows.Err()
}

func (c *conn) TableStats(ctx context.Context, schema, table string) (adapters.TableStats, error) {
	var ts adapters.TableStats
	err := c.db.QueryRowContext(ctx,
		`SELECT COALESCE(data_length+index_length,0), COALESCE(table_rows,0)
FROM information_schema.tables
WHERE table_schema = ? AND table_name = ?`, schema, table).Scan(&ts.SizeBytes, &ts.RowEstimate)
	if err != nil && err != sql.ErrNoRows {
		return ts, err
	}
	rows, err := c.db.QueryContext(ctx,
		`SELECT DISTINCT index_name FROM information_schema.statistics
WHERE table_schema = ? AND table_name = ?`, schema, table)
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

func (c *conn) SampleRows(ctx context.Context, schema, table string, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 5
	}
	q := fmt.Sprintf("SELECT * FROM `%s`.`%s` LIMIT %d", schema, table, limit)
	return queryToMaps(ctx, c.db, q)
}

func (c *conn) FindRelationships(ctx context.Context, schema, table string) ([]adapters.Relationship, error) {
	rows, err := c.db.QueryContext(ctx,
		`SELECT constraint_name, column_name, referenced_table_schema, referenced_table_name, referenced_column_name
FROM information_schema.key_column_usage
WHERE table_schema = ? AND table_name = ? AND referenced_table_name IS NOT NULL`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapters.Relationship
	for rows.Next() {
		var r adapters.Relationship
		var toSchema, toTable, toCol sql.NullString
		if err := rows.Scan(&r.Name, &r.FromColumn, &toSchema, &toTable, &toCol); err != nil {
			return nil, err
		}
		r.FromSchema, r.FromTable = schema, table
		r.ToSchema, r.ToTable, r.ToColumn = toSchema.String, toTable.String, toCol.String
		out = append(out, r)
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

	// Writes (INSERT/UPDATE/DELETE/DDL) go through Exec so we can return
	// rows-affected. SELECT-shaped queries keep the streaming path.
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

// looksLikeSelect inspects the first keyword to decide between Exec and Query.
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

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func init() {
	adapters.Register(New())
}
