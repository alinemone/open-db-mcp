package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	es "github.com/elastic/go-elasticsearch/v8"

	"github.com/open-db-mcp/open-db-mcp/internal/adapters"
	esad "github.com/open-db-mcp/open-db-mcp/internal/adapters/elasticsearch"
	"github.com/open-db-mcp/open-db-mcp/internal/clog"
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
		Description: "List concrete indices or data streams for a pattern (uses _cat/indices, or _resolve/index for read-only users).",
		InputSchema: schemaObj(map[string]any{
			"source":  map[string]any{"type": "string"},
			"pattern": map[string]any{"type": "string", "default": "*"},
			"limit":   map[string]any{"type": "number", "default": 250, "description": "Max rows (1-2000)"},
		}, "source"),
		Handler: d.esListIndices,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "es_resolve_index",
		Description: "Resolve an index, alias, data stream or pattern to the concrete names behind it (_resolve/index).",
		InputSchema: schemaObj(map[string]any{
			"source": map[string]any{"type": "string"},
			"name":   map[string]any{"type": "string", "description": "Index, alias, data stream or pattern (e.g. logs-*)"},
		}, "source", "name"),
		Handler: d.esResolveIndex,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "es_field_caps",
		Description: "Inspect available fields and their types for an index pattern (_field_caps).",
		InputSchema: schemaObj(map[string]any{
			"source":     map[string]any{"type": "string"},
			"index":      map[string]any{"type": "string"},
			"fields":     map[string]any{"type": "string", "default": "*"},
			"max_fields": map[string]any{"type": "number", "default": 250, "description": "Max fields returned (sorted by name)"},
		}, "source", "index"),
		Handler: d.esFieldCaps,
	})
	describeSchema := schemaObj(map[string]any{
		"source":      map[string]any{"type": "string"},
		"index":       map[string]any{"type": "string"},
		"fields":      map[string]any{"type": "string", "default": "*"},
		"max_fields":  map[string]any{"type": "number", "default": 300, "description": "10-3000"},
		"sample_size": map[string]any{"type": "number", "default": 5, "description": "0-50 sample documents"},
		"max_indices": map[string]any{"type": "number", "default": 250, "description": "1-2000"},
		"timeout_ms":  map[string]any{"type": "number", "default": 20000},
	}, "source", "index")
	s.RegisterTool(mcp.Tool{
		Name:        "es_describe_index",
		Description: "Describe an index or pattern: the concrete indices behind it, document count, fields with types, and a few sample documents (secrets masked).",
		InputSchema: describeSchema,
		Handler:     d.esDescribeIndex,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "es_analyze_index",
		Description: "es_describe_index plus analysis for writing queries: field-type histogram, notable date/keyword/text/numeric/boolean fields, and query guidance.",
		InputSchema: describeSchema,
		Handler:     d.esAnalyzeIndex,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "es_search",
		Description: "Run a raw Elasticsearch Query DSL search. Returns compact JSON: took_ms, total, hits (_index/_id/_score/_source) and aggregations.",
		InputSchema: schemaObj(map[string]any{
			"source":     map[string]any{"type": "string"},
			"index":      map[string]any{"type": "string"},
			"body":       map[string]any{"type": "object"},
			"max_hits":   map[string]any{"type": "number", "default": 100, "description": "1-500; caps body.size"},
			"timeout_ms": map[string]any{"type": "number", "default": 20000},
		}, "source", "index", "body"),
		Handler: d.esSearch,
	})
}

func (d *Deps) esClient(name string) (*es.Client, error) {
	sr, err := d.findSource(name, adapters.KindElasticsearch)
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
	limit := clampInt(numArg(args, "limit", 250), 1, 2000)
	cli, err := d.esClient(src)
	if err != nil {
		return "", err
	}
	res, err := cli.Cat.Indices(
		cli.Cat.Indices.WithContext(ctx),
		cli.Cat.Indices.WithIndex(pattern),
		cli.Cat.Indices.WithFormat("json"),
		cli.Cat.Indices.WithH("index,health,docs.count,store.size"),
		cli.Cat.Indices.WithS("index"),
	)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var out []map[string]any
	switch {
	case res.StatusCode == http.StatusForbidden:
		// The user may read indices but lacks the "monitor" privilege that
		// _cat/indices needs; list names (without health/size) instead.
		rows, err := esad.ResolveIndex(ctx, cli, pattern)
		if err != nil {
			return "", err
		}
		for _, r := range rows {
			out = append(out, map[string]any{"index": r["index"], "type": r["type"]})
		}
	case res.IsError():
		return "", esError(res.StatusCode, res.Body)
	default:
		body, _ := io.ReadAll(res.Body)
		_ = json.Unmarshal(body, &out)
	}
	return truncatedTOON("Indices", out, limit), nil
}

func (d *Deps) esResolveIndex(ctx context.Context, args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	name, _ := args["name"].(string)
	if name == "" {
		name, _ = args["pattern"].(string) // db-mcp accepted "pattern" too
	}
	if name == "" {
		return "", fmt.Errorf("invalid params: name is required")
	}
	cli, err := d.esClient(src)
	if err != nil {
		return "", err
	}
	rows, err := esad.ResolveIndex(ctx, cli, name)
	if err != nil {
		return "", err
	}
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		out[i] = map[string]any{"name": r["index"], "type": r["type"]}
	}
	return format.ToTOON("Resolved", out), nil
}

func (d *Deps) esFieldCaps(ctx context.Context, args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	index, _ := args["index"].(string)
	fields, _ := args["fields"].(string)
	if fields == "" {
		fields = "*"
	}
	maxFields := clampInt(numArg(args, "max_fields", 250), 1, 5000)
	cli, err := d.esClient(src)
	if err != nil {
		return "", err
	}
	caps, err := fetchFieldCaps(ctx, cli, index, strings.Split(fields, ","))
	if err != nil {
		return "", err
	}
	return truncatedTOON("FieldCaps", fieldRows(caps), maxFields), nil
}

func (d *Deps) esSearch(ctx context.Context, args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	index, _ := args["index"].(string)
	body, _ := args["body"].(map[string]any)
	if body == nil {
		body = map[string]any{}
	}
	maxHits := clampInt(numArg(args, "max_hits", 100), 1, 500)
	if size, ok := body["size"].(float64); !ok || int(size) > maxHits {
		body["size"] = maxHits
	}
	cli, err := d.esClient(src)
	if err != nil {
		return "", err
	}
	res, err := runSearch(ctx, cli, index, body, timeoutArg(args, 20000))
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(res)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// indexDescription is what es_describe_index reports; es_analyze_index
// renders it too and adds analysis on top.
type indexDescription struct {
	Resolved []map[string]string
	Fields   map[string]fieldCap
	Sample   esSearchResult
}

func (d *Deps) describeIndex(ctx context.Context, args map[string]any) (indexDescription, error) {
	var desc indexDescription
	src, _ := args["source"].(string)
	index, _ := args["index"].(string)
	if index == "" {
		return desc, fmt.Errorf("invalid params: index is required")
	}
	fields, _ := args["fields"].(string)
	if fields == "" {
		fields = "*"
	}
	cli, err := d.esClient(src)
	if err != nil {
		return desc, err
	}
	timeout := timeoutArg(args, 20000)
	sampleSize := clampInt(numArg(args, "sample_size", 5), 0, 50)

	if desc.Resolved, err = esad.ResolveIndex(ctx, cli, index); err != nil {
		return desc, err
	}
	if desc.Fields, err = fetchFieldCaps(ctx, cli, index, strings.Split(fields, ",")); err != nil {
		return desc, err
	}
	desc.Sample, err = runSearch(ctx, cli, index, map[string]any{
		"size":             sampleSize,
		"track_total_hits": true,
		"sort":             []any{"_doc"},
		"query":            map[string]any{"match_all": map[string]any{}},
	}, timeout)
	return desc, err
}

func (d *Deps) esDescribeIndex(ctx context.Context, args map[string]any) (string, error) {
	desc, err := d.describeIndex(ctx, args)
	if err != nil {
		return "", err
	}
	return strings.Join(renderDescription(desc, args), "\n\n"), nil
}

func (d *Deps) esAnalyzeIndex(ctx context.Context, args map[string]any) (string, error) {
	desc, err := d.describeIndex(ctx, args)
	if err != nil {
		return "", err
	}
	sections := renderDescription(desc, args)

	histogram := map[string]int{}
	notable := map[string][]string{}
	for _, name := range sortedFieldNames(desc.Fields) {
		for _, typ := range desc.Fields[name].Types {
			histogram[typ]++
			if kind := typeKind(typ); kind != "" && len(notable[kind]) < 50 {
				notable[kind] = append(notable[kind], name)
			}
		}
	}
	hist := make([]map[string]any, 0, len(histogram))
	for typ, n := range histogram {
		hist = append(hist, map[string]any{"type": typ, "fields": n})
	}
	sort.Slice(hist, func(a, b int) bool { return hist[a]["fields"].(int) > hist[b]["fields"].(int) })
	sections = append(sections, format.ToTOON("TypeHistogram", hist))

	var nf []map[string]any
	for _, kind := range []string{"date", "keyword", "text", "numeric", "boolean"} {
		if len(notable[kind]) > 0 {
			nf = append(nf, map[string]any{"kind": kind, "fields": strings.Join(notable[kind], " ")})
		}
	}
	if len(nf) > 0 {
		sections = append(sections, format.ToTOON("NotableFields", nf))
	}

	guide := []map[string]any{
		{"hint": "keyword fields: exact term filters and terms aggregations"},
		{"hint": "text fields: match / simple_query_string, not term (use field.keyword for exact)"},
	}
	if len(notable["date"]) > 0 {
		guide = append([]map[string]any{{"hint": "always add a range filter on " + notable["date"][0] + " (e.g. gte now-15m) — it lets ES skip whole shards"}}, guide...)
	}
	if len(desc.Resolved) > 1 {
		guide = append(guide, map[string]any{"hint": fmt.Sprintf("the pattern spans %d indices — narrow it when you can", len(desc.Resolved))})
	}
	sections = append(sections, format.ToTOON("QueryGuidance", guide))
	return strings.Join(sections, "\n\n"), nil
}

func renderDescription(desc indexDescription, args map[string]any) []string {
	maxFields := clampInt(numArg(args, "max_fields", 300), 10, 3000)
	maxIndices := clampInt(numArg(args, "max_indices", 250), 1, 2000)
	counts := map[string]int{}
	for _, r := range desc.Resolved {
		counts[r["type"]]++
	}
	index, _ := args["index"].(string)
	sections := []string{format.ToTOON("Summary", []map[string]any{{
		"index":          index,
		"indices":        counts["index"],
		"data_streams":   counts["data_stream"],
		"aliases":        counts["alias"],
		"fields":         len(desc.Fields),
		"total_docs":     desc.Sample.Total,
		"total_relation": desc.Sample.Relation,
		"took_ms":        desc.Sample.TookMS,
		"timed_out":      desc.Sample.TimedOut,
	}})}

	res := make([]map[string]any, len(desc.Resolved))
	for i, r := range desc.Resolved {
		res[i] = map[string]any{"name": r["index"], "type": r["type"]}
	}
	sections = append(sections, truncatedTOON("Indices", res, maxIndices))
	sections = append(sections, truncatedTOON("Fields", fieldRows(desc.Fields), maxFields))

	if len(desc.Sample.Hits) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "Sample[%d] (secrets masked):", len(desc.Sample.Hits))
		for _, h := range desc.Sample.Hits {
			fmt.Fprintf(&b, "\n%s/%s %s", h.Index, h.ID, clog.Redact(compactJSON(h.Source)))
		}
		sections = append(sections, b.String())
	}

	var warn []map[string]any
	if len(desc.Resolved) > 1 {
		warn = append(warn, map[string]any{"warning": fmt.Sprintf("pattern matches %d indices/streams; mappings may differ between them", len(desc.Resolved))})
	}
	if len(desc.Fields) > maxFields {
		warn = append(warn, map[string]any{"warning": fmt.Sprintf("field list truncated to %d of %d; pass fields= to narrow", maxFields, len(desc.Fields))})
	}
	for _, name := range sortedFieldNames(desc.Fields) {
		if len(desc.Fields[name].Types) > 1 {
			warn = append(warn, map[string]any{"warning": "mapping conflict on " + name + ": " + strings.Join(desc.Fields[name].Types, "/")})
		}
	}
	if len(warn) > 0 {
		sections = append(sections, format.ToTOON("Warnings", warn))
	}
	return sections
}

func fieldRows(caps map[string]fieldCap) []map[string]any {
	rows := make([]map[string]any, 0, len(caps))
	for _, name := range sortedFieldNames(caps) {
		fc := caps[name]
		rows = append(rows, map[string]any{
			"field": name, "type": strings.Join(fc.Types, "|"),
			"aggregatable": fc.Aggregatable, "searchable": fc.Searchable,
		})
	}
	return rows
}

func sortedFieldNames(caps map[string]fieldCap) []string {
	names := make([]string, 0, len(caps))
	for n := range caps {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// typeKind groups ES field types for es_analyze_index.
func typeKind(typ string) string {
	switch typ {
	case "date", "date_nanos":
		return "date"
	case "keyword", "constant_keyword", "wildcard":
		return "keyword"
	case "text", "match_only_text":
		return "text"
	case "boolean":
		return "boolean"
	}
	if isNumericType(typ) {
		return "numeric"
	}
	return ""
}

func isNumericType(typ string) bool {
	switch typ {
	case "long", "integer", "short", "byte", "double", "float", "half_float", "scaled_float", "unsigned_long":
		return true
	}
	return false
}

// truncatedTOON renders at most limit rows and says so when it cut.
func truncatedTOON(name string, rows []map[string]any, limit int) string {
	if len(rows) <= limit {
		return format.ToTOON(name, rows)
	}
	return format.ToTOON(name, rows[:limit]) +
		fmt.Sprintf("\n(truncated: showing %d of %d)", limit, len(rows))
}
