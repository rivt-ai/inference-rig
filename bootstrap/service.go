// Package bootstrap assembles the runnable control daemon.
package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"inferencerig/backends"
	"inferencerig/backends/all"
	"inferencerig/config"
	"inferencerig/core/configstore"
	"inferencerig/core/control"
	"inferencerig/core/modelcatalog"
	"inferencerig/core/modeldownload"
	"inferencerig/core/profiles"
	"inferencerig/core/rpc"
	"inferencerig/core/signals"
	"inferencerig/platform/audit"
	"inferencerig/platform/pidfile"
)

const shutdownTimeout = 5 * time.Second

// Service owns the canonical control daemon and its dependencies.
type Service struct {
	Manager *control.Manager
	Server  rpc.Server
	pidFile pidfile.File
	pid     int
}

// NewService assembles all built-in backends behind the neutral control plane.
func NewService() (*Service, error) {
	registry := backends.NewRegistry()
	if err := all.Register(registry); err != nil {
		return nil, err
	}
	profileRoot, err := config.ProfilesDir()
	if err != nil {
		return nil, err
	}
	store := profiles.NewFileStore(profileRoot, 0, registry.BackendLookup())
	cacheDir, err := config.DefaultCatalogCacheDir()
	if err != nil {
		return nil, err
	}
	configPath, err := config.ConfigPath()
	if err != nil {
		return nil, err
	}
	manager := control.NewManager(control.Dependencies{
		Registry: registry, Profiles: store,
		Downloads: modeldownload.New(modeldownload.Options{}),
		Signals:   signals.NewGopsutilCollector(nil, nil),
		Audit:     audit.NewSink(slog.Default()),
		Catalog:   modelcatalog.NewClient(modelcatalog.ClientOptions{CacheDir: cacheDir, CacheTTL: time.Hour}),
		Config:    configstore.NewFileStore(configPath, 0),
	})
	path, handler := rpc.ControlHandler(rpc.NewControlService(manager))
	server, err := rpc.NewServer(path, handler)
	if err != nil {
		return nil, err
	}
	home, err := config.Home()
	if err != nil {
		_ = server.Listener.Close()
		return nil, err
	}
	file := pidfile.New(filepath.Join(home, "run", config.ProjectName+".pid"))
	pid := os.Getpid()
	if err := file.Write(pid); err != nil {
		_ = server.Listener.Close()
		_ = os.Remove(server.SocketPath)
		return nil, err
	}
	return &Service{Manager: manager, Server: server, pidFile: file, pid: pid}, nil
}

// Run serves until ctx is cancelled or the HTTP server fails.
func (s *Service) Run(ctx context.Context) error {
	errs := make(chan error, 1)
	go func() { errs <- s.Server.HTTP.Serve(s.Server.Listener) }()
	select {
	case <-ctx.Done():
		return s.shutdown()
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		_ = s.shutdown()
		return err
	}
}

// shutdown deliberately builds its own context: Run reaches here precisely
// when the caller's context is already cancelled, so inheriting it would give
// Shutdown no time to stop runtimes and clear the socket.
func (s *Service) shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return s.Shutdown(ctx)
}

// Shutdown stops active runtimes, closes the server, and removes its socket.
func (s *Service) Shutdown(ctx context.Context) error {
	err := errors.Join(s.Manager.StopAllRuntimes(ctx), s.Server.HTTP.Shutdown(ctx))
	_ = s.Server.Listener.Close()
	if removeErr := os.Remove(s.Server.SocketPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		err = errors.Join(err, removeErr)
	}
	if removeErr := s.pidFile.Remove(s.pid); removeErr != nil {
		err = errors.Join(err, removeErr)
	}
	return err
}
