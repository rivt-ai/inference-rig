package modelcatalog

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DirectoryPolicy decides whether a directory is a complete model snapshot.
// The shared scanner owns traversal and safety; backends own format knowledge.
type DirectoryPolicy interface {
	InspectDirectory(path string) (size int64, modified time.Time, ok bool, err error)
}

// SnapshotScanner lists complete model directories beneath a storage root.
type SnapshotScanner struct {
	root   string
	policy DirectoryPolicy
}

// NewSnapshotScanner builds a directory-model scanner.
func NewSnapshotScanner(root string, policy DirectoryPolicy) *SnapshotScanner {
	return &SnapshotScanner{root: filepath.Clean(root), policy: policy}
}

// ListLocal returns complete snapshots sorted by path. Symlinks and staging
// directories ending in .part are ignored.
func (s *SnapshotScanner) ListLocal(ctx context.Context) ([]LocalModel, error) {
	models := []LocalModel{}
	err := filepath.WalkDir(s.root, func(path string, entry fs.DirEntry, walkErr error) error {
		return s.collectSnapshot(ctx, path, entry, walkErr, &models)
	})
	sort.Slice(models, func(i, j int) bool { return models[i].Path < models[j].Path })
	return models, err
}

func (s *SnapshotScanner) collectSnapshot(ctx context.Context, path string, entry fs.DirEntry, walkErr error, models *[]LocalModel) error {
	if walkErr != nil {
		if path == s.root && errors.Is(walkErr, os.ErrNotExist) {
			return nil
		}
		return walkErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if entry.Type()&fs.ModeSymlink != 0 {
		if entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}
	if !entry.IsDir() || path == s.root {
		return nil
	}
	if strings.HasSuffix(entry.Name(), ".part") {
		return filepath.SkipDir
	}
	size, modified, ok, err := s.policy.InspectDirectory(path)
	if err != nil || !ok {
		return err
	}
	*models = append(*models, LocalModel{
		Path: path, Filename: entry.Name(), SizeBytes: size, ModifiedAt: modified.UTC(),
	})
	return filepath.SkipDir
}
