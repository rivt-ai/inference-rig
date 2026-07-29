package signals

import (
	"context"
	"testing"
)

func TestGopsutilCollectorMachineReportsTotalMemory(t *testing.T) {
	machine, err := (&GopsutilCollector{}).Machine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if machine.Memory.TotalBytes == 0 {
		t.Fatalf("machine = %#v", machine)
	}
}

// A unified-memory device draws from system RAM, so the collector fills its
// byte fields from the memory stats; a discrete device keeps its own VRAM.
func TestGopsutilCollectorResolvesUnifiedMemoryAccelerators(t *testing.T) {
	const vramTotal, vramUsed = 24 << 30, 3 << 30
	collector := NewGopsutilCollector(nil, nil)
	collector.Accelerators = func(context.Context) ([]AcceleratorStats, []string) {
		return []AcceleratorStats{
			{Name: "Apple Metal", UnifiedMemory: true},
			{Name: "NVIDIA GeForce RTX 4090", TotalBytes: vramTotal, UsedBytes: vramUsed},
		}, nil
	}
	snapshot, err := collector.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Memory.TotalBytes == 0 {
		t.Skip("host memory stats unavailable")
	}
	if len(snapshot.Accelerators) != 2 {
		t.Fatalf("accelerators = %#v", snapshot.Accelerators)
	}
	unified := snapshot.Accelerators[0]
	if unified.TotalBytes != snapshot.Memory.TotalBytes || unified.UsedBytes != snapshot.Memory.UsedBytes {
		t.Fatalf("unified = %#v, memory = %#v", unified, snapshot.Memory)
	}
	if discrete := snapshot.Accelerators[1]; discrete.TotalBytes != vramTotal || discrete.UsedBytes != vramUsed {
		t.Fatalf("discrete = %#v", discrete)
	}
}

// A collector without an accelerator probe reports no rows and no warning.
func TestGopsutilCollectorWithoutAcceleratorProbe(t *testing.T) {
	snapshot, err := NewGopsutilCollector(nil, nil).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Accelerators) != 0 {
		t.Fatalf("accelerators = %#v", snapshot.Accelerators)
	}
	for _, warning := range snapshot.Warnings {
		if warning == "runtime process provider unavailable" {
			t.Fatalf("warnings = %v", snapshot.Warnings)
		}
	}
}
