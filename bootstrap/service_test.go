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
	coreruntime "inferencerig/core/runtime"
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
	if err != nil || len(backends.GetBackends()) == 0 {
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

func TestSecondDaemonFailsBeforeAutostartCanLaunchAnEngine(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)
	t.Setenv("PATH", bin)
	marker := filepath.Join(home, "engine-started")
	executable := filepath.Join(bin, "llama-server")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n: > "+marker+"\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("autostart_profiles: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profileDir := filepath.Join(home, "profiles", "auto")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	profile := "version: 1\nname: auto\nbackend: llamacpp\nmodel:\n  source: /missing/model.gguf\nlisten:\n  host: 127.0.0.1\n  port: 19877\n"
	if err := os.WriteFile(filepath.Join(profileDir, "profile.yaml"), []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := NewService()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Shutdown(t.Context()) })
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("autostart_profiles: [auto]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if second, err := NewService(); err == nil {
		_ = second.Shutdown(t.Context())
		t.Fatal("second daemon unexpectedly acquired the control socket")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("engine launched before daemon resources were acquired: %v", err)
	}
}

//nolint:gocyclo // One bootstrap scenario proves failed autostart, health, state, and event visibility together.
func TestServiceStaysHealthyWhenAutostartExhaustsRetries(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)
	t.Setenv("PATH", t.TempDir())
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("autostart_profiles: [broken]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profileDir := filepath.Join(home, "profiles", "broken")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	profile := "version: 1\nname: broken\nbackend: llamacpp\nmodel:\n  source: /missing/model.gguf\nlisten:\n  host: 127.0.0.1\n  port: 19876\n"
	if err := os.WriteFile(filepath.Join(profileDir, "profile.yaml"), []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}

	service, err := NewService()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	client, err := rpc.DialControl(service.Server.SocketPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	health, err := client.Health(t.Context(), &controlv1.HealthRequest{})
	if err != nil || !health.GetOk() {
		t.Fatalf("health = %#v, err = %v", health, err)
	}
	status, err := service.Manager.RuntimeStatus(t.Context(), "broken")
	if err != nil || status.State != coreruntime.Stopped {
		t.Fatalf("status = %#v, err = %v", status, err)
	}
	events := service.Manager.Events().List()
	if len(events) == 0 || events[0].Action != "runtime.autostart" || events[0].Success || !strings.Contains(events[0].Detail, "3 attempt") {
		t.Fatalf("events = %#v", events)
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
