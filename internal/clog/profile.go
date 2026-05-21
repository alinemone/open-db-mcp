// Package clog resolves the field names used for company-log style indexes.
//
// CLOG is an opt-in profile on top of an Elasticsearch source: it tells the
// clog_* tools where to find namespace, container, status, latency, etc. on
// your access logs. Enable it by setting CLOG_ES_SOURCE in env.
package clog

import (
	"strings"

	"github.com/open-db-mcp/open-db-mcp/internal/config"
)

// Profile holds the canonical (resolved) field names for a CLOG-style ES index.
//
// Each field has a list of candidates because different Kubernetes log
// shippers use different conventions. The first candidate present in the
// index wins.
type Profile struct {
	ESSource   string
	IngressIdx string
	LogsPrefix string
	AllLogsIdx string
	TimeField  string
	MsgField   string
	Candidates Candidates
}

type Candidates struct {
	Namespace      []string
	Container      []string
	Service        []string
	IngressStatus  []string
	IngressLatency []string
	IngressHost    []string
	IngressPath    []string
}

// FromConfig builds a Profile from CLOGConfig, applying sane defaults that
// match common Kubernetes/vector deployments.
func FromConfig(c config.CLOGConfig) Profile {
	def := func(got []string, fallback ...string) []string {
		if len(got) > 0 {
			return got
		}
		return fallback
	}
	return Profile{
		ESSource:   c.ESSource,
		IngressIdx: c.IngressIndex,
		LogsPrefix: c.LogsPrefix,
		AllLogsIdx: c.AllLogsIndex,
		TimeField:  "@timestamp",
		MsgField:   "message",
		Candidates: Candidates{
			Namespace: def(c.NamespaceFlds,
				"clog_namespace", "data_stream.namespace", "kubernetes.pod_namespace", "kubernetes.namespace"),
			Container: def(c.ContainerFlds,
				"kubernetes.container_name", "kubernetes.container.name", "container.name"),
			Service: def(c.ServiceFlds,
				"kubernetes.pod_labels.app", "kubernetes.labels.app", "service.name"),
			IngressStatus: def(c.StatusFlds,
				"status", "http.status_code", "response.status", "message_json.status"),
			IngressLatency: def(c.LatencyFlds,
				"request_time", "upstream_response_time", "latency", "duration", "message_json.request_time"),
			IngressHost: def(c.HostFlds,
				"host", "server_name", "http.host", "message_json.host"),
			IngressPath: def(c.PathFlds,
				"path", "uri", "url.path", "request_uri", "message_json.request_uri", "message_json.path"),
		},
	}
}

// Enabled reports whether the CLOG profile should expose its tools.
func (p Profile) Enabled() bool { return p.ESSource != "" }

// KeywordVariants returns each field name plus its ".keyword" sub-field, so
// callers can request both when calling _field_caps.
func KeywordVariants(fields []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(fields)*2)
	add := func(s string) {
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		base := strings.TrimSuffix(f, ".keyword")
		add(base)
		add(base + ".keyword")
	}
	return out
}
