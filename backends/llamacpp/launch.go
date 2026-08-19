package llamacpp

import (
	"strconv"

	"inferencerig/backends"
	"inferencerig/config"
	"inferencerig/core/profiles"
	"inferencerig/core/runtime"
)

// routerName is the supervised router process (and PID-file) name.
const routerName = "router"

// readinessPath is the llama-server endpoint polled for readiness.
const readinessPath = "/health"

// LaunchSpec builds the Phase-3 supervisor spec for the llama-server router
// process. It points the router at the generated models.ini via --models-preset,
// binds the profile's listen host/port, and probes /health. --models-dir is
// passed only when ExposeModelsWithoutProfile is set: it makes the router serve
// every model file in storage, including those no profile declares, so by
// default the preset — one section per profile — is the router's only source of
// models. The preset names each model by absolute path, so it stands on its own
// without a models dir. A path-resolution failure is deferred via
// LaunchSpec.BuildErr so it surfaces cleanly at Start rather than launching a
// misconfigured process. Modeled on llamarig core/runtime/build.go BuildRouter.
func (b *Backend) LaunchSpec(p profiles.Profile, _ backends.Materialization) (runtime.LaunchSpec, error) {
	spec := runtime.LaunchSpec{
		Name:          routerName,
		Executable:    b.routerExecutable(),
		Host:          p.Listen.Host,
		Port:          p.Listen.Port,
		ReadinessPath: readinessPath,
		LogName:       config.LogServiceEngine,
	}
	iniPath, err := b.generatedININPath()
	if err != nil {
		spec.BuildErr = err
		return spec, nil
	}
	pidDir, err := b.pidDir()
	if err != nil {
		spec.BuildErr = err
		return spec, nil
	}
	spec.PIDDir = pidDir
	if b.opts.ExposeModelsWithoutProfile {
		modelsDir, err := b.modelStorageDir()
		if err != nil {
			spec.BuildErr = err
			return spec, nil
		}
		spec.Argv = append(spec.Argv, "--models-dir", modelsDir)
	}
	spec.Argv = append(spec.Argv,
		"--models-preset", iniPath,
		"--models-max", strconv.Itoa(b.opts.ModelsMax),
		"--host", p.Listen.Host,
		"--port", strconv.Itoa(p.Listen.Port),
	)
	return spec, nil
}

// routerExecutable prefers the managed install's active binary, else the
// configured default executable.
func (b *Backend) routerExecutable() string {
	if exe, ok := b.installer.activeExecutable(); ok {
		return exe
	}
	return b.opts.Executable
}
