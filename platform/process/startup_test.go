package process

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"inferencerig/config"
	"inferencerig/platform/pidfile"
)

// helperEnv switches the test binary into a stand-in for the daemon.
// StartDetached re-execs os.Executable() — which under `go test` is the test
// binary — and copies os.Environ(), so a helper process needs no fixture
// binary and no build step.
const helperEnv = "INFERENCERIG_STARTUP_HELPER"

const helperMarker = "helper: refusing to start"

func TestStartupHelperProcess(t *testing.T) {
	switch os.Getenv(helperEnv) {
	case "die":
		// Stderr is what AttachLogs captures into the service log.
		if _, err := os.Stderr.WriteString("Error: " + helperMarker + "\n"); err != nil {
			t.Fatal(err)
		}
		os.Exit(1)
	case "live":
		time.Sleep(5 * time.Second)
		os.Exit(0)
	}
}

// startHelper runs the helper as a detached "daemon" under a temp home.
func startHelper(t *testing.T, mode string) error {
	t.Helper()
	t.Setenv(config.ProjectHomeEnv, t.TempDir())
	t.Setenv(helperEnv, mode)
	startupGrace = 200 * time.Millisecond
	t.Cleanup(func() { startupGrace = 300 * time.Millisecond })
	return StartDetached(config.ProjectName, "-test.run=TestStartupHelperProcess")
}

// The bug this whole change exists for: a daemon that dies on a bad config used
// to look exactly like a healthy start.
func TestStartDetachedReportsImmediateExit(t *testing.T) {
	err := startHelper(t, "die")

	var failure *StartupError
	if !errors.As(err, &failure) {
		t.Fatalf("StartDetached err = %v, want *StartupError", err)
	}
	if failure.PID <= 0 {
		t.Errorf("PID = %d, want the exited child's pid", failure.PID)
	}
	if !strings.Contains(failure.Tail, helperMarker) {
		t.Errorf("Tail = %q, want the child's own error", failure.Tail)
	}
	if !strings.Contains(failure.Error(), "inferencerig doctor") {
		t.Errorf("Error() = %q, want a pointer to doctor", failure.Error())
	}
	// Summary feeds a single truncating status line, so it must stay one line
	// and must not drag the tail along.
	if strings.Contains(failure.Summary(), "\n") || strings.Contains(failure.Summary(), helperMarker) {
		t.Errorf("Summary() = %q, want one line without the tail", failure.Summary())
	}
	// A dead daemon must not leave a PID file claiming it is up.
	path := filepath.Join(os.Getenv(config.ProjectHomeEnv), "run", config.ProjectName+".pid")
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("PID file still present after an immediate exit: %v", statErr)
	}
}

func TestStartDetachedAcceptsSurvivingProcess(t *testing.T) {
	if err := startHelper(t, "live"); err != nil {
		t.Fatalf("StartDetached err = %v, want nil for a process that stays up", err)
	}
	t.Cleanup(func() { _ = StopDetached(config.ProjectName) })

	status, err := StatusDetached(config.ProjectName)
	if err != nil || !status.Running {
		t.Fatalf("status = %#v, err = %v", status, err)
	}
}

func TestCheckStartupFailure(t *testing.T) {
	// A pid that is genuinely dead and already reaped, so it cannot be a
	// zombie that kill(pid, 0) would still report as alive.
	reaped := exec.Command("/bin/sh", "-c", "exit 0")
	if err := reaped.Run(); err != nil {
		t.Fatal(err)
	}
	deadPID := reaped.Process.Pid

	tests := []struct {
		name    string
		pid     int
		write   bool
		wantErr bool
	}{
		{name: "no pid file", write: false},
		{name: "live process", pid: os.Getpid(), write: true},
		{name: "dead process", pid: deadPID, write: true, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writePIDFixture(t, test.pid, test.write)

			err := CheckStartupFailure(config.ProjectName)
			if (err != nil) != test.wantErr {
				t.Fatalf("CheckStartupFailure = %v, wantErr %v", err, test.wantErr)
			}
			if !test.write {
				return
			}
			// Read-only by contract: a diagnostic calls this, so it must not
			// clean up the evidence the way pidfile.Running would.
			if _, statErr := os.Stat(path); statErr != nil {
				t.Errorf("PID file was removed: %v", statErr)
			}
		})
	}
}

func writePIDFixture(t *testing.T, pid int, write bool) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)
	path := filepath.Join(home, "run", config.ProjectName+".pid")
	if !write {
		return path
	}
	if err := pidfile.New(path).Write(pid); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSignificantTailPrefersTheErrorLine(t *testing.T) {
	// Cobra prints usage after the error unless silenced, so the literal last
	// lines of a service log are often flag docs rather than the failure.
	tail := "Error: parse config: bad\nUsage:\n  inferencerig serve [flags]\n\nFlags:\n  -d, --detach\n"
	got := significantTail(tail)
	if len(got) != 1 || got[0] != "Error: parse config: bad" {
		t.Errorf("significantTail = %q, want just the error line", got)
	}

	// Without an Error: line, fall back to the last non-blank lines.
	got = significantTail("one\n\ntwo\nthree\nfour\n")
	if strings.Join(got, ",") != "two,three,four" {
		t.Errorf("significantTail = %q, want the last three non-blank lines", got)
	}
}

func TestStartupErrorSummaryWithoutPID(t *testing.T) {
	failure := &StartupError{Name: "inferencerig"}
	if strings.Contains(failure.Summary(), "pid "+strconv.Itoa(0)) {
		t.Errorf("Summary() = %q, want no pid when it is unknown", failure.Summary())
	}
}
