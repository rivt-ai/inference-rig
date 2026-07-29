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
	total := 16 * gib
	if got := MemoryBudget(total, 0.8, 2*gib); got != int64(float64(total)*0.8)-2*gib {
		t.Fatalf("budget = %d", got)
	}
}
