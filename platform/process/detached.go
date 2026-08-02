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

func StartDetached(name string, args ...string) error {
	file, err := detachedPIDFile(name)
	if err != nil {
		return err
	}
	if pid, ok, err := file.Running(); err != nil {
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
	pid, ok, err := file.Running()
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
	pid, ok, err := file.Running()
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
	// PIDs are recycled: a crashed daemon can leave a pidfile pointing at a
	// PID the OS has since handed to an unrelated process. Refuse to signal
	// it unless it is still running the same binary.
	if self, err := os.Executable(); err == nil {
		if _, err := SameExecutable(context.Background(), pid, self); err != nil {
			return file.Remove(pid)
		}
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
