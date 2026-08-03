package llamacpp

import (
	"path"
	"strconv"

	"inferencerig/backends"
	"inferencerig/core/modelcatalog"
	"inferencerig/core/profiles"
)

// Fit estimates whether a model of sizeBytes fits the host. llama.cpp uses the
// discrete axis: VRAM when the host reports a GPU, otherwise available RAM.
// When the profile names a local, already-downloaded GGUF file and a context
// length, the estimate includes a KV-cache term read from that file's header;
// otherwise it falls back to the flat on-disk-size-plus-overhead estimate.
func (b *Backend) Fit(p profiles.Profile, sizeBytes int64, host backends.HostResources) (backends.FitEstimate, error) {
	contextLen := contextLenFromArgs(p.EngineArgs)
	return FitWithKV(sizeBytes, host, b.kvBytesForProfile(p, contextLen), contextLen), nil
}

// FitBySize estimates fit for a known on-disk model size against the host's
// discrete memory, adding flat runtime overhead. Ported from llamarig
// core/modelcatalog/fit.go estimateFileFit (discrete RAM/VRAM policy).
func FitBySize(sizeBytes int64, host backends.HostResources) backends.FitEstimate {
	return FitWithKV(sizeBytes, host, 0, 0)
}

// FitWithKV is FitBySize plus an optional KV-cache size: when kvBytes is
// positive, the estimate's required bytes include it (see
// modelcatalog.RequiredBytes); otherwise it is the same flat estimate as
// FitBySize.
func FitWithKV(sizeBytes int64, host backends.HostResources, kvBytes int64, contextLen int) backends.FitEstimate {
	capacity, resource := host.AvailableRAMBytes, "RAM"
	if host.HasGPU && host.VRAMBytes > 0 {
		capacity, resource = host.VRAMBytes, "VRAM"
	}
	required, note := modelcatalog.RequiredBytes(sizeBytes, kvBytes, contextLen)
	v := modelcatalog.EstimateFitDetailed(required, capacity, resource, note)
	return backends.FitEstimate{
		Level:          backends.FitLevel(v.Level),
		Reason:         v.Reason,
		RequiredBytes:  v.RequiredBytes,
		AvailableBytes: v.AvailableBytes,
	}
}

// kvBytesForProfile resolves the profile's model to a local file, if one is
// already downloaded, and estimates its KV-cache size at contextLen tokens. It
// returns 0 when the model has no local file or the file cannot be parsed as
// GGUF — both routine (a catalog listing names models that are not downloaded
// yet), not errors.
func (b *Backend) kvBytesForProfile(p profiles.Profile, contextLen int) int64 {
	if p.Model.Source == "" {
		return 0
	}
	name, _ := resolveArtifact(p.Model.Source, p.Model.Reference)
	root, err := b.modelStorageDir()
	if err != nil {
		return 0
	}
	return b.ggufKV.get(path.Join(root, path.Base(name)), contextLen)
}

// contextLenFromArgs reads engine_args.ctx-size from a profile's free-form
// engine args, which decode from YAML as int, int64, or float64 depending on
// the source. A missing or non-numeric value yields 0 (unknown context).
func contextLenFromArgs(args map[string]any) int {
	raw, ok := args["ctx-size"]
	if !ok {
		return 0
	}
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}
