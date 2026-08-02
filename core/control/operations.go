package control

import (
	"context"
	"errors"
	"os"
	"slices"
	"sort"

	"inferencerig/backends"
	"inferencerig/config"
	"inferencerig/core/configstore"
	"inferencerig/core/modeldownload"
	"inferencerig/core/profiles"
	coreruntime "inferencerig/core/runtime"
)

type Info struct {
	Profiles          int
	Backends          int
	RunningProfiles   []string
	AutostartProfiles []string
	StartupServices   []string
	// ActiveBackend is the one backend currently serving, empty when none is.
	// Profiles naming any other backend cannot start until a reset, and every
	// front end renders that from this field rather than deriving it.
	ActiveBackend string
}

// RestartRuntime stops then starts a profile. The stop leaves the slot empty (or
// the profile deactivated on a router backend), so the start needs no replace.
func (m *Manager) RestartRuntime(ctx context.Context, name string) (result RuntimeRestart, err error) {
	defer m.recording(ctx, "runtime.restart", &err)()
	stopped, err := m.StopRuntime(ctx, name)
	if err != nil {
		return result, err
	}
	started, err := m.StartRuntime(ctx, name, false)
	return RuntimeRestart{Stopped: stopped, Started: started}, err
}

type RuntimeRestart struct {
	Stopped, Started coreruntime.CommandResult
}

func (m *Manager) ApplyDownloadToProfile(ctx context.Context, name, id string) (doc profiles.ProfileDocument, err error) {
	defer m.recording(ctx, "download.apply", &err)()
	_, updated, err := m.planDownloadApply(ctx, name, id)
	if err != nil {
		return profiles.ProfileDocument{}, err
	}
	return m.PutProfile(ctx, name, updated, false)
}

// PreviewDownloadApply returns the profile YAML before and after applying a
// download, without writing anything, so a caller can show the change and let
// the user decide.
func (m *Manager) PreviewDownloadApply(ctx context.Context, name, id string) (original, updated string, err error) {
	doc, updated, err := m.planDownloadApply(ctx, name, id)
	if err != nil {
		return "", "", err
	}
	return doc.ProfileYAML, updated, nil
}

// planDownloadApply validates that the download belongs to the profile and
// renders the resulting YAML. Applying and previewing must agree, so both go
// through here rather than duplicating the checks.
func (m *Manager) planDownloadApply(
	ctx context.Context,
	name, id string,
) (profiles.ProfileDocument, string, error) {
	if m.downloads == nil {
		return profiles.ProfileDocument{}, "", errNoDownloads
	}
	job, err := m.downloads.Get(ctx, id)
	if err != nil {
		return profiles.ProfileDocument{}, "", mapDownloadError(err)
	}
	if job.State != modeldownload.StateCompleted && job.State != modeldownload.StateAlreadyDownloaded {
		return profiles.ProfileDocument{}, "", Errorf(ErrorConflict, "download %q is %s", id, job.State)
	}
	doc, backend, err := m.profileBackend(ctx, name)
	if err != nil {
		return profiles.ProfileDocument{}, "", err
	}
	// A catalog download carries no profile by design — it is started while
	// browsing, before the user has decided where it goes — so an empty profile
	// is applicable anywhere the backend and layout agree. A job that names a
	// different profile is still refused: that one was started for something else.
	if job.Profile != "" && job.Profile != name {
		return profiles.ProfileDocument{}, "", Errorf(ErrorConflict, "download %q does not belong to profile %q", id, name)
	}
	if job.Backend != backend.Name() || job.MultiFile != backend.Capabilities().MultiFileArtifacts {
		return profiles.ProfileDocument{}, "", Errorf(ErrorConflict,
			"download %q was fetched for backend %q, which profile %q does not use", id, job.Backend, name)
	}
	updated := doc.Parsed
	updated.Model.Source, updated.Model.Reference = job.TargetPath, ""
	// Merged into the file rather than re-rendered from the struct: applying a
	// download changes one path, and must not cost the operator the comments
	// they wrote around everything else in the profile.
	data, err := profiles.MergeYAML(doc.ProfileYAML, updated)
	if err != nil {
		return profiles.ProfileDocument{}, "", CoreError(ErrorRuntime, err.Error(), err)
	}
	return doc, data, nil
}

func (m *Manager) CleanupProfile(ctx context.Context, name string) (err error) {
	defer m.recording(ctx, "profile.cleanup", &err)()
	_, backend, err := m.profileBackend(ctx, name)
	if err != nil {
		return err
	}
	_, plan, err := m.ResolveProfileModel(ctx, name)
	if err != nil {
		return err
	}
	if err := m.requireUnsharedTarget(ctx, name, plan.TargetRoot); err != nil {
		return err
	}
	if _, err := m.DeleteProfile(ctx, name); err != nil {
		return err
	}
	if err := backend.CatalogPolicy().DeleteLocal(plan.TargetRoot); err != nil && !errors.Is(err, os.ErrNotExist) {
		return CoreError(ErrorRuntime, "profile removed but local model cleanup failed", err)
	}
	return nil
}

func (m *Manager) requireUnsharedTarget(ctx context.Context, name, target string) error {
	items, err := m.profiles.List(ctx)
	if err != nil {
		return mapProfileError(err)
	}
	for _, item := range items {
		if item.Name == name {
			continue
		}
		// Resolution can reach the network, so a failure means "unknown", not
		// "unrelated". Refuse rather than delete files another profile may own;
		// the caller can retry once resolution works again.
		_, plan, err := m.ResolveProfileModel(ctx, item.Name)
		if err != nil {
			return Errorf(ErrorConflict,
				"cannot verify whether profile %q shares this local model: %v", item.Name, err)
		}
		if plan.TargetRoot == target {
			return Errorf(ErrorConflict, "local model is also referenced by profile %q", item.Name)
		}
	}
	return nil
}

func (m *Manager) SetProfileAutostart(ctx context.Context, name string, enabled bool) (configstore.WriteResult, error) {
	if m.config == nil {
		return configstore.WriteResult{}, Errorf(ErrorInvalidInput, "config store is not configured")
	}
	if enabled {
		if _, err := m.GetProfile(ctx, name); err != nil {
			return configstore.WriteResult{}, err
		}
		cfg, err := m.config.Read(ctx)
		if err != nil {
			return configstore.WriteResult{}, mapConfigError(err)
		}
		prospective := append([]string(nil), cfg.AutostartProfiles...)
		if !slices.Contains(prospective, name) {
			prospective = append(prospective, name)
		}
		if err := m.ValidateAutostart(ctx, prospective); err != nil {
			return configstore.WriteResult{}, err
		}
	}
	result, err := m.config.SetProfileAutostart(ctx, name, enabled)
	return result, mapConfigError(err)
}

func (m *Manager) SetStartupServices(ctx context.Context, services []string) (configstore.WriteResult, error) {
	if m.config == nil {
		return configstore.WriteResult{}, Errorf(ErrorInvalidInput, "config store is not configured")
	}
	if err := config.ValidateStartupServices(services); err != nil {
		return configstore.WriteResult{}, Errorf(ErrorInvalidInput, "%v", err)
	}
	result, err := m.config.SetStartupServices(ctx, services)
	return result, mapConfigError(err)
}

func (m *Manager) GetInfo(ctx context.Context) (Info, error) {
	profileItems, err := m.profiles.List(ctx)
	if err != nil {
		return Info{}, mapProfileError(err)
	}
	info := Info{Profiles: len(profileItems), Backends: len(m.registry.Names())}
	if m.config != nil {
		if cfg, err := m.config.Read(ctx); err == nil {
			info.AutostartProfiles = append([]string(nil), cfg.AutostartProfiles...)
			info.StartupServices = append([]string(nil), cfg.StartupServices...)
		}
	}
	m.mu.Lock()
	if m.slot != nil {
		info.ActiveBackend = m.slot.backend
		info.RunningProfiles = append(info.RunningProfiles, m.slot.profiles...)
	}
	m.mu.Unlock()
	sort.Strings(info.RunningProfiles)
	return info, nil
}

func (m *Manager) GetBackendParams(ctx context.Context, name string) ([]backends.Parameter, error) {
	backend, err := m.Backend(name)
	if err != nil {
		return nil, err
	}
	if !backend.Capabilities().ParameterIntrospection {
		return nil, Errorf(ErrorInvalidInput, "backend %q does not support parameter introspection", name)
	}
	provider, ok := backend.(backends.ParameterProvider)
	if !ok {
		return nil, Errorf(ErrorRuntime, "backend %q advertised parameter introspection without an adapter", name)
	}
	params, err := provider.Parameters(ctx)
	if err != nil {
		return nil, CoreError(ErrorRuntime, err.Error(), err)
	}
	return params, nil
}

func mapConfigError(err error) error {
	if err == nil {
		return nil
	}
	return CoreError(ErrorInvalidInput, err.Error(), err)
}
