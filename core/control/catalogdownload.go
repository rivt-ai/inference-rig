package control

import (
	"context"

	"inferencerig/backends"
	"inferencerig/core/modeldownload"
	"inferencerig/core/profiles"
)

// ResolveModel resolves an arbitrary catalog reference through a backend,
// without a profile. Browsing a catalog comes before having a profile, so
// requiring one to look at a model inverts the order a user works in.
func (m *Manager) ResolveModel(
	ctx context.Context,
	backendName, reference, variantReference string,
) (backends.ResolvedModel, backends.ArtifactPlan, error) {
	if backendName == "" || reference == "" {
		return backends.ResolvedModel{}, backends.ArtifactPlan{},
			Errorf(ErrorInvalidInput, "backend and reference are required")
	}
	backend, err := m.Backend(backendName)
	if err != nil {
		return backends.ResolvedModel{}, backends.ArtifactPlan{}, err
	}
	// A synthetic profile carries the reference into the backend's own resolver,
	// so catalog resolution and profile resolution share one code path and
	// cannot disagree about how a reference is interpreted.
	// The variant travels in Reference, not folded into Source: a catalog entry
	// is a repository plus a file inside it, and a bare filename has no host to
	// fetch from. This is the same split a profile stores.
	probe := profiles.Profile{
		Backend: backendName,
		Model:   profiles.ModelSpec{Source: reference, Reference: variantReference},
	}
	resolved, err := backend.Resolve(ctx, probe)
	if err != nil {
		return resolved, backends.ArtifactPlan{}, CoreError(ErrorInvalidInput, err.Error(), err)
	}
	plan, err := backend.Plan(resolved)
	if err != nil {
		return resolved, plan, CoreError(ErrorInvalidInput, err.Error(), err)
	}
	return resolved, plan, nil
}

// StartCatalogDownload downloads a catalog reference that no profile points at
// yet. The resulting job carries the backend but no profile, so applying it
// later is an explicit choice of which profile receives the model.
func (m *Manager) StartCatalogDownload(
	ctx context.Context,
	backendName, reference, variantReference string,
	force bool,
) (job modeldownload.Job, err error) {
	defer m.recording(ctx, "download.start", &err)()
	if m.downloads == nil {
		return job, Errorf(ErrorInvalidInput, "downloads are not configured")
	}
	_, plan, err := m.ResolveModel(ctx, backendName, reference, variantReference)
	if err != nil {
		return job, err
	}
	job, err = m.downloads.Start(ctx, modeldownload.Request{
		Plan: plan, Force: force, Backend: backendName,
	})
	return job, mapDownloadError(err)
}
