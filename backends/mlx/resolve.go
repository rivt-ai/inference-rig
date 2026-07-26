package mlx

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"inferencerig/backends"
	"inferencerig/core/modelcatalog"
	"inferencerig/core/profiles"
)

type hfSibling struct {
	Name string `json:"rfilename"`
	Size int64  `json:"size"`
	LFS  *struct {
		Size int64 `json:"size"`
	} `json:"lfs"`
}

// Resolve expands a Hugging Face repository into its complete snapshot. Other
// sources remain a one-item multi-file plan so custom artifact transports can
// still flow through the neutral downloader.
func (b *Backend) Resolve(ctx context.Context, p profiles.Profile) (backends.ResolvedModel, error) {
	if p.Model.Source == "" {
		return backends.ResolvedModel{}, errMissingModel
	}
	owner, repo, ok := parseRepository(p.Model.Source)
	if !ok {
		name := p.Model.Reference
		if name == "" {
			name = path.Base(p.Model.Source)
		}
		return backends.ResolvedModel{
			Source: p.Model.Source, Reference: p.Model.Reference, MultiFile: true,
			Artifacts: []backends.ArtifactRef{{Name: name, URI: p.Model.Source}},
			Metadata:  map[string]string{"target_dir": safeTargetName(name)},
		}, nil
	}
	siblings, err := b.fetchSiblings(ctx, owner, repo)
	if err != nil {
		return backends.ResolvedModel{}, err
	}
	artifacts, err := b.snapshotArtifacts(owner, repo, siblings)
	if err != nil {
		return backends.ResolvedModel{}, err
	}
	return backends.ResolvedModel{
		Source: p.Model.Source, Reference: p.Model.Reference, MultiFile: true, Artifacts: artifacts,
		Metadata: map[string]string{"target_dir": filepath.Join(owner, repo)},
	}, nil
}

func (b *Backend) snapshotArtifacts(owner, repo string, siblings []hfSibling) ([]backends.ArtifactRef, error) {
	artifacts := make([]backends.ArtifactRef, 0, len(siblings))
	hasConfig, hasWeights := false, false
	for _, sibling := range siblings {
		if !safeRelativePath(sibling.Name) {
			return nil, fmt.Errorf("unsafe snapshot path %q", sibling.Name)
		}
		size := sibling.Size
		if sibling.LFS != nil && sibling.LFS.Size > 0 {
			size = sibling.LFS.Size
		}
		name := strings.TrimPrefix(sibling.Name, "/")
		artifacts = append(artifacts, backends.ArtifactRef{
			Name:      name,
			URI:       strings.TrimRight(b.opts.HuggingFaceURL, "/") + "/" + owner + "/" + repo + "/resolve/main/" + encodeSegments(name),
			SizeBytes: size,
		})
		hasConfig = hasConfig || name == "config.json"
		hasWeights = hasWeights || strings.EqualFold(filepath.Ext(name), ".safetensors")
	}
	if !hasConfig || !hasWeights {
		return nil, fmt.Errorf("repository is not a complete MLX snapshot")
	}
	return artifacts, nil
}

func (b *Backend) fetchSiblings(ctx context.Context, owner, repo string) ([]hfSibling, error) {
	endpoint := strings.TrimRight(b.opts.HuggingFaceURL, "/") + "/api/models/" +
		url.PathEscape(owner) + "/" + url.PathEscape(repo) + "?blobs=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.opts.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("resolve MLX snapshot: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("resolve MLX snapshot: %s", resp.Status)
	}
	var raw struct {
		Siblings []hfSibling `json:"siblings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	return raw.Siblings, nil
}

// Plan creates a multi-file executor plan rooted at one snapshot directory.
func (b *Backend) Plan(r backends.ResolvedModel) (backends.ArtifactPlan, error) {
	if len(r.Artifacts) == 0 {
		return backends.ArtifactPlan{}, fmt.Errorf("resolved model has no artifacts")
	}
	root, err := b.storageDir()
	if err != nil {
		return backends.ArtifactPlan{}, err
	}
	targetDir := r.Metadata["target_dir"]
	if targetDir == "" {
		targetDir = safeTargetName(r.Reference)
	}
	if !safeRelativePath(targetDir) {
		return backends.ArtifactPlan{}, fmt.Errorf("unsafe snapshot target %q", targetDir)
	}
	plan := backends.ArtifactPlan{MultiFile: true, TargetRoot: filepath.Join(root, targetDir)}
	for _, artifact := range r.Artifacts {
		if !safeRelativePath(artifact.Name) {
			return backends.ArtifactPlan{}, fmt.Errorf("unsafe artifact path %q", artifact.Name)
		}
		plan.Items = append(plan.Items, backends.ArtifactItem{
			URI: artifact.URI, Filename: artifact.Name,
			TargetPath: filepath.Join(root, targetDir, filepath.FromSlash(artifact.Name)),
			SizeBytes:  artifact.SizeBytes,
		})
		plan.TotalBytes += artifact.SizeBytes
	}
	return plan, nil
}

func parseRepository(raw string) (string, string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host != "huggingface.co" {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || !safeSegment(parts[0]) || !safeSegment(parts[1]) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func safeSegment(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, `/\`)
}

func safeRelativePath(value string) bool {
	value = filepath.Clean(filepath.FromSlash(value))
	return value != "." && value != "" && !filepath.IsAbs(value) &&
		value != ".." && !strings.HasPrefix(value, ".."+string(filepath.Separator))
}

func safeTargetName(value string) string {
	value = strings.TrimSpace(value)
	if safeRelativePath(value) {
		return value
	}
	return "model"
}

func encodeSegments(value string) string {
	parts := strings.Split(filepath.ToSlash(value), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

type snapshotPolicy struct{}

func (snapshotPolicy) InspectDirectory(dir string) (int64, time.Time, bool, error) {
	if info, err := os.Stat(filepath.Join(dir, "config.json")); err != nil || !info.Mode().IsRegular() {
		return 0, time.Time{}, false, nil
	}
	stats := snapshotStats{}
	err := filepath.WalkDir(dir, stats.collect)
	return stats.size, stats.modified, stats.hasWeights, err
}

type snapshotStats struct {
	size       int64
	modified   time.Time
	hasWeights bool
}

func (s *snapshotStats) collect(_ string, entry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return walkErr
	}
	if entry.Type()&fs.ModeSymlink != 0 {
		if entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}
	if entry.IsDir() {
		return nil
	}
	info, err := entry.Info()
	if err != nil {
		return err
	}
	s.size += info.Size()
	if info.ModTime().After(s.modified) {
		s.modified = info.ModTime()
	}
	s.hasWeights = s.hasWeights || strings.EqualFold(filepath.Ext(entry.Name()), ".safetensors")
	return nil
}

// ListLocal returns complete local MLX snapshots.
func (b *Backend) ListLocal(ctx context.Context) ([]modelcatalog.LocalModel, error) {
	root, err := b.storageDir()
	if err != nil {
		return nil, err
	}
	return modelcatalog.NewSnapshotScanner(root, snapshotPolicy{}).ListLocal(ctx)
}
