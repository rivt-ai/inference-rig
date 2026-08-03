package modelcatalog

import "fmt"

// Fit levels are the neutral verdicts the shared fit math produces. They mirror
// the backend contract's FitLevel vocabulary without coupling this package to
// it, so a backend maps them onto its own return type.
const (
	FitUnknown  = "unknown"
	FitFits     = "fits"
	FitMarginal = "marginal"
	FitTooLarge = "too_large"
)

// DefaultOverheadBytes is the runtime overhead added to a model's on-disk size
// when estimating memory required to serve it. Used as the whole estimate when
// architecture metadata or context length is unavailable, and as the non-KV
// overhead term when they are.
const DefaultOverheadBytes int64 = 512 * 1024 * 1024

// DefaultKVBytesPerElem is the per-element size assumed for the KV cache
// (f16). llama.cpp's --cache-type-k/v flags can change this at runtime; this
// is a default, not a hard constant.
const DefaultKVBytesPerElem int64 = 2

// KVCacheBytes estimates the KV cache size for a model with architecture a
// served at contextLen tokens, at bytesPerElem bytes per cached value.
//
// This is the standard estimate for transformer KV cache size (2 caches ×
// layers × context × head_dim × kv_heads × element size) and is believed to
// match llama.cpp's cache layout, but has not been verified against llama.cpp
// source or a measured allocation — validate against a real model load before
// relying on it for capacity decisions.
func KVCacheBytes(a Arch, contextLen int, bytesPerElem int64) int64 {
	if a.BlockCount == 0 || a.HeadCountKV == 0 || contextLen <= 0 || bytesPerElem <= 0 {
		return 0
	}
	headDim := a.KeyLength
	if headDim == 0 && a.EmbeddingLength > 0 && a.HeadCountKV > 0 {
		headDim = a.EmbeddingLength / a.HeadCountKV
	}
	if headDim == 0 {
		return 0
	}
	valueDim := a.ValueLength
	if valueDim == 0 {
		valueDim = headDim
	}
	perToken := int64(headDim+valueDim) * int64(a.HeadCountKV) * bytesPerElem
	return perToken * int64(a.BlockCount) * int64(contextLen)
}

// RequiredBytes estimates the memory required to serve a model of sizeBytes
// on disk. When arch is nil or contextLen is non-positive, it falls back to
// sizeBytes plus DefaultOverheadBytes — today's behavior — and reports that
// the estimate excludes the KV cache. Otherwise it adds the KV-cache term
// computed from arch and contextLen.
func RequiredBytes(sizeBytes int64, arch *Arch, contextLen int) (bytes int64, note string) {
	if sizeBytes <= 0 {
		return 0, ""
	}
	if arch == nil || contextLen <= 0 {
		return sizeBytes + DefaultOverheadBytes, "context not known; excludes KV cache"
	}
	kv := KVCacheBytes(*arch, contextLen, DefaultKVBytesPerElem)
	if kv == 0 {
		return sizeBytes + DefaultOverheadBytes, "architecture metadata incomplete; excludes KV cache"
	}
	return sizeBytes + DefaultOverheadBytes + kv, fmt.Sprintf("includes %.1f GiB KV cache at %d tokens context", gib(kv), contextLen)
}

// Verdict is a neutral memory-fit estimate.
type Verdict struct {
	Level          string
	Reason         string
	RequiredBytes  int64
	AvailableBytes int64
}

// EstimateFit compares required against available capacity using a shared
// policy: a model fitting within 90% of capacity "fits", within 100%
// "marginal", beyond "too_large"; a non-positive requirement or capacity is
// "unknown". resource names the capacity axis (e.g. "RAM" or "VRAM") for the
// human-readable reason.
func EstimateFit(required, available int64, resource string) Verdict {
	v := Verdict{Level: FitUnknown, RequiredBytes: required, AvailableBytes: available}
	if required <= 0 || available <= 0 {
		v.Reason = "memory capacity is unknown"
		return v
	}
	switch {
	case required <= int64(float64(available)*0.90):
		v.Level = FitFits
		v.Reason = fmt.Sprintf("estimated %s %.1f GiB is within 90%% of capacity", resource, gib(required))
	case required <= available:
		v.Level = FitMarginal
		v.Reason = fmt.Sprintf("estimated %s %.1f GiB is close to capacity", resource, gib(required))
	default:
		v.Level = FitTooLarge
		v.Reason = fmt.Sprintf("estimated %s %.1f GiB exceeds capacity", resource, gib(required))
	}
	return v
}

// EstimateFitDetailed is EstimateFit with an additional free-form note (e.g.
// from RequiredBytes, or a multi-GPU device breakdown) appended to the reason.
// note is ignored when the verdict is FitUnknown, since "capacity is unknown"
// already explains itself.
func EstimateFitDetailed(required, available int64, resource, note string) Verdict {
	v := EstimateFit(required, available, resource)
	if note != "" && v.Level != FitUnknown {
		v.Reason = fmt.Sprintf("%s (%s)", v.Reason, note)
	}
	return v
}

func gib(value int64) float64 { return float64(value) / (1024 * 1024 * 1024) }
