package llamacpp

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
func TestINIImporterIsDeterministicAndReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.ini")
	source := "version = 1\n[*]\nctx-size = 2048\n[zeta]\nmodel = /models/z.gguf\n[alpha]\nmodel = /models/a.gguf\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	importer := NewINIImporter(path, "127.0.0.1", 8080)
	ctx := context.Background()
	first, err := importer.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := importer.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].Name != "alpha" || first[0].ProfileYAML != second[0].ProfileYAML {
		t.Fatalf("candidates = %#v", first)
	}
	if !strings.Contains(first[0].ProfileYAML, "ctx-size: \"2048\"") {
		t.Fatalf("profile = %q", first[0].ProfileYAML)
	}
	store := profiles.NewFileStore(t.TempDir(), 0, func(string) (profiles.BackendValidator, error) {
		return New(Options{}), nil
	})
	service := coremigrate.NewService(store)
	plan, err := service.Preview(ctx, importer)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := service.Apply(ctx, plan); err != nil || len(result.Created) != 2 {
		t.Fatalf("apply = %#v, err = %v", result, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != source {
		t.Fatalf("source changed: %q, err = %v", after, err)
	}
}
