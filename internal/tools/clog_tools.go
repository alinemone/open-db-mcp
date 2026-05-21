package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/open-db-mcp/open-db-mcp/internal/clog"
	"github.com/open-db-mcp/open-db-mcp/internal/config"
	"github.com/open-db-mcp/open-db-mcp/internal/format"
	"github.com/open-db-mcp/open-db-mcp/internal/mcp"
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

	s.RegisterTool(mcp.Tool{
		Name:        "clog_profile",
		Description: "Describe the active CLOG profile: which indices and field candidates are used.",
		InputSchema: schemaObj(map[string]any{}),
		Handler:     d.clogProfileHandler,
	})
	s.RegisterTool(mcp.Tool{
		Name:        "clog_container_logs",
		Description: "Tail/search logs for a container in a namespace. Returns recent messages.",
		InputSchema: schemaObj(map[string]any{
			"namespace": map[string]any{"type": "string"},
			"container": map[string]any{"type": "string"},
			"query":     map[string]any{"type": "string", "description": "Optional simple_query_string filter"},
			"limit":     map[string]any{"type": "number", "default": 50},
		}, "namespace", "container"),
		Handler: d.clogContainerLogs,
	})
}

func (d *Deps) clogProfileHandler(_ context.Context, _ map[string]any) (string, error) {
	p := d.clogProfile
	rows := []map[string]any{
		{"key": "es_source", "value": p.ESSource},
		{"key": "ingress_index", "value": p.IngressIdx},
		{"key": "logs_prefix", "value": p.LogsPrefix},
		{"key": "all_logs_index", "value": p.AllLogsIdx},
		{"key": "time_field", "value": p.TimeField},
		{"key": "message_field", "value": p.MsgField},
		{"key": "namespace_candidates", "value": fmt.Sprint(p.Candidates.Namespace)},
		{"key": "container_candidates", "value": fmt.Sprint(p.Candidates.Container)},
		{"key": "service_candidates", "value": fmt.Sprint(p.Candidates.Service)},
		{"key": "ingress_status_candidates", "value": fmt.Sprint(p.Candidates.IngressStatus)},
		{"key": "ingress_latency_candidates", "value": fmt.Sprint(p.Candidates.IngressLatency)},
		{"key": "ingress_host_candidates", "value": fmt.Sprint(p.Candidates.IngressHost)},
		{"key": "ingress_path_candidates", "value": fmt.Sprint(p.Candidates.IngressPath)},
	}
	return format.ToTOON("CLOGProfile", rows), nil
}

func (d *Deps) clogContainerLogs(ctx context.Context, args map[string]any) (string, error) {
	ns, _ := args["namespace"].(string)
	container, _ := args["container"].(string)
	query, _ := args["query"].(string)
	limit := 50
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}
	if limit < 1 || limit > 500 {
		limit = 50
	}

	cli, err := d.esClient(d.clogProfile.ESSource)
	if err != nil {
		return "", err
	}

	// Build a small query: match namespace AND container, optional
	// simple_query_string. Try each namespace/container candidate via "should"
	// so we don't depend on a single field name.
	mustNs := orQuery("term", d.clogProfile.Candidates.Namespace, ns)
	mustContainer := orQuery("term", d.clogProfile.Candidates.Container, container)

	body := map[string]any{
		"size": limit,
		"sort": []any{map[string]any{d.clogProfile.TimeField: map[string]any{"order": "desc"}}},
		"query": map[string]any{
			"bool": map[string]any{
				"must": []any{mustNs, mustContainer},
			},
		},
	}
	if query != "" {
		b := body["query"].(map[string]any)["bool"].(map[string]any)
		b["must"] = append(b["must"].([]any), map[string]any{
			"simple_query_string": map[string]any{
				"query":  query,
				"fields": []string{d.clogProfile.MsgField},
			},
		})
	}

	raw, _ := json.Marshal(body)
	res, err := cli.Search(
		cli.Search.WithContext(ctx),
		cli.Search.WithIndex(d.clogProfile.AllLogsIdx),
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

// orQuery builds {"bool": {"should": [{op: {fieldA: value}}, {op: {fieldB: value}}], "minimum_should_match": 1}}
func orQuery(op string, fields []string, value string) map[string]any {
	if len(fields) == 0 {
		return map[string]any{"match_all": map[string]any{}}
	}
	should := make([]any, 0, len(fields))
	for _, f := range fields {
		should = append(should, map[string]any{op: map[string]any{f: value}})
	}
	return map[string]any{"bool": map[string]any{
		"should":               should,
		"minimum_should_match": 1,
	}}
}
