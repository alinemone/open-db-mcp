package tools

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	es "github.com/elastic/go-elasticsearch/v8"

	"github.com/open-db-mcp/open-db-mcp/internal/clog"
	"github.com/open-db-mcp/open-db-mcp/internal/config"
	"github.com/open-db-mcp/open-db-mcp/internal/format"
	"github.com/open-db-mcp/open-db-mcp/internal/mcp"
)

const (
	capsTTL        = 10 * time.Minute
	maxMessageLen  = 2000
	defaultFrom    = "now-15m"
	defaultTo      = "now"
	maxSeriesSteps = 200
)

// RegisterCLOG attaches the clog_* tools when a CLOG_ES_SOURCE is configured.
// If CLOG_ES_SOURCE is empty the tools are not registered, so they won't even
// appear in tools/list — keeping the menu clean for users who don't care.
func RegisterCLOG(s *mcp.Server, d *Deps, c config.CLOGConfig) {
	prof := clog.FromConfig(c)
	if !prof.Enabled() {
		return
	}
	d.clogProfile = prof

	source := map[string]any{"type": "string", "description": "ES source (default: CLOG_ES_SOURCE)"}
	from := map[string]any{"type": "string", "description": "Range start, ES date math or ISO (default now-15m)"}
	to := map[string]any{"type": "string", "description": "Range end (default now)"}
	timeout := map[string]any{"type": "number", "default": 20000}
	slowTimeout := map[string]any{"type": "number", "default": 60000}

	s.RegisterTool(mcp.Tool{
		Name:        "clog_profile",
		Description: "Describe the active CLOG profile: indices, field candidates, and the fields actually resolved on the live indices.",
		InputSchema: schemaObj(map[string]any{"source": source}),
		Handler:     d.clogProfileHandler,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "clog_list_namespaces",
		Description: "List Kubernetes namespaces that logged in the time range, by log volume.",
		InputSchema: schemaObj(map[string]any{
			"source": source, "from": from, "to": to, "timeout_ms": timeout,
			"limit": map[string]any{"type": "number", "default": 50, "description": "1-200"},
		}),
		Handler: d.clogListNamespaces,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "clog_list_containers",
		Description: "List containers that logged in the time range, optionally within one namespace, by log volume.",
		InputSchema: schemaObj(map[string]any{
			"source": source, "from": from, "to": to, "timeout_ms": timeout,
			"namespace": map[string]any{"type": "string"},
			"limit":     map[string]any{"type": "number", "default": 50, "description": "1-200"},
		}),
		Handler: d.clogListContainers,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "clog_find_targets",
		Description: "Given free text (an error message, service name, ...), find which namespaces, services and containers log it.",
		InputSchema: schemaObj(map[string]any{
			"source": source, "from": from, "to": to, "timeout_ms": timeout,
			"text":  map[string]any{"type": "string"},
			"limit": map[string]any{"type": "number", "default": 10, "description": "1-50 per group"},
		}, "text"),
		Handler: d.clogFindTargets,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "clog_container_logs",
		Description: "Tail/search logs of one container in a namespace, newest first. Secrets are masked and long messages truncated.",
		InputSchema: schemaObj(map[string]any{
			"source": source, "from": from, "to": to, "timeout_ms": timeout,
			"namespace": map[string]any{"type": "string"},
			"container": map[string]any{"type": "string"},
			"query":     map[string]any{"type": "string", "description": "Optional simple_query_string on the message (AND)"},
			"limit":     map[string]any{"type": "number", "default": 50, "description": "1-200"},
		}, "namespace", "container"),
		Handler: d.clogContainerLogs,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "clog_ingress_metrics",
		Description: "Ingress traffic health: request count, 2xx-5xx split, 5xx rate, latency percentiles and a time series.",
		InputSchema: schemaObj(map[string]any{
			"source": source, "from": from, "to": to, "timeout_ms": slowTimeout,
			"host":        map[string]any{"type": "string"},
			"path_prefix": map[string]any{"type": "string"},
			"buckets":     map[string]any{"type": "number", "default": 60, "description": "Auto time buckets (5-200)"},
			"interval":    map[string]any{"type": "string", "description": "Fixed interval like 5m; overrides buckets"},
			"percentiles": map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "description": "Default [50,95,99]"},
		}),
		Handler: d.clogIngressMetrics,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "clog_ingress_top_routes",
		Description: "Busiest ingress routes (query strings stripped) with request count, 5xx rate and latency percentiles.",
		InputSchema: schemaObj(map[string]any{
			"source": source, "from": from, "to": to, "timeout_ms": slowTimeout,
			"host":        map[string]any{"type": "string"},
			"path_prefix": map[string]any{"type": "string"},
			"limit":       map[string]any{"type": "number", "default": 20, "description": "1-50"},
			"percentiles": map[string]any{"type": "array", "items": map[string]any{"type": "number"}},
		}),
		Handler: d.clogIngressTopRoutes,
	})
}

// ---- field resolution ----

// capsCache memoizes _field_caps per (source, index, fields) for capsTTL.
type capsCache struct {
	mu sync.Mutex
	m  map[string]capsEntry
}

type capsEntry struct {
	at   time.Time
	caps map[string]fieldCap
}

func (c *capsCache) get(ctx context.Context, cli *es.Client, source, index string, fields []string) (map[string]fieldCap, error) {
	key := source + "|" + index + "|" + strings.Join(fields, ",")
	c.mu.Lock()
	if e, ok := c.m[key]; ok && time.Since(e.at) < capsTTL {
		c.mu.Unlock()
		return e.caps, nil
	}
	c.mu.Unlock()

	caps, err := fetchFieldCaps(ctx, cli, index, fields)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.m == nil {
		c.m = map[string]capsEntry{}
	}
	c.m[key] = capsEntry{at: time.Now(), caps: caps}
	c.mu.Unlock()
	return caps, nil
}

// clogFields are the concrete field names resolved on one index.
type clogFields struct {
	Namespace, Container, Service, Pod string // term-filterable (keyword)
	Status, Latency, Host, Path        string
	StatusNumeric                      bool
}

// resolve picks, for every role, the first candidate present on index:
// keyword roles prefer an aggregatable keyword (falling back to .keyword
// when the base field is text), numeric roles need a numeric type.
func (d *Deps) resolve(ctx context.Context, cli *es.Client, source, index string) (clogFields, error) {
	c := d.clogProfile.Candidates
	var all []string
	for _, group := range [][]string{c.Namespace, c.Container, c.Service, c.Pod,
		c.IngressStatus, c.IngressLatency, c.IngressHost, c.IngressPath} {
		all = append(all, group...)
	}
	caps, err := d.clogCaps.get(ctx, cli, source, index, clog.KeywordVariants(all))
	if err != nil {
		return clogFields{}, err
	}
	f := clogFields{
		Namespace: termField(caps, c.Namespace),
		Container: termField(caps, c.Container),
		Service:   termField(caps, c.Service),
		Pod:       termField(caps, c.Pod),
		Latency:   numericField(caps, c.IngressLatency),
		Host:      termField(caps, c.IngressHost),
		Path:      termField(caps, c.IngressPath),
	}
	if f.Status = numericField(caps, c.IngressStatus); f.Status != "" {
		f.StatusNumeric = true
	} else {
		f.Status = termField(caps, c.IngressStatus)
	}
	return f, nil
}

func termField(caps map[string]fieldCap, candidates []string) string {
	for _, cand := range candidates {
		base := strings.TrimSuffix(strings.TrimSpace(cand), ".keyword")
		if fc, ok := caps[base]; ok && fc.Aggregatable && !hasType(fc, "text") {
			return base
		}
		if fc, ok := caps[base+".keyword"]; ok && fc.Aggregatable {
			return base + ".keyword"
		}
	}
	return ""
}

func numericField(caps map[string]fieldCap, candidates []string) string {
	for _, cand := range candidates {
		if fc, ok := caps[strings.TrimSpace(cand)]; ok {
			for _, t := range fc.Types {
				if isNumericType(t) {
					return cand
				}
			}
		}
	}
	return ""
}

func hasType(fc fieldCap, typ string) bool {
	for _, t := range fc.Types {
		if t == typ {
			return true
		}
	}
	return false
}

// ---- shared helpers ----

func (d *Deps) clogClient(args map[string]any) (*es.Client, string, error) {
	src, _ := args["source"].(string)
	if src == "" {
		src = d.clogProfile.ESSource
	}
	cli, err := d.esClient(src)
	return cli, src, err
}

func (d *Deps) timeRange(args map[string]any) (from, to string, filter map[string]any) {
	from, _ = args["from"].(string)
	to, _ = args["to"].(string)
	if from == "" {
		from = defaultFrom
	}
	if to == "" {
		to = defaultTo
	}
	return from, to, map[string]any{"range": map[string]any{
		d.clogProfile.TimeField: map[string]any{"gte": from, "lte": to},
	}}
}

// namespaceIndex is the per-namespace data stream (<prefix><namespace>).
func (d *Deps) namespaceIndex(ns string) string {
	if strings.HasPrefix(ns, d.clogProfile.LogsPrefix) {
		return ns
	}
	return d.clogProfile.LogsPrefix + ns
}

// searchNamespace searches the namespace's own data stream (cheap) and, if
// it does not exist, every log index with the namespace filter.
func (d *Deps) searchNamespace(ctx context.Context, cli *es.Client, ns string, body map[string]any, timeout time.Duration) (esSearchResult, string, error) {
	idx := d.namespaceIndex(ns)
	res, err := runSearch(ctx, cli, idx, body, timeout)
	if err == nil && res.Shards == 0 {
		idx = d.clogProfile.AllLogsIdx
		res, err = runSearch(ctx, cli, idx, body, timeout)
	}
	return res, idx, err
}

func term(field, value string) map[string]any {
	return map[string]any{"term": map[string]any{field: value}}
}

func boolQuery(filters []any) map[string]any {
	return map[string]any{"bool": map[string]any{"filter": filters}}
}

func requireField(field, role string, candidates []string, index string) error {
	if field == "" {
		return fmt.Errorf("invalid params: no usable %s field on %s (candidates: %s); set the matching CLOG_*_FIELDS env var",
			role, index, strings.Join(candidates, ", "))
	}
	return nil
}

// termsRows turns a terms aggregation into {key, docs} rows.
func termsRows(aggs map[string]any, name, keyName string) []map[string]any {
	var rows []map[string]any
	for _, b := range aggBuckets(aggs, name) {
		rows = append(rows, map[string]any{keyName: b["key"], "docs": toInt(b["doc_count"])})
	}
	return rows
}

func aggBuckets(aggs map[string]any, name string) []map[string]any {
	agg, _ := aggs[name].(map[string]any)
	raw, _ := agg["buckets"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, b := range raw {
		if m, ok := b.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func toInt(v any) int64 {
	if f, ok := v.(float64); ok {
		return int64(f)
	}
	return 0
}

// ---- tools ----

func (d *Deps) clogProfileHandler(ctx context.Context, args map[string]any) (string, error) {
	p := d.clogProfile
	rows := []map[string]any{
		{"key": "es_source", "value": p.ESSource},
		{"key": "ingress_index", "value": p.IngressIdx},
		{"key": "logs_prefix", "value": p.LogsPrefix},
		{"key": "all_logs_index", "value": p.AllLogsIdx},
		{"key": "time_field", "value": p.TimeField},
		{"key": "message_field", "value": p.MsgField},
		{"key": "namespace_candidates", "value": strings.Join(p.Candidates.Namespace, " ")},
		{"key": "container_candidates", "value": strings.Join(p.Candidates.Container, " ")},
		{"key": "service_candidates", "value": strings.Join(p.Candidates.Service, " ")},
		{"key": "pod_candidates", "value": strings.Join(p.Candidates.Pod, " ")},
		{"key": "ingress_status_candidates", "value": strings.Join(p.Candidates.IngressStatus, " ")},
		{"key": "ingress_latency_candidates", "value": strings.Join(p.Candidates.IngressLatency, " ")},
		{"key": "ingress_host_candidates", "value": strings.Join(p.Candidates.IngressHost, " ")},
		{"key": "ingress_path_candidates", "value": strings.Join(p.Candidates.IngressPath, " ")},
	}
	out := format.ToTOON("CLOGProfile", rows)

	cli, src, err := d.clogClient(args)
	if err != nil {
		return out + "\n\nResolved: unavailable (" + err.Error() + ")", nil
	}
	logs, err1 := d.resolve(ctx, cli, src, p.AllLogsIdx)
	ing, err2 := d.resolve(ctx, cli, src, p.IngressIdx)
	if err1 != nil || err2 != nil {
		return out + "\n\nResolved: unavailable (field_caps failed)", nil
	}
	none := func(s string) string {
		if s == "" {
			return "(none)"
		}
		return s
	}
	resolved := []map[string]any{
		{"role": "namespace", "index": p.AllLogsIdx, "field": none(logs.Namespace)},
		{"role": "container", "index": p.AllLogsIdx, "field": none(logs.Container)},
		{"role": "service", "index": p.AllLogsIdx, "field": none(logs.Service)},
		{"role": "pod", "index": p.AllLogsIdx, "field": none(logs.Pod)},
		{"role": "status", "index": p.IngressIdx, "field": none(ing.Status)},
		{"role": "latency", "index": p.IngressIdx, "field": none(ing.Latency)},
		{"role": "host", "index": p.IngressIdx, "field": none(ing.Host)},
		{"role": "path", "index": p.IngressIdx, "field": none(ing.Path)},
	}
	return out + "\n\n" + format.ToTOON("Resolved", resolved), nil
}

func (d *Deps) clogListNamespaces(ctx context.Context, args map[string]any) (string, error) {
	cli, src, err := d.clogClient(args)
	if err != nil {
		return "", err
	}
	idx := d.clogProfile.AllLogsIdx
	f, err := d.resolve(ctx, cli, src, idx)
	if err != nil {
		return "", err
	}
	if err := requireField(f.Namespace, "namespace", d.clogProfile.Candidates.Namespace, idx); err != nil {
		return "", err
	}
	_, _, tf := d.timeRange(args)
	limit := clampInt(numArg(args, "limit", 50), 1, 200)
	res, err := runSearch(ctx, cli, idx, map[string]any{
		"size":  0,
		"query": boolQuery([]any{tf}),
		"aggs":  map[string]any{"ns": map[string]any{"terms": map[string]any{"field": f.Namespace, "size": limit}}},
	}, timeoutArg(args, 20000))
	if err != nil {
		return "", err
	}
	return format.ToTOON("Namespaces", termsRows(res.Aggs, "ns", "namespace")), nil
}

func (d *Deps) clogListContainers(ctx context.Context, args map[string]any) (string, error) {
	cli, src, err := d.clogClient(args)
	if err != nil {
		return "", err
	}
	f, err := d.resolve(ctx, cli, src, d.clogProfile.AllLogsIdx)
	if err != nil {
		return "", err
	}
	if err := requireField(f.Container, "container", d.clogProfile.Candidates.Container, d.clogProfile.AllLogsIdx); err != nil {
		return "", err
	}
	_, _, tf := d.timeRange(args)
	limit := clampInt(numArg(args, "limit", 50), 1, 200)
	filters := []any{tf}
	ns, _ := args["namespace"].(string)
	if ns != "" && f.Namespace != "" {
		filters = append(filters, term(f.Namespace, ns))
	}
	aggs := map[string]any{"c": map[string]any{"terms": map[string]any{"field": f.Container, "size": limit}}}
	if f.Service != "" {
		aggs["c"].(map[string]any)["aggs"] = map[string]any{
			"svc": map[string]any{"terms": map[string]any{"field": f.Service, "size": 1}},
		}
	}
	body := map[string]any{"size": 0, "query": boolQuery(filters), "aggs": aggs}
	timeout := timeoutArg(args, 20000)

	var res esSearchResult
	if ns != "" {
		res, _, err = d.searchNamespace(ctx, cli, ns, body, timeout)
	} else {
		res, err = runSearch(ctx, cli, d.clogProfile.AllLogsIdx, body, timeout)
	}
	if err != nil {
		return "", err
	}
	var rows []map[string]any
	for _, b := range aggBuckets(res.Aggs, "c") {
		row := map[string]any{"container": b["key"], "docs": toInt(b["doc_count"])}
		if svc := aggBuckets(b, "svc"); len(svc) > 0 {
			row["service"] = svc[0]["key"]
		}
		rows = append(rows, row)
	}
	return format.ToTOON("Containers", rows), nil
}

func (d *Deps) clogFindTargets(ctx context.Context, args map[string]any) (string, error) {
	text, _ := args["text"].(string)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("invalid params: text is required")
	}
	cli, src, err := d.clogClient(args)
	if err != nil {
		return "", err
	}
	idx := d.clogProfile.AllLogsIdx
	f, err := d.resolve(ctx, cli, src, idx)
	if err != nil {
		return "", err
	}
	_, _, tf := d.timeRange(args)
	limit := clampInt(numArg(args, "limit", 10), 1, 50)

	fields := []string{d.clogProfile.MsgField}
	aggs := map[string]any{}
	for name, field := range map[string]string{"namespaces": f.Namespace, "services": f.Service, "containers": f.Container} {
		if field == "" {
			continue
		}
		fields = append(fields, field)
		aggs[name] = map[string]any{"terms": map[string]any{"field": field, "size": limit}}
	}
	res, err := runSearch(ctx, cli, idx, map[string]any{
		"size":             0,
		"track_total_hits": true,
		"query": map[string]any{"bool": map[string]any{
			"filter": []any{tf},
			"must": []any{map[string]any{"simple_query_string": map[string]any{
				"query": text, "fields": fields, "default_operator": "and",
			}}},
		}},
		"aggs": aggs,
	}, timeoutArg(args, 20000))
	if err != nil {
		return "", err
	}
	sections := []string{fmt.Sprintf("Matched: %d documents", res.Total)}
	for _, g := range []struct{ agg, title, key string }{
		{"namespaces", "Namespaces", "namespace"},
		{"services", "Services", "service"},
		{"containers", "Containers", "container"},
	} {
		if _, ok := aggs[g.agg]; ok {
			sections = append(sections, format.ToTOON(g.title, termsRows(res.Aggs, g.agg, g.key)))
		}
	}
	return strings.Join(sections, "\n\n"), nil
}

func (d *Deps) clogContainerLogs(ctx context.Context, args map[string]any) (string, error) {
	ns, _ := args["namespace"].(string)
	container, _ := args["container"].(string)
	if ns == "" || container == "" {
		return "", fmt.Errorf("invalid params: namespace and container are required")
	}
	cli, src, err := d.clogClient(args)
	if err != nil {
		return "", err
	}
	p := d.clogProfile
	f, err := d.resolve(ctx, cli, src, p.AllLogsIdx)
	if err != nil {
		return "", err
	}
	if err := requireField(f.Container, "container", p.Candidates.Container, p.AllLogsIdx); err != nil {
		return "", err
	}
	_, _, tf := d.timeRange(args)
	limit := clampInt(numArg(args, "limit", 50), 1, 200)

	filters := []any{tf, term(f.Container, container)}
	if f.Namespace != "" {
		filters = append(filters, term(f.Namespace, ns))
	}
	query := map[string]any{"bool": map[string]any{"filter": filters}}
	if q, _ := args["query"].(string); q != "" {
		query["bool"].(map[string]any)["must"] = []any{map[string]any{"simple_query_string": map[string]any{
			"query": q, "fields": []string{p.MsgField}, "default_operator": "and",
		}}}
	}
	include := []string{p.TimeField, p.MsgField, "stream"}
	include = append(include, p.Candidates.Pod...)
	include = append(include, p.Candidates.Service...)

	res, _, err := d.searchNamespace(ctx, cli, ns, map[string]any{
		"size":             limit,
		"track_total_hits": true,
		"sort":             []any{map[string]any{p.TimeField: map[string]any{"order": "desc"}}},
		"query":            query,
		"_source":          map[string]any{"includes": include},
	}, timeoutArg(args, 20000))
	if err != nil {
		return "", err
	}

	rows := make([]map[string]any, 0, len(res.Hits))
	for _, h := range res.Hits {
		msg, _ := getPath(h.Source, p.MsgField).(string)
		row := map[string]any{
			"ts":      getPath(h.Source, p.TimeField),
			"pod":     firstPath(h.Source, p.Candidates.Pod),
			"stream":  getPath(h.Source, "stream"),
			"message": clog.Truncate(clog.Redact(strings.TrimSpace(msg)), maxMessageLen),
		}
		rows = append(rows, row)
	}
	header := fmt.Sprintf("Matched: %d (showing newest %d; secrets masked, messages capped at %d chars)",
		res.Total, len(rows), maxMessageLen)
	if svc := firstHitPath(res.Hits, p.Candidates.Service); svc != nil {
		header += fmt.Sprintf("\nService: %v", svc)
	}
	return header + "\n\n" + format.ToTOON("Logs", rows), nil
}

func firstPath(doc map[string]any, paths []string) any {
	for _, p := range paths {
		if v := getPath(doc, p); v != nil {
			return v
		}
	}
	return nil
}

func firstHitPath(hits []esHit, paths []string) any {
	for _, h := range hits {
		if v := firstPath(h.Source, paths); v != nil {
			return v
		}
	}
	return nil
}

// ---- ingress ----

// ingressFilters builds the time/host/path filters for ingress queries.
// Host and path use their resolved keyword fields; without one they fall
// back to a phrase search on the message.
func (d *Deps) ingressFilters(args map[string]any, f clogFields) []any {
	_, _, tf := d.timeRange(args)
	filters := []any{tf}
	if host, _ := args["host"].(string); host != "" {
		if f.Host != "" {
			filters = append(filters, term(f.Host, host))
		} else {
			filters = append(filters, phrase(d.clogProfile.MsgField, host))
		}
	}
	if prefix, _ := args["path_prefix"].(string); prefix != "" {
		if f.Path != "" {
			filters = append(filters, map[string]any{"prefix": map[string]any{f.Path: prefix}})
		} else {
			filters = append(filters, phrase(d.clogProfile.MsgField, prefix))
		}
	}
	return filters
}

func phrase(field, text string) map[string]any {
	return map[string]any{"match_phrase": map[string]any{field: text}}
}

// statusClass matches one HTTP status class (2..5) on the status field.
func statusClass(f clogFields, class int) map[string]any {
	if f.StatusNumeric {
		return map[string]any{"range": map[string]any{f.Status: map[string]any{"gte": class * 100, "lt": class*100 + 100}}}
	}
	return map[string]any{"prefix": map[string]any{f.Status: fmt.Sprint(class)}}
}

func percentilesArg(args map[string]any) []float64 {
	raw, _ := args["percentiles"].([]any)
	var out []float64
	for _, v := range raw {
		if p, ok := v.(float64); ok && p > 0 && p < 100 {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		out = []float64{50, 95, 99}
	}
	return out
}

// percentileValues reads a percentiles aggregation into "p95" → value.
func percentileValues(aggs map[string]any, name string) map[string]any {
	out := map[string]any{}
	agg, _ := aggs[name].(map[string]any)
	values, _ := agg["values"].(map[string]any)
	for k, v := range values {
		var p float64
		if _, err := fmt.Sscanf(k, "%g", &p); err != nil || v == nil {
			continue
		}
		if f, ok := v.(float64); ok {
			v = math.Round(f*10000) / 10000 // latencies: 4 decimals is plenty
		}
		out[fmt.Sprintf("p%g", p)] = v
	}
	return out
}

func filterCount(aggs map[string]any, name string) int64 {
	agg, _ := aggs[name].(map[string]any)
	return toInt(agg["doc_count"])
}

func rate(part, total int64) string {
	if total == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.2f%%", 100*float64(part)/float64(total))
}

func (d *Deps) ingressSetup(ctx context.Context, args map[string]any) (*es.Client, clogFields, error) {
	cli, src, err := d.clogClient(args)
	if err != nil {
		return nil, clogFields{}, err
	}
	f, err := d.resolve(ctx, cli, src, d.clogProfile.IngressIdx)
	return cli, f, err
}

func (d *Deps) clogIngressMetrics(ctx context.Context, args map[string]any) (string, error) {
	cli, f, err := d.ingressSetup(ctx, args)
	if err != nil {
		return "", err
	}
	p := d.clogProfile
	if err := requireField(f.Status, "status", p.Candidates.IngressStatus, p.IngressIdx); err != nil {
		return "", err
	}
	pcts := percentilesArg(args)

	aggs := map[string]any{}
	for c := 2; c <= 5; c++ {
		aggs[fmt.Sprintf("s%dxx", c)] = map[string]any{"filter": statusClass(f, c)}
	}
	seriesSub := map[string]any{"e5xx": map[string]any{"filter": statusClass(f, 5)}}
	if f.Latency != "" {
		aggs["lat"] = map[string]any{"percentiles": map[string]any{"field": f.Latency, "percents": pcts}}
		seriesSub["lat"] = map[string]any{"percentiles": map[string]any{"field": f.Latency, "percents": []float64{95}}}
	}
	if interval, _ := args["interval"].(string); interval != "" {
		aggs["series"] = map[string]any{
			"date_histogram": map[string]any{"field": p.TimeField, "fixed_interval": interval, "min_doc_count": 0},
			"aggs":           seriesSub,
		}
	} else {
		aggs["series"] = map[string]any{
			"auto_date_histogram": map[string]any{"field": p.TimeField, "buckets": clampInt(numArg(args, "buckets", 60), 5, 200)},
			"aggs":                seriesSub,
		}
	}
	res, err := runSearch(ctx, cli, p.IngressIdx, map[string]any{
		"size":             0,
		"track_total_hits": true,
		"query":            boolQuery(d.ingressFilters(args, f)),
		"aggs":             aggs,
	}, timeoutArg(args, 60000))
	if err != nil {
		return "", err
	}

	from, to, _ := d.timeRange(args)
	summary := map[string]any{
		"from": from, "to": to, "requests": res.Total,
		"2xx": filterCount(res.Aggs, "s2xx"), "3xx": filterCount(res.Aggs, "s3xx"),
		"4xx": filterCount(res.Aggs, "s4xx"), "5xx": filterCount(res.Aggs, "s5xx"),
		"5xx_rate": rate(filterCount(res.Aggs, "s5xx"), res.Total),
	}
	for k, v := range percentileValues(res.Aggs, "lat") {
		summary["latency_"+k] = v
	}

	var series []map[string]any
	for i, b := range aggBuckets(res.Aggs, "series") {
		if i >= maxSeriesSteps {
			break
		}
		row := map[string]any{
			"time":     b["key_as_string"],
			"requests": toInt(b["doc_count"]),
			"5xx":      filterCount(b, "e5xx"),
		}
		if v, ok := percentileValues(b, "lat")["p95"]; ok {
			row["latency_p95"] = v
		}
		series = append(series, row)
	}

	fields := fmt.Sprintf("Fields: status=%s latency=%s host=%s path=%s (index %s)",
		f.Status, orNone(f.Latency), orNone(f.Host), orNone(f.Path), p.IngressIdx)
	out := []string{format.ToTOON("Summary", []map[string]any{summary}), format.ToTOON("Series", series), fields}
	if f.Latency == "" {
		out = append(out, "Note: no numeric latency field found — latency percentiles skipped")
	}
	return strings.Join(out, "\n\n"), nil
}

func (d *Deps) clogIngressTopRoutes(ctx context.Context, args map[string]any) (string, error) {
	cli, f, err := d.ingressSetup(ctx, args)
	if err != nil {
		return "", err
	}
	p := d.clogProfile
	if err := requireField(f.Path, "path", p.Candidates.IngressPath, p.IngressIdx); err != nil {
		return "", err
	}
	limit := clampInt(numArg(args, "limit", 20), 1, 50)
	pcts := percentilesArg(args)

	sub := map[string]any{}
	if f.Status != "" {
		sub["e5xx"] = map[string]any{"filter": statusClass(f, 5)}
	}
	if f.Latency != "" {
		sub["lat"] = map[string]any{"percentiles": map[string]any{"field": f.Latency, "percents": pcts}}
	}
	// Raw paths carry query strings, so several raw keys collapse into one
	// route after normalization; over-fetch, then merge.
	res, err := runSearch(ctx, cli, p.IngressIdx, map[string]any{
		"size":  0,
		"query": boolQuery(d.ingressFilters(args, f)),
		"aggs": map[string]any{"routes": map[string]any{
			"terms": map[string]any{"field": f.Path, "size": min(limit*5, 250)},
			"aggs":  sub,
		}},
	}, timeoutArg(args, 60000))
	if err != nil {
		return "", err
	}

	type route struct {
		requests, e5xx, best int64
		lat                  map[string]any
	}
	merged := map[string]*route{}
	for _, b := range aggBuckets(res.Aggs, "routes") {
		key := clog.RouteKey(fmt.Sprint(b["key"]))
		r := merged[key]
		if r == nil {
			r = &route{}
			merged[key] = r
		}
		n := toInt(b["doc_count"])
		r.requests += n
		r.e5xx += filterCount(b, "e5xx")
		if n > r.best { // percentiles cannot be merged; keep the largest bucket's
			r.best, r.lat = n, percentileValues(b, "lat")
		}
	}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool { return merged[keys[a]].requests > merged[keys[b]].requests })
	if len(keys) > limit {
		keys = keys[:limit]
	}
	rows := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		r := merged[k]
		row := map[string]any{"route": k, "requests": r.requests, "5xx": r.e5xx, "5xx_rate": rate(r.e5xx, r.requests)}
		for pk, v := range r.lat {
			row["latency_"+pk] = v
		}
		rows = append(rows, row)
	}
	note := fmt.Sprintf("Fields: path=%s status=%s latency=%s (index %s). Routes are normalized (query string dropped); latency is from the busiest raw variant.",
		f.Path, orNone(f.Status), orNone(f.Latency), p.IngressIdx)
	return format.ToTOON("Routes", rows) + "\n\n" + note, nil
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
