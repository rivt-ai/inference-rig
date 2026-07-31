package control

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"inferencerig/backends"
	"inferencerig/config"
	"inferencerig/core/configstore"
	"inferencerig/core/modelcatalog"
	"inferencerig/core/modeldownload"
	"inferencerig/core/profiles"
	coreruntime "inferencerig/core/runtime"
	"inferencerig/core/signals"
	"inferencerig/platform/filedoc"
)

// ProfileStore is the canonical profile persistence API used by control.
type ProfileStore interface {
	List(context.Context) ([]profiles.ProfileSummary, error)
	ListDocuments(context.Context) ([]profiles.ProfileDocument, error)
	Get(context.Context, string) (profiles.ProfileDocument, error)
	Create(context.Context, profiles.CreateRequest) (profiles.ProfileDocument, error)
	Replace(context.Context, string, string) (profiles.WriteResult, error)
	Delete(context.Context, string) (profiles.DeleteResult, error)
}

// Runtime is the shared supervisor surface used by control.
type Runtime interface {
	Start(context.Context) (coreruntime.CommandResult, error)
	Stop(context.Context) (coreruntime.CommandResult, error)
	Status(context.Context) (coreruntime.Status, error)
}

type ModelCatalog interface {
	Search(context.Context, modelcatalog.SearchRequest, modelcatalog.CatalogPolicy) (modelcatalog.Result, error)
	Subscribe() (<-chan modelcatalog.RefreshEvent, func())
}

type ConfigStore interface {
	Read(context.Context) (config.Config, error)
	SetStartupServices(context.Context, []string) (configstore.WriteResult, error)
	SetProfileAutostart(context.Context, string, bool) (configstore.WriteResult, error)
}

// Dependencies wires the neutral control manager.
type Dependencies struct {
	Registry       *backends.Registry
	Profiles       ProfileStore
	Downloads      modeldownload.Downloader
	Signals        signals.Collector
	Events         *EventStore
	Audit          AuditSink
	RuntimeFactory func(coreruntime.LaunchSpec) Runtime
	Catalog        ModelCatalog
	Config         ConfigStore
}

type runtimeSlot struct {
	process Runtime
	profile string
}

// Manager is the canonical in-process control plane used by RPC and later
// adapters. It coordinates only neutral interfaces.
type Manager struct {
	registry  *backends.Registry
	profiles  ProfileStore
	downloads modeldownload.Downloader
	signals   signals.Collector
	events    *EventStore
	audit     AuditSink
	factory   func(coreruntime.LaunchSpec) Runtime
	catalog   ModelCatalog
	config    ConfigStore

	mu       sync.Mutex
	runtimes map[string]runtimeSlot
}

// NewManager creates a control manager.
func NewManager(deps Dependencies) *Manager {
	if deps.Registry == nil || deps.Profiles == nil {
		panic("control: registry and profiles are required")
	}
	events := deps.Events
	if events == nil {
		events = NewEventStore(DefaultEventLimit)
	}
	audit := MultiAuditSink{events, deps.Audit}
	factory := deps.RuntimeFactory
	if factory == nil {
		factory = func(spec coreruntime.LaunchSpec) Runtime { return coreruntime.NewSupervisor(spec) }
	}
	return &Manager{
		registry: deps.Registry, profiles: deps.Profiles, downloads: deps.Downloads,
		signals: deps.Signals, events: events, audit: audit, factory: factory,
		catalog: deps.Catalog, config: deps.Config, runtimes: map[string]runtimeSlot{},
	}
}

func (m *Manager) ListModelCatalog(ctx context.Context, req modelcatalog.SearchRequest) (modelcatalog.Result, error) {
	if m.catalog == nil {
		return modelcatalog.Result{}, Errorf(ErrorInvalidInput, "model catalog is not configured")
	}
	backend, err := m.Backend(req.Backend)
	if err != nil {
		return modelcatalog.Result{}, err
	}
	result, err := m.catalog.Search(ctx, req, backend.CatalogPolicy())
	if err != nil {
		return result, CoreError(ErrorRuntime, err.Error(), err)
	}
	return result, nil
}

func (m *Manager) WatchModelCatalog() (<-chan modelcatalog.RefreshEvent, func(), error) {
	if m.catalog == nil {
		return nil, nil, Errorf(ErrorInvalidInput, "model catalog is not configured")
	}
	events, unsubscribe := m.catalog.Subscribe()
	return events, unsubscribe, nil
}

func (m *Manager) ListLocalModels(ctx context.Context, backendName string) ([]modelcatalog.LocalModel, error) {
	backend, err := m.Backend(backendName)
	if err != nil {
		return nil, err
	}
	models, err := backend.CatalogPolicy().ListLocal(ctx)
	if err != nil {
		return nil, CoreError(ErrorRuntime, err.Error(), err)
	}
	return models, nil
}

func (m *Manager) DeleteLocalModel(ctx context.Context, backendName, path string) (err error) {
	defer m.recording(ctx, "model.delete", &err)()
	if path == "" {
		return Errorf(ErrorInvalidInput, "local model path is required")
	}
	backend, err := m.Backend(backendName)
	if err != nil {
		return err
	}
	if err := backend.CatalogPolicy().DeleteLocal(path); err != nil {
		return CoreError(ErrorInvalidInput, err.Error(), err)
	}
	return nil
}

// Backend returns a registered backend or a typed not-found error.
func (m *Manager) Backend(name string) (backends.Backend, error) {
	backend, ok := m.registry.Lookup(name)
	if !ok {
		return nil, Errorf(ErrorNotFound, "backend %q not found", name)
	}
	return backend, nil
}

// Backends lists registered backend implementations in stable order.
func (m *Manager) Backends() []backends.Backend {
	names := m.registry.Names()
	result := make([]backends.Backend, 0, len(names))
	for _, name := range names {
		backend, _ := m.registry.Lookup(name)
		result = append(result, backend)
	}
	return result
}

func (m *Manager) ListProfiles(ctx context.Context) ([]profiles.ProfileSummary, error) {
	items, err := m.profiles.List(ctx)
	return items, mapProfileError(err)
}

// ListProfileDocuments returns every profile as a full document. Listing
// already reads and validates each one, so callers needing the documents take
// them here instead of re-reading every profile through GetProfile.
func (m *Manager) ListProfileDocuments(ctx context.Context) ([]profiles.ProfileDocument, error) {
	docs, err := m.profiles.ListDocuments(ctx)
	return docs, mapProfileError(err)
}

func (m *Manager) GetProfile(ctx context.Context, name string) (profiles.ProfileDocument, error) {
	doc, err := m.profiles.Get(ctx, name)
	return doc, mapProfileError(err)
}

// PutProfile creates or replaces a canonical YAML profile.
func (m *Manager) PutProfile(ctx context.Context, name, yaml string, createOnly bool) (doc profiles.ProfileDocument, err error) {
	defer m.recording(ctx, "profile.put", &err)()
	// Defers run LIFO, so this mapping is applied before the audit record above
	// observes err — matching the previous behavior of mapping at every return,
	// without a path that can forget to.
	defer func() { err = mapProfileError(err) }()
	create := profiles.CreateRequest{Name: name, ProfileYAML: yaml}
	if createOnly {
		return m.profiles.Create(ctx, create)
	}
	if _, err = m.profiles.Get(ctx, name); errors.Is(err, os.ErrNotExist) {
		return m.profiles.Create(ctx, create)
	}
	if err != nil {
		return profiles.ProfileDocument{}, err
	}
	if _, err = m.profiles.Replace(ctx, name, yaml); err != nil {
		return profiles.ProfileDocument{}, err
	}
	return m.profiles.Get(ctx, name)
}

// DeleteProfile stops its backend runtime before deleting the profile.
func (m *Manager) DeleteProfile(ctx context.Context, name string) (result profiles.DeleteResult, err error) {
	defer m.recording(ctx, "profile.delete", &err)()
	if _, stopErr := m.StopRuntime(ctx, name); stopErr != nil && Kind(stopErr) != ErrorNotFound {
		return result, stopErr
	}
	result, err = m.profiles.Delete(ctx, name)
	return result, mapProfileError(err)
}

// InstallBackend runs a backend's managed installer.
func (m *Manager) InstallBackend(ctx context.Context, name string, opts backends.InstallOptions) (result backends.InstallResult, err error) {
	defer m.recording(ctx, "backend.install", &err)()
	backend, err := m.Backend(name)
	if err != nil {
		return result, err
	}
	result, err = backend.Install(ctx, opts)
	if err != nil {
		err = CoreError(ErrorRuntime, err.Error(), err)
	}
	return result, err
}

// BackendInstallStatus reports whether a backend has a usable engine.
func (m *Manager) BackendInstallStatus(ctx context.Context, name string) (backends.InstallStatus, error) {
	backend, err := m.Backend(name)
	if err != nil {
		return backends.InstallStatus{}, err
	}
	status, err := backend.InstallStatus(ctx)
	if err != nil {
		return backends.InstallStatus{}, CoreError(ErrorRuntime, err.Error(), err)
	}
	return status, nil
}

// StartRuntime materializes all relevant profiles and starts the selected
// backend through the shared supervisor. One runtime slot per backend also
// implements single-active-profile switching without engine-name branches.
func (m *Manager) StartRuntime(ctx context.Context, name string) (result coreruntime.CommandResult, err error) {
	defer m.recording(ctx, "runtime.start", &err)()
	doc, backend, err := m.profileBackend(ctx, name)
	if err != nil {
		return result, err
	}
	materialization, err := m.materialize(ctx, backend, doc.Effective)
	if err != nil {
		return result, CoreError(ErrorInvalidInput, err.Error(), err)
	}
	if err := writeMaterialization(materialization); err != nil {
		return result, CoreError(ErrorRuntime, err.Error(), err)
	}
	spec, err := backend.LaunchSpec(doc.Effective, materialization)
	if err != nil {
		return result, CoreError(ErrorInvalidInput, err.Error(), err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.runtimes[backend.Name()]; ok {
		if _, err := current.process.Stop(ctx); err != nil {
			return result, mapRuntimeError(err)
		}
	}
	process := m.factory(spec)
	result, err = process.Start(ctx)
	if err != nil {
		delete(m.runtimes, backend.Name())
		return result, mapRuntimeError(err)
	}
	m.runtimes[backend.Name()] = runtimeSlot{process: process, profile: name}
	activate(ctx, backend, doc.Effective, &result)
	return result, nil
}

// activate runs the backend's optional post-start step, which for a router-style
// engine is what makes the started process serve this profile rather than sit
// idle until the first request.
//
// A failure here is reported but does not fail the start: the process is up and
// healthy, and an engine that could not be told which model to load will still
// load it on demand. Turning that into a start failure would stop a runtime
// that actually works.
func activate(ctx context.Context, backend backends.Backend, p profiles.Profile, result *coreruntime.CommandResult) {
	activator, ok := backend.(backends.RuntimeActivator)
	if !ok {
		return
	}
	if err := activator.ActivateRuntime(ctx, p); err != nil {
		result.Stderr = strings.TrimSpace(result.Stderr + "\nactivate " + p.Name + ": " + err.Error())
	}
}

// StopRuntime stops the backend slot selected by profile.
func (m *Manager) StopRuntime(ctx context.Context, name string) (result coreruntime.CommandResult, err error) {
	defer m.recording(ctx, "runtime.stop", &err)()
	_, backend, err := m.profileBackend(ctx, name)
	if err != nil {
		return result, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	slot, ok := m.runtimes[backend.Name()]
	if !ok || slot.profile != name {
		return coreruntime.CommandResult{Action: "stop"}, nil
	}
	result, err = slot.process.Stop(ctx)
	if err == nil {
		delete(m.runtimes, backend.Name())
	}
	return result, mapRuntimeError(err)
}

// RuntimeStatus reports the selected backend slot.
func (m *Manager) RuntimeStatus(ctx context.Context, name string) (coreruntime.Status, error) {
	_, backend, err := m.profileBackend(ctx, name)
	if err != nil {
		return coreruntime.Status{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	slot, ok := m.runtimes[backend.Name()]
	if !ok || slot.profile != name {
		return coreruntime.Status{State: coreruntime.Stopped, CheckedAt: time.Now().UTC()}, nil
	}
	status, err := slot.process.Status(ctx)
	return status, mapRuntimeError(err)
}

// StopAllRuntimes stops every active backend slot during daemon shutdown.
func (m *Manager) StopAllRuntimes(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []error
	for name, slot := range m.runtimes {
		if _, err := slot.process.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop backend %q runtime: %w", name, err))
			continue
		}
		delete(m.runtimes, name)
	}
	return errors.Join(errs...)
}

// ResolveProfileModel resolves and plans the selected profile through its
// backend.
func (m *Manager) ResolveProfileModel(ctx context.Context, name string) (backends.ResolvedModel, backends.ArtifactPlan, error) {
	doc, backend, err := m.profileBackend(ctx, name)
	if err != nil {
		return backends.ResolvedModel{}, backends.ArtifactPlan{}, err
	}
	resolved, err := backend.Resolve(ctx, doc.Effective)
	if err != nil {
		return resolved, backends.ArtifactPlan{}, CoreError(ErrorInvalidInput, err.Error(), err)
	}
	plan, err := backend.Plan(resolved)
	if err != nil {
		return resolved, plan, CoreError(ErrorInvalidInput, err.Error(), err)
	}
	return resolved, plan, nil
}

// StartDownload resolves a profile then submits its neutral plan.
func (m *Manager) StartDownload(ctx context.Context, name string, force bool) (job modeldownload.Job, err error) {
	defer m.recording(ctx, "download.start", &err)()
	if m.downloads == nil {
		return job, Errorf(ErrorInvalidInput, "downloads are not configured")
	}
	_, plan, err := m.ResolveProfileModel(ctx, name)
	if err != nil {
		return job, err
	}
	doc, _, profileErr := m.profileBackend(ctx, name)
	if profileErr != nil {
		return job, profileErr
	}
	job, err = m.downloads.Start(ctx, modeldownload.Request{
		Plan: plan, Force: force, Backend: doc.Effective.Backend, Profile: name,
	})
	return job, mapDownloadError(err)
}

func (m *Manager) GetDownload(ctx context.Context, id string) (modeldownload.Job, error) {
	if m.downloads == nil {
		return modeldownload.Job{}, Errorf(ErrorInvalidInput, "downloads are not configured")
	}
	job, err := m.downloads.Get(ctx, id)
	return job, mapDownloadError(err)
}

func (m *Manager) CancelDownload(ctx context.Context, id string) (modeldownload.Job, error) {
	if m.downloads == nil {
		return modeldownload.Job{}, Errorf(ErrorInvalidInput, "downloads are not configured")
	}
	job, err := m.downloads.Cancel(ctx, id)
	return job, mapDownloadError(err)
}

func (m *Manager) Signals(ctx context.Context) (signals.Snapshot, error) {
	if m.signals == nil {
		return signals.Snapshot{}, Errorf(ErrorInvalidInput, "signals are not configured")
	}
	snapshot, err := m.signals.Snapshot(ctx)
	if err != nil {
		return snapshot, CoreError(ErrorRuntime, err.Error(), err)
	}
	return snapshot, nil
}

func (m *Manager) Events() *EventStore { return m.events }

func (m *Manager) profileBackend(ctx context.Context, name string) (profiles.ProfileDocument, backends.Backend, error) {
	doc, err := m.GetProfile(ctx, name)
	if err != nil {
		return doc, nil, err
	}
	backend, err := m.Backend(doc.Effective.Backend)
	return doc, backend, err
}

type batchMaterializer interface {
	MaterializeProfiles([]profiles.Profile) (backends.Materialization, error)
}

func (m *Manager) materialize(ctx context.Context, backend backends.Backend, selected profiles.Profile) (backends.Materialization, error) {
	batch, ok := backend.(batchMaterializer)
	if !ok {
		return backend.Materialize(selected)
	}
	summaries, err := m.profiles.List(ctx)
	if err != nil {
		return backends.Materialization{}, err
	}
	items := make([]profiles.Profile, 0, len(summaries))
	for _, summary := range summaries {
		if summary.Backend != backend.Name() {
			continue
		}
		doc, err := m.profiles.Get(ctx, summary.Name)
		if err != nil {
			return backends.Materialization{}, err
		}
		items = append(items, doc.Effective)
	}
	return batch.MaterializeProfiles(items)
}

func writeMaterialization(materialization backends.Materialization) error {
	for _, generated := range materialization.Files {
		if generated.Path == "" {
			return fmt.Errorf("generated file path is empty")
		}
		if err := os.MkdirAll(filepath.Dir(generated.Path), 0o700); err != nil {
			return err
		}
		if err := filedoc.RejectSymlinkAncestors(filepath.Dir(generated.Path)); err != nil {
			return err
		}
		if err := filedoc.RejectSymlink(generated.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		mode := os.FileMode(generated.Mode)
		if mode == 0 {
			mode = 0o600
		}
		if _, err := filedoc.WriteFile(generated.Path, string(generated.Content), filedoc.WriteOptions{Perm: mode}); err != nil {
			return err
		}
	}
	return nil
}

// audit starts the clock and returns the deferred recorder, so a caller cannot
// register the audit without also timing it. Use as: defer m.audit(...)().
func (m *Manager) recording(ctx context.Context, action string, err *error) func() {
	start := time.Now()
	return func() { m.record(ctx, action, start, *err) }
}

func (m *Manager) record(ctx context.Context, action string, start time.Time, err error) {
	m.audit.Record(ctx, AuditEvent{
		Protocol: "core", Action: action, Success: err == nil,
		ErrorKind: Kind(err), Duration: time.Since(start),
	})
}

var profileErrorKinds = []SentinelKind{
	{Target: profiles.ErrInvalid, Kind: ErrorInvalidInput},
	{Target: profiles.ErrTooLarge, Kind: ErrorInvalidInput},
	{Target: profiles.ErrExists, Kind: ErrorConflict},
	{Target: os.ErrNotExist, Kind: ErrorNotFound},
}

func mapProfileError(err error) error { return MapSentinel(err, profileErrorKinds) }

var downloadErrorKinds = []SentinelKind{
	{Target: modeldownload.ErrInvalidInput, Kind: ErrorInvalidInput},
	{Target: modeldownload.ErrNotFound, Kind: ErrorNotFound},
	{Target: modeldownload.ErrConflict, Kind: ErrorConflict},
}

func mapDownloadError(err error) error { return MapSentinel(err, downloadErrorKinds) }

func mapRuntimeError(err error) error {
	if err == nil {
		return nil
	}
	switch coreruntime.Kind(err) {
	case coreruntime.ErrorInvalidInput:
		return CoreError(ErrorInvalidInput, err.Error(), err)
	case coreruntime.ErrorTimeout:
		return CoreError(ErrorTimeout, err.Error(), err)
	default:
		return CoreError(ErrorRuntime, err.Error(), err)
	}
}
