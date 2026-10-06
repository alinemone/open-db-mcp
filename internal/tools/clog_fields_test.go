package tools

import "testing"

func TestFieldResolution(t *testing.T) {
	caps := map[string]fieldCap{
		"clog_namespace":            {Types: []string{"text"}},
		"clog_namespace.keyword":    {Types: []string{"keyword"}, Aggregatable: true},
		"kubernetes.pod_namespace":  {Types: []string{"keyword"}, Aggregatable: true},
		"kubernetes.container_name": {Types: []string{"keyword"}, Aggregatable: true},
		"message_json.status":       {Types: []string{"long"}, Aggregatable: true},
		"message_json.request_time": {Types: []string{"float"}, Aggregatable: true},
		"status":                    {Types: []string{"keyword"}, Aggregatable: true},
	}
	// A text field must resolve to its .keyword sibling, never itself.
	if got := termField(caps, []string{"clog_namespace", "kubernetes.pod_namespace"}); got != "clog_namespace.keyword" {
		t.Errorf("text field should resolve to .keyword, got %q", got)
	}
	if got := termField(caps, []string{"missing", "kubernetes.container_name"}); got != "kubernetes.container_name" {
		t.Errorf("keyword field should resolve to itself, got %q", got)
	}
	if got := termField(caps, []string{"missing"}); got != "" {
		t.Errorf("no candidate should resolve to empty, got %q", got)
	}
	// Numeric resolution skips a keyword "status" and finds the long one.
	if got := numericField(caps, []string{"status", "message_json.status"}); got != "message_json.status" {
		t.Errorf("numeric status = %q", got)
	}
	if got := numericField(caps, []string{"request_time", "message_json.request_time"}); got != "message_json.request_time" {
		t.Errorf("numeric latency = %q", got)
	}
}

func TestGetPath(t *testing.T) {
	doc := map[string]any{
		"@timestamp": "t",
		"kubernetes": map[string]any{
			"pod_name":   "p-1",
			"pod_labels": map[string]any{"titan/service": "svc"},
		},
		"flat.key": "v",
	}
	for path, want := range map[string]any{
		"@timestamp":                          "t",
		"kubernetes.pod_name":                 "p-1",
		"kubernetes.pod_labels.titan/service": "svc",
		"flat.key":                            "v",
		"kubernetes.missing":                  nil,
	} {
		if got := getPath(doc, path); got != want {
			t.Errorf("getPath(%q) = %v, want %v", path, got, want)
		}
	}
}
