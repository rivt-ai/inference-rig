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

// LocalModel is one model file discovered under a storage root.
type LocalModel struct {
	Path       string    `json:"path"`
	Filename   string    `json:"filename"`
	SizeBytes  int64     `json:"size_bytes"`
	ModifiedAt time.Time `json:"modified_at"`
}

// Scanner lists a backend's model files under a storage root, filtered by the
// backend's FormatPolicy. The traversal, symlink rejection, path canonicalize
// and containment logic are shared; only the file-membership test is policy.
type Scanner struct {
	root   string
	policy FormatPolicy
}

// NewScanner builds a scanner rooted at root using policy to decide membership.
func NewScanner(root string, policy FormatPolicy) *Scanner {
	return &Scanner{root: filepath.Clean(root), policy: policy}
}

// ListLocal walks the storage root and returns the model files the policy
// accepts, sorted by path. A missing root is treated as an empty inventory.
func (s *Scanner) ListLocal(ctx context.Context) ([]LocalModel, error) {
	models := make([]LocalModel, 0)
	err := filepath.WalkDir(s.root, func(path string, entry fs.DirEntry, walkErr error) error {
		return s.collect(ctx, path, entry, walkErr, &models)
	})
	sort.Slice(models, func(i, j int) bool { return models[i].Path < models[j].Path })
	return models, err
}

func (s *Scanner) collect(ctx context.Context, path string, entry fs.DirEntry, walkErr error, models *[]LocalModel) error {
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
		return nil
	}
	if entry.IsDir() || !s.policy.IsModelFile(entry.Name()) {
		return nil
	}
	info, err := entry.Info()
	if err != nil {
		return err
	}
	if info.Mode().IsRegular() {
		*models = append(*models, LocalModel{
			Path:       filepath.Clean(path),
			Filename:   entry.Name(),
			SizeBytes:  info.Size(),
			ModifiedAt: info.ModTime().UTC(),
		})
	}
	return nil
}

// CanonicalPath resolves a path to an absolute, symlink-free, cleaned form for
// safe containment checks against a storage root.
func CanonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

// PathContains reports whether path lies within dir (both should be canonical).
func PathContains(dir string, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
