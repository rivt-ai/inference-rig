package control

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"inferencerig/backends"
	"inferencerig/backends/backendtest"
	"inferencerig/core/profiles"
	coreruntime "inferencerig/core/runtime"
)

// generatedPath is where generatingBackend renders, relative to the working
// directory the helper below moves into.
const generatedPath = "generated/models.ini"

// generatingBackend renders the model source of every profile into one
// generated file, standing in for llama.cpp's models.ini without importing the
// engine here.
type generatingBackend struct {
	*backendtest.Fake
}

func (b *generatingBackend) Materialize(p profiles.Profile) (backends.Materialization, error) {
	return b.MaterializeProfiles([]profiles.Profile{p})
}

func (b *generatingBackend) MaterializeProfiles(ps []profiles.Profile) (backends.Materialization, error) {
	lines := make([]string, 0, len(ps))
	for _, p := range ps {
		lines = append(lines, "["+p.Name+"]\nmodel = "+p.Model.Source)
	}
	return backends.Materialization{
		Files:   []backends.GeneratedFile{{Path: generatedPath, Content: []byte(strings.Join(lines, "\n"))}},
		Summary: "test command",
	}, nil
}

func generatingManager(t *testing.T) *Manager {
	t.Helper()
	t.Chdir(t.TempDir())
	registry := backends.NewRegistry()
	if err := registry.Register(&generatingBackend{Fake: backendtest.New("test")}); err != nil {
		t.Fatal(err)
	}
	return NewManager(Dependencies{
		Registry:       registry,
		Profiles:       profiles.NewFileStore(t.TempDir(), 0, registry.BackendLookup()),
		RuntimeFactory: func(coreruntime.LaunchSpec) Runtime { return &fakeRuntime{} },
	})
}

func readGenerated(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}
	return string(data)
}

// A profile edit used to leave the generated file naming the previous model,
// because only a start ever re-rendered it. The engine then failed to load a
// model file that no longer existed, long after the edit reported success.
func TestProfileWriteRegeneratesTheBackendFile(t *testing.T) {
	cases := []struct {
		name    string
		sources []string
	}{
		{name: "create", sources: []string{"/models/first.gguf"}},
		{name: "edit", sources: []string{"/models/first.gguf", "/models/second.gguf"}},
		{name: "edit twice", sources: []string{"/models/a.gguf", "/models/b.gguf", "/models/c.gguf"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { assertRegenerated(t, tc.sources) })
	}
}

// assertRegenerated writes the profile once per source and checks the generated
// file names only the last one.
func assertRegenerated(t *testing.T, sources []string) {
	t.Helper()
	manager := generatingManager(t)
	ctx := context.Background()
	for i, source := range sources {
		if _, err := manager.PutProfile(ctx, "one", profileYAML("one", source), i == 0); err != nil {
			t.Fatalf("put %q: %v", source, err)
		}
	}
	content, final := readGenerated(t), sources[len(sources)-1]
	if !strings.Contains(content, "model = "+final) {
		t.Fatalf("generated file %q does not name the current model %q", content, final)
	}
	for _, stale := range sources[:len(sources)-1] {
		if strings.Contains(content, "model = "+stale) {
			t.Fatalf("generated file %q still names the replaced model %q", content, stale)
		}
	}
}

// routerGeneratingBackend is a router (several profiles in one process) whose
// profiles all render into one shared generated file, like llama.cpp's
// models.ini.
type routerGeneratingBackend struct {
	*generatingBackend
	activated []string
}

func (b *routerGeneratingBackend) Capabilities() backends.Capabilities {
	capabilities := b.Fake.Capabilities()
	capabilities.SingleActiveProfile = false
	return capabilities
}

func (b *routerGeneratingBackend) ActivateRuntime(_ context.Context, p profiles.Profile) error {
	b.activated = append(b.activated, p.Name)
	return nil
}

// A router engine reads the shared generated file only at startup, so a profile
// created or deleted while the router ran never appeared in (or left) its model
// list until a manual restart. A write that changes the file must replace the
// process and re-activate the profiles it held; a write that changes nothing
// must leave it alone.
func TestProfileWriteReloadsARunningRouter(t *testing.T) {
	backend := &routerGeneratingBackend{generatingBackend: &generatingBackend{Fake: backendtest.New("test")}}
	registry := backends.NewRegistry()
	if err := registry.Register(backend); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	var engines []*fakeRuntime
	manager := NewManager(Dependencies{
		Registry: registry,
		Profiles: profiles.NewFileStore(t.TempDir(), 0, registry.BackendLookup()),
		RuntimeFactory: func(coreruntime.LaunchSpec) Runtime {
			engine := &fakeRuntime{}
			engines = append(engines, engine)
			return engine
		},
	})
	ctx := context.Background()
	if _, err := manager.PutProfile(ctx, "one", profileYAML("one", "https://example.test/m"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartRuntime(ctx, "one", false); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.PutProfile(ctx, "two", profileYAML("two", "https://example.test/m"), true); err != nil {
		t.Fatal(err)
	}
	if len(engines) != 2 || engines[0].stops != 1 || engines[1].starts != 1 {
		t.Fatalf("creating a profile under a running router: %d engines, first stops = %d", len(engines), engines[0].stops)
	}
	if got := strings.Join(backend.activated, ","); got != "one,one" {
		t.Fatalf("activated = %q, want the held profile re-activated after the reload", got)
	}
	if status, err := manager.RuntimeStatus(ctx, "one"); err != nil || status.State != coreruntime.Running {
		t.Fatalf("held profile after reload: status = %#v, err = %v", status, err)
	}

	if _, err := manager.PutProfile(ctx, "two", profileYAML("two", "https://example.test/m"), false); err != nil {
		t.Fatal(err)
	}
	if len(engines) != 2 {
		t.Fatalf("a write changing nothing replaced the process: %d engines", len(engines))
	}

	if _, err := manager.DeleteProfile(ctx, "two"); err != nil {
		t.Fatal(err)
	}
	if len(engines) != 3 || strings.Contains(readGenerated(t), "[two]") {
		t.Fatalf("deleting a profile: %d engines, generated file = %q", len(engines), readGenerated(t))
	}
}

// A model file that is not on disk must fail the start with a typed, readable
// error rather than reaching the engine as an opaque per-request load failure.
func TestStartRejectsAProfileWhoseModelFileIsMissing(t *testing.T) {
	present := filepath.Join(t.TempDir(), "present.gguf")
	if err := os.WriteFile(present, []byte("model"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		source string
		kind   ErrorKind
	}{
		{name: "model file on disk", source: present},
		{name: "model file deleted", source: filepath.Join(t.TempDir(), "gone.gguf"), kind: ErrorNotFound},
		{name: "remote source is not a path", source: "https://example.test/m"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { assertStartKind(t, tc.source, tc.kind) })
	}
}

// assertStartKind starts a profile naming source and checks the typed outcome.
func assertStartKind(t *testing.T, source string, kind ErrorKind) {
	t.Helper()
	manager := generatingManager(t)
	ctx := context.Background()
	if _, err := manager.PutProfile(ctx, "one", profileYAML("one", source), true); err != nil {
		t.Fatal(err)
	}
	_, err := manager.StartRuntime(ctx, "one", false)
	if kind == "" {
		if err != nil {
			t.Fatalf("start = %v, want success", err)
		}
		return
	}
	if Kind(err) != kind {
		t.Fatalf("start error kind = %v (%v), want %v", Kind(err), err, kind)
	}
	if !strings.Contains(err.Error(), "is missing") {
		t.Fatalf("start error %q does not say the model file is missing", err)
	}
}
