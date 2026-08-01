// Package backendtest provides a deterministic fake backend and a reusable
// contract test suite. The fake implements the full backends.Backend contract
// with no real engine, so the registry and profile-store tests can drive a
// backend purely through interfaces. Phases 6/7 run their real backends through
// RunContractTests to prove they honor the same invariants.
package backendtest

import (
	"context"
	"fmt"

	"inferencerig/backends"
	"inferencerig/core/modelcatalog"
	"inferencerig/core/profiles"
	"inferencerig/core/runtime"
)

// Fake is a deterministic, engine-free implementation of backends.Backend.
type Fake struct {
	name      string
	installed bool
	version   string
	previous  string
}

// New returns a Fake registered under name.
func New(name string) *Fake { return &Fake{name: name} }

// Name returns the backend key.
func (f *Fake) Name() string { return f.name }

// ValidateProfile accepts a structurally valid profile and rejects one missing
// a model source or carrying an engine_args "reject" flag. It normalizes by
// defaulting the listen host.
func (f *Fake) ValidateProfile(p profiles.Profile) (profiles.Profile, error) {
	if p.Model.Source == "" {
		return profiles.Profile{}, fmt.Errorf("model.source is required")
	}
	if p.Listen.Port < 1 || p.Listen.Port > 65535 {
		return profiles.Profile{}, fmt.Errorf("listen.port %d out of range", p.Listen.Port)
	}
	if _, bad := p.EngineArgs["reject"]; bad {
		return profiles.Profile{}, fmt.Errorf("engine_args rejected by backend")
	}
	if p.Listen.Host == "" {
		p.Listen.Host = "127.0.0.1"
	}
	return p, nil
}

// Materialize renders a single deterministic config file from the profile.
func (f *Fake) Materialize(p profiles.Profile) (backends.Materialization, error) {
	content := fmt.Sprintf("name=%s\nsource=%s\nport=%d\n", p.Name, p.Model.Source, p.Listen.Port)
	return backends.Materialization{
		Files:   []backends.GeneratedFile{{Path: p.Name + ".conf", Content: []byte(content), Mode: 0o600}},
		Summary: "rendered 1 config file",
	}, nil
}

// LaunchSpec builds a runnable supervisor spec from the profile.
func (f *Fake) LaunchSpec(p profiles.Profile, _ backends.Materialization) (runtime.LaunchSpec, error) {
	return runtime.LaunchSpec{
		Name:       p.Name,
		Executable: "fake-server",
		Argv:       []string{"--source", p.Model.Source},
		Host:       p.Listen.Host,
		Port:       p.Listen.Port,
		PIDDir:     "/tmp",
	}, nil
}

// Resolve maps the profile's model source to a single concrete artifact.
func (f *Fake) Resolve(_ context.Context, p profiles.Profile) (backends.ResolvedModel, error) {
	return backends.ResolvedModel{
		Source:    p.Model.Source,
		Reference: p.Model.Reference,
		MultiFile: false,
		Artifacts: []backends.ArtifactRef{{
			Name:      "model.bin",
			URI:       p.Model.Source,
			SizeBytes: 1024,
		}},
	}, nil
}

// Plan turns a resolved model into a download plan mirroring its artifacts.
func (f *Fake) Plan(r backends.ResolvedModel) (backends.ArtifactPlan, error) {
	if len(r.Artifacts) == 0 {
		return backends.ArtifactPlan{}, fmt.Errorf("resolved model has no artifacts")
	}
	plan := backends.ArtifactPlan{MultiFile: r.MultiFile}
	for _, a := range r.Artifacts {
		plan.Items = append(plan.Items, backends.ArtifactItem{
			URI:        a.URI,
			Filename:   a.Name,
			TargetPath: "/models/" + a.Name,
			SizeBytes:  a.SizeBytes,
		})
		plan.TotalBytes += a.SizeBytes
	}
	if len(plan.Items) == 1 {
		plan.TargetRoot = plan.Items[0].TargetPath
	}
	return plan, nil
}

// Fit compares a fixed model size to the host's discrete VRAM (or RAM).
func (f *Fake) Fit(_ profiles.Profile, sizeBytes int64, host backends.HostResources) (backends.FitEstimate, error) {
	required := sizeBytes
	if required <= 0 {
		required = 2 << 30
	}
	capacity := host.AvailableRAMBytes
	if host.HasGPU {
		capacity = host.VRAMBytes
	}
	est := backends.FitEstimate{RequiredBytes: required, AvailableBytes: capacity}
	switch {
	case capacity <= 0:
		est.Level, est.Reason = backends.FitUnknown, "capacity unknown"
	case required <= int64(float64(capacity)*0.90):
		est.Level, est.Reason = backends.FitFits, "within capacity"
	case required <= capacity:
		est.Level, est.Reason = backends.FitMarginal, "close to capacity"
	default:
		est.Level, est.Reason = backends.FitTooLarge, "exceeds capacity"
	}
	return est, nil
}

// Install is idempotent: the first call reports Changed, later calls do not
// (unless Force is set).
func (f *Fake) Install(_ context.Context, opts backends.InstallOptions) (backends.InstallResult, error) {
	version := opts.Version
	if version == "" {
		version = "1.0.0"
	}
	changed := opts.Force || !f.installed
	if changed && f.installed {
		f.previous = f.version
	}
	f.installed, f.version = true, version
	return backends.InstallResult{
		Version: version,
		Path:    "/opt/fake/" + version,
		Changed: changed,
		Message: "fake engine ready",
	}, nil
}

// Rollback swaps back to the version installed before the current one.
func (f *Fake) Rollback(context.Context) (backends.InstallResult, error) {
	if f.previous == "" {
		return backends.InstallResult{}, backends.ErrNoPreviousInstall
	}
	f.version, f.previous = f.previous, f.version
	return backends.InstallResult{
		Version: f.version,
		Path:    "/opt/fake/" + f.version,
		Changed: true,
		Message: "rolled back to fake " + f.version,
	}, nil
}

func (f *Fake) InstallStatus(context.Context) (backends.InstallStatus, error) {
	if !f.installed {
		return backends.InstallStatus{}, nil
	}
	return backends.InstallStatus{
		Installed: true,
		Managed:   true,
		Version:   f.version,
		Path:      "/opt/fake/" + f.version,
	}, nil
}

// Capabilities advertises a single-file, discrete-VRAM, managed-install backend.
func (f *Fake) Capabilities() backends.Capabilities {
	return backends.Capabilities{
		SingleFileArtifacts: true,
		DiscreteVRAM:        true,
		ManagedInstall:      true,
	}
}

func (f *Fake) CatalogPolicy() modelcatalog.CatalogPolicy { return fakeCatalogPolicy{} }

type fakeCatalogPolicy struct{}

func (fakeCatalogPolicy) SearchFilter() string { return "" }

func (fakeCatalogPolicy) Variants(_ modelcatalog.Source, files []modelcatalog.RemoteFile) ([]modelcatalog.Variant, error) {
	if len(files) == 0 {
		return nil, nil
	}
	return []modelcatalog.Variant{{Name: files[0].Name, SizeBytes: files[0].SizeBytes}}, nil
}
func (fakeCatalogPolicy) ListLocal(context.Context) ([]modelcatalog.LocalModel, error) {
	return nil, nil
}
func (fakeCatalogPolicy) DeleteLocal(string) error { return nil }

// Ensure the fake satisfies the full contract at compile time.
var _ backends.Backend = (*Fake)(nil)
