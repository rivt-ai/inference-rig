package pidfile

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/shirou/gopsutil/v4/process"
)

type File struct{ path string }

func New(path string) File { return File{path: path} }

func (f File) Path() string { return f.path }

func (f File) Write(pid int) error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(f.path, []byte(strconv.Itoa(pid)+"\n"), 0o600)
}

func (f File) Running() (int, bool, error) {
	data, err := os.ReadFile(f.path)
	if os.IsNotExist(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || !Alive(pid) {
		_ = os.Remove(f.path)
		return 0, false, nil
	}
	return pid, true, nil
}

func (f File) Remove(expectedPID int) error {
	if expectedPID > 0 {
		data, err := os.ReadFile(f.path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if convErr == nil && pid != expectedPID {
			return nil
		}
	}
	err := os.Remove(f.path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func Alive(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

func ExecutableMatches(pid int, expected string) bool {
	actual, err := executable(pid)
	resolved, _ := exec.LookPath(expected)
	evaluated, _ := filepath.EvalSymlinks(resolved)
	return err == nil && (actual == expected || actual == resolved || actual == evaluated || filepath.Base(actual) == filepath.Base(expected))
}

func executable(pid int) (string, error) {
	proc, err := process.NewProcess(int32(pid))
	if err != nil {
		return "", err
	}
	return proc.Exe()
}
