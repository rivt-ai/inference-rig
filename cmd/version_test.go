package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"inferencerig/config"
)

func TestVersionCommandText(t *testing.T) {
	out := runVersion(t, "version")
	if !strings.Contains(out, config.CommandName) {
		t.Errorf("version output %q missing command name", out)
	}
}

func TestVersionCommandJSON(t *testing.T) {
	out := runVersion(t, "version", "--json")
	var fields map[string]string
	if err := json.Unmarshal([]byte(out), &fields); err != nil {
		t.Fatalf("version --json is not valid JSON: %v (%q)", err, out)
	}
	for _, key := range []string{"version", "commit", "commit_time"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("version --json missing %q: %v", key, fields)
		}
	}
}

func runVersion(t *testing.T, args ...string) string {
	t.Helper()
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}
	return out.String()
}
