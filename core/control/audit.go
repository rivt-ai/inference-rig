package control

import (
	"context"
	"time"
)

// AuditEvent is the neutral record of a control-plane operation, emitted to
// every configured AuditSink after a mutating action completes.
type AuditEvent struct {
	Protocol  string
	Action    string
	Success   bool
	ErrorKind ErrorKind
	Duration  time.Duration
}

// AuditSink receives audit events. Implementations must be safe for concurrent
// use.
type AuditSink interface {
	Record(ctx context.Context, event AuditEvent)
}
