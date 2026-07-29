package mlx

import (
	"path/filepath"
	"testing"

	"inferencerig/backends"
	"inferencerig/backends/backendtest"
)

func newTestBackend(t *testing.T) *Backend {
	t.Helper()
	root := t.TempDir()
	return New(Options{
		ModelStorageDir: filepath.Join(root, "models"),
		PIDDir:          filepath.Join(root, "run"),
		EngineRoot:      filepath.Join(root, "engine"),
		Executable:      "/usr/bin/python3",
		runner:          &stubRunner{},
		goos:            "darwin",
		goarch:          "arm64",
	})
}

func TestBackendContract(t *testing.T) {
	backendtest.RunContractTests(t, func() backends.Backend { return newTestBackend(t) })
}
