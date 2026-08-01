package llamacpp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"inferencerig/backends"
	"inferencerig/config"
)

// Accel is a llama.cpp compute backend (accelerator) selection.
type Accel string

const (
	AccelAuto   Accel = "auto"
	AccelCPU    Accel = "cpu"
	AccelCUDA   Accel = "cuda"
	AccelROCm   Accel = "rocm"
	AccelVulkan Accel = "vulkan"
	AccelMetal  Accel = "metal"
)

// ErrNoManagedInstall marks an upgrade with nothing installed to upgrade.
var ErrNoManagedInstall = errors.New("no managed llama.cpp install")

// Release identifies a resolved llama.cpp release to install.
type Release struct {
	Version    string
	TarballURL string
	Assets     []ReleaseAsset
}

// ReleaseAsset is one downloadable prebuilt artifact of a release.
type ReleaseAsset struct {
	Name   string
	URL    string
	Digest string
	Size   int64
}

// Payload is a staged engine payload: the executable to activate plus the
// provenance recorded for it.
type Payload struct {
	Executable string
	// Source is where the payload came from (the release asset URL).
	Source string
	// Digest is the verified content digest of the downloaded asset.
	Digest string
}

// Fetcher resolves and provisions the managed engine payload. The default
// GitHub fetcher hits the network; tests inject a hermetic stub so the install
// state machine (idempotency, activation, retention) is exercised offline.
type Fetcher interface {
	// Resolve reports the release to install for accel; version "" means latest.
	Resolve(ctx context.Context, accel Accel, version string) (Release, error)
	// Fetch places the engine payload under dir and describes what it staged.
	Fetch(ctx context.Context, rel Release, accel Accel, dir string, progress io.Writer) (Payload, error)
}

// probeTimeout bounds the version probe a staged binary must answer before it
// is allowed to become the active install.
const probeTimeout = 30 * time.Second

// installer is the managed llama.cpp install/upgrade state machine. Ported and
// neutralized from llamarig core/llamainstall (install/state/detection/
// retention); the network+archive payload logic lives behind Fetcher.
type installer struct {
	root    string
	fetcher Fetcher
	goos    string
	goarch  string
	mu      sync.Mutex
}

func newInstaller(root string, fetcher Fetcher) *installer {
	if fetcher == nil {
		fetcher = &githubFetcher{goos: runtime.GOOS, goarch: runtime.GOARCH}
	}
	return &installer{root: root, fetcher: fetcher, goos: runtime.GOOS, goarch: runtime.GOARCH}
}

// resolveRoot returns the managed-install root, defaulting to <home>/engine/llamacpp.
func (i *installer) resolveRoot() (string, error) {
	if i.root != "" {
		return i.root, nil
	}
	home, err := config.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "engine", Name), nil
}

// activeExecutable returns the path of the active managed router binary, if any.
func (i *installer) activeExecutable() (string, bool) {
	root, err := i.resolveRoot()
	if err != nil {
		return "", false
	}
	st, err := backends.ReadInstallState(root)
	if err != nil || st.Active == nil || st.Active.Executable == "" {
		return "", false
	}
	if info, err := os.Stat(st.Active.Executable); err != nil || info.IsDir() {
		return "", false
	}
	return st.Active.Executable, true
}

// Install installs or upgrades the managed engine and reports what happened.
// It is idempotent: when the resolved release is already active (and Force is
// unset) nothing is fetched and Changed is false.
func (b *Backend) Install(ctx context.Context, opts backends.InstallOptions) (backends.InstallResult, error) {
	return b.installer.install(ctx, opts)
}

// Rollback returns the managed install to the previously recorded version. The
// retention policy keeps that install on disk, so restoring it is a state swap
// once its binary answers the version probe.
func (b *Backend) Rollback(ctx context.Context) (backends.InstallResult, error) {
	b.installer.mu.Lock()
	defer b.installer.mu.Unlock()
	root, err := b.installer.resolveRoot()
	if err != nil {
		return backends.InstallResult{}, err
	}
	return backends.RollbackInstall(ctx, root, func(ctx context.Context, rec backends.InstallRecord) error {
		return probeExecutable(ctx, rec.Executable)
	})
}

// probeExecutable proves a binary is the runnable engine before it is trusted:
// a payload that cannot answer --version must never become the active install.
func probeExecutable(ctx context.Context, executable string) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, executable, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("version probe of %s failed: %w: %s", executable, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// InstallStatus reports a valid managed binary first, then falls back to the
// configured host executable.
func (b *Backend) InstallStatus(context.Context) (backends.InstallStatus, error) {
	root, err := b.installer.resolveRoot()
	if err != nil {
		return backends.InstallStatus{}, err
	}
	state, err := backends.ReadInstallState(root)
	if err != nil {
		return backends.InstallStatus{}, err
	}
	if state.Active != nil && usableExecutable(state.Active.Executable) {
		return backends.InstallStatus{
			Installed: true, Managed: true,
			Version: state.Active.Version, Path: state.Active.Executable,
		}, nil
	}
	path, err := exec.LookPath(b.opts.Executable)
	if err != nil {
		return backends.InstallStatus{}, nil
	}
	return backends.InstallStatus{Installed: true, Path: path}, nil
}

func usableExecutable(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (i *installer) install(ctx context.Context, opts backends.InstallOptions) (backends.InstallResult, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	root, err := i.resolveRoot()
	if err != nil {
		return backends.InstallResult{}, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return backends.InstallResult{}, fmt.Errorf("create engine root: %w", err)
	}
	current, err := backends.ReadInstallState(root)
	if err != nil {
		return backends.InstallResult{}, err
	}
	if opts.Upgrade && current.Active == nil {
		return backends.InstallResult{}, ErrNoManagedInstall
	}
	accel := i.detect(ctx)
	rel, err := i.fetcher.Resolve(ctx, accel, opts.Version)
	if err != nil {
		return backends.InstallResult{}, err
	}
	if current.Active != nil && current.Active.Version == rel.Version && current.Active.Accelerator == string(accel) && !opts.Force {
		return backends.InstallResult{
			Version: rel.Version, Path: current.Active.Executable, Changed: false,
			Message: "llama.cpp " + rel.Version + " already installed",
		}, nil
	}
	return i.provision(ctx, root, current, rel, accel, opts.Progress)
}

func (i *installer) provision(ctx context.Context, root string, current backends.InstallState, rel Release, accel Accel, progress io.Writer) (backends.InstallResult, error) {
	if progress == nil {
		progress = io.Discard
	}
	staging, err := os.MkdirTemp(root, ".staging-*")
	if err != nil {
		return backends.InstallResult{}, err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	payload, err := i.fetcher.Fetch(ctx, rel, accel, staging, progress)
	if err != nil {
		return backends.InstallResult{}, err
	}
	// Probe while the payload is still staged: a binary that fails here is
	// discarded with the staging directory and never replaces a working install.
	if err := probeExecutable(ctx, payload.Executable); err != nil {
		return backends.InstallResult{}, err
	}
	next, err := i.activate(root, rel, accel, staging, payload)
	if err != nil {
		return backends.InstallResult{}, err
	}
	i.retire(root, current)
	if err := backends.WriteInstallState(root, backends.InstallState{Active: next, Previous: current.Active}); err != nil {
		return backends.InstallResult{}, err
	}
	return backends.InstallResult{
		Version: next.Version, Path: next.Executable, Changed: true,
		Message: "installed llama.cpp " + next.Version,
	}, nil
}

// activate moves the staged payload into its final managed location and returns
// the record of what was installed.
func (i *installer) activate(root string, rel Release, accel Accel, staging string, payload Payload) (*backends.InstallRecord, error) {
	final := filepath.Join(root, rel.Version, fmt.Sprintf("%s-%s-%s", i.goos, i.goarch, accel))
	if !managedPath(root, final) {
		return nil, fmt.Errorf("unsafe install path %q", final)
	}
	if err := os.MkdirAll(filepath.Dir(final), 0o700); err != nil {
		return nil, err
	}
	relExe, err := filepath.Rel(staging, payload.Executable)
	if err != nil {
		return nil, err
	}
	if err := os.RemoveAll(final); err != nil {
		return nil, fmt.Errorf("replace install: %w", err)
	}
	if err := os.Rename(staging, final); err != nil {
		return nil, fmt.Errorf("activate install: %w", err)
	}
	return &backends.InstallRecord{
		Backend:     Name,
		Version:     rel.Version,
		Source:      payload.Source,
		Digest:      payload.Digest,
		Platform:    i.goos + "/" + i.goarch,
		Accelerator: string(accel),
		Directory:   final,
		Executable:  filepath.Join(final, relExe),
		InstalledAt: time.Now().UTC(),
	}, nil
}

// retire enforces retention (keep active + previous only): the outgoing active
// becomes the new previous, so the old previous install directory is removed.
func (i *installer) retire(root string, current backends.InstallState) {
	old := current.Previous
	if old == nil {
		return
	}
	if current.Active != nil && old.Directory == current.Active.Directory {
		return
	}
	if managedPath(root, old.Directory) {
		_ = os.RemoveAll(old.Directory)
		_ = os.Remove(filepath.Dir(old.Directory))
	}
}

// managedPath reports whether candidate is safely under root.
func managedPath(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
