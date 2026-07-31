package control

import (
	"context"
	"log/slog"
)

// SlogSink records control-plane audit events to a structured slog logger. It
// implements AuditSink. It lives beside the interface it satisfies rather than
// in platform/audit because a platform package must not depend on core; that
// inverted edge is what kept core/runtime's supervisor from opening its own
// service log.
type SlogSink struct {
	logger *slog.Logger
}

// NewSlogSink returns a SlogSink that writes to logger. A nil logger discards
// events.
func NewSlogSink(logger *slog.Logger) SlogSink {
	return SlogSink{logger: logger}
}

func (s SlogSink) Record(_ context.Context, event AuditEvent) {
	if s.logger == nil {
		return
	}
	attrs := []any{
		slog.String("action", event.Action),
		slog.Bool("success", event.Success),
		slog.Duration("duration", event.Duration),
	}
	if event.ErrorKind != "" {
		attrs = append(attrs, slog.String("error_kind", string(event.ErrorKind)))
	}
	s.logger.Info("audit event", attrs...)
}
