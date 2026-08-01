package modeldownload

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"inferencerig/backends"
	"inferencerig/platform/filedoc"
)

const (
	// DefaultMaxBytes caps one artifact transfer, bounding a server that
	// streams a body without end.
	DefaultMaxBytes = 512 << 30
	// DefaultMaxRedirects caps redirects followed per artifact request.
	DefaultMaxRedirects = 5
)

// Options configures a Manager.
type Options struct {
	HTTPClient *http.Client
	// StateDir persists job records so interrupted downloads are recoverable.
	// Empty disables persistence, and with it Recover.
	StateDir string
	// Logger records transfer and reconciliation decisions. Nil discards them.
	Logger *slog.Logger
	// MaxBytes caps one artifact transfer. Zero means DefaultMaxBytes.
	MaxBytes int64
	// MaxRedirects caps redirects per request. Zero means DefaultMaxRedirects.
	MaxRedirects int
	// AllowedHosts restricts artifact hosts to this set. Empty allows any host;
	// the scheme policy (http/https only) applies either way.
	AllowedHosts []string
}

// Manager executes artifact plans asynchronously and retains in-memory job
// state for observation and cancellation.
type Manager struct {
	client       *http.Client
	logger       *slog.Logger
	stateDir     string
	maxBytes     int64
	maxRedirects int
	allowedHosts map[string]struct{}
	mu           sync.Mutex
	jobs         map[string]*Job
	requests     map[string]Request
	active       map[string]string
	cancel       map[string]context.CancelFunc
}

// New creates a download manager.
func New(opts Options) *Manager {
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	m := &Manager{
		logger: opts.Logger, stateDir: opts.StateDir,
		maxBytes: opts.MaxBytes, maxRedirects: opts.MaxRedirects,
		allowedHosts: map[string]struct{}{},
		jobs:         map[string]*Job{}, requests: map[string]Request{},
		active: map[string]string{}, cancel: map[string]context.CancelFunc{},
	}
	if m.maxBytes <= 0 {
		m.maxBytes = DefaultMaxBytes
	}
	if m.maxRedirects <= 0 {
		m.maxRedirects = DefaultMaxRedirects
	}
	for _, host := range opts.AllowedHosts {
		m.allowedHosts[strings.ToLower(host)] = struct{}{}
	}
	policed := *client
	policed.CheckRedirect = m.checkRedirect
	m.client = &policed
	if m.stateDir != "" {
		if err := os.MkdirAll(m.stateDir, 0o700); err != nil {
			m.log("download state directory is unusable", "dir", m.stateDir, "error", err)
			m.stateDir = ""
		}
	}
	return m
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
		m.store(job, req)
		return job, nil
	}
	return m.launch(newJob(req, StateQueued), req), nil
}

// launch registers a job and runs its transfer. A target that is already being
// downloaded returns the job that owns it instead of a second one.
func (m *Manager) launch(job Job, req Request) Job {
	m.mu.Lock()
	if id := m.active[job.TargetPath]; id != "" {
		existing := *m.jobs[id]
		m.mu.Unlock()
		return existing
	}
	stored := job
	m.jobs[job.ID] = &stored
	m.requests[job.ID] = req
	m.active[job.TargetPath] = job.ID
	downloadCtx, cancel := context.WithCancel(context.Background())
	m.cancel[job.ID] = cancel
	m.mu.Unlock()
	m.persist(job.ID)
	go m.run(downloadCtx, job.ID, req)
	return job
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
	// The run goroutine observes the cancelled context and persists the
	// terminal record on its way out, so Cancel does not write one itself.
	return *job, nil
}

func (m *Manager) run(ctx context.Context, id string, req Request) {
	m.update(id, func(job *Job) {
		if job.State != StateCancelled {
			job.State, job.StartedAt = StateRunning, timestamp()
		}
	})
	m.persist(id)
	defer m.clearActive(id)
	defer m.persist(id)
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
	if err := m.downloadItem(ctx, id, item, stage); err != nil {
		// The partial file survives a failure on purpose: the next attempt
		// resumes from it, and a digest mismatch has already removed it.
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
		if err := prepareParent(stagedPath, ""); err != nil {
			return err
		}
		if err := m.downloadItem(ctx, id, item, stagedPath); err != nil {
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

// downloadItem transfers one artifact into destination. An existing partial
// file is resumed only when the server answers the range request with a
// matching 206; any other success restarts the transfer from zero.
func (m *Manager) downloadItem(ctx context.Context, id string, item backends.ArtifactItem, destination string) error {
	offset := partialSize(destination)
	resp, err := m.get(ctx, item.URI, offset)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	offset, err = m.resumeOffset(id, item.URI, resp, offset)
	if err != nil {
		return err
	}
	if resp.ContentLength > 0 && offset+resp.ContentLength > m.maxBytes {
		return fmt.Errorf("%w: artifact is larger than the %d byte limit", ErrInvalidInput, m.maxBytes)
	}
	written, err := m.writeBody(id, resp.Body, destination, offset)
	if err != nil {
		return err
	}
	total := offset + written
	if item.SizeBytes > 0 && total != item.SizeBytes {
		return fmt.Errorf("artifact size %d differs from expected %d", total, item.SizeBytes)
	}
	return verifyDigest(destination, item.SHA256)
}

// resumeOffset reports where the transfer continues: offset when the server
// answered the range request with a matching 206, and zero when the artifact
// has to be fetched again from the start.
func (m *Manager) resumeOffset(id, uri string, resp *http.Response, offset int64) (int64, error) {
	switch {
	case offset > 0 && resp.StatusCode == http.StatusPartialContent && resumesAt(resp, offset):
		m.log("resuming artifact download", "job", id, "uri", uri, "offset", offset)
		m.addProgress(id, offset)
		return offset, nil
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		if offset > 0 {
			m.log("restarting artifact download: no usable range support",
				"job", id, "uri", uri, "status", resp.StatusCode)
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("download artifact: status %d", resp.StatusCode)
	}
}

// get issues the artifact request under the host, scheme and redirect policy,
// asking for the remainder of the file when a partial one is already on disk.
func (m *Manager) get(ctx context.Context, uri string, offset int64) (*http.Response, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("%w: artifact URI %q", ErrInvalidInput, uri)
	}
	if err := m.checkURL(parsed); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download artifact: %w", err)
	}
	return resp, nil
}

func (m *Manager) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > m.maxRedirects {
		return fmt.Errorf("%w: more than %d redirects", ErrInvalidInput, m.maxRedirects)
	}
	return m.checkURL(req.URL)
}

func (m *Manager) checkURL(uri *url.URL) error {
	if uri.Scheme != "http" && uri.Scheme != "https" {
		return fmt.Errorf("%w: unsupported artifact scheme %q", ErrInvalidInput, uri.Scheme)
	}
	if len(m.allowedHosts) == 0 {
		return nil
	}
	if _, ok := m.allowedHosts[strings.ToLower(uri.Hostname())]; !ok {
		return fmt.Errorf("%w: artifact host %q is not allowed", ErrInvalidInput, uri.Hostname())
	}
	return nil
}

func (m *Manager) writeBody(id string, body io.Reader, destination string, offset int64) (int64, error) {
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if offset > 0 {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	file, err := os.OpenFile(destination, flags, 0o600)
	if err != nil {
		return 0, err
	}
	// One byte past the cap so an over-long body is detected rather than
	// silently truncated into a file that looks complete.
	limited := io.LimitReader(body, m.maxBytes-offset+1)
	written, copyErr := io.Copy(&progressWriter{writer: file, add: func(n int64) { m.addProgress(id, n) }}, limited)
	syncErr := file.Sync()
	closeErr := file.Close()
	switch {
	case copyErr != nil:
		return written, copyErr
	case offset+written > m.maxBytes:
		return written, fmt.Errorf("%w: artifact is larger than the %d byte limit", ErrInvalidInput, m.maxBytes)
	case syncErr != nil:
		return written, syncErr
	}
	return written, closeErr
}

// resumesAt reports whether the response continues the file at offset.
func resumesAt(resp *http.Response, offset int64) bool {
	return strings.HasPrefix(resp.Header.Get("Content-Range"), "bytes "+strconv.FormatInt(offset, 10)+"-")
}

func partialSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return 0
	}
	return info.Size()
}

// verifyDigest checks a downloaded artifact against its catalog digest and
// removes it on mismatch, so a corrupt transfer is never finalized or resumed.
// ponytail: re-reads the file to hash it; incremental hash state would have to
// survive both a resume and a restart to avoid that.
func verifyDigest(path, want string) error {
	if want == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if got := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(got, want) {
		_ = os.Remove(path)
		return fmt.Errorf("%w: artifact digest %s differs from expected %s", ErrInvalidInput, got, want)
	}
	return nil
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

func (m *Manager) store(job Job, req Request) {
	m.mu.Lock()
	stored := job
	m.jobs[job.ID] = &stored
	m.requests[job.ID] = req
	m.mu.Unlock()
	m.persist(job.ID)
}

func (m *Manager) log(message string, args ...any) {
	if m.logger != nil {
		m.logger.Info(message, args...)
	}
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
		ItemCount: len(plan.Items), TotalBytes: plan.TotalBytes, Revision: plan.Revision,
		Backend: req.Backend, Profile: req.Profile,
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
