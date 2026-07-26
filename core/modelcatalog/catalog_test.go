package modelcatalog_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"inferencerig/backends/llamacpp"
	"inferencerig/backends/mlx"
	"inferencerig/core/modelcatalog"
)

//nolint:gocyclo // One recorded HTTP scenario exercises both real policy adapters and refresh.
func TestClientSearchesBothArtifactPoliciesAndCachesByBackend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/models" {
			_, _ = w.Write([]byte(`[{"id":"owner/repo","downloads":42}]`))
			return
		}
		_, _ = w.Write([]byte(`{
			"id":"owner/repo",
			"siblings":[
				{"rfilename":"config.json","size":10},
				{"rfilename":"weights.safetensors","size":20},
				{"rfilename":"model-q4.gguf","lfs":{"size":30}}
			]
		}`))
	}))
	defer server.Close()

	client := modelcatalog.NewClient(modelcatalog.ClientOptions{
		HTTPClient: server.Client(), BaseURL: server.URL,
		CacheDir: filepath.Join(t.TempDir(), "cache"), CacheTTL: time.Nanosecond,
	})
	ctx := context.Background()
	single, err := client.Search(ctx, modelcatalog.SearchRequest{Backend: "single", Limit: 1},
		llamacpp.New(llamacpp.Options{}).CatalogPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(single.Models) != 1 || len(single.Models[0].Variants) != 1 ||
		single.Models[0].Variants[0].Reference != "model-q4.gguf" {
		t.Fatalf("single-file result = %#v", single)
	}
	multi, err := client.Search(ctx, modelcatalog.SearchRequest{Backend: "multi", Limit: 1},
		mlx.New(mlx.Options{}).CatalogPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(multi.Models) != 1 || !multi.Models[0].Variants[0].MultiFile ||
		multi.Models[0].Variants[0].SizeBytes != 60 {
		t.Fatalf("multi-file result = %#v", multi)
	}
	events, unsubscribe := client.Subscribe()
	defer unsubscribe()
	cached, err := client.Search(ctx, modelcatalog.SearchRequest{Backend: "single", Limit: 1},
		llamacpp.New(llamacpp.Options{}).CatalogPolicy())
	if err != nil || !cached.CacheHit || !cached.Stale {
		t.Fatalf("cached result = %#v, err = %v", cached, err)
	}
	select {
	case event := <-events:
		if event.Backend != "single" || event.Error != "" {
			t.Fatalf("refresh event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for catalog refresh")
	}
}
