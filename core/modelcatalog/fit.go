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
// when estimating memory required to serve it.
const DefaultOverheadBytes int64 = 512 * 1024 * 1024

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

func gib(value int64) float64 { return float64(value) / (1024 * 1024 * 1024) }
