package configstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"inferencerig/config"
)

func TestFileStoreRead(t *testing.T) {
	path := writeConfig(t, `listen_addr: "127.0.0.1:9999"
model_storage_dir: /data/models
`)
	store := NewFileStore(path, 1024*1024)
	ctx := context.Background()

	cfg, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:9999" || cfg.ModelStorageDir != "/data/models" {
		t.Fatalf("cfg = %#v", cfg)
	}
}

func TestFileStoreRejectsInvalidContent(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "config.yaml"), 8)
	if err := store.Validate(context.Background(), " \n"); err == nil {
		t.Fatal("expected empty error")
	}
	if err := store.Validate(context.Background(), strings.Repeat("x", 16)); err == nil {
		t.Fatal("expected too large error")
	}
	if err := store.Validate(context.Background(), "unknown_field: true\n"); err == nil {
		t.Fatal("expected malformed unknown-field error")
	}
}

func TestSetStartupServicesPreservesDocument(t *testing.T) {
	path := writeConfig(t, "# keep this comment\nlisten_addr: 127.0.0.1:7000\nstartup_services: [control, web]\nmodel_storage_dir: /data/models\n")
	store := NewFileStore(path, DefaultLimitBytes)
	if _, err := store.SetStartupServices(context.Background(), []string{config.StartupServiceControl}); err != nil {
		t.Fatal(err)
	}
	doc, err := store.Read(context.Background())
	if err != nil || len(doc.StartupServices) != 1 || doc.StartupServices[0] != config.StartupServiceControl {
		t.Fatalf("document=%#v error=%v", doc, err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# keep this comment") || !strings.Contains(string(data), "model_storage_dir: /data/models") {
		t.Fatalf("unrelated config changed: %s", data)
	}
}

func TestSetStartupServicesAddsMissingKeyAndValidates(t *testing.T) {
	path := writeConfig(t, "listen_addr: 127.0.0.1:7000\n")
	store := NewFileStore(path, DefaultLimitBytes)
	if _, err := store.SetStartupServices(context.Background(), []string{config.StartupServiceWeb}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if _, err := store.SetStartupServices(context.Background(), []string{"invalid"}); err == nil {
		t.Fatal("expected invalid startup service error")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("invalid mutation changed config")
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, dir)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
