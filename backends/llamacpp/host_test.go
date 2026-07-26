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

func TestHostResourcesFromNVIDIA(t *testing.T) {
	b := New(Options{gpu: stubRunner{out: []byte("24564\n8192\n")}})
	host, warnings := b.HostResources(context.Background())
	if !host.HasGPU || host.VRAMBytes != int64(24564+8192)*1024*1024 {
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
