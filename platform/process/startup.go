package process

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"inferencerig/platform/audit"
	"inferencerig/platform/pidfile"
)

// startupGrace bounds how long StartDetached waits to see whether a freshly
// spawned process survives. A config or bind failure aborts in milliseconds;
// anything still alive after this is reporting its own health another way.
// A var so tests can shrink it.
var startupGrace = 300 * time.Millisecond

// startupTailLines is how much of the service log a StartupError carries. The
// daemon's own error is the last thing it writes, so a short tail is enough.
const startupTailLines = 20

// StartupError reports a detached process that exited instead of staying up.
// It carries the log tail because the process wrote its error there and then
// died: without it the caller has a dead PID and nothing to show the operator.
type StartupError struct {
	Name    string
	PID     int
	LogPath string
	Tail    string
	Err     error
}

func (e *StartupError) Error() string {
	var b strings.Builder
	b.WriteString(e.Summary())
	if e.LogPath != "" {
		b.WriteString("\n  log: " + e.LogPath)
	}
	for _, line := range significantTail(e.Tail) {
		b.WriteString("\n  " + line)
	}
	b.WriteString("\n\nrun `inferencerig doctor` for a full diagnosis")
	return b.String()
}

// Summary is the one-line form, for surfaces that cannot show the tail — the
// TUI status bar is a single truncating line shared with every other warning.
func (e *StartupError) Summary() string {
	name := e.Name
	if name == "" {
		name = "process"
	}
	if e.PID > 0 {
		return name + " exited immediately (pid " + strconv.Itoa(e.PID) + "); run: inferencerig doctor"
	}
	return name + " exited immediately; run: inferencerig doctor"
}

func (e *StartupError) Unwrap() error { return e.Err }

// significantTail picks the lines worth showing from a service log tail. Cobra
// prints a usage block after an error on commands that have not silenced it, so
// the literal last lines are often flag documentation rather than the failure.
func significantTail(tail string) []string {
	lines := strings.Split(strings.TrimRight(tail, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); strings.HasPrefix(line, "Error:") {
			return []string{line}
		}
	}
	var kept []string
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	if len(kept) > 3 {
		kept = kept[len(kept)-3:]
	}
	return kept
}

// watchStartup reaps cmd and reports an exit inside the startup grace period.
//
// It waits rather than polling liveness: the child is still this process's
// direct child, so an exited one becomes a zombie and pidfile.Alive — a
// kill(pid, 0) probe — keeps answering true for it. Wait is exact, needs no
// interval, and yields the exit error. Reaping here is also what makes a later
// pidfile.Alive check honest for callers polling a daemon that died after this
// window closed.
func watchStartup(cmd *exec.Cmd, file pidfile.File, name string, pid int) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(startupGrace)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case err := <-done:
		_ = file.Remove(pid)
		return newStartupError(name, pid, err)
	}
}

func newStartupError(name string, pid int, cause error) *StartupError {
	failure := &StartupError{Name: name, PID: pid, Err: cause}
	if path, err := audit.GetLogPath(name); err == nil {
		failure.LogPath = path
	}
	if tail, err := audit.TailLogLines(name, startupTailLines); err == nil {
		failure.Tail = tail
	}
	return failure
}

// CheckStartupFailure reports a process that was recorded in its PID file and
// is no longer alive — the daemon that started, bound nothing and gave up.
//
// A missing PID file is not a failure: a daemon supervised elsewhere never
// writes one here. Read-only by contract, so a diagnostic can call it; in
// particular it uses pidfile.Read rather than Running, which deletes.
func CheckStartupFailure(name string) error {
	file, err := detachedPIDFile(name)
	if err != nil {
		return nil
	}
	pid, exists, err := file.Read()
	if err != nil || !exists || pidfile.Alive(pid) {
		return nil
	}
	return newStartupError(name, pid, fmt.Errorf("pid %d is no longer running", pid))
}
