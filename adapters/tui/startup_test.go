package tui

import (
	"strings"
	"testing"

	"inferencerig/config"
	"inferencerig/platform/process"
)

// The Services page used to report "action completed" for a Start that spawned
// a daemon which had already died. The error now propagates, but the status bar
// is one truncating line shared with every other warning, so what lands there
// has to be the summary rather than the whole failure.
func TestStartupFailureSurfacesAsOneLineWarning(t *testing.T) {
	failure := &process.StartupError{
		Name: config.ProjectName, PID: 4711,
		LogPath: "/home/user/.inferencerig/run/inferencerig.log",
		Tail:    "Error: parse config: security.disable_auth with a non-loopback listen_addr\n",
	}

	warning := actionWarning(failure)
	if strings.Contains(warning, "\n") {
		t.Errorf("warning = %q, want a single line", warning)
	}
	if strings.Contains(warning, "security.disable_auth") || strings.Contains(warning, "/home/user") {
		t.Errorf("warning = %q, want the summary rather than the log tail", warning)
	}
	if !strings.Contains(warning, "doctor") {
		t.Errorf("warning = %q, want a pointer to doctor", warning)
	}

	// Unrelated errors keep their own message.
	if got := actionWarning(errDispatch); got != errDispatch.Error() {
		t.Errorf("actionWarning = %q, want %q", got, errDispatch.Error())
	}
}

// processAction is the Services page "Start" button. It must hand the failure
// back rather than swallowing it into a success notice.
func TestProcessActionReturnsStartupFailure(t *testing.T) {
	failure := &process.StartupError{Name: config.ProjectName, PID: 4711}
	startDetached = func(string, ...string) error { return failure }
	t.Cleanup(func() { startDetached = process.StartDetached })

	if err := processAction(config.ProjectName, 0, []string{"serve"}); err != failure {
		t.Fatalf("processAction = %v, want the startup failure", err)
	}
}

// Autostart runs unattended at TUI launch, so a daemon that cannot start has to
// reach the operator here too.
func TestAutostartServicesReportsStartupFailure(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir())
	failure := &process.StartupError{Name: config.ProjectName, PID: 4711}
	startDetached = func(string, ...string) error { return failure }
	t.Cleanup(func() { startDetached = process.StartDetached })

	msg, ok := autostartServices()().(actionMsg)
	if !ok {
		t.Fatalf("autostartServices returned %T, want actionMsg", msg)
	}
	if msg.err != failure {
		t.Fatalf("err = %v, want the startup failure", msg.err)
	}
	if msg.notice != "" {
		t.Errorf("notice = %q, want none when a service failed to start", msg.notice)
	}
}
