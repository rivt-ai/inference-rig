package control

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"inferencerig/backends"
	"inferencerig/backends/backendtest"
	"inferencerig/core/profiles"
	coreruntime "inferencerig/core/runtime"
)

// blockingRuntime holds a start or a stop inside the supervisor call — exactly
// where the manager used to be holding its mutex — so a test can observe what
// the rest of the control plane can still do meanwhile.
type blockingRuntime struct {
	entered             chan string
	startGate, stopGate chan struct{}
	state               coreruntime.State
}

func newBlockingRuntime() *blockingRuntime {
	return &blockingRuntime{
		entered:   make(chan string, 4),
		startGate: make(chan struct{}),
		stopGate:  make(chan struct{}),
	}
}

func (r *blockingRuntime) Start(context.Context) (coreruntime.CommandResult, error) {
	r.entered <- "start"
	<-r.startGate
	r.state = coreruntime.Running
	return coreruntime.CommandResult{Action: "start"}, nil
}

func (r *blockingRuntime) Stop(context.Context) (coreruntime.CommandResult, error) {
	r.entered <- "stop"
	<-r.stopGate
	r.state = coreruntime.Stopped
	return coreruntime.CommandResult{Action: "stop"}, nil
}

func (r *blockingRuntime) Status(context.Context) (coreruntime.Status, error) {
	return coreruntime.Status{State: r.state, CheckedAt: time.Now()}, nil
}

// recordingSink is the external audit log, kept separate from the event store so
// a test can prove a transition reaches both. Concurrent lifecycle calls record
// from their own goroutines, which is what an AuditSink must tolerate.
type recordingSink struct {
	mu     sync.Mutex
	events []AuditEvent
}

func (s *recordingSink) Record(_ context.Context, event AuditEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *recordingSink) transitions() []AuditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.events)
}

func lifecycleManager(t *testing.T, backend backends.Backend, runtime Runtime, names ...string) (*Manager, *recordingSink) {
	t.Helper()
	// Materialized files carry relative paths, so without this the suite writes
	// generated config into the package source tree.
	t.Chdir(t.TempDir())
	registry := backends.NewRegistry()
	if err := registry.Register(backend); err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	manager := NewManager(Dependencies{
		Registry:       registry,
		Profiles:       profiles.NewFileStore(t.TempDir(), 0, registry.BackendLookup()),
		Audit:          sink,
		RuntimeFactory: func(coreruntime.LaunchSpec) Runtime { return runtime },
	})
	for _, name := range names {
		if _, err := manager.PutProfile(context.Background(), name, profileYAML(name, "https://example.test/m"), true); err != nil {
			t.Fatal(err)
		}
	}
	return manager, sink
}

// statusWithin fails rather than hanging when a status call is blocked behind
// somebody else's lock, which is the regression this whole ticket is about.
func statusWithin(t *testing.T, manager *Manager, profile string) coreruntime.Status {
	t.Helper()
	done := make(chan coreruntime.Status, 1)
	go func() {
		status, err := manager.RuntimeStatus(context.Background(), profile)
		if err != nil {
			t.Errorf("status %s: %v", profile, err)
		}
		done <- status
	}()
	select {
	case status := <-done:
		return status
	case <-time.After(5 * time.Second):
		t.Fatalf("status %s blocked while another operation was in flight", profile)
		return coreruntime.Status{}
	}
}

// A cold engine start can take a minute. It must not stall the dashboard: the
// slot's own state answers a status call, so neither the starting profile nor an
// unrelated one waits for the engine to come up.
func TestStatusDoesNotWaitForAColdStart(t *testing.T) {
	engine := newBlockingRuntime()
	manager, _ := lifecycleManager(t, backendtest.New("test"), engine, "one", "two")
	started := make(chan error, 1)
	go func() {
		_, err := manager.StartRuntime(context.Background(), "one", false)
		started <- err
	}()
	if entered := <-engine.entered; entered != "start" {
		t.Fatalf("entered %q, want start", entered)
	}

	if status := statusWithin(t, manager, "two"); status.State != coreruntime.Stopped {
		t.Fatalf("unrelated profile status = %#v, want stopped", status)
	}
	if status := statusWithin(t, manager, "one"); status.State != coreruntime.Starting {
		t.Fatalf("starting profile status = %#v, want starting", status)
	}

	close(engine.startGate)
	if err := <-started; err != nil {
		t.Fatalf("start: %v", err)
	}
	if status := statusWithin(t, manager, "one"); status.State != coreruntime.Running {
		t.Fatalf("started profile status = %#v, want running", status)
	}
}

// Ordering one: the start reserves the slot first. The stop that arrives while
// the engine is still coming up loses, and loses immediately with a typed
// conflict rather than being queued behind a start that may take a minute.
func TestStopDuringStartLosesWithAConflict(t *testing.T) {
	engine := newBlockingRuntime()
	manager, _ := lifecycleManager(t, backendtest.New("test"), engine, "one")
	started := make(chan error, 1)
	go func() {
		_, err := manager.StartRuntime(context.Background(), "one", false)
		started <- err
	}()
	<-engine.entered

	stopped := make(chan error, 1)
	go func() {
		_, err := manager.StopRuntime(context.Background(), "one")
		stopped <- err
	}()
	select {
	case err := <-stopped:
		if Kind(err) != ErrorConflict {
			t.Fatalf("stop during start = %v, want a conflict", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop queued behind the start instead of losing")
	}

	close(engine.startGate)
	if err := <-started; err != nil {
		t.Fatalf("start: %v", err)
	}
	if status := statusWithin(t, manager, "one"); status.State != coreruntime.Running {
		t.Fatalf("status = %#v, want the winning start to have run to completion", status)
	}
}

// Ordering two: the stop reserves the slot first, so the start is the loser.
// The same rule decides both, which is what makes the outcome depend on which
// call reached the slot rather than on how long either one takes.
func TestStartDuringStopLosesWithAConflict(t *testing.T) {
	engine := newBlockingRuntime()
	close(engine.startGate) // the setup start runs straight through
	manager, _ := lifecycleManager(t, backendtest.New("test"), engine, "one")
	ctx := context.Background()
	if _, err := manager.StartRuntime(ctx, "one", false); err != nil {
		t.Fatal(err)
	}
	<-engine.entered

	stopped := make(chan error, 1)
	go func() {
		_, err := manager.StopRuntime(ctx, "one")
		stopped <- err
	}()
	<-engine.entered

	started := make(chan error, 1)
	go func() {
		_, err := manager.StartRuntime(ctx, "one", false)
		started <- err
	}()
	select {
	case err := <-started:
		if Kind(err) != ErrorConflict {
			t.Fatalf("start during stop = %v, want a conflict", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("start queued behind the stop instead of losing")
	}

	close(engine.stopGate)
	if err := <-stopped; err != nil {
		t.Fatalf("stop: %v", err)
	}
	if status := statusWithin(t, manager, "one"); status.State != coreruntime.Stopped {
		t.Fatalf("status = %#v, want the winning stop to have run to completion", status)
	}
}

// A client should be able to follow a lifecycle through the event stream instead
// of polling status, so every transition carries the operation ID that ties it
// to the others, plus the profile, backend and state it describes — and reaches
// the external audit log as well as the in-memory stream.
func TestEveryTransitionIsObservable(t *testing.T) {
	engine := newBlockingRuntime()
	close(engine.startGate)
	close(engine.stopGate)
	manager, sink := lifecycleManager(t, backendtest.New("test"), engine, "one")
	ctx := context.Background()
	if _, err := manager.StartRuntime(ctx, "one", false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StopRuntime(ctx, "one"); err != nil {
		t.Fatal(err)
	}

	states, operations := streamedTransitions(t, manager.Events().List())
	// List returns newest first.
	want := []coreruntime.State{
		coreruntime.Stopped, coreruntime.Stopping,
		coreruntime.Running, coreruntime.Activating, coreruntime.Starting,
	}
	if !slices.Equal(states, want) {
		t.Fatalf("transitions = %v, want %v", states, want)
	}
	if operations != 2 {
		t.Fatalf("start and stop shared %d operation ID(s), want one each", operations)
	}
	if audited := auditedTransitions(sink); audited != len(want) {
		t.Fatalf("audit log recorded %d transitions, want %d", audited, len(want))
	}
}

// streamedTransitions returns the states of every transition event in order,
// failing if one arrived without the identity a client needs to make sense of
// it, plus how many distinct operations they belong to.
func streamedTransitions(t *testing.T, events []Event) ([]coreruntime.State, int) {
	t.Helper()
	states, operations := []coreruntime.State{}, map[string]struct{}{}
	for _, event := range events {
		if event.Action != "runtime.transition" {
			continue
		}
		if event.Profile != "one" || event.Backend != "test" || event.OperationID == "" {
			t.Fatalf("transition missing its identity: %#v", event)
		}
		states = append(states, event.State)
		operations[event.OperationID] = struct{}{}
	}
	return states, len(operations)
}

func auditedTransitions(sink *recordingSink) int {
	audited := 0
	for _, event := range sink.transitions() {
		if event.Action == "runtime.transition" && event.OperationID != "" {
			audited++
		}
	}
	return audited
}

// routerBackend serves several profiles from one process, so activating a second
// one must not stop the first.
type routerBackend struct{ *backendtest.Fake }

func (b *routerBackend) Capabilities() backends.Capabilities {
	capabilities := b.Fake.Capabilities()
	capabilities.SingleActiveProfile = false
	return capabilities
}

func (b *routerBackend) ActivateRuntime(context.Context, profiles.Profile) error { return nil }

// A router backend holds every activated profile in its one process: starting a
// second profile spawns nothing, and stopping one of several leaves the process
// up for the rest.
func TestRouterBackendSharesOneProcessAcrossProfiles(t *testing.T) {
	engine := newBlockingRuntime()
	close(engine.startGate)
	close(engine.stopGate)
	spawns := 0
	registry := backends.NewRegistry()
	if err := registry.Register(&routerBackend{Fake: backendtest.New("test")}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	manager := NewManager(Dependencies{
		Registry: registry,
		Profiles: profiles.NewFileStore(t.TempDir(), 0, registry.BackendLookup()),
		RuntimeFactory: func(coreruntime.LaunchSpec) Runtime {
			spawns++
			return engine
		},
	})
	ctx := context.Background()
	for _, name := range []string{"one", "two"} {
		if _, err := manager.PutProfile(ctx, name, profileYAML(name, "https://example.test/m"), true); err != nil {
			t.Fatal(err)
		}
		if _, err := manager.StartRuntime(ctx, name, false); err != nil {
			t.Fatalf("start %s: %v", name, err)
		}
	}
	if spawns != 1 {
		t.Fatalf("spawned %d processes, want the router's one", spawns)
	}
	info, err := manager.GetInfo(ctx)
	if err != nil || info.ActiveBackend != "test" || len(info.RunningProfiles) != 2 {
		t.Fatalf("info = %#v, err = %v", info, err)
	}

	if _, err := manager.StopRuntime(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	if status := statusWithin(t, manager, "two"); status.State != coreruntime.Running {
		t.Fatalf("stopping one profile took the shared process down: %#v", status)
	}
	if status := statusWithin(t, manager, "one"); status.State != coreruntime.Stopped {
		t.Fatalf("stopped profile status = %#v", status)
	}
}

// The router's process is bound to the address of whichever profile started it,
// so a profile listening somewhere else cannot join it. Activating it there
// would report a runtime serving an address nothing is listening on — the exact
// class of lie the explicit state machine exists to remove — so it takes a
// replace, like an exclusive backend does.
func TestRouterProfileOnAnotherAddressNeedsTheProcessReplaced(t *testing.T) {
	engine := newBlockingRuntime()
	close(engine.startGate)
	close(engine.stopGate)
	manager, _ := lifecycleManager(t, &routerBackend{Fake: backendtest.New("test")}, engine, "one")
	ctx := context.Background()
	elsewhere := profileYAMLOnPort("two", "https://example.test/m", 8081)
	if _, err := manager.PutProfile(ctx, "two", elsewhere, true); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartRuntime(ctx, "one", false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartRuntime(ctx, "two", false); Kind(err) != ErrorConflict {
		t.Fatalf("start on another address = %v, want a conflict", err)
	}
	if _, err := manager.StartRuntime(ctx, "two", true); err != nil {
		t.Fatalf("replace: %v", err)
	}
	info, err := manager.GetInfo(ctx)
	if err != nil || len(info.RunningProfiles) != 1 || info.RunningProfiles[0] != "two" {
		t.Fatalf("info = %#v, err = %v", info, err)
	}
}

// The active backend is what a front end renders to explain why another
// backend's profiles cannot start, and a reset is what clears it.
func TestResetClearsTheActiveBackend(t *testing.T) {
	engine := newBlockingRuntime()
	close(engine.startGate)
	close(engine.stopGate)
	manager, _ := lifecycleManager(t, backendtest.New("test"), engine, "one")
	ctx := context.Background()
	if _, err := manager.StartRuntime(ctx, "one", false); err != nil {
		t.Fatal(err)
	}
	if info, err := manager.GetInfo(ctx); err != nil || info.ActiveBackend != "test" {
		t.Fatalf("info = %#v, err = %v", info, err)
	}
	if _, err := manager.ResetRuntimes(ctx); err != nil {
		t.Fatal(err)
	}
	info, err := manager.GetInfo(ctx)
	if err != nil || info.ActiveBackend != "" || len(info.RunningProfiles) != 0 {
		t.Fatalf("info after reset = %#v, err = %v", info, err)
	}
}
