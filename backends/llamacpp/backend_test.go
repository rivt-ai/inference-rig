package llamacpp

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"inferencerig/backends"
	"inferencerig/backends/backendtest"
)

// stubFetcher provisions a fake llama-server offline so the install state
// machine runs hermetically through the contract suite.
type stubFetcher struct{ version string }

func (f stubFetcher) Resolve(_ context.Context, _ Accel, version string) (Release, error) {
	v := f.version
	if version != "" {
		v = version
	}
	return Release{Version: v}, nil
}

func (f stubFetcher) Fetch(_ context.Context, _ Release, _ Accel, dir string, _ io.Writer) (string, error) {
	exe := filepath.Join(dir, defaultExecutable)
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		return "", err
	}
	return exe, nil
}

// newTestBackend builds a fully hermetic backend: temp generated/storage/engine
// dirs and an offline install fetcher. It is the real *Backend type, so the
// contract suite exercises the real facets (only the network payload is stubbed).
func newTestBackend(t *testing.T) *Backend {
	t.Helper()
	dir := t.TempDir()
	return New(Options{
		ModelStorageDir:   filepath.Join(dir, "models"),
		GeneratedININPath: filepath.Join(dir, "generated", "models.ini"),
		PIDDir:            filepath.Join(dir, "run"),
		EngineRoot:        filepath.Join(dir, "engine"),
		Fetcher:           stubFetcher{version: "b-test"},
	})
}

func TestLlamaCPPBackendContract(t *testing.T) {
	backendtest.RunContractTests(t, func() backends.Backend { return newTestBackend(t) })
}

func TestCapabilitiesSelfConsistent(t *testing.T) {
	c := newTestBackend(t).Capabilities()
	if !c.SingleFileArtifacts || c.MultiFileArtifacts {
		t.Fatalf("capabilities = %#v; want single-file only", c)
	}
	if !c.DiscreteVRAM || !c.ManagedInstall {
		t.Fatalf("capabilities = %#v; want discrete-VRAM managed-install", c)
	}
}
