package mlx

import (
	"inferencerig/backends"
	"inferencerig/core/modelcatalog"
	"inferencerig/core/profiles"
)

// Fit estimates unified-memory fit for a model of sizeBytes.
func (b *Backend) Fit(_ profiles.Profile, sizeBytes int64, host backends.HostResources) (backends.FitEstimate, error) {
	return FitBySize(sizeBytes, host), nil
}

// FitBySize estimates unified-memory fit for a known snapshot size.
func FitBySize(size int64, host backends.HostResources) backends.FitEstimate {
	capacity := host.MemoryBudgetBytes
	if capacity <= 0 {
		capacity = host.AvailableRAMBytes
	}
	required := int64(0)
	if size > 0 {
		required = size + modelcatalog.DefaultOverheadBytes
	}
	verdict := modelcatalog.EstimateFit(required, capacity, "unified memory")
	return backends.FitEstimate{
		Level: backends.FitLevel(verdict.Level), Reason: verdict.Reason,
		RequiredBytes: verdict.RequiredBytes, AvailableBytes: verdict.AvailableBytes,
	}
}

// MemoryBudget reserves floor bytes after applying fraction to total memory.
func MemoryBudget(total int64, fraction float64, floor int64) int64 {
	return max(int64(float64(total)*fraction)-floor, 0)
}
