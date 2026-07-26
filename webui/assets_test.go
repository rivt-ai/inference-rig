package webui

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
)

func TestBrowserUsesPublicCanonicalFacade(t *testing.T) {
	var bundle bytes.Buffer
	err := fs.WalkDir(Files, "dist", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(Files, path)
		bundle.Write(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/api/backends", "/api/profiles", "/api/catalog", "/api/events"} {
		if !strings.Contains(bundle.String(), endpoint) {
			t.Fatalf("browser app does not call %s", endpoint)
		}
	}
}
