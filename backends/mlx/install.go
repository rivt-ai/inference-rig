package mlx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"inferencerig/backends"
)

// ErrNoManagedInstall marks an upgrade without an existing environment.
var ErrNoManagedInstall = errors.New("no managed MLX install")

type commandRunner interface {
	Run(context.Context, string, io.Writer, string, ...string) error
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, dir string, out io.Writer, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir, command.Stdout, command.Stderr = dir, out, out
	return command.Run()
}

type installState struct {
	Version    string `json:"version"`
	Executable string `json:"executable"`
}

type installer struct {
	backend *Backend
	mu      sync.Mutex
}

// Install creates or upgrades the managed Python environment idempotently.
func (b *Backend) Install(ctx context.Context, opts backends.InstallOptions) (backends.InstallResult, error) {
	return b.installer.install(ctx, opts)
}

// InstallStatus reports a valid managed environment first, then checks whether
// the configured host Python can import the server package.
func (b *Backend) InstallStatus(ctx context.Context) (backends.InstallStatus, error) {
	root, err := b.engineRoot()
	if err != nil {
		return backends.InstallStatus{}, err
	}
	state, err := backends.ReadInstallState[installState](root)
	if err != nil {
		return backends.InstallStatus{}, err
	}
	if state.Executable != "" {
		if info, statErr := os.Stat(state.Executable); statErr == nil && !info.IsDir() {
			return backends.InstallStatus{
				Installed: true, Managed: true,
				Version: state.Version, Path: state.Executable,
			}, nil
		}
	}
	path, err := exec.LookPath(b.opts.Executable)
	if err != nil {
		return backends.InstallStatus{}, nil
	}
	if err := b.opts.runner.Run(ctx, "", io.Discard, path, "-c", "import mlx_lm, mlx_lm.server"); err != nil {
		if ctx.Err() != nil {
			return backends.InstallStatus{}, ctx.Err()
		}
		return backends.InstallStatus{}, nil
	}
	return backends.InstallStatus{Installed: true, Path: path}, nil
}

func (i *installer) install(ctx context.Context, opts backends.InstallOptions) (backends.InstallResult, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.validateHost(); err != nil {
		return backends.InstallResult{}, err
	}
	root, err := i.backend.engineRoot()
	if err != nil {
		return backends.InstallResult{}, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return backends.InstallResult{}, err
	}
	inspection, err := inspectInstall(root, opts)
	if err != nil {
		return backends.InstallResult{}, err
	}
	if inspection.state.Version == inspection.version && inspection.state.Executable == inspection.python &&
		inspection.statErr == nil && !opts.Force {
		return backends.InstallResult{
			Version: inspection.version, Path: inspection.python, Changed: false,
			Message: "mlx-lm " + inspection.version + " already installed",
		}, nil
	}
	progress := opts.Progress
	if progress == nil {
		progress = io.Discard
	}
	if err := i.ensureEnvironment(ctx, root, inspection.python, inspection.statErr, progress); err != nil {
		return backends.InstallResult{}, err
	}
	return i.installPackage(ctx, root, inspection.python, inspection.version, progress)
}

type installInspection struct {
	state   installState
	python  string
	statErr error
	version string
}

func inspectInstall(root string, opts backends.InstallOptions) (installInspection, error) {
	state, err := backends.ReadInstallState[installState](root)
	if err != nil {
		return installInspection{}, err
	}
	python := filepath.Join(root, "venv", "bin", "python")
	_, statErr := os.Stat(python)
	if opts.Upgrade && errors.Is(statErr, os.ErrNotExist) {
		return installInspection{}, ErrNoManagedInstall
	}
	version := opts.Version
	if version == "" {
		version = ManagedVersion
	}
	return installInspection{state: state, python: python, statErr: statErr, version: version}, nil
}

func (i *installer) validateHost() error {
	if i.backend.opts.goos == "darwin" && i.backend.opts.goarch == "arm64" {
		return nil
	}
	return fmt.Errorf(
		"mlx-lm requires darwin/arm64; current host is %s/%s",
		i.backend.opts.goos, i.backend.opts.goarch,
	)
}

func (i *installer) ensureEnvironment(ctx context.Context, root, python string, statErr error, progress io.Writer) error {
	if statErr == nil {
		return nil
	}
	if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if err := i.run(ctx, root, progress, i.backend.opts.Executable, "-m", "venv", filepath.Join(root, "venv")); err != nil {
		return fmt.Errorf("create Python environment: %w", err)
	}
	if _, err := os.Stat(python); err != nil {
		return fmt.Errorf("managed Python was not created: %w", err)
	}
	return nil
}

func (i *installer) installPackage(ctx context.Context, root, python, version string, progress io.Writer) (backends.InstallResult, error) {
	requirement := "mlx-lm==" + version
	if err := i.run(ctx, root, progress, python, "-m", "pip", "install", "--upgrade", requirement); err != nil {
		return backends.InstallResult{}, fmt.Errorf("install %s: %w", requirement, err)
	}
	if err := i.run(ctx, root, progress, python, "-c", "import mlx_lm, mlx_lm.server"); err != nil {
		return backends.InstallResult{}, fmt.Errorf("validate mlx-lm: %w", err)
	}
	if err := backends.WriteInstallState(root, installState{Version: version, Executable: python}); err != nil {
		return backends.InstallResult{}, err
	}
	return backends.InstallResult{
		Version: version, Path: python, Changed: true, Message: "installed mlx-lm " + version,
	}, nil
}

func (i *installer) run(ctx context.Context, dir string, progress io.Writer, name string, args ...string) error {
	if _, err := fmt.Fprintln(progress, name+" "+strings.Join(args, " ")); err != nil {
		return err
	}
	return i.backend.opts.runner.Run(ctx, dir, progress, name, args...)
}

func (i *installer) activeExecutable() (string, bool) {
	root, err := i.backend.engineRoot()
	if err != nil {
		return "", false
	}
	state, err := backends.ReadInstallState[installState](root)
	if err != nil || state.Executable == "" {
		return "", false
	}
	// Report an empty path whenever the bool is false, matching the llama.cpp
	// backend, so a caller that ignores the bool cannot pick up a path that was
	// just found to be missing or a directory.
	if info, err := os.Stat(state.Executable); err != nil || info.IsDir() {
		return "", false
	}
	return state.Executable, true
}
