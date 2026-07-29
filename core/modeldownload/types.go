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
