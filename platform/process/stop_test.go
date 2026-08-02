package process

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"inferencerig/config"
)

// TestStopDetachedHelperSleep is not a real test: it is re-exec'd as a plain
// child process by TestStopDetachedTerminatesARunningProcessAndClearsItsPIDFile
// so the child is a copy of this test binary (see the identity check in
// StopDetached) rather than an unrelated command like sleep(1).
func TestStopDetachedHelperSleep(t *testing.T) {
	if os.Getenv("STOP_DETACHED_HELPER_SLEEP") != "1" {
		t.Skip("helper process, not a real test")
	}
	time.Sleep(2 * time.Minute)
}

// StopDetached is the escalation path: terminate, wait, and kill if the process
// will not go. It is also the only code that clears a PID file for a process it
// did not start, so a bug here either leaves a daemon running or leaves a stale
// PID file that makes the next start refuse.

func TestStopDetachedIsANoOpWithoutAPIDFile(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir())
	if err := StopDetached(config.ProjectName); err != nil {
		t.Fatalf("StopDetached with no PID file = %v, want nil", err)
	}
}

// A PID file naming a process that no longer exists must be cleared rather than
// reported as an error; otherwise a crashed daemon can never be restarted.
func TestStopDetachedClearsStalePIDFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)
	path := writePID(t, home, deadPID(t))
	if err := StopDetached(config.ProjectName); err != nil {
		t.Fatalf("StopDetached = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("stale PID file survived: %v", err)
	}
}

func TestStopDetachedTerminatesARunningProcessAndClearsItsPIDFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)

	// A real child process, so the terminate/wait/kill escalation runs against
	// a real signal path rather than a stub that cannot refuse to die. It must
	// also be a copy of this test binary, not an arbitrary command: StopDetached
	// now refuses to signal a PID unless it is running the same executable as
	// the caller (identity.go), matching how a real daemon is the same binary
	// as the CLI stopping it.
	self, err := os.Executable()
	if err != nil {
		t.Skipf("cannot resolve own executable: %v", err)
	}
	child := exec.Command(self, "-test.run=TestStopDetachedHelperSleep")
	child.Env = append(os.Environ(), "STOP_DETACHED_HELPER_SLEEP=1")
	if err := child.Start(); err != nil {
		t.Skipf("cannot start a child process here: %v", err)
	}
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})
	path := writePID(t, home, child.Process.Pid)

	if err := StopDetached(config.ProjectName); err != nil {
		t.Fatalf("StopDetached = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("PID file survived a successful stop: %v", err)
	}
	done := make(chan struct{})
	go func() { _ = child.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("child process was still running after StopDetached returned")
	}
}

// StartDetached must refuse rather than orphan the process already recorded in
// the PID file.
func TestStartDetachedRefusesWhenAlreadyRunning(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)
	writePID(t, home, os.Getpid())
	err := StartDetached(config.ProjectName, "serve")
	if err == nil {
		t.Fatal("StartDetached started a second copy while one was recorded as running")
	}
	if want := strconv.Itoa(os.Getpid()); !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name the running pid %s", err, want)
	}
}

func writePID(t *testing.T, home string, pid int) string {
	t.Helper()
	path := filepath.Join(home, "run", config.ProjectName+".pid")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// deadPID returns a PID that has certainly exited: a child is started and
// reaped, so the number is real but no longer live.
func deadPID(t *testing.T) int {
	t.Helper()
	child := exec.Command("true")
	if err := child.Run(); err != nil {
		t.Skipf("cannot run a child process here: %v", err)
	}
	return child.ProcessState.Pid()
}
