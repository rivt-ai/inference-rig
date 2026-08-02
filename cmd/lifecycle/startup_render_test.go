package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"inferencerig/config"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/platform/pidfile"
	"inferencerig/platform/process"
)

// deadPID returns a pid that has exited and been reaped, so it cannot be a
// zombie that kill(pid, 0) would still report as alive.
func deadPID(t *testing.T) int {
	t.Helper()
	reaped := exec.Command("/bin/sh", "-c", "exit 0")
	if err := reaped.Run(); err != nil {
		t.Fatal(err)
	}
	return reaped.Process.Pid
}

type stubControlClient struct{ calls int }

func (s *stubControlClient) Health(context.Context, *controlv1.HealthRequest) (*controlv1.HealthResponse, error) {
	s.calls++
	return nil, errors.New("connection refused")
}

// A daemon that dies after its start window used to cost the caller the full
// five-second timeout and then report only the timeout, never the reason.
func TestWaitForControlFailsFastOnDeadDaemon(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)
	pidPath := filepath.Join(home, "run", config.ProjectName+".pid")
	if err := pidfile.New(pidPath).Write(deadPID(t)); err != nil {
		t.Fatal(err)
	}

	client := &stubControlClient{}
	start := time.Now()
	err := waitForControl(context.Background(), client)
	elapsed := time.Since(start)

	var failure *process.StartupError
	if !errors.As(err, &failure) {
		t.Fatalf("waitForControl err = %v, want *process.StartupError", err)
	}
	if elapsed > time.Second {
		t.Errorf("took %s, want a fast failure rather than the 5s timeout", elapsed)
	}
}

// A daemon supervised elsewhere writes no PID file here, so its absence must
// not be read as a failure — the wait still has to run its course.
func TestWaitForControlStillTimesOutWithoutPIDFile(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := waitForControl(ctx, &stubControlClient{})

	var failure *process.StartupError
	if errors.As(err, &failure) {
		t.Fatalf("err = %v, want a plain timeout rather than a startup failure", err)
	}
	if err == nil {
		t.Fatal("expected a timeout error")
	}
}

func TestReportStartupFailurePrintsDaemonErrorOnce(t *testing.T) {
	var out bytes.Buffer
	command := &cobra.Command{}
	command.SetErr(&out)

	failure := &process.StartupError{
		Name: config.ProjectName, PID: 4711,
		LogPath: "/tmp/inferencerig.log",
		Tail:    "Error: parse config: security.disable_auth with a non-loopback listen_addr\n",
	}
	err := ReportStartupFailure(command, failure)

	if !errors.Is(err, errStartupFailed) {
		t.Fatalf("err = %v, want errStartupFailed", err)
	}
	printed := out.String()
	for _, want := range []string{"4711", "/tmp/inferencerig.log", "security.disable_auth", "inferencerig doctor"} {
		if !strings.Contains(printed, want) {
			t.Errorf("output %q missing %q", printed, want)
		}
	}
	// The returned error is rendered again by main, so it must stay short
	// rather than repeating everything already printed.
	if strings.Contains(err.Error(), "security.disable_auth") {
		t.Errorf("returned error %q duplicates the printed detail", err)
	}
}

func TestReportStartupFailurePassesOtherErrorsThrough(t *testing.T) {
	var out bytes.Buffer
	command := &cobra.Command{}
	command.SetErr(&out)

	sentinel := errors.New("something else")
	if err := ReportStartupFailure(command, sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the original error", err)
	}
	if out.Len() != 0 {
		t.Errorf("wrote %q for an unrelated error", out.String())
	}
	if err := ReportStartupFailure(command, nil); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

// The commands whose stderr is read back as a service log must not bury the
// failure under a usage dump, and must not print it twice.
func TestStartupCommandsSilenceUsageAndErrors(t *testing.T) {
	for name, command := range map[string]*cobra.Command{
		"serve": ServeCommand(), "tui": TuiCommand(), "setup": SetupCommand(),
	} {
		if !command.SilenceUsage || !command.SilenceErrors {
			t.Errorf("%s: SilenceUsage = %v, SilenceErrors = %v, want both true",
				name, command.SilenceUsage, command.SilenceErrors)
		}
	}
}

// serve --detach used to exit 0 while the daemon it spawned was already dead.
func TestServeDetachReportsStartupFailure(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir())
	startDetached = func(string, ...string) error {
		return &process.StartupError{
			Name: config.ProjectName, PID: 4711,
			Tail: "Error: parse config: security.disable_auth with a non-loopback listen_addr\n",
		}
	}
	t.Cleanup(func() { startDetached = process.StartDetached })

	root := ServeCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--detach"})

	if err := root.Execute(); err == nil {
		t.Fatal("serve --detach returned nil for a daemon that could not start")
	}
	if printed := out.String(); !strings.Contains(printed, "security.disable_auth") {
		t.Errorf("output %q does not carry the daemon's own error", printed)
	}
}
