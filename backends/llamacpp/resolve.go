package llamacpp

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"inferencerig/backends"
	"inferencerig/config"
	"inferencerig/core/profiles"
)

// hfHost is the Hugging Face host a repo model source is recognized by.
const hfHost = "huggingface.co"

// Resolve maps the profile's model source (and reference) to a single-file GGUF
// artifact. It recognizes a Hugging Face repo URL (the reference names the .gguf
// file within it), a direct http(s) .gguf URL, a local path, and an opaque URI,
// without any network access — the reference selects the concrete file. GGUF
// models are always single-file, so MultiFile is false.
func (b *Backend) Resolve(_ context.Context, p profiles.Profile) (backends.ResolvedModel, error) {
	if p.Model.Source == "" {
		return backends.ResolvedModel{}, fmt.Errorf("model.source is required")
	}
	name, uri := resolveArtifact(p.Model.Source, p.Model.Reference)
	ref := backends.ArtifactRef{Name: name, URI: uri}
	meta := map[string]string{}
	if quant := InferQuant(name); quant != "" {
		meta["quant"] = quant
	}
	return backends.ResolvedModel{
		Source:    p.Model.Source,
		Reference: p.Model.Reference,
		MultiFile: false,
		Artifacts: []backends.ArtifactRef{ref},
		Metadata:  meta,
	}, nil
}

// resolveArtifact derives the artifact filename and fetch URI from a source and
// optional reference.
func resolveArtifact(source, reference string) (name, uri string) {
	if hf, ok := parseHuggingFaceRepo(source); ok && reference != "" {
		return path.Base(reference), hf + "/resolve/main/" + encodePath(reference)
	}
	if parsed, err := url.Parse(source); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		return path.Base(parsed.Path), source
	}
	if expanded := config.ExpandHome(source); filepath.IsAbs(expanded) {
		return filepath.Base(expanded), expanded
	}
	if reference != "" {
		return path.Base(reference), source
	}
	return path.Base(source), source
}

// Plan turns a resolved single-file model into a neutral download plan whose one
// item targets the model storage directory.
func (b *Backend) Plan(r backends.ResolvedModel) (backends.ArtifactPlan, error) {
	if len(r.Artifacts) == 0 {
		return backends.ArtifactPlan{}, fmt.Errorf("resolved model has no artifacts")
	}
	storage, err := b.modelStorageDir()
	if err != nil {
		return backends.ArtifactPlan{}, err
	}
	plan := backends.ArtifactPlan{MultiFile: r.MultiFile}
	for _, a := range r.Artifacts {
		plan.Items = append(plan.Items, backends.ArtifactItem{
			URI:        a.URI,
			Filename:   a.Name,
			TargetPath: filepath.Join(storage, a.Name),
			SizeBytes:  a.SizeBytes,
		})
		plan.TotalBytes += a.SizeBytes
	}
	if len(plan.Items) == 1 {
		plan.TargetRoot = plan.Items[0].TargetPath
	}
	return plan, nil
}

// parseHuggingFaceRepo reports the canonical repo base URL for a Hugging Face
// model source. Ported from llamarig core/modelcatalog/url.go.
func parseHuggingFaceRepo(rawURL string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host != hfHost {
		return "", false
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 2 {
		return "", false
	}
	owner, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", false
	}
	repo, err := url.PathUnescape(parts[1])
	if err != nil || !safeSegment(owner) || !safeSegment(repo) {
		return "", false
	}
	return "https://" + hfHost + "/" + owner + "/" + repo, true
}

func safeSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	return !strings.ContainsAny(value, `/\`)
}

// encodePath URL-escapes each segment of a slash-separated reference path.
func encodePath(reference string) string {
	segments := strings.Split(reference, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}
