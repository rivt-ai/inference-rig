package backends

import (
	"context"
	"io"
)

// FitLevel is the neutral verdict of a fit estimate. It is deliberately engine
// agnostic: the same four levels describe a discrete-VRAM engine and a
// unified-memory engine.
type FitLevel string

const (
	// FitUnknown means capacity or requirement could not be determined.
	FitUnknown FitLevel = "unknown"
	// FitFits means the model comfortably fits available capacity.
	FitFits FitLevel = "fits"
	// FitMarginal means the model fits but leaves little headroom.
	FitMarginal FitLevel = "marginal"
	// FitTooLarge means the model exceeds available capacity.
	FitTooLarge FitLevel = "too_large"
)

// Valid reports whether l is one of the defined levels.
func (l FitLevel) Valid() bool {
	switch l {
	case FitUnknown, FitFits, FitMarginal, FitTooLarge:
		return true
	default:
		return false
	}
}

// GeneratedFile is one rendered on-disk artifact a backend materializes from a
// profile (for example a generated config file). Content is the exact bytes to
// write; Path is where the backend expects it, relative to the backend's
// generated-output root or absolute.
type GeneratedFile struct {
	Path    string
	Content []byte
	Mode    uint32
}

// Materialization is the neutral result of rendering a profile into the
// backend's runtime form. A backend that renders on-disk config populates
// Files; a backend that renders only a launch command may leave Files empty and
// carry the command in the LaunchSpec instead. Summary is a human-readable
// description of what was rendered. No engine-specific format leaks through
// this type.
type Materialization struct {
	Files   []GeneratedFile
	Summary string
}

// ArtifactRef is a concrete artifact a resolved model is composed of — one
// file, identified neutrally by a fetch URI and a name.
type ArtifactRef struct {
	Name      string
	URI       string
	SizeBytes int64
}

// ResolvedModel is the outcome of mapping a profile's model source/reference to
// concrete artifact references. MultiFile distinguishes a directory-style
// snapshot (many files) from a single-file artifact, without naming either
// engine's format.
type ResolvedModel struct {
	Source    string
	Reference string
	MultiFile bool
	Artifacts []ArtifactRef
	Metadata  map[string]string
}

// ArtifactItem is one unit of a download plan.
type ArtifactItem struct {
	URI        string
	Filename   string
	TargetPath string
	SizeBytes  int64
}

// ArtifactPlan is a neutral, executor-ready download plan. It covers both a
// single-file artifact (one item) and a multi-file snapshot (many items) with
// no engine branch; the Phase-8 download executor consumes this type.
type ArtifactPlan struct {
	MultiFile  bool
	TargetRoot string
	Items      []ArtifactItem
	TotalBytes int64
}

// HostResources describes the machine a fit estimate runs against. It carries
// both the discrete-accelerator axis (HasGPU/VRAMBytes) and the unified-memory
// axis (UnifiedMemory/MemoryBudgetBytes) so a single neutral type serves both
// backend families; a backend reads only the fields it cares about.
type HostResources struct {
	TotalRAMBytes     int64
	AvailableRAMBytes int64
	HasGPU            bool
	VRAMBytes         int64
	UnifiedMemory     bool
	MemoryBudgetBytes int64
}

// FitEstimate is a backend's decision about whether a model fits a host.
type FitEstimate struct {
	Level          FitLevel
	Reason         string
	RequiredBytes  int64
	AvailableBytes int64
}

// InstallOptions requests an install or upgrade of a managed engine. Version
// empty means "latest". Progress, when non-nil, receives human-readable
// progress output.
type InstallOptions struct {
	Version  string
	Upgrade  bool
	Force    bool
	Progress io.Writer
}

// InstallResult reports what an install did. Changed is the idempotency signal:
// false means the requested version was already active and nothing was done.
type InstallResult struct {
	Version string
	Path    string
	Changed bool
	Message string
}

// Capabilities advertises what a backend supports so shared code can gate
// behavior by capability rather than by branching on a backend name. Fields are
// intentionally minimal; later phases extend this type as new gated behavior
// appears.
type Capabilities struct {
	// SingleFileArtifacts / MultiFileArtifacts report which artifact forms the
	// backend produces. At least one must be true.
	SingleFileArtifacts bool
	MultiFileArtifacts  bool
	// DiscreteVRAM / UnifiedMemory report which memory model the backend's fit
	// estimation uses. At least one must be true.
	DiscreteVRAM  bool
	UnifiedMemory bool
	// ManagedInstall reports whether Install manages an engine install/upgrade.
	ManagedInstall bool
	// SingleActiveProfile reports whether the backend serves at most one profile
	// at a time (switching stops the current process before starting the next).
	SingleActiveProfile bool
	// ParameterIntrospection reports whether the optional ParameterProvider
	// interface is available.
	ParameterIntrospection bool
}

type Parameter struct {
	Name        string
	Description string
	Required    bool
}

// ParameterProvider is an optional backend facet used only when advertised.
type ParameterProvider interface {
	Parameters(context.Context) ([]Parameter, error)
}
