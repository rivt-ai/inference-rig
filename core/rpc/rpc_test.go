package rpc

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"

	"inferencerig/config"
	"inferencerig/core/control"
)

func TestNewControlListenerCreatesSecureSocket(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir())
	listener, socketPath, err := NewControlListener()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()

	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket perm = %v", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(socketPath))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("socket dir perm = %v", dirInfo.Mode().Perm())
	}
}

func TestServerServesOverControlTransport(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir())
	srv, err := NewServer("/ping", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "pong")
	}))
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.HTTP.Serve(srv.Listener) }()
	t.Cleanup(func() { _ = srv.HTTP.Close() })

	transport, err := ControlTransport(srv.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	resp, err := client.Get("http://unix/ping")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "pong" {
		t.Fatalf("body = %q", body)
	}
}

func TestValidateRequestInterceptorRejectsNilRequest(t *testing.T) {
	called := false
	next := connect.UnaryFunc(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		called = true
		return connect.NewResponse(&struct{}{}), nil
	})
	handler := ValidateRequestInterceptor()(next)
	_, err := handler(context.Background(), connect.NewRequest[struct{}](nil))
	if err == nil {
		t.Fatal("expected error for nil request")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("code = %v", connect.CodeOf(err))
	}
	if called {
		t.Fatal("next should not be called for a nil request")
	}
}

func TestValidateRequestInterceptorPassesValidRequest(t *testing.T) {
	next := connect.UnaryFunc(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&struct{}{}), nil
	})
	handler := ValidateRequestInterceptor()(next)
	if _, err := handler(context.Background(), connect.NewRequest(&struct{}{})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestErrorKindRoundTripsThroughRPC(t *testing.T) {
	for _, kind := range []control.ErrorKind{
		control.ErrorInvalidInput,
		control.ErrorNotFound,
		control.ErrorConflict,
		control.ErrorTimeout,
		control.ErrorRuntime,
		control.ErrorPermission,
	} {
		err := rpcError(control.Errorf(kind, "boom"))
		if got := ErrorKindFromRPC(err); got != kind {
			t.Fatalf("kind %q round-tripped to %q", kind, got)
		}
	}
}
