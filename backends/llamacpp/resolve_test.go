package llamacpp

import (
	"context"
	"path/filepath"
	"testing"

	"inferencerig/backends"
)

func TestResolveHuggingFaceReference(t *testing.T) {
	b := New(Options{ModelStorageDir: t.TempDir()})
	p := demoProfile("demo", "https://huggingface.co/owner/repo")
	p.Model.Reference = "sub/model.Q4_K_M.gguf"
	r, err := b.Resolve(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if r.MultiFile || len(r.Artifacts) != 1 {
		t.Fatalf("resolved = %#v", r)
	}
	a := r.Artifacts[0]
	if a.Name != "model.Q4_K_M.gguf" || a.URI != "https://huggingface.co/owner/repo/resolve/main/sub/model.Q4_K_M.gguf" {
		t.Fatalf("artifact = %#v", a)
	}
	if r.Metadata["quant"] != "Q4_K_M" {
		t.Fatalf("metadata = %#v", r.Metadata)
	}
}

func TestResolveDirectURLAndPlanSingleFile(t *testing.T) {
	storage := t.TempDir()
	b := New(Options{ModelStorageDir: storage})
	p := demoProfile("demo", "https://example.test/models/tiny.gguf")
	r, err := b.Resolve(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := b.Plan(r)
	if err != nil {
		t.Fatal(err)
	}
	if plan.MultiFile || len(plan.Items) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
	item := plan.Items[0]
	if item.Filename != "tiny.gguf" || item.TargetPath != filepath.Join(storage, "tiny.gguf") {
		t.Fatalf("item = %#v", item)
	}
}

func TestResolveRejectsEmptySource(t *testing.T) {
	b := New(Options{ModelStorageDir: t.TempDir()})
	if _, err := b.Resolve(context.Background(), demoProfile("demo", "")); err == nil {
		t.Fatal("Resolve accepted an empty source")
	}
}

func TestPlanRejectsNoArtifacts(t *testing.T) {
	b := New(Options{ModelStorageDir: t.TempDir()})
	if _, err := b.Plan(backends.ResolvedModel{}); err == nil {
		t.Fatal("Plan accepted a model with no artifacts")
	}
}
