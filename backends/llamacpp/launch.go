package llamacpp

import (
	"strconv"

	"inferencerig/backends"
	"inferencerig/core/profiles"
	"inferencerig/core/runtime"
)

// routerName is the supervised router process (and PID-file) name.
const routerName = "router"

// readinessPath is the llama-server endpoint polled for readiness.
const readinessPath = "/health"

// LaunchSpec builds the Phase-3 supervisor spec for the llama-server router
// process. It points the router at the generated models.ini via --models-preset
// and at the model storage dir via --models-dir, binds the profile's listen
// host/port, and probes /health. A path-resolution failure is deferred via
// LaunchSpec.BuildErr so it surfaces cleanly at Start rather than launching a
// misconfigured process. Modeled on llamarig core/runtime/build.go BuildRouter.
func (b *Backend) LaunchSpec(p profiles.Profile, _ backends.Materialization) (runtime.LaunchSpec, error) {
	spec := runtime.LaunchSpec{
		Name:          routerName,
		Executable:    b.routerExecutable(),
		Host:          p.Listen.Host,
		Port:          p.Listen.Port,
		ReadinessPath: readinessPath,
	}
	iniPath, err := b.generatedININPath()
	if err != nil {
		spec.BuildErr = err
		return spec, nil
	}
	modelsDir, err := b.modelStorageDir()
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
	spec.Argv = []string{
		"--models-dir", modelsDir,
		"--models-preset", iniPath,
		"--models-max", strconv.Itoa(b.opts.ModelsMax),
		"--host", p.Listen.Host,
		"--port", strconv.Itoa(p.Listen.Port),
	}
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
