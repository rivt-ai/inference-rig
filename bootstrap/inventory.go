package bootstrap

import (
	"context"
	"errors"

	"github.com/shirou/gopsutil/v4/disk"

	"inferencerig/backends"
	"inferencerig/config"
	"inferencerig/core/doctor"
	"inferencerig/core/modelcatalog"
	"inferencerig/core/signals"
)

// Inventory answers what only the backend registry can: which engines are
// installed and intact, what accelerators the host has, and what is in model
// storage.
//
// It is the single seam keeping every registry-dependent check out of
// core/doctor's import graph, in the same shape as ValidateConfig. It opens no
// socket and starts nothing.
func Inventory(ctx context.Context, verifyModels bool) (doctor.Inventory, error) {
	cfg, err := config.LoadOrDefault()
	if err != nil {
		return doctor.Inventory{}, err
	}
	registry, _, _, modelStorageDir, err := controlInputs(cfg)
	if err != nil {
		return doctor.Inventory{}, err
	}
	accelerators, warnings := acceleratorProbe(registry)(ctx)
	return doctor.Inventory{
		Engines:      engineStates(ctx, registry),
		Accelerators: acceleratorStates(accelerators),
		Models:       modelState(ctx, registry, modelStorageDir, verifyModels),
		Warnings:     warnings,
	}, nil
}

// engineStates asks every backend for its install status, then — for those
// implementing the digest facet — whether what is on disk still matches what
// was installed.
func engineStates(ctx context.Context, registry *backends.Registry) []doctor.EngineState {
	var states []doctor.EngineState
	for _, name := range registry.Names() {
		backend, found := registry.Lookup(name)
		if !found {
			continue
		}
		status, err := backend.InstallStatus(ctx)
		if err != nil {
			states = append(states, doctor.EngineState{Backend: name, SkipReason: err.Error()})
			continue
		}
		state := doctor.EngineState{
			Backend: name, Installed: status.Installed, Managed: status.Managed,
			Version: status.Version, Path: status.Path,
		}
		verifyEngine(ctx, backend, name, &state)
		states = append(states, state)
	}
	return states
}

// verifyEngine re-hashes a managed install against its record. Only the backend
// knows what its own digest covers — an executable, a lock file — so this goes
// through the facet rather than switching on the backend name.
func verifyEngine(ctx context.Context, backend backends.Backend, name string, state *doctor.EngineState) {
	if !state.Installed || !state.Managed {
		return
	}
	verifier, ok := backend.(backends.InstallDigestVerifier)
	if !ok {
		state.SkipReason = "this backend records no verifiable digest"
		return
	}
	root, err := backends.EngineRoot(name)
	if err != nil {
		state.SkipReason = err.Error()
		return
	}
	installed, err := backends.ReadInstallState(root)
	if err != nil || installed.Active == nil {
		state.SkipReason = "no install record found"
		return
	}
	result, err := verifier.VerifyInstall(ctx, *installed.Active)
	switch {
	case err != nil:
		state.SkipReason = err.Error()
	case result.Skipped:
		state.SkipReason = result.Reason
	case result.Matched:
		state.Verified = true
	default:
		state.Mismatched = true
	}
}

func acceleratorStates(stats []signals.AcceleratorStats) []doctor.AcceleratorState {
	states := make([]doctor.AcceleratorState, 0, len(stats))
	for _, stat := range stats {
		states = append(states, doctor.AcceleratorState{
			Name: stat.Name, UnifiedMemory: stat.UnifiedMemory,
			TotalBytes: stat.TotalBytes, UsedBytes: stat.UsedBytes,
		})
	}
	return states
}

// modelState reports what is in model storage and how much room is left.
//
// Listing goes through each backend's own catalog policy, because which files
// count as models is a backend question. Hashing is opt-in: model files run to
// hundreds of gigabytes, and reading all of them is not something a diagnostic
// should do unasked.
func modelState(ctx context.Context, registry *backends.Registry, root string, verify bool) doctor.ModelState {
	state := doctor.ModelState{Root: root, VerifyRequested: verify}
	if root == "" {
		return state
	}
	if usage, err := disk.UsageWithContext(ctx, root); err == nil {
		state.FreeBytes, state.DiskBytes = usage.Free, usage.Total
	}
	seen := map[string]struct{}{}
	for _, name := range registry.Names() {
		backend, found := registry.Lookup(name)
		if !found {
			continue
		}
		models, err := backend.CatalogPolicy().ListLocal(ctx)
		if err != nil {
			continue
		}
		for _, model := range models {
			// Two backends can accept the same file; count it once.
			if _, duplicate := seen[model.Path]; duplicate {
				continue
			}
			seen[model.Path] = struct{}{}
			state.Count++
			state.TotalBytes += model.SizeBytes
			if verify {
				countVerification(model.Path, &state)
			}
		}
	}
	return state
}

// countVerification tallies one model against its recorded digest. A model with
// nothing recorded is counted separately rather than as a failure: models that
// predate digest recording have nothing to compare against, and calling that
// corruption would be a lie.
func countVerification(path string, state *doctor.ModelState) {
	matched, err := modelcatalog.VerifyDigest(path)
	switch {
	case errors.Is(err, modelcatalog.ErrNoDigest):
		state.Unverifiable++
	case err != nil:
		state.Corrupt++
	case matched:
		state.Verified++
	default:
		state.Corrupt++
	}
}
