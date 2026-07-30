//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newLlamacppRig builds a rig wired to the pinned llama.cpp fixtures. The
// engine binary is reached through PATH and the GGUF is copied into the rig's
// own model storage, so nothing is shared between tests.
func newLlamacppRig(t *testing.T) *rig {
	t.Helper()
	engine := requireEnv(t, engineBinEnv)
	model := requireEnv(t, modelEnv)
	r := newRig(t, filepath.Dir(engine))
	r.installModel(model)
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

func (r *rig) socketPath() string { return filepath.Join(r.home, "run", "control.sock") }

func (r *rig) pidPath(name string) string {
	return filepath.Join(r.home, "run", name+".pid")
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
