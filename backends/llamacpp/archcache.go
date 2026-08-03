package llamacpp

import (
	"os"
	"sync"

	gguf "github.com/gpustack/gguf-parser-go"
)

// ggufKVCache memoizes KV-cache size estimates by path and context length,
// keyed on the file's mtime so a re-downloaded or replaced model gets
// re-estimated. Fit is called once per catalog variant per request; without
// this, listing the catalog would re-parse every local GGUF header on every
// call.
type ggufKVCache struct {
	mu      sync.Mutex
	entries map[kvCacheKey]kvCacheEntry
}

type kvCacheKey struct {
	path       string
	contextLen int
}

type kvCacheEntry struct {
	modTime int64
	kvBytes int64 // 0 means "estimated, no usable figure"
}

// get estimates and caches the KV-cache bytes for the model at path served at
// contextLen tokens. It returns 0 when the file is missing, is not a readable
// GGUF, or no context length is configured — all routine (a catalog listing
// names models that are not downloaded yet), not errors.
func (c *ggufKVCache) get(path string, contextLen int) int64 {
	if path == "" || contextLen <= 0 {
		return 0
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	key := kvCacheKey{path: path, contextLen: contextLen}
	mtime := info.ModTime().UnixNano()

	c.mu.Lock()
	if entry, ok := c.entries[key]; ok && entry.modTime == mtime {
		c.mu.Unlock()
		return entry.kvBytes
	}
	c.mu.Unlock()

	kvBytes := estimateKVBytes(path, contextLen)

	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[kvCacheKey]kvCacheEntry)
	}
	c.entries[key] = kvCacheEntry{modTime: mtime, kvBytes: kvBytes}
	c.mu.Unlock()

	return kvBytes
}

// estimateKVBytes reads the GGUF header at path and asks gguf-parser-go for
// llama.cpp's KV-cache size at contextLen tokens. The per-device split depends
// on offload decisions this estimate does not model, so the figures are summed
// back into one total: a fit verdict cares about the whole cache, wherever it
// lands. Returns 0 when the file cannot be parsed as GGUF.
//
// The estimator indexes into tensor metadata that a well-formed model always
// has but a truncated or hand-built file may not, and panics rather than
// erroring when it is missing. A corrupt file on disk is a routine condition
// here, so the panic is recovered into "no estimate" rather than being allowed
// to take down the daemon.
func estimateKVBytes(path string, contextLen int) (kvBytes int64) {
	defer func() {
		if recover() != nil {
			kvBytes = 0
		}
	}()
	f, err := gguf.ParseGGUFFile(path)
	if err != nil {
		return 0
	}
	estimate := f.EstimateLLaMACppRun(gguf.WithLLaMACppContextSize(int32(contextLen)))
	for _, device := range estimate.Devices {
		kvBytes += int64(device.KVCache.Sum())
	}
	return kvBytes
}
