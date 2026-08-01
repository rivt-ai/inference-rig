package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"inferencerig/config"
)

func TestConfigValidateChecksAutostartProfiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("autostart_profiles: [deleted]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := NewRootCommand()
	root.SetArgs([]string{"config", "validate"})
	root.SilenceErrors = true
	err := root.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), `autostart profile "deleted" is invalid`) {
		t.Fatalf("config validate error = %v", err)
	}
}
