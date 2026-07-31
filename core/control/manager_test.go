package control

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"inferencerig/backends"
	"inferencerig/backends/backendtest"
	"inferencerig/core/modeldownload"
	"inferencerig/core/profiles"
	coreruntime "inferencerig/core/runtime"
)

type testBackend struct {
	*backendtest.Fake
	target string
}

func (b *testBackend) Materialize(profiles.Profile) (backends.Materialization, error) {
	return backends.Materialization{Summary: "test command"}, nil
}

func (b *testBackend) Plan(r backends.ResolvedModel) (backends.ArtifactPlan, error) {
	plan, err := b.Fake.Plan(r)
	if err != nil {
		return plan, err
	}
	plan.TargetRoot = b.target
	plan.Items[0].TargetPath = b.target
	plan.Items[0].SizeBytes, plan.TotalBytes = 0, 0
	return plan, nil
}

type fakeRuntime struct {
	state         coreruntime.State
	starts, stops int
}

func (r *fakeRuntime) Start(context.Context) (coreruntime.CommandResult, error) {
	r.starts++
	r.state = coreruntime.Running
	return coreruntime.CommandResult{Action: "start"}, nil
}
func (r *fakeRuntime) Stop(context.Context) (coreruntime.CommandResult, error) {
	r.stops++
	r.state = coreruntime.Stopped
	return coreruntime.CommandResult{Action: "stop"}, nil
}
func (r *fakeRuntime) Status(context.Context) (coreruntime.Status, error) {
	return coreruntime.Status{State: r.state, CheckedAt: time.Now()}, nil
}
func (r *fakeRuntime) Recover(context.Context) (bool, error) { return false, nil }

// A catalog entry is a repository plus a file inside it. Folding the two into
// one field left the backend resolving a bare filename, which fetched a URI
// with no scheme, so both halves must reach the backend intact.
func TestResolveModelCarriesTheVariantIntoTheBackend(t *testing.T) {
	registry := backends.NewRegistry()
	if err := registry.Register(backendtest.New("test")); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Dependencies{
		Registry: registry,
		Profiles: profiles.NewFileStore(t.TempDir(), 0, registry.BackendLookup()),
	})
	resolved, _, err := manager.ResolveModel(
		context.Background(), "test", "https://example.test/owner/repo", "sub/model.gguf",
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Source != "https://example.test/owner/repo" || resolved.Reference != "sub/model.gguf" {
		t.Fatalf("resolved = %#v", resolved)
	}
}

//nolint:gocognit,gocyclo // One integration scenario verifies the manager's coordinated lifecycle.
func TestManagerControlsProfilesRuntimeInstallAndDownloads(t *testing.T) {
	body := []byte("model")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()

	registry := backends.NewRegistry()
	backend := &testBackend{Fake: backendtest.New("test"), target: filepath.Join(t.TempDir(), "model.bin")}
	if err := registry.Register(backend); err != nil {
		t.Fatal(err)
	}
	store := profiles.NewFileStore(t.TempDir(), 0, registry.BackendLookup())
	var runtimes []*fakeRuntime
	manager := NewManager(Dependencies{
		Registry: registry, Profiles: store,
		Downloads: modeldownload.New(modeldownload.Options{HTTPClient: server.Client()}),
		RuntimeFactory: func(coreruntime.LaunchSpec) Runtime {
			runtime := &fakeRuntime{}
			runtimes = append(runtimes, runtime)
			return runtime
		},
	})
	ctx := context.Background()
	for _, name := range []string{"one", "two"} {
		if _, err := manager.PutProfile(ctx, name, profileYAML(name, server.URL), true); err != nil {
			t.Fatal(err)
		}
	}
	if profiles, err := manager.ListProfiles(ctx); err != nil || len(profiles) != 2 {
		t.Fatalf("profiles = %#v, err = %v", profiles, err)
	}
	if _, err := manager.StartRuntime(ctx, "one", false); err != nil {
		t.Fatal(err)
	}
	// The backend serves one profile at a time, so switching is the caller's
	// decision to make: without replace the running engine stays up.
	if _, err := manager.StartRuntime(ctx, "two", false); Kind(err) != ErrorConflict {
		t.Fatalf("start without replace = %v, want a conflict", err)
	}
	if len(runtimes) != 1 || runtimes[0].stops != 0 {
		t.Fatalf("refused start touched the running runtime: %#v", runtimes)
	}
	if _, err := manager.StartRuntime(ctx, "two", true); err != nil {
		t.Fatal(err)
	}
	if len(runtimes) != 2 || runtimes[0].stops != 1 {
		t.Fatalf("runtimes = %#v", runtimes)
	}
	status, err := manager.RuntimeStatus(ctx, "two")
	if err != nil || status.State != coreruntime.Running {
		t.Fatalf("status = %#v, err = %v", status, err)
	}
	if status, err := manager.RuntimeStatus(ctx, "one"); err != nil || status.State != coreruntime.Stopped {
		t.Fatalf("inactive profile status = %#v, err = %v", status, err)
	}
	if _, err := manager.StopRuntime(ctx, "one"); err != nil || runtimes[1].stops != 0 {
		t.Fatalf("stopping inactive profile affected active runtime: stops=%d err=%v", runtimes[1].stops, err)
	}
	installed, err := manager.InstallBackend(ctx, "test", backends.InstallOptions{})
	if err != nil || !installed.Changed {
		t.Fatalf("install = %#v, err = %v", installed, err)
	}
	installStatus, err := manager.BackendInstallStatus(ctx, "test")
	if err != nil || !installStatus.Installed || installStatus.Path == "" {
		t.Fatalf("install status = %#v, err = %v", installStatus, err)
	}
	job, err := manager.StartDownload(ctx, "one", false)
	if err != nil {
		t.Fatal(err)
	}
	job = waitDownload(t, manager, job.ID)
	if job.State != modeldownload.StateCompleted {
		t.Fatalf("job = %#v", job)
	}
	data, err := os.ReadFile(backend.target)
	if err != nil || string(data) != string(body) {
		t.Fatalf("data = %q, err = %v", data, err)
	}
	if len(manager.Events().List()) < 5 {
		t.Fatalf("events = %#v", manager.Events().List())
	}
}

func profileYAML(name, source string) string {
	return profileYAMLOnPort(name, source, 8080)
}

func profileYAMLOnPort(name, source string, port int) string {
	return "version: 1\nname: " + name + "\nbackend: test\nmodel:\n  source: " + source +
		"\nlisten:\n  host: 127.0.0.1\n  port: " + strconv.Itoa(port) + "\n"
}

func waitDownload(t *testing.T, manager *Manager, id string) modeldownload.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := manager.GetDownload(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if job.State == modeldownload.StateCompleted || job.State == modeldownload.StateFailed {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("download did not finish")
	return modeldownload.Job{}
}

// recording must start the clock when the defer is registered, not when it
// runs, so the recorded duration covers the operation rather than being zero.
func TestRecordingMeasuresOperationDuration(t *testing.T) {
	store := NewEventStore(DefaultEventLimit)
	m := &Manager{events: store, audit: MultiAuditSink{store}}
	_ = func() (err error) {
		defer m.recording(context.Background(), "probe.action", &err)()
		time.Sleep(2 * time.Millisecond)
		return nil
	}()
	events := store.List()
	if len(events) != 1 || events[0].Action != "probe.action" || !events[0].Success {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Duration == "" || events[0].Duration == "0s" {
		t.Fatalf("duration not measured: %q", events[0].Duration)
	}
}

// A failing operation records the mapped error kind.
func TestRecordingCapturesErrorKind(t *testing.T) {
	store := NewEventStore(DefaultEventLimit)
	m := &Manager{events: store, audit: MultiAuditSink{store}}
	_ = func() (err error) {
		defer m.recording(context.Background(), "probe.fail", &err)()
		return Errorf(ErrorNotFound, "nope")
	}()
	events := store.List()
	if len(events) != 1 || events[0].Success || events[0].ErrorKind != ErrorNotFound {
		t.Fatalf("events = %#v", events)
	}
}

// activatingBackend records the post-start activation the manager drives
// through the optional backends.RuntimeActivator facet.
type activatingBackend struct {
	*backendtest.Fake
	activated []string
	err       error
}

func (b *activatingBackend) Materialize(profiles.Profile) (backends.Materialization, error) {
	return backends.Materialization{Summary: "test command"}, nil
}

func (b *activatingBackend) ActivateRuntime(_ context.Context, p profiles.Profile) error {
	b.activated = append(b.activated, p.Name)
	return b.err
}

func startWithActivator(t *testing.T, backend backends.Backend) (coreruntime.CommandResult, error) {
	t.Helper()
	// Materialized files carry relative paths, so they land in the working
	// directory: without this the suite writes generated config into the
	// package source tree.
	t.Chdir(t.TempDir())
	registry := backends.NewRegistry()
	if err := registry.Register(backend); err != nil {
		t.Fatal(err)
	}
	store := profiles.NewFileStore(t.TempDir(), 0, registry.BackendLookup())
	manager := NewManager(Dependencies{
		Registry: registry, Profiles: store,
		RuntimeFactory: func(coreruntime.LaunchSpec) Runtime { return &fakeRuntime{} },
	})
	ctx := context.Background()
	if _, err := manager.PutProfile(ctx, "one", profileYAML("one", "https://example.test/m"), true); err != nil {
		t.Fatal(err)
	}
	return manager.StartRuntime(ctx, "one", false)
}

// A router-style engine starts serving no model at all, so the manager must ask
// the backend to activate the started profile rather than leaving it idle until
// the first request arrives.
func TestStartRuntimeActivatesTheStartedProfile(t *testing.T) {
	backend := &activatingBackend{Fake: backendtest.New("test")}
	if _, err := startWithActivator(t, backend); err != nil {
		t.Fatalf("StartRuntime: %v", err)
	}
	if len(backend.activated) != 1 || backend.activated[0] != "one" {
		t.Fatalf("activated = %#v, want [one]", backend.activated)
	}
}

// The process is up and healthy whether or not activation lands, and an engine
// that was not told which model to load still loads it on demand. Failing the
// start would stop a runtime that actually works.
func TestStartRuntimeReportsActivationFailureWithoutFailingTheStart(t *testing.T) {
	backend := &activatingBackend{Fake: backendtest.New("test"), err: errors.New("router refused")}
	result, err := startWithActivator(t, backend)
	if err != nil {
		t.Fatalf("activation failure failed the start: %v", err)
	}
	if !strings.Contains(result.Stderr, "router refused") {
		t.Fatalf("result.Stderr = %q, want the activation failure reported", result.Stderr)
	}
}

// A backend whose engine loads its model at startup implements no activation
// facet, and must start exactly as before.
func TestStartRuntimeWithoutActivatorIsUnchanged(t *testing.T) {
	if _, err := startWithActivator(t, backendtest.New("test")); err != nil {
		t.Fatalf("StartRuntime without activator: %v", err)
	}
}
