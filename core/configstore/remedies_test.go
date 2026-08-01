package configstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"inferencerig/config"

	"gopkg.in/yaml.v3"
)

// A real operator's file: the broken combination, plus comments and
// hand-edited fields a repair must not destroy.
const brokenWithComments = `# InferenceRig configuration
listen_addr: "0.0.0.0:7000"  # exposed on purpose
model_storage_dir: /srv/models
autostart_profiles:
  - ornith-9b-mtp-kl-Q8_0
security:
  # auth is off while we work on the reverse proxy
  disable_auth: true
`

func newBrokenStore(t *testing.T) (*FileStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(brokenWithComments), 0o600); err != nil {
		t.Fatal(err)
	}
	return NewFileStore(path, 0), path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// mutateDocument must keep refusing an unloadable file; only Repair may touch
// it. Otherwise an ordinary TUI toggle would silently rewrite a broken config.
func TestMutateDocumentStillRefusesUnloadableConfig(t *testing.T) {
	store, path := newBrokenStore(t)

	if _, err := store.SetProfileAutostart(context.Background(), "demo", true); err == nil {
		t.Fatal("SetProfileAutostart accepted a config that does not load")
	}
	if readFile(t, path) != brokenWithComments {
		t.Error("a refused mutation still rewrote the file")
	}
}

func TestRepairRemediesFixTheConfigAndKeepComments(t *testing.T) {
	tests := []struct {
		name   string
		repair func(*FileStore, context.Context) (WriteResult, error)
		want   string
	}{
		{
			name:   "bind loopback",
			repair: (*FileStore).RepairBindLoopback,
			want:   "127.0.0.1:7000",
		},
		{
			name:   "require auth",
			repair: (*FileStore).RepairRequireAuth,
			want:   "disable_auth: false",
		},
		{
			name:   "allow exposed",
			repair: (*FileStore).RepairAllowExposed,
			want:   "allow_exposed_without_auth: true",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, path := newBrokenStore(t)

			result, err := test.repair(store, context.Background())
			if err != nil {
				t.Fatalf("repair: %v", err)
			}

			body := readFile(t, path)
			if !strings.Contains(body, test.want) {
				t.Errorf("config does not contain %q:\n%s", test.want, body)
			}
			assertPreservedUserContent(t, body)
			// A repair that leaves the config still broken is worse than none.
			if _, err := config.Parse([]byte(body)); err != nil {
				t.Errorf("repaired config still does not load: %v\n%s", err, body)
			}
			assertBackupWritten(t, result.BackupPath)
		})
	}
}

// Applying a remedy twice must not write a second backup and an identical file.
func TestRepairIsIdempotent(t *testing.T) {
	store, path := newBrokenStore(t)
	if _, err := store.RepairBindLoopback(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := readFile(t, path)

	result, err := store.RepairBindLoopback(context.Background())
	if err != nil {
		t.Fatalf("second repair: %v", err)
	}
	if result.BackupPath != "" {
		t.Errorf("a no-op repair wrote a backup at %s", result.BackupPath)
	}
	if readFile(t, path) != after {
		t.Error("a no-op repair rewrote the file")
	}
}

// The output check is what actually guards the write, so a mutation that would
// leave the file invalid must be rejected and the original left byte-identical.
func TestRepairRejectsAMutationThatLeavesConfigInvalid(t *testing.T) {
	store, path := newBrokenStore(t)

	_, err := store.Repair(context.Background(), func(document *yaml.Node) bool {
		return setScalar(documentRoot(document), []string{"startup_services"}, "nonsense", "!!str")
	})

	if err == nil {
		t.Fatal("Repair accepted a mutation producing an invalid config")
	}
	if readFile(t, path) != brokenWithComments {
		t.Error("a rejected repair modified the file")
	}
}

// Repair cannot invent a node tree from unparseable YAML, and must say so
// rather than truncating the file.
func TestRepairRefusesMalformedYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "listen_addr: \"0.0.0.0:7000\"\n  bad indent: [unclosed\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewFileStore(path, 0)

	if _, err := store.RepairBindLoopback(context.Background()); err == nil {
		t.Fatal("Repair accepted malformed YAML")
	}
	if readFile(t, path) != body {
		t.Error("a failed repair modified the file")
	}
}

// The whole point of repairing rather than regenerating: the operator's
// comments and hand-edited fields survive.
func assertPreservedUserContent(t *testing.T, body string) {
	t.Helper()
	for _, keep := range []string{
		"# InferenceRig configuration",
		"# auth is off while we work on the reverse proxy",
		"model_storage_dir: /srv/models",
		"ornith-9b-mtp-kl-Q8_0",
	} {
		if !strings.Contains(body, keep) {
			t.Errorf("repair dropped %q:\n%s", keep, body)
		}
	}
}

func assertBackupWritten(t *testing.T, path string) {
	t.Helper()
	if path == "" {
		t.Fatal("no backup was written")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("backup missing: %v", err)
	}
}

// A newly added key is annotated so the operator can tell later what changed
// the file. The annotation has to introduce the key, not trail it — a
// HeadComment set on the value of a mapping pair is emitted after the "key:".
func TestRepairAnnotatesAnAddedKeyAboveIt(t *testing.T) {
	store, path := newBrokenStore(t)
	if _, err := store.RepairAllowExposed(context.Background()); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(readFile(t, path), "\n")
	for i, line := range lines {
		if !strings.Contains(line, "allow_exposed_without_auth") {
			continue
		}
		if i == 0 || !strings.Contains(lines[i-1], "doctor --fix") {
			t.Fatalf("the annotation does not precede the key it explains:\n%s",
				strings.Join(lines, "\n"))
		}
		return
	}
	t.Fatal("allow_exposed_without_auth was not added")
}
