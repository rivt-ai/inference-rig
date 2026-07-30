//go:build e2e || e2emlx || e2ebrowser

// Package e2e drives compiled InferenceRig binaries against a real, pinned
// llama.cpp build and a real, pinned GGUF model.
//
// Nothing here is faked. The tests exec the same binary a user installs, talk
// to it over the same control socket and the same public gateway, and require
// the engine to load the model and generate tokens. That is the point: package
// tests already cover branches exhaustively, so the only thing this layer can
// usefully add is proof that the compiled pieces fit together.
//
// Build with -tags=e2e and run through `make e2e`, which provisions the pinned
// fixtures first.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Bounds on every wait in the suite. They are generous enough for a cold CI
// runner and short enough that a hang fails rather than eating the job budget.
const (
	readyTimeout = 90 * time.Second
	pollInterval = 200 * time.Millisecond
	cliTimeout   = 60 * time.Second
)

// Environment contract, resolved by scripts/provision-e2e-llamacpp.sh for the
// llama.cpp suite and by the MLX workflow for the Apple Silicon suite.
const (
	engineBinEnv = "INFERENCERIG_E2E_LLAMACPP_BIN"
	modelEnv     = "INFERENCERIG_E2E_MODEL"
	mlxPythonEnv = "INFERENCERIG_LIVE_MLX_PYTHON"
	mlxModelEnv  = "INFERENCERIG_LIVE_MLX_MODEL"
)

// requireEnv resolves a provisioned fixture path. A missing fixture fails the
// run; it is never a skip, because a skipped engine test and a passing engine
// test are indistinguishable in a green check, and that ambiguity is exactly
// what this suite exists to remove.
func requireEnv(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		t.Fatalf("%s is unset; the job must provision this fixture before running the suite", key)
	}
	return value
}

// binary is the coverage-instrumented inferencerig binary, built once for the
// whole package.
var binary string

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(m *testing.M) (int, error) {
	dir, err := os.MkdirTemp("", "inferencerig-e2e-bin")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	binary, err = buildInstrumented(dir)
	if err != nil {
		return 0, err
	}
	return m.Run(), nil
}

// buildInstrumented builds the real root command with coverage instrumentation
// so the child processes these tests drive contribute to the coverage artifact
// rather than reading as dead code.
func buildInstrumented(dir string) (string, error) {
	root, err := repoRoot()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "inferencerig")
	build := exec.Command("go", "build", "-cover", "-coverpkg=./...", "-o", path, ".")
	build.Dir = root
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return "", fmt.Errorf("build instrumented binary: %w", err)
	}
	return path, nil
}

func repoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("resolve repo root: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// rig is one fully isolated InferenceRig installation: its own home, config,
// control socket, model copy, ports, and logs. Nothing is shared between rigs,
// so tests are parallel-safe and a developer's real ~/.inferencerig is never
// touched.
type rig struct {
	t           *testing.T
	home        string
	modelPath   string
	gatewayPort int
	token       string
	env         []string
}

// newRig builds an isolated installation whose PATH resolves the engine binary
// from engineDir, so the daemon finds the engine exactly the way it finds a
// user's installed one rather than through a test-only hook.
func newRig(t *testing.T, engineDir string) *rig {
	t.Helper()
	r := &rig{
		t:           t,
		home:        t.TempDir(),
		gatewayPort: freePort(t),
		token:       "e2e-test-token",
	}
	r.writeConfig()
	r.env = r.buildEnv(engineDir)
	return r
}

// installModel gives every rig its own model file under the rig's model storage
// dir, so the engine resolves it through the same directory the daemon
// configures rather than through a path only the test knows.
func (r *rig) installModel(source string) string {
	r.t.Helper()
	dir := filepath.Join(r.home, "models")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.t.Fatal(err)
	}
	dest := filepath.Join(dir, filepath.Base(source))
	// A hard link keeps a ~100 MB fixture free per rig; it falls back to a copy
	// when the cache and the temp dir are on different filesystems.
	if err := os.Link(source, dest); err == nil {
		r.modelPath = dest
		return dest
	}
	if err := copyFile(source, dest); err != nil {
		r.t.Fatal(err)
	}
	r.modelPath = dest
	return dest
}

func copyFile(source, dest string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func (r *rig) writeConfig() {
	r.t.Helper()
	content := fmt.Sprintf("listen_addr: 127.0.0.1:%d\nmodel_storage_dir: %s\n",
		r.gatewayPort, filepath.Join(r.home, "models"))
	if err := os.WriteFile(r.configPath(), []byte(content), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) configPath() string { return filepath.Join(r.home, "config.yaml") }

// buildEnv layers the rig's private locations over the ambient environment and
// puts the provisioned engine first on PATH.
func (r *rig) buildEnv(engineDir string) []string {
	r.t.Helper()
	env := []string{
		"INFERENCERIG_HOME=" + r.home,
		"INFERENCERIG_CONFIG=" + r.configPath(),
		"INFERENCERIG_CONTROL_TOKEN=" + r.token,
		"PATH=" + engineDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + r.home,
	}
	if dir := coverDir(r.t); dir != "" {
		env = append(env, "GOCOVERDIR="+dir)
	}
	return env
}

// coverDir is where instrumented child processes drop their coverage data. It
// is the repo's coverage artifact dir so scripts/go-coverage.sh can fold system
// coverage into the same report as the package suite.
func coverDir(t *testing.T) string {
	t.Helper()
	root, err := repoRoot()
	if err != nil {
		return ""
	}
	dir := filepath.Join(root, "artifacts", "coverage", "e2e")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return dir
}

// process is a child InferenceRig process owned by the harness. Foreground
// children are used deliberately: the harness holds the handle, so a leak is
// impossible and output is captured for free.
type process struct {
	name    string
	cmd     *exec.Cmd
	logPath string
	// exited is closed once, so every observer sees the exit. A one-shot value
	// channel would let the first reader consume the only notification and
	// leave the next stop (the cleanup one) waiting forever on a dead process.
	exited  chan struct{}
	waitErr error
}

func (r *rig) start(name string, args ...string) *process {
	r.t.Helper()
	logDir := filepath.Join(r.home, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		r.t.Fatal(err)
	}
	logPath := filepath.Join(logDir, name+".log")
	// Child output goes to a real file rather than an in-process buffer on
	// purpose. os/exec implements a non-*os.File writer with a pipe plus a
	// copying goroutine, and Wait blocks until that pipe reaches EOF — which a
	// grandchild engine process inheriting the same pipe never allows. A file
	// descriptor removes the dependency: Wait then observes only the child.
	logFile, err := os.Create(logPath)
	if err != nil {
		r.t.Fatal(err)
	}
	cmd := exec.Command(binary, args...)
	cmd.Env = r.env
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		r.t.Fatalf("start %s: %v", name, err)
	}
	_ = logFile.Close()
	p := &process{name: name, cmd: cmd, logPath: logPath, exited: make(chan struct{})}
	go func() {
		p.waitErr = cmd.Wait()
		close(p.exited)
	}()
	r.t.Cleanup(func() {
		p.stop(r.t)
		if r.t.Failed() {
			r.t.Logf("%s output:\n%s", p.name, p.output())
		}
	})
	return p
}

// output is the child's captured stdout and stderr, used to explain a failure
// without an interactive rerun.
func (p *process) output() string {
	data, err := os.ReadFile(p.logPath)
	if err != nil {
		return "(no output: " + err.Error() + ")"
	}
	return string(data)
}

// stop asks the process to exit the way an operator would, and only reports
// failure if it does not. It is idempotent so an explicit stop inside a test
// does not conflict with the cleanup stop.
func (p *process) stop(t *testing.T) {
	t.Helper()
	if p.cmd.Process == nil {
		return
	}
	select {
	case <-p.exited:
		return
	default:
	}
	_ = p.cmd.Process.Signal(os.Interrupt)
	select {
	case <-p.exited:
	case <-time.After(30 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.exited
		t.Errorf("%s did not exit on SIGINT\n%s", p.name, p.output())
	}
}

// startControl launches the control daemon and waits until it answers a real
// RPC, not merely until its socket file appears.
func (r *rig) startControl() *process {
	r.t.Helper()
	daemon := r.start("control", "serve")
	waitFor(r.t, "control daemon health", func() bool {
		_, _, err := r.tryCLI("health")
		return err == nil
	}, daemon)
	return daemon
}

func (r *rig) socketPath() string { return filepath.Join(r.home, "run", "control.sock") }

func (r *rig) pidPath(name string) string {
	return filepath.Join(r.home, "run", name+".pid")
}

// cli runs a compiled CLI command and fails the test on a non-zero exit.
func (r *rig) cli(args ...string) string {
	r.t.Helper()
	stdout, stderr, err := r.tryCLI(args...)
	if err != nil {
		r.t.Fatalf("inferencerig %s: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, stdout, stderr)
	}
	return stdout
}

func (r *rig) tryCLI(args ...string) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = r.env
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

// cliJSON runs a CLI command and decodes its protojson output.
func (r *rig) cliJSON(args ...string) map[string]any {
	r.t.Helper()
	var decoded map[string]any
	output := r.cli(args...)
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		r.t.Fatalf("inferencerig %s: decode %q: %v", strings.Join(args, " "), output, err)
	}
	return decoded
}

// profileYAML renders a canonical llama.cpp profile pointing at this rig's
// model copy.
func (r *rig) profileYAML(name string, port int) string {
	return fmt.Sprintf("version: 1\nname: %s\nbackend: llamacpp\nmodel:\n  source: %s\nlisten:\n  host: 127.0.0.1\n  port: %d\nengine_args:\n  ctx-size: 512\n",
		name, r.modelPath, port)
}

// writeProfile writes a canonical profile YAML file and returns its path.
func (r *rig) writeProfile(name string, port int) string {
	r.t.Helper()
	yaml := r.profileYAML(name, port)
	path := filepath.Join(r.home, name+".yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		r.t.Fatal(err)
	}
	return path
}

// waitFor polls condition until it holds, failing with the child's captured
// output when it does not. Polling an observable condition is the only
// readiness signal used in this package; there are deliberately no sleeps.
func waitFor(t *testing.T, what string, condition func() bool, children ...*process) {
	t.Helper()
	deadline := time.Now().Add(readyTimeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		for _, child := range children {
			select {
			case <-child.exited:
				t.Fatalf("%s: %s exited early (%v)\n%s", what, child.name, child.waitErr, child.output())
			default:
			}
		}
		time.Sleep(pollInterval)
	}
	for _, child := range children {
		t.Logf("%s output:\n%s", child.name, child.output())
	}
	t.Fatalf("timed out waiting for %s after %s", what, readyTimeout)
}

func freePort(t *testing.T) int {
	t.Helper()
	// Binding a real listener and reading back the assigned port is the only
	// allocation that cannot collide with a parallel test guessing the same
	// number.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port
}

// chatCompletion asks the running engine to generate and returns the text it
// produced. Readiness proves a port is open; only this proves the model loaded
// and the pipeline works.
func chatCompletion(t *testing.T, baseURL, model string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"model":       model,
		"messages":    []map[string]string{{"role": "user", "content": "Reply with the single word: ready"}},
		"max_tokens":  16,
		"temperature": 0,
		"seed":        1,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("chat completion: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	payload, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("chat completion: status %d: %s", response.StatusCode, payload)
	}
	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("chat completion: decode %s: %v", payload, err)
	}
	if decoded.Usage.CompletionTokens < 1 || len(decoded.Choices) == 0 {
		t.Fatalf("engine generated no tokens: %s", payload)
	}
	return decoded.Choices[0].Message.Content
}

// httpGet is a small helper for the plain (non-Connect) gateway routes.
func httpGet(t *testing.T, url string, headers map[string]string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(body)
}

// runtimeState pulls the state out of any response carrying a runtime status.
func runtimeState(response map[string]any) string {
	status, _ := response["status"].(map[string]any)
	state, _ := status["state"].(string)
	return state
}

// startGateway launches `inferencerig web` and waits until it serves.
func (r *rig) startGateway() *process {
	r.t.Helper()
	gateway := r.start("web", "web")
	waitFor(r.t, "gateway health", func() bool {
		// A soft probe: before the listener is up a dial simply fails, which is
		// the normal state being polled for, not a test failure.
		response, err := http.Get(r.gatewayURL() + "/health") //nolint:noctx // bounded by waitFor
		if err != nil {
			return false
		}
		_ = response.Body.Close()
		return response.StatusCode == http.StatusOK
	}, gateway)
	return gateway
}

func (r *rig) gatewayURL() string {
	return "http://127.0.0.1:" + itoa(r.gatewayPort)
}

func itoa(n int) string { return strconv.Itoa(n) }

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
