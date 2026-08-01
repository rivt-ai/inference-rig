package rpc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"

	"inferencerig/backends"
	"inferencerig/backends/backendtest"
	"inferencerig/core/control"
	"inferencerig/core/modelcatalog"
	"inferencerig/core/profiles"
	controlv1 "inferencerig/core/rpc/gen/v1"
	coreruntime "inferencerig/core/runtime"
)

// The RPC layer's job is to reject bad input before it reaches the manager and
// to translate domain errors into Connect codes. Both are invisible when they
// break: a missing check turns a client bug into a server-side failure, and a
// wrong code turns "you asked for something that does not exist" into "the
// daemon is broken".

func newTestService(t *testing.T) *ControlService {
	t.Helper()
	registry := backends.NewRegistry()
	if err := registry.Register(backendtest.New("test")); err != nil {
		t.Fatal(err)
	}
	store := profiles.NewFileStore(t.TempDir(), 0, registry.BackendLookup())
	return NewControlService(control.NewManager(control.Dependencies{
		Registry: registry, Profiles: store,
		RuntimeFactory: func(coreruntime.LaunchSpec) control.Runtime { return &rpcRuntime{} },
	}))
}

// Every required-field check, in one table. A new procedure that forgets its
// guard is the failure this is guarding against, so the cases are grouped by
// the field rather than by the procedure.
func TestControlServiceRejectsMissingRequiredFields(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	tests := []struct {
		name string
		call func() error
	}{
		{"profile name on delete", func() error {
			_, err := service.DeleteProfile(ctx, &controlv1.DeleteProfileRequest{})
			return err
		}},
		{"profile name on get", func() error {
			_, err := service.GetProfile(ctx, &controlv1.GetProfileRequest{})
			return err
		}},
		{"profile YAML on put", func() error {
			_, err := service.PutProfile(ctx, &controlv1.PutProfileRequest{Name: "p"})
			return err
		}},
		{"download ID on get", func() error {
			_, err := service.GetModelDownload(ctx, &controlv1.GetModelDownloadRequest{})
			return err
		}},
		{"download ID on cancel", func() error {
			_, err := service.CancelModelDownload(ctx, &controlv1.CancelModelDownloadRequest{})
			return err
		}},
		{"backend on install", func() error {
			_, err := service.InstallBackend(ctx, &controlv1.InstallBackendRequest{})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			if connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Fatalf("error = %v (code %v), want invalid_argument", err, connect.CodeOf(err))
			}
		})
	}
}

// A request for something that does not exist must be not_found, not internal:
// the distinction is what tells a caller whether retrying could ever help.
func TestControlServiceMapsMissingResourcesToNotFound(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	if _, err := service.GetProfile(ctx, &controlv1.GetProfileRequest{Name: "absent"}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("GetProfile(absent) = %v (code %v), want not_found", err, connect.CodeOf(err))
	}
	if _, err := service.DeleteProfile(ctx, &controlv1.DeleteProfileRequest{Name: "absent"}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("DeleteProfile(absent) = %v (code %v), want not_found", err, connect.CodeOf(err))
	}
	if _, err := service.GetModelDownload(ctx, &controlv1.GetModelDownloadRequest{Id: "absent"}); err == nil {
		t.Error("GetModelDownload(absent) succeeded")
	}
	if _, err := service.CancelModelDownload(ctx, &controlv1.CancelModelDownloadRequest{Id: "absent"}); err == nil {
		t.Error("CancelModelDownload(absent) succeeded")
	}
}

func TestControlServiceRejectsUnknownBackend(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	if _, err := service.ResolveModel(ctx, &controlv1.ResolveModelRequest{Backend: "absent", Reference: "r"}); err == nil {
		t.Error("ResolveModel accepted an unknown backend")
	}
	if _, err := service.EstimateFit(ctx, &controlv1.EstimateFitRequest{Backend: "absent", SizeBytes: 1}); err == nil {
		t.Error("EstimateFit accepted an unknown backend")
	}
}

// A structured profile is rendered to canonical YAML before it is validated, so
// the rendering has to survive the JSON round trip the transport imposes.
func TestRenderProfileYAMLDemotesWholeFloats(t *testing.T) {
	args, err := structpb.NewStruct(map[string]any{
		"threads":    float64(8),
		"temp":       0.7,
		"tags":       []any{float64(1), "auto"},
		"nested":     map[string]any{"layers": float64(32)},
		"flash-attn": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := renderProfileYAML("demo", &controlv1.Profile{
		Backend: "test", ModelSource: "/models/demo.bin", Host: "127.0.0.1", Port: 8080, EngineArgs: args,
	})
	if err != nil {
		t.Fatal(err)
	}
	// structpb carries every number as a float64, and "threads: 8.0" is not a
	// value any engine command line accepts.
	for _, want := range []string{"threads: 8\n", "temp: 0.7\n", "layers: 32\n", "flash-attn: true\n"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered YAML missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "threads: 8.0") {
		t.Errorf("whole float was not demoted:\n%s", rendered)
	}
}

func TestDemoteWholeFloatsLeavesNonIntegralValuesAlone(t *testing.T) {
	if got := demoteWholeFloats(0.5); got != 0.5 {
		t.Errorf("demoteWholeFloats(0.5) = %v", got)
	}
	if got := demoteWholeFloats("auto"); got != "auto" {
		t.Errorf("demoteWholeFloats(auto) = %v", got)
	}
	if got := demoteWholeFloats(float64(3)); got != int64(3) {
		t.Errorf("demoteWholeFloats(3.0) = %v (%T), want int64(3)", got, got)
	}
}

// A catalog stream must end when the client goes away. If it did not, every
// abandoned UI panel would hold a control-socket connection open until the
// daemon restarted.
func TestWatchModelCatalogStopsOnClientCancellation(t *testing.T) {
	registry := backends.NewRegistry()
	if err := registry.Register(backendtest.New("test")); err != nil {
		t.Fatal(err)
	}
	catalog := &blockingCatalog{events: make(chan modelcatalog.RefreshEvent)}
	service := NewControlService(control.NewManager(control.Dependencies{
		Registry: registry,
		Profiles: profiles.NewFileStore(t.TempDir(), 0, registry.BackendLookup()),
		Catalog:  catalog,
	}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- service.WatchModelCatalog(ctx, &controlv1.WatchModelCatalogRequest{}, nil)
	}()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancelled watch returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled watch did not return")
	}
	if !catalog.unsubscribed() {
		t.Error("cancelled watch did not unsubscribe, leaking the upstream subscription")
	}
}

// A closed upstream must end the stream too, rather than spinning on a closed
// channel.
func TestWatchModelCatalogEndsWhenUpstreamCloses(t *testing.T) {
	registry := backends.NewRegistry()
	if err := registry.Register(backendtest.New("test")); err != nil {
		t.Fatal(err)
	}
	events := make(chan modelcatalog.RefreshEvent)
	catalog := &blockingCatalog{events: events}
	service := NewControlService(control.NewManager(control.Dependencies{
		Registry: registry,
		Profiles: profiles.NewFileStore(t.TempDir(), 0, registry.BackendLookup()),
		Catalog:  catalog,
	}))
	done := make(chan error, 1)
	go func() {
		done <- service.WatchModelCatalog(context.Background(), &controlv1.WatchModelCatalogRequest{}, nil)
	}()
	close(events)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("closed upstream returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not end when the upstream closed")
	}
}

type blockingCatalog struct {
	events chan modelcatalog.RefreshEvent
	closed chan struct{}
}

func (c *blockingCatalog) Search(context.Context, modelcatalog.SearchRequest, modelcatalog.CatalogPolicy) (modelcatalog.Result, error) {
	return modelcatalog.Result{}, errors.New("not used")
}

func (c *blockingCatalog) Subscribe() (<-chan modelcatalog.RefreshEvent, func()) {
	c.closed = make(chan struct{})
	return c.events, func() {
		select {
		case <-c.closed:
		default:
			close(c.closed)
		}
	}
}

func (c *blockingCatalog) unsubscribed() bool {
	if c.closed == nil {
		return false
	}
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}
