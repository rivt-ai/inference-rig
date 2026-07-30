//go:build e2e || e2ebrowser

package e2e

import (
	"path/filepath"
	"testing"
)

// newLlamacppRig builds a rig wired to the pinned llama.cpp fixtures. The
// engine binary is reached through PATH and the GGUF is copied into the rig's
// own model storage, so nothing is shared between tests.
func newLlamacppRig(t *testing.T) *rig {
	t.Helper()
	engine := requireEnv(t, engineBinEnv)
	model := requireEnv(t, modelEnv)
	r := newRig(t, filepath.Dir(engine))
	r.installModel(model)
	return r
}
