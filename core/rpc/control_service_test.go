package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"

	"inferencerig/backends"
	"inferencerig/backends/backendtest"
	"inferencerig/config"
	"inferencerig/core/configstore"
	"inferencerig/core/control"
	"inferencerig/core/modelcatalog"
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

func (b *rpcBackend) Capabilities() backends.Capabilities {
	capabilities := b.Fake.Capabilities()
	capabilities.ParameterIntrospection = true
	return capabilities
}

func (b *rpcBackend) Parameters(context.Context) ([]backends.Parameter, error) {
	return []backends.Parameter{{Name: "engine_args.test"}}, nil
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

type rpcCatalog struct{}

func (rpcCatalog) Search(_ context.Context, req modelcatalog.SearchRequest, _ modelcatalog.CatalogPolicy) (modelcatalog.Result, error) {
	return modelcatalog.Result{Models: []modelcatalog.Model{{
		ID: "owner/repo", URL: "https://example.test/owner/repo",
		Variants: []modelcatalog.Variant{{Name: req.Backend + "-model"}},
	}}}, nil
}

func (rpcCatalog) Subscribe() (<-chan modelcatalog.RefreshEvent, func()) {
	events := make(chan modelcatalog.RefreshEvent)
	return events, func() { close(events) }
}

//nolint:gocognit,gocyclo,funlen // One end-to-end scenario verifies the canonical control surface.
func TestCanonicalControlServiceOverUnixSocket(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)
	configPath := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(configPath, []byte("startup_services: [control]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
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
		Catalog:        rpcCatalog{},
		Config:         configstore.NewFileStore(configPath, 0),
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
	if _, err := client.InstallBackend(ctx, &controlv1.InstallBackendRequest{Backend: "test"}); err != nil {
		t.Fatal(err)
	}
	installStatus, err := client.GetBackendInstallStatus(ctx, &controlv1.GetBackendInstallStatusRequest{Backend: "test"})
	if err != nil || !installStatus.GetInstalled() || installStatus.GetPath() == "" {
		t.Fatalf("install status = %#v, err = %v", installStatus, err)
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
	applied, err := client.ApplyDownloadToProfile(ctx, &controlv1.ApplyDownloadToProfileRequest{
		Profile: "demo", Id: downloadID,
	})
	if err != nil || applied.GetProfile().GetModelSource() != backend.target {
		t.Fatalf("applied = %#v, err = %v", applied, err)
	}
	autostart, err := client.SetProfileAutostart(ctx, &controlv1.SetProfileAutostartRequest{
		Name: "demo", Enabled: true,
	})
	if err != nil || !autostart.GetOk() {
		t.Fatalf("autostart = %#v, err = %v", autostart, err)
	}
	startup, err := client.SetStartupServices(ctx, &controlv1.SetStartupServicesRequest{
		Services: []string{config.StartupServiceControl},
	})
	if err != nil || !startup.GetOk() {
		t.Fatalf("startup = %#v, err = %v", startup, err)
	}
	restarted, err := client.RestartRuntime(ctx, &controlv1.RestartRuntimeRequest{Profile: "demo"})
	if err != nil || restarted.GetStatus().GetState() != string(coreruntime.Running) {
		t.Fatalf("restarted = %#v, err = %v", restarted, err)
	}
	info, err := client.GetInfo(ctx, &controlv1.GetInfoRequest{})
	if err != nil || info.GetProfiles() != 1 || len(info.GetAutostartProfiles()) != 1 {
		t.Fatalf("info = %#v, err = %v", info, err)
	}
	params, err := client.GetBackendParams(ctx, &controlv1.GetBackendParamsRequest{Backend: "test"})
	if err != nil || len(params.GetParams()) != 1 {
		t.Fatalf("params = %#v, err = %v", params, err)
	}
	signalsResponse, err := client.GetSignals(ctx, &controlv1.GetSignalsRequest{})
	if err != nil || signalsResponse.GetSignals().GetLogicalCpuCores() != 4 {
		t.Fatalf("signals = %#v, err = %v", signalsResponse, err)
	}
	events, err := client.ListEvents(ctx, &controlv1.ListEventsRequest{})
	if err != nil || len(events.GetEvents()) == 0 {
		t.Fatalf("events = %#v, err = %v", events, err)
	}
	catalog, err := client.ListModelCatalog(ctx, &controlv1.ListModelCatalogRequest{Backend: "test"})
	if err != nil || len(catalog.GetModels()) != 1 {
		t.Fatalf("catalog = %#v, err = %v", catalog, err)
	}
	local, err := client.ListLocalModels(ctx, &controlv1.ListLocalModelsRequest{Backend: "test"})
	if err != nil || !local.GetOk() {
		t.Fatalf("local = %#v, err = %v", local, err)
	}
	deleted, err := client.DeleteLocalModel(ctx, &controlv1.DeleteLocalModelRequest{
		Backend: "test", Path: "/models/model.bin",
	})
	if err != nil || !deleted.GetOk() {
		t.Fatalf("deleted = %#v, err = %v", deleted, err)
	}
	// A catalog download starts with no profile, because browsing comes before
	// deciding where a model goes. It must still be applicable afterwards.
	catalogDownload, err := client.StartModelDownload(ctx, &controlv1.StartModelDownloadRequest{
		Backend: "test", Reference: downloadServer.URL, VariantReference: "model.bin", Force: true,
	})
	if err != nil || catalogDownload.GetDownload().GetProfile() != "" {
		t.Fatalf("catalog download = %#v, err = %v", catalogDownload, err)
	}
	catalogID := catalogDownload.GetDownload().GetId()
	catalogState := catalogDownload.GetDownload().GetState()
	deadline = time.Now().Add(3 * time.Second)
	for catalogState != string(modeldownload.StateCompleted) &&
		catalogState != string(modeldownload.StateAlreadyDownloaded) &&
		time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		status, statusErr := client.GetModelDownload(ctx, &controlv1.GetModelDownloadRequest{Id: catalogID})
		if statusErr != nil {
			t.Fatal(statusErr)
		}
		catalogState = status.GetDownload().GetState()
	}
	appliedCatalog, err := client.ApplyDownloadToProfile(ctx, &controlv1.ApplyDownloadToProfileRequest{
		Profile: "demo", Id: catalogID,
	})
	if err != nil || appliedCatalog.GetProfile().GetModelSource() != backend.target {
		t.Fatalf("applied catalog download = %#v, err = %v (state %s)", appliedCatalog, err, catalogState)
	}

	cleaned, err := client.CleanupProfile(ctx, &controlv1.CleanupProfileRequest{Name: "demo"})
	if err != nil || !cleaned.GetOk() {
		t.Fatalf("cleaned = %#v, err = %v", cleaned, err)
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
