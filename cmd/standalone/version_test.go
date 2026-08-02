package standalone

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"inferencerig/config"
)

func TestVersionCommandText(t *testing.T) {
	out := runVersion(t)
	if !strings.Contains(out, config.CommandName) {
		t.Errorf("version output %q missing command name", out)
	}
}

func TestVersionCommandJSON(t *testing.T) {
	out := runVersion(t, "--json")
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

// runVersion nests VersionCommand under a stand-in root named like the real
// one, since the command's own output includes cmd.Root().Name().
func runVersion(t *testing.T, args ...string) string {
	t.Helper()
	root := &cobra.Command{Use: config.CommandName}
	root.AddCommand(VersionCommand())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"version"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}
	return out.String()
}
