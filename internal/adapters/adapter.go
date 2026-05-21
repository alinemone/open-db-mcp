// Package adapters defines the contract every database adapter must satisfy.
//
// Adding a new database is a single-file change:
//
//  1. Create internal/adapters/<dbname>/<dbname>.go
//  2. Implement the Adapter interface
//  3. Register your adapter in init()
//  4. Add a blank import in cmd/server/main.go
//
// See CONTRIBUTING.md for a worked example.
package adapters

import (
	"context"
	"errors"
)

// Kind identifies a database family. New kinds must be added here.
type Kind string

const (
	KindPostgres      Kind = "postgres"
	KindMySQL         Kind = "mysql"
	KindClickHouse    Kind = "clickhouse"
	KindMongoDB       Kind = "mongodb"
	KindRedis         Kind = "redis"
	KindSQLite        Kind = "sqlite"
	KindElasticsearch Kind = "elasticsearch"
)

// Source is a single connection target discovered from environment variables.
//
// Name is the user-facing identifier (e.g. "ANALYTICS" from PG_ANALYTICS_HOST).
// Cfg holds the lowercase, prefix-stripped env values (host, port, user, ...).
type Source struct {
	Name string
	Kind Kind
	Cfg  map[string]string
}

// TableInfo describes a single table or view in a schema.
type TableInfo struct {
	Schema  string
	Name    string
	Kind    string // "table", "view", "materialized_view"
	Comment string
}

// ColumnInfo describes a single column.
type ColumnInfo struct {
	Name     string
	Type     string
	Nullable bool
	Default  string
	Comment  string
}

// TableStats holds approximate counters for a table.
type TableStats struct {
	RowEstimate int64
	SizeBytes   int64
	Indexes     []string
}

// Relationship represents a foreign-key edge from one column to another.
type Relationship struct {
	FromSchema string
	FromTable  string
	FromColumn string
	ToSchema   string
	ToTable    string
	ToColumn   string
	Name       string
}

// Query carries an execute request.
type Query struct {
	SQL     string
	Catalog string // Trino-style sources can override; ignored elsewhere.
	Schema  string
}

// QueryResult is a generic tabular response.
type QueryResult struct {
	Columns  []string
	Rows     [][]any
	Affected int64
}

// ErrNotSupported is returned by Conn methods that don't apply to a given DB.
// Tools should treat this as "skip this adapter for this call" rather than fail.
var ErrNotSupported = errors.New("operation not supported by this adapter")

// Conn is a live, pooled connection to a single Source. Implementations must
// be safe for concurrent use.
type Conn interface {
	ListSchemas(ctx context.Context) ([]string, error)
	ListTables(ctx context.Context, schema string) ([]TableInfo, error)
	ListColumns(ctx context.Context, schema, table string) ([]ColumnInfo, error)
	TableStats(ctx context.Context, schema, table string) (TableStats, error)
	SampleRows(ctx context.Context, schema, table string, limit int) ([]map[string]any, error)
	FindRelationships(ctx context.Context, schema, table string) ([]Relationship, error)
	ExecuteQuery(ctx context.Context, q Query) (QueryResult, error)
	Close() error
}

// Adapter is the per-database plugin contract.
//
// One Adapter instance lives for the lifetime of the process. It is responsible
// for discovering its own Sources from env and lazily opening Conns to them.
type Adapter interface {
	Kind() Kind
	EnvPrefix() string
	Discover(env map[string]string) ([]Source, error)
	Connect(ctx context.Context, src Source) (Conn, error)
	CloseAll() error
}
