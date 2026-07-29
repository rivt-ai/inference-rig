package llamacpp

import (
	"testing"

	"inferencerig/backends"
)

func TestFitBySizeUsesGPUThenRAM(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	// GPU present: VRAM is the capacity axis.
	est := FitBySize(8*gib, backends.HostResources{HasGPU: true, VRAMBytes: 24 * gib, AvailableRAMBytes: gib})
	if est.Level != backends.FitFits {
		t.Fatalf("gpu fit = %#v", est)
	}
	// No GPU: falls back to available RAM; a huge model is too large.
	est = FitBySize(40*gib, backends.HostResources{AvailableRAMBytes: 16 * gib})
	if est.Level != backends.FitTooLarge {
		t.Fatalf("ram fit = %#v", est)
	}
}

func TestFitUnknownForUnsizedProfile(t *testing.T) {
	b := New(Options{})
	est, err := b.Fit(demoProfile("demo", "/m.gguf"), 0, backends.HostResources{AvailableRAMBytes: 64 << 30})
	if err != nil {
		t.Fatal(err)
	}
	if est.Level != backends.FitUnknown {
		t.Fatalf("fit = %#v", est)
	}
}
