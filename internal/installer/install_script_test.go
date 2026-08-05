package installer

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScriptValidatesCommandNameBeforeDownload(t *testing.T) {
	tests := []struct {
		name, commandName, existingName, want string
	}{
		{name: "default name", existingName: "infr", want: "command 'infr' already exists at "},
		{name: "custom name", commandName: "inference-rig", existingName: "inference-rig", want: "command 'inference-rig' already exists at "},
		{name: "invalid name", commandName: "../infr", want: "invalid COMMAND_NAME"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertScriptRejectedBeforeDownload(t, test.commandName, test.existingName, test.want)
		})
	}
}

func assertScriptRejectedBeforeDownload(t *testing.T, commandName, existingName, want string) {
	t.Helper()
	bin := t.TempDir()
	if existingName != "" {
		if err := os.WriteFile(filepath.Join(bin, existingName), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	called := filepath.Join(bin, "curl-called")
	if err := os.WriteFile(filepath.Join(bin, "curl"), []byte("#!/bin/sh\ntouch '"+called+"'\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	command := exec.Command("sh", "-s", "--", "v0.0.0")
	command.Stdin = strings.NewReader(script)
	command.Stdout, command.Stderr = &output, &output
	command.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "INSTALL_DIR="+t.TempDir())
	if commandName != "" {
		command.Env = append(command.Env, "COMMAND_NAME="+commandName)
	}

	if err := command.Run(); err == nil {
		t.Fatal("install succeeded despite an invalid or conflicting command name")
	}
	if !strings.Contains(output.String(), want) {
		t.Fatalf("output = %q", output.String())
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("curl ran before command validation: %v", err)
	}
}
