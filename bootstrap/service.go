// Package bootstrap assembles the runnable control daemon.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
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
	"inferencerig/platform/pidfile"
)

const shutdownTimeout = 5 * time.Second

// hostResourceProber is the optional backend facet reporting accelerator
// hardware: llamacpp probes discrete VRAM, mlx reports Apple unified memory.
// Backends that do not implement it contribute no accelerator rows.
type hostResourceProber interface {
	HostResources(context.Context) (backends.HostResources, []string)
}

// telemetry builds the host signals collector, wiring the disks worth watching
// and the accelerator probe assembled from registered backend policy.
func telemetry(registry *backends.Registry, modelStorageDir string) *signals.GopsutilCollector {
	collector := signals.NewGopsutilCollector(nil, diskTargets(modelStorageDir))
	collector.Accelerators = acceleratorProbe(registry)
	return collector
}

// diskTargets watches the root filesystem plus model storage when it exists.
// A not-yet-created storage dir is skipped so a fresh install reports no
// spurious disk warning.
func diskTargets(modelStorageDir string) []signals.DiskTarget {
	targets := []signals.DiskTarget{{Label: "root", Path: "/"}}
	if modelStorageDir == "" {
		return targets
	}
	if _, err := os.Stat(modelStorageDir); err != nil {
		return targets
	}
	return append(targets, signals.DiskTarget{Label: "model_storage", Path: modelStorageDir})
}

// acceleratorProbe asks every registered backend implementing the host-resource
// facet what accelerator it sees, translating both memory models into the one
// neutral telemetry shape. Unified devices leave their byte fields to the
// collector, which resolves them against system RAM.
func acceleratorProbe(registry *backends.Registry) func(context.Context) ([]signals.AcceleratorStats, []string) {
	return func(ctx context.Context) ([]signals.AcceleratorStats, []string) {
		stats := []signals.AcceleratorStats{}
		warnings := []string{}
		for _, name := range registry.Names() {
			backend, ok := registry.Lookup(name)
			if !ok {
				continue
			}
			prober, ok := backend.(hostResourceProber)
			if !ok {
				continue
			}
			host, hostWarnings := prober.HostResources(ctx)
			warnings = append(warnings, hostWarnings...)
			if !host.HasGPU && !host.UnifiedMemory {
				continue
			}
			stats = append(stats, signals.AcceleratorStats{
				Name:          host.AcceleratorName,
				UnifiedMemory: host.UnifiedMemory,
				TotalBytes:    uint64(max(host.VRAMBytes, 0)),
				UsedBytes:     uint64(max(host.VRAMUsedBytes, 0)),
			})
		}
		return stats, warnings
	}
}

// Service owns the canonical control daemon and its dependencies.
type Service struct {
	Manager *control.Manager
	Server  rpc.Server
	pidFile pidfile.File
	pid     int
}

// NewService assembles all built-in backends behind the neutral control plane.
func NewService() (*Service, error) {
	cfg, err := config.LoadOrDefault()
	if err != nil {
		return nil, err
	}
	modelStorageDir := cfg.ModelStorageDir
	if modelStorageDir == "" {
		modelStorageDir, err = config.DefaultModelStorageDir()
		if err != nil {
			return nil, err
		}
	}
	modelStorageDir = config.ExpandHome(modelStorageDir)
	registry := backends.NewRegistry()
	if err := all.Register(registry, all.Options{ModelStorageDir: modelStorageDir}); err != nil {
		return nil, err
	}
	paths, err := config.ResolvePaths()
	if err != nil {
		return nil, err
	}
	store := profiles.NewFileStore(paths.Profiles, 0, registry.BackendLookup())
	downloads := modeldownload.New(modeldownload.Options{
		StateDir: paths.DownloadState, Logger: slog.Default(),
	})
	if err := downloads.Recover(context.Background()); err != nil {
		slog.Default().Warn("reconciling interrupted downloads failed", "error", err)
	}
	manager := control.NewManager(control.Dependencies{
		Registry: registry, Profiles: store,
		Downloads: downloads,
		Signals:   telemetry(registry, modelStorageDir),
		Audit:     control.NewSlogSink(slog.Default()),
		Catalog:   modelcatalog.NewClient(modelcatalog.ClientOptions{CacheDir: paths.CatalogCache, CacheTTL: time.Hour}),
		Config:    configstore.NewFileStore(paths.Config, 0),
	})
	if err := prepareRuntimes(context.Background(), manager, cfg.AutostartProfiles); err != nil {
		return nil, err
	}
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

func prepareRuntimes(ctx context.Context, manager *control.Manager, autostart []string) error {
	if err := manager.ValidateAutostart(ctx, autostart); err != nil {
		return fmt.Errorf("validate autostart profiles: %w", err)
	}
	if err := manager.RecoverRuntimes(ctx); err != nil {
		return err
	}
	return manager.AutostartProfiles(ctx, autostart)
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
