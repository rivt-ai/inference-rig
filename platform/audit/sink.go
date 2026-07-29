package audit

import (
	"context"
	"log/slog"

	"inferencerig/core/control"
)

// Sink records control-plane audit events to a structured slog logger. It
// implements control.AuditSink.
type Sink struct {
	logger *slog.Logger
}

// NewSink returns a Sink that writes to logger. A nil logger discards events.
func NewSink(logger *slog.Logger) Sink {
	return Sink{logger: logger}
}

func (s Sink) Record(_ context.Context, event control.AuditEvent) {
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
