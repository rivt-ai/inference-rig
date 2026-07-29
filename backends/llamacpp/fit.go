package llamacpp

import (
	"inferencerig/backends"
	"inferencerig/core/modelcatalog"
	"inferencerig/core/profiles"
)

// Fit estimates whether a model of sizeBytes fits the host. llama.cpp uses the
// discrete axis: VRAM when the host reports a GPU, otherwise available RAM.
func (b *Backend) Fit(_ profiles.Profile, sizeBytes int64, host backends.HostResources) (backends.FitEstimate, error) {
	return FitBySize(sizeBytes, host), nil
}

// FitBySize estimates fit for a known on-disk model size against the host's
// discrete memory, adding runtime overhead. Ported from llamarig
// core/modelcatalog/fit.go estimateFileFit (discrete RAM/VRAM policy).
func FitBySize(sizeBytes int64, host backends.HostResources) backends.FitEstimate {
	capacity, resource := host.AvailableRAMBytes, "RAM"
	if host.HasGPU && host.VRAMBytes > 0 {
		capacity, resource = host.VRAMBytes, "VRAM"
	}
	required := int64(0)
	if sizeBytes > 0 {
		required = sizeBytes + modelcatalog.DefaultOverheadBytes
	}
	v := modelcatalog.EstimateFit(required, capacity, resource)
	return backends.FitEstimate{
		Level:          backends.FitLevel(v.Level),
		Reason:         v.Reason,
		RequiredBytes:  v.RequiredBytes,
		AvailableBytes: v.AvailableBytes,
	}
}
