// Package mongodb implements the Adapter contract for MongoDB.
//
// Discovery prefix: MONGO_
// Per-source env vars (NAME uppercase, e.g. MONGO_ANALYTICS_HOST):
//
// Either set a full connection URI...
//
//	MONGO_<NAME>_URI  e.g. mongodb://user:pass@host:27017/dbname
//
// ...or supply the parts individually:
//
//	MONGO_<NAME>_HOST     (required)
//	MONGO_<NAME>_PORT     (default 27017)
//	MONGO_<NAME>_USER     (optional)
//	MONGO_<NAME>_PASS     (optional)
//	MONGO_<NAME>_DB       (default empty — default database for the connection)
//	MONGO_<NAME>_AUTH_DB  (default "admin")
//
// MongoDB is a document store, so the generic relational concepts are mapped
// as follows:
//
//   - schema   -> database
//   - table    -> collection
//   - columns  -> top-level field names inferred from a sample document
//
// ExecuteQuery and FindRelationships return ErrNotSupported — use the mongo_*
// tools (mongo_find, mongo_aggregate, ...) for real access via Client().
package mongodb

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/open-db-mcp/open-db-mcp/internal/adapters"
	"github.com/open-db-mcp/open-db-mcp/internal/config"
)

type Adapter struct {
	mu      sync.Mutex
	clients map[string]*mongo.Client
	cfgs    map[string]adapters.Source
}

func New() *Adapter {
	return &Adapter{
		clients: map[string]*mongo.Client{},
		cfgs:    map[string]adapters.Source{},
	}
}

func (a *Adapter) Kind() adapters.Kind { return adapters.KindMongoDB }
func (a *Adapter) EnvPrefix() string   { return "MONGO_" }

func (a *Adapter) Discover(env map[string]string) ([]adapters.Source, error) {
	groups := config.ParsePrefixed(env, a.EnvPrefix())
	var out []adapters.Source
	for name, cfg := range groups {
		uri := cfg["URI"]
		host := cfg["HOST"]
		if uri == "" && host == "" {
			continue // not a complete source, skip silently
		}
		src := adapters.Source{
			Name: name,
			Kind: a.Kind(),
			Cfg: map[string]string{
				"uri":     uri,
				"host":    host,
				"port":    orDefault(cfg["PORT"], "27017"),
				"user":    cfg["USER"],
				"pass":    cfg["PASS"],
				"db":      cfg["DB"],
				"auth_db": orDefault(cfg["AUTH_DB"], "admin"),
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
	if cli, ok := a.clients[src.Name]; ok {
		a.mu.Unlock()
		return &conn{client: cli, defaultDB: src.Cfg["db"]}, nil
	}
	a.mu.Unlock()

	uri := src.Cfg["uri"]
	if uri == "" {
		uri = buildURI(src.Cfg)
	}

	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	clientOpts := options.Client().ApplyURI(uri).SetServerSelectionTimeout(5 * time.Second)
	cli, err := mongo.Connect(clientOpts)
	if err != nil {
		return nil, fmt.Errorf("mongo connect %s: %w", src.Name, err)
	}
	if err := cli.Ping(cctx, nil); err != nil {
		_ = cli.Disconnect(context.Background())
		return nil, fmt.Errorf("mongo ping %s: %w", src.Name, err)
	}

	a.mu.Lock()
	a.clients[src.Name] = cli
	a.mu.Unlock()
	return &conn{client: cli, defaultDB: src.Cfg["db"]}, nil
}

func (a *Adapter) CloseAll() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range a.clients {
		_ = c.Disconnect(context.Background())
	}
	a.clients = map[string]*mongo.Client{}
	return nil
}

// Client returns the underlying mongo client for a source — used by mongo_* tools.
func (a *Adapter) Client(name string) *mongo.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.clients[name]
}

// buildURI assembles a mongodb:// connection string from individual cfg parts.
func buildURI(cfg map[string]string) string {
	var userinfo string
	if u := cfg["user"]; u != "" {
		if p := cfg["pass"]; p != "" {
			userinfo = url.QueryEscape(u) + ":" + url.QueryEscape(p) + "@"
		} else {
			userinfo = url.QueryEscape(u) + "@"
		}
	}
	hostPort := fmt.Sprintf("%s:%s", cfg["host"], cfg["port"])
	path := ""
	if cfg["db"] != "" {
		path = "/" + cfg["db"]
	}
	q := ""
	if cfg["user"] != "" && cfg["auth_db"] != "" {
		if path == "" {
			path = "/"
		}
		q = "?authSource=" + url.QueryEscape(cfg["auth_db"])
	}
	return "mongodb://" + userinfo + hostPort + path + q
}

// ---- Conn ----

type conn struct {
	client    *mongo.Client
	defaultDB string
}

func (c *conn) Close() error { return nil } // client lifetime owned by Adapter

// Client exposes the underlying mongo client to mongo_* tools through the Conn.
func (c *conn) Client() *mongo.Client { return c.client }

// systemDBs are filtered out of ListSchemas — they're MongoDB internals.
var systemDBs = map[string]bool{
	"admin":  true,
	"config": true,
	"local":  true,
}

func (c *conn) ListSchemas(ctx context.Context) ([]string, error) {
	names, err := c.client.ListDatabaseNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range names {
		if systemDBs[n] {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

func (c *conn) ListTables(ctx context.Context, schema string) ([]adapters.TableInfo, error) {
	db := c.resolveDB(schema)
	if db == "" {
		return nil, fmt.Errorf("mongo: no database specified")
	}
	names, err := c.client.Database(db).ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	out := make([]adapters.TableInfo, 0, len(names))
	for _, n := range names {
		out = append(out, adapters.TableInfo{
			Schema: db,
			Name:   n,
			Kind:   "table",
		})
	}
	return out, nil
}

func (c *conn) ListColumns(ctx context.Context, schema, table string) ([]adapters.ColumnInfo, error) {
	db := c.resolveDB(schema)
	if db == "" {
		return nil, fmt.Errorf("mongo: no database specified")
	}
	coll := c.client.Database(db).Collection(table)
	var doc bson.M
	if err := coll.FindOne(ctx, bson.D{}).Decode(&doc); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	out := make([]adapters.ColumnInfo, 0, len(doc))
	for k, v := range doc {
		out = append(out, adapters.ColumnInfo{
			Name:     k,
			Type:     goTypeName(v),
			Nullable: true,
		})
	}
	return out, nil
}

func (c *conn) TableStats(ctx context.Context, schema, table string) (adapters.TableStats, error) {
	db := c.resolveDB(schema)
	if db == "" {
		return adapters.TableStats{}, fmt.Errorf("mongo: no database specified")
	}
	cmd := bson.D{{Key: "collStats", Value: table}}
	var res bson.M
	if err := c.client.Database(db).RunCommand(ctx, cmd).Decode(&res); err != nil {
		return adapters.TableStats{}, err
	}
	var ts adapters.TableStats
	if v, ok := res["count"]; ok {
		ts.RowEstimate = toInt64(v)
	}
	if v, ok := res["size"]; ok {
		ts.SizeBytes = toInt64(v)
	}
	if idx, ok := res["indexSizes"].(bson.M); ok {
		for name := range idx {
			ts.Indexes = append(ts.Indexes, name)
		}
	}
	return ts, nil
}

func (c *conn) SampleRows(ctx context.Context, schema, table string, limit int) ([]map[string]any, error) {
	db := c.resolveDB(schema)
	if db == "" {
		return nil, fmt.Errorf("mongo: no database specified")
	}
	if limit <= 0 {
		limit = 5
	}
	coll := c.client.Database(db).Collection(table)
	findOpts := options.Find().SetLimit(int64(limit))
	cur, err := coll.Find(ctx, bson.D{}, findOpts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var out []map[string]any
	for cur.Next(ctx) {
		var m bson.M
		if err := cur.Decode(&m); err != nil {
			return nil, err
		}
		out = append(out, map[string]any(m))
	}
	return out, cur.Err()
}

func (c *conn) FindRelationships(_ context.Context, _, _ string) ([]adapters.Relationship, error) {
	return nil, adapters.ErrNotSupported
}

func (c *conn) ExecuteQuery(_ context.Context, _ adapters.Query) (adapters.QueryResult, error) {
	return adapters.QueryResult{}, adapters.ErrNotSupported
}

// resolveDB picks the explicitly-requested schema, falling back to the
// connection's default database.
func (c *conn) resolveDB(schema string) string {
	if schema != "" {
		return schema
	}
	return c.defaultDB
}

// goTypeName returns a short, human-readable Go type label for a decoded BSON value.
func goTypeName(v any) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case bson.ObjectID:
		return "ObjectID"
	case bson.DateTime:
		return "DateTime"
	case bson.Decimal128:
		return "Decimal128"
	case bson.Binary:
		return "Binary"
	case bson.M:
		return "object"
	case bson.D:
		return "object"
	case bson.A:
		return "array"
	case string:
		return "string"
	case bool:
		return "bool"
	case int32:
		return "int32"
	case int64:
		return "int64"
	case int:
		return "int"
	case float32:
		return "float32"
	case float64:
		return "float64"
	case time.Time:
		return "time.Time"
	}
	t := reflect.TypeOf(v)
	if t == nil {
		return "null"
	}
	return t.String()
}

// toInt64 coerces a BSON-decoded numeric value to int64.
func toInt64(v any) int64 {
	switch n := v.(type) {
	case int32:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case float32:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
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
