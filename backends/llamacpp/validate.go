package llamacpp

import (
	"fmt"

	"inferencerig/core/profiles"
)

// defaultListenHost is the loopback host filled in when a profile omits one.
const defaultListenHost = "127.0.0.1"

// ValidateProfile checks and normalizes the llama.cpp-specific fields of an
// already common-validated profile and returns the effective profile. It
// requires a model source and a valid listen port, verifies every engine_arg
// maps to a renderable models.ini key/value, and defaults the listen host. It
// satisfies profiles.BackendValidator.
func (b *Backend) ValidateProfile(p profiles.Profile) (profiles.Profile, error) {
	if p.Model.Source == "" {
		return profiles.Profile{}, fmt.Errorf("model.source is required")
	}
	if p.Listen.Port < 1 || p.Listen.Port > 65535 {
		return profiles.Profile{}, fmt.Errorf("listen.port %d out of range", p.Listen.Port)
	}
	if _, err := engineArgValues(p.EngineArgs); err != nil {
		return profiles.Profile{}, err
	}
	// The storage directory only decides where a section's model path points,
	// never whether the profile renders, so validation needs no real one.
	if _, err := profileSection(p, ""); err != nil {
		return profiles.Profile{}, err
	}
	if p.Listen.Host == "" {
		p.Listen.Host = defaultListenHost
	}
	return p, nil
}
