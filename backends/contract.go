package backends

import (
	"context"

	"inferencerig/core/modelcatalog"
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

	// Fit estimates whether a model of sizeBytes fits the given host resources;
	// it works for both discrete-VRAM and unified-memory hosts. A non-positive
	// sizeBytes yields an "unknown" verdict, which is all a profile alone can
	// support: a profile names a model but does not know how large it is.
	Fit(p profiles.Profile, sizeBytes int64, host HostResources) (FitEstimate, error)

	// Install installs or upgrades the managed engine and reports what happened.
	Install(ctx context.Context, opts InstallOptions) (InstallResult, error)

	// Rollback returns the managed engine to its previously recorded
	// installation, reporting the restored version. It fails with
	// ErrNoPreviousInstall when nothing is recorded to return to.
	Rollback(ctx context.Context) (InstallResult, error)

	// InstallStatus reports whether the engine is currently usable.
	InstallStatus(ctx context.Context) (InstallStatus, error)

	// Capabilities advertises what this backend supports for capability gating.
	Capabilities() Capabilities

	// CatalogPolicy interprets remote repository files and owns local artifact
	// layout behind the shared catalog mechanism.
	CatalogPolicy() modelcatalog.CatalogPolicy
}

// RuntimeActivator is the optional facet a backend implements when a started
// process is not yet serving the profile's model. It is deliberately outside
// Backend: an engine that loads its model at startup needs no such step, and
// requiring an empty method of every backend would suggest otherwise.
//
// llama.cpp's router is the case that motivates it. It is launched with every
// profile as a preset and loads one only when a request for it arrives, so
// starting a profile otherwise leaves the engine idle and the first caller pays
// the whole model-load latency. Activation makes "started" mean what the UI
// already implies: this profile is the one being loaded.
type RuntimeActivator interface {
	// ActivateRuntime asks the running process to begin serving p. It is called
	// after the supervisor reports readiness. Implementations should be
	// idempotent, since a profile already active is a success, not a conflict.
	ActivateRuntime(ctx context.Context, p profiles.Profile) error
}

// ProfileImporter is the optional facet a backend implements when the file it
// materializes is one users edit in place, and those edits can be carried back
// into the canonical YAML the profile store owns. It is outside Backend because
// most backends generate a launch command and nothing durable to edit.
//
// The direction of authority does not change: YAML stays canonical, the
// generated file stays derived, and importing happens *before* regeneration so
// the very next render carries the adopted values. An importer decides nothing
// about persistence — it returns the YAML it would like written, and the
// control manager writes it through the store, which re-validates.
type ProfileImporter interface {
	// ImportGenerated inspects this backend's generated file against docs —
	// every profile the backend owns — and returns the canonical YAML each
	// profile should adopt, keyed by profile name, plus the keys whose edits it
	// discarded because canonical YAML had moved too. Both empty is the common
	// case: the file holds nothing to adopt.
	//
	// Conflicting keys are reported rather than merged: with both sides moved
	// there is no non-arbitrary way to pick, so the canonical side wins by the
	// same rule that governs every render, and the operator is told what was
	// dropped. Implementations must read only — the control manager persists.
	ImportGenerated(docs []profiles.ProfileDocument) (adopt map[string]string, conflicts []string, err error)
}

// Compile-time proof that the backend contract satisfies the profile store's
// validator interface, so a registered Backend can validate profiles directly.
var _ profiles.BackendValidator = (Backend)(nil)
