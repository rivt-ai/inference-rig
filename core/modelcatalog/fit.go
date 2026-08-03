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

// RequiredBytes estimates the memory required to serve a model of sizeBytes on
// disk, given a KV-cache size for the context it will be served at. kvBytes is
// zero when the KV cache could not be sized — the model is not downloaded yet,
// no context length is configured, or the file is not a readable GGUF — and the
// estimate then falls back to sizeBytes plus DefaultOverheadBytes and says the
// KV cache is excluded. Sizing the KV cache is the caller's job, since it is
// engine-specific; see the llamacpp backend's ggufKVCache.
func RequiredBytes(sizeBytes, kvBytes int64, contextLen int) (bytes int64, note string) {
	if sizeBytes <= 0 {
		return 0, ""
	}
	if kvBytes <= 0 {
		return sizeBytes + DefaultOverheadBytes, "context not known; excludes KV cache"
	}
	return sizeBytes + DefaultOverheadBytes + kvBytes, fmt.Sprintf("includes %.1f GiB KV cache at %d tokens context", gib(kvBytes), contextLen)
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
