package process

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"inferencerig/config"
)

func TestStatusDetachedStoppedWhenPIDMissing(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir())

	status, err := StatusDetached(config.ProjectName)
	if err != nil {
		t.Fatalf("StatusDetached returned error: %v", err)
	}
	if status.Running || status.PID != 0 {
		t.Fatalf("status = %#v", status)
	}
}

func TestStatusDetachedRunningFromPIDFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)
	pidPath := filepath.Join(home, "run", config.ProjectName+".pid")
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := StatusDetached(config.ProjectName)
	if err != nil {
		t.Fatalf("StatusDetached returned error: %v", err)
	}
	if !status.Running || status.PID != os.Getpid() || status.Uptime < 0 {
		t.Fatalf("status = %#v", status)
	}
}

func TestStartDetachedReturnsLogAttachmentErrorWithoutPanic(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)
	if err := os.WriteFile(filepath.Join(home, "run"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := StartDetached(config.ProjectName, "serve"); err == nil {
		t.Fatal("expected log attachment error")
	}
}

// A daemon that dies without cleaning up leaves a PID file behind, and the OS
// eventually hands that PID to something unrelated. The recorded process is
// then alive but is not our daemon, which used to read as Running forever.
func TestStatusDetachedStoppedWhenPIDWasRecycled(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)

	other := exec.Command("sleep", "60")
	if err := other.Start(); err != nil {
		t.Skipf("cannot start a stand-in process: %v", err)
	}
	defer func() {
		_ = other.Process.Kill()
		_, _ = other.Process.Wait()
	}()

	pidPath := filepath.Join(home, "run", config.ProjectName+".pid")
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(other.Process.Pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := StatusDetached(config.ProjectName)
	if err != nil {
		t.Fatalf("StatusDetached returned error: %v", err)
	}
	if status.Running || status.PID != 0 {
		t.Fatalf("status = %#v, want stopped", status)
	}
	// Clearing the file is what lets the next start recover unattended.
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("stale PID file remains: %v", err)
	}
}

// StopDetached must not signal the unrelated process that inherited the PID.
func TestStopDetachedLeavesRecycledPIDAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)

	other := exec.Command("sleep", "60")
	if err := other.Start(); err != nil {
		t.Skipf("cannot start a stand-in process: %v", err)
	}
	defer func() {
		_ = other.Process.Kill()
		_, _ = other.Process.Wait()
	}()

	pidPath := filepath.Join(home, "run", config.ProjectName+".pid")
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(other.Process.Pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := StopDetached(config.ProjectName); err != nil {
		t.Fatalf("StopDetached returned error: %v", err)
	}
	if err := other.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("stand-in process was signalled: %v", err)
	}
}
