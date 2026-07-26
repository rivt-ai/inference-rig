package modeldownload

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"inferencerig/backends"
	"inferencerig/platform/filedoc"
)

// Options configures a Manager.
type Options struct {
	HTTPClient *http.Client
}

// Manager executes artifact plans asynchronously and retains in-memory job
// state for observation and cancellation.
type Manager struct {
	client *http.Client
	mu     sync.Mutex
	jobs   map[string]*Job
	active map[string]string
	cancel map[string]context.CancelFunc
}

// New creates a download manager.
func New(opts Options) *Manager {
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &Manager{
		client: client, jobs: map[string]*Job{},
		active: map[string]string{}, cancel: map[string]context.CancelFunc{},
	}
}

// Start validates and queues a plan. A duplicate active target returns the
// existing job.
func (m *Manager) Start(ctx context.Context, req Request) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	if err := validatePlan(req.Plan); err != nil {
		return Job{}, err
	}
	if exists(req.Plan.TargetRoot) && !req.Force {
		job := newJob(req, StateAlreadyDownloaded)
		job.ReceivedBytes, job.Percent, job.CompletedAt = job.TotalBytes, 100, timestamp()
		m.store(job)
		return job, nil
	}
	m.mu.Lock()
	if id := m.active[req.Plan.TargetRoot]; id != "" {
		job := *m.jobs[id]
		m.mu.Unlock()
		return job, nil
	}
	job := newJob(req, StateQueued)
	stored := job
	m.jobs[job.ID] = &stored
	m.active[job.TargetPath] = job.ID
	downloadCtx, cancel := context.WithCancel(context.Background())
	m.cancel[job.ID] = cancel
	m.mu.Unlock()
	go m.run(downloadCtx, job.ID, req)
	return job, nil
}

// Get returns a snapshot of a job.
func (m *Manager) Get(ctx context.Context, id string) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.jobs[id]
	if job == nil {
		return Job{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return *job, nil
}

// Cancel cancels a queued or running job.
func (m *Manager) Cancel(ctx context.Context, id string) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.jobs[id]
	if job == nil {
		return Job{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if job.State != StateQueued && job.State != StateRunning {
		return Job{}, fmt.Errorf("%w: download %s is %s", ErrConflict, id, job.State)
	}
	if cancel := m.cancel[id]; cancel != nil {
		cancel()
	}
	job.State, job.CompletedAt = StateCancelled, timestamp()
	return *job, nil
}

func (m *Manager) run(ctx context.Context, id string, req Request) {
	m.update(id, func(job *Job) {
		if job.State != StateCancelled {
			job.State, job.StartedAt = StateRunning, timestamp()
		}
	})
	defer m.clearActive(id)
	err := m.execute(ctx, id, req)
	m.update(id, func(job *Job) {
		job.CompletedAt = timestamp()
		switch {
		case job.State == StateCancelled || errors.Is(err, context.Canceled):
			job.State, job.Error = StateCancelled, ""
		case err != nil:
			job.State, job.Error = StateFailed, err.Error()
		default:
			job.State, job.Percent = StateCompleted, 100
		}
	})
}

func (m *Manager) execute(ctx context.Context, id string, req Request) error {
	if req.Plan.MultiFile {
		return m.executeDirectory(ctx, id, req)
	}
	return m.executeFile(ctx, id, req.Plan.Items[0], req.Force)
}

func (m *Manager) executeFile(ctx context.Context, id string, item backends.ArtifactItem, force bool) error {
	stage := item.TargetPath + ".part"
	if err := prepareParent(item.TargetPath, stage); err != nil {
		return err
	}
	if err := m.downloadItem(ctx, id, item.URI, stage, item.SizeBytes); err != nil {
		_ = os.Remove(stage)
		return err
	}
	if force {
		_ = os.Remove(item.TargetPath)
	}
	return finalize(stage, item.TargetPath)
}

func (m *Manager) executeDirectory(ctx context.Context, id string, req Request) error {
	root, stage := req.Plan.TargetRoot, req.Plan.TargetRoot+".part"
	if err := prepareDirectory(root, stage, req.Force); err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	for _, item := range req.Plan.Items {
		relative, err := filepath.Rel(root, item.TargetPath)
		if err != nil || !safeRelative(relative) {
			return fmt.Errorf("%w: artifact escapes target root", ErrInvalidInput)
		}
		stagedPath := filepath.Join(stage, relative)
		if err := prepareParent(stagedPath, stagedPath); err != nil {
			return err
		}
		if err := m.downloadItem(ctx, id, item.URI, stagedPath, item.SizeBytes); err != nil {
			return err
		}
	}
	if req.Force {
		if err := os.RemoveAll(root); err != nil {
			return err
		}
	}
	return finalize(stage, root)
}

func (m *Manager) downloadItem(ctx context.Context, id, uri, destination string, expected int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("download artifact: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download artifact: status %d", resp.StatusCode)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(&progressWriter{writer: file, add: func(n int64) { m.addProgress(id, n) }}, resp.Body)
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if expected > 0 && written != expected {
		return fmt.Errorf("artifact size %d differs from expected %d", written, expected)
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

type progressWriter struct {
	writer io.Writer
	add    func(int64)
}

func (w *progressWriter) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	w.add(int64(n))
	return n, err
}

func (m *Manager) store(job Job) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored := job
	m.jobs[job.ID] = &stored
}

func (m *Manager) update(id string, update func(*Job)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job := m.jobs[id]; job != nil {
		update(job)
	}
}

func (m *Manager) addProgress(id string, n int64) {
	m.update(id, func(job *Job) {
		job.ReceivedBytes += n
		if job.TotalBytes > 0 {
			job.Percent = float64(job.ReceivedBytes) * 100 / float64(job.TotalBytes)
		}
	})
}

func (m *Manager) clearActive(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job := m.jobs[id]; job != nil && m.active[job.TargetPath] == id {
		delete(m.active, job.TargetPath)
	}
	delete(m.cancel, id)
}

func newJob(req Request, state State) Job {
	plan := req.Plan
	return Job{
		ID: newID(), State: state, MultiFile: plan.MultiFile, TargetPath: plan.TargetRoot,
		ItemCount: len(plan.Items), TotalBytes: plan.TotalBytes, Backend: req.Backend, Profile: req.Profile,
	}
}

func timestamp() string { return time.Now().UTC().Format(time.RFC3339) }

func newID() string {
	var random [3]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Sprintf("dl_%d", time.Now().UnixNano())
	}
	return "dl_" + time.Now().UTC().Format("20060102_150405") + "_" + hex.EncodeToString(random[:])
}

var _ Downloader = (*Manager)(nil)

func finalize(stage, target string) error {
	if err := os.Rename(stage, target); err != nil {
		return fmt.Errorf("finalize artifact: %w", err)
	}
	return filedoc.SyncDir(filepath.Dir(target))
}
