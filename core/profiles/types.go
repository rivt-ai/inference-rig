package profiles

import "inferencerig/platform/filedoc"

// DefaultLimitBytes caps a single profile.yaml at 2 MiB.
const DefaultLimitBytes int64 = 2 << 20

// BackendValidator checks and normalizes backend-specific fields (engine_args)
// and returns the effective profile. It is called after the shared common-field
// validation. This is the minimal shared interface the store depends on; the
// real backend registry is wired in a later phase.
type BackendValidator interface {
	ValidateProfile(p Profile) (Profile, error)
}

// BackendLookup resolves a registered backend key to its validator. The store
// uses it to reach the backend named by a profile's `backend` field; an unknown
// key must return an error, which the store surfaces to the caller. Injected so
// the store never hardcodes or imports a concrete engine.
type BackendLookup func(backend string) (BackendValidator, error)

// ProfileSummary is the compact listing entry for a profile.
type ProfileSummary struct {
	Name            string    `json:"name"`
	Dir             string    `json:"dir"`
	ProfileYAMLPath string    `json:"profile_yaml_path"`
	Backend         string    `json:"backend"`
	Model           ModelSpec `json:"model"`
	Host            string    `json:"host,omitempty"`
	Port            int       `json:"port"`
}

// ProfileDocument is the full result of reading or validating a profile. Parsed
// is the canonical Profile exactly as decoded; Effective is the profile after
// shared normalization and backend validation.
type ProfileDocument struct {
	Name            string  `json:"name"`
	Dir             string  `json:"dir"`
	ProfileYAMLPath string  `json:"profile_yaml_path"`
	ProfileYAML     string  `json:"profile_yaml"`
	Parsed          Profile `json:"parsed"`
	Effective       Profile `json:"effective"`
}

// CreateRequest is the input to Create/Validate.
type CreateRequest struct {
	Name        string `json:"name"`
	ProfileYAML string `json:"profile_yaml"`
}

// WriteResult is the outcome of an atomic profile write.
type WriteResult = filedoc.WriteResult

// DeleteResult reports the profile removed by Delete.
type DeleteResult struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

// ValidationResult wraps a validated ProfileDocument.
type ValidationResult struct {
	OK      bool            `json:"ok"`
	Profile ProfileDocument `json:"profile"`
}
