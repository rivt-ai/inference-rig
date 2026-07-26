package backends

import (
	"fmt"

	"inferencerig/core/profiles"
)

// BackendLookup adapts the registry to the profile store's lookup seam. The
// returned function resolves a backend key to its validator, letting a
// profiles.FileStore drive every registered backend purely through the Backend
// interface (a Backend is a profiles.BackendValidator). An unregistered key
// returns an error, which the store surfaces as an invalid profile.
func (r *Registry) BackendLookup() profiles.BackendLookup {
	return func(name string) (profiles.BackendValidator, error) {
		b, ok := r.Lookup(name)
		if !ok {
			return nil, fmt.Errorf("backend %q is not registered", name)
		}
		return b, nil
	}
}
