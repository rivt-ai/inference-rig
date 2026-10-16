// Package strata implements the Strata backend behind InferenceRig's neutral
// backend contract. Strata ships its own Python launcher (setup.py) that
// fetches the engine, downloads and prepares a model, and its own server
// (serve/server.py); this backend drives those rather than re-implementing
// them, so all Strata-specific command, install, and layout policy lives here.
package strata

import (
	"context"
	"path/filepath"
	"runtime"

	"inferencerig/backends"
	"inferencerig/config"
	"inferencerig/core/modelcatalog"
)

const (
	// Name is the stable registry key.
	Name = "strata"
	// ManagedVersion is the pinned Strata release tag installed by default.
	ManagedVersion = "v0.1.44.1"
	// RepositoryURL is where managed installs are cloned from.
	RepositoryURL = "https://github.com/Niko1221/Strata.git"

	defaultPython = "python3"
	defaultGit    = "git"
)

// Options configures a Backend. Empty fields use InferenceRig home defaults.
type Options struct {
	ModelStorageDir string
	PIDDir          string
	EngineRoot      string
	// Python is the host interpreter a managed install creates its venv with.
	Python string
	// Git clones the managed checkout.
	Git    string
	runner commandRunner
	goos   string
	goarch string
}

// Backend implements backends.Backend for Strata.
type Backend struct {
	opts      Options
	installer *installer
}

// New creates a Strata backend.
func New(opts Options) *Backend {
	if opts.Python == "" {
		opts.Python = defaultPython
	}
	if opts.Git == "" {
		opts.Git = defaultGit
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

// Name returns the registry key.
func (b *Backend) Name() string { return Name }

// Capabilities advertises a multi-file model (Strata's setup prepares a pack
// directory beside the GGUF shards), a discrete-VRAM fit, managed installation,
// and one active profile — Strata holds one resident sequence per GPU.
func (b *Backend) Capabilities() backends.Capabilities {
	return backends.Capabilities{
		MultiFileArtifacts:     true,
		DiscreteVRAM:           true,
		ManagedInstall:         true,
		SingleActiveProfile:    true,
		ParameterIntrospection: true,
	}
}

// Parameters describes the canonical profile inputs and the commonly set
// engine args. Which command engine_args reach depends on model.source: a
// setup-written config runs the server directly, a size name runs setup.
func (b *Backend) Parameters(context.Context) ([]backends.Parameter, error) {
	return []backends.Parameter{
		{
			Name:        "model.source",
			Description: "Strata model size (managed install) or path to a setup-written strata-<size>.json",
			Required:    true, Type: backends.ParameterString, ValueHint: "IQ2_XS",
		},
		{
			Name: "listen.host", Description: "server listen host",
			Type: backends.ParameterString, ValueHint: "127.0.0.1",
		},
		{
			Name: "listen.port", Description: "server listen port",
			Required: true, Type: backends.ParameterInt, ValueHint: "8080",
		},
		{Name: "engine_args.*", Description: "setup or server command argument", Type: backends.ParameterString},
		{
			Name: "engine_args.family", Description: "model family (size-name sources only)",
			Type: backends.ParameterString, ValueHint: "qwen",
		},
		{
			Name: "engine_args.context", Description: "context length (size-name sources only)",
			Type: backends.ParameterInt, ValueHint: "65536",
		},
		{
			Name: "engine_args.kv", Description: "KV cache type (size-name sources only)",
			Type: backends.ParameterString, ValueHint: "int8",
		},
		{
			Name: "engine_args.vision", Description: "image input (size-name sources only)",
			Type: backends.ParameterString, ValueHint: "no",
		},
		{
			Name: "engine_args.gpu", Description: "GPU index, or several for a layer split",
			Type: backends.ParameterString, ValueHint: "0",
		},
		{
			Name: "engine_args.api-key", Description: "require this key on /v1/*",
			Type: backends.ParameterString,
		},
	}, nil
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
	return backends.EngineRoot(Name)
}

// CatalogPolicy returns the catalog adapter. Strata's setup owns model
// downloads, so the shared catalog has nothing to offer or list for it.
func (b *Backend) CatalogPolicy() modelcatalog.CatalogPolicy { return catalogPolicy{} }

type catalogPolicy struct{}

func (catalogPolicy) SearchFilter() string { return "gguf" }

func (catalogPolicy) Variants(modelcatalog.Source, []modelcatalog.RemoteFile) ([]modelcatalog.Variant, error) {
	return nil, nil
}

func (catalogPolicy) ListLocal(context.Context) ([]modelcatalog.LocalModel, error) { return nil, nil }

func (catalogPolicy) DeleteLocal(string) error { return errSetupOwnsModels }

var _ backends.Backend = (*Backend)(nil)
var _ backends.ParameterProvider = (*Backend)(nil)
