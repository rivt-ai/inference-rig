package backends

import (
	"context"

	"inferencerig/core/profiles"
	"inferencerig/core/runtime"
)

// Backend is the full contract every inference backend implements. It composes
// identity with the backend facets shared control logic drives entirely through
// this interface: profile validation, config/command materialization, launch
// specification, model resolution, artifact planning, fit estimation, engine
// install, and capability discovery. The neutral core never branches on a
// concrete engine — it calls these methods.
//
// A Backend also satisfies profiles.BackendValidator (via ValidateProfile), so
// a Registry can serve as the profile store's BackendLookup (see BackendLookup).
type Backend interface {
	// Name is the stable registry key for this backend. It must be non-empty.
	Name() string

	// ValidateProfile checks and normalizes the backend-specific fields of an
	// already common-validated profile and returns the effective profile. It
	// satisfies profiles.BackendValidator.
	ValidateProfile(p profiles.Profile) (profiles.Profile, error)

	// Materialize renders the effective profile into the backend's runtime form
	// as a neutral Materialization (rendered files and/or a summary). No engine
	// format is exposed by the return type.
	Materialize(p profiles.Profile) (Materialization, error)

	// LaunchSpec produces the Phase-3 supervisor launch spec for a validated
	// profile and its materialization, tying the backend to the generic
	// supervisor. A render failure may be deferred via LaunchSpec.BuildErr.
	LaunchSpec(p profiles.Profile, m Materialization) (runtime.LaunchSpec, error)

	// Resolve maps the profile's model source/reference to concrete artifact
	// references.
	Resolve(ctx context.Context, p profiles.Profile) (ResolvedModel, error)

	// Plan turns a resolved model into a neutral artifact download plan covering
	// single-file and multi-file artifacts alike.
	Plan(r ResolvedModel) (ArtifactPlan, error)

	// Fit estimates whether the model fits the given host resources; it works
	// for both discrete-VRAM and unified-memory hosts.
	Fit(p profiles.Profile, host HostResources) (FitEstimate, error)

	// Install installs or upgrades the managed engine and reports what happened.
	Install(ctx context.Context, opts InstallOptions) (InstallResult, error)

	// Capabilities advertises what this backend supports for capability gating.
	Capabilities() Capabilities
}

// Compile-time proof that the backend contract satisfies the profile store's
// validator interface, so a registered Backend can validate profiles directly.
var _ profiles.BackendValidator = (Backend)(nil)
