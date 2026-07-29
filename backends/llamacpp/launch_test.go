package llamacpp

import (
	"path/filepath"
	"strings"
	"testing"

	"inferencerig/backends"
)

func TestLaunchSpecRouterArgv(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "models.ini")
	storage := filepath.Join(dir, "models")
	b := New(Options{GeneratedININPath: ini, ModelStorageDir: storage, PIDDir: dir, ModelsMax: 3})
	spec, err := b.LaunchSpec(demoProfile("demo", "/m.gguf"), backends.Materialization{})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Executable != defaultExecutable || spec.ReadinessPath != readinessPath || spec.Name != routerName {
		t.Fatalf("spec = %#v", spec)
	}
	argv := strings.Join(spec.Argv, " ")
	for _, want := range []string{"--models-dir " + storage, "--models-preset " + ini, "--models-max 3", "--host 127.0.0.1", "--port 8080"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("argv %q missing %q", argv, want)
		}
	}
	if spec.BuildErr != nil {
		t.Fatalf("unexpected BuildErr: %v", spec.BuildErr)
	}
}
