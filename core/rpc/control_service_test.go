package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"

	"inferencerig/backends"
	"inferencerig/backends/backendtest"
	"inferencerig/config"
	"inferencerig/core/control"
	"inferencerig/core/modeldownload"
	"inferencerig/core/profiles"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
	coreruntime "inferencerig/core/runtime"
	"inferencerig/core/signals"
)

type rpcBackend struct {
	*backendtest.Fake
	target string
}

func (b *rpcBackend) Materialize(profiles.Profile) (backends.Materialization, error) {
	return backends.Materialization{Summary: "command"}, nil
}

func (b *rpcBackend) Plan(r backends.ResolvedModel) (backends.ArtifactPlan, error) {
	plan, err := b.Fake.Plan(r)
	if err != nil {
		return plan, err
	}
	plan.TargetRoot, plan.TotalBytes = b.target, 0
	plan.Items[0].TargetPath, plan.Items[0].SizeBytes = b.target, 0
	return plan, nil
}

type rpcRuntime struct{ state coreruntime.State }

func (r *rpcRuntime) Start(context.Context) (coreruntime.CommandResult, error) {
	r.state = coreruntime.Running
	return coreruntime.CommandResult{Action: "start"}, nil
}
func (r *rpcRuntime) Stop(context.Context) (coreruntime.CommandResult, error) {
	r.state = coreruntime.Stopped
	return coreruntime.CommandResult{Action: "stop"}, nil
}
func (r *rpcRuntime) Status(context.Context) (coreruntime.Status, error) {
	return coreruntime.Status{State: r.state, CheckedAt: time.Now().UTC()}, nil
}
func (r *rpcRuntime) Recover(context.Context) (bool, error) { return false, nil }

type rpcSignals struct{}

func (rpcSignals) Snapshot(context.Context) (signals.Snapshot, error) {
	return signals.Snapshot{
		CapturedAt: "now", Memory: signals.MemoryStats{TotalBytes: 16, AvailableBytes: 8},
		CPU: signals.CPUStats{LogicalCores: 4},
	}, nil
}

//nolint:gocognit,gocyclo,funlen // One end-to-end scenario verifies the canonical control surface.
func TestCanonicalControlServiceOverUnixSocket(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir())
	artifact := []byte("model")
	downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(artifact)
	}))
	defer downloadServer.Close()

	registry := backends.NewRegistry()
	backend := &rpcBackend{Fake: backendtest.New("test"), target: filepath.Join(t.TempDir(), "model.bin")}
	if err := registry.Register(backend); err != nil {
		t.Fatal(err)
	}
	store := profiles.NewFileStore(t.TempDir(), 0, registry.BackendLookup())
	manager := control.NewManager(control.Dependencies{
		Registry: registry, Profiles: store, Signals: rpcSignals{},
		Downloads:      modeldownload.New(modeldownload.Options{HTTPClient: downloadServer.Client()}),
		RuntimeFactory: func(coreruntime.LaunchSpec) control.Runtime { return &rpcRuntime{} },
	})
	path, handler := ControlHandler(NewControlService(manager))
	server, err := NewServer(path, handler)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.HTTP.Serve(server.Listener) }()
	t.Cleanup(func() { _ = server.HTTP.Close() })
	transport, err := ControlTransport(server.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	client := controlv1connect.NewControlServiceClient(
		&http.Client{Transport: transport, Timeout: 3 * time.Second}, "http://unix",
	)
	ctx := context.Background()
	health, err := client.Health(ctx, &controlv1.HealthRequest{})
	if err != nil || !health.GetOk() || health.GetService() != ServiceName {
		t.Fatalf("health = %#v, err = %v", health, err)
	}
	yaml := "version: 1\nname: demo\nbackend: test\nmodel:\n  source: " + downloadServer.URL +
		"\nlisten:\n  host: 127.0.0.1\n  port: 8080\n"
	put, err := client.PutProfile(ctx, &controlv1.PutProfileRequest{
		Name: "demo", ProfileYaml: yaml, CreateOnly: true,
	})
	if err != nil || put.GetProfile().GetBackend() != "test" {
		t.Fatalf("put = %#v, err = %v", put, err)
	}
	listed, err := client.ListBackends(ctx, &controlv1.ListBackendsRequest{})
	if err != nil || len(listed.GetBackends()) != 1 {
		t.Fatalf("backends = %#v, err = %v", listed, err)
	}
	started, err := client.StartRuntime(ctx, &controlv1.StartRuntimeRequest{Profile: "demo"})
	if err != nil || started.GetStatus().GetState() != string(coreruntime.Running) {
		t.Fatalf("started = %#v, err = %v", started, err)
	}
	resolved, err := client.ResolveProfileModel(ctx, &controlv1.ResolveProfileModelRequest{Profile: "demo"})
	if err != nil || resolved.GetPlan().GetTargetRoot() != backend.target {
		t.Fatalf("resolved = %#v, err = %v", resolved, err)
	}
	download, err := client.StartModelDownload(ctx, &controlv1.StartModelDownloadRequest{Profile: "demo"})
	if err != nil || download.GetDownload().GetId() == "" {
		t.Fatalf("download = %#v, err = %v", download, err)
	}
	downloadID := download.GetDownload().GetId()
	downloadState := download.GetDownload().GetState()
	deadline := time.Now().Add(3 * time.Second)
	for downloadState != string(modeldownload.StateCompleted) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		status, err := client.GetModelDownload(ctx, &controlv1.GetModelDownloadRequest{Id: downloadID})
		if err != nil {
			t.Fatal(err)
		}
		downloadState = status.GetDownload().GetState()
	}
	if downloadState != string(modeldownload.StateCompleted) {
		t.Fatalf("download state = %q", downloadState)
	}
	signalsResponse, err := client.GetSignals(ctx, &controlv1.GetSignalsRequest{})
	if err != nil || signalsResponse.GetSignals().GetLogicalCpuCores() != 4 {
		t.Fatalf("signals = %#v, err = %v", signalsResponse, err)
	}
	events, err := client.ListEvents(ctx, &controlv1.ListEventsRequest{})
	if err != nil || len(events.GetEvents()) == 0 {
		t.Fatalf("events = %#v, err = %v", events, err)
	}
}

func TestCanonicalControlServiceMapsValidationErrors(t *testing.T) {
	registry := backends.NewRegistry()
	backend := backendtest.New("test")
	if err := registry.Register(backend); err != nil {
		t.Fatal(err)
	}
	manager := control.NewManager(control.Dependencies{
		Registry: registry,
		Profiles: profiles.NewFileStore(t.TempDir(), 0, registry.BackendLookup()),
	})
	_, err := NewControlService(manager).GetProfile(context.Background(), &controlv1.GetProfileRequest{})
	if connect.CodeOf(err) != connect.CodeInvalidArgument || ErrorKindFromRPC(err) != control.ErrorInvalidInput {
		t.Fatalf("err = %v, code = %v, kind = %v", err, connect.CodeOf(err), ErrorKindFromRPC(err))
	}
}
