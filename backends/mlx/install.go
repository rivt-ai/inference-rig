package mlx

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
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

// ErrNoManagedInstall marks an upgrade without an existing environment.
var ErrNoManagedInstall = errors.New("no managed MLX install")

// ErrUnlockedVersion marks a request for a version this build has no lock for.
var ErrUnlockedVersion = errors.New("unlocked mlx-lm version")

// importProbe is the smallest statement that proves an environment can serve.
const importProbe = "import mlx_lm, mlx_lm.server"

// lockDigest identifies the artifact set an install recorded, so a record can
// be matched back to the exact pins it was produced from.
var lockDigest = sha256.Sum256([]byte(requirementsLock))

type commandRunner interface {
	Run(context.Context, string, io.Writer, string, ...string) error
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, dir string, out io.Writer, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir, command.Stdout, command.Stderr = dir, out, out
	return command.Run()
}

// requirementsLock is the complete pinned package set for ManagedVersion. It is
// embedded so an install never resolves anything at runtime: pip is handed this
// file and --no-deps, which is the whole supply-chain guarantee.
//
//go:embed requirements.lock
var requirementsLock string

// lockFileName is where the lock is written inside an environment, so the
// installed set stays inspectable next to what it produced.
const lockFileName = "requirements.lock"

type installer struct {
	backend *Backend
	mu      sync.Mutex
}

// Install creates or upgrades the managed Python environment idempotently.
func (b *Backend) Install(ctx context.Context, opts backends.InstallOptions) (backends.InstallResult, error) {
	return b.installer.install(ctx, opts)
}

// Rollback returns the managed environment to the previously recorded version.
// Environments are version-scoped, so the previous one is still on disk and
// restoring it is a state swap once its interpreter can import the server.
func (b *Backend) Rollback(ctx context.Context) (backends.InstallResult, error) {
	b.installer.mu.Lock()
	defer b.installer.mu.Unlock()
	root, err := b.engineRoot()
	if err != nil {
		return backends.InstallResult{}, err
	}
	return backends.RollbackInstall(ctx, root, func(ctx context.Context, rec backends.InstallRecord) error {
		return b.installer.probe(ctx, rec.Executable)
	})
}

// probe proves an environment can serve before it is trusted: a Python that
// cannot import the server package must never become the active install.
func (i *installer) probe(ctx context.Context, python string) error {
	if err := i.backend.opts.runner.Run(ctx, "", io.Discard, python, "-c", importProbe); err != nil {
		return fmt.Errorf("validate mlx-lm: %w", err)
	}
	return nil
}

// InstallStatus reports a valid managed environment first, then checks whether
// the configured host Python can import the server package.
func (b *Backend) InstallStatus(ctx context.Context) (backends.InstallStatus, error) {
	root, err := b.engineRoot()
	if err != nil {
		return backends.InstallStatus{}, err
	}
	status, ok, err := backends.ManagedStatus(root)
	if err != nil || ok {
		return status, err
	}
	path, err := exec.LookPath(b.opts.Executable)
	if err != nil {
		return backends.InstallStatus{}, nil
	}
	if err := b.opts.runner.Run(ctx, "", io.Discard, path, "-c", importProbe); err != nil {
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
	if inspection.active != nil && inspection.active.Version == inspection.version &&
		inspection.active.Executable == inspection.python && inspection.statErr == nil && !opts.Force {
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
	return i.installPackage(ctx, root, inspection, progress)
}

type installInspection struct {
	state   backends.InstallState
	active  *backends.InstallRecord
	dir     string
	python  string
	statErr error
	version string
}

func inspectInstall(root string, opts backends.InstallOptions) (installInspection, error) {
	state, err := backends.ReadInstallState(root)
	if err != nil {
		return installInspection{}, err
	}
	version := opts.Version
	if version == "" {
		version = ManagedVersion
	}
	// ponytail: one installable version, the locked one. Honouring an arbitrary
	// requested version would mean resolving its dependencies at runtime, which
	// is exactly the unpinned install the lock file exists to remove; ship a
	// lock per version if managed installs ever need to span versions.
	if version != ManagedVersion {
		return installInspection{}, fmt.Errorf(
			"%w: mlx-lm %s is not installable; this build pins the locked set for mlx-lm %s",
			ErrUnlockedVersion, version, ManagedVersion,
		)
	}
	dir := filepath.Join(root, "venv-"+version)
	python := filepath.Join(dir, "bin", "python")
	_, statErr := os.Stat(python)
	if opts.Upgrade && state.Active == nil {
		return installInspection{}, ErrNoManagedInstall
	}
	return installInspection{
		state: state, active: state.Active, dir: dir,
		python: python, statErr: statErr, version: version,
	}, nil
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
	if err := i.run(ctx, root, progress, i.backend.opts.Executable, "-m", "venv", filepath.Dir(filepath.Dir(python))); err != nil {
		return fmt.Errorf("create Python environment: %w", err)
	}
	if _, err := os.Stat(python); err != nil {
		return fmt.Errorf("managed Python was not created: %w", err)
	}
	return nil
}

// installPackage installs exactly the locked set into the environment, probes
// it, and only then records it as active.
func (i *installer) installPackage(ctx context.Context, root string, in installInspection, progress io.Writer) (backends.InstallResult, error) {
	lock := filepath.Join(in.dir, lockFileName)
	if err := os.WriteFile(lock, []byte(requirementsLock), 0o600); err != nil {
		return backends.InstallResult{}, err
	}
	// --no-deps: the lock is the whole dependency set, so pip installs it and
	// resolves nothing of its own.
	if err := i.run(ctx, root, progress, in.python, "-m", "pip", "install", "--no-deps", "-r", lock); err != nil {
		return backends.InstallResult{}, fmt.Errorf("install locked mlx-lm %s: %w", in.version, err)
	}
	if err := i.probe(ctx, in.python); err != nil {
		return backends.InstallResult{}, err
	}
	record := &backends.InstallRecord{
		Backend:     Name,
		Version:     in.version,
		Source:      "pypi:" + lockFileName,
		Digest:      "sha256:" + hex.EncodeToString(lockDigest[:]),
		Platform:    i.backend.opts.goos + "/" + i.backend.opts.goarch,
		Directory:   in.dir,
		Executable:  in.python,
		InstalledAt: time.Now().UTC(),
	}
	backends.RetirePrevious(root, in.state, record.Directory)
	if err := backends.WriteInstallState(root, backends.InstallState{Active: record, Previous: in.active}); err != nil {
		return backends.InstallResult{}, err
	}
	return backends.InstallResult{
		Version: in.version, Path: in.python, Changed: true, Message: "installed mlx-lm " + in.version,
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
	return backends.ActiveExecutable(root)
}

// VerifyInstall re-hashes the requirements lock against the digest recorded at
// install time. This backend installs a Python environment rather than a single
// binary, so the lock file is what its digest covers.
func (b *Backend) VerifyInstall(_ context.Context, record backends.InstallRecord) (backends.DigestVerification, error) {
	path := ""
	if record.Directory != "" {
		path = filepath.Join(record.Directory, lockFileName)
	}
	return backends.VerifyRecordedDigest(record, path)
}
