package cmd

import (
	"os"
	"testing"

	"inferencerig/config"
)

func TestRootIncludesDaemonCommands(t *testing.T) {
	root := NewRootCommand()
	if root.RunE == nil {
		t.Fatal("bare command does not launch the TUI")
	}
	for _, name := range []string{"serve", "daemon", "setup", "tui", "web", "doctor", "uninstall"} {
		if command, _, err := root.Find([]string{name}); err != nil || command.Name() != name {
			t.Fatalf("command %q not registered: command=%v err=%v", name, command, err)
		}
	}
}

func TestFirstRunUsesConfigExistence(t *testing.T) {
	root := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, root)
	path, err := config.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("config unexpectedly exists: %v", err)
	}
	if err := os.WriteFile(path, []byte("listen_addr: 127.0.0.1:7000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
