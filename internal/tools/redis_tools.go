package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/open-db-mcp/open-db-mcp/internal/adapters"
	redisad "github.com/open-db-mcp/open-db-mcp/internal/adapters/redis"
	"github.com/open-db-mcp/open-db-mcp/internal/format"
	"github.com/open-db-mcp/open-db-mcp/internal/mcp"
)

// RegisterRedis attaches the redis_* tools to s. They are only useful for
// REDIS_* sources, but they are always registered — the handlers return a
// helpful error when called against the wrong source.
func RegisterRedis(s *mcp.Server, d *Deps) {
	s.RegisterTool(mcp.Tool{
		Name:        "redis_keys",
		Description: "List Redis keys matching a glob pattern (uses SCAN, not KEYS).",
		InputSchema: schemaObj(map[string]any{
			"source":  map[string]any{"type": "string"},
			"pattern": map[string]any{"type": "string", "default": "*"},
			"limit":   map[string]any{"type": "number", "default": 100},
		}, "source"),
		Handler: d.redisKeys,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "redis_get",
		Description: "Get the value of a single key (auto-detects type: string/list/set/hash/zset).",
		InputSchema: schemaObj(map[string]any{
			"source": map[string]any{"type": "string"},
			"key":    map[string]any{"type": "string"},
		}, "source", "key"),
		Handler: d.redisGet,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "redis_info",
		Description: "Return INFO output for the Redis server (sections supported, e.g. memory, clients).",
		InputSchema: schemaObj(map[string]any{
			"source":  map[string]any{"type": "string"},
			"section": map[string]any{"type": "string", "default": "default"},
		}, "source"),
		Handler: d.redisInfo,
	})
}

func (d *Deps) redisClient(name string) (*redisAdapterClient, error) {
	sr, err := d.findSource(name)
	if err != nil {
		return nil, err
	}
	if sr.Source.Kind != adapters.KindRedis {
		return nil, fmt.Errorf("source %s is not a redis source (kind=%s)", name, sr.Source.Kind)
	}
	a, ok := sr.Adapter.(*redisad.Adapter)
	if !ok {
		return nil, fmt.Errorf("internal: source %s is registered with a non-redis adapter", name)
	}
	cli := a.Client(sr.Source.Name)
	if cli == nil {
		// Force connect by calling Adapter.Connect; that fills the pool.
		if _, err := sr.Adapter.Connect(context.Background(), sr.Source); err != nil {
			return nil, err
		}
		cli = a.Client(sr.Source.Name)
	}
	return &redisAdapterClient{cli: cli}, nil
}

// redisAdapterClient is a thin shim so this file doesn't import go-redis
// directly — the redis adapter package returns the typed client to us.
type redisAdapterClient struct{ cli any }

func (d *Deps) redisKeys(ctx context.Context, args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		pattern = "*"
	}
	limit := 100
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}
	c, err := d.redisClient(src)
	if err != nil {
		return "", err
	}
	keys, err := redisScan(ctx, c.cli, pattern, limit)
	if err != nil {
		return "", err
	}
	rows := make([]map[string]any, len(keys))
	for i, k := range keys {
		rows[i] = map[string]any{"key": k}
	}
	return format.ToTOON("Keys", rows), nil
}

func (d *Deps) redisGet(ctx context.Context, args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	key, _ := args["key"].(string)
	if key == "" {
		return "", fmt.Errorf("key is required")
	}
	c, err := d.redisClient(src)
	if err != nil {
		return "", err
	}
	kind, value, err := redisGetAny(ctx, c.cli, key)
	if err != nil {
		return "", err
	}
	return format.ToTOON("Value", []map[string]any{{
		"key": key, "type": kind, "value": value,
	}}), nil
}

func (d *Deps) redisInfo(ctx context.Context, args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	section, _ := args["section"].(string)
	if section == "" {
		section = "default"
	}
	c, err := d.redisClient(src)
	if err != nil {
		return "", err
	}
	raw, err := redisInfo(ctx, c.cli, section)
	if err != nil {
		return "", err
	}
	// INFO returns key:value lines. Convert to TOON rows.
	rows := []map[string]any{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		rows = append(rows, map[string]any{"key": k, "value": v})
	}
	return format.ToTOON("Info", rows), nil
}
