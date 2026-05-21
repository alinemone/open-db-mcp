package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	es "github.com/elastic/go-elasticsearch/v8"

	"github.com/open-db-mcp/open-db-mcp/internal/adapters"
	esad "github.com/open-db-mcp/open-db-mcp/internal/adapters/elasticsearch"
	"github.com/open-db-mcp/open-db-mcp/internal/format"
	"github.com/open-db-mcp/open-db-mcp/internal/mcp"
)

// RegisterES attaches es_* tools. Sources with kind=elasticsearch are
// addressed by name as usual.
func RegisterES(s *mcp.Server, d *Deps) {
	s.RegisterTool(mcp.Tool{
		Name:        "es_list_sources",
		Description: "List configured Elasticsearch sources.",
		InputSchema: schemaObj(map[string]any{}),
		Handler:     d.esListSources,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "es_list_indices",
		Description: "List concrete indices or data streams for a pattern (uses _cat/indices).",
		InputSchema: schemaObj(map[string]any{
			"source":  map[string]any{"type": "string"},
			"pattern": map[string]any{"type": "string", "default": "*"},
		}, "source"),
		Handler: d.esListIndices,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "es_field_caps",
		Description: "Inspect available fields and their types for an index pattern (_field_caps).",
		InputSchema: schemaObj(map[string]any{
			"source": map[string]any{"type": "string"},
			"index":  map[string]any{"type": "string"},
			"fields": map[string]any{"type": "string", "default": "*"},
		}, "source", "index"),
		Handler: d.esFieldCaps,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "es_search",
		Description: "Run a raw Elasticsearch Query DSL search. Returns raw JSON.",
		InputSchema: schemaObj(map[string]any{
			"source":   map[string]any{"type": "string"},
			"index":    map[string]any{"type": "string"},
			"body":     map[string]any{"type": "object"},
			"max_hits": map[string]any{"type": "number", "default": 100},
		}, "source", "index", "body"),
		Handler: d.esSearch,
	})
}

func (d *Deps) esClient(name string) (*es.Client, error) {
	sr, err := d.findSource(name)
	if err != nil {
		return nil, err
	}
	if sr.Source.Kind != adapters.KindElasticsearch {
		return nil, fmt.Errorf("source %s is not elasticsearch (kind=%s)", name, sr.Source.Kind)
	}
	a, ok := sr.Adapter.(*esad.Adapter)
	if !ok {
		return nil, fmt.Errorf("internal: %s is not registered as an elasticsearch adapter", name)
	}
	cli := a.Client(sr.Source.Name)
	if cli == nil {
		if _, err := sr.Adapter.Connect(context.Background(), sr.Source); err != nil {
			return nil, err
		}
		cli = a.Client(sr.Source.Name)
	}
	if cli == nil {
		return nil, fmt.Errorf("es client unavailable for %s", name)
	}
	return cli, nil
}

func (d *Deps) esListSources(_ context.Context, _ map[string]any) (string, error) {
	rows := []map[string]any{}
	for _, sr := range d.Sources {
		if sr.Source.Kind != adapters.KindElasticsearch {
			continue
		}
		rows = append(rows, map[string]any{
			"name": sr.Source.Name, "host": sr.Source.Cfg["host"], "url": sr.Source.Cfg["url"],
		})
	}
	return format.ToTOON("ESSources", rows), nil
}

func (d *Deps) esListIndices(ctx context.Context, args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		pattern = "*"
	}
	cli, err := d.esClient(src)
	if err != nil {
		return "", err
	}
	res, err := cli.Cat.Indices(
		cli.Cat.Indices.WithContext(ctx),
		cli.Cat.Indices.WithIndex(pattern),
		cli.Cat.Indices.WithFormat("json"),
		cli.Cat.Indices.WithH("index,health,docs.count,store.size"),
	)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.IsError() {
		return "", fmt.Errorf("es: %s", res.String())
	}
	body, _ := io.ReadAll(res.Body)
	var out []map[string]any
	_ = json.Unmarshal(body, &out)
	return format.ToTOON("Indices", out), nil
}

func (d *Deps) esFieldCaps(ctx context.Context, args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	index, _ := args["index"].(string)
	fields, _ := args["fields"].(string)
	if fields == "" {
		fields = "*"
	}
	cli, err := d.esClient(src)
	if err != nil {
		return "", err
	}
	res, err := cli.FieldCaps(
		cli.FieldCaps.WithContext(ctx),
		cli.FieldCaps.WithIndex(index),
		cli.FieldCaps.WithFields(strings.Split(fields, ",")...),
	)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.IsError() {
		return "", fmt.Errorf("es: %s", res.String())
	}
	body, _ := io.ReadAll(res.Body)
	// Flatten the {fields: {name: {type: {...}}}} structure into rows.
	var parsed struct {
		Fields map[string]map[string]map[string]any `json:"fields"`
	}
	_ = json.Unmarshal(body, &parsed)
	var rows []map[string]any
	for fName, types := range parsed.Fields {
		for tName, meta := range types {
			rows = append(rows, map[string]any{
				"field": fName, "type": tName,
				"aggregatable": meta["aggregatable"],
				"searchable":   meta["searchable"],
			})
		}
	}
	return format.ToTOON("FieldCaps", rows), nil
}

func (d *Deps) esSearch(ctx context.Context, args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	index, _ := args["index"].(string)
	body, _ := args["body"].(map[string]any)
	if body == nil {
		body = map[string]any{}
	}
	maxHits := 100
	if v, ok := args["max_hits"].(float64); ok {
		maxHits = int(v)
	}
	if maxHits < 1 {
		maxHits = 1
	}
	if maxHits > 500 {
		maxHits = 500
	}
	if _, ok := body["size"]; !ok {
		body["size"] = maxHits
	}
	cli, err := d.esClient(src)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	res, err := cli.Search(
		cli.Search.WithContext(ctx),
		cli.Search.WithIndex(index),
		cli.Search.WithBody(bytes.NewReader(raw)),
	)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	if res.IsError() {
		return "", fmt.Errorf("es: %s", string(out))
	}
	return string(out), nil
}
