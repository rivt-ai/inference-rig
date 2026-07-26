package bootstrap

import (
	"context"
	"os"
	"testing"
	"time"

	"inferencerig/config"
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
