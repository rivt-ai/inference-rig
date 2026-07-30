// Package logs provides the process-wide structured logger. It uses the
// standard library's log/slog so the neutral core carries no logging
// dependency; backends and services share this logger.
package logs

import (
	"io"
	"log/slog"
)

// New returns a text slog.Logger at the given level, writing to w.
func New(level slog.Level, w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}
