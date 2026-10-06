package search

import (
	"regexp"
	"testing"
)

func testIndex() *Index {
	idx := New()
	idx.Replace([]Entry{
		{Source: "CORE", Schema: "public", Table: "orders"},
		{Source: "CORE", Schema: "public", Table: "orders", Column: "id"},
		{Source: "CORE", Schema: "public", Table: "orders", Column: "vendor_id"},
		{Source: "CORE", Schema: "public", Table: "orders", Column: "category_id"},
		{Source: "CORE", Schema: "public", Table: "order_items"},
		{Source: "CORE", Schema: "public", Table: "order_items", Column: "order_id"},
		{Source: "CORE", Schema: "public", Table: "vendors"},
		{Source: "CORE", Schema: "public", Table: "vendors", Column: "id"},
		{Source: "CORE", Schema: "public", Table: "categories"},
		{Source: "CORE", Schema: "public", Table: "categories", Column: "id"},
		{Source: "CORE", Schema: "public", Table: "booths"},
		{Source: "OTHER", Schema: "public", Table: "shipments", Column: "order_id"},
	})
	return idx
}

func TestInferRelationships(t *testing.T) {
	got := testIndex().InferRelationships("CORE", "public", "orders",
		[]string{"id", "vendor_id", "category_id"}, []string{"id"}, 50)
	want := map[string]float64{
		"inbound order_items.order_id": 0.8, // singular_id_match ("orders" → "order")
		"outbound vendors.id":          0.7,
		"outbound categories.id":       0.7, // "ies" plural
	}
	if len(got) != len(want) {
		t.Fatalf("got %d inferred, want %d: %+v", len(got), len(want), got)
	}
	for _, r := range got {
		key := r.Direction + " " + r.Table + "." + r.Column
		if conf, ok := want[key]; !ok || conf != r.Confidence {
			t.Errorf("unexpected %s (conf %.1f); %+v", key, r.Confidence, r)
		}
		if r.Table == "shipments" {
			t.Errorf("must not cross sources: %+v", r)
		}
	}
}

func TestSearchFeatures(t *testing.T) {
	idx := testIndex()
	idx.SetSynonyms(ParseSynonyms("vendor:seller|booth; customer:user|buyer"))

	if r := idx.Search("order items", "table", 5); len(r) == 0 || r[0].Table != "order_items" {
		t.Errorf("space should match underscore: %+v", r)
	}
	if r := idx.Search("venders", "table", 5); len(r) == 0 || r[0].Table != "vendors" {
		t.Errorf("one-typo match failed: %+v", r)
	}
	// Synonyms work from a variant too: "seller" → vendor, booth.
	found := map[string]bool{}
	for _, r := range idx.Search("seller", "table", 10) {
		found[r.Table] = true
	}
	if !found["vendors"] || !found["booths"] {
		t.Errorf("synonym expansion from variant failed: %v", found)
	}
	r := idx.SearchRegex(regexp.MustCompile(`(?i)^order_`), "table", 10)
	if len(r) != 1 || r[0].Table != "order_items" {
		t.Errorf("regex search: %+v", r)
	}
}

func TestWithinOneEdit(t *testing.T) {
	for _, c := range []struct {
		a, b string
		ok   bool
	}{
		{"vendors", "venders", true}, {"vendors", "vendor", true}, {"vendors", "xvendors", true},
		{"vendors", "vandars", false}, {"abc", "abc", true}, {"abc", "a", false},
	} {
		if got := withinOneEdit(c.a, c.b); got != c.ok {
			t.Errorf("withinOneEdit(%q,%q) = %v", c.a, c.b, got)
		}
	}
}
