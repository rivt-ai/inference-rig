package mlx

import (
	"fmt"

	"inferencerig/backends"
	"inferencerig/core/profiles"
	"inferencerig/core/runtime"
)

const defaultHost = "127.0.0.1"

// ValidateProfile validates MLX arguments and fills the loopback host.
func (b *Backend) ValidateProfile(p profiles.Profile) (profiles.Profile, error) {
	if p.Model.Source == "" {
		return profiles.Profile{}, errMissingModel
	}
	if p.Listen.Port < 1 || p.Listen.Port > 65535 {
		return profiles.Profile{}, fmt.Errorf("listen.port %d out of range", p.Listen.Port)
	}
	if p.Listen.Host == "" {
		p.Listen.Host = defaultHost
	}
	if _, err := buildCommand(b.executable(), p); err != nil {
		return profiles.Profile{}, err
	}
	return p, nil
}

// Materialize renders the generated command without writing backend files.
func (b *Backend) Materialize(p profiles.Profile) (backends.Materialization, error) {
	command, err := buildCommand(b.executable(), p)
	if err != nil {
		return backends.Materialization{}, err
	}
	return backends.Materialization{Summary: command.Display}, nil
}

// LaunchSpec returns the neutral supervisor specification for one profile.
func (b *Backend) LaunchSpec(p profiles.Profile, _ backends.Materialization) (runtime.LaunchSpec, error) {
	spec := runtime.LaunchSpec{
		Name:          "mlx-" + p.Name,
		Host:          p.Listen.Host,
		Port:          p.Listen.Port,
		ReadinessPath: defaultReadinessURL,
	}
	command, err := buildCommand(b.executable(), p)
	if err != nil {
		spec.BuildErr = fmt.Errorf("render MLX command: %w", err)
		return spec, nil
	}
	spec.Executable, spec.Argv = command.Executable, command.Argv
	spec.PIDDir, err = b.pidDir()
	if err != nil {
		spec.BuildErr = err
	}
	return spec, nil
}

func (b *Backend) executable() string {
	if path, ok := b.installer.activeExecutable(); ok {
		return path
	}
	return b.opts.Executable
}
