package doctor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"inferencerig/config"
)

// brokenConfig is the failure this package was built for: authentication off
// on a bind that reaches the network.
const brokenConfig = "listen_addr: \"0.0.0.0:7000\"\nsecurity: {disable_auth: true}\n"

const healthyConfig = "listen_addr: \"127.0.0.1:7000\"\n"

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)
	if body != "" {
		path := filepath.Join(home, "config.yaml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

// realValidator mirrors what bootstrap.ValidateConfig does for the config
// layer, so these tests exercise the same verdict startup would reach.
func realValidator(context.Context) error {
	_, err := config.LoadOrDefault()
	return err
}

func runDoctor(t *testing.T, opts Options) Report {
	t.Helper()
	report, err := NewRunner(opts).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return report
}

func find(t *testing.T, report Report, id string) Check {
	t.Helper()
	for _, c := range report.Checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no check %q in %+v", id, report.Checks)
	return Check{}
}

func TestDoctorReportsExposedWithoutAuthWithAllThreeRemedies(t *testing.T) {
	writeConfig(t, brokenConfig)

	report := runDoctor(t, Options{ValidateConfig: realValidator})

	check := find(t, report, "config.valid")
	if check.Status != StatusFail {
		t.Fatalf("status = %q, want fail", check.Status)
	}
	if report.Worst() != StatusFail {
		t.Errorf("Worst = %q, want fail", report.Worst())
	}
	// The operator chooses; doctor must present every legal way out, never
	// just the one it would have picked.
	var ids []string
	for _, remedy := range check.Remedies {
		ids = append(ids, remedy.ID)
		if remedy.ConfigEdit == "" {
			t.Errorf("remedy %q has no literal config edit", remedy.ID)
		}
	}
	want := []string{RemedyBindLoopback, RemedyRequireAuth, RemedyAllowExposed}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("remedies = %v, want %v", ids, want)
	}
	// The loopback remedy must keep the configured port, not reset it.
	if edit := check.Remedies[0].ConfigEdit; !strings.Contains(edit, "127.0.0.1:7000") {
		t.Errorf("loopback remedy = %q, want the configured port preserved", edit)
	}
}

func TestDoctorAcceptsHealthyConfig(t *testing.T) {
	writeConfig(t, healthyConfig)

	report := runDoctor(t, Options{ValidateConfig: realValidator})

	if got := find(t, report, "config.valid").Status; got != StatusOK {
		t.Errorf("config.valid = %q, want ok", got)
	}
	if report.Worst() == StatusFail {
		t.Errorf("Worst = fail for a healthy install: %+v", report.Checks)
	}
}

// A missing config file is the first-run case, not a fault.
func TestDoctorTreatsMissingConfigAsDefaults(t *testing.T) {
	writeConfig(t, "")

	report := runDoctor(t, Options{ValidateConfig: realValidator})

	if got := find(t, report, "config.valid").Status; got != StatusOK {
		t.Errorf("config.valid = %q, want ok when no config file exists", got)
	}
}

// Doctor exists to be run against a stopped daemon, so daemon checks must skip
// rather than fail — otherwise every ordinary run reads as a wall of problems.
func TestDoctorSkipsDaemonChecksWhenNothingIsRunning(t *testing.T) {
	writeConfig(t, healthyConfig)

	report := runDoctor(t, Options{
		ValidateConfig: realValidator,
		DialControl:    func(string) (HealthChecker, error) { return nil, errors.New("must not dial") },
	})

	if got := find(t, report, "daemon.reachable").Status; got != StatusSkipped {
		t.Errorf("daemon.reachable = %q, want skip with no daemon running", got)
	}
	if got := find(t, report, "daemon.pidfile").Status; got != StatusOK {
		t.Errorf("daemon.pidfile = %q, want ok when nothing is recorded", got)
	}
}

type stubHealth struct{ err error }

func (s stubHealth) Health(context.Context) error { return s.err }

// A daemon that accepts connections but fails health is genuinely broken, and
// that is the one case daemon.reachable may fail on.
func TestDoctorFailsWhenDaemonAnswersUnhealthily(t *testing.T) {
	home := writeConfig(t, healthyConfig)
	socket := filepath.Join(home, "run", "control.sock")
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()

	report := runDoctor(t, Options{
		ValidateConfig: realValidator,
		DialControl: func(string) (HealthChecker, error) {
			return stubHealth{err: errors.New("service unavailable")}, nil
		},
	})

	if got := find(t, report, "daemon.reachable").Status; got != StatusFail {
		t.Errorf("daemon.reachable = %q, want fail for a daemon that answers unhealthily", got)
	}
	if got := find(t, report, "daemon.socket").Status; got != StatusOK {
		t.Errorf("daemon.socket = %q, want ok while a listener is accepting", got)
	}
}

// A crashed daemon leaves a socket file with no listener behind. That is what
// makes clients hang, so it has to be named.
func TestDoctorWarnsOnStaleSocket(t *testing.T) {
	home := writeConfig(t, healthyConfig)
	socket := filepath.Join(home, "run", "control.sock")
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(socket, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	report := runDoctor(t, Options{ValidateConfig: realValidator})

	if got := find(t, report, "daemon.socket").Status; got != StatusWarn {
		t.Errorf("daemon.socket = %q, want warn for a socket with no listener", got)
	}
	// Read-only by contract: reporting the stale socket must not remove it.
	if _, err := os.Stat(socket); err != nil {
		t.Errorf("doctor removed the stale socket: %v", err)
	}
}

func TestDoctorFlagsWorldWritablePaths(t *testing.T) {
	home := writeConfig(t, healthyConfig)
	if err := os.Chmod(filepath.Join(home, "config.yaml"), 0o666); err != nil {
		t.Fatal(err)
	}

	report := runDoctor(t, Options{ValidateConfig: realValidator})

	check := find(t, report, "files.permissions")
	if check.Status != StatusFail {
		t.Fatalf("files.permissions = %q, want fail for a world-writable config", check.Status)
	}
	if !strings.Contains(check.Detail, "config.yaml") {
		t.Errorf("detail %q does not name the offending path", check.Detail)
	}
}

// An opted-in exposed bind loads by design, so it is not a failure — but it is
// the most consequential setting here and must not pass silently.
func TestDoctorWarnsOnOptedInExposedBind(t *testing.T) {
	writeConfig(t, "listen_addr: \"0.0.0.0:7000\"\n"+
		"security: {disable_auth: true, allow_exposed_without_auth: true}\n")

	report := runDoctor(t, Options{ValidateConfig: realValidator})

	if got := find(t, report, "config.valid").Status; got != StatusOK {
		t.Errorf("config.valid = %q, want ok once opted in", got)
	}
	posture := find(t, report, "gateway.auth_posture")
	if posture.Status != StatusWarn {
		t.Errorf("gateway.auth_posture = %q, want warn", posture.Status)
	}
	if len(posture.Remedies) == 0 {
		t.Error("an exposed unauthenticated bind was reported with no way out")
	}
}

// Skipped must never outrank ok: an undetermined check is not a problem.
func TestWorstIgnoresSkipped(t *testing.T) {
	report := Report{Counts: map[Status]int{StatusOK: 1, StatusSkipped: 5}}
	if got := report.Worst(); got != StatusOK {
		t.Errorf("Worst = %q, want ok", got)
	}
}

func TestWriteTextIncludesRemediesAndCounts(t *testing.T) {
	writeConfig(t, brokenConfig)
	report := runDoctor(t, Options{ValidateConfig: realValidator})

	var out strings.Builder
	if err := report.WriteText(&out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"[FAIL]", "127.0.0.1:7000", "allow_exposed_without_auth", "1 FAIL"} {
		if !strings.Contains(text, want) {
			t.Errorf("rendered report missing %q:\n%s", want, text)
		}
	}
}

// Doctor is run when things are already broken, so it must not change them.
func TestDoctorStartsNothing(t *testing.T) {
	home := writeConfig(t, brokenConfig)

	runDoctor(t, Options{ValidateConfig: realValidator, DialControl: dialFails})

	for _, name := range []string{"run/inferencerig.pid", "run/control.sock"} {
		if _, err := os.Stat(filepath.Join(home, name)); err == nil {
			t.Errorf("doctor created %s", name)
		}
	}
}

func dialFails(string) (HealthChecker, error) {
	return nil, fmt.Errorf("dial must not be attempted without a listener")
}

func TestPortCheckReportsAvailablePort(t *testing.T) {
	writeConfig(t, "listen_addr: \"127.0.0.1:0\"\n")

	report := runDoctor(t, Options{ValidateConfig: realValidator})

	if got := find(t, report, "port.listen").Status; got != StatusOK {
		t.Errorf("port.listen = %q, want ok for a free port", got)
	}
}

// An occupied port is why a daemon fails to bind, so the check has to name who
// is holding it rather than just reporting a bind error.
func TestPortCheckNamesTheOwnerOfAnOccupiedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	writeConfig(t, fmt.Sprintf("listen_addr: %q\n", listener.Addr().String()))

	report := runDoctor(t, Options{ValidateConfig: realValidator})

	check := find(t, report, "port.listen")
	if check.Status != StatusFail {
		t.Fatalf("port.listen = %q, want fail for an occupied port", check.Status)
	}
	// The owner lookup needs privileges on some systems, so a partial answer is
	// acceptable — but the detail must never be empty.
	if check.Detail == "" {
		t.Error("an occupied port was reported with no explanation")
	}
}
