package control

import (
	"context"

	"inferencerig/backends"
	"inferencerig/core/profiles"
)

// hostResourceProber is the optional backend facet reporting accelerator
// hardware. It mirrors the one bootstrap uses to build the telemetry collector:
// only the backend knows whether its device owns discrete VRAM or draws on
// system memory, and a fit verdict is meaningless without that distinction.
type hostResourceProber interface {
	HostResources(context.Context) (backends.HostResources, []string)
}

// HostResources assembles the neutral host view a fit estimate needs. Shared
// memory comes from the telemetry collector; the accelerator axis comes from
// the named backend's own probe. A backend without the probe still gets the
// memory fields, which is enough for a CPU-bound verdict.
func (m *Manager) HostResources(ctx context.Context, backendName string) (backends.HostResources, error) {
	backend, err := m.Backend(backendName)
	if err != nil {
		return backends.HostResources{}, err
	}
	host := backends.HostResources{}
	if prober, ok := backend.(hostResourceProber); ok {
		host, _ = prober.HostResources(ctx)
	}
	snapshot, err := m.Signals(ctx)
	if err != nil {
		// Telemetry is best-effort here: a host with no collector can still
		// receive an "unknown" verdict, which is honest, rather than an error
		// that fails the whole catalog listing.
		return host, nil
	}
	host.TotalRAMBytes = int64(snapshot.Memory.TotalBytes)
	host.AvailableRAMBytes = int64(snapshot.Memory.AvailableBytes)
	if host.UnifiedMemory && host.MemoryBudgetBytes <= 0 {
		// A unified device shares system RAM, so available RAM is the budget.
		host.MemoryBudgetBytes = host.AvailableRAMBytes
	}
	return host, nil
}

// EstimateFit reports whether a model of sizeBytes fits the host for the named
// backend's memory model. When profileName is set, the profile supplies the
// backend and the model context; sizeBytes still decides the verdict, because a
// profile names a model without knowing how large it is.
func (m *Manager) EstimateFit(
	ctx context.Context,
	backendName, profileName string,
	sizeBytes int64,
) (backends.FitEstimate, backends.HostResources, error) {
	profile := profiles.Profile{}
	if profileName != "" {
		doc, backend, err := m.profileBackend(ctx, profileName)
		if err != nil {
			return backends.FitEstimate{}, backends.HostResources{}, err
		}
		profile, backendName = doc.Effective, backend.Name()
	}
	if backendName == "" {
		return backends.FitEstimate{}, backends.HostResources{},
			Errorf(ErrorInvalidInput, "backend or profile is required")
	}
	backend, err := m.Backend(backendName)
	if err != nil {
		return backends.FitEstimate{}, backends.HostResources{}, err
	}
	host, err := m.HostResources(ctx, backendName)
	if err != nil {
		return backends.FitEstimate{}, backends.HostResources{}, err
	}
	estimate, err := backend.Fit(profile, sizeBytes, host)
	if err != nil {
		return backends.FitEstimate{}, host, CoreError(ErrorRuntime, err.Error(), err)
	}
	return estimate, host, nil
}
