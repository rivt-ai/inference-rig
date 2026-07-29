package modelcatalog

import (
	"strings"
	"testing"
)

// Ported and neutralized from an upstream catalog fit test: the
// discrete capacity math is engine-agnostic here (the capacity axis is a label).
func TestEstimateFitWithinCapacity(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	v := EstimateFit(8*gib, 10*gib, "VRAM")
	if v.Level != FitFits || !strings.Contains(v.Reason, "VRAM") {
		t.Fatalf("verdict = %#v", v)
	}
}

func TestEstimateFitMarginalAndTooLarge(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	if v := EstimateFit(95*gib/10, 10*gib, "RAM"); v.Level != FitMarginal {
		t.Fatalf("marginal verdict = %#v", v)
	}
	if v := EstimateFit(20*gib, 10*gib, "RAM"); v.Level != FitTooLarge {
		t.Fatalf("too-large verdict = %#v", v)
	}
}

func TestEstimateFitUnknownCapacity(t *testing.T) {
	if v := EstimateFit(0, 10, "RAM"); v.Level != FitUnknown {
		t.Fatalf("verdict = %#v", v)
	}
	if v := EstimateFit(10, 0, "RAM"); v.Level != FitUnknown {
		t.Fatalf("verdict = %#v", v)
	}
}
