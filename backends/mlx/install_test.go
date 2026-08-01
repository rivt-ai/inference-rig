package mlx

import (
	"context"
	"errors"
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
	lock := filepath.Join(root, "venv-"+ManagedVersion, lockFileName)
	wantInstall := []string{first.Path, "-m", "pip", "install", "--no-deps", "-r", lock}
	if len(runner.calls) != 3 || !reflect.DeepEqual(runner.calls[1], wantInstall) {
		t.Fatalf("calls = %#v", runner.calls)
	}
	if content, err := os.ReadFile(lock); err != nil || string(content) != requirementsLock {
		t.Fatalf("lock file = %q, err = %v", content, err)
	}
	second, err := b.Install(context.Background(), backends.InstallOptions{})
	if err != nil || second.Changed || len(runner.calls) != 3 {
		t.Fatalf("second = %#v, err = %v, calls = %#v", second, err, runner.calls)
	}
	if b.executable() != first.Path {
		t.Fatalf("executable = %q", b.executable())
	}
}

func TestInstallRecordsProvenance(t *testing.T) {
	root := t.TempDir()
	b := New(Options{
		EngineRoot: root, Executable: "/usr/bin/python3", runner: &stubRunner{},
		goos: "darwin", goarch: "arm64",
	})
	if _, err := b.Install(context.Background(), backends.InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	state, err := backends.ReadInstallState(root)
	if err != nil || state.Active == nil {
		t.Fatalf("state = %#v, err = %v", state, err)
	}
	record := *state.Active
	if record.Backend != Name || record.Version != ManagedVersion || record.Source != "pypi:"+lockFileName {
		t.Fatalf("record = %#v", record)
	}
	if record.Platform != "darwin/arm64" || record.Digest == "" || record.InstalledAt.IsZero() {
		t.Fatalf("record = %#v", record)
	}
}

// TestInstallRejectsUnlockedVersion is the whole supply-chain guarantee: the
// only installable package set is the one this build ships a lock for.
func TestInstallRejectsUnlockedVersion(t *testing.T) {
	b := New(Options{
		EngineRoot: t.TempDir(), Executable: "/usr/bin/python3", runner: &stubRunner{},
		goos: "darwin", goarch: "arm64",
	})
	_, err := b.Install(context.Background(), backends.InstallOptions{Version: "0.0.1"})
	if !errors.Is(err, ErrUnlockedVersion) {
		t.Fatalf("install of an unlocked version = %v, want ErrUnlockedVersion", err)
	}
}

func TestRollbackRestoresPreviousEnvironment(t *testing.T) {
	root := t.TempDir()
	b := New(Options{
		EngineRoot: root, Executable: "/usr/bin/python3", runner: &stubRunner{},
		goos: "darwin", goarch: "arm64",
	})
	if _, err := b.Install(context.Background(), backends.InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Rollback(context.Background()); !errors.Is(err, backends.ErrNoPreviousInstall) {
		t.Fatalf("rollback with one install = %v, want ErrNoPreviousInstall", err)
	}
	// A managed upgrade to a build with a newer lock: same code path, a
	// different version-scoped environment, so the old one survives for rollback.
	previous := backends.InstallRecord{
		Backend: Name, Version: "0.0.0", Executable: filepath.Join(root, "venv-0.0.0", "bin", "python"),
		Directory: filepath.Join(root, "venv-0.0.0"),
	}
	state, err := backends.ReadInstallState(root)
	if err != nil {
		t.Fatal(err)
	}
	state.Previous = &previous
	if err := backends.WriteInstallState(root, state); err != nil {
		t.Fatal(err)
	}
	back, err := b.Rollback(context.Background())
	if err != nil || back.Version != "0.0.0" || back.Path != previous.Executable {
		t.Fatalf("rollback = %#v, err = %v", back, err)
	}
	if exe, ok := b.installer.activeExecutable(); ok || exe != "" {
		t.Fatalf("activeExecutable = %q, %v; the restored environment is not on disk", exe, ok)
	}
}

func TestInstallStatusFindsUsableHostPython(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	backend := New(Options{EngineRoot: t.TempDir(), Executable: executable, runner: &stubRunner{}})
	status, err := backend.InstallStatus(context.Background())
	if err != nil || !status.Installed || status.Managed || status.Path != executable {
		t.Fatalf("status = %#v, err = %v", status, err)
	}
}

func TestInstallRejectsUnsupportedHost(t *testing.T) {
	b := New(Options{EngineRoot: t.TempDir(), goos: "linux", goarch: "arm64"})
	if _, err := b.Install(context.Background(), backends.InstallOptions{}); err == nil {
		t.Fatal("unsupported host accepted")
	}
}
