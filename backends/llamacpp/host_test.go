package llamacpp

import (
	"context"
	"os/exec"
	"testing"
)

type stubRunner struct {
	out []byte
	err error
}

func (s stubRunner) Run(context.Context, string, ...string) ([]byte, error) { return s.out, s.err }

// byCommandRunner routes to a different stub per command name, so a test can
// exercise the NVIDIA-then-AMD fallback order.
type byCommandRunner map[string]stubRunner

func (r byCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	stub, ok := r[name]
	if !ok {
		return nil, exec.ErrNotFound
	}
	return stub.Run(ctx, name, args...)
}

const mib = 1024 * 1024

func TestHostResourcesFromNVIDIA(t *testing.T) {
	b := New(Options{gpu: stubRunner{out: []byte("24564, 3072, NVIDIA GeForce RTX 4090\n")}})
	host, warnings := b.HostResources(context.Background())
	if !host.HasGPU || host.VRAMBytes != 24564*mib || host.VRAMUsedBytes != 3072*mib {
		t.Fatalf("host = %#v, warnings = %v", host, warnings)
	}
	if host.AcceleratorName != "NVIDIA GeForce RTX 4090" || host.UnifiedMemory {
		t.Fatalf("host = %#v", host)
	}
}

// Multiple adapters pool into one capacity figure named after the first.
func TestHostResourcesPoolsMultipleNVIDIA(t *testing.T) {
	b := New(Options{gpu: stubRunner{out: []byte("24564, 3072, NVIDIA GeForce RTX 4090\n8192, 1024, NVIDIA GeForce RTX 3070\n")}})
	host, warnings := b.HostResources(context.Background())
	if host.VRAMBytes != (24564+8192)*mib || host.VRAMUsedBytes != (3072+1024)*mib {
		t.Fatalf("host = %#v, warnings = %v", host, warnings)
	}
	if host.AcceleratorName != "2× NVIDIA GeForce RTX 4090" {
		t.Fatalf("name = %q", host.AcceleratorName)
	}
}

func TestHostResourcesRejectsMalformedNVIDIA(t *testing.T) {
	b := New(Options{gpu: stubRunner{out: []byte("24564\n")}})
	host, warnings := b.HostResources(context.Background())
	if host.HasGPU || len(warnings) == 0 {
		t.Fatalf("host = %#v, warnings = %v", host, warnings)
	}
}

func TestHostResourcesNoGPU(t *testing.T) {
	b := New(Options{gpu: stubRunner{out: []byte(""), err: context.Canceled}})
	host, warnings := b.HostResources(context.Background())
	if host.HasGPU || len(warnings) == 0 {
		t.Fatalf("host = %#v, warnings = %v", host, warnings)
	}
}

// No accelerator found: the state is named explicitly rather than left as a
// zero value, so a client can say "CPU only" instead of showing nothing.
func TestHostResourcesCPUOnly(t *testing.T) {
	b := New(Options{gpu: byCommandRunner{}}) // neither nvidia-smi nor rocm-smi resolve
	host, _ := b.HostResources(context.Background())
	if host.HasGPU {
		t.Fatalf("host = %#v, want HasGPU false", host)
	}
	if host.AcceleratorName != "CPU" {
		t.Fatalf("AcceleratorName = %q, want %q", host.AcceleratorName, "CPU")
	}
}

func TestHostResourcesFromAMDWhenNVIDIAAbsent(t *testing.T) {
	amdJSON := `{
		"card0": {
			"Card series": "AMD Radeon RX 7900 XTX",
			"VRAM Total Memory (B)": "25753026560",
			"VRAM Total Used Memory (B)": "1073741824"
		}
	}`
	b := New(Options{gpu: byCommandRunner{
		"rocm-smi": stubRunner{out: []byte(amdJSON)},
	}})
	host, warnings := b.HostResources(context.Background())
	if !host.HasGPU {
		t.Fatalf("host = %#v, warnings = %v", host, warnings)
	}
	if host.VRAMBytes != 25753026560 || host.VRAMUsedBytes != 1073741824 {
		t.Fatalf("host = %#v", host)
	}
	if host.AcceleratorName != "AMD Radeon RX 7900 XTX" {
		t.Fatalf("AcceleratorName = %q", host.AcceleratorName)
	}
}

func TestHostResourcesPoolsMultipleAMD(t *testing.T) {
	amdJSON := `{
		"card0": {"VRAM Total Memory (B)": "12884901888", "VRAM Total Used Memory (B)": "0"},
		"card1": {"VRAM Total Memory (B)": "12884901888", "VRAM Total Used Memory (B)": "0"}
	}`
	b := New(Options{gpu: byCommandRunner{
		"rocm-smi": stubRunner{out: []byte(amdJSON)},
	}})
	host, _ := b.HostResources(context.Background())
	if host.VRAMBytes != 2*12884901888 {
		t.Fatalf("VRAMBytes = %d, want pooled sum", host.VRAMBytes)
	}
	if host.AcceleratorName != "2× AMD GPU" {
		t.Fatalf("AcceleratorName = %q, want disclosed device count", host.AcceleratorName)
	}
}

func TestHostResourcesRejectsMalformedAMD(t *testing.T) {
	b := New(Options{gpu: byCommandRunner{
		"rocm-smi": stubRunner{out: []byte("not json")},
	}})
	host, warnings := b.HostResources(context.Background())
	if host.HasGPU || len(warnings) == 0 {
		t.Fatalf("host = %#v, warnings = %v", host, warnings)
	}
}

// NVIDIA present: rocm-smi is never invoked, so an NVIDIA host does not warn
// about a missing rocm-smi.
func TestHostResourcesPrefersNVIDIAOverAMD(t *testing.T) {
	b := New(Options{gpu: byCommandRunner{
		"nvidia-smi": stubRunner{out: []byte("24564, 3072, NVIDIA GeForce RTX 4090\n")},
	}})
	host, warnings := b.HostResources(context.Background())
	if host.AcceleratorName != "NVIDIA GeForce RTX 4090" {
		t.Fatalf("host = %#v", host)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none when nvidia-smi succeeds", warnings)
	}
}
