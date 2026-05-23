// Package elasticsearch implements the Adapter contract for Elasticsearch.
//
// Discovery prefix: ES_
// Per-source env vars (NAME is uppercase, e.g. ES_LOGS_URL):
//
// Either set a single URL...
//
//	ES_<NAME>_URL  e.g. https://host:9200
//
// ...or supply host/port individually:
//
//	ES_<NAME>_HOST          (alternative to URL)
//	ES_<NAME>_PORT          (default 9200)
//	ES_<NAME>_SCHEME        (default "http"; set "https" for TLS)
//
// Authentication (optional):
//
//	ES_<NAME>_USER          Basic auth username
//	ES_<NAME>_PASS          Basic auth password
//	ES_<NAME>_API_KEY       Base64-encoded "id:key" — takes precedence over USER/PASS
//
// TLS:
//
//	ES_<NAME>_INSECURE_TLS  "true" → skip TLS verification
//
// Elasticsearch is not relational, so the generic db_* tools that need columns,
// stats, samples or relationships return ErrNotSupported. Use the es_* tools
// instead — they reach the underlying client through Client() below.
package elasticsearch

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	es "github.com/elastic/go-elasticsearch/v8"

	"github.com/open-db-mcp/open-db-mcp/internal/adapters"
	"github.com/open-db-mcp/open-db-mcp/internal/config"
)

type Adapter struct {
	mu      sync.Mutex
	clients map[string]*es.Client
}

func New() *Adapter { return &Adapter{clients: map[string]*es.Client{}} }

func (a *Adapter) Kind() adapters.Kind { return adapters.KindElasticsearch }
func (a *Adapter) EnvPrefix() string   { return "ES_" }

func (a *Adapter) Discover(env map[string]string) ([]adapters.Source, error) {
	groups := config.ParsePrefixed(env, a.EnvPrefix())
	var out []adapters.Source
	for name, cfg := range groups {
		if cfg["URL"] == "" && cfg["HOST"] == "" {
			continue // incomplete source, skip silently
		}
		out = append(out, adapters.Source{
			Name: name,
			Kind: a.Kind(),
			Cfg: map[string]string{
				"url":          cfg["URL"],
				"host":         cfg["HOST"],
				"port":         orDefault(cfg["PORT"], "9200"),
				"scheme":       orDefault(strings.ToLower(cfg["SCHEME"]), "http"),
				"user":         cfg["USER"],
				"pass":         cfg["PASS"],
				"api_key":      cfg["API_KEY"],
				"insecure_tls": cfg["INSECURE_TLS"],
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

	addr := src.Cfg["url"]
	if addr == "" {
		scheme := src.Cfg["scheme"]
		if scheme == "" {
			scheme = "http"
		}
		addr = fmt.Sprintf("%s://%s:%s", scheme, src.Cfg["host"], src.Cfg["port"])
	}

	cfg := es.Config{
		Addresses: []string{addr},
	}
	if k := src.Cfg["api_key"]; k != "" {
		cfg.APIKey = k
	} else {
		cfg.Username = src.Cfg["user"]
		cfg.Password = src.Cfg["pass"]
	}
	if strings.EqualFold(src.Cfg["insecure_tls"], "true") {
		cfg.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // explicit opt-in
		}
	}

	cli, err := es.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("es client %s: %w", src.Name, err)
	}

	// Light ping via Info.
	res, err := cli.Info(cli.Info.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("es ping %s: %w", src.Name, err)
	}
	_ = res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("es ping %s: %s", src.Name, res.Status())
	}

	a.mu.Lock()
	a.clients[src.Name] = cli
	a.mu.Unlock()
	return &conn{client: cli}, nil
}

func (a *Adapter) CloseAll() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.clients = map[string]*es.Client{}
	return nil
}

// Client returns the underlying ES client for source — used by es_* tools.
func (a *Adapter) Client(name string) *es.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.clients[name]
}

// ---- Conn ----

// conn satisfies adapters.Conn but most operations return ErrNotSupported,
// because Elasticsearch is not relational. Use the es_* tools for real access.
type conn struct{ client *es.Client }

func (c *conn) Close() error { return nil }

// ListSchemas returns a single placeholder "schema" so generic introspection
// loops don't blow up. Elasticsearch has no schemas.
func (c *conn) ListSchemas(_ context.Context) ([]string, error) {
	return []string{"_all"}, nil
}

// ListTables returns the list of indices via _cat/indices?format=json.
func (c *conn) ListTables(ctx context.Context, schema string) ([]adapters.TableInfo, error) {
	_ = schema // ES has no schemas; ignore.
	res, err := c.client.Cat.Indices(
		c.client.Cat.Indices.WithContext(ctx),
		c.client.Cat.Indices.WithFormat("json"),
	)
	if err != nil {
		return nil, fmt.Errorf("es cat indices: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("es cat indices: %s", res.Status())
	}
	var rows []map[string]any
	if err := json.NewDecoder(res.Body).Decode(&rows); err != nil {
		return nil, fmt.Errorf("es cat indices decode: %w", err)
	}
	out := make([]adapters.TableInfo, 0, len(rows))
	for _, r := range rows {
		name, _ := r["index"].(string)
		if name == "" {
			continue
		}
		out = append(out, adapters.TableInfo{
			Schema: "_all",
			Name:   name,
			Kind:   "index",
		})
	}
	return out, nil
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

// Client exposes the underlying ES client to es_* tools through the Conn.
func (c *conn) Client() *es.Client { return c.client }

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func init() {
	adapters.Register(New())
}
