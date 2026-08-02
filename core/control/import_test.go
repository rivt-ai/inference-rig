package control

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"inferencerig/backends"
	"inferencerig/backends/backendtest"
	"inferencerig/core/profiles"
	coreruntime "inferencerig/core/runtime"
)

// importingBackend is a backend whose generated file is editable, standing in
// for llama.cpp's models.ini without importing it here — the neutral core must
// drive the facet, not the engine.
type importingBackend struct {
	*backendtest.Fake
	adopt      map[string]string
	conflicts  []string
	err        error
	calls      int
	rendered   []string
	seenNames  []string
	renderFrom func(profiles.Profile) string
}

func (b *importingBackend) ImportGenerated(docs []profiles.ProfileDocument) (map[string]string, []string, error) {
	b.calls++
	for _, doc := range docs {
		b.seenNames = append(b.seenNames, doc.Name)
	}
	return b.adopt, b.conflicts, b.err
}

func (b *importingBackend) Materialize(p profiles.Profile) (backends.Materialization, error) {
	b.rendered = append(b.rendered, b.renderFrom(p))
	return backends.Materialization{Summary: "test command"}, nil
}

func startWithImporter(t *testing.T, backend backends.Backend) (*Manager, error) {
	t.Helper()
	t.Chdir(t.TempDir())
	registry := backends.NewRegistry()
	if err := registry.Register(backend); err != nil {
		t.Fatal(err)
	}
	store := profiles.NewFileStore(t.TempDir(), 0, registry.BackendLookup())
	manager := NewManager(Dependencies{
		Registry: registry, Profiles: store,
		RuntimeFactory: func(coreruntime.LaunchSpec) Runtime { return &fakeRuntime{} },
	})
	ctx := context.Background()
	if _, err := manager.PutProfile(ctx, "one", profileYAML("one", "https://example.test/m"), true); err != nil {
		t.Fatal(err)
	}
	_, err := manager.StartRuntime(ctx, "one", false)
	return manager, err
}

func importedYAML() string {
	return strings.Replace(profileYAML("one", "https://example.test/m"),
		"listen:", "engine_args:\n  ctx-size: 16384\nlisten:", 1)
}

func renderCtxSize(p profiles.Profile) string {
	if value, ok := p.EngineArgs["ctx-size"]; ok {
		return strings.TrimSpace(strings.Join([]string{"ctx-size", toString(value)}, "="))
	}
	return "ctx-size=<unset>"
}

func toString(value any) string {
	switch v := value.(type) {
	case int:
		return strconv.Itoa(v)
	default:
		return fmt.Sprint(v)
	}
}

// The point of importing before rendering: the adopted value must reach this
// start's generated file, not the next one's.
func TestStartRuntimeImportsBeforeMaterializing(t *testing.T) {
	backend := &importingBackend{
		Fake:       backendtest.New("test"),
		renderFrom: renderCtxSize,
		adopt:      map[string]string{"one": importedYAML()},
	}
	manager, err := startWithImporter(t, backend)
	if err != nil {
		t.Fatalf("StartRuntime: %v", err)
	}
	if backend.calls != 1 {
		t.Fatalf("ImportGenerated calls = %d, want 1", backend.calls)
	}
	if len(backend.rendered) == 0 || backend.rendered[len(backend.rendered)-1] != "ctx-size=16384" {
		t.Fatalf("rendered = %#v, want the imported value", backend.rendered)
	}
	// And the canonical profile is the one that changed — the file, not a copy
	// held in memory for this start.
	doc, err := manager.GetProfile(context.Background(), "one")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.ProfileYAML, "ctx-size: 16384") {
		t.Fatalf("stored profile = %q", doc.ProfileYAML)
	}
}

// Every profile the backend owns is offered, not just the one being started:
// one file holds them all, so an edit to any section must be seen.
func TestStartRuntimeOffersEveryProfileToTheImporter(t *testing.T) {
	backend := &importingBackend{Fake: backendtest.New("test"), renderFrom: renderCtxSize}
	t.Chdir(t.TempDir())
	registry := backends.NewRegistry()
	if err := registry.Register(backend); err != nil {
		t.Fatal(err)
	}
	store := profiles.NewFileStore(t.TempDir(), 0, registry.BackendLookup())
	manager := NewManager(Dependencies{
		Registry: registry, Profiles: store,
		RuntimeFactory: func(coreruntime.LaunchSpec) Runtime { return &fakeRuntime{} },
	})
	ctx := context.Background()
	for _, name := range []string{"one", "two"} {
		if _, err := manager.PutProfile(ctx, name, profileYAML(name, "https://example.test/m"), true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := manager.StartRuntime(ctx, "one", false); err != nil {
		t.Fatal(err)
	}
	if len(backend.seenNames) != 2 {
		t.Fatalf("importer saw %#v, want both profiles", backend.seenNames)
	}
}

// The generated file is derived state. If reading it back fails, the canonical
// YAML is still complete, and refusing to start would trade a lost edit for an
// unstartable engine.
func TestStartRuntimeSurvivesImportFailure(t *testing.T) {
	backend := &importingBackend{
		Fake: backendtest.New("test"), renderFrom: renderCtxSize,
		err: errors.New("models.ini is not parseable"),
	}
	if _, err := startWithImporter(t, backend); err != nil {
		t.Fatalf("import failure failed the start: %v", err)
	}
}

// An imported profile the store rejects must cost the user nothing but the
// edit: the profile on disk stays valid and the runtime still starts.
func TestStartRuntimeRejectsInvalidImportWithoutBreakingTheProfile(t *testing.T) {
	backend := &importingBackend{
		Fake: backendtest.New("test"), renderFrom: renderCtxSize,
		adopt: map[string]string{"one": "this: is not a profile\n"},
	}
	manager, err := startWithImporter(t, backend)
	if err != nil {
		t.Fatalf("invalid import failed the start: %v", err)
	}
	doc, err := manager.GetProfile(context.Background(), "one")
	if err != nil {
		t.Fatalf("profile no longer readable after a rejected import: %v", err)
	}
	if !strings.Contains(doc.ProfileYAML, "name: one") {
		t.Fatalf("stored profile = %q", doc.ProfileYAML)
	}
}

// A backend with nothing editable on disk implements no facet and must start
// exactly as before.
func TestStartRuntimeWithoutImporterIsUnchanged(t *testing.T) {
	if _, err := startWithImporter(t, backendtest.New("test")); err != nil {
		t.Fatalf("StartRuntime without importer: %v", err)
	}
}
