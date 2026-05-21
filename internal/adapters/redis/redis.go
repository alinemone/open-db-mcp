// Package redis implements the Adapter contract for Redis.
//
// Discovery prefix: REDIS_
// Per-source env vars (NAME uppercase, e.g. REDIS_CACHE_URL):
//
// Either set a single URL...
//
//	REDIS_<NAME>_URL  e.g. redis://:password@host:6379/0
//
// ...or supply host/port/auth/db individually:
//
//	REDIS_<NAME>_HOST (required)
//	REDIS_<NAME>_PORT (default 6379)
//	REDIS_<NAME>_PASS (default empty)
//	REDIS_<NAME>_DB   (default 0)
//
// Redis is not relational, so the generic db_* tools that need schemas/tables
// return ErrNotSupported. Use the redis_* tools (redis_keys, redis_get,
// redis_info) instead — they go through Client() below.
package redis

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/redis/go-redis/v9"

	"github.com/open-db-mcp/open-db-mcp/internal/adapters"
	"github.com/open-db-mcp/open-db-mcp/internal/config"
)

type Adapter struct {
	mu      sync.Mutex
	clients map[string]*redis.Client
}

func New() *Adapter { return &Adapter{clients: map[string]*redis.Client{}} }

func (a *Adapter) Kind() adapters.Kind { return adapters.KindRedis }
func (a *Adapter) EnvPrefix() string   { return "REDIS_" }

func (a *Adapter) Discover(env map[string]string) ([]adapters.Source, error) {
	groups := config.ParsePrefixed(env, a.EnvPrefix())
	var out []adapters.Source
	for name, cfg := range groups {
		if cfg["URL"] == "" && cfg["HOST"] == "" {
			continue
		}
		out = append(out, adapters.Source{
			Name: name,
			Kind: a.Kind(),
			Cfg: map[string]string{
				"url":  cfg["URL"],
				"host": cfg["HOST"],
				"port": orDefault(cfg["PORT"], "6379"),
				"pass": cfg["PASS"],
				"db":   orDefault(cfg["DB"], "0"),
			},
		})
	}
	return out, nil
}

func (a *Adapter) Connect(ctx context.Context, src adapters.Source) (adapters.Conn, error) {
	a.mu.Lock()
	if cli, ok := a.clients[src.Name]; ok {
		a.mu.Unlock()
		return &conn{client: cli}, nil
	}
	a.mu.Unlock()

	var opts *redis.Options
	if u := src.Cfg["url"]; u != "" {
		o, err := redis.ParseURL(u)
		if err != nil {
			return nil, fmt.Errorf("redis url %s: %w", src.Name, err)
		}
		opts = o
	} else {
		db, _ := strconv.Atoi(src.Cfg["db"])
		opts = &redis.Options{
			Addr:     fmt.Sprintf("%s:%s", src.Cfg["host"], src.Cfg["port"]),
			Password: src.Cfg["pass"],
			DB:       db,
		}
	}
	cli := redis.NewClient(opts)
	if err := cli.Ping(ctx).Err(); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("redis ping %s: %w", src.Name, err)
	}
	a.mu.Lock()
	a.clients[src.Name] = cli
	a.mu.Unlock()
	return &conn{client: cli}, nil
}

func (a *Adapter) CloseAll() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range a.clients {
		_ = c.Close()
	}
	a.clients = map[string]*redis.Client{}
	return nil
}

// Client returns the underlying redis client for source — used by redis_* tools.
func (a *Adapter) Client(name string) *redis.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.clients[name]
}

// ---- Conn ----

// conn satisfies adapters.Conn but most operations return ErrNotSupported,
// because Redis is not relational. Use the redis_* tools for real access.
type conn struct{ client *redis.Client }

func (c *conn) Close() error { return nil }

func (c *conn) ListSchemas(_ context.Context) ([]string, error) {
	return []string{"db0"}, nil
}

func (c *conn) ListTables(_ context.Context, _ string) ([]adapters.TableInfo, error) {
	return nil, adapters.ErrNotSupported
}
func (c *conn) ListColumns(_ context.Context, _, _ string) ([]adapters.ColumnInfo, error) {
	return nil, adapters.ErrNotSupported
}
func (c *conn) TableStats(_ context.Context, _, _ string) (adapters.TableStats, error) {
	return adapters.TableStats{}, adapters.ErrNotSupported
}
func (c *conn) SampleRows(_ context.Context, _, _ string, _ int) ([]map[string]any, error) {
	return nil, adapters.ErrNotSupported
}
func (c *conn) FindRelationships(_ context.Context, _, _ string) ([]adapters.Relationship, error) {
	return nil, adapters.ErrNotSupported
}
func (c *conn) ExecuteQuery(_ context.Context, _ adapters.Query) (adapters.QueryResult, error) {
	return adapters.QueryResult{}, adapters.ErrNotSupported
}

// Client exposes the underlying redis client to redis_* tools through the Conn.
func (c *conn) Client() *redis.Client { return c.client }

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func init() {
	adapters.Register(New())
}
