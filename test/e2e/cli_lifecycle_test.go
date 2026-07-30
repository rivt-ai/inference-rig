//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLIControlLifecycle is the canonical process E2E: one representative
// backend driven end to end through the compiled binary, from daemon start to
// real generated tokens to a clean shutdown.
//
// It deliberately does not sweep the backend matrix or malformed input; the
// package suite owns those. What only this test can prove is that the compiled
// command registration, control socket, RPC service, profile store,
// materialization, supervisor, PID handling, and a real llama.cpp process
// still fit together.
func TestCLIControlLifecycle(t *testing.T) {
	rig := newLlamacppRig(t)
	daemon := rig.startControl()

	// 1. The daemon answers the three commands every other command depends on.
	assertDaemonIntrospection(t, rig)

	// 2. Create a real profile from a YAML file, through the CLI.
	port := freePort(t)
	rig.cli("profile", "create", "e2e", rig.writeProfile("e2e", port))
	if listed := rig.cli("profile", "list"); !strings.Contains(listed, "e2e") {
		t.Fatalf("profile list = %s", listed)
	}

	// 3. The model resolves to a single-file plan, which is what a GGUF backend
	// must produce; a multi-file plan here would mean the wrong policy ran.
	resolved := rig.cliJSON("model", "resolve", "e2e")
	plan, _ := resolved["plan"].(map[string]any)
	if plan == nil || plan["multiFile"] == true {
		t.Fatalf("resolve plan = %v", resolved)
	}

	// 4. Start it. The real llama-server must load the GGUF and become ready.
	started := rig.cliJSON("runtime", "start", "e2e")
	if state := runtimeState(started); state != "running" {
		t.Fatalf("start state = %q (%v)", state, started)
	}
	if state := runtimeState(rig.cliJSON("runtime", "status", "e2e")); state != "running" {
		t.Fatalf("status after start = %q", state)
	}

	// 5. The generated models.ini is what pointed the engine at the model, so
	// its content is part of the contract this test covers.
	assertGeneratedINI(t, rig)

	// 6. The assertion the old live tests were missing: readiness is not
	// inference. Require actual generated tokens.
	if reply := chatCompletion(t, "http://127.0.0.1:"+itoa(port), "e2e"); strings.TrimSpace(reply) == "" {
		t.Fatal("engine returned an empty completion")
	}

	// 7. Restart and stop through the CLI, then confirm the reported state.
	if state := runtimeState(rig.cliJSON("runtime", "restart", "e2e")); state != "running" {
		t.Fatalf("restart state = %q", state)
	}
	rig.cli("runtime", "stop", "e2e")
	if state := runtimeState(rig.cliJSON("runtime", "status", "e2e")); state == "running" {
		t.Fatal("runtime still running after stop")
	}

	// 8. Every operation above should have been audited in order.
	assertAuditSequence(t, rig, "profile.put", "runtime.start", "runtime.restart", "runtime.stop")

	// 9. A clean shutdown must leave nothing behind for the next run to trip on.
	daemon.stop(t)
	assertCleanShutdown(t, rig)
}

func assertDaemonIntrospection(t *testing.T, rig *rig) {
	t.Helper()
	if health := rig.cliJSON("health"); health["ok"] != true {
		t.Fatalf("health = %v", health)
	}
	if backends := rig.cli("backend", "list"); !strings.Contains(backends, "llamacpp") {
		t.Fatalf("backend list did not report llamacpp: %s", backends)
	}
	info := rig.cliJSON("info")
	if build, _ := info["build"].(map[string]any); build["version"] == nil || info["backends"] == nil {
		t.Fatalf("info = %v", info)
	}
}

func assertGeneratedINI(t *testing.T, rig *rig) {
	t.Helper()
	path := filepath.Join(rig.home, "generated", "llamacpp", "models.ini")
	generated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated models.ini: %v", err)
	}
	if !strings.Contains(string(generated), "[e2e]") || !strings.Contains(string(generated), rig.modelPath) {
		t.Fatalf("generated models.ini = %s", generated)
	}
}

func assertCleanShutdown(t *testing.T, rig *rig) {
	t.Helper()
	if fileExists(rig.socketPath()) {
		t.Errorf("control socket %s survived shutdown", rig.socketPath())
	}
	if fileExists(rig.pidPath("inferencerig")) {
		t.Errorf("control PID file survived shutdown")
	}
}

// runtimeState pulls the state out of any response carrying a runtime status.
func runtimeState(response map[string]any) string {
	status, _ := response["status"].(map[string]any)
	state, _ := status["state"].(string)
	return state
}

// assertAuditSequence checks that the named actions were recorded in order,
// tolerating other events interleaved between them.
func assertAuditSequence(t *testing.T, rig *rig, actions ...string) {
	t.Helper()
	events := rig.cliJSON("events", "list")
	recorded, _ := events["events"].([]any)
	// The store lists newest first; compare against the order they happened in.
	var order []string
	for i := len(recorded) - 1; i >= 0; i-- {
		event, _ := recorded[i].(map[string]any)
		if action, _ := event["action"].(string); action != "" {
			order = append(order, action)
		}
	}
	remaining := actions
	for _, action := range order {
		if len(remaining) > 0 && action == remaining[0] {
			remaining = remaining[1:]
		}
	}
	if len(remaining) > 0 {
		t.Errorf("audit events %v did not contain %v in order; got %v", actions, remaining, order)
	}
}
