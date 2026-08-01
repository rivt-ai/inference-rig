package pidfile

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestRunningRemovesMalformedAndDeadFiles(t *testing.T) {
	for name, contents := range map[string]string{"malformed": "not-a-pid\n", "dead": "999999999\n"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agent.pid")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, running, err := New(path).Running(); err != nil || running {
				t.Fatalf("running = %v, err = %v", running, err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("stale PID file remains: %v", err)
			}
		})
	}
}

func TestRemoveDoesNotDeleteReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.pid")
	file := New(path)
	if err := file.Write(os.Getpid()); err != nil {
		t.Fatal(err)
	}
	if err := file.Remove(os.Getpid() + 1); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != strconv.Itoa(os.Getpid())+"\n" {
		t.Fatalf("PID file = %q", data)
	}
}
