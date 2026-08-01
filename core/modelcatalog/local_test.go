package modelcatalog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// extPolicy is a test FormatPolicy that accepts a single file extension.
type extPolicy struct {
	ext   string
	multi bool
}

func (p extPolicy) IsModelFile(name string) bool {
	return strings.EqualFold(filepath.Ext(name), p.ext)
}
func (p extPolicy) MultiFile() bool { return p.multi }

func TestScannerListsPolicyFilesRecursively(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "owner", "repo")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(nested, "model.bin"), "x")
	writeFile(t, filepath.Join(nested, "notes.txt"), "ignore me")
	writeFile(t, filepath.Join(root, "top.bin"), "yy")

	models, err := NewScanner(root, extPolicy{ext: ".bin"}).ListLocal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %#v", models)
	}
	// Sorted by path: the nested one sorts before the top-level one.
	if filepath.Base(models[0].Path) != "model.bin" || models[1].Filename != "top.bin" {
		t.Fatalf("models = %#v", models)
	}
}

func TestScannerMissingRootIsEmpty(t *testing.T) {
	models, err := NewScanner(filepath.Join(t.TempDir(), "absent"), extPolicy{ext: ".bin"}).ListLocal(context.Background())
	if err != nil || len(models) != 0 {
		t.Fatalf("models = %#v, err = %v", models, err)
	}
}

func TestPathContainment(t *testing.T) {
	root := CanonicalPath(t.TempDir())
	inside := filepath.Join(root, "a", "b")
	if !PathContains(root, inside) {
		t.Fatalf("expected %q within %q", inside, root)
	}
	if PathContains(root, filepath.Dir(root)) {
		t.Fatalf("parent of root reported as contained")
	}
}

func TestRemoveLocalRejectsEscapeAndDeletesExpectedShape(t *testing.T) {
	root := t.TempDir()
	model := filepath.Join(root, "model.bin")
	writeFile(t, model, "model")
	if err := RemoveLocal(root, filepath.Join(root, "..", "escape.bin"), false); err == nil {
		t.Fatal("expected path escape rejection")
	}
	if err := RemoveLocal(root, model, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(model); !os.IsNotExist(err) {
		t.Fatalf("model remains: %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
