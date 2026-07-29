package mlx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAndPlanSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/models/owner/repo" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"siblings":[
			{"rfilename":"config.json","size":2},
			{"rfilename":"weights/model.safetensors","lfs":{"size":9}}
		]}`))
	}))
	defer server.Close()
	storage := t.TempDir()
	b := New(Options{ModelStorageDir: storage, HuggingFaceURL: server.URL, HTTPClient: server.Client()})
	resolved, err := b.Resolve(context.Background(), testProfile("demo", "https://huggingface.co/owner/repo"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := b.Plan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.MultiFile || len(plan.Items) != 2 || plan.TotalBytes != 11 {
		t.Fatalf("plan = %#v", plan)
	}
	want := filepath.Join(storage, "owner", "repo", "weights", "model.safetensors")
	if plan.Items[1].TargetPath != want {
		t.Fatalf("target = %q, want %q", plan.Items[1].TargetPath, want)
	}
}

func TestResolveRejectsIncompleteAndUnsafeSnapshot(t *testing.T) {
	for _, body := range []string{
		`{"siblings":[{"rfilename":"config.json"}]}`,
		`{"siblings":[{"rfilename":"config.json"},{"rfilename":"../model.safetensors"}]}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		b := New(Options{HuggingFaceURL: server.URL, HTTPClient: server.Client()})
		_, err := b.Resolve(context.Background(), testProfile("demo", "https://huggingface.co/owner/repo"))
		server.Close()
		if err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestListLocalSnapshots(t *testing.T) {
	root := t.TempDir()
	model := filepath.Join(root, "owner", "repo")
	if err := os.MkdirAll(model, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(model, "config.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(model, "model.safetensors"), []byte("weights"), 0o600); err != nil {
		t.Fatal(err)
	}
	models, err := New(Options{ModelStorageDir: root}).ListLocal(context.Background())
	if err != nil || len(models) != 1 || models[0].Path != model {
		t.Fatalf("models = %#v, err = %v", models, err)
	}
}
