//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"inferencerig/backends"
	"inferencerig/backends/llamacpp"
	"inferencerig/core/profiles"
)

// The KV-cache term in a llama.cpp fit estimate comes from gguf-parser-go
// reading a real GGUF header. The package tests cover the plumbing with an
// injected figure and the fallback for files that cannot be sized, but neither
// proves the estimator produces a usable number from an actual model. This does,
// against the same pinned SmolLM2 GGUF the inference tests run.
//
// It asserts the estimate's shape, not an exact byte count: the KV size is
// upstream's to compute, and pinning it here would break on every dependency
// bump for no defect.
func TestFitKVCacheAgainstRealGGUF(t *testing.T) {
	model := requireEnv(t, modelEnv)

	dir := t.TempDir()
	local := filepath.Join(dir, filepath.Base(model))
	if err := os.Link(model, local); err != nil {
		if err := copyFile(model, local); err != nil {
			t.Fatal(err)
		}
	}

	const gib = int64(1024 * 1024 * 1024)
	host := backends.HostResources{AvailableRAMBytes: 64 * gib}
	size := fileSize(t, local)

	b := llamacpp.New(llamacpp.Options{ModelStorageDir: dir})
	fitAt := func(contextLen int) backends.FitEstimate {
		t.Helper()
		p := profiles.Profile{
			Version: "1", Name: "fit", Backend: llamacpp.Name,
			Model:      profiles.ModelSpec{Source: local},
			Listen:     profiles.ListenSpec{Host: "127.0.0.1", Port: 8080},
			EngineArgs: map[string]any{"ctx-size": contextLen},
		}
		est, err := b.Fit(p, size, host)
		if err != nil {
			t.Fatal(err)
		}
		return est
	}

	small, large := fitAt(512), fitAt(8192)

	// A real GGUF must actually yield a KV term, not silently fall back.
	if !strings.Contains(small.Reason, "KV cache") || strings.Contains(small.Reason, "excludes KV cache") {
		t.Fatalf("reason = %q, want a KV-cache term for a real GGUF", small.Reason)
	}

	// The KV cache is per-token, so 16x the context must cost materially more.
	if large.RequiredBytes <= small.RequiredBytes {
		t.Fatalf("required bytes at 8192 context (%d) should exceed 512 context (%d)",
			large.RequiredBytes, small.RequiredBytes)
	}

	// Sanity-check the magnitude rather than the value: a KV cache for a 135M
	// model at 8192 tokens is far smaller than the flat runtime overhead, so an
	// estimate above that bound means the units are wrong, not merely imprecise.
	kv := large.RequiredBytes - size - int64(512*1024*1024)
	if kv <= 0 || kv > gib {
		t.Fatalf("KV term of %d bytes is not plausible for a 135M model at 8192 tokens", kv)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}
