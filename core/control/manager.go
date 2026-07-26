package control

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"inferencerig/backends"
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
	Recover(context.Context) (bool, error)
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
}

type runtimeSlot struct {
	process Runtime
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
		runtimes: map[string]runtimeSlot{},
	}
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
	start := time.Now()
	defer func() { m.record(ctx, "profile.put", start, err) }()
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
	start := time.Now()
	defer func() { m.record(ctx, "profile.delete", start, err) }()
	if _, stopErr := m.StopRuntime(ctx, name); stopErr != nil && Kind(stopErr) != ErrorNotFound {
		return result, stopErr
	}
	result, err = m.profiles.Delete(ctx, name)
	return result, mapProfileError(err)
}

// InstallBackend runs a backend's managed installer.
func (m *Manager) InstallBackend(ctx context.Context, name string, opts backends.InstallOptions) (result backends.InstallResult, err error) {
	start := time.Now()
	defer func() { m.record(ctx, "backend.install", start, err) }()
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

// StartRuntime materializes all relevant profiles and starts the selected
// backend through the shared supervisor. One runtime slot per backend also
// implements single-active-profile switching without engine-name branches.
func (m *Manager) StartRuntime(ctx context.Context, name string) (result coreruntime.CommandResult, err error) {
	start := time.Now()
	defer func() { m.record(ctx, "runtime.start", start, err) }()
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
	m.runtimes[backend.Name()] = runtimeSlot{process: process}
	return result, nil
}

// StopRuntime stops the backend slot selected by profile.
func (m *Manager) StopRuntime(ctx context.Context, name string) (result coreruntime.CommandResult, err error) {
	start := time.Now()
	defer func() { m.record(ctx, "runtime.stop", start, err) }()
	_, backend, err := m.profileBackend(ctx, name)
	if err != nil {
		return result, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	slot, ok := m.runtimes[backend.Name()]
	if !ok {
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
	if !ok {
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
	start := time.Now()
	defer func() { m.record(ctx, "download.start", start, err) }()
	if m.downloads == nil {
		return job, Errorf(ErrorInvalidInput, "downloads are not configured")
	}
	_, plan, err := m.ResolveProfileModel(ctx, name)
	if err != nil {
		return job, err
	}
	job, err = m.downloads.Start(ctx, modeldownload.Request{Plan: plan, Force: force})
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
