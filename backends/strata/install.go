package strata

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"inferencerig/backends"
)

// ErrNoManagedInstall marks an upgrade without an existing checkout.
var ErrNoManagedInstall = errors.New("no managed Strata install")

type commandRunner interface {
	Run(context.Context, string, io.Writer, string, ...string) error
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, dir string, out io.Writer, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir, command.Stdout, command.Stderr = dir, out, out
	return command.Run()
}

type installer struct {
	backend *Backend
	mu      sync.Mutex
}

// Install clones a pinned Strata release and prepares its Python environment.
// The engine binary and model are fetched by Strata's own setup on the first
// start of a size-name profile, since which build fits depends on the GPU and
// which files to fetch depends on the profile.
func (b *Backend) Install(ctx context.Context, opts backends.InstallOptions) (backends.InstallResult, error) {
	return b.installer.install(ctx, opts)
}

// Rollback returns to the previously recorded checkout. Checkouts are
// version-scoped, so the previous one is still on disk.
func (b *Backend) Rollback(ctx context.Context) (backends.InstallResult, error) {
	b.installer.mu.Lock()
	defer b.installer.mu.Unlock()
	root, err := b.engineRoot()
	if err != nil {
		return backends.InstallResult{}, err
	}
	return backends.RollbackInstall(ctx, root, func(ctx context.Context, rec backends.InstallRecord) error {
		return b.installer.probe(ctx, rec.Directory)
	})
}

// InstallStatus reports the managed checkout. A host install is used through a
// config-path profile and needs no status of its own.
func (b *Backend) InstallStatus(context.Context) (backends.InstallStatus, error) {
	root, err := b.engineRoot()
	if err != nil {
		return backends.InstallStatus{}, err
	}
	status, _, err := backends.ManagedStatus(root)
	return status, err
}

// probe proves a checkout can run its launcher before it is trusted.
func (i *installer) probe(ctx context.Context, checkout string) error {
	err := i.backend.opts.runner.Run(ctx, checkout, io.Discard, venvPython(checkout), filepath.Join(checkout, "setup.py"), "--help")
	if err != nil {
		return fmt.Errorf("validate Strata checkout: %w", err)
	}
	return nil
}

func (i *installer) install(ctx context.Context, opts backends.InstallOptions) (backends.InstallResult, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	root, state, version, err := i.inspect(opts)
	if err != nil {
		return backends.InstallResult{}, err
	}
	dir := filepath.Join(root, "src-"+version)
	if current(state.Active, version, dir) && !opts.Force {
		return backends.InstallResult{
			Version: version, Path: venvPython(dir), Message: "strata " + version + " already installed",
		}, nil
	}
	progress := cmp.Or[io.Writer](opts.Progress, io.Discard)
	if err := i.prepare(ctx, root, dir, version, progress); err != nil {
		return backends.InstallResult{}, err
	}
	return i.record(root, state, dir, version)
}

// inspect checks the host and request, and reads the recorded install state.
func (i *installer) inspect(opts backends.InstallOptions) (string, backends.InstallState, string, error) {
	var state backends.InstallState
	if i.backend.opts.goos != "linux" {
		return "", state, "", fmt.Errorf("managed Strata install supports linux; current host is %s", i.backend.opts.goos)
	}
	version := cmp.Or(opts.Version, ManagedVersion)
	if strings.HasPrefix(version, "-") || strings.Contains(version, "..") || strings.ContainsAny(version, `/\ `) {
		return "", state, "", fmt.Errorf("invalid Strata version %q", version)
	}
	root, err := i.backend.engineRoot()
	if err != nil {
		return "", state, "", err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", state, "", err
	}
	state, err = backends.ReadInstallState(root)
	if err != nil {
		return "", state, "", err
	}
	if opts.Upgrade && state.Active == nil {
		return "", state, "", ErrNoManagedInstall
	}
	return root, state, version, nil
}

// current reports whether active is version at dir and still runnable.
func current(active *backends.InstallRecord, version, dir string) bool {
	if active == nil || active.Version != version || active.Directory != dir {
		return false
	}
	_, err := os.Stat(venvPython(dir))
	return err == nil
}

// prepare clones the release (again, if an earlier clone did not finish) and
// installs Strata's own pinned requirements into the checkout's venv, which is
// where its setup expects them.
func (i *installer) prepare(ctx context.Context, root, dir, version string, progress io.Writer) error {
	if _, err := os.Stat(filepath.Join(dir, "setup.py")); err != nil {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		if err := i.run(ctx, root, progress, i.backend.opts.Git,
			"clone", "--depth", "1", "--branch", version, RepositoryURL, dir); err != nil {
			return fmt.Errorf("clone Strata %s: %w", version, err)
		}
	}
	if _, err := os.Stat(venvPython(dir)); err != nil {
		if err := i.run(ctx, dir, progress, i.backend.opts.Python, "-m", "venv", filepath.Join(dir, ".venv")); err != nil {
			return fmt.Errorf("create Python environment: %w", err)
		}
	}
	if err := i.run(ctx, dir, progress, venvPython(dir), "-m", "pip", "install", "-r", filepath.Join(dir, "requirements.txt")); err != nil {
		return fmt.Errorf("install Strata requirements: %w", err)
	}
	return i.probe(ctx, dir)
}

func (i *installer) record(root string, state backends.InstallState, dir, version string) (backends.InstallResult, error) {
	record := &backends.InstallRecord{
		Backend:     Name,
		Version:     version,
		Source:      RepositoryURL + "@" + version,
		Platform:    i.backend.opts.goos + "/" + i.backend.opts.goarch,
		Directory:   dir,
		Executable:  venvPython(dir),
		InstalledAt: time.Now().UTC(),
	}
	backends.RetirePrevious(root, state, record.Directory)
	if err := backends.WriteInstallState(root, backends.InstallState{Active: record, Previous: state.Active}); err != nil {
		return backends.InstallResult{}, err
	}
	return backends.InstallResult{
		Version: version, Path: record.Executable, Changed: true, Message: "installed strata " + version,
	}, nil
}

func (i *installer) run(ctx context.Context, dir string, progress io.Writer, name string, args ...string) error {
	if _, err := fmt.Fprintln(progress, name+" "+strings.Join(args, " ")); err != nil {
		return err
	}
	return i.backend.opts.runner.Run(ctx, dir, progress, name, args...)
}
