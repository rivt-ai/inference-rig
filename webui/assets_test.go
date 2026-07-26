package webui

import (
	"io/fs"
	"strings"
	"testing"
)

func TestBrowserUsesPublicCanonicalFacade(t *testing.T) {
	data, err := fs.ReadFile(Files, "dist/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/api/backends", "/api/profiles"} {
		if !strings.Contains(string(data), endpoint) {
			t.Fatalf("browser app does not call %s", endpoint)
		}
	}
}
