// Package modeldownload executes neutral backend artifact plans. It owns job
// state, HTTP transfer, cancellation, staging, and atomic finalization; it has
// no backend or model-format knowledge.
package modeldownload

import (
	"context"
	"errors"

	"inferencerig/backends"
)

var (
	ErrInvalidInput = errors.New("download input is invalid")
	ErrNotFound     = errors.New("download not found")
	ErrConflict     = errors.New("download conflict")
)

// State is a download job state.
type State string

const (
	StateQueued            State = "queued"
	StateRunning           State = "running"
	StateCompleted         State = "completed"
	StateFailed            State = "failed"
	StateCancelled         State = "cancelled"
	StateAlreadyDownloaded State = "already_downloaded"
)

// IsTerminal reports whether a job in this state will never change again.
//
// It lives here, next to the constants, because every caller that waits on a
// download needs the same answer and each one spelling out its own list is how
// a new state gets missed by all but one of them. Callers that only have the
// wire string can convert: State(job.GetState()).
func (s State) IsTerminal() bool {
	switch s {
	case StateCompleted, StateFailed, StateCancelled, StateAlreadyDownloaded:
		return true
	default:
		return false
	}
}

// Succeeded reports whether the job finished with the artifact in place.
// already_downloaded is a success: the file the caller asked for is there.
func (s State) Succeeded() bool {
	return s == StateCompleted || s == StateAlreadyDownloaded
}

// Request starts execution of one backend-generated plan.
type Request struct {
	Plan    backends.ArtifactPlan `json:"plan"`
	Force   bool                  `json:"force,omitempty"`
	Backend string                `json:"backend,omitempty"`
	Profile string                `json:"profile,omitempty"`
}

// Job is the observable state of one artifact-plan execution.
type Job struct {
	ID            string  `json:"id"`
	State         State   `json:"state"`
	MultiFile     bool    `json:"multi_file"`
	TargetPath    string  `json:"target_path"`
	ItemCount     int     `json:"item_count"`
	ReceivedBytes int64   `json:"received_bytes"`
	TotalBytes    int64   `json:"total_bytes"`
	Percent       float64 `json:"percent"`
	Revision      string  `json:"revision,omitempty"`
	Error         string  `json:"error,omitempty"`
	StartedAt     string  `json:"started_at,omitempty"`
	CompletedAt   string  `json:"completed_at,omitempty"`
	Backend       string  `json:"backend,omitempty"`
	Profile       string  `json:"profile,omitempty"`
}

// Downloader is the neutral download job API consumed by control.
type Downloader interface {
	Start(context.Context, Request) (Job, error)
	Get(context.Context, string) (Job, error)
	Cancel(context.Context, string) (Job, error)
}
