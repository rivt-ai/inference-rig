package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"inferencerig/backends"
	"inferencerig/backends/backendtest"
	"inferencerig/core/configstore"
	"inferencerig/core/profiles"
	coreruntime "inferencerig/core/runtime"
)

type failingStartRuntime struct{ starts *int }

func (r *failingStartRuntime) Start(context.Context) (coreruntime.CommandResult, error) {
	(*r.starts)++
	return coreruntime.CommandResult{Action: "start"}, errors.New("engine unavailable")
}

func TestSetProfileAutostartRejectsImpossibleConfiguration(t *testing.T) {
	manager, _ := lifecycleManager(t, backendtest.New("test"), &fakeRuntime{}, "one")
	if _, err := manager.PutProfile(t.Context(), "two", profileYAML("two", "https://example.test/m"), true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("autostart_profiles: [one]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager.config = configstore.NewFileStore(path, 0)
	if _, err := manager.SetProfileAutostart(t.Context(), "two", true); err == nil || !strings.Contains(err.Error(), "single-active") {
		t.Fatalf("SetProfileAutostart error = %v", err)
	}
	cfg, err := manager.config.Read(t.Context())
	if err != nil || strings.Join(cfg.AutostartProfiles, ",") != "one" {
		t.Fatalf("config = %#v, err = %v", cfg, err)
	}
}
func (*failingStartRuntime) Stop(context.Context) (coreruntime.CommandResult, error) {
	return coreruntime.CommandResult{Action: "stop"}, nil
}
func (*failingStartRuntime) Status(context.Context) (coreruntime.Status, error) {
	return coreruntime.Status{}, nil
}
func (*failingStartRuntime) Recover(context.Context) (bool, error) { return false, nil }

func TestAutostartValidationRejectsImpossibleSets(t *testing.T) {
	manager, _ := lifecycleManager(t, backendtest.New("test"), &fakeRuntime{}, "one")
	if _, err := manager.PutProfile(t.Context(), "two", profileYAML("two", "https://example.test/m"), true); err != nil {
		t.Fatal(err)
	}
	if err := manager.ValidateAutostart(t.Context(), []string{"one", "two"}); err == nil || !strings.Contains(err.Error(), "single-active") {
		t.Fatalf("exclusive validation error = %v", err)
	}

	other := backendtest.New("other")
	if err := manager.registry.Register(other); err != nil {
		t.Fatal(err)
	}
	store := manager.profiles.(*profiles.FileStore)
	if _, err := store.Create(t.Context(), profiles.CreateRequest{Name: "other", ProfileYAML: strings.Replace(profileYAML("other", "https://example.test/m"), "backend: test", "backend: other", 1)}); err != nil {
		t.Fatal(err)
	}
	if err := manager.ValidateAutostart(t.Context(), []string{"one", "other"}); err == nil || !strings.Contains(err.Error(), "one backend") {
		t.Fatalf("mixed validation error = %v", err)
	}
}

//nolint:gocyclo // One retry scenario verifies the bound, final state, and canonical outcome event.
func TestAutostartRetriesInOrderAndMakesFinalFailureVisible(t *testing.T) {
	starts := 0
	manager, _ := lifecycleManager(t, backendtest.New("test"), &fakeRuntime{}, "zeta")
	if _, err := manager.PutProfile(t.Context(), "alpha", profileYAML("alpha", "https://example.test/m"), true); err != nil {
		t.Fatal(err)
	}
	manager.factory = func(coreruntime.LaunchSpec) Runtime { return &failingStartRuntime{starts: &starts} }

	if err := manager.autostartProfiles(t.Context(), []string{"zeta"}, 3, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if starts != 3 {
		t.Fatalf("starts = %d, want bounded 3", starts)
	}
	status, err := manager.RuntimeStatus(t.Context(), "zeta")
	if err != nil || status.State != coreruntime.Stopped {
		t.Fatalf("status = %#v, err = %v", status, err)
	}
	events := manager.Events().List()
	var outcome Event
	for _, event := range events {
		if event.Action == "runtime.autostart" {
			outcome = event
			break
		}
	}
	if outcome.Profile != "zeta" || outcome.Success || !strings.Contains(outcome.Detail, "engine unavailable") {
		t.Fatalf("outcome event = %#v", outcome)
	}
	if !slices.ContainsFunc(events, func(event Event) bool { return event.State == coreruntime.Failed }) {
		t.Fatalf("failed transition missing: %#v", events)
	}
}

func TestAutostartKeepsRecoveredProfileWithoutRestart(t *testing.T) {
	engine := &recoveredRuntime{pid: 4242}
	manager, _ := lifecycleManager(t, backendtest.New("test"), engine, "one")
	if err := manager.RecoverRuntimes(t.Context()); err != nil {
		t.Fatal(err)
	}
	err := manager.autostartProfiles(t.Context(), []string{"one"}, 3, 0)
	if err != nil || engine.starts != 0 {
		t.Fatalf("starts = %d, err = %v", engine.starts, err)
	}
}

type routerAutostartBackend struct {
	*backendtest.Fake
	activated []string
}

func (*routerAutostartBackend) Capabilities() backends.Capabilities {
	return backends.Capabilities{SingleFileArtifacts: true, DiscreteVRAM: true}
}
func (b *routerAutostartBackend) ActivateRuntime(_ context.Context, profile profiles.Profile) error {
	b.activated = append(b.activated, profile.Name)
	return nil
}

func TestAutostartRouterProfilesStartInLexicalOrder(t *testing.T) {
	backend := &routerAutostartBackend{Fake: backendtest.New("test")}
	engine := &fakeRuntime{}
	manager, _ := lifecycleManager(t, backend, engine, "zeta")
	if _, err := manager.PutProfile(t.Context(), "alpha", profileYAML("alpha", "https://example.test/m"), true); err != nil {
		t.Fatal(err)
	}
	err := manager.autostartProfiles(t.Context(), []string{"zeta", "alpha"}, 1, 0)
	if err != nil || strings.Join(backend.activated, ",") != "alpha,zeta" || engine.starts != 1 {
		t.Fatalf("activated = %v, starts = %d, err = %v", backend.activated, engine.starts, err)
	}
}

func TestAutostartContinuesAfterOneProfileFails(t *testing.T) {
	backend := &routerAutostartBackend{Fake: backendtest.New("test")}
	manager, _ := lifecycleManager(t, backend, &fakeRuntime{}, "zeta")
	if _, err := manager.PutProfile(t.Context(), "alpha", profileYAML("alpha", "https://example.test/m"), true); err != nil {
		t.Fatal(err)
	}
	failures, success := 0, &fakeRuntime{}
	manager.factory = func(spec coreruntime.LaunchSpec) Runtime {
		if spec.Name == "alpha" {
			return &failingStartRuntime{starts: &failures}
		}
		return success
	}
	if err := manager.autostartProfiles(t.Context(), []string{"zeta", "alpha"}, 2, 0); err != nil {
		t.Fatal(err)
	}
	info, err := manager.GetInfo(t.Context())
	if err != nil || strings.Join(info.RunningProfiles, ",") != "zeta" || failures != 2 || success.starts != 1 {
		t.Fatalf("info = %#v, failures = %d, successful starts = %d, err = %v", info, failures, success.starts, err)
	}
}

type failingStatusStore struct{ ProfileStore }

func (*failingStatusStore) Get(context.Context, string) (profiles.ProfileDocument, error) {
	return profiles.ProfileDocument{}, errors.New("profile store unavailable")
}

func TestAutostartReportsStatusFailureAndContinues(t *testing.T) {
	manager, _ := lifecycleManager(t, backendtest.New("test"), &fakeRuntime{}, "one", "two")
	manager.profiles = &failingStatusStore{ProfileStore: manager.profiles}
	if err := manager.autostartProfiles(t.Context(), []string{"two", "one"}, 1, 0); err != nil {
		t.Fatal(err)
	}
	outcomes := 0
	for _, event := range manager.Events().List() {
		if event.Action == "runtime.autostart" && !event.Success && strings.Contains(event.Detail, "profile store unavailable") {
			outcomes++
		}
	}
	if outcomes != 2 {
		t.Fatalf("autostart outcomes = %d, want both profiles", outcomes)
	}
}

func TestAutostartRouterProfilesRequireOneListenAddress(t *testing.T) {
	manager, _ := lifecycleManager(t, &routerAutostartBackend{Fake: backendtest.New("test")}, &fakeRuntime{}, "one")
	if _, err := manager.PutProfile(t.Context(), "two", profileYAMLOnPort("two", "https://example.test/m", 8081), true); err != nil {
		t.Fatal(err)
	}
	if err := manager.ValidateAutostart(t.Context(), []string{"one", "two"}); err == nil || !strings.Contains(err.Error(), "listen address") {
		t.Fatalf("router validation error = %v", err)
	}
}
