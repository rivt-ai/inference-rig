package doctor

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"inferencerig/config"
	"inferencerig/core/control"
	"inferencerig/platform/audit"
	"inferencerig/platform/pidfile"
	"inferencerig/platform/process"
)

// socketDialTimeout bounds the staleness probe. A live daemon accepts on a Unix
// socket immediately; anything slower is not a daemon this check can vouch for.
const socketDialTimeout = 500 * time.Millisecond

const startCommand = "inferencerig serve --detach"

// recentFailureLimit bounds what the report shows; the journal keeps more.
const recentFailureLimit = 5

// checkPIDFile distinguishes "not running" from "recorded as running, and
// isn't" — the second means the daemon died rather than being stopped, which is
// the case worth reporting.
//
// It reads rather than reconciles: pidfile.Running would delete the stale file,
// destroying the evidence this check exists to report.
func checkPIDFile(_ context.Context, e *env) Check {
	const id, title = "daemon.pidfile", "daemon process"
	path := pidFilePath(e.paths)
	pid, exists, err := pidfile.New(path).Read()
	switch {
	case err != nil:
		return fail(id, title, "the PID file is unreadable").withDetail(err.Error())
	case !exists:
		return ok(id, title, "not running").
			withRemedies(Remedy{ID: "start-daemon", Title: "start the control daemon", Command: startCommand})
	case pidfile.Alive(pid):
		return identifyDaemon(id, title, pid)
	default:
		return warn(id, title, "pid "+strconv.Itoa(pid)+" is recorded but no longer running").
			withDetail("The daemon exited without clearing " + path +
				". The next start or stop removes the stale file.").
			withRemedies(Remedy{ID: "start-daemon", Title: "start the control daemon", Command: startCommand})
	}
}

// identifyDaemon confirms the live PID is actually this binary. PIDs are
// recycled, so "something with this PID exists" is not evidence the daemon is
// running — an unrelated process inheriting the number would otherwise be
// reported as a healthy daemon.
func identifyDaemon(id, title string, pid int) Check {
	running := "running as pid " + strconv.Itoa(pid)
	self, err := os.Executable()
	if err != nil {
		return ok(id, title, running)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := process.SameExecutable(ctx, pid, self); err != nil {
		// Not a failure on its own: the daemon may legitimately be an older
		// build, or the process may be unreadable without more privilege.
		return warn(id, title, running+", but it is not this binary").
			withDetail(err.Error() + "\nThe recorded PID may have been recycled, or the daemon may be a different build.")
	}
	return ok(id, title, running)
}

// checkSocket reports whether anything is accepting on the control socket. A
// socket file with no listener is what a crashed daemon leaves behind, and it
// is why clients hang rather than failing fast.
func checkSocket(_ context.Context, e *env) Check {
	const id, title = "daemon.socket", "control socket"
	path := e.paths.ControlSocket
	if _, err := os.Stat(path); err != nil {
		return skip(id, title, "no socket at "+path)
	}
	if !socketAccepts(path) {
		return warn(id, title, "the socket file exists but nothing is listening").
			withDetail("A daemon that exits without cleaning up leaves " + path +
				" behind; the next start replaces it.")
	}
	return ok(id, title, "accepting connections")
}

// socketAccepts reports whether a listener is answering on a Unix socket. This
// is the read-only half of what the daemon's own listener does before binding;
// doctor must not take the other half, which deletes the stale socket.
func socketAccepts(path string) bool {
	conn, err := net.DialTimeout("unix", path, socketDialTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// checkDaemonReachable is the only check that talks to the control plane. It
// skips rather than fails when there is nothing to talk to: doctor is for a
// broken installation, and a stopped daemon is not itself a fault.
func checkDaemonReachable(ctx context.Context, e *env) Check {
	const id, title = "daemon.reachable", "control plane"
	if e.opts.DialControl == nil {
		return skip(id, title, "no control client wired")
	}
	// A socket nothing is accepting on means the daemon is down, not faulty —
	// a crashed daemon leaves its socket file behind. Probing first keeps that
	// case a skip; only a daemon that accepts and then misbehaves is a failure.
	if !socketAccepts(e.paths.ControlSocket) {
		return skip(id, title, "the control daemon is not running").
			withRemedies(Remedy{ID: "start-daemon", Title: "start the control daemon", Command: startCommand})
	}
	client, err := e.opts.DialControl(e.paths.ControlSocket)
	if err != nil {
		return fail(id, title, "could not dial the control socket").withDetail(err.Error())
	}
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := client.Health(callCtx); err != nil {
		return fail(id, title, "the daemon is not answering health checks").withDetail(err.Error())
	}
	return ok(id, title, "healthy")
}

// checkRecentLog surfaces the daemon's own last words when it is not running.
// That text is the actual diagnosis in most failures, and until now it sat in a
// file nothing pointed the operator at.
func checkRecentLog(_ context.Context, e *env) Check {
	const id, title = "daemon.recent_log", "recent daemon log"
	pid, exists, err := pidfile.New(pidFilePath(e.paths)).Read()
	if err == nil && exists && pidfile.Alive(pid) {
		return skip(id, title, "the daemon is running")
	}
	present, err := audit.LogExists(config.ProjectName)
	if err != nil || !present {
		return skip(id, title, "no service log yet")
	}
	tail, err := audit.TailLogLines(config.ProjectName, 20)
	if err != nil {
		return skip(id, title, "the service log could not be read")
	}
	path, _ := audit.GetLogPath(config.ProjectName)
	return ok(id, title, "last output from the stopped daemon").
		withDetail(path + "\n" + tail)
}

// pidFilePath is the daemon's PID file, which lives beside the control socket
// in the run directory.
func pidFilePath(paths config.Paths) string {
	return filepath.Join(filepath.Dir(paths.ControlSocket), config.ProjectName+".pid")
}

// checkRecentFailures reads the on-disk failure journal.
//
// It reads the file directly rather than asking the daemon, which is the whole
// point: the daemon's in-memory event history is gone after a restart, and a
// diagnostic runs precisely when the daemon is not there to ask.
func checkRecentFailures(_ context.Context, e *env) Check {
	const id, title = "failures.recent", "recent failures"
	path, err := config.FailureJournalPath()
	if err != nil {
		return skip(id, title, "the failure journal could not be located")
	}
	entries, err := control.NewFileJournal(path, 0).Recent(recentFailureLimit)
	if err != nil {
		return skip(id, title, "the failure journal could not be read").withDetail(err.Error())
	}
	if len(entries) == 0 {
		return ok(id, title, "none recorded")
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		line := entry.Time.Local().Format(time.RFC3339) + "  " + entry.Action
		if entry.Profile != "" {
			line += " " + entry.Profile
		}
		if entry.Detail != "" {
			line += ": " + entry.Detail
		}
		lines = append(lines, line)
	}
	return warn(id, title, fmt.Sprintf("%d recorded", len(entries))).
		withDetail(path + "\n" + strings.Join(lines, "\n"))
}
