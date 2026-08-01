package modelcatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type markerPolicy struct{}

func (markerPolicy) InspectDirectory(path string) (int64, time.Time, bool, error) {
	info, err := os.Stat(filepath.Join(path, "complete"))
	if os.IsNotExist(err) {
		return 0, time.Time{}, false, nil
	}
	if err != nil {
		return 0, time.Time{}, false, err
	}
	return info.Size(), info.ModTime(), true, nil
}

func TestSnapshotScannerListsCompleteDirectories(t *testing.T) {
	root := t.TempDir()
	complete := filepath.Join(root, "owner", "model")
	if err := os.MkdirAll(complete, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(complete, "complete"), "ok")
	partial := filepath.Join(root, "other.part")
	if err := os.MkdirAll(partial, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(partial, "complete"), "ignored")

	models, err := NewSnapshotScanner(root, markerPolicy{}).ListLocal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Path != complete {
		t.Fatalf("models = %#v", models)
	}
}
