// Package mlx implements the MLX backend behind InferenceRig's neutral backend
// contract. All MLX-specific command, snapshot, memory, and install policy lives
// here.
package mlx

import (
	"net/http"
	"path/filepath"
	"runtime"
	"time"

	"inferencerig/backends"
	"inferencerig/config"
	"inferencerig/core/modelcatalog"
)

const (
	// Name is the stable registry key.
	Name = "mlx"
	// ManagedVersion is the pinned mlx-lm release installed by default.
	ManagedVersion = "0.31.3"

	defaultExecutable   = "python3"
	defaultReadinessURL = "/v1/models"
)

// Options configures a Backend. Empty fields use InferenceRig home defaults.
type Options struct {
	ModelStorageDir string
	PIDDir          string
	Executable      string
	EngineRoot      string
	HTTPClient      *http.Client
	HuggingFaceURL  string
	runner          commandRunner
	goos            string
	goarch          string
}

// Backend implements backends.Backend for MLX.
type Backend struct {
	opts      Options
	installer *installer
}

// New creates an MLX backend.
func New(opts Options) *Backend {
	if opts.Executable == "" {
		opts.Executable = defaultExecutable
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if opts.HuggingFaceURL == "" {
		opts.HuggingFaceURL = "https://huggingface.co"
	}
	if opts.runner == nil {
		opts.runner = execRunner{}
	}
	if opts.goos == "" {
		opts.goos = runtime.GOOS
	}
	if opts.goarch == "" {
		opts.goarch = runtime.GOARCH
	}
	b := &Backend{opts: opts}
	b.installer = &installer{backend: b}
	return b
}

// Register adds a default MLX backend to reg.
func Register(reg *backends.Registry) error { return reg.Register(New(Options{})) }

// Name returns the registry key.
func (b *Backend) Name() string { return Name }

// Capabilities advertises multi-file snapshots, unified memory, managed
// installation, and one active profile.
func (b *Backend) Capabilities() backends.Capabilities {
	return backends.Capabilities{
		MultiFileArtifacts:  true,
		UnifiedMemory:       true,
		ManagedInstall:      true,
		SingleActiveProfile: true,
	}
}

// CatalogPolicy returns the backend adapter for remote and local snapshots.
func (b *Backend) CatalogPolicy() modelcatalog.CatalogPolicy {
	return catalogPolicy{backend: b}
}

func (b *Backend) storageDir() (string, error) {
	if b.opts.ModelStorageDir != "" {
		return b.opts.ModelStorageDir, nil
	}
	return config.DefaultModelStorageDir()
}

func (b *Backend) pidDir() (string, error) {
	if b.opts.PIDDir != "" {
		return b.opts.PIDDir, nil
	}
	home, err := config.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "run"), nil
}

func (b *Backend) engineRoot() (string, error) {
	if b.opts.EngineRoot != "" {
		return b.opts.EngineRoot, nil
	}
	home, err := config.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "engine", Name), nil
}

var _ backends.Backend = (*Backend)(nil)
var _ modelcatalog.DirectoryPolicy = snapshotPolicy{}
