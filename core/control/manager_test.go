package control

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	if _, err := manager.StartRuntime(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartRuntime(ctx, "two"); err != nil {
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
	return "version: 1\nname: " + name + "\nbackend: test\nmodel:\n  source: " + source +
		"\nlisten:\n  host: 127.0.0.1\n  port: 8080\n"
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
