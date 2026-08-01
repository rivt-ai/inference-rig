package modeldownload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"inferencerig/backends"
)

// rangeServer serves body, honouring a byte-range request only when ranges is
// true — the two halves of the resume-or-restart decision.
func rangeServer(t *testing.T, body []byte, ranges bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := 0
		if prefix := r.Header.Get("Range"); ranges && strings.HasPrefix(prefix, "bytes=") {
			offset, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(prefix, "bytes="), "-"))
			if err != nil || offset > len(body) {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			start = offset
			w.Header().Set("Content-Range", "bytes "+strconv.Itoa(start)+"-"+
				strconv.Itoa(len(body)-1)+"/"+strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = w.Write(body[start:])
	}))
	t.Cleanup(server.Close)
	return server
}

func filePlan(target, uri string, size int64, digest string) backends.ArtifactPlan {
	return backends.ArtifactPlan{
		TargetRoot: target, TotalBytes: size,
		Items: []backends.ArtifactItem{{
			URI: uri, Filename: filepath.Base(target), TargetPath: target, SizeBytes: size, SHA256: digest,
		}},
	}
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestResumeContinuesFromPartialFile(t *testing.T) {
	body := []byte("artifact-payload")
	server := rangeServer(t, body, true)
	target := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(target+".part", body[:6], 0o600); err != nil {
		t.Fatal(err)
	}
	plan := filePlan(target, server.URL, int64(len(body)), digestOf(body))

	manager := New(Options{HTTPClient: server.Client()})
	job, err := manager.Start(context.Background(), Request{Plan: plan})
	if err != nil {
		t.Fatal(err)
	}
	job = waitJob(t, manager, job.ID)
	if job.State != StateCompleted {
		t.Fatalf("job = %#v", job)
	}
	// Only the missing tail was transferred; the resumed prefix still counts.
	if job.ReceivedBytes != int64(len(body)) {
		t.Fatalf("received %d bytes, want %d", job.ReceivedBytes, len(body))
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != string(body) {
		t.Fatalf("data = %q, err = %v", data, err)
	}
}

func TestRestartsWhenServerIgnoresRange(t *testing.T) {
	body := []byte("artifact-payload")
	server := rangeServer(t, body, false)
	target := filepath.Join(t.TempDir(), "model.bin")
	// A stale partial whose bytes are wrong: appending would corrupt the file.
	if err := os.WriteFile(target+".part", []byte("XXXXXX"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := filePlan(target, server.URL, int64(len(body)), digestOf(body))

	manager := New(Options{HTTPClient: server.Client()})
	job, err := manager.Start(context.Background(), Request{Plan: plan})
	if err != nil {
		t.Fatal(err)
	}
	if job = waitJob(t, manager, job.ID); job.State != StateCompleted {
		t.Fatalf("job = %#v", job)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != string(body) {
		t.Fatalf("data = %q, err = %v", data, err)
	}
}

func TestDigestMismatchFailsAndRemovesArtifact(t *testing.T) {
	body := []byte("artifact")
	server := rangeServer(t, body, false)
	target := filepath.Join(t.TempDir(), "model.bin")
	plan := filePlan(target, server.URL, int64(len(body)), digestOf([]byte("something else")))

	manager := New(Options{HTTPClient: server.Client()})
	job, err := manager.Start(context.Background(), Request{Plan: plan})
	if err != nil {
		t.Fatal(err)
	}
	if job = waitJob(t, manager, job.ID); job.State != StateFailed {
		t.Fatalf("job = %#v", job)
	}
	for _, path := range []string{target, target + ".part"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s survived a digest mismatch: %v", path, err)
		}
	}
}

func TestSizeCapFailsOversizedArtifact(t *testing.T) {
	body := []byte("artifact-payload")
	server := rangeServer(t, body, false)
	target := filepath.Join(t.TempDir(), "model.bin")

	manager := New(Options{HTTPClient: server.Client(), MaxBytes: 4})
	job, err := manager.Start(context.Background(), Request{Plan: filePlan(target, server.URL, 0, "")})
	if err != nil {
		t.Fatal(err)
	}
	job = waitJob(t, manager, job.ID)
	if job.State != StateFailed || !strings.Contains(job.Error, "larger than") {
		t.Fatalf("job = %#v", job)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("oversized artifact was finalized: %v", err)
	}
}

func TestRedirectToDisallowedHostIsRefused(t *testing.T) {
	body := []byte("artifact")
	final := rangeServer(t, body, false)
	// Same process, different hostname: reachable, but off the allowlist.
	elsewhere := strings.Replace(final.URL, "127.0.0.1", "localhost", 1)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere, http.StatusFound)
	}))
	defer redirect.Close()
	target := filepath.Join(t.TempDir(), "model.bin")

	// Only the redirecting host is allowed, so following it must fail.
	allowed := mustHost(t, redirect.URL)
	manager := New(Options{HTTPClient: redirect.Client(), AllowedHosts: []string{allowed}})
	job, err := manager.Start(context.Background(), Request{Plan: filePlan(target, redirect.URL, 0, "")})
	if err != nil {
		t.Fatal(err)
	}
	if job = waitJob(t, manager, job.ID); job.State != StateFailed || !strings.Contains(job.Error, "not allowed") {
		t.Fatalf("job = %#v", job)
	}
}

func TestRecoverResumesInterruptedJobAndDiscardsStalePartial(t *testing.T) {
	body := []byte("artifact-payload")
	server := rangeServer(t, body, true)
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	target := filepath.Join(dir, "model.bin")
	plan := filePlan(target, server.URL, int64(len(body)), digestOf(body))

	// A record left behind by a daemon killed mid-transfer, plus its partial.
	crashed := New(Options{HTTPClient: server.Client(), StateDir: state})
	interrupted := newJob(Request{Plan: plan}, StateRunning)
	crashed.store(interrupted, Request{Plan: plan})
	if err := os.WriteFile(target+".part", body[:6], 0o600); err != nil {
		t.Fatal(err)
	}

	restarted := New(Options{HTTPClient: server.Client(), StateDir: state})
	if err := restarted.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if job := waitJob(t, restarted, interrupted.ID); job.State != StateCompleted {
		t.Fatalf("job = %#v", job)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != string(body) {
		t.Fatalf("data = %q, err = %v", data, err)
	}

	// A finished job's leftover partial is discarded rather than resumed.
	if err := os.WriteFile(target+".part", []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := New(Options{HTTPClient: server.Client(), StateDir: state}).Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target + ".part"); !os.IsNotExist(err) {
		t.Fatalf("stale partial survived recovery: %v", err)
	}
}

func mustHost(t *testing.T, raw string) string {
	t.Helper()
	host := strings.TrimPrefix(raw, "http://")
	if index := strings.Index(host, ":"); index >= 0 {
		host = host[:index]
	}
	return host
}
