//go:build live

package live

import (
	"context"
	"net"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"testing"
	"time"

	"inferencerig/backends/llamacpp"
	"inferencerig/backends/mlx"
	"inferencerig/core/profiles"
	coreruntime "inferencerig/core/runtime"
)

func TestSingleFileBackendHardware(t *testing.T) {
	executable := os.Getenv("INFERENCERIG_LIVE_LLAMACPP_BIN")
	model := os.Getenv("INFERENCERIG_LIVE_LLAMACPP_MODEL")
	if executable == "" || model == "" {
		t.Skip("set INFERENCERIG_LIVE_LLAMACPP_BIN and INFERENCERIG_LIVE_LLAMACPP_MODEL")
	}
	root := t.TempDir()
	profile := profiles.Profile{
		Version: 1, Name: "live-single", Backend: llamacpp.Name,
		Model:  profiles.ModelSpec{Source: model},
		Listen: profiles.ListenSpec{Host: "127.0.0.1", Port: freePort(t)},
	}
	backend := llamacpp.New(llamacpp.Options{
		Executable: executable, GeneratedININPath: filepath.Join(root, "models.ini"),
		ModelStorageDir: filepath.Dir(model), PIDDir: filepath.Join(root, "run"),
	})
	materialization, err := backend.Materialize(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(materialization.Files[0].Path, materialization.Files[0].Content, 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := backend.LaunchSpec(profile, materialization)
	runBackend(t, spec, err)
}

func TestDirectoryBackendAppleSiliconHardware(t *testing.T) {
	if goruntime.GOOS != "darwin" || goruntime.GOARCH != "arm64" {
		t.Skip("directory backend hardware validation requires Apple Silicon")
	}
	executable := os.Getenv("INFERENCERIG_LIVE_MLX_PYTHON")
	model := os.Getenv("INFERENCERIG_LIVE_MLX_MODEL")
	if executable == "" || model == "" {
		t.Skip("set INFERENCERIG_LIVE_MLX_PYTHON and INFERENCERIG_LIVE_MLX_MODEL")
	}
	root := t.TempDir()
	profile := profiles.Profile{
		Version: 1, Name: "live-directory", Backend: mlx.Name,
		Model:  profiles.ModelSpec{Source: model},
		Listen: profiles.ListenSpec{Host: "127.0.0.1", Port: freePort(t)},
	}
	backend := mlx.New(mlx.Options{Executable: executable, PIDDir: filepath.Join(root, "run")})
	materialization, err := backend.Materialize(profile)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := backend.LaunchSpec(profile, materialization)
	runBackend(t, spec, err)
}

func runBackend(
	t *testing.T,
	spec coreruntime.LaunchSpec,
	err error,
) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	spec.ReadinessTimeout = 3 * time.Minute
	supervisor := coreruntime.NewSupervisor(spec)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if _, err := supervisor.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = supervisor.Stop(context.Background()) })
	status, err := supervisor.Status(ctx)
	if err != nil || status.State != coreruntime.Running {
		t.Fatalf("status = %#v, err = %v", status, err)
	}
	if _, err := supervisor.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	_, raw, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatal(err)
	}
	return port
}
