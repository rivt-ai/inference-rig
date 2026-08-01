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

	// The fields below describe one runtime state-machine transition and are
	// empty on every other action. OperationID ties the transitions of a single
	// start, stop or reset together.
	OperationID string
	Profile     string
	Backend     string
	State       runtime.State
}

// AuditSink receives audit events. Implementations must be safe for concurrent
// use.
type AuditSink interface {
	Record(ctx context.Context, event AuditEvent)
}
