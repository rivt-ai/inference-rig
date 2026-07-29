package modeldownload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"inferencerig/backends"
)

func TestManagerExecutesSingleAndMultiFilePlans(t *testing.T) {
	body := []byte("artifact")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()

	t.Run("single", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "model.bin")
		plan := backends.ArtifactPlan{
			TargetRoot: target, TotalBytes: int64(len(body)),
			Items: []backends.ArtifactItem{{
				URI: server.URL, Filename: "model.bin", TargetPath: target, SizeBytes: int64(len(body)),
			}},
		}
		assertDownloaded(t, New(Options{HTTPClient: server.Client()}), plan, target, body)
	})

	t.Run("multi", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "snapshot")
		plan := backends.ArtifactPlan{
			MultiFile: true, TargetRoot: root, TotalBytes: 2 * int64(len(body)),
			Items: []backends.ArtifactItem{
				{URI: server.URL, Filename: "config.json", TargetPath: filepath.Join(root, "config.json"), SizeBytes: int64(len(body))},
				{URI: server.URL, Filename: "weights/model.bin", TargetPath: filepath.Join(root, "weights", "model.bin"), SizeBytes: int64(len(body))},
			},
		}
		assertDownloaded(t, New(Options{HTTPClient: server.Client()}), plan, plan.Items[1].TargetPath, body)
		if _, err := os.Stat(root + ".part"); !os.IsNotExist(err) {
			t.Fatalf("staging directory remains: %v", err)
		}
	})
}

func TestManagerAlreadyDownloadedAndDuplicateActive(t *testing.T) {
	target := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(target, []byte("done"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := backends.ArtifactPlan{
		TargetRoot: target,
		Items:      []backends.ArtifactItem{{URI: "https://example.invalid/model", TargetPath: target}},
	}
	manager := New(Options{})
	job, err := manager.Start(context.Background(), Request{Plan: plan})
	if err != nil || job.State != StateAlreadyDownloaded {
		t.Fatalf("job = %#v, err = %v", job, err)
	}

	started := make(chan struct{})
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer blocked.Close()
	activeTarget := filepath.Join(t.TempDir(), "active.bin")
	activePlan := backends.ArtifactPlan{
		TargetRoot: activeTarget,
		Items:      []backends.ArtifactItem{{URI: blocked.URL, TargetPath: activeTarget}},
	}
	manager = New(Options{HTTPClient: blocked.Client()})
	first, err := manager.Start(context.Background(), Request{Plan: activePlan})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	second, err := manager.Start(context.Background(), Request{Plan: activePlan})
	if err != nil || first.ID != second.ID {
		t.Fatalf("first = %#v, second = %#v, err = %v", first, second, err)
	}
	if _, err := manager.Cancel(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	waitJob(t, manager, first.ID)
}

func TestManagerRejectsEscapingMultiFileTarget(t *testing.T) {
	root := t.TempDir()
	plan := backends.ArtifactPlan{
		MultiFile: true, TargetRoot: filepath.Join(root, "snapshot"),
		Items: []backends.ArtifactItem{{
			URI: "https://example.invalid/file", TargetPath: filepath.Join(root, "escape"),
		}},
	}
	if _, err := New(Options{}).Start(context.Background(), Request{Plan: plan}); err == nil {
		t.Fatal("escaping target accepted")
	}
}

func assertDownloaded(t *testing.T, manager *Manager, plan backends.ArtifactPlan, target string, want []byte) {
	t.Helper()
	job, err := manager.Start(context.Background(), Request{Plan: plan})
	if err != nil {
		t.Fatal(err)
	}
	job = waitJob(t, manager, job.ID)
	if job.State != StateCompleted || job.Percent != 100 {
		t.Fatalf("job = %#v", job)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != string(want) {
		t.Fatalf("data = %q, err = %v", data, err)
	}
}

func waitJob(t *testing.T, manager *Manager, id string) Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := manager.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		switch job.State {
		case StateCompleted, StateFailed, StateCancelled:
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("download did not finish")
	return Job{}
}
