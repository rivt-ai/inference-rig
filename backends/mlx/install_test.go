package mlx

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"inferencerig/backends"
)

type stubRunner struct {
	calls [][]string
}

func (r *stubRunner) Run(_ context.Context, _ string, _ io.Writer, name string, args ...string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	if len(args) >= 3 && args[0] == "-m" && args[1] == "venv" {
		python := filepath.Join(args[2], "bin", "python")
		if err := os.MkdirAll(filepath.Dir(python), 0o700); err != nil {
			return err
		}
		return os.WriteFile(python, nil, 0o700)
	}
	return nil
}

func TestInstallCreatesPinnedEnvironmentAndIsIdempotent(t *testing.T) {
	runner := &stubRunner{}
	root := t.TempDir()
	b := New(Options{
		EngineRoot: root, Executable: "/usr/bin/python3", runner: runner,
		goos: "darwin", goarch: "arm64",
	})
	first, err := b.Install(context.Background(), backends.InstallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed || first.Version != ManagedVersion {
		t.Fatalf("first = %#v", first)
	}
	wantInstall := []string{first.Path, "-m", "pip", "install", "--upgrade", "mlx-lm==" + ManagedVersion}
	if len(runner.calls) != 3 || !reflect.DeepEqual(runner.calls[1], wantInstall) {
		t.Fatalf("calls = %#v", runner.calls)
	}
	second, err := b.Install(context.Background(), backends.InstallOptions{})
	if err != nil || second.Changed || len(runner.calls) != 3 {
		t.Fatalf("second = %#v, err = %v, calls = %#v", second, err, runner.calls)
	}
	if b.executable() != first.Path {
		t.Fatalf("executable = %q", b.executable())
	}
}

func TestInstallRejectsUnsupportedHost(t *testing.T) {
	b := New(Options{EngineRoot: t.TempDir(), goos: "linux", goarch: "arm64"})
	if _, err := b.Install(context.Background(), backends.InstallOptions{}); err == nil {
		t.Fatal("unsupported host accepted")
	}
}
