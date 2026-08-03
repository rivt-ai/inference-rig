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

func TestRequiredBytesWithoutArch(t *testing.T) {
	const sizeBytes = 10 * 1024 * 1024 * 1024
	got, note := RequiredBytes(sizeBytes, nil, 4096)
	if want := int64(sizeBytes) + DefaultOverheadBytes; got != want {
		t.Fatalf("RequiredBytes() = %d, want %d (today's flat-overhead behavior)", got, want)
	}
	if !strings.Contains(note, "excludes KV cache") {
		t.Fatalf("note = %q, want mention of excluded KV cache", note)
	}

	got2, _ := RequiredBytes(sizeBytes, nil, 0)
	if got2 != got {
		t.Fatalf("nil arch and zero context should both hit the flat-overhead fallback")
	}
}

func TestRequiredBytesWithArchScalesWithContext(t *testing.T) {
	const sizeBytes = 10 * 1024 * 1024 * 1024
	arch := &Arch{BlockCount: 32, HeadCountKV: 8, EmbeddingLength: 4096}

	small, note := RequiredBytes(sizeBytes, arch, 4096)
	large, _ := RequiredBytes(sizeBytes, arch, 32768)

	if !strings.Contains(note, "KV cache") {
		t.Fatalf("note = %q, want mention of KV cache", note)
	}
	if large <= small {
		t.Fatalf("required bytes at 32768 context (%d) should exceed 4096 context (%d)", large, small)
	}
	// KV term must be a material fraction of the difference, not lost in rounding.
	if diff := large - small; diff < sizeBytes/100 {
		t.Fatalf("context scaling produced a suspiciously small difference: %d bytes", diff)
	}
}

func TestKVCacheBytesUsesKeyValueLengthWhenPresent(t *testing.T) {
	withExplicitDims := KVCacheBytes(Arch{BlockCount: 32, HeadCountKV: 8, KeyLength: 128, ValueLength: 128}, 4096, 2)
	withDerivedDims := KVCacheBytes(Arch{BlockCount: 32, HeadCountKV: 8, EmbeddingLength: 1024}, 4096, 2)
	if withExplicitDims != withDerivedDims {
		t.Fatalf("explicit key/value length (%d) should match embedding_length/head_count_kv derivation (%d) for equivalent dims", withExplicitDims, withDerivedDims)
	}
	if KVCacheBytes(Arch{}, 4096, 2) != 0 {
		t.Fatal("zero-value Arch should yield zero KV bytes, not a bogus estimate")
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
