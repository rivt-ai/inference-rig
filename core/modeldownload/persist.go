package modeldownload

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"inferencerig/platform/filedoc"
)

// record is the persisted form of one job: the observable state plus the plan
// needed to finish it after a restart.
type record struct {
	Job     Job     `json:"job"`
	Request Request `json:"request"`
}

// Recover reconciles persisted jobs and their partial files after a restart.
// A job interrupted mid-transfer is re-queued — the transfer then resumes when
// the server proves range support and restarts from zero when it does not — a
// job whose target landed is completed, and a partial file left by a finished
// job is discarded. Every decision is logged.
func (m *Manager) Recover(ctx context.Context) error {
	if m.stateDir == "" {
		return nil
	}
	entries, err := os.ReadDir(m.stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(m.stateDir, entry.Name())
		rec, err := readRecord(path)
		if err != nil {
			m.log("discarding unreadable download record", "file", entry.Name(), "error", err)
			_ = os.Remove(path)
			continue
		}
		m.reconcile(ctx, rec)
	}
	return nil
}

func (m *Manager) reconcile(ctx context.Context, rec record) {
	if err := ctx.Err(); err != nil {
		return
	}
	partial := rec.Request.Plan.TargetRoot + ".part"
	switch {
	case rec.Job.State != StateQueued && rec.Job.State != StateRunning:
		m.restore(rec)
		m.discard(partial, rec.Job.ID, "download already finished")
	case exists(rec.Job.TargetPath):
		rec.Job.State, rec.Job.Percent = StateCompleted, 100
		if rec.Job.CompletedAt == "" {
			rec.Job.CompletedAt = timestamp()
		}
		m.restore(rec)
		m.persist(rec.Job.ID)
		m.discard(partial, rec.Job.ID, "target landed before the restart")
	default:
		m.log("requeueing interrupted download", "job", rec.Job.ID,
			"target", rec.Job.TargetPath, "partial_bytes", partialSize(partial))
		rec.Job.State, rec.Job.Error = StateQueued, ""
		rec.Job.ReceivedBytes, rec.Job.Percent = 0, 0
		m.launch(rec.Job, rec.Request)
	}
}

// restore makes a persisted job observable again without running it.
func (m *Manager) restore(rec record) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := rec.Job
	m.jobs[job.ID] = &job
	m.requests[job.ID] = rec.Request
}

func (m *Manager) discard(partial, id, reason string) {
	if partialSize(partial) == 0 && !exists(partial) {
		return
	}
	m.log("discarding partial artifact", "job", id, "path", partial, "reason", reason)
	if err := os.RemoveAll(partial); err != nil {
		m.log("discarding partial artifact failed", "job", id, "path", partial, "error", err)
	}
}

// persist writes the job's record atomically. Failures are logged rather than
// surfaced: a lost record costs recoverability, not the running download.
func (m *Manager) persist(id string) {
	if m.stateDir == "" {
		return
	}
	m.mu.Lock()
	job, req := m.jobs[id], m.requests[id]
	var rec record
	if job != nil {
		rec = record{Job: *job, Request: req}
	}
	m.mu.Unlock()
	if job == nil {
		return
	}
	data, err := json.Marshal(rec)
	if err == nil {
		err = filedoc.AtomicCreate(filepath.Join(m.stateDir, id+".json"), data, 0o600)
	}
	if err != nil {
		m.log("persisting download record failed", "job", id, "error", err)
	}
}

func readRecord(path string) (record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return record{}, err
	}
	var rec record
	if err := json.Unmarshal(data, &rec); err != nil {
		return record{}, err
	}
	if rec.Job.ID == "" || rec.Request.Plan.TargetRoot == "" {
		return record{}, errors.New("record is missing a job id or plan")
	}
	return rec, nil
}
