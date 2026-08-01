package control

import (
	"context"
	"time"

	"inferencerig/core/runtime"
)

// AuditEvent is the neutral record of a control-plane operation, emitted to
// every configured AuditSink after a mutating action completes.
type AuditEvent struct {
	Protocol  string
	Action    string
	Success   bool
	ErrorKind ErrorKind
	Duration  time.Duration
	Detail    string

	// OperationID and State describe runtime transitions; Profile also identifies
	// per-profile autostart outcomes.
	OperationID string
	Profile     string
	Backend     string
	State       runtime.State
	Recovery    runtime.RecoveryClassification
}

// AuditSink receives audit events. Implementations must be safe for concurrent
// use.
type AuditSink interface {
	Record(ctx context.Context, event AuditEvent)
}
