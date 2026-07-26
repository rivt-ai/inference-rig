package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"inferencerig/config"
)

func TestRootIncludesDaemonCommands(t *testing.T) {
	root := NewRootCommand()
	if root.RunE == nil {
		t.Fatal("bare command does not launch the TUI")
	}
	for _, name := range []string{"serve", "daemon", "setup", "tui", "web"} {
		if command, _, err := root.Find([]string{name}); err != nil || command.Name() != name {
			t.Fatalf("command %q not registered: command=%v err=%v", name, command, err)
		}
	}
}

func TestProfilesEmptyRecognizesCanonicalProfile(t *testing.T) {
	root := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, root)
	if empty, err := profilesEmpty(); err != nil || !empty {
		t.Fatalf("empty=%v err=%v", empty, err)
	}
	dir := filepath.Join(root, "profiles", "demo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profile.yaml"), []byte("name: demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if empty, err := profilesEmpty(); err != nil || empty {
		t.Fatalf("empty=%v err=%v", empty, err)
	}
}
