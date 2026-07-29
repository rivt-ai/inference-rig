package mlx

import (
	"reflect"
	"strings"
	"testing"

	"inferencerig/backends"
	"inferencerig/core/profiles"
)

func testProfile(name, source string) profiles.Profile {
	return profiles.Profile{
		Version: "1", Name: name, Backend: Name,
		Model:  profiles.ModelSpec{Source: source},
		Listen: profiles.ListenSpec{Host: defaultHost, Port: 8080},
	}
}

func TestCommandRendering(t *testing.T) {
	p := testProfile("demo", "/models/demo")
	p.EngineArgs = map[string]any{
		"max-tokens":   4096,
		"trust-remote": true,
		"log-disable":  false,
	}
	command, err := buildCommand("/venv/bin/python", p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"-m", "mlx_lm", "server", "--model", "/models/demo",
		"--host", defaultHost, "--port", "8080",
		"--no-log-disable", "--max-tokens", "4096", "--trust-remote",
	}
	if !reflect.DeepEqual(command.Argv, want) {
		t.Fatalf("argv = %#v, want %#v", command.Argv, want)
	}
}

func TestReservedAndUnsupportedArgsRejected(t *testing.T) {
	for _, args := range []map[string]any{
		{"port": 9},
		{"bad": map[string]string{"nested": "value"}},
		{"--leading": true},
	} {
		p := testProfile("demo", "model")
		p.EngineArgs = args
		if _, err := buildCommand("python", p); err == nil {
			t.Fatalf("accepted args %#v", args)
		}
	}
}

func TestLaunchSpecUsesGeneratedCommand(t *testing.T) {
	b := New(Options{Executable: "/venv/bin/python", PIDDir: t.TempDir()})
	p := testProfile("demo", "owner/model")
	m, err := b.Materialize(p)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := b.LaunchSpec(p, m)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Executable != "/venv/bin/python" || spec.Name != "mlx-demo" ||
		spec.ReadinessPath != defaultReadinessURL || !strings.Contains(m.Summary, "mlx_lm server") {
		t.Fatalf("spec = %#v, materialization = %#v", spec, m)
	}
}

func TestCapabilities(t *testing.T) {
	c := New(Options{}).Capabilities()
	if !c.MultiFileArtifacts || !c.UnifiedMemory || !c.ManagedInstall || !c.SingleActiveProfile ||
		c.SingleFileArtifacts || c.DiscreteVRAM {
		t.Fatalf("capabilities = %#v", c)
	}
}

var _ backends.Backend = New(Options{})
