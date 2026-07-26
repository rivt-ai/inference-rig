package cmd

import "testing"

func TestRootIncludesDaemonCommands(t *testing.T) {
	root := NewRootCommand()
	for _, name := range []string{"serve", "daemon", "setup", "tui"} {
		if command, _, err := root.Find([]string{name}); err != nil || command.Name() != name {
			t.Fatalf("command %q not registered: command=%v err=%v", name, command, err)
		}
	}
}
