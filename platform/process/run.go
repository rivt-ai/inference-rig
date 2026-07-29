package process

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"inferencerig/config"
	"inferencerig/platform/audit"
)

// defaultShutdownTimeout bounds graceful shutdown of a serving process.
const defaultShutdownTimeout = 5 * time.Second

// Run supervises a long-lived serving process: it runs run in a goroutine,
// handles interrupt/terminate signals, periodically prunes archived logs, and
// invokes shutdown with a bounded timeout. It terminates the process with a
// non-zero exit status on an unrecoverable serve or shutdown error.
func Run(
	ctx context.Context,
	logger *slog.Logger,
	name string,
	cfg config.Config,
	run func() error,
	shutdown func(context.Context) error,
) {
	errs := make(chan error, 1)
	go func() { errs <- run() }()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	cleanupArchives(logger, cfg.LogArchiveRetention)
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cleanupArchives(logger, cfg.LogArchiveRetention)
		case <-ctx.Done():
			logger.Info("stopping "+name, "error", ctx.Err())
			shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
			defer cancel()
			if err := shutdown(shutdownCtx); err != nil {
				logger.Error("shutdown "+name, "error", err)
				os.Exit(1)
			}
			return
		case err := <-errs:
			if !errors.Is(err, http.ErrServerClosed) {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
				defer cancel()
				_ = shutdown(shutdownCtx)
				logger.Error("serve "+name, "error", err)
				os.Exit(1)
			}
			return
		}
	}
}

func cleanupArchives(logger *slog.Logger, retention time.Duration) {
	if _, err := audit.CleanupArchives(retention, time.Now()); err != nil {
		logger.Warn("clean log archives", "error", err)
	}
}
