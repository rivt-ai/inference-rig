package llamacpp

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"inferencerig/backends"
)

func TestFitBySizeUsesGPUThenRAM(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	// GPU present: VRAM is the capacity axis.
	est := FitBySize(8*gib, backends.HostResources{HasGPU: true, VRAMBytes: 24 * gib, AvailableRAMBytes: gib})
	if est.Level != backends.FitFits {
		t.Fatalf("gpu fit = %#v", est)
	}
	// No GPU: falls back to available RAM; a huge model is too large.
	est = FitBySize(40*gib, backends.HostResources{AvailableRAMBytes: 16 * gib})
	if est.Level != backends.FitTooLarge {
		t.Fatalf("ram fit = %#v", est)
	}
}

// tensorlessGGUF builds a GGUF header with valid architecture keys but no
// tensor table. Real models always carry tensors; this stands in for a
// truncated or corrupt file on disk, which the estimator cannot size.
func tensorlessGGUF(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	write := func(v any) {
		if err := binary.Write(&buf, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	writeString := func(s string) {
		write(uint64(len(s)))
		buf.WriteString(s)
	}
	writeUint32KV := func(key string, v uint32) {
		writeString(key)
		write(uint32(4)) // ggufTypeUint32
		write(v)
	}

	write(uint32(0x46554747)) // "GGUF"
	write(uint32(3))          // version
	write(uint64(0))          // tensor count
	write(uint64(3))          // kv count
	writeUint32KV("llama.block_count", 32)
	writeUint32KV("llama.attention.head_count_kv", 8)
	writeUint32KV("llama.embedding_length", 4096)
	return buf.Bytes()
}

// The KV-cache figure itself comes from gguf-parser-go, which is tested
// upstream against real models; what is worth pinning here is that a known
// KV size lands in the estimate and is disclosed in the reason.
func TestFitWithKVIncludesKVCache(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	host := backends.HostResources{AvailableRAMBytes: 64 * gib}

	withKV := FitWithKV(4*gib, host, 2*gib, 32768)
	withoutKV := FitBySize(4*gib, host)

	if !strings.Contains(withKV.Reason, "KV cache") {
		t.Fatalf("reason = %q, want mention of KV cache", withKV.Reason)
	}
	if withKV.RequiredBytes != withoutKV.RequiredBytes+2*gib {
		t.Fatalf("required bytes with KV (%d) should exceed the flat estimate (%d) by the KV size",
			withKV.RequiredBytes, withoutKV.RequiredBytes)
	}
	if !strings.Contains(withoutKV.Reason, "excludes KV cache") {
		t.Fatalf("reason = %q, want it to disclose the missing KV term", withoutKV.Reason)
	}
}

// A model file that cannot be sized must degrade to the flat estimate rather
// than erroring or panicking — the estimator panics on some malformed files.
func TestFitFallsBackForUnsizeableGGUF(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.gguf")
	if err := os.WriteFile(modelPath, tensorlessGGUF(t), 0o644); err != nil {
		t.Fatal(err)
	}
	b := New(Options{ModelStorageDir: dir})

	p := demoProfile("demo", modelPath)
	p.EngineArgs = map[string]any{"ctx-size": 32768}

	const gib = int64(1024 * 1024 * 1024)
	est, err := b.Fit(p, 4*gib, backends.HostResources{AvailableRAMBytes: 64 * gib})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(est.Reason, "excludes KV cache") {
		t.Fatalf("reason = %q, want the flat-estimate disclosure for an unsizeable file", est.Reason)
	}
}

func TestFitFallsBackForModelNotYetDownloaded(t *testing.T) {
	b := New(Options{ModelStorageDir: t.TempDir()})
	p := demoProfile("demo", "/does/not/exist.gguf")
	p.EngineArgs = map[string]any{"ctx-size": 32768}

	const gib = int64(1024 * 1024 * 1024)
	est, err := b.Fit(p, 4*gib, backends.HostResources{AvailableRAMBytes: 64 * gib})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(est.Reason, "excludes KV cache") {
		t.Fatalf("reason = %q, want the flat-estimate disclosure for an unresolvable local file", est.Reason)
	}
}

func TestFitUnknownForUnsizedProfile(t *testing.T) {
	b := New(Options{})
	est, err := b.Fit(demoProfile("demo", "/m.gguf"), 0, backends.HostResources{AvailableRAMBytes: 64 << 30})
	if err != nil {
		t.Fatal(err)
	}
	if est.Level != backends.FitUnknown {
		t.Fatalf("fit = %#v", est)
	}
}
