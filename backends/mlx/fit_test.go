package mlx

import (
	"testing"

	"inferencerig/backends"
)

func TestFitUsesUnifiedMemoryBudget(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	fit := FitBySize(8*gib, backends.HostResources{
		UnifiedMemory: true, MemoryBudgetBytes: 10 * gib, AvailableRAMBytes: gib,
	})
	if fit.Level != backends.FitFits {
		t.Fatalf("fit = %#v", fit)
	}
}
