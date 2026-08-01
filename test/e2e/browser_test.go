//go:build e2e && e2ebrowser

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestBrowserProfileLifecycle runs the Chromium workflow against this package's
// process harness rather than against a mock.
//
// It layers the e2ebrowser tag on top of e2e rather than replacing it, so the
// harness compiles as one consistent whole and -run selects this test alone.
//
// The Go side owns the environment — compiled binaries, a private home, the
// control socket, the gateway, the pinned engine and model, a reserved port —
// and hands Playwright only a URL and a token. That split is deliberate: the
// browser test then has nothing to fake, and `pnpm test:e2e` stays usable
// against any already-running harness.
func TestBrowserProfileLifecycle(t *testing.T) {
	rig := newLlamacppRig(t)
	rig.startControl()
	rig.startGateway()

	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("pnpm", "run", "test:e2e")
	command.Dir = filepath.Join(root, "webui")
	command.Env = append(os.Environ(),
		"INFERENCERIG_E2E_BASE_URL="+rig.gatewayURL(),
		"INFERENCERIG_E2E_TOKEN="+rig.token,
		"INFERENCERIG_E2E_MODEL_SOURCE="+rig.modelPath,
		"INFERENCERIG_E2E_PROFILE_PORT="+itoa(freePort(t)),
	)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		t.Fatalf("browser suite failed: %v (traces and screenshots are under webui/test-results)", err)
	}
}
