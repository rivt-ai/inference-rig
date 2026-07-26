package mlx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coremigrate "inferencerig/core/migrate"
	"inferencerig/core/profiles"
)

//nolint:gocyclo // One migration scenario verifies preview, apply, and source immutability.
func TestYAMLImporterPreservesArgumentsAndSource(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "base.yaml")
	source := "name: demo\nmodel: community/model\nhost: 127.0.0.1\nport: 8080\nmlx_args:\n  adapter-path: /adapters/demo\n  trust-remote-code: true\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	importer := NewYAMLImporter(root, "127.0.0.1", 9000)
	ctx := context.Background()
	first, err := importer.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := importer.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].ProfileYAML != second[0].ProfileYAML {
		t.Fatalf("candidates = %#v", first)
	}
	for _, want := range []string{"backend: mlx", "adapter-path: /adapters/demo", "trust-remote-code: true"} {
		if !strings.Contains(first[0].ProfileYAML, want) {
			t.Fatalf("profile = %q", first[0].ProfileYAML)
		}
	}
	store := profiles.NewFileStore(t.TempDir(), 0, func(string) (profiles.BackendValidator, error) {
		return New(Options{}), nil
	})
	service := coremigrate.NewService(store)
	plan, err := service.Preview(ctx, importer)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := service.Apply(ctx, plan); err != nil || len(result.Created) != 1 {
		t.Fatalf("apply = %#v, err = %v", result, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != source {
		t.Fatalf("source changed: %q, err = %v", after, err)
	}
}
