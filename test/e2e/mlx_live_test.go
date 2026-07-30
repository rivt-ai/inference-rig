//go:build e2emlx

package e2e

import (
	"fmt"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

// TestMLXInference is the Apple Silicon hardware gate. Unlike the llama.cpp
// suite it cannot run on an ordinary PR runner, so it is scheduled, dispatched,
// and run on release tags instead.
//
// It goes through the compiled control daemon and RPC rather than calling the
// backend directly: a second, backend-only path would prove the engine works
// while leaving the code a user actually runs untested. And like every test
// here it requires generated tokens — a green job that only proved a port
// opened is the signal this plan set out to remove.
func TestMLXInference(t *testing.T) {
	// A wrong runner is a workflow configuration error, not a condition to skip
	// over. Skipping is what let the old live job pass with nothing executed.
	if goruntime.GOOS != "darwin" || goruntime.GOARCH != "arm64" {
		t.Fatalf("MLX validation requires Apple Silicon; this runner is %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
	python := requireEnv(t, mlxPythonEnv)
	model := requireEnv(t, mlxModelEnv)
	t.Logf("engine mlx via %s, model %s, host %s/%s", python, model, goruntime.GOOS, goruntime.GOARCH)

	// The MLX backend launches `python3 -m mlx_lm server`, so putting the job's
	// virtual environment first on PATH is what selects the pinned interpreter —
	// the same resolution a user's install performs.
	rig := newRig(t, filepath.Dir(python))
	rig.startControl()

	port := freePort(t)
	profile := filepath.Join(rig.home, "mlx.yaml")
	writeFile(t, profile, fmt.Sprintf(
		"version: 1\nname: mlx\nbackend: mlx\nmodel:\n  source: %s\nlisten:\n  host: 127.0.0.1\n  port: %d\n",
		model, port))
	rig.cli("profile", "create", "mlx", profile)

	if state := runtimeState(rig.cliJSON("runtime", "start", "mlx")); state != "running" {
		t.Fatalf("mlx runtime did not start: %s", state)
	}
	if reply := chatCompletion(t, "http://127.0.0.1:"+itoa(port), model); strings.TrimSpace(reply) == "" {
		t.Fatal("MLX returned an empty completion")
	}
	rig.cli("runtime", "stop", "mlx")
}
