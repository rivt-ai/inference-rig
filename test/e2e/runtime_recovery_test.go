//go:build e2e

package e2e

import (
	"strings"
	"syscall"
	"testing"
)

func TestControlRestartAdoptsSurvivingRuntime(t *testing.T) {
	rig := newLlamacppRig(t)
	daemon := rig.startControl()
	port := freePort(t)
	rig.cli("profile", "create", "recovery", rig.writeProfile("recovery", port))
	started := rig.cliJSON("runtime", "start", "recovery")
	pid := runtimePID(started)
	if pid == 0 {
		t.Fatalf("start did not report an engine PID: %v", started)
	}

	daemon.crash(t)
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("engine %d did not survive daemon crash: %v", pid, err)
	}

	restarted := rig.startControl()
	status := rig.cliJSON("runtime", "status", "recovery")
	if runtimeState(status) != "running" || runtimePID(status) != pid {
		t.Fatalf("recovered status = %v, want running PID %d", status, pid)
	}
	if events := rig.cli("events", "list"); !strings.Contains(events, "valid_adoptee") {
		t.Fatalf("recovery classification missing from events: %s", events)
	}

	restarted.stop(t)
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("adopted engine %d survived graceful daemon shutdown", pid)
	}
}

func runtimePID(response map[string]any) int {
	status, _ := response["status"].(map[string]any)
	processes, _ := status["processes"].([]any)
	if len(processes) == 0 {
		return 0
	}
	process, _ := processes[0].(map[string]any)
	pid, _ := process["pid"].(float64)
	return int(pid)
}
