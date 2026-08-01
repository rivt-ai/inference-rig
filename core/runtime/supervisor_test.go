package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"inferencerig/config"
	"inferencerig/platform/pidfile"
)

// TestMain doubles as the fake-process helper. When IR_FAKE_MODE is set the
// test binary re-execs into a fake server instead of running the suite, so the
// supervisor can drive a real child process without a real engine. Modes:
//   - "server": bind the readiness address, optionally serve ReadinessPath, then
//     exit 0 on SIGINT (the happy path).
//   - "ignore": bind the readiness address but ignore SIGINT and loop forever, so
//     Stop must escalate to SIGKILL.
//   - "noready": never bind the address (readiness never succeeds), exit 0 on
//     SIGINT, so Start hits the readiness timeout.
func TestMain(m *testing.M) {
	if os.Getenv("IR_FAKE_MODE") == "" {
		os.Exit(m.Run())
	}
	runFakeChild()
}

func runFakeChild() {
	mode := os.Getenv("IR_FAKE_MODE")
	if banner := os.Getenv("IR_FAKE_STDOUT"); banner != "" {
		fmt.Println(banner)
		fmt.Fprintln(os.Stderr, banner+"-stderr")
	}
	if mode == "server" || mode == "ignore" {
		startFakeListener(os.Getenv("IR_FAKE_ADDR"), os.Getenv("IR_FAKE_PATH"))
	}
	if mode == "ignore" {
		signal.Ignore(syscall.SIGINT)
		select {}
	}
	waitForSIGINT()
}

// startFakeListener binds addr so the supervisor's readiness probe succeeds. An
// empty path serves raw TCP (drained accepts); otherwise path is served over
// HTTP with a 200 response.
func startFakeListener(addr, path string) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		os.Exit(1)
	}
	if path == "" {
		go drainConns(ln)
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	go func() { _ = http.Serve(ln, mux) }()
}

func drainConns(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
	}
}

func waitForSIGINT() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT)
	<-ch
	os.Exit(0)
}

// freePort returns a currently-unused TCP port on the loopback interface.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// fakeSpec builds a LaunchSpec that re-execs the test binary as a fake child in
// the given mode, bound to a free port under a fresh PID directory.
func fakeSpec(t *testing.T, mode, readinessPath string) LaunchSpec {
	t.Helper()
	port := freePort(t)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	return LaunchSpec{
		Name:       "fake",
		Executable: os.Args[0],
		Env: map[string]string{
			"IR_FAKE_MODE": mode,
			"IR_FAKE_ADDR": addr,
			"IR_FAKE_PATH": readinessPath,
		},
		Host:              "127.0.0.1",
		Port:              port,
		StopTimeout:       2 * time.Second,
		ReadinessPath:     readinessPath,
		ReadinessTimeout:  3 * time.Second,
		ReadinessInterval: 25 * time.Millisecond,
		PIDDir:            t.TempDir(),
	}
}

// spawnFakeChild launches a fake child directly (not through the supervisor) and
// returns its PID, for exercising Recover. The child is killed at test cleanup.
func spawnFakeChild(t *testing.T, spec LaunchSpec, mode string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(spec.Executable)
	cmd.Env = append(os.Environ(), envList(map[string]string{
		"IR_FAKE_MODE": mode,
		"IR_FAKE_ADDR": net.JoinHostPort(spec.Host, strconv.Itoa(spec.Port)),
		"IR_FAKE_PATH": spec.ReadinessPath,
	})...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	return cmd
}

func writeSupervisorPID(t *testing.T, spec LaunchSpec, pid int) {
	t.Helper()
	if err := pidfile.New(filepath.Join(spec.PIDDir, spec.Name+".pid")).Write(pid); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorRecoversValidSurvivorWithoutRestartingIt(t *testing.T) {
	spec := fakeSpec(t, "server", "/health")
	child := spawnFakeChild(t, spec, "server")
	writeSupervisorPID(t, spec, child.Process.Pid)
	deadline := time.Now().Add(spec.ReadinessTimeout)
	for time.Now().Before(deadline) {
		if err := NewSupervisor(spec).probeReady(context.Background()); err == nil {
			break
		}
		time.Sleep(spec.ReadinessInterval)
	}

	sup := NewSupervisor(spec)
	adopted, err := sup.Recover(context.Background())
	if err != nil || !adopted {
		t.Fatalf("Recover = %v, %v", adopted, err)
	}
	status, _ := sup.Status(context.Background())
	if status.State != Running || status.Processes[0].PID != child.Process.Pid {
		t.Fatalf("status = %#v", status)
	}
	go func() { _ = child.Wait() }()
	if _, err := sup.Stop(context.Background()); err != nil {
		t.Fatalf("stop adopted survivor: %v", err)
	}
}

func TestSupervisorRecoveryClassifications(t *testing.T) {
	t.Run("stale PID file", func(t *testing.T) {
		spec := fakeSpec(t, "server", "")
		writeSupervisorPID(t, spec, 999999999)
		_, err := NewSupervisor(spec).Recover(context.Background())
		if got := RecoveryClass(err); got != RecoveryStalePIDFile {
			t.Fatalf("classification = %q, want %q (err=%v)", got, RecoveryStalePIDFile, err)
		}
	})

	t.Run("mismatched executable", func(t *testing.T) {
		spec := fakeSpec(t, "server", "")
		child := exec.Command("sleep", "120")
		child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
		writeSupervisorPID(t, spec, child.Process.Pid)
		_, err := NewSupervisor(spec).Recover(context.Background())
		if got := RecoveryClass(err); got != RecoveryMismatchedExecutable {
			t.Fatalf("classification = %q, want %q (err=%v)", got, RecoveryMismatchedExecutable, err)
		}
	})

	t.Run("occupied port", func(t *testing.T) {
		spec := fakeSpec(t, "noready", "")
		child := spawnFakeChild(t, spec, "noready")
		writeSupervisorPID(t, spec, child.Process.Pid)
		listener, err := net.Listen("tcp", net.JoinHostPort(spec.Host, strconv.Itoa(spec.Port)))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = listener.Close() }()
		go drainConns(listener)
		_, err = NewSupervisor(spec).Recover(context.Background())
		if got := RecoveryClass(err); got != RecoveryOccupiedPort {
			t.Fatalf("classification = %q, want %q (err=%v)", got, RecoveryOccupiedPort, err)
		}
	})

	t.Run("unhealthy survivor", func(t *testing.T) {
		spec := fakeSpec(t, "noready", "")
		child := spawnFakeChild(t, spec, "noready")
		writeSupervisorPID(t, spec, child.Process.Pid)
		_, err := NewSupervisor(spec).Recover(context.Background())
		if got := RecoveryClass(err); got != RecoveryUnhealthySurvivor {
			t.Fatalf("classification = %q, want %q (err=%v)", got, RecoveryUnhealthySurvivor, err)
		}
	})
}

func TestSupervisorStalePIDCleanupPreservesReplacement(t *testing.T) {
	spec := fakeSpec(t, "server", "")
	recordedPID, replacementPID := os.Getpid(), os.Getpid()+1
	writeSupervisorPID(t, spec, recordedPID)
	sup := NewSupervisor(spec)
	sup.alive = func(int) bool {
		writeSupervisorPID(t, spec, replacementPID)
		return false
	}

	if _, err := sup.Recover(context.Background()); RecoveryClass(err) != RecoveryStalePIDFile {
		t.Fatalf("Recover error = %v", err)
	}
	pid, exists, err := pidfile.New(filepath.Join(spec.PIDDir, spec.Name+".pid")).Read()
	if err != nil || !exists || pid != replacementPID {
		t.Fatalf("replacement PID file = pid %d, exists %v, err %v", pid, exists, err)
	}
}

func TestSupervisorStartStopLifecycle(t *testing.T) {
	spec := fakeSpec(t, "server", "")
	sup := NewSupervisor(spec)

	result, err := sup.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v (%+v)", err, result)
	}
	if result.ExitCode != 0 {
		t.Fatalf("Start exit code = %d, stderr=%q", result.ExitCode, result.Stderr)
	}

	status, _ := sup.Status(context.Background())
	if status.State != Running {
		t.Fatalf("state after Start = %q, want running (%#v)", status.State, status)
	}
	if len(status.Processes) != 1 || !status.Processes[0].Ready {
		t.Fatalf("process status = %#v", status.Processes)
	}
	pidPath := filepath.Join(spec.PIDDir, spec.Name+".pid")
	if _, err := os.Stat(pidPath); err != nil {
		t.Fatalf("PID file missing after Start: %v", err)
	}
	pid := status.Processes[0].PID

	if _, err := sup.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if pidfile.Alive(pid) {
		t.Fatalf("process %d still alive after Stop", pid)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("PID file not removed after Stop: %v", err)
	}
	status, _ = sup.Status(context.Background())
	if status.State != Stopped {
		t.Fatalf("state after Stop = %q, want stopped", status.State)
	}
}

func TestSupervisorHTTPReadinessLifecycle(t *testing.T) {
	spec := fakeSpec(t, "server", "/health")
	sup := NewSupervisor(spec)
	if result, err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v (%+v)", err, result)
	}
	status, _ := sup.Status(context.Background())
	if status.State != Running {
		t.Fatalf("state = %q, want running", status.State)
	}
	if _, err := sup.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestSupervisorStatusStoppedWhenNeverStarted(t *testing.T) {
	sup := NewSupervisor(LaunchSpec{Name: "fake", Host: "127.0.0.1", Port: 8080, PIDDir: t.TempDir()})
	status, err := sup.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != Stopped || len(status.Processes) != 1 || status.Processes[0].Name != "fake" {
		t.Fatalf("status = %#v", status)
	}
}

// --- Readiness timeout ----------------------------------------------------

func TestSupervisorReadinessTimeout(t *testing.T) {
	spec := fakeSpec(t, "noready", "")
	spec.ReadinessTimeout = 400 * time.Millisecond
	sup := NewSupervisor(spec)

	result, err := sup.Start(context.Background())
	if err == nil {
		t.Fatal("Start succeeded despite unbound readiness port")
	}
	if !strings.Contains(err.Error(), "readiness") {
		t.Fatalf("error = %v, want readiness timeout", err)
	}
	if result.ExitCode == 0 {
		t.Fatalf("exit code = 0 on readiness failure")
	}
	// The failed process must be cleaned up.
	status, _ := sup.Status(context.Background())
	if status.State != Stopped {
		t.Fatalf("state after failed Start = %q, want stopped", status.State)
	}
	if _, err := os.Stat(filepath.Join(spec.PIDDir, spec.Name+".pid")); !os.IsNotExist(err) {
		t.Fatalf("PID file not cleaned up after readiness failure: %v", err)
	}
}

// --- Stop-timeout escalation to SIGKILL -----------------------------------

func TestSupervisorStopTimeoutEscalatesToKill(t *testing.T) {
	spec := fakeSpec(t, "ignore", "")
	spec.StopTimeout = 300 * time.Millisecond
	sup := NewSupervisor(spec)

	if result, err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v (%+v)", err, result)
	}
	status, _ := sup.Status(context.Background())
	pid := status.Processes[0].PID
	if !pidfile.Alive(pid) {
		t.Fatalf("child %d not alive after Start", pid)
	}

	// The child ignores SIGINT, so Stop reports a timeout but SIGKILL still
	// takes it down.
	result, err := sup.Stop(context.Background())
	if err == nil {
		t.Fatal("Stop succeeded despite SIGINT-ignoring child")
	}
	if !strings.Contains(result.Stderr, "timed out") {
		t.Fatalf("stop stderr = %q, want timeout", result.Stderr)
	}
	deadline := time.Now().Add(2 * time.Second)
	for pidfile.Alive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if pidfile.Alive(pid) {
		t.Fatalf("child %d survived SIGKILL escalation", pid)
	}
}

// --- Recover --------------------------------------------------------------

func TestSupervisorNoEngineDefaultsLeak(t *testing.T) {
	sup := NewSupervisor(LaunchSpec{})
	if sup.spec.Name != "" || sup.spec.Executable != "" || sup.spec.Host != "" || sup.spec.Port != 0 {
		t.Fatalf("engine defaults leaked into spec: %#v", sup.spec)
	}
	// Neutral timing defaults are applied.
	if sup.spec.StopTimeout == 0 || sup.spec.ReadinessTimeout == 0 || sup.spec.ReadinessInterval == 0 {
		t.Fatalf("timing defaults not applied: %#v", sup.spec)
	}
}

func TestSupervisorRejectsUnsafePIDFileName(t *testing.T) {
	sup := NewSupervisor(LaunchSpec{Name: "../escape", PIDDir: t.TempDir()})
	if _, err := sup.pidFile(); err == nil {
		t.Fatal("PID file accepted unsafe name")
	}
}

func TestSupervisorPIDDirRequired(t *testing.T) {
	sup := NewSupervisor(LaunchSpec{Name: "fake"})
	if _, err := sup.pidFile(); err == nil {
		t.Fatal("PID file accepted empty PIDDir")
	}
}

func TestSupervisorStatusShape(t *testing.T) {
	sup := NewSupervisor(LaunchSpec{Name: "svc", Host: "127.0.0.1", Port: 9099, PIDDir: t.TempDir()})
	status, err := sup.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != Stopped || len(status.Processes) != 1 {
		t.Fatalf("status = %#v", status)
	}
	p := status.Processes[0]
	if p.Name != "svc" || p.Host != "127.0.0.1" || p.Port != 9099 || p.State != Stopped {
		t.Fatalf("process = %#v", p)
	}
}

func TestSupervisorBuildErrSurfacesAtStart(t *testing.T) {
	buildErr := Errorf(ErrorInvalidInput, "bad command render")
	sup := NewSupervisor(LaunchSpec{Name: "fake", PIDDir: t.TempDir(), BuildErr: buildErr})
	result, err := sup.Start(context.Background())
	if err == nil {
		t.Fatal("Start ignored BuildErr")
	}
	if Kind(err) != ErrorInvalidInput {
		t.Fatalf("kind = %q, want invalid_input", Kind(err))
	}
	if result.ExitCode != 1 {
		t.Fatalf("exit code = %d", result.ExitCode)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestSupervisorReadinessRequiresSuccessStatus(t *testing.T) {
	status := http.StatusNotFound
	sup := NewSupervisor(LaunchSpec{
		Host:              "127.0.0.1",
		Port:              8080,
		ReadinessPath:     "/v1/models",
		ReadinessInterval: time.Second,
		PIDDir:            t.TempDir(),
	})
	sup.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}

	if err := sup.probeReady(context.Background()); err == nil {
		t.Fatal("404 readiness response accepted")
	}
	status = http.StatusNoContent
	if err := sup.probeReady(context.Background()); err != nil {
		t.Fatalf("204 readiness response rejected: %v", err)
	}
}

// Engine output used to be inherited from the daemon's stdout, which meant the
// control daemon's own structured log and the engine's raw chatter shared one
// file and neither view could be read on its own. LogName gives the child its
// own service log; without it the parent's stdout is still inherited.
func TestSupervisorLogNameDivertsChildOutputToItsOwnLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)

	spec := fakeSpec(t, "server", "")
	spec.LogName = "engine"
	spec.Env["IR_FAKE_STDOUT"] = "engine-banner"
	sup := NewSupervisor(spec)

	if _, err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _, _ = sup.Stop(context.Background()) })

	logPath := filepath.Join(home, "run", "engine.log")
	var data []byte
	for range 40 {
		if read, err := os.ReadFile(logPath); err == nil && bytes.Contains(read, []byte("engine-banner")) {
			data = read
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !bytes.Contains(data, []byte("engine-banner")) {
		t.Fatalf("engine log %q missing child stdout, got %q", logPath, data)
	}
	if !bytes.Contains(data, []byte("engine-banner-stderr")) {
		t.Fatalf("engine log missing child stderr, got %q", data)
	}
}

// A spec without LogName must keep inheriting the parent's stdout, so backends
// that have not opted in are unaffected and no stray log file appears.
func TestSupervisorWithoutLogNameWritesNoServiceLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.ProjectHomeEnv, home)

	spec := fakeSpec(t, "server", "")
	spec.Env["IR_FAKE_STDOUT"] = "inherited-banner"
	sup := NewSupervisor(spec)
	if _, err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _, _ = sup.Stop(context.Background()) })

	entries, err := os.ReadDir(filepath.Join(home, "run"))
	if err == nil && len(entries) > 0 {
		t.Fatalf("unexpected service logs written: %v", entries)
	}
}

// Display is the command line that reaches logs, events and API responses, so a
// credential on the argv must never survive into it — while the paths and flags
// that explain a failed launch must survive intact.
func TestRedactArgvMasksCredentialsOnly(t *testing.T) {
	spec := LaunchSpec{Executable: "/opt/engine/server", Argv: []string{
		"--model", "/home/user/models/qwen.gguf",
		"--api-key", "sk-live-secret",
		"--hf_token=hf_secret",
		"--port", "8080",
		"--APIKEY=shouty",
		"--verbose",
	}}
	command, err := spec.command()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"sk-live-secret", "hf_secret", "shouty"} {
		if strings.Contains(command.Display, secret) {
			t.Errorf("Display leaked %q: %s", secret, command.Display)
		}
	}
	for _, kept := range []string{"/opt/engine/server", "/home/user/models/qwen.gguf", "--port", "8080", "--verbose"} {
		if !strings.Contains(command.Display, kept) {
			t.Errorf("Display dropped %q, which is diagnostic not credential: %s", kept, command.Display)
		}
	}
	// The process still receives the real arguments; only the display is masked.
	if !slices.Contains(command.Argv, "sk-live-secret") {
		t.Errorf("Argv was redacted too, so the engine would launch without its key: %v", command.Argv)
	}
}
