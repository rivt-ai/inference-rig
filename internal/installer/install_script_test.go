package installer

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScriptRejectsCommandClashBeforeDownload(t *testing.T) {
	bin := t.TempDir()
	existing := filepath.Join(bin, "infr")
	if err := os.WriteFile(existing, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
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

	if err := command.Run(); err == nil {
		t.Fatal("install succeeded despite an existing infr command")
	}
	if !strings.Contains(output.String(), "command 'infr' already exists at "+existing) {
		t.Fatalf("output = %q", output.String())
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("curl ran before the command clash was reported: %v", err)
	}
}
