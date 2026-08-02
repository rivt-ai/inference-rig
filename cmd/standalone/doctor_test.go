package standalone

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"inferencerig/bootstrap"
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
	root := DoctorCommand(bootstrap.ValidateConfig)
	root.SilenceErrors = true
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// The exact configuration that motivated this command.
// Port 0 keeps the port check deterministic; a fixed port is free or taken
// depending on what else this machine happens to be running.
const brokenConfig = "listen_addr: \"0.0.0.0:0\"\nsecurity: {disable_auth: true}\n"

func TestDoctorCommandDiagnosesBrokenConfig(t *testing.T) {
	output, err := runDoctorCommand(t, brokenConfig)

	if err == nil {
		t.Fatal("doctor exited 0 with a config the daemon would reject")
	}
	text := output
	for _, want := range []string{"[FAIL]", "127.0.0.1:0", "disable_auth: false", "allow_exposed_without_auth"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
}

func TestDoctorCommandExitsZeroOnHealthyInstall(t *testing.T) {
	output, err := runDoctorCommand(t, "listen_addr: \"127.0.0.1:0\"\n")

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

func TestDoctorFixWithRepairsTheConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)
	path := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(path, []byte(brokenConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	root := DoctorCommand(bootstrap.ValidateConfig)
	root.SilenceErrors = true
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--fix-with=bind-loopback"})
	if err := root.Execute(); err != nil {
		t.Fatalf("doctor --fix-with err = %v:\n%s", err, out.String())
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "127.0.0.1:0") {
		t.Errorf("config was not repaired:\n%s", body)
	}
	// The operator is shown the result, not just told a write happened.
	if !strings.Contains(out.String(), "Applied bind-loopback") {
		t.Errorf("output does not report what was applied:\n%s", out.String())
	}
	if _, err := config.LoadFile(path); err != nil {
		t.Errorf("repaired config does not load: %v", err)
	}
}

// --fix cannot prompt without a terminal, and must not pick a security posture
// on the operator's behalf.
func TestDoctorFixRefusesWithoutATerminal(t *testing.T) {
	output, err := runDoctorCommand(t, brokenConfig, "--fix")

	if err == nil {
		t.Fatal("--fix succeeded with no terminal to prompt on")
	}
	for _, want := range []string{"interactive terminal", "--fix-with=bind-loopback", "disable_auth: false"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}
}

func TestDoctorRejectsUnknownRemedy(t *testing.T) {
	_, err := runDoctorCommand(t, brokenConfig, "--fix-with=nonsense")
	if err == nil {
		t.Fatal("doctor accepted an unknown remedy")
	}
}

// Repairing writes to disk and prompts; neither belongs in a stream something
// else is parsing.
func TestDoctorRejectsFixWithJSON(t *testing.T) {
	if _, err := runDoctorCommand(t, brokenConfig, "--fix", "--json"); err == nil {
		t.Error("--fix --json was accepted")
	}
	if _, err := runDoctorCommand(t, brokenConfig, "--fix", "--fix-with=bind-loopback"); err == nil {
		t.Error("--fix --fix-with was accepted")
	}
}
