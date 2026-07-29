package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"inferencerig/config"
	"inferencerig/core/profiles"
	"inferencerig/core/rpc"
	controlv1 "inferencerig/core/rpc/gen/v1"
)

func TestServiceRunsCanonicalControlSocket(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir())
	service, err := NewService()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	client, err := rpc.DialControl(service.Server.SocketPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	health, err := client.Health(context.Background(), &controlv1.HealthRequest{})
	if err != nil || !health.GetOk() {
		t.Fatalf("health = %#v, err = %v", health, err)
	}
	backends, err := client.ListBackends(context.Background(), &controlv1.ListBackendsRequest{})
	if err != nil || len(backends.GetBackends()) != 2 {
		t.Fatalf("backends = %#v, err = %v", backends, err)
	}
	if _, err := os.Stat(service.pidFile.Path()); err != nil {
		t.Fatalf("daemon PID file: %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(service.Server.SocketPath); !os.IsNotExist(err) {
		t.Fatalf("socket remains after shutdown: %v", err)
	}
	if _, err := os.Stat(service.pidFile.Path()); !os.IsNotExist(err) {
		t.Fatalf("PID file remains after shutdown: %v", err)
	}
}

func TestServiceHonorsConfiguredModelStorage(t *testing.T) {
	home := t.TempDir()
	models := filepath.Join(home, "custom-models")
	t.Setenv(config.ProjectHomeEnv, home)
	if err := os.WriteFile(
		filepath.Join(home, "config.yaml"),
		[]byte("model_storage_dir: "+models+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	service, err := NewService()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	registered := service.Manager.Backends()
	if len(registered) == 0 {
		t.Fatal("no built-in backends registered")
	}
	resolved, err := registered[0].Resolve(context.Background(), profiles.Profile{
		Model: profiles.ModelSpec{Source: "example://model", Reference: "artifact.bin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := registered[0].Plan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plan.TargetRoot, models+string(os.PathSeparator)) {
		t.Fatalf("target root = %q, want under %q", plan.TargetRoot, models)
	}
}
