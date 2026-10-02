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

// A cache entry written by older query logic outlives an upgrade, and a fresh
// entry is never refreshed on its own, so a client needs a way to bypass it.
// Refresh must also store over the entry a normal read loads: if it filled a
// parallel key the next ordinary read would serve the stale models again.
func TestSearchRefreshBypassesAndReplacesTheCachedEntry(t *testing.T) {
	var listings int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/models" {
			listings++
			if listings == 1 {
				_, _ = w.Write([]byte(`[{"id":"owner/before","downloads":1}]`))
				return
			}
			_, _ = w.Write([]byte(`[{"id":"owner/after","downloads":2}]`))
			return
		}
		_, _ = w.Write([]byte(`{"siblings":[{"rfilename":"model-q4.gguf","lfs":{"size":30}}]}`))
	}))
	defer server.Close()

	// An hour-long TTL keeps the entry fresh, so nothing but Refresh can
	// dislodge it — a stale entry would refresh on its own and prove nothing.
	client := modelcatalog.NewClient(modelcatalog.ClientOptions{
		HTTPClient: server.Client(), BaseURL: server.URL,
		CacheDir: filepath.Join(t.TempDir(), "cache"), CacheTTL: time.Hour,
	})
	ctx := context.Background()
	policy := llamacpp.New(llamacpp.Options{}).CatalogPolicy()
	request := modelcatalog.SearchRequest{Backend: "single", Limit: 1}

	first, err := client.Search(ctx, request, policy)
	if err != nil || len(first.Models) != 1 || first.Models[0].ID != "owner/before" {
		t.Fatalf("first result = %#v, err = %v", first, err)
	}
	cached, err := client.Search(ctx, request, policy)
	if err != nil || !cached.CacheHit || cached.Models[0].ID != "owner/before" {
		t.Fatalf("cached result = %#v, err = %v", cached, err)
	}
	if listings != 1 {
		t.Fatalf("cached read refetched: listings = %d, want 1", listings)
	}

	refresh := request
	refresh.Refresh = true
	refreshed, err := client.Search(ctx, refresh, policy)
	if err != nil || refreshed.CacheHit || refreshed.Models[0].ID != "owner/after" {
		t.Fatalf("refreshed result = %#v, err = %v", refreshed, err)
	}

	// The decisive assertion: an ordinary read now serves the refreshed models
	// from cache, so Refresh replaced the entry instead of adding a new one.
	after, err := client.Search(ctx, request, policy)
	if err != nil || !after.CacheHit || after.Models[0].ID != "owner/after" {
		t.Fatalf("post-refresh cached result = %#v, err = %v", after, err)
	}
	if listings != 2 {
		t.Fatalf("listings = %d, want 2", listings)
	}
}
