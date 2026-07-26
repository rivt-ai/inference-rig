package modeldownload

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"inferencerig/backends"
	"inferencerig/platform/filedoc"
)

func validatePlan(plan backends.ArtifactPlan) error {
	if plan.TargetRoot == "" || len(plan.Items) == 0 {
		return fmt.Errorf("%w: target root and items are required", ErrInvalidInput)
	}
	if !plan.MultiFile && len(plan.Items) != 1 {
		return fmt.Errorf("%w: a single-file plan must contain one item", ErrInvalidInput)
	}
	seen := map[string]struct{}{}
	for _, item := range plan.Items {
		if err := validateItem(plan, item, seen); err != nil {
			return err
		}
	}
	return nil
}

func validateItem(plan backends.ArtifactPlan, item backends.ArtifactItem, seen map[string]struct{}) error {
	if item.URI == "" || item.TargetPath == "" {
		return fmt.Errorf("%w: item URI and target path are required", ErrInvalidInput)
	}
	if _, duplicate := seen[item.TargetPath]; duplicate {
		return fmt.Errorf("%w: duplicate target %q", ErrInvalidInput, item.TargetPath)
	}
	seen[item.TargetPath] = struct{}{}
	if !plan.MultiFile && item.TargetPath != plan.TargetRoot {
		return fmt.Errorf("%w: single-file target differs from root", ErrInvalidInput)
	}
	if !plan.MultiFile {
		return nil
	}
	relative, err := filepath.Rel(plan.TargetRoot, item.TargetPath)
	if err != nil || !safeRelative(relative) {
		return fmt.Errorf("%w: target %q escapes root", ErrInvalidInput, item.TargetPath)
	}
	return nil
}

func prepareParent(target, stage string) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := rejectSymlinkAncestors(dir); err != nil {
		return err
	}
	for _, path := range []string{target, stage} {
		if err := filedoc.RejectSymlink(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
	}
	_ = os.Remove(stage)
	return nil
}

func prepareDirectory(target, stage string, force bool) error {
	if err := prepareParent(target, stage); err != nil {
		return err
	}
	if exists(target) && !force {
		return fmt.Errorf("%w: target already exists", ErrConflict)
	}
	if err := os.RemoveAll(stage); err != nil {
		return err
	}
	return os.Mkdir(stage, 0o700)
}

func rejectSymlinkAncestors(path string) error {
	if err := filedoc.RejectSymlinkAncestors(path); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return nil
}

func safeRelative(value string) bool {
	return value != "" && value != "." && value != ".." && !filepath.IsAbs(value) &&
		!strings.HasPrefix(value, ".."+string(filepath.Separator))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
