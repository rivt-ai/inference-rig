package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"inferencerig/config"
	"inferencerig/platform/audit"
	"inferencerig/platform/pidfile"

	gopsprocess "github.com/shirou/gopsutil/v4/process"
)

// defaultShutdownTimeout bounds graceful shutdown of a serving process.
const defaultShutdownTimeout = 5 * time.Second

type DetachedStatus struct {
	Running bool
	PID     int
	Uptime  time.Duration
}

// runningSelf reports the PID recorded in file, but only when that PID is
// still running this same binary.
//
// pidfile.Running answers "does a process with this PID exist", which a
// recycled PID answers yes to. A daemon that died without cleaning up then
// looks alive forever: StartDetached refuses to start a replacement and
// StatusDetached reports Running, so callers wait on a socket nobody is
// listening to. Treating a mismatched PID as stale — and clearing the file —
// lets the next start recover on its own.
func runningSelf(file pidfile.File) (int, bool, error) {
	pid, ok, err := file.Running()
	if err != nil || !ok {
		return 0, false, err
	}
	self, err := os.Executable()
	if err != nil {
		// Without our own path there is nothing to compare against; trust the
		// PID file rather than killing a daemon that may well be healthy.
		return pid, true, nil
	}
	if _, err := SameExecutable(context.Background(), pid, self); err != nil {
		return 0, false, file.Remove(pid)
	}
	return pid, true, nil
}

func StartDetached(name string, args ...string) error {
	file, err := detachedPIDFile(name)
	if err != nil {
		return err
	}
	if pid, ok, err := runningSelf(file); err != nil {
		return err
	} else if ok {
		return errors.New("detached " + config.ProjectName + " " + name + " already running pid=" + strconv.Itoa(pid))
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, args...)

	closeLogs, err := audit.AttachLogs(cmd, name)
	if err != nil {
		return err
	}
	defer closeLogs()

	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	if err := file.Write(pid); err != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		return err
	}
	// Not Release: a fork/exec that succeeded says nothing about whether the
	// process stayed up, and every caller used to report success for a daemon
	// that had already died. watchStartup reaps it and reports that.
	return watchStartup(cmd, file, name, pid)
}

func StatusDetached(name string) (DetachedStatus, error) {
	file, err := detachedPIDFile(name)
	if err != nil {
		return DetachedStatus{}, err
	}
	status := DetachedStatus{}
	pid, ok, err := runningSelf(file)
	if err != nil {
		return DetachedStatus{}, err
	}
	if !ok {
		return status, nil
	}
	status.Running = true
	status.PID = pid
	if info, err := os.Stat(file.Path()); err == nil {
		status.Uptime = time.Since(info.ModTime()).Round(time.Second)
	}
	return status, nil
}

func StopDetached(name string) error {
	file, err := detachedPIDFile(name)
	if err != nil {
		return err
	}
	// runningSelf refuses to hand back a recycled PID, so a crashed daemon
	// whose PID the OS has since reassigned is cleared rather than signalled.
	pid, ok, err := runningSelf(file)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	proc, err := gopsprocess.NewProcess(int32(pid))
	if err != nil {
		return file.Remove(pid)
	}
	_ = proc.Terminate()
	wait := defaultShutdownTimeout + time.Second
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if running, _ := proc.IsRunning(); !running {
			return file.Remove(pid)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if running, _ := proc.IsRunning(); running {
		_ = proc.Kill()
	}
	return file.Remove(pid)
}

func detachedPIDFile(name string) (pidfile.File, error) {
	home, err := config.Home()
	if err != nil {
		return pidfile.File{}, err
	}
	return pidfile.New(filepath.Join(home, "run", name+".pid")), nil
}
