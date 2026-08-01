package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"inferencerig/config"
	"inferencerig/core/doctor"
)

func runDoctorCommand(t *testing.T, body string, args ...string) (string, error) {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)
	if body != "" {
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root := NewRootCommand()
	root.SilenceErrors = true
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"doctor"}, args...))
	err := root.Execute()
	return out.String(), err
}

// The exact configuration that motivated this command.
const brokenConfig = "listen_addr: \"0.0.0.0:7000\"\nsecurity: {disable_auth: true}\n"

func TestDoctorCommandDiagnosesBrokenConfig(t *testing.T) {
	output, err := runDoctorCommand(t, brokenConfig)

	if err == nil {
		t.Fatal("doctor exited 0 with a config the daemon would reject")
	}
	text := output
	for _, want := range []string{"[FAIL]", "127.0.0.1:7000", "disable_auth: false", "allow_exposed_without_auth"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
}

func TestDoctorCommandExitsZeroOnHealthyInstall(t *testing.T) {
	output, err := runDoctorCommand(t, "listen_addr: \"127.0.0.1:7000\"\n")

	if err != nil {
		t.Fatalf("doctor err = %v, want nil:\n%s", err, output)
	}
}

func TestDoctorCommandJSONIsMachineReadable(t *testing.T) {
	output, err := runDoctorCommand(t, brokenConfig, "--json")
	if err == nil {
		t.Fatal("expected a non-zero exit for a broken config")
	}

	var report doctor.Report
	if decodeErr := json.Unmarshal([]byte(output), &report); decodeErr != nil {
		t.Fatalf("decode: %v\n%s", decodeErr, output)
	}
	if report.SchemaVersion != doctor.SchemaVersion {
		t.Errorf("schema_version = %d, want %d", report.SchemaVersion, doctor.SchemaVersion)
	}
	var check doctor.Check
	for _, c := range report.Checks {
		if c.ID == "config.valid" {
			check = c
		}
	}
	if check.Status != doctor.StatusFail {
		t.Fatalf("config.valid status = %q, want fail", check.Status)
	}
	if len(check.Remedies) != 3 {
		t.Errorf("remedies = %d, want all three", len(check.Remedies))
	}
	for _, c := range report.Checks {
		if c.ID == "daemon.reachable" && c.Status != doctor.StatusSkipped {
			t.Errorf("daemon.reachable = %q, want skip with no daemon", c.Status)
		}
	}
}

// The report is the output; a usage dump after it buries the findings.
func TestDoctorCommandSilencesUsage(t *testing.T) {
	output, _ := runDoctorCommand(t, brokenConfig)
	if strings.Contains(output, "Usage:") {
		t.Errorf("doctor printed a usage dump:\n%s", output)
	}
}
