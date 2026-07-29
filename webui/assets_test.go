package webui

import (
	"io/fs"
	"testing"
)

// TestEmbeddedBundlePresent checks the embedded tree only when a real build is
// present. dist/ is a build artifact that is not committed, so `go test ./...`
// on a fresh checkout sees just dist/.gitkeep and has nothing to assert about.
func TestEmbeddedBundlePresent(t *testing.T) {
	if _, err := fs.Stat(Files, "dist/index.html"); err != nil {
		t.Skip("no web build present; run `make webui` to produce webui/dist")
	}
	entries, err := fs.ReadDir(Files, "dist/assets")
	if err != nil {
		t.Fatalf("built bundle has no assets directory: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("built bundle has an empty assets directory")
	}
}
