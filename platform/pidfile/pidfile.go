package pidfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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

// Read returns the recorded PID without changing the file or inferring whether
// the process is alive. Callers doing reconciliation need that distinction.
func (f File) Read() (int, bool, error) {
	data, err := os.ReadFile(f.path)
	if os.IsNotExist(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, true, fmt.Errorf("invalid PID file %s", f.path)
	}
	return pid, true, nil
}

func (f File) Running() (int, bool, error) {
	pid, exists, err := f.Read()
	if err != nil || (exists && !Alive(pid)) {
		_ = os.Remove(f.path)
		return 0, false, nil
	}
	return pid, exists, nil
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
