// Package llamacpp is the llama.cpp backend: it implements the neutral
// backends.Backend contract for the llama.cpp inference engine. It owns every
// engine-specific concern — models.ini materialization, the llama-server router
// launch spec and HTTP client, GGUF model resolution and single-file artifact
// planning, discrete RAM/VRAM fit, host GPU telemetry, and managed engine
// install — behind the shared interfaces. The shared control plane never
// imports this package; the backend registers itself.
//
// Ported and neutralized from llamarig (core/modelpresets, core/router,
// core/llamainstall, core/modelcatalog, core/runtime/build.go, core/signals).
package llamacpp

import (
	"context"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"inferencerig/backends"
	"inferencerig/config"
	"inferencerig/core/modelcatalog"
)

// ggufExt is the file extension that identifies a single-file GGUF model.
const ggufExt = ".gguf"

// Name is the stable registry key for this backend.
const Name = "llamacpp"

// defaultExecutable is the PATH-resolvable router binary used when a profile or
// managed install does not point at a specific llama-server.
const defaultExecutable = "llama-server"

// Options configures a Backend. Every field has a neutral default resolved from
// config, so New(Options{}) yields a working backend; tests override paths and
// the install/telemetry seams to stay hermetic.
type Options struct {
	// ModelStorageDir is where resolved model artifacts are planned to land.
	ModelStorageDir string
	// GeneratedININPath overrides the generated models.ini location. Empty means
	// config.GeneratedDir(Name)/models.ini.
	GeneratedININPath string
	// PIDDir is the supervisor PID-file directory. Empty means <home>/run.
	PIDDir string
	// Executable is the default router binary. Empty means defaultExecutable.
	Executable string
	// ModelsMax caps concurrently loaded router models. Zero means 1.
	ModelsMax int
	// Defaults are the backend-wide models.ini keys rendered into the [*]
	// cascade section (canonical CLI keys). Empty renders no [*] section.
	Defaults map[string]string
	// Fetcher supplies the managed-install payload. Nil means the GitHub fetcher.
	Fetcher Fetcher
	// EngineRoot is the managed-install root. Empty means <home>/engine/llamacpp.
	EngineRoot string
	// gpu runs host GPU telemetry commands; tests inject a stub.
	gpu commandRunner
}

// Backend implements backends.Backend for llama.cpp.
type Backend struct {
	opts      Options
	installer *installer
}

// New builds a llama.cpp backend with defaults filled from Options.
func New(opts Options) *Backend {
	if opts.Executable == "" {
		opts.Executable = defaultExecutable
	}
	if opts.ModelsMax <= 0 {
		opts.ModelsMax = 1
	}
	if opts.gpu == nil {
		opts.gpu = execCommandRunner{}
	}
	b := &Backend{opts: opts}
	b.installer = newInstaller(opts.EngineRoot, opts.Fetcher)
	return b
}

// Register constructs the backend and adds it to the registry.
func Register(reg *backends.Registry) error {
	return reg.Register(New(Options{}))
}

// Name returns the backend key.
func (b *Backend) Name() string { return Name }

// Capabilities advertises a single-file (GGUF), discrete-VRAM, managed-install
// backend that ships a router HTTP client and serves many models at once.
func (b *Backend) Capabilities() backends.Capabilities {
	return backends.Capabilities{
		SingleFileArtifacts:    true,
		DiscreteVRAM:           true,
		ManagedInstall:         true,
		ParameterIntrospection: true,
	}
}

// Parameters describes the canonical profile inputs and the backend-owned
// engine argument namespace.
func (b *Backend) Parameters(context.Context) ([]backends.Parameter, error) {
	return []backends.Parameter{
		{
			Name: "model.source", Description: "model repository, URL, or local path",
			Required: true, Type: backends.ParameterString,
			ValueHint: "TheBloke/Llama-2-7B-GGUF",
		},
		{
			Name: "model.reference", Description: "artifact filename within a repository",
			Type: backends.ParameterString, ValueHint: "llama-2-7b.Q4_K_M.gguf",
		},
		{
			Name: "listen.host", Description: "server listen host",
			Type: backends.ParameterString, DefaultValue: defaultListenHost, ValueHint: defaultListenHost,
		},
		{
			Name: "listen.port", Description: "server listen port",
			Required: true, Type: backends.ParameterInt, ValueHint: "8080",
		},
		{
			Name: "engine_args.*", Description: "backend command/config argument",
			Type: backends.ParameterString,
		},
		// The engine args below are the ones worth completing: they are the
		// knobs that decide whether a model loads at all on a given machine.
		{
			Name: "engine_args.ctx-size", Aliases: []string{"c"},
			Description: "context window in tokens",
			Type:        backends.ParameterInt, ValueHint: "4096",
		},
		{
			Name: "engine_args.n-gpu-layers", Aliases: []string{"ngl"},
			Description: "layers to offload to the GPU; auto offloads as many as fit",
			Type:        backends.ParameterString, ValueHint: "auto",
		},
		{
			Name: "engine_args.threads", Aliases: []string{"t"},
			Description: "CPU threads used for generation",
			Type:        backends.ParameterInt,
		},
		{
			Name: "engine_args.flash-attn", Aliases: []string{"fa"},
			Description: "enable flash attention",
			Type:        backends.ParameterBool,
		},
		{
			Name: "engine_args.ubatch-size", Aliases: []string{"ub"},
			Description: "physical batch size",
			Type:        backends.ParameterInt, ValueHint: "512",
		},
	}, nil
}

// CatalogPolicy returns the backend adapter for remote variants and local
// single-file artifacts.
func (b *Backend) CatalogPolicy() modelcatalog.CatalogPolicy {
	return catalogPolicy{backend: b}
}

// generatedININPath resolves where the generated models.ini is written.
func (b *Backend) generatedININPath() (string, error) {
	if b.opts.GeneratedININPath != "" {
		return b.opts.GeneratedININPath, nil
	}
	dir, err := config.GeneratedDir(Name)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "models.ini"), nil
}

// pidDir resolves the supervisor PID-file directory.
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

// modelStorageDir resolves where resolved artifacts are planned to land.
func (b *Backend) modelStorageDir() (string, error) {
	if b.opts.ModelStorageDir != "" {
		return b.opts.ModelStorageDir, nil
	}
	return config.DefaultModelStorageDir()
}

// ggufPolicy is the GGUF format policy handed to the shared catalog mechanism:
// single-file models identified by the .gguf extension.
type ggufPolicy struct{}

func (ggufPolicy) IsModelFile(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ggufExt)
}
func (ggufPolicy) MultiFile() bool { return false }

// Ensure the policy satisfies the shared interface at compile time.
var _ modelcatalog.FormatPolicy = ggufPolicy{}

type catalogPolicy struct{ backend *Backend }

func (p catalogPolicy) Variants(source modelcatalog.Source, files []modelcatalog.RemoteFile) ([]modelcatalog.Variant, error) {
	var variants []modelcatalog.Variant
	for _, file := range files {
		if !(ggufPolicy{}).IsModelFile(file.Name) {
			continue
		}
		variants = append(variants, modelcatalog.Variant{
			Name: path.Base(file.Name), Reference: file.Name, SizeBytes: file.SizeBytes,
		})
	}
	sort.Slice(variants, func(i, j int) bool { return variants[i].Name < variants[j].Name })
	return variants, nil
}

func (p catalogPolicy) ListLocal(ctx context.Context) ([]modelcatalog.LocalModel, error) {
	root, err := p.backend.modelStorageDir()
	if err != nil {
		return nil, err
	}
	return modelcatalog.NewScanner(root, ggufPolicy{}).ListLocal(ctx)
}

func (p catalogPolicy) DeleteLocal(target string) error {
	root, err := p.backend.modelStorageDir()
	if err != nil {
		return err
	}
	return modelcatalog.RemoveLocal(root, target, false)
}

var _ modelcatalog.CatalogPolicy = catalogPolicy{}

// Ensure the backend satisfies the full contract at compile time.
var _ backends.Backend = (*Backend)(nil)
var _ backends.ParameterProvider = (*Backend)(nil)
