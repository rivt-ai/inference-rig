package backends_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"inferencerig/backends"
	"inferencerig/backends/backendtest"
	"inferencerig/core/profiles"
)

const demoProfileYAML = `version: "1"
name: demo
backend: fake
model:
  source: example://model
listen:
  host: 127.0.0.1
  port: 8080
`

// TestRegistryBackedProfileStore proves the shared profile store drives a
// registered backend purely through interfaces: the FileStore is constructed
// with the registry's BackendLookup, so common-field validation is shared and
// engine_args validation is delegated to the registered fake backend.
func TestRegistryBackedProfileStore(t *testing.T) {
	reg := backends.NewRegistry()
	if err := reg.Register(backendtest.New("fake")); err != nil {
		t.Fatal(err)
	}
	store := profiles.NewFileStore(t.TempDir(), 0, reg.BackendLookup())
	ctx := context.Background()

	createAndRead(ctx, t, store)
	replaceAndVerify(ctx, t, store)
	deleteAndVerify(ctx, t, store)
}

func createAndRead(ctx context.Context, t *testing.T, store *profiles.FileStore) {
	t.Helper()
	doc, err := store.Create(ctx, profiles.CreateRequest{Name: "demo", ProfileYAML: demoProfileYAML})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if doc.Effective.Backend != "fake" || doc.Effective.Listen.Host != "127.0.0.1" {
		t.Fatalf("unexpected effective profile: %+v", doc.Effective)
	}
	if got, err := store.Get(ctx, "demo"); err != nil || got.Name != "demo" {
		t.Fatalf("Get: doc=%+v err=%v", got, err)
	}
	if list, err := store.List(ctx); err != nil || len(list) != 1 {
		t.Fatalf("List: %v err=%v", list, err)
	}
}

func replaceAndVerify(ctx context.Context, t *testing.T, store *profiles.FileStore) {
	t.Helper()
	replaced := strings.Replace(demoProfileYAML, "port: 8080", "port: 9090", 1)
	if _, err := store.Replace(ctx, "demo", replaced); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if got, _ := store.Get(ctx, "demo"); got.Effective.Listen.Port != 9090 {
		t.Fatalf("Replace did not persist: port=%d", got.Effective.Listen.Port)
	}
}

func deleteAndVerify(ctx context.Context, t *testing.T, store *profiles.FileStore) {
	t.Helper()
	if _, err := store.Delete(ctx, "demo"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if list, _ := store.List(ctx); len(list) != 0 {
		t.Fatalf("profile survived delete: %v", list)
	}
}

// TestRegistryBackedLookupRejectsUnknownBackend proves an unregistered backend
// key surfaces as an invalid profile through the shared store.
func TestRegistryBackedLookupRejectsUnknownBackend(t *testing.T) {
	reg := backends.NewRegistry() // nothing registered
	store := profiles.NewFileStore(filepath.Clean(t.TempDir()), 0, reg.BackendLookup())
	_, err := store.Validate(context.Background(), profiles.CreateRequest{
		Name:        "demo",
		ProfileYAML: demoProfileYAML,
	})
	if err == nil {
		t.Fatal("unknown backend accepted")
	}
}
