package process

import (
	"os"
	"path/filepath"
	"strconv"
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
