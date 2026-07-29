package test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"inferencerig/backends"
	"inferencerig/backends/llamacpp"
	"inferencerig/backends/mlx"
	"inferencerig/config"
	"inferencerig/core/control"
	"inferencerig/core/profiles"
	"inferencerig/core/rpc"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
	coreruntime "inferencerig/core/runtime"
)

type integrationRuntime struct{ state coreruntime.State }

func (r *integrationRuntime) Start(context.Context) (coreruntime.CommandResult, error) {
	r.state = coreruntime.Running
	return coreruntime.CommandResult{Action: "start"}, nil
}

func (r *integrationRuntime) Stop(context.Context) (coreruntime.CommandResult, error) {
	r.state = coreruntime.Stopped
	return coreruntime.CommandResult{Action: "stop"}, nil
}

func (r *integrationRuntime) Status(context.Context) (coreruntime.Status, error) {
	return coreruntime.Status{State: r.state, CheckedAt: time.Now().UTC()}, nil
}

func (*integrationRuntime) Recover(context.Context) (bool, error) { return false, nil }

//nolint:gocognit,gocyclo,funlen // One stack test proves both real backends use the same RPC path.
func TestCanonicalControlStackWithBothBackends(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)
	registry := backends.NewRegistry()
	iniPath := filepath.Join(home, "generated", "single", "models.ini")
	if err := registry.Register(llamacpp.New(llamacpp.Options{
		GeneratedININPath: iniPath, ModelStorageDir: filepath.Join(home, "single-models"),
		PIDDir: filepath.Join(home, "run"), Executable: "single-server",
	})); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(mlx.New(mlx.Options{
		ModelStorageDir: filepath.Join(home, "snapshots"), PIDDir: filepath.Join(home, "run"),
		Executable: "snapshot-server",
	})); err != nil {
		t.Fatal(err)
	}
	store := profiles.NewFileStore(filepath.Join(home, "profiles"), 0, registry.BackendLookup())
	manager := control.NewManager(control.Dependencies{
		Registry: registry, Profiles: store,
		RuntimeFactory: func(coreruntime.LaunchSpec) control.Runtime {
			return &integrationRuntime{state: coreruntime.Stopped}
		},
	})
	path, handler := rpc.ControlHandler(rpc.NewControlService(manager))
	server, err := rpc.NewServer(path, handler)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.HTTP.Serve(server.Listener) }()
	t.Cleanup(func() { _ = server.HTTP.Close() })
	transport, err := rpc.ControlTransport(server.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	client := controlv1connect.NewControlServiceClient(
		&http.Client{Transport: transport, Timeout: 3 * time.Second}, "http://unix",
	)
	ctx := context.Background()
	putProfile(t, ctx, client, "single", llamacpp.Name, "/models/demo.gguf", 8081)
	putProfile(t, ctx, client, "snapshot", mlx.Name, "local-snapshot", 8082)
	listed, err := client.ListBackends(ctx, &controlv1.ListBackendsRequest{})
	if err != nil || len(listed.GetBackends()) != 2 {
		t.Fatalf("backends = %#v, err = %v", listed, err)
	}
	assertRuntime(t, ctx, client, "single")
	assertRuntime(t, ctx, client, "snapshot")
	single, err := client.ResolveProfileModel(ctx, &controlv1.ResolveProfileModelRequest{Profile: "single"})
	if err != nil || single.GetPlan().GetMultiFile() {
		t.Fatalf("single-file plan = %#v, err = %v", single, err)
	}
	snapshot, err := client.ResolveProfileModel(ctx, &controlv1.ResolveProfileModelRequest{Profile: "snapshot"})
	if err != nil || !snapshot.GetPlan().GetMultiFile() {
		t.Fatalf("snapshot plan = %#v, err = %v", snapshot, err)
	}
	generated, err := os.ReadFile(iniPath)
	if err != nil || !strings.Contains(string(generated), "[single]") || strings.Contains(string(generated), "[snapshot]") {
		t.Fatalf("generated config = %q, err = %v", generated, err)
	}
}

func putProfile(
	t *testing.T,
	ctx context.Context,
	client controlv1connect.ControlServiceClient,
	name, backend, source string,
	port int,
) {
	t.Helper()
	yaml := "version: 1\nname: " + name + "\nbackend: " + backend +
		"\nmodel:\n  source: " + source + "\nlisten:\n  host: 127.0.0.1\n  port: " + fmt.Sprint(port) + "\n"
	if _, err := client.PutProfile(ctx, &controlv1.PutProfileRequest{
		Name: name, ProfileYaml: yaml, CreateOnly: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func assertRuntime(t *testing.T, ctx context.Context, client controlv1connect.ControlServiceClient, profile string) {
	t.Helper()
	started, err := client.StartRuntime(ctx, &controlv1.StartRuntimeRequest{Profile: profile})
	if err != nil || started.GetStatus().GetState() != string(coreruntime.Running) {
		t.Fatalf("start %s = %#v, err = %v", profile, started, err)
	}
}
