package adapters

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

var (
	regMu      sync.RWMutex
	registered = map[Kind]Adapter{}
)

// Register adds an adapter to the global registry. Each adapter file calls
// this from its init() function.
func Register(a Adapter) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := registered[a.Kind()]; dup {
		panic(fmt.Sprintf("adapter already registered for kind %q", a.Kind()))
	}
	registered[a.Kind()] = a
}

// All returns every registered adapter, ordered by Kind for stable output.
func All() []Adapter {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Adapter, 0, len(registered))
	for _, a := range registered {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind() < out[j].Kind() })
	return out
}

// Get returns the adapter registered for a given Kind.
func Get(k Kind) (Adapter, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	a, ok := registered[k]
	return a, ok
}

// SourceRef pairs a Source with the Adapter that owns it.
type SourceRef struct {
	Adapter Adapter
	Source  Source
}

// DiscoverAll walks every registered adapter and collects its Sources.
// Sources are returned in (Kind, Name) order.
func DiscoverAll(env map[string]string) ([]SourceRef, error) {
	var out []SourceRef
	for _, a := range All() {
		srcs, err := a.Discover(env)
		if err != nil {
			return nil, fmt.Errorf("%s discover: %w", a.Kind(), err)
		}
		for _, s := range srcs {
			out = append(out, SourceRef{Adapter: a, Source: s})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source.Kind != out[j].Source.Kind {
			return out[i].Source.Kind < out[j].Source.Kind
		}
		return out[i].Source.Name < out[j].Source.Name
	})
	return out, nil
}

// CloseAll closes every adapter's pooled connections. Errors are joined.
func CloseAll(ctx context.Context) error {
	_ = ctx // reserved for future use (per-adapter close timeouts)
	var errs []error
	for _, a := range All() {
		if err := a.CloseAll(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", a.Kind(), err))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("close errors: %v", errs)
}
