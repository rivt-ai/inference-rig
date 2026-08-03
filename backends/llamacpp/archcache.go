package llamacpp

import (
	"os"
	"sync"

	"inferencerig/core/modelcatalog"
)

// ggufArchCache memoizes GGUF architecture reads by path, keyed on the file's
// mtime so a re-downloaded or replaced model gets re-parsed. Fit is called
// once per catalog variant per request; without this, listing the catalog
// would re-parse every local GGUF header on every call.
type ggufArchCache struct {
	mu      sync.Mutex
	entries map[string]archCacheEntry
}

type archCacheEntry struct {
	modTime int64
	arch    *modelcatalog.Arch // nil means "parsed, not a usable GGUF"
}

// get reads and caches the GGUF architecture at path, or nil if the file is
// missing or is not a well-formed GGUF header.
func (c *ggufArchCache) get(path string) *modelcatalog.Arch {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	mtime := info.ModTime().UnixNano()

	c.mu.Lock()
	if entry, ok := c.entries[path]; ok && entry.modTime == mtime {
		c.mu.Unlock()
		return entry.arch
	}
	c.mu.Unlock()

	var arch *modelcatalog.Arch
	if a, err := modelcatalog.ReadArch(path); err == nil {
		arch = &a
	}

	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[string]archCacheEntry)
	}
	c.entries[path] = archCacheEntry{modTime: mtime, arch: arch}
	c.mu.Unlock()

	return arch
}
