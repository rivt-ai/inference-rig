package strata

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"inferencerig/backends"
	"inferencerig/backends/backendtest"
	"inferencerig/core/profiles"
)

// stubRunner fakes git clone and venv creation by writing the files the
// installer checks for.
type stubRunner struct {
	calls [][]string
}

func (r *stubRunner) Run(_ context.Context, _ string, _ io.Writer, name string, args ...string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	switch {
	case len(args) > 0 && args[0] == "clone":
		dir := args[len(args)-1]
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "setup.py"), nil, 0o600)
	case len(args) >= 3 && args[1] == "venv":
		python := filepath.Join(args[2], "bin", "python")
		if err := os.MkdirAll(filepath.Dir(python), 0o700); err != nil {
			return err
		}
		return os.WriteFile(python, nil, 0o700)
	}
	return nil
}

func newTestBackend(t *testing.T, runner *stubRunner) *Backend {
	t.Helper()
	root := t.TempDir()
	return New(Options{
		ModelStorageDir: filepath.Join(root, "models"),
		PIDDir:          filepath.Join(root, "run"),
		EngineRoot:      filepath.Join(root, "engine"),
		Python:          "/usr/bin/python3",
		Git:             "/usr/bin/git",
		runner:          runner,
		goos:            "linux",
		goarch:          "amd64",
	})
}

func TestBackendContract(t *testing.T) {
	backendtest.RunContractTests(t, func() backends.Backend { return newTestBackend(t, &stubRunner{}) })
}

func profile(source string, args map[string]any) profiles.Profile {
	return profiles.Profile{
		Name: "demo", Backend: Name,
		Model:      profiles.ModelSpec{Source: source},
		Listen:     profiles.ListenSpec{Host: "127.0.0.1", Port: 8080},
		EngineArgs: args,
	}
}

func TestConfigSourceRunsServerFromItsCheckout(t *testing.T) {
	b := newTestBackend(t, &stubRunner{})
	spec, err := b.LaunchSpec(profile("/opt/Strata/strata-iq2_xs.json", map[string]any{"gpu": "0", "lazy": true, "fit-max-tokens": false}), backends.Materialization{})
	if err != nil || spec.BuildErr != nil {
		t.Fatalf("spec = %#v, err = %v", spec, err)
	}
	want := []string{
		"/opt/Strata/serve/server.py", "--engine", "strata", "--config", "/opt/Strata/strata-iq2_xs.json",
		"--host", "127.0.0.1", "--port", "8080", "--gpu", "0", "--lazy",
	}
	if spec.Executable != "/opt/Strata/.venv/bin/python" || !reflect.DeepEqual(spec.Argv, want) {
		t.Fatalf("exe = %q, argv = %#v", spec.Executable, spec.Argv)
	}
	if spec.Env != nil || spec.ReadinessPath != "/health" {
		t.Fatalf("env = %#v, readiness = %q", spec.Env, spec.ReadinessPath)
	}
}

func TestSizeSourceNeedsInstallThenRunsSetup(t *testing.T) {
	b := newTestBackend(t, &stubRunner{})
	p := profile("IQ2_XS", map[string]any{"context": 65536, "kv": "int8"})
	spec, _ := b.LaunchSpec(p, backends.Materialization{})
	if !errors.Is(spec.BuildErr, errNotInstalled) {
		t.Fatalf("BuildErr = %v, want errNotInstalled", spec.BuildErr)
	}
	result, err := b.Install(context.Background(), backends.InstallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Dir(filepath.Dir(filepath.Dir(result.Path)))
	spec, _ = b.LaunchSpec(p, backends.Materialization{})
	want := []string{
		filepath.Join(checkout, "setup.py"), "--model", "IQ2_XS", "--yes", "--no-browser",
		"--data-dir", filepath.Join(filepath.Dir(checkout), "data"), "--host", "127.0.0.1", "--port", "8080", "--context", "65536", "--kv", "int8",
	}
	if spec.BuildErr != nil || spec.Executable != result.Path || !reflect.DeepEqual(spec.Argv, want) {
		t.Fatalf("spec = %#v", spec)
	}
	if spec.Env["STRATA_EXECV"] != "1" || spec.ReadinessTimeout != setupReadiness {
		t.Fatalf("env = %#v, timeout = %v", spec.Env, spec.ReadinessTimeout)
	}
}

func TestValidateRejectsReservedArgs(t *testing.T) {
	b := newTestBackend(t, &stubRunner{})
	for source, key := range map[string]string{"IQ2_XS": "no-start", "/opt/Strata/strata.json": "config"} {
		if _, err := b.ValidateProfile(profile(source, map[string]any{key: true})); err == nil {
			t.Fatalf("%s: engine_args.%s accepted", source, key)
		}
	}
}

func TestInstallClonesPinnedReleaseAndIsIdempotent(t *testing.T) {
	runner := &stubRunner{}
	b := newTestBackend(t, runner)
	first, err := b.Install(context.Background(), backends.InstallOptions{})
	if err != nil || !first.Changed || first.Version != ManagedVersion {
		t.Fatalf("first = %#v, err = %v", first, err)
	}
	clone := runner.calls[0]
	if clone[0] != "/usr/bin/git" || !slices.Contains(clone, ManagedVersion) || !slices.Contains(clone, RepositoryURL) {
		t.Fatalf("clone = %#v", clone)
	}
	if len(runner.calls) != 4 {
		t.Fatalf("calls = %#v", runner.calls)
	}
	second, err := b.Install(context.Background(), backends.InstallOptions{})
	if err != nil || second.Changed || len(runner.calls) != 4 {
		t.Fatalf("second = %#v, err = %v, calls = %d", second, err, len(runner.calls))
	}
}

func TestInstallRejectsPathLikeVersion(t *testing.T) {
	b := newTestBackend(t, &stubRunner{})
	if _, err := b.Install(context.Background(), backends.InstallOptions{Version: "../x"}); err == nil {
		t.Fatal("path-like version accepted")
	}
}

func TestFitCountsRAMAndVRAMTogether(t *testing.T) {
	b := newTestBackend(t, &stubRunner{})
	est, _ := b.Fit(profiles.Profile{}, 40<<30, backends.HostResources{AvailableRAMBytes: 36 << 30, VRAMBytes: 12 << 30})
	if est.Level == backends.FitTooLarge || est.AvailableBytes != 48<<30 {
		t.Fatalf("est = %#v", est)
	}
}

func TestConfigOutsideACheckoutRunsFromTheManagedOne(t *testing.T) {
	b := newTestBackend(t, &stubRunner{})
	result, err := b.Install(context.Background(), backends.InstallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Dir(filepath.Dir(filepath.Dir(result.Path)))
	cfg := filepath.Join(t.TempDir(), "strata-coder.json")
	spec, _ := b.LaunchSpec(profile(cfg, nil), backends.Materialization{})
	if spec.BuildErr != nil || spec.Executable != result.Path || spec.Argv[0] != filepath.Join(checkout, "serve", "server.py") {
		t.Fatalf("spec = %#v", spec)
	}
}
