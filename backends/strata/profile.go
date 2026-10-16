package strata

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"inferencerig/backends"
	"inferencerig/config"
	"inferencerig/core/modelcatalog"
	"inferencerig/core/profiles"
	"inferencerig/core/runtime"
)

const (
	defaultHost   = "127.0.0.1"
	readinessPath = "/health"
	// A config source loads tens of GB of experts into RAM before serving.
	serverReadiness = 15 * time.Minute
	// A setup run may first fetch the engine and download a 30-55 GB model.
	setupReadiness = 4 * time.Hour
)

// ValidateProfile validates Strata arguments and fills the loopback host.
func (b *Backend) ValidateProfile(p profiles.Profile) (profiles.Profile, error) {
	if p.Listen.Port < 1 || p.Listen.Port > 65535 {
		return profiles.Profile{}, fmt.Errorf("listen.port %d out of range", p.Listen.Port)
	}
	if p.Listen.Host == "" {
		p.Listen.Host = defaultHost
	}
	if _, err := buildCommand(b.checkout(), p); err != nil {
		return profiles.Profile{}, err
	}
	return p, nil
}

// Materialize renders the command without writing backend files: Strata's
// setup writes and owns its own engine config.
func (b *Backend) Materialize(p profiles.Profile) (backends.Materialization, error) {
	command, err := buildCommand(b.checkout(), p)
	if err != nil {
		return backends.Materialization{}, err
	}
	return backends.Materialization{Summary: command.Display}, nil
}

// LaunchSpec returns the neutral supervisor specification for one profile.
func (b *Backend) LaunchSpec(p profiles.Profile, _ backends.Materialization) (runtime.LaunchSpec, error) {
	spec := runtime.LaunchSpec{
		Name: "strata-" + p.Name, Host: p.Listen.Host, Port: p.Listen.Port,
		ReadinessPath: readinessPath, ReadinessTimeout: serverReadiness,
		LogName: config.LogServiceEngine,
	}
	checkout := b.checkout()
	command, err := buildCommand(checkout, p)
	switch {
	case err != nil:
		spec.BuildErr = fmt.Errorf("render Strata command: %w", err)
		return spec, nil
	case command.Setup && checkout == "":
		spec.BuildErr = errNotInstalled
		return spec, nil
	case command.Setup:
		spec.ReadinessTimeout = setupReadiness
	}
	spec.Executable, spec.Argv, spec.Env = command.Executable, command.Argv, command.Env
	spec.PIDDir, err = b.pidDir()
	if err != nil {
		spec.BuildErr = err
	}
	return spec, nil
}

// checkout is the managed Strata checkout, or "" when none is installed.
func (b *Backend) checkout() string {
	root, err := b.engineRoot()
	if err != nil {
		return ""
	}
	state, err := backends.ReadInstallState(root)
	if err != nil || state.Active == nil {
		return ""
	}
	return state.Active.Directory
}

// Fit estimates against RAM plus VRAM: Strata keeps the hot experts on the GPU
// and computes the rest from system RAM, so the model needs both together, not
// either one alone.
func (b *Backend) Fit(_ profiles.Profile, sizeBytes int64, host backends.HostResources) (backends.FitEstimate, error) {
	required := int64(0)
	if sizeBytes > 0 {
		required = sizeBytes + modelcatalog.DefaultOverheadBytes
	}
	verdict := modelcatalog.EstimateFit(required, host.AvailableRAMBytes+host.VRAMBytes, "RAM + VRAM")
	return backends.FitEstimate{
		Level: backends.FitLevel(verdict.Level), Reason: verdict.Reason,
		RequiredBytes: verdict.RequiredBytes, AvailableBytes: verdict.AvailableBytes,
	}, nil
}

// Resolve passes the source through as one opaque artifact. Strata's setup
// downloads and prepares its own model files, so there is nothing for the
// shared downloader to expand.
func (b *Backend) Resolve(_ context.Context, p profiles.Profile) (backends.ResolvedModel, error) {
	if p.Model.Source == "" {
		return backends.ResolvedModel{}, errMissingModel
	}
	name := cmp.Or(p.Model.Reference, filepath.Base(p.Model.Source))
	return backends.ResolvedModel{
		Source: p.Model.Source, Reference: p.Model.Reference, MultiFile: true,
		Artifacts: []backends.ArtifactRef{{Name: name, URI: p.Model.Source}},
	}, nil
}

// Plan creates a one-item multi-file plan under model storage.
func (b *Backend) Plan(r backends.ResolvedModel) (backends.ArtifactPlan, error) {
	if len(r.Artifacts) == 0 {
		return backends.ArtifactPlan{}, fmt.Errorf("resolved model has no artifacts")
	}
	root, err := b.storageDir()
	if err != nil {
		return backends.ArtifactPlan{}, err
	}
	target := filepath.Join(root, Name)
	plan := backends.ArtifactPlan{MultiFile: true, TargetRoot: target}
	for _, artifact := range r.Artifacts {
		name := filepath.Base(filepath.Clean(artifact.Name))
		if name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			return backends.ArtifactPlan{}, fmt.Errorf("unsafe artifact name %q", artifact.Name)
		}
		plan.Items = append(plan.Items, backends.ArtifactItem{
			URI: artifact.URI, Filename: name, TargetPath: filepath.Join(target, name),
			SizeBytes: artifact.SizeBytes, SHA256: artifact.SHA256,
		})
		plan.TotalBytes += artifact.SizeBytes
	}
	return plan, nil
}
