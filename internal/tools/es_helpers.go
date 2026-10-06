package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	es "github.com/elastic/go-elasticsearch/v8"
)

// fieldCap is the merged _field_caps entry for one field across indices.
type fieldCap struct {
	Types        []string // sorted; more than one means a mapping conflict
	Aggregatable bool
	Searchable   bool
}

// fetchFieldCaps calls _field_caps and merges per-type entries. Metadata
// fields (leading "_") and bare object/nested containers are dropped.
func fetchFieldCaps(ctx context.Context, cli *es.Client, index string, fields []string) (map[string]fieldCap, error) {
	res, err := cli.FieldCaps(
		cli.FieldCaps.WithContext(ctx),
		cli.FieldCaps.WithIndex(index),
		cli.FieldCaps.WithFields(fields...),
		cli.FieldCaps.WithIgnoreUnavailable(true),
		cli.FieldCaps.WithAllowNoIndices(true),
	)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, esError(res.StatusCode, res.Body)
	}
	var parsed struct {
		Fields map[string]map[string]struct {
			Aggregatable bool `json:"aggregatable"`
			Searchable   bool `json:"searchable"`
		} `json:"fields"`
	}
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("es field_caps decode: %w", err)
	}
	out := make(map[string]fieldCap, len(parsed.Fields))
	for name, byType := range parsed.Fields {
		if strings.HasPrefix(name, "_") {
			continue
		}
		var fc fieldCap
		for typ, meta := range byType {
			if typ == "object" || typ == "nested" {
				continue
			}
			fc.Types = append(fc.Types, typ)
			fc.Aggregatable = fc.Aggregatable || meta.Aggregatable
			fc.Searchable = fc.Searchable || meta.Searchable
		}
		if len(fc.Types) == 0 {
			continue
		}
		sort.Strings(fc.Types)
		out[name] = fc
	}
	return out, nil
}

// esHit is one search hit, trimmed to what tools show.
type esHit struct {
	Index  string         `json:"_index"`
	ID     string         `json:"_id"`
	Score  *float64       `json:"_score,omitempty"`
	Source map[string]any `json:"_source,omitempty"`
}

// esSearchResult is a compact search response.
type esSearchResult struct {
	TookMS   int            `json:"took_ms"`
	TimedOut bool           `json:"timed_out"`
	Shards   int            `json:"shards"`
	Total    int64          `json:"total"`
	Relation string         `json:"total_relation,omitempty"`
	Hits     []esHit        `json:"hits"`
	Aggs     map[string]any `json:"aggregations,omitempty"`
}

// runSearch posts body to index/_search with a per-call timeout. Missing
// indices are ignored so a pattern that matches nothing returns zero hits
// instead of failing.
func runSearch(ctx context.Context, cli *es.Client, index string, body map[string]any, timeout time.Duration) (esSearchResult, error) {
	var out esSearchResult
	raw, err := json.Marshal(body)
	if err != nil {
		return out, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := cli.Search(
		cli.Search.WithContext(ctx),
		cli.Search.WithIndex(index),
		cli.Search.WithBody(bytes.NewReader(raw)),
		cli.Search.WithIgnoreUnavailable(true),
		cli.Search.WithAllowNoIndices(true),
	)
	if err != nil {
		if ctx.Err() != nil {
			return out, fmt.Errorf("invalid params: elasticsearch did not answer within %s — narrow the time range or raise timeout_ms", timeout)
		}
		return out, err
	}
	defer res.Body.Close()
	if res.IsError() {
		return out, esError(res.StatusCode, res.Body)
	}
	var parsed struct {
		Took     int  `json:"took"`
		TimedOut bool `json:"timed_out"`
		Shards   struct {
			Total int `json:"total"`
		} `json:"_shards"`
		Hits struct {
			Total struct {
				Value    int64  `json:"value"`
				Relation string `json:"relation"`
			} `json:"total"`
			Hits []esHit `json:"hits"`
		} `json:"hits"`
		Aggregations map[string]any `json:"aggregations"`
	}
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return out, fmt.Errorf("es search decode: %w", err)
	}
	return esSearchResult{
		TookMS: parsed.Took, TimedOut: parsed.TimedOut, Shards: parsed.Shards.Total,
		Total: parsed.Hits.Total.Value, Relation: parsed.Hits.Total.Relation,
		Hits: parsed.Hits.Hits, Aggs: parsed.Aggregations,
	}, nil
}

// esError turns an ES error response into a user-visible message. Query
// errors (bad DSL, unknown field) are the caller's to fix, so they surface
// behind the whitelisted "invalid params" prefix; the body is capped.
func esError(status int, body io.Reader) error {
	b, _ := io.ReadAll(io.LimitReader(body, 2000))
	var parsed struct {
		Error struct {
			Type   string `json:"type"`
			Reason string `json:"reason"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &parsed) == nil && parsed.Error.Reason != "" {
		return fmt.Errorf("invalid params: elasticsearch %d %s: %s", status, parsed.Error.Type, parsed.Error.Reason)
	}
	return fmt.Errorf("es: HTTP %d: %s", status, strings.TrimSpace(string(b)))
}

// timeoutArg reads timeout_ms, clamped to [1s, 5m].
func timeoutArg(args map[string]any, defMS int) time.Duration {
	return time.Duration(clampInt(numArg(args, "timeout_ms", defMS), 1000, 300000)) * time.Millisecond
}

// getPath reads a dotted path from an ES _source document, accepting both
// nested objects and flattened keys that contain dots
// ("kubernetes.pod_labels.titan/service" or {"kubernetes": {...}}).
func getPath(doc map[string]any, path string) any {
	if v, ok := doc[path]; ok {
		return v
	}
	for i := 0; i < len(path); i++ {
		if path[i] != '.' {
			continue
		}
		if sub, ok := doc[path[:i]].(map[string]any); ok {
			if v := getPath(sub, path[i+1:]); v != nil {
				return v
			}
		}
	}
	return nil
}

// compactJSON renders v on one line, for embedding documents in tool output.
func compactJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
