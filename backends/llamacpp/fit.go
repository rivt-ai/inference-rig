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
	arch := b.archForProfile(p)
	return FitWithArch(sizeBytes, host, arch, contextLenFromArgs(p.EngineArgs)), nil
}

// FitBySize estimates fit for a known on-disk model size against the host's
// discrete memory, adding flat runtime overhead. Ported from llamarig
// core/modelcatalog/fit.go estimateFileFit (discrete RAM/VRAM policy).
func FitBySize(sizeBytes int64, host backends.HostResources) backends.FitEstimate {
	return FitWithArch(sizeBytes, host, nil, 0)
}

// FitWithArch is FitBySize plus an optional architecture and context length:
// when both are known, the estimate's required bytes include a KV-cache term
// (see modelcatalog.RequiredBytes); otherwise it is the same flat estimate as
// FitBySize.
func FitWithArch(sizeBytes int64, host backends.HostResources, arch *modelcatalog.Arch, contextLen int) backends.FitEstimate {
	capacity, resource := host.AvailableRAMBytes, "RAM"
	if host.HasGPU && host.VRAMBytes > 0 {
		capacity, resource = host.VRAMBytes, "VRAM"
	}
	required, note := modelcatalog.RequiredBytes(sizeBytes, arch, contextLen)
	v := modelcatalog.EstimateFitDetailed(required, capacity, resource, note)
	return backends.FitEstimate{
		Level:          backends.FitLevel(v.Level),
		Reason:         v.Reason,
		RequiredBytes:  v.RequiredBytes,
		AvailableBytes: v.AvailableBytes,
	}
}

// archForProfile resolves the profile's model to a local file, if one is
// already downloaded, and reads its GGUF architecture metadata. It returns nil
// when the model has no local file or the file cannot be parsed as GGUF —
// both are routine (a catalog listing names models that are not downloaded
// yet), not errors.
func (b *Backend) archForProfile(p profiles.Profile) *modelcatalog.Arch {
	if p.Model.Source == "" {
		return nil
	}
	name, _ := resolveArtifact(p.Model.Source, p.Model.Reference)
	root, err := b.modelStorageDir()
	if err != nil {
		return nil
	}
	return b.ggufArches.get(path.Join(root, path.Base(name)))
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
