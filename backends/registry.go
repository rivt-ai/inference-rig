// Package backends holds the backend registry and, from Phase 5, the backend
// contracts (runtime control, profile validation/materialization, model
// resolution, fit, install). Engine-specific implementations live in
// subpackages (backends/llamacpp, backends/mlx) and register themselves here;
// the neutral core depends only on the interfaces in this package.
package backends

import (
	"fmt"
	"slices"
	"sync"
)

// Backend is the minimal identity a registered inference backend exposes.
// Phase 5 widens this into the full backend contract; the bootstrap registry
// is intentionally empty.
type Backend interface {
	Name() string
}

// Registry is a concurrency-safe set of backends keyed by name.
type Registry struct {
	mu sync.RWMutex
	m  map[string]Backend
}

// NewRegistry returns an empty backend registry.
func NewRegistry() *Registry {
	return &Registry{m: map[string]Backend{}}
}

// Register adds b under its Name, rejecting duplicates.
func (r *Registry) Register(b Backend) error {
	name := b.Name()
	if name == "" {
		return fmt.Errorf("backend has empty name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.m[name]; exists {
		return fmt.Errorf("backend %q already registered", name)
	}
	r.m[name] = b
	return nil
}

// Lookup returns the backend registered under name.
func (r *Registry) Lookup(name string) (Backend, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.m[name]
	return b, ok
}

// Names returns the registered backend names in sorted order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.m))
	for name := range r.m {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
