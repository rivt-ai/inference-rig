package llamacpp

import (
	"context"
	"testing"
)

type stubRunner struct {
	out []byte
	err error
}

func (s stubRunner) Run(context.Context, string, ...string) ([]byte, error) { return s.out, s.err }

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
