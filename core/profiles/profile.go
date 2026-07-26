// Package profiles is the canonical YAML profile schema and CRUD store.
//
// YAML is the only user-managed profile format, stored one directory per
// profile at ${INFERENCERIG_HOME}/profiles/<name>/profile.yaml. The schema owns
// the neutral common fields every backend relies on (version, name, backend,
// model, listen); the selected backend interprets and validates the free-form
// engine_args via a BackendValidator. The store mechanism is engine-agnostic —
// it never imports a backend package. Command/config rendering is intentionally
// out of scope here; it lands in a later phase.
package profiles

// Profile is the canonical, backend-neutral profile schema. The store owns and
// validates the common fields; the backend named by Backend interprets the
// free-form EngineArgs map.
type Profile struct {
	Version    any            `yaml:"version" json:"version"`
	Name       string         `yaml:"name" json:"name"`
	Backend    string         `yaml:"backend" json:"backend"`
	Model      ModelSpec      `yaml:"model" json:"model"`
	Listen     ListenSpec     `yaml:"listen" json:"listen"`
	EngineArgs map[string]any `yaml:"engine_args" json:"engine_args,omitempty"`
}

// ModelSpec identifies the model a profile serves.
type ModelSpec struct {
	Source    string `yaml:"source" json:"source"`
	Reference string `yaml:"reference" json:"reference,omitempty"`
}

// ListenSpec is the profile's listen address.
type ListenSpec struct {
	Host string `yaml:"host" json:"host,omitempty"`
	Port int    `yaml:"port" json:"port,omitempty"`
}
