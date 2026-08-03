package tui

import (
	"strings"
	"testing"

	controlv1 "inferencerig/core/rpc/gen/v1"
)

// The accelerator name is the only thing that discloses a pooled multi-GPU
// capacity or a CPU-only host. Without it a summed VRAM figure reads as one
// device, which would invite fitting a model no single card can hold.
func TestRankedAcceleratorNamesDeviceAndCapacity(t *testing.T) {
	got := rankedAccelerator(&controlv1.MachineProfile{
		AcceleratorName:        "2× NVIDIA GeForce RTX 4090",
		AcceleratorMemoryBytes: 48 * 1024 * 1024 * 1024,
	})
	if !strings.Contains(got, "2× NVIDIA GeForce RTX 4090") {
		t.Fatalf("rankedAccelerator() = %q, want the pooled device name", got)
	}
	if !strings.Contains(got, "48") {
		t.Fatalf("rankedAccelerator() = %q, want the capacity alongside the name", got)
	}
}

func TestRankedAcceleratorHandlesCPUOnlyHost(t *testing.T) {
	if got := rankedAccelerator(&controlv1.MachineProfile{AcceleratorName: "CPU"}); got != "CPU" {
		t.Fatalf("rankedAccelerator() = %q, want a bare CPU name with no capacity", got)
	}
}

// An older server sends no name, and a nil profile is what a failed catalog
// fetch leaves behind. Both must render nothing rather than an empty field.
func TestRankedAcceleratorEmptyWithoutName(t *testing.T) {
	if got := rankedAccelerator(&controlv1.MachineProfile{AcceleratorMemoryBytes: 1024}); got != "" {
		t.Fatalf("rankedAccelerator() = %q, want empty when the server sent no name", got)
	}
	if got := rankedAccelerator(nil); got != "" {
		t.Fatalf("rankedAccelerator(nil) = %q, want empty", got)
	}
}
