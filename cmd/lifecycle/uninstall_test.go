package lifecycle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveUninstallTargetsHonorsChoices(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, ".inferencerig")
	executable := filepath.Join(root, "infr")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("command"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := removeUninstallTargets(home, executable, uninstallChoices{settings: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("settings still exist: %v", err)
	}
	if _, err := os.Stat(executable); err != nil {
		t.Fatalf("command was deleted without consent: %v", err)
	}

	if err := removeUninstallTargets(home, executable, uninstallChoices{command: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(executable); !os.IsNotExist(err) {
		t.Fatalf("command still exists: %v", err)
	}
}

func TestRemoveUninstallTargetsRejectsUnsafeSettingsPath(t *testing.T) {
	if err := removeUninstallTargets(string(os.PathSeparator), "", uninstallChoices{settings: true}); err == nil {
		t.Fatal("filesystem root was accepted as a settings path")
	}
}
