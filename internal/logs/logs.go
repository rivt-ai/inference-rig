// Package logs provides the process-wide structured logger. It uses the
// standard library's log/slog so the neutral core carries no logging
// dependency; backends and services share this logger.
package logs

import (
	"log/slog"
	"os"
)

// New returns a text slog.Logger at the given level, writing to stderr.
func New(level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
