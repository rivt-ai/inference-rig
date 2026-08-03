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

func TestRequiredBytesWithoutKVFallsBackToFlatOverhead(t *testing.T) {
	const sizeBytes = 10 * 1024 * 1024 * 1024
	got, note := RequiredBytes(sizeBytes, 0, 4096)
	if want := int64(sizeBytes) + DefaultOverheadBytes; got != want {
		t.Fatalf("RequiredBytes() = %d, want %d (today's flat-overhead behavior)", got, want)
	}
	if !strings.Contains(note, "excludes KV cache") {
		t.Fatalf("note = %q, want mention of excluded KV cache", note)
	}
}

func TestRequiredBytesAddsKVCache(t *testing.T) {
	const sizeBytes = 10 * 1024 * 1024 * 1024
	const kvBytes = 2 * 1024 * 1024 * 1024

	got, note := RequiredBytes(sizeBytes, kvBytes, 4096)
	if want := int64(sizeBytes) + DefaultOverheadBytes + kvBytes; got != want {
		t.Fatalf("RequiredBytes() = %d, want %d", got, want)
	}
	if !strings.Contains(note, "KV cache") {
		t.Fatalf("note = %q, want mention of KV cache", note)
	}
}

func TestEstimateFitDetailedAppendsNoteExceptWhenUnknown(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	v := EstimateFitDetailed(8*gib, 10*gib, "VRAM", "2 devices: 6.0 + 4.0 GiB")
	if !strings.Contains(v.Reason, "2 devices") {
		t.Fatalf("reason = %q, want device breakdown appended", v.Reason)
	}

	unknown := EstimateFitDetailed(0, 10*gib, "VRAM", "should not appear")
	if strings.Contains(unknown.Reason, "should not appear") {
		t.Fatalf("reason = %q, note must be suppressed for an unknown verdict", unknown.Reason)
	}
}
